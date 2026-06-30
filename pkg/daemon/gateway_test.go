package daemon

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestGetCidrByProtocol(t *testing.T) {
	cases := []struct {
		name     string
		cidr     string
		protocol string
		wantErr  bool
		expected string
	}{{
		name:     "ipv4 only",
		cidr:     "1.1.1.0/24",
		protocol: kubeovnv1.ProtocolIPv4,
		expected: "1.1.1.0/24",
	}, {
		name:     "ipv6 only",
		cidr:     "2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv6,
		expected: "2001:db8::/120",
	}, {
		name:     "get ipv4 from ipv6",
		cidr:     "2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv4,
	}, {
		name:     "get ipv4 from dual stack",
		cidr:     "1.1.1.0/24,2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv4,
		expected: "1.1.1.0/24",
	}, {
		name:     "get ipv6 from ipv4",
		cidr:     "1.1.1.0/24",
		protocol: kubeovnv1.ProtocolIPv6,
	}, {
		name:     "get ipv6 from dual stack",
		cidr:     "1.1.1.0/24,2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv6,
		expected: "2001:db8::/120",
	}, {
		name:     "invalid cidr",
		cidr:     "foo bar",
		protocol: kubeovnv1.ProtocolIPv4,
		wantErr:  true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := getCidrByProtocol(c.cidr, c.protocol)
			if (err != nil) != c.wantErr {
				t.Errorf("getCidrByProtocol(%q, %q) error = %v, wantErr = %v", c.cidr, c.protocol, err, c.wantErr)
			}
			require.Equal(t, c.expected, got)
		})
	}
}

func mkTProxyPod(ns, name string, annotations map[string]string, podIPs ...string) *corev1.Pod {
	ips := make([]corev1.PodIP, 0, len(podIPs))
	for _, ip := range podIPs {
		ips = append(ips, corev1.PodIP{IP: ip})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Annotations: annotations},
		Status:     corev1.PodStatus{PodIPs: ips},
	}
}

func TestGetPodPrimaryNetworkProvider(t *testing.T) {
	attachProvider := "macvlan.default.ovn"
	cases := []struct {
		name        string
		annotations map[string]string
		podIPs      []string
		expected    string
		found       bool
	}{{
		name: "primary cni",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
		},
		podIPs:   []string{"10.16.0.5"},
		expected: util.OvnProvider,
		found:    true,
	}, {
		name: "primary cni dual stack",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5,fd00:10:16::5",
		},
		podIPs:   []string{"10.16.0.5", "fd00:10:16::5"},
		expected: util.OvnProvider,
		found:    true,
	}, {
		name: "attachment network as multus default network",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider): "172.17.0.5",
		},
		podIPs:   []string{"172.17.0.5"},
		expected: attachProvider,
		found:    true,
	}, {
		name: "pure secondary cni",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider):   "172.17.0.5",
		},
		podIPs: []string{"192.168.0.5"},
	}, {
		name: "default provider preferred on identical ips",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider):   "10.16.0.5",
		},
		podIPs:   []string{"10.16.0.5"},
		expected: util.OvnProvider,
		found:    true,
	}, {
		name: "partial ip coverage does not match",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
		},
		podIPs: []string{"10.16.0.5", "fd00:10:16::5"},
	}, {
		name: "no pod ips",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
		},
	}, {
		name:   "no annotations",
		podIPs: []string{"10.16.0.5"},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pod := mkTProxyPod("default", "pod", c.annotations, c.podIPs...)
			provider, found := getPodPrimaryNetworkProvider(pod)
			require.Equal(t, c.found, found)
			require.Equal(t, c.expected, provider)
		})
	}
}

func TestGetTProxyConditionPod(t *testing.T) {
	attachProvider := "macvlan.default.ovn"
	subnets := []*kubeovnv1.Subnet{{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-subnet"},
		Spec:       kubeovnv1.SubnetSpec{Vpc: "custom-vpc"},
	}, {
		ObjectMeta: metav1.ObjectMeta{Name: "default-subnet"},
		Spec:       kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc},
	}, {
		ObjectMeta: metav1.ObjectMeta{Name: "attach-subnet"},
		Spec:       kubeovnv1.SubnetSpec{Vpc: "custom-vpc"},
	}}

	kubeovnClient := kubeovnfake.NewSimpleClientset()
	informerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnClient, 0)
	subnetInformer := informerFactory.Kubeovn().V1().Subnets()
	for _, subnet := range subnets {
		require.NoError(t, subnetInformer.Informer().GetStore().Add(subnet))
	}

	c := &Controller{
		subnetsLister: subnetInformer.Lister(),
		config:        &Configuration{ClusterRouter: util.DefaultVpc},
	}

	primaryPod := mkTProxyPod("default", "primary", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.16.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "custom-subnet",
	}, "10.16.0.5")
	defaultVpcPod := mkTProxyPod("default", "default-vpc", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.17.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "default-subnet",
	}, "10.17.0.5")
	attachPod := mkTProxyPod("default", "attach", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider):     "172.17.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, attachProvider): "attach-subnet",
	}, "172.17.0.5")
	// kube-ovn allocated an unused address for the default provider while another
	// CNI provides the primary network; tproxy must not intercept its probes
	secondaryPod := mkTProxyPod("default", "secondary", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.16.0.6",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "custom-subnet",
	}, "192.168.0.6")
	staleSubnetPod := mkTProxyPod("default", "stale", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.18.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "missing-subnet",
	}, "10.18.0.5")

	pods := []*corev1.Pod{secondaryPod, primaryPod, defaultVpcPod, attachPod, staleSubnetPod}
	filtered, err := c.getTProxyConditionPod(pods, true)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	require.Equal(t, "attach", filtered[0].Name)
	require.Equal(t, "primary", filtered[1].Name)
}
