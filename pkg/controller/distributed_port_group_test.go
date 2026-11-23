package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestReconcileDistributedSubnetPortGroupsRetriesInterruptedWrite(t *testing.T) {
	t.Parallel()

	const subnet = "ovn-default"
	moving := distributedPortGroupTestPod("moving", subnet, "node-b")
	steady := distributedPortGroupTestPod("steady", subnet, "node-a")
	movingPort := distributedPortGroupTestLSP(moving.Name, subnet)
	steadyPort := distributedPortGroupTestLSP(steady.Name, subnet)
	ports := []ovnnb.LogicalSwitchPort{movingPort, steadyPort}

	source := distributedPortGroupTestGroup(subnet, "node-a", movingPort.UUID, steadyPort.UUID)
	target := distributedPortGroupTestGroup(subnet, "node-b")
	delete(target.ExternalIDs, "vendor") // Existing groups may lack a vendor ID.
	unrelated := ovnnb.PortGroup{Name: "ovn.sg.example", Ports: []string{movingPort.UUID}}
	beforeWrite := []ovnnb.PortGroup{source, target, unrelated}
	updatedSource, updatedTarget := source, target
	updatedSource.Ports = []string{steadyPort.UUID}
	updatedTarget.Ports = []string{movingPort.UUID}
	afterWrite := []ovnnb.PortGroup{updatedSource, updatedTarget, unrelated}
	changes := []ovs.PortGroupMembershipChange{
		{Group: source, Remove: []string{movingPort.UUID}},
		{Group: target, Add: []string{movingPort.UUID}},
	}

	tests := []struct {
		name         string
		committed    bool
		wantPodReads []string
	}{
		{name: "write not committed", wantPodReads: []string{moving.Name, moving.Name}},
		{name: "write committed before disconnect", committed: true, wantPodReads: []string{moving.Name}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{moving, steady}})
			require.NoError(t, err)
			ctrl := fixture.fakeController
			nb := fixture.mockOvnClient.EXPECT()
			ctx := t.Context()
			disconnected := errors.New("NB connection closed")

			retrySnapshot := afterWrite
			if !tt.committed {
				retrySnapshot = beforeWrite
			}
			calls := []any{
				nb.GetSubnetPortGroupMembership(ctx, subnet).Return(beforeWrite, ports, nil),
				nb.ApplyPortGroupMembershipChanges(ctx, changes, []string{movingPort.UUID}).Return(disconnected),
				nb.GetSubnetPortGroupMembership(ctx, subnet).Return(retrySnapshot, ports, nil),
			}
			if !tt.committed {
				calls = append(calls, nb.ApplyPortGroupMembershipChanges(ctx, changes, []string{movingPort.UUID}).Return(nil))
			}
			calls = append(calls, nb.GetSubnetPortGroupMembership(ctx, subnet).Return(afterWrite, ports, nil))
			gomock.InOrder(calls...)

			require.ErrorIs(t, ctrl.reconcileDistributedSubnetPortGroups(ctx, subnet), disconnected)
			require.NoError(t, ctrl.reconcileDistributedSubnetPortGroups(ctx, subnet))
			require.NoError(t, ctrl.reconcileDistributedSubnetPortGroups(ctx, subnet))

			kube := ctrl.config.KubeClient.(*fake.Clientset)
			var podReads []string
			for _, action := range kube.Actions() {
				if action.Matches("get", "pods") {
					podReads = append(podReads, action.(clienttesting.GetAction).GetName())
				}
			}
			require.Equal(t, tt.wantPodReads, podReads)
		})
	}
}

