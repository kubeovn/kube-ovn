package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestPolicyNamedPortSelectorsAndResolution(t *testing.T) {
	pod := &corev1.Pod{
		Name:      "backend",
		Namespace: "remote",
		Labels:    map[string]string{"app": "backend"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.2,fd00::2",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 80, Protocol: corev1.ProtocolTCP},
			{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolUDP},
		}}}},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Namespaces: []*corev1.Namespace{{Name: "remote", Labels: map[string]string{"tenant": "prod"}}},
		Pods:       []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{Name: util.DefaultSubnet, Spec: kubeovnv1.SubnetSpec{
			Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolDual,
		}}},
	})
	require.NoError(t, err)
	c := fake.fakeController
	c.namedPort = NewNamedPort()
	c.namedPort.AddNamedPortByPod(pod)

	selectors, err := c.namedPortSelectors(
		&metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "prod"}},
		&metav1.LabelSelector{MatchLabels: map[string]string{"app": "backend"}},
	)
	require.NoError(t, err)
	variants, err := c.namedPortVariantsForSelectors(selectors, map[string]struct{}{"http": {}}, "anp.example")
	require.NoError(t, err)
	require.Len(t, variants, 2)
	for _, variant := range variants {
		require.Equal(t, []string{ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)}, variant.ports)
		require.Contains(t, variant.pgName, "anp.example.named.")
	}

	addresses, err := c.namedPortAddressesForSelectors(selectors, "http", "")
	require.NoError(t, err)
	require.Len(t, addresses, 2)
	for _, selected := range addresses {
		require.Equal(t, []string{"10.16.0.2"}, selected.v4)
		require.Equal(t, []string{"fd00::2"}, selected.v6)
	}

	addresses, err = c.namedPortAddressesForSelectors(selectors, "http", corev1.ProtocolUDP)
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.Equal(t, int32(8080), addresses[namedPortKey{protocol: corev1.ProtocolUDP, port: 8080}].variant.port)

	anpAddresses, err := c.namedPortAddressesForAnpEgressPeers([]v1alpha1.AdminNetworkPolicyEgressPeer{{
		Pods: &v1alpha1.NamespacedPod{NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "prod"}}, PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "backend"}}},
	}}, "http")
	require.NoError(t, err)
	require.Len(t, anpAddresses, 2)
	banpAddresses, err := c.namedPortAddressesForBanpEgressPeers([]v1alpha1.BaselineAdminNetworkPolicyEgressPeer{{
		Namespaces: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "prod"}},
	}}, "http")
	require.NoError(t, err)
	require.Len(t, banpAddresses, 2)
	cnpAddresses, err := c.namedPortAddressesForCnpEgressPeers([]v1alpha2.ClusterNetworkPolicyEgressPeer{{
		Namespaces: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "prod"}},
	}}, "http")
	require.NoError(t, err)
	require.Len(t, cnpAddresses, 2)
}

func TestPolicyNamedPortPeerSelectors(t *testing.T) {
	allNamespaces := &metav1.LabelSelector{}
	allPods := &metav1.LabelSelector{}
	for _, test := range []struct {
		name string
		got  func() (*metav1.LabelSelector, *metav1.LabelSelector)
	}{
		{name: "anp subject namespaces", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForAnpSubject(&v1alpha1.AdminNetworkPolicySubject{Namespaces: allNamespaces})
		}},
		{name: "anp subject pods", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForAnpSubject(&v1alpha1.AdminNetworkPolicySubject{Pods: &v1alpha1.NamespacedPod{NamespaceSelector: *allNamespaces, PodSelector: *allPods}})
		}},
		{name: "cnp subject namespaces", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForCnpSubject(&v1alpha2.ClusterNetworkPolicySubject{Namespaces: allNamespaces})
		}},
		{name: "cnp subject pods", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForCnpSubject(&v1alpha2.ClusterNetworkPolicySubject{Pods: &v1alpha2.NamespacedPod{NamespaceSelector: *allNamespaces, PodSelector: *allPods}})
		}},
		{name: "anp egress namespaces", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForAnpEgressPeer(&v1alpha1.AdminNetworkPolicyEgressPeer{Namespaces: allNamespaces})
		}},
		{name: "anp egress pods", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForAnpEgressPeer(&v1alpha1.AdminNetworkPolicyEgressPeer{Pods: &v1alpha1.NamespacedPod{NamespaceSelector: *allNamespaces, PodSelector: *allPods}})
		}},
		{name: "cnp egress namespaces", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForCnpEgressPeer(&v1alpha2.ClusterNetworkPolicyEgressPeer{Namespaces: allNamespaces})
		}},
		{name: "cnp egress pods", got: func() (*metav1.LabelSelector, *metav1.LabelSelector) {
			return namedPortSelectorsForCnpEgressPeer(&v1alpha2.ClusterNetworkPolicyEgressPeer{Pods: &v1alpha2.NamespacedPod{NamespaceSelector: *allNamespaces, PodSelector: *allPods}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			nsSelector, podSelector := test.got()
			require.NotNil(t, nsSelector)
			if podSelector != nil {
				require.NotNil(t, podSelector)
			}
		})
	}

	nsSelector, podSelector := namedPortSelectorsForAnpEgressPeer(&v1alpha1.AdminNetworkPolicyEgressPeer{})
	require.Nil(t, nsSelector)
	require.Nil(t, podSelector)
	nsSelector, podSelector = namedPortSelectorsForCnpEgressPeer(&v1alpha2.ClusterNetworkPolicyEgressPeer{})
	require.Nil(t, nsSelector)
	require.Nil(t, podSelector)
}

