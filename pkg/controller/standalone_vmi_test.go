package controller

import (
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHandleDeletePodStandaloneVMINetworkLifetime(t *testing.T) {
	for _, tt := range []struct {
		name        string
		vmiState    string
		sourceAlive bool
		wantKeep    bool
	}{
		{name: "failed migration target", vmiState: "running", sourceAlive: true, wantKeep: true},
		{name: "VMI API object gone before source pod", vmiState: "missing", sourceAlive: true, wantKeep: true},
		{name: "VMI deleting before source pod", vmiState: "deleting", sourceAlive: true, wantKeep: true},
		{name: "running VMI without sibling", vmiState: "running", wantKeep: true},
		{name: "last pod of deleted VMI", vmiState: "missing"},
		{name: "last pod of deleting VMI", vmiState: "deleting"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testHandleDeletePodStandaloneVMI(t, tt.vmiState, tt.sourceAlive, tt.wantKeep)
		})
	}
}

func testHandleDeletePodStandaloneVMI(t *testing.T, vmiState string, sourceAlive, wantKeep bool) {
	const vmName = "vm1"
	target, subnet := podEventFixture()
	target.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	now := metav1.Now()
	target.DeletionTimestamp = &now
	target.Spec.NodeName = "target-node"
	target.Spec.RestartPolicy = corev1.RestartPolicyNever
	target.Status.Phase = corev1.PodFailed
	target.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance,
		Name: vmName, UID: "vmi-uid",
	}}
	source := target.DeepCopy()
	source.Name = "virt-launcher-vm1-source"
	source.UID = "source-uid"
	source.DeletionTimestamp = nil
	source.Spec.NodeName = "source-node"
	source.Status.Phase = corev1.PodRunning
	portName := ovs.PodNameToPortName(vmName, target.Namespace, util.OvnProvider)
	ipCR := &kubeovnv1.IP{Name: portName, Spec: kubeovnv1.IPSpec{
		PodName: vmName, Namespace: target.Namespace, Subnet: subnet.Name, PodType: util.KindVirtualMachine,
	}}
	pods := []*corev1.Pod{target}
	if sourceAlive {
		pods = append(pods, source)
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: pods, Subnets: []*kubeovnv1.Subnet{subnet}, IPs: []*kubeovnv1.IP{ipCR},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableKeepVMIP = true
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	_, _, _, err = c.ipam.GetStaticAddress("default/"+vmName, portName, "10.0.0.2", nil, subnet.Name, false)
	require.NoError(t, err)

	vmi := newTestVMI("source-node", "")
	vmi.UID = "vmi-uid"
	vmi.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnet.Name}
	if vmiState == "deleting" {
		vmi.DeletionTimestamp = &now
	}
	mockCtrl := gomock.NewController(t)
	mockVMIs := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
	mockVMs := kubecli.NewMockVirtualMachineInterface(mockCtrl)
	mockKubevirt := kubecli.NewMockKubevirtClient(mockCtrl)
	mockKubevirt.EXPECT().VirtualMachineInstance(target.Namespace).Return(mockVMIs).AnyTimes()
	mockKubevirt.EXPECT().VirtualMachine(target.Namespace).Return(mockVMs).AnyTimes()
	if vmiState == "missing" {
		mockVMIs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(nil,
			k8serrors.NewNotFound(schema.GroupResource{Resource: "virtualmachineinstances"}, vmName)).AnyTimes()
	} else {
		mockVMIs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(vmi, nil).AnyTimes()
	}
	mockVMs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(nil,
		k8serrors.NewNotFound(schema.GroupResource{Resource: "virtualmachines"}, vmName)).AnyTimes()
	c.config.KubevirtClient = mockKubevirt

	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": "default/" + vmName}).
		Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
	fc.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions(portName).Return(nil).AnyTimes()
	fc.mockOvnClient.EXPECT().RemovePortFromPortGroups(portName).Return(nil).AnyTimes()
	deleteCalls := 0
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).DoAndReturn(func(string) error {
		deleteCalls++
		return nil
	}).AnyTimes()
	c.deletingPodObjMap = xsync.NewMap[string, *corev1.Pod]()
	c.deletingPodObjMap.Store("default/"+target.Name, target)

	require.NoError(t, c.handleDeletePod("default/"+target.Name))
	_, err = c.config.KubeOvnClient.KubeovnV1().IPs().Get(t.Context(), portName, metav1.GetOptions{})
	if wantKeep {
		require.Zero(t, deleteCalls, "the VMI still uses the shared LSP")
		require.NoError(t, err, "the VMI still owns the IP CR")
		require.NotEmpty(t, c.ipam.GetPodAddress("default/"+vmName))
	} else {
		require.Equal(t, 1, deleteCalls)
		require.True(t, k8serrors.IsNotFound(err))
		require.Empty(t, c.ipam.GetPodAddress("default/"+vmName))
	}
}