func TestSyncDistributedSubnetPortGroupsAfterLeadershipChange(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "ovn-default",
		Spec: kubeovnv1.SubnetSpec{
			Vpc:         "ovn-cluster",
			GatewayType: kubeovnv1.GWDistributedType,
		},
	}
	pod := distributedPortGroupTestPod("moving", subnet.Name, "node-b")
	port := distributedPortGroupTestLSP(pod.Name, subnet.Name)
	source := distributedPortGroupTestGroup(subnet.Name, "node-a", port.UUID)
	target := distributedPortGroupTestGroup(subnet.Name, "node-b")
	groups := []ovnnb.PortGroup{source, target}
	ports := []ovnnb.LogicalSwitchPort{port}
	opts := &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}, Pods: []*corev1.Pod{pod}}

	old, err := newFakeControllerWithOptions(t, opts)
	require.NoError(t, err)
	t.Cleanup(old.fakeController.addOrUpdateSubnetQueue.ShutDown)
	lostLeaderCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	nbUnavailable := errors.New("NB connection unavailable")
	oldNB := old.mockOvnClient.EXPECT()
	gomock.InOrder(
		oldNB.GetSubnetPortGroupMembership(lostLeaderCtx, subnet.Name).Return(nil, nil, nbUnavailable),
		oldNB.GetSubnetPortGroupMembership(lostLeaderCtx, subnet.Name).
			DoAndReturn(func(context.Context, string) ([]ovnnb.PortGroup, []ovnnb.LogicalSwitchPort, error) {
				cancel()
				return groups, ports, nil
			}),
	)
	old.fakeController.distributedSubnetNeedSync.Store(true)
	old.fakeController.syncDistributedSubnetRoutesWithContext(lostLeaderCtx)
	require.Equal(t, 1, old.fakeController.addOrUpdateSubnetQueue.NumRequeues(subnet.Name))
	require.ErrorIs(t, old.fakeController.syncDistributedSubnetRoute(lostLeaderCtx, subnet.Name), context.Canceled)

	next, err := newFakeControllerWithOptions(t, opts)
	require.NoError(t, err)
	t.Cleanup(next.fakeController.addOrUpdateSubnetQueue.ShutDown)
	nextNB := next.mockOvnClient.EXPECT()
	nextCtx := t.Context()
	gomock.InOrder(
		nextNB.GetSubnetPortGroupMembership(nextCtx, subnet.Name).Return(groups, ports, nil),
		nextNB.ApplyPortGroupMembershipChanges(nextCtx, []ovs.PortGroupMembershipChange{
			{Group: source, Remove: []string{port.UUID}},
			{Group: target, Add: []string{port.UUID}},
		}, []string{port.UUID}).Return(nil),
	)
	require.NoError(t, next.fakeController.syncDistributedSubnetRoute(nextCtx, subnet.Name))
}

