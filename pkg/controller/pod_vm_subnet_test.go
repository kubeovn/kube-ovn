package controller

import (
	"cmp"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVMMigrationPreservesDefaultSubnet(t *testing.T) {
	for _, tt := range []struct {
		name             string
		originalSubnet   int
		availableInFirst int64
		availableInLast  int64
	}{
		{name: "original subnet exhausted", originalSubnet: 0, availableInLast: 4},
		{name: "earlier subnet has free addresses", originalSubnet: 1, availableInFirst: 4, availableInLast: 3},
		{name: "all namespace subnets exhausted", originalSubnet: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			subnets := []*kubeovnv1.Subnet{
				{
					Name: "sandbox1-net1",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "29.29.0.0/29", Protocol: kubeovnv1.ProtocolIPv4,
						Provider: util.OvnProvider, ExcludeIps: []string{"29.29.0.1"},
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(tt.availableInFirst)},
				},
				{
					Name: "sandbox1-net2",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "29.29.0.8/29", Protocol: kubeovnv1.ProtocolIPv4,
						Provider: util.OvnProvider, ExcludeIps: []string{"29.29.0.9"},
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(tt.availableInLast)},
				},
			}
			pod := &corev1.Pod{
				Name: "virt-launcher-debian-test-6-target", Namespace: "sandbox1",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "debian-test-6",
				}},
				Labels:      map[string]string{kubevirtv1.MigrationJobLabel: "migration-uid"},
				Annotations: map[string]string{kubevirtv1.MigrationJobNameAnnotation: "migration"},
			}
			originalIP := []string{"29.29.0.3", "29.29.0.10"}[tt.originalSubnet]
			originalSubnet := subnets[tt.originalSubnet].Name
			portName := ovs.PodNameToPortName("debian-test-6", pod.Namespace, util.OvnProvider)
			ip := &kubeovnv1.IP{
				Name: portName,
				Spec: kubeovnv1.IPSpec{
					PodName: "debian-test-6", Namespace: pod.Namespace, PodType: util.KindVirtualMachine,
					Subnet: originalSubnet, IPAddress: originalIP, V4IPAddress: originalIP, MacAddress: "02:00:00:00:00:01",
				},
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: subnets, IPs: []*kubeovnv1.IP{ip}, Pods: []*corev1.Pod{pod},
				Namespaces: []*corev1.Namespace{{
					Name: pod.Namespace, Annotations: map[string]string{util.LogicalSwitchAnnotation: "sandbox1-net1,sandbox1-net2"},
				}},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.config.EnableKeepVMIP = true
			ctrl.ipam = newIPAMForTest(subnets)
			_, _, _, err = ctrl.ipam.GetStaticAddress("sandbox1/debian-test-6", portName, originalIP, &ip.Spec.MacAddress, originalSubnet, true)
			require.NoError(t, err)
			for _, subnet := range subnets {
				if subnet.Status.V4AvailableIPs.EqualInt64(0) {
					exhaustVMTestSubnet(t, ctrl, subnet.Name)
				}
			}

			nets, err := ctrl.getPodKubeovnNets(pod)
			require.NoError(t, err)
			require.Len(t, nets, 1)
			v4, v6, mac, subnet, err := ctrl.acquireAddress(pod, nets[0])
			require.NoError(t, err)
			require.Equal(t, originalSubnet, subnet.Name, "migration must retain the source subnet")
			require.Equal(t, originalIP, v4, "migration must retain the source IP")
			require.Empty(t, v6)
			require.Equal(t, ip.Spec.MacAddress, mac)
			require.Len(t, ctrl.ipam.GetPodAddress("sandbox1/debian-test-6"), 1, "migration must not allocate in a second subnet")
		})
	}
}

func exhaustVMTestSubnet(t *testing.T, ctrl *Controller, subnetName string) {
	t.Helper()
	for i := range 8 {
		name := fmt.Sprintf("filler-%s-%d", subnetName, i)
		_, _, _, err := ctrl.ipam.GetRandomAddress(name, name, nil, subnetName, "", nil, true)
		if err != nil {
			require.ErrorIs(t, err, ipam.ErrNoAvailable)
			return
		}
	}
	t.Fatal("expected the /29 subnet to be exhausted")
}

func TestGetPodDefaultSubnetVMSelection(t *testing.T) {
	for _, tt := range []struct {
		name          string
		annotation    string
		value         string
		ipName        string
		namespaceNets string
		disableKeepIP bool
		ordinaryPod   bool
		expected      string
	}{
		{name: "reuse VM subnet", expected: "subnet2"},
		{name: "explicit subnet", annotation: util.LogicalSwitchAnnotation, value: "subnet1", expected: "subnet1"},
		{name: "explicit named pool", annotation: util.IPPoolAnnotation, value: "pool", expected: "subnet1"},
		{name: "namespace subnet changed", namespaceNets: "subnet1", expected: "subnet1"},
		{name: "keep VM IP disabled", disableKeepIP: true, expected: "subnet1"},
		{name: "ordinary pod", ordinaryPod: true, expected: "subnet1"},
		{name: "no saved default IP", ipName: "unrelated.default", expected: "subnet1"},
		{name: "attachment IP is not default", ipName: "vm.default.net.ns.ovn", expected: "subnet1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Name: "virt-launcher-vm", Namespace: "default", Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "vm",
				}},
			}
			if tt.ordinaryPod {
				pod.OwnerReferences = nil
			}
			if tt.annotation != "" {
				pod.Annotations[tt.annotation] = tt.value
			}
			ipName := cmp.Or(tt.ipName, "vm.default")
			namespaceNets := cmp.Or(tt.namespaceNets, "subnet1,subnet2")
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{
					{Name: "subnet1", Spec: kubeovnv1.SubnetSpec{Protocol: kubeovnv1.ProtocolIPv4}, Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(4)}},
					{Name: "subnet2", Spec: kubeovnv1.SubnetSpec{Protocol: kubeovnv1.ProtocolIPv4}},
				},
				IPs:     []*kubeovnv1.IP{{Name: ipName, Spec: kubeovnv1.IPSpec{Subnet: "subnet2"}}},
				IPPools: []*kubeovnv1.IPPool{{Name: "pool", Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet1"}}},
				Namespaces: []*corev1.Namespace{{
					Name: pod.Namespace, Annotations: map[string]string{util.LogicalSwitchAnnotation: namespaceNets},
				}},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.config.EnableKeepVMIP = !tt.disableKeepIP

			subnet, err := ctrl.getPodDefaultSubnet(pod)
			require.NoError(t, err)
			require.Equal(t, tt.expected, subnet.Name)
		})
	}
}