func TestPolicyNamedPortHelperVariants(t *testing.T) {
	name := "http"
	anpPorts := []v1alpha1.AdminNetworkPolicyPort{{NamedPort: &name}}
	anpIngress := []v1alpha1.AdminNetworkPolicyIngressRule{{Ports: &anpPorts}}
	anpEgress := []v1alpha1.AdminNetworkPolicyEgressRule{{Ports: &anpPorts}}
	require.Equal(t, map[string]struct{}{name: {}}, requestedNamedPortNamesAnpIngress(anpIngress))
	require.Equal(t, map[string]struct{}{name: {}}, requestedNamedPortNamesAnpEgress(anpEgress))
	require.True(t, hasAnpNamedPort(anpIngress, anpEgress))
	require.Equal(t, map[string]struct{}{name: {}}, requestedNamedPortNamesBanpIngress([]v1alpha1.BaselineAdminNetworkPolicyIngressRule{{Ports: &anpPorts}}))
	require.Equal(t, map[string]struct{}{name: {}}, requestedNamedPortNamesBanpEgress([]v1alpha1.BaselineAdminNetworkPolicyEgressRule{{Ports: &anpPorts}}))
	require.True(t, hasBanpNamedPort([]v1alpha1.BaselineAdminNetworkPolicyIngressRule{{Ports: &anpPorts}}, nil))

	protocols := []v1alpha2.ClusterNetworkPolicyProtocol{{DestinationNamedPort: name}}
	require.Equal(t, map[string]struct{}{name: {}}, requestedNamedPortNamesCnp([]v1alpha2.ClusterNetworkPolicyIngressRule{{Protocols: protocols}}))
	require.Equal(t, map[string]struct{}{name: {}}, requestedNamedPortNamesCnpEgress([]v1alpha2.ClusterNetworkPolicyEgressRule{{Protocols: protocols}}))
	require.True(t, hasCnpNamedPort(&v1alpha2.ClusterNetworkPolicy{Spec: v1alpha2.ClusterNetworkPolicySpec{
		Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{{Protocols: protocols}},
	}}))

	for _, test := range []struct {
		protocol corev1.Protocol
		check    func(v1alpha2.ClusterNetworkPolicyProtocol) bool
	}{
		{corev1.ProtocolTCP, func(p v1alpha2.ClusterNetworkPolicyProtocol) bool { return p.TCP != nil }},
		{corev1.ProtocolUDP, func(p v1alpha2.ClusterNetworkPolicyProtocol) bool { return p.UDP != nil }},
		{corev1.ProtocolSCTP, func(p v1alpha2.ClusterNetworkPolicyProtocol) bool { return p.SCTP != nil }},
	} {
		protocol := cnpNamedPortProtocol(namedPortKey{protocol: test.protocol, port: 80})
		require.True(t, test.check(protocol))
	}

	dst := map[namedPortKey]*policyNamedPortAddresses{}
	mergeNamedPortAddresses(dst, map[namedPortKey]*policyNamedPortAddresses{
		{protocol: corev1.ProtocolTCP, port: 80}: {v4: []string{"10.0.0.1"}},
	})
	mergeNamedPortAddresses(dst, map[namedPortKey]*policyNamedPortAddresses{
		{protocol: corev1.ProtocolTCP, port: 80}: {v6: []string{"fd00::1"}},
	})
	require.Equal(t, []string{"10.0.0.1"}, dst[namedPortKey{protocol: corev1.ProtocolTCP, port: 80}].v4)
	require.Equal(t, []string{"fd00::1"}, dst[namedPortKey{protocol: corev1.ProtocolTCP, port: 80}].v6)
	require.Equal(t, int32(80), namedPortNumber(name, corev1.ProtocolTCP, 80).PortNumber.Port)
}

func TestPolicyNamedPortGroupLifecycle(t *testing.T) {
	fake, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	c := fake.fakeController
	c.OVNNbClient = newInMemoryOVNNbClient(t)

	variant := networkPolicyNamedPortVariant{pgName: "anp.example.named.tcp80", ports: []string{"pod.remote.ovn"}}
	require.NoError(t, c.OVNNbClient.CreatePortGroup("stale.named.port", map[string]string{
		adminNetworkPolicyKey:     "anp/example",
		networkPolicyNamedPortKey: "true",
	}))
	require.NoError(t, c.ensurePolicyNamedPortGroups("anp.example", adminNetworkPolicyKey, "anp/example", []networkPolicyNamedPortVariant{variant}))
	groups, err := c.OVNNbClient.ListPortGroups(map[string]string{adminNetworkPolicyKey: "anp/example", networkPolicyNamedPortKey: "true"})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, variant.pgName, groups[0].Name)

	require.NoError(t, c.deletePolicyNamedPortGroups(adminNetworkPolicyKey, "anp/example"))
	groups, err = c.OVNNbClient.ListPortGroups(map[string]string{adminNetworkPolicyKey: "anp/example", networkPolicyNamedPortKey: "true"})
	require.NoError(t, err)
	require.Empty(t, groups)
}

func TestPolicyNamedPortSelectorsRejectInvalidLabelSelectors(t *testing.T) {
	c := &Controller{}
	_, err := c.namedPortSelectors(&metav1.LabelSelector{MatchLabels: map[string]string{"bad/key/extra": "value"}}, nil)
	require.ErrorContains(t, err, "error creating namespace label selector")
	_, err = c.namedPortSelectors(nil, &metav1.LabelSelector{MatchLabels: map[string]string{"bad/key/extra": "value"}})
	require.ErrorContains(t, err, "error creating pod label selector")
}