func TestReconcileDistributedSubnetPortGroupsChecksRetainedPortOwner(t *testing.T) {
	t.Parallel()

	const subnet = "ovn-default"
	pod := distributedPortGroupTestPod("retained-0", subnet, "node-b")
	port := distributedPortGroupTestLSP(pod.Name, subnet)
	ordinary := distributedPortGroupTestLSP("ordinary", subnet)
	group := distributedPortGroupTestGroup(subnet, "node-a", ordinary.UUID, port.UUID)
	ip := &kubeovnv1.IP{
		Name: port.Name,
		Spec: kubeovnv1.IPSpec{
			PodName:   pod.Name,
			Namespace: pod.Namespace,
			Subnet:    subnet,
			PodType:   util.KindStatefulSet,
		},
	}

	t.Run("missed deletion removes membership and preserves retained IP", func(t *testing.T) {
		t.Parallel()

		fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPs: []*kubeovnv1.IP{ip}})
		require.NoError(t, err)
		ctrl := fixture.fakeController
		nb := fixture.mockOvnClient.EXPECT()
		ctx := t.Context()

		gomock.InOrder(
			nb.GetSubnetPortGroupMembership(ctx, subnet).Return([]ovnnb.PortGroup{group}, []ovnnb.LogicalSwitchPort{ordinary, port}, nil),
			nb.ApplyPortGroupMembershipChanges(ctx, []ovs.PortGroupMembershipChange{
				{Group: group, Remove: []string{port.UUID}},
			}, []string{port.UUID}).Return(nil),
		)

		require.NoError(t, ctrl.reconcileDistributedSubnetPortGroups(ctx, subnet))
		retained, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(ctx, port.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, ip.Spec, retained.Spec)
	})

	t.Run("pod recreated between checks keeps membership", func(t *testing.T) {
		t.Parallel()

		fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			IPs:  []*kubeovnv1.IP{ip},
			Pods: []*corev1.Pod{pod},
		})
		require.NoError(t, err)
		ctrl := fixture.fakeController
		require.NoError(t, fixture.fakeInformers.podInformer.Informer().GetStore().Delete(pod))
		kube := ctrl.config.KubeClient.(*fake.Clientset)
		var checked bool
		kube.PrependReactor("get", "pods", func(_ clienttesting.Action) (bool, runtime.Object, error) {
			if checked {
				return false, nil, nil
			}
			checked = true
			return true, nil, k8serrors.NewNotFound(corev1.Resource("pods"), pod.Name)
		})
		ctx := t.Context()

		fixture.mockOvnClient.EXPECT().GetSubnetPortGroupMembership(ctx, subnet).
			Return([]ovnnb.PortGroup{group}, []ovnnb.LogicalSwitchPort{ordinary, port}, nil)

		require.ErrorContains(t, ctrl.reconcileDistributedSubnetPortGroups(ctx, subnet), "changed during reconciliation")
	})
}

func TestReconcileDistributedSubnetPortGroupsChecksRetainedVM(t *testing.T) {
	t.Parallel()

	const subnet, vmName = "ovn-default", "vm"
	launcher := distributedPortGroupTestPod("virt-launcher-vm", subnet, "node-b")
	launcher.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: kubevirtv1.SchemeGroupVersion.String(),
		Kind:       util.KindVirtualMachineInstance,
		Name:       vmName,
	}}
	port := distributedPortGroupTestLSP(vmName, subnet)
	group := distributedPortGroupTestGroup(subnet, "node-a", port.UUID)
	ip := &kubeovnv1.IP{
		Name: port.Name,
		Spec: kubeovnv1.IPSpec{
			PodName: vmName, Namespace: launcher.Namespace, Subnet: subnet, PodType: util.KindVirtualMachine,
		},
	}

	for _, tt := range []struct {
		name        string
		phase       kubevirtv1.VirtualMachineInstancePhase
		hasLauncher bool
		wantRemoval bool
	}{
		{name: "final VMI without launcher", phase: kubevirtv1.Succeeded, wantRemoval: true},
		{name: "running VMI", phase: kubevirtv1.Running},
		{name: "live launcher absent from informer", phase: kubevirtv1.Succeeded, hasLauncher: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := &FakeControllerOptions{IPs: []*kubeovnv1.IP{ip}}
			if tt.hasLauncher {
				opts.Pods = []*corev1.Pod{launcher}
			}
			fixture, err := newFakeControllerWithOptions(t, opts)
			require.NoError(t, err)
			if tt.hasLauncher {
				require.NoError(t, fixture.fakeInformers.podInformer.Informer().GetStore().Delete(launcher))
			}

			mockCtrl := gomock.NewController(t)
			virtClient := kubecli.NewMockKubevirtClient(mockCtrl)
			vmis := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
			ctrl := fixture.fakeController
			ctrl.config.KubevirtClient = virtClient
			ctrl.config.EnableKeepVMIP = true

			ctx := t.Context()
			checks := 1
			if tt.wantRemoval {
				checks = 2
			}
			virtClient.EXPECT().VirtualMachineInstance(launcher.Namespace).Return(vmis).Times(checks)
			vmis.EXPECT().Get(gomock.Any(), vmName, metav1.GetOptions{}).Return(&kubevirtv1.VirtualMachineInstance{
				Status: kubevirtv1.VirtualMachineInstanceStatus{Phase: tt.phase},
			}, nil).Times(checks)

			nb := fixture.mockOvnClient.EXPECT()
			nb.GetSubnetPortGroupMembership(ctx, subnet).Return([]ovnnb.PortGroup{group}, []ovnnb.LogicalSwitchPort{port}, nil)
			if tt.wantRemoval {
				nb.ApplyPortGroupMembershipChanges(ctx, []ovs.PortGroupMembershipChange{
					{Group: group, Remove: []string{port.UUID}},
				}, []string{port.UUID}).Return(nil)
			}
			require.NoError(t, ctrl.reconcileDistributedSubnetPortGroups(ctx, subnet))
		})
	}
}

