package controller

import (
	"fmt"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestPolicySelectorCacheClonesResultsAndInvalidatesByGeneration(t *testing.T) {
	controller := &Controller{}
	generation := controller.policySelectorCacheGeneration()

	selected := []string{"10.0.0.1"}
	except := []string{"10.0.0.2"}
	controller.setPolicyAddressCache("peer", selected, except, generation)
	selected[0] = "10.0.0.3"
	except[0] = "10.0.0.4"

	gotSelected, gotExcept, ok := controller.getPolicyAddressCache("peer", generation)
	require.True(t, ok)
	require.Equal(t, []string{"10.0.0.1"}, gotSelected)
	require.Equal(t, []string{"10.0.0.2"}, gotExcept)

	gotSelected[0] = "192.0.2.1"
	gotExcept[0] = "192.0.2.2"
	gotSelected, gotExcept, ok = controller.getPolicyAddressCache("peer", generation)
	require.True(t, ok)
	require.Equal(t, []string{"10.0.0.1"}, gotSelected)
	require.Equal(t, []string{"10.0.0.2"}, gotExcept)

	controller.invalidatePolicySelectorCache()
	_, _, ok = controller.getPolicyAddressCache("peer", generation)
	require.False(t, ok)

	controller.setPolicyAddressCache("stale", []string{"10.0.0.5"}, nil, generation)
	_, _, ok = controller.getPolicyAddressCache("stale", generation)
	require.False(t, ok)
}

func TestPolicySelectorCacheClonesPodScanResults(t *testing.T) {
	controller := &Controller{}
	generation := controller.policySelectorCacheGeneration()
	ports := []string{"pod.default.ovn"}
	v4 := []string{"10.0.0.1"}
	v6 := []string{"fd00::1"}

	controller.setPolicyPodScanCache("selectors", ports, v4, v6, generation)
	ports[0], v4[0], v6[0] = "changed", "changed", "changed"

	gotPorts, gotV4, gotV6, ok := controller.getPolicyPodScanCache("selectors", generation)
	require.True(t, ok)
	require.Equal(t, []string{"pod.default.ovn"}, gotPorts)
	require.Equal(t, []string{"10.0.0.1"}, gotV4)
	require.Equal(t, []string{"fd00::1"}, gotV6)
}

func TestFetchPolicySelectedAddressesUsesCacheUntilPodEvent(t *testing.T) {
	firstPod := &corev1.Pod{
		Name:      "first",
		Namespace: metav1.NamespaceDefault,
		Labels:    map[string]string{"app": "selected"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.0.0.1",
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{firstPod},
		Subnets: []*kubeovnv1.Subnet{{
			Name: util.DefaultSubnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4},
		}},
	})
	require.NoError(t, err)
	controller := fake.fakeController
	peer := netv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "selected"}}}

	addresses, _, err := controller.fetchPolicySelectedAddresses(metav1.NamespaceDefault, kubeovnv1.ProtocolIPv4, peer, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1"}, addresses)
	emptyProviderAddresses, _, err := controller.fetchPolicySelectedAddresses(metav1.NamespaceDefault, kubeovnv1.ProtocolIPv4, peer, set.New[string]())
	require.NoError(t, err)
	require.Empty(t, emptyProviderAddresses)

	secondPod := firstPod.DeepCopy()
	secondPod.Name = "second"
	secondPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider)] = "10.0.0.2"
	require.NoError(t, controller.podIndexer.Add(secondPod))

	equivalentPeer := netv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "selected"}}}
	addresses, _, err = controller.fetchPolicySelectedAddresses(metav1.NamespaceDefault, kubeovnv1.ProtocolIPv4, equivalentPeer, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1"}, addresses)

	addresses, _, err = controller.fetchPolicySelectedAddresses(
		metav1.NamespaceDefault,
		kubeovnv1.ProtocolIPv4,
		peer,
		set.New(util.OvnProvider),
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"10.0.0.1", "10.0.0.2"}, addresses)
	addresses, _, err = controller.fetchPolicySelectedAddresses(
		metav1.NamespaceDefault,
		kubeovnv1.ProtocolIPv4,
		peer,
		set.New("other.default.ovn"),
	)
	require.NoError(t, err)
	require.Empty(t, addresses)

	controller.invalidatePolicySelectorCache()
	addresses, _, err = controller.fetchPolicySelectedAddresses(metav1.NamespaceDefault, kubeovnv1.ProtocolIPv4, peer, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"10.0.0.1", "10.0.0.2"}, addresses)
}

func TestPolicySelectorCacheInvalidatesOnNetworkAttachmentInformerEvents(t *testing.T) {
	fake, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	controller := fake.fakeController
	require.NoError(t, controller.addNetworkAttachmentEventHandler(fake.fakeInformers.netAttachInformer))

	nadClient := controller.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(metav1.NamespaceDefault)
	nad := &nadv1.NetworkAttachmentDefinition{
		Name:      "policy-cache",
		Namespace: metav1.NamespaceDefault,
	}

	assertInvalidated := func(name string, mutate func() error) {
		generation := controller.policySelectorCacheGeneration()
		controller.setPolicyAddressCache(name, []string{"10.0.0.1"}, nil, generation)
		require.NoError(t, mutate())
		require.Eventually(t, func() bool {
			_, _, ok := controller.getPolicyAddressCache(name, generation)
			return !ok
		}, time.Second, 10*time.Millisecond)
	}

	assertInvalidated("add", func() error {
		_, err := nadClient.Create(t.Context(), nad.DeepCopy(), metav1.CreateOptions{})
		return err
	})

	assertInvalidated("update", func() error {
		updated := nad.DeepCopy()
		updated.Spec.Config = `{"cniVersion":"0.3.1","name":"policy-cache","type":"bridge"}`
		_, err := nadClient.Update(t.Context(), updated, metav1.UpdateOptions{})
		return err
	})

	assertInvalidated("delete", func() error {
		return nadClient.Delete(t.Context(), nad.Name, metav1.DeleteOptions{})
	})
}

func TestPolicySelectorCacheRejectsInvalidLabelSelector(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"invalid key": "value"}}
	_, err := policyLabelSelectorCacheKey(selector)
	require.Error(t, err)

	peer := netv1.NetworkPolicyPeer{PodSelector: selector}
	_, _, err = (&Controller{}).fetchPolicySelectedAddresses(
		metav1.NamespaceDefault,
		kubeovnv1.ProtocolIPv4,
		peer,
		nil,
	)
	require.Error(t, err)
}