func TestAppendCheckPodNetToDelUsesStandaloneVMIAnnotations(t *testing.T) {
	const vmName = "vm1"
	for _, tt := range []struct {
		name            string
		vmiSubnet       string
		expectIPCleanup bool
	}{
		{name: "same subnet", vmiSubnet: "subnet-a"},
		{name: "changed subnet", vmiSubnet: "subnet-b", expectIPCleanup: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod, subnet := podEventFixture()
			pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
			require.NoError(t, err)
			c := fc.fakeController

			mockCtrl := gomock.NewController(t)
			mockVMIs := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
			mockVMs := kubecli.NewMockVirtualMachineInterface(mockCtrl)
			mockKubevirt := kubecli.NewMockKubevirtClient(mockCtrl)
			mockKubevirt.EXPECT().VirtualMachine(pod.Namespace).Return(mockVMs)
			mockKubevirt.EXPECT().VirtualMachineInstance(pod.Namespace).Return(mockVMIs)
			mockVMs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(nil,
				k8serrors.NewNotFound(schema.GroupResource{Resource: "virtualmachines"}, vmName))
			vmi := newTestVMI("source-node", "")
			vmi.Annotations = map[string]string{util.LogicalSwitchAnnotation: tt.vmiSubnet}
			mockVMIs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(vmi, nil)
			c.config.KubevirtClient = mockKubevirt

			ownerDeleted, ipcrToDelete, err := appendCheckPodNetToDel(c, pod, vmName, util.KindVirtualMachineInstance)
			require.NoError(t, err)
			require.False(t, ownerDeleted)
			if tt.expectIPCleanup {
				require.Equal(t, []string{ovs.PodNameToPortName(vmName, pod.Namespace, util.OvnProvider)}, ipcrToDelete)
			} else {
				require.Empty(t, ipcrToDelete)
			}
		})
	}
}

func TestCheckIPOwnerExistsWithStandaloneVMI(t *testing.T) {
	const vmName = "vm1"
	for _, tt := range []struct {
		name        string
		vmiState    string
		sourceAlive bool
		ownerAlive  bool
	}{
		{name: "running VMI", vmiState: "running", ownerAlive: true},
		{name: "deleted VMI with live launcher", vmiState: "missing", sourceAlive: true, ownerAlive: true},
		{name: "deleting VMI with live launcher", vmiState: "deleting", sourceAlive: true, ownerAlive: true},
		{name: "deleted VMI without launcher", vmiState: "missing"},
		{name: "deleting VMI without launcher", vmiState: "deleting"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, subnet := podEventFixture()
			var pods []*corev1.Pod
			if tt.sourceAlive {
				pods = []*corev1.Pod{{
					Name: "virt-launcher-vm1-source", Namespace: metav1.NamespaceDefault,
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance,
						Name: vmName, UID: "vmi-uid",
					}},
				}}
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{subnet}, Pods: pods,
			})
			require.NoError(t, err)
			c := fc.fakeController
			require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

			mockCtrl := gomock.NewController(t)
			mockVMIs := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
			mockVMs := kubecli.NewMockVirtualMachineInterface(mockCtrl)
			mockKubevirt := kubecli.NewMockKubevirtClient(mockCtrl)
			mockKubevirt.EXPECT().VirtualMachine(metav1.NamespaceDefault).Return(mockVMs)
			mockKubevirt.EXPECT().VirtualMachineInstance(metav1.NamespaceDefault).Return(mockVMIs).AnyTimes()
			mockVMs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(nil,
				k8serrors.NewNotFound(schema.GroupResource{Resource: "virtualmachines"}, vmName))
			if tt.vmiState == "missing" {
				mockVMIs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(nil,
					k8serrors.NewNotFound(schema.GroupResource{Resource: "virtualmachineinstances"}, vmName))
			} else {
				vmi := newTestVMI("source-node", "")
				if tt.vmiState == "deleting" {
					now := metav1.Now()
					vmi.DeletionTimestamp = &now
				}
				mockVMIs.EXPECT().Get(gomock.Any(), vmName, gomock.Any()).Return(vmi, nil)
			}
			c.config.KubevirtClient = mockKubevirt

			ip := &kubeovnv1.IP{Spec: kubeovnv1.IPSpec{
				PodName: vmName, Namespace: metav1.NamespaceDefault, Subnet: subnet.Name,
				PodType: util.KindVirtualMachine,
			}}
			ownerAlive, err := c.checkIPOwnerExists(ip)
			require.NoError(t, err)
			require.Equal(t, tt.ownerAlive, ownerAlive)
		})
	}
}