func distributedPortGroupTestPod(name, subnet, node string) *corev1.Pod {
	return &corev1.Pod{
		Name:      name,
		Namespace: "default",
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: subnet,
			util.IPAddressAnnotation:     "10.0.0.2",
		},
		Spec:   corev1.PodSpec{NodeName: node},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func distributedPortGroupTestLSP(podName, subnet string) ovnnb.LogicalSwitchPort {
	return ovnnb.LogicalSwitchPort{
		UUID: "lsp-" + podName,
		Name: ovs.PodNameToPortName(podName, "default", util.OvnProvider),
		ExternalIDs: map[string]string{
			"pod":                "default/" + podName,
			ovs.LogicalSwitchKey: subnet,
		},
	}
}

func distributedPortGroupTestGroup(subnet, node string, ports ...string) ovnnb.PortGroup {
	return ovnnb.PortGroup{
		UUID:  "pg-" + node,
		Name:  getOverlaySubnetsPortGroupName(subnet, node),
		Ports: ports,
		ExternalIDs: map[string]string{
			"subnet":         subnet,
			"node":           node,
			"vendor":         util.CniTypeName,
			networkPolicyKey: subnet + "/" + node,
		},
	}
}

func TestPeriodicDistributedPortGroupsRepairsRoutedPod(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "ovn-default",
		Spec: kubeovnv1.SubnetSpec{Vpc: "ovn-cluster", GatewayType: kubeovnv1.GWDistributedType},
	}
	pod := distributedPortGroupTestPod("moving", subnet.Name, "node-c")
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	port := distributedPortGroupTestLSP(pod.Name, subnet.Name)
	target := distributedPortGroupTestGroup(subnet.Name, "node-c", port.UUID)
	missing := target
	missing.Ports = nil
	fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet}, Pods: []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	ctrl := fixture.fakeController
	t.Cleanup(ctrl.addOrUpdateSubnetQueue.ShutDown)
	ctx := t.Context()
	nb := fixture.mockOvnClient.EXPECT()
	gomock.InOrder(
		nb.GetSubnetPortGroupMembership(ctx, subnet.Name).Return([]ovnnb.PortGroup{target}, []ovnnb.LogicalSwitchPort{port}, nil),
		// Drift occurs after a successful reconcile without any Pod/node event.
		nb.GetSubnetPortGroupMembership(ctx, subnet.Name).Return([]ovnnb.PortGroup{missing}, []ovnnb.LogicalSwitchPort{port}, nil),
		nb.ApplyPortGroupMembershipChanges(ctx, []ovs.PortGroupMembershipChange{
			{Group: missing, Add: []string{port.UUID}},
		}, []string{port.UUID}).Return(nil),
		nb.GetSubnetPortGroupMembership(ctx, subnet.Name).Return([]ovnnb.PortGroup{target}, []ovnnb.LogicalSwitchPort{port}, nil),
	)
	for range 3 {
		require.False(t, ctrl.distributedSubnetNeedSync.Load())
		ctrl.syncDistributedSubnetPortGroups(ctx)
	}
	require.Zero(t, ctrl.addOrUpdateSubnetQueue.NumRequeues(subnet.Name))
}

