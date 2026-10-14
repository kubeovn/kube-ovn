package controller

import (
	"cmp"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	kubevirtv1 "kubevirt.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVMMigrationPreservesDefaultSubnet(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "virt-launcher-debian-test-6-target", Namespace: "sandbox1",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "debian-test-6",
			}},
			Labels:      map[string]string{kubevirtv1.MigrationJobLabel: "migration-uid"},
			Annotations: map[string]string{kubevirtv1.MigrationJobNameAnnotation: "migration"},
		},
	}
	testPodPreservesDefaultSubnet(t, pod, "debian-test-6", true)
}

func TestStatefulSetRecreationPreservesDefaultSubnet(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-0", Namespace: "sandbox1",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: appsv1.SchemeGroupVersion.String(), Kind: util.KindStatefulSet, Name: "web",
			}},
		},
	}
	for _, keepVMIP := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep VM IP %t", keepVMIP), func(t *testing.T) {
			testPodPreservesDefaultSubnet(t, pod, pod.Name, keepVMIP)
		})
	}
}

func testPodPreservesDefaultSubnet(t *testing.T, pod *corev1.Pod, allocationName string, keepVMIP bool) {
	t.Helper()
	for _, tt := range []struct {
		name             string
		originalSubnet   int
		availableInFirst float64
		availableInLast  float64
	}{
		{name: "original subnet exhausted", originalSubnet: 0, availableInLast: 4},
		{name: "earlier subnet has free addresses", originalSubnet: 1, availableInFirst: 4, availableInLast: 3},
		{name: "all namespace subnets exhausted", originalSubnet: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			subnets := []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "sandbox1-net1"},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "29.29.0.0/29", Protocol: kubeovnv1.ProtocolIPv4,
						Provider: util.OvnProvider, ExcludeIps: []string{"29.29.0.1"},
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: tt.availableInFirst},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "sandbox1-net2"},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "29.29.0.8/29", Protocol: kubeovnv1.ProtocolIPv4,
						Provider: util.OvnProvider, ExcludeIps: []string{"29.29.0.9"},
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: tt.availableInLast},
				},
			}

			originalIP := []string{"29.29.0.3", "29.29.0.10"}[tt.originalSubnet]
			originalSubnet := subnets[tt.originalSubnet].Name
			portName := ovs.PodNameToPortName(allocationName, pod.Namespace, util.OvnProvider)
			podType := util.KindStatefulSet
			if pod.OwnerReferences[0].Kind == util.KindVirtualMachineInstance {
				podType = util.KindVirtualMachine
			}
			ip := &kubeovnv1.IP{
				ObjectMeta: metav1.ObjectMeta{Name: portName},
				Spec: kubeovnv1.IPSpec{
					PodName: allocationName, Namespace: pod.Namespace, PodType: podType,
					Subnet: originalSubnet, IPAddress: originalIP, V4IPAddress: originalIP, MacAddress: "02:00:00:00:00:01",
				},
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: subnets, IPs: []*kubeovnv1.IP{ip}, Pods: []*corev1.Pod{pod},
				Namespaces: []*corev1.Namespace{{
					ObjectMeta: metav1.ObjectMeta{Name: pod.Namespace, Annotations: map[string]string{util.LogicalSwitchAnnotation: "sandbox1-net1,sandbox1-net2"}},
				}},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.config.EnableKeepVMIP = keepVMIP
			ctrl.ipam = newIPAMForTest(subnets)
			_, _, _, err = ctrl.ipam.GetStaticAddress(pod.Namespace+"/"+allocationName, portName, originalIP, &ip.Spec.MacAddress, originalSubnet, true)
			require.NoError(t, err)
			for _, subnet := range subnets {
				if subnet.Status.V4AvailableIPs == 0 {
					exhaustKeepIPTestSubnet(t, ctrl, subnet.Name)
				}
			}

			nets, err := ctrl.getPodKubeovnNets(pod)
			require.NoError(t, err)
			require.Len(t, nets, 1)
			v4, v6, mac, subnet, err := ctrl.acquireAddress(pod, nets[0])
			require.NoError(t, err)
			require.Equal(t, originalSubnet, subnet.Name, "a recreated pod must retain its subnet")
			require.Equal(t, originalIP, v4, "a recreated pod must retain its IP")
			require.Empty(t, v6)
			require.Equal(t, ip.Spec.MacAddress, mac)
			require.Len(t, ctrl.ipam.GetPodAddress(pod.Namespace+"/"+allocationName), 1, "a recreated pod must not allocate in a second subnet")
		})
	}
}

func exhaustKeepIPTestSubnet(t *testing.T, ctrl *Controller, subnetName string) {
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

func TestGetPodDefaultSubnetRetainedIPSelection(t *testing.T) {
	for _, workload := range []string{util.KindVirtualMachine, util.KindStatefulSet} {
		t.Run(workload, func(t *testing.T) {
			testGetPodDefaultSubnetRetainedIPSelection(t, workload)
		})
	}
}

func testGetPodDefaultSubnetRetainedIPSelection(t *testing.T, workload string) {
	t.Helper()
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
		{name: "reuse retained subnet", expected: "subnet2"},
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
				ObjectMeta: metav1.ObjectMeta{
					Name: "virt-launcher-vm", Namespace: "default", Annotations: map[string]string{},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "vm",
					}},
				},
			}
			allocationName := "vm"
			expected := tt.expected
			if workload == util.KindStatefulSet {
				pod.Name = "web-0"
				pod.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: appsv1.SchemeGroupVersion.String(), Kind: util.KindStatefulSet, Name: "web",
				}}
				allocationName = pod.Name
				if tt.disableKeepIP {
					expected = "subnet2"
				}
			}
			if tt.ordinaryPod {
				pod.OwnerReferences = nil
			}
			if tt.annotation != "" {
				pod.Annotations[tt.annotation] = tt.value
			}
			ipName := cmp.Or(tt.ipName, allocationName+".default")
			namespaceNets := cmp.Or(tt.namespaceNets, "subnet1,subnet2")
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{
					{ObjectMeta: metav1.ObjectMeta{Name: "subnet1"}, Spec: kubeovnv1.SubnetSpec{Protocol: kubeovnv1.ProtocolIPv4}, Status: kubeovnv1.SubnetStatus{V4AvailableIPs: 4}},
					{ObjectMeta: metav1.ObjectMeta{Name: "subnet2"}, Spec: kubeovnv1.SubnetSpec{Protocol: kubeovnv1.ProtocolIPv4}},
				},
				IPs: []*kubeovnv1.IP{{ObjectMeta: metav1.ObjectMeta{Name: ipName}, Spec: kubeovnv1.IPSpec{Subnet: "subnet2"}}},
				Namespaces: []*corev1.Namespace{{
					ObjectMeta: metav1.ObjectMeta{Name: pod.Namespace, Annotations: map[string]string{util.LogicalSwitchAnnotation: namespaceNets}},
				}},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.config.EnableKeepVMIP = !tt.disableKeepIP
			poolIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, poolIndexer.Add(&kubeovnv1.IPPool{ObjectMeta: metav1.ObjectMeta{Name: "pool"}, Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet1"}}))
			ctrl.ippoolLister = kubeovnlister.NewIPPoolLister(poolIndexer)

			subnet, err := ctrl.getPodDefaultSubnet(pod)
			require.NoError(t, err)
			require.Equal(t, expected, subnet.Name)
		})
	}
}
