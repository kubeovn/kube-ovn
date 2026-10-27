package controller

import (
	"fmt"
	"testing"

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
	require.NoError(t, fake.fakeInformers.podInformer.Informer().GetIndexer().Add(secondPod))

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

func TestPolicySelectorCacheInvalidatesOnPodAndNetworkAttachmentEvents(t *testing.T) {
	controller := &Controller{}

	generation := controller.policySelectorCacheGeneration()
	controller.setPolicyAddressCache("pod", []string{"10.0.0.1"}, nil, generation)
	controller.enqueueAddPod(&corev1.Pod{Spec: corev1.PodSpec{HostNetwork: true}})
	_, _, ok := controller.getPolicyAddressCache("pod", generation)
	require.False(t, ok)

	for _, event := range []struct {
		name string
		hand func()
	}{
		{name: "add", hand: func() { controller.enqueueAddNetworkAttachment(nil) }},
		{name: "update", hand: func() { controller.enqueueUpdateNetworkAttachment(nil, nil) }},
		{name: "delete", hand: func() { controller.enqueueDeleteNetworkAttachment(nil) }},
	} {
		t.Run(event.name, func(t *testing.T) {
			generation := controller.policySelectorCacheGeneration()
			controller.setPolicyAddressCache(event.name, []string{"10.0.0.1"}, nil, generation)
			event.hand()
			_, _, ok := controller.getPolicyAddressCache(event.name, generation)
			require.False(t, ok)
		})
	}
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