func TestVMMigrationPortGroupsConverge(t *testing.T) {
	for _, tc := range []struct {
		name    string
		success bool
	}{
		{name: "success", success: true},
		{name: "rollback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const vm = "vm"
			subnet := &kubeovnv1.Subnet{
				Name: "ovn-default",
				Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, GatewayType: kubeovnv1.GWDistributedType},
			}
			nodeA := &corev1.Node{
				Name: "node-a", Annotations: map[string]string{
					util.PortNameAnnotation: "node-node-a", util.IPAddressAnnotation: "100.64.0.2",
				},
			}
			nodeC := nodeA.DeepCopy()
			nodeC.Name = "node-c"
			nodeC.Annotations[util.PortNameAnnotation] = "node-node-c"
			nodeC.Annotations[util.IPAddressAnnotation] = "100.64.0.3"
			source := distributedPortGroupTestPod("source-launcher", subnet.Name, nodeA.Name)
			source.UID = "source-uid"
			source.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: vm, UID: "vmi-uid",
			}}
			source.Annotations[util.AllocatedAnnotation] = "true"
			source.Annotations[util.RoutedAnnotation] = "true"
			target := source.DeepCopy()
			target.Name, target.UID, target.Spec.NodeName = "target-launcher", "target-uid", nodeC.Name

			fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{subnet}, Pods: []*corev1.Pod{source, target}, Nodes: []*corev1.Node{nodeA, nodeC},
			})
			require.NoError(t, err)
			ctrl := fixture.fakeController
			ctrl.config.EnableKeepVMIP = true
			ctrl.config.EnableNP = false
			t.Cleanup(ctrl.addOrUpdateSubnetQueue.ShutDown)
			nb := newInMemoryOVNNbClient(t)
			ctrl.OVNNbClient = nb

			ctx := t.Context()
			port := distributedPortGroupTestLSP(vm, subnet.Name)
			require.NoError(t, nb.CreateBareLogicalSwitch(subnet.Name))
			require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet.Name, port.Name, "10.0.0.2", "00:00:00:00:00:02"))
			lsp, err := nb.GetLogicalSwitchPort(port.Name, false)
			require.NoError(t, err)
			lsp.ExternalIDs = port.ExternalIDs
			require.NoError(t, nb.UpdateLogicalSwitchPort(lsp, &lsp.ExternalIDs))

			for _, node := range []*corev1.Node{nodeA, nodeC} {
				group := distributedPortGroupTestGroup(subnet.Name, node.Name)
				require.NoError(t, nb.CreatePortGroup(group.Name, group.ExternalIDs))
				require.NoError(t, nb.CreatePortGroup("node.node."+node.Name[len("node-"):],
					map[string]string{"node": node.Name, networkPolicyKey: "node/" + node.Name}))
			}
			move := func(pod *corev1.Pod, nodeGroup string) {
				t.Helper()
				require.NoError(t, ctrl.movePodPortToPortGroup(pod, subnet.Name, nodeGroup, port.Name,
					map[string]string{"node": "", networkPolicyKey: ""}, "subnet"))
				require.NoError(t, ctrl.movePodPortToPortGroup(pod, subnet.Name,
					getOverlaySubnetsPortGroupName(subnet.Name, pod.Spec.NodeName), port.Name,
					map[string]string{"subnet": subnet.Name, "node": "", networkPolicyKey: ""}))
			}
			move(source, "node.node.a")
			move(target, "node.node.c")

			winner, obsolete := source, target
			winnerPG, obsoletePG := "node.node.a", "node.node.c"
			if tc.success {
				// A delayed route task can run after the destination has routed.
				move(source, "node.node.a")
				winner, obsolete = target, source
				winnerPG, obsoletePG = obsoletePG, winnerPG
			}
			assertGroups := func(pod *corev1.Pod, nodeGroup string, members ...string) {
				t.Helper()
				for _, name := range []string{nodeGroup, getOverlaySubnetsPortGroupName(subnet.Name, pod.Spec.NodeName)} {
					pg, err := nb.GetPortGroup(name, false)
					require.NoError(t, err)
					require.ElementsMatch(t, members, pg.Ports, name)
				}
			}
			// Preserve the existing subnet membership while both launchers
			// are alive, even though it is now on the obsolete side.
			ctrl.syncDistributedSubnetPortGroups(ctx)
			assertGroups(obsolete, obsoletePG, lsp.UUID)
			assertGroups(winner, winnerPG)

			// The periodic checks alone must restore both kinds of membership.
			require.NoError(t, fixture.fakeInformers.podInformer.Informer().GetStore().Delete(obsolete))
			require.NoError(t, ctrl.config.KubeClient.CoreV1().Pods(obsolete.Namespace).Delete(ctx, obsolete.Name, metav1.DeleteOptions{}))
			require.NoError(t, ctrl.checkAndUpdateNodePortGroup())
			ctrl.syncDistributedSubnetPortGroups(ctx)
			assertGroups(winner, winnerPG, lsp.UUID)
			assertGroups(obsolete, obsoletePG)
			require.Zero(t, ctrl.addOrUpdateSubnetQueue.NumRequeues(subnet.Name))
		})
	}
}

func TestPeriodicDistributedPortGroupsQueuesMissingTarget(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "ovn-default",
		Spec: kubeovnv1.SubnetSpec{Vpc: "ovn-cluster", GatewayType: kubeovnv1.GWDistributedType},
	}
	pod := distributedPortGroupTestPod("moving", subnet.Name, "node-c")
	port := distributedPortGroupTestLSP(pod.Name, subnet.Name)
	group := distributedPortGroupTestGroup(subnet.Name, "node-a", port.UUID)
	fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet}, Pods: []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	t.Cleanup(fixture.fakeController.addOrUpdateSubnetQueue.ShutDown)
	fixture.mockOvnClient.EXPECT().GetSubnetPortGroupMembership(t.Context(), subnet.Name).
		Return([]ovnnb.PortGroup{group}, []ovnnb.LogicalSwitchPort{port}, nil)
	fixture.fakeController.syncDistributedSubnetPortGroups(t.Context())
	require.Equal(t, 1, fixture.fakeController.addOrUpdateSubnetQueue.NumRequeues(subnet.Name))
}

func TestRemoveRetainedPodPortRechecksCurrentOwner(t *testing.T) {
	for _, vm := range []bool{false, true} {
		t.Run(fmt.Sprintf("vm=%t", vm), func(t *testing.T) {
			pod := distributedPortGroupTestPod("retained", "ovn-default", "node-a")
			pod.UID = "old-owner"
			if vm {
				pod.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "vm",
				}}
			}
			next := pod.DeepCopy()
			next.UID, next.Spec.NodeName = "new-owner", "node-c"
			if vm {
				next.Name = "destination-launcher"
			}
			fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{next}})
			require.NoError(t, err)
			ctrl := fixture.fakeController
			ctrl.config.EnableKeepVMIP = true
			// Simulate a delete decision based on a stale informer, before the
			// destination's assignment or repair acquires the membership lock.
			require.NoError(t, fixture.fakeInformers.podInformer.Informer().GetStore().Delete(next))
			port := distributedPortGroupTestLSP(ctrl.getNameByPod(pod), "ovn-default")
			require.NoError(t, ctrl.removeRetainedPodPort(pod, port))
			// No OVN write is expected while the successor still owns the LSP.
			require.NoError(t, ctrl.config.KubeClient.CoreV1().Pods(next.Namespace).Delete(t.Context(), next.Name, metav1.DeleteOptions{}))
			fixture.mockOvnClient.EXPECT().RemovePortFromPortGroups(port.Name).Return(nil)
			require.NoError(t, ctrl.removeRetainedPodPort(pod, port))
		})
	}
}

func TestDistributedPortGroupBatchBoundsConcurrentOwnerReads(t *testing.T) {
	const subnet = "ovn-default"
	pods := make(map[string]*corev1.Pod)
	owners := make(map[string]distributedPortOwner)
	batch := distributedPortGroupBatch{}
	for i := range 2 * distributedPortOwnerConcurrency {
		pod := distributedPortGroupTestPod(fmt.Sprintf("pod-%d", i), subnet, "node-c")
		pods[pod.Name] = pod
		uuid := "lsp-" + pod.Name
		batch.lspUUIDs = append(batch.lspUUIDs, uuid)
		owners[uuid] = distributedPortOwner{pod: pod, provider: util.OvnProvider}
	}
	var active, peak atomic.Int32
	started := make(chan struct{}, len(pods))
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pods[path.Base(r.URL.Path)]); err != nil {
			t.Errorf("encode Pod response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	fixture := newFakeController(t)
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, QPS: 1000, Burst: 1000})
	require.NoError(t, err)
	fixture.fakeController.config.KubeClient = kube
	fixture.mockOvnClient.EXPECT().ApplyPortGroupMembershipChanges(t.Context(), batch.changes, batch.lspUUIDs).Return(nil)
	done := make(chan error, 1)
	go func() {
		done <- fixture.fakeController.applyDistributedPortGroupBatch(t.Context(), subnet, batch, owners)
	}()
	for range distributedPortOwnerConcurrency {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("owner reads did not run concurrently")
		}
	}
	require.EqualValues(t, distributedPortOwnerConcurrency, active.Load())
	// Serve both waves after observing the maximum permitted concurrency.
	for range len(pods) {
		release <- struct{}{}
	}
	require.NoError(t, <-done)
	require.LessOrEqual(t, peak.Load(), int32(distributedPortOwnerConcurrency))
}

func TestDistributedPortGroupBatchSharesVMLauncherRead(t *testing.T) {
	const subnet = "ovn-default"
	pod := distributedPortGroupTestPod("vm-launcher", subnet, "node-c")
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "vm",
	}}
	otherProvider := "secondary.default.ovn"
	pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, otherProvider)] = subnet
	pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, otherProvider)] = "10.0.0.3"
	fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
	require.NoError(t, err)
	ctrl := fixture.fakeController
	ctrl.config.EnableKeepVMIP = true
	kube := ctrl.config.KubeClient.(*fake.Clientset)
	kube.ClearActions()
	batch := distributedPortGroupBatch{lspUUIDs: []string{"vm-primary", "vm-secondary"}}
	owners := map[string]distributedPortOwner{
		"vm-primary": {pod: pod, provider: util.OvnProvider}, "vm-secondary": {pod: pod, provider: otherProvider},
	}
	fixture.mockOvnClient.EXPECT().ApplyPortGroupMembershipChanges(t.Context(), batch.changes, batch.lspUUIDs).Return(nil)
	require.NoError(t, ctrl.applyDistributedPortGroupBatch(t.Context(), subnet, batch, owners))
	gets, lists := 0, 0
	for _, action := range kube.Actions() {
		if action.Matches("get", "pods") {
			gets++
		}
		if action.Matches("list", "pods") {
			lists++
		}
	}
	require.Zero(t, gets)
	require.Equal(t, 1, lists)

	// A later batch must read again: the new migration launcher invalidates
	// the old owner even though the previous batch passed validation.
	sibling := pod.DeepCopy()
	sibling.Name, sibling.Spec.NodeName = "new-launcher", "node-d"
	_, err = kube.CoreV1().Pods(sibling.Namespace).Create(t.Context(), sibling, metav1.CreateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, ctrl.applyDistributedPortGroupBatch(t.Context(), subnet, batch, owners), "changed during reconciliation")

	unavailable := errors.New("Pod API unavailable")
	kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, unavailable
	})
	require.ErrorIs(t, ctrl.applyDistributedPortGroupBatch(t.Context(), subnet, batch, owners), unavailable)
}
