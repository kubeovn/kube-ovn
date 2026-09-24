package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/informer"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	testVMName    = "vm1"
	testVMKey     = "default/vm1"
	testSrcNode   = "node-src"
	testTgtNode   = "node-tgt"
	testMigration = "default/migration-cur"
)

var testBaseTime = time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

func newTestVMIMigration(name string, uid types.UID, createdAt time.Time, phase kubevirtv1.VirtualMachineInstanceMigrationPhase) *kubevirtv1.VirtualMachineInstanceMigration {
	m := &kubevirtv1.VirtualMachineInstanceMigration{
		Name:              name,
		Namespace:         metav1.NamespaceDefault,
		UID:               uid,
		CreationTimestamp: metav1.NewTime(createdAt),
		Spec:              kubevirtv1.VirtualMachineInstanceMigrationSpec{VMIName: testVMName},
		Status:            kubevirtv1.VirtualMachineInstanceMigrationStatus{Phase: phase},
	}
	// the handler bails out early when the migration itself has no state, so every
	// fixture carries one unless a test explicitly clears it
	m.Status.MigrationState = &kubevirtv1.VirtualMachineInstanceMigrationState{MigrationUID: uid}
	return m
}

// newTestVMI builds a VMI whose MigrationState belongs to migrationUID. An empty
// migrationUID leaves MigrationState nil, mimicking a VMI that never migrated.
func newTestVMI(nodeName string, migrationUID types.UID) *kubevirtv1.VirtualMachineInstance {
	vmi := &kubevirtv1.VirtualMachineInstance{
		Name:      testVMName,
		Namespace: metav1.NamespaceDefault,
		Status:    kubevirtv1.VirtualMachineInstanceStatus{NodeName: nodeName},
	}
	if migrationUID != "" {
		vmi.Status.MigrationState = &kubevirtv1.VirtualMachineInstanceMigrationState{
			MigrationUID: migrationUID,
			SourceNode:   testSrcNode,
			TargetNode:   testTgtNode,
		}
	}
	return vmi
}

func newTestMigrationTargetPod(name, nodeName string, uid types.UID) *corev1.Pod {
	return newTestMigrationPod(name, nodeName, uid, virtLauncherAppLabel)
}

// hotplug attachment pods share the migration job label with the target launcher pod
func newTestMigrationAttachmentPod(name, nodeName string, uid types.UID) *corev1.Pod {
	return newTestMigrationPod(name, nodeName, uid, string(kubevirtv1.HotplugAttachment))
}

func newTestMigrationPod(name, nodeName string, uid types.UID, app string) *corev1.Pod {
	return &corev1.Pod{
		Name:      name,
		Namespace: metav1.NamespaceDefault,
		Labels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(uid),
			kubevirtv1.AppLabel:          app,
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
	}
}

type kubevirtFixture struct {
	ctrl              *Controller
	mockOvnClient     *mockovs.MockNbClient
	mockVMIMigrations *kubecli.MockVirtualMachineInstanceMigrationInterface
	mockVMIs          *kubecli.MockVirtualMachineInstanceInterface
	vmimIndexer       cache.Indexer
}

func newKubevirtFixture(t *testing.T, opts *FakeControllerOptions) *kubevirtFixture {
	t.Helper()
	fc, err := newFakeControllerWithOptions(t, opts)
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	f := &kubevirtFixture{
		ctrl:              fc.fakeController,
		mockOvnClient:     fc.mockOvnClient,
		mockVMIMigrations: kubecli.NewMockVirtualMachineInstanceMigrationInterface(mockCtrl),
		mockVMIs:          kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl),
	}

	mockKubevirt := kubecli.NewMockKubevirtClient(mockCtrl)
	mockKubevirt.EXPECT().VirtualMachineInstanceMigration(metav1.NamespaceDefault).Return(f.mockVMIMigrations).AnyTimes()
	mockKubevirt.EXPECT().VirtualMachineInstance(metav1.NamespaceDefault).Return(f.mockVMIs).AnyTimes()

	ctrl := f.ctrl
	ctrl.config.KubevirtClient = mockKubevirt
	factory := informer.NewKubeVirtInformerFactoryWithOptions(nil, nil)
	ctrl.kubevirtInformerFactory = factory
	f.vmimIndexer = factory.VirtualMachineInstanceMigration().GetIndexer()
	ctrl.addOrUpdateVMIMigrationQueue = newTypedRateLimitingQueue[string]("AddOrUpdateVMIMigration", nil)
	ctrl.deleteVMQueue = newTypedRateLimitingQueue[string]("DeleteVM", nil)
	t.Cleanup(func() {
		ctrl.addOrUpdateVMIMigrationQueue.ShutDown()
		ctrl.deleteVMQueue.ShutDown()
	})

	return f
}

// expectMigrationAndVMI wires the two API reads every handler invocation performs.
func (f *kubevirtFixture) expectMigrationAndVMI(m *kubevirtv1.VirtualMachineInstanceMigration, vmi *kubevirtv1.VirtualMachineInstance) {
	f.mockVMIMigrations.EXPECT().Get(gomock.Any(), m.Name, gomock.Any()).Return(m, nil)
	f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(vmi, nil)
}

func (f *kubevirtFixture) expectPorts(portNames ...string) {
	lsps := make([]ovnnb.LogicalSwitchPort, 0, len(portNames))
	for _, name := range portNames {
		lsps = append(lsps, ovnnb.LogicalSwitchPort{Name: name})
	}
	f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, map[string]string{"pod": testVMKey}).Return(lsps, nil)
}

func TestHandleAddOrUpdateVMIMigrationEarlyReturns(t *testing.T) {
	t.Run("invalid key is dropped without error", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration("a/b/c"))
	})

	t.Run("migration get failure is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockVMIMigrations.EXPECT().Get(gomock.Any(), "migration-cur", gomock.Any()).Return(nil, errors.New("boom"))
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "boom")
	})

	t.Run("migration without state does not touch ovn", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		m.Status.MigrationState = nil
		f.mockVMIMigrations.EXPECT().Get(gomock.Any(), m.Name, gomock.Any()).Return(m, nil)
		// no VMI read and no OVN call is expected; gomock fails the test on any other call
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("vmi get failure is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.mockVMIMigrations.EXPECT().Get(gomock.Any(), m.Name, gomock.Any()).Return(m, nil)
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(nil, errors.New("no vmi"))
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "no vmi")
	})

	t.Run("lsp list failure is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, "uid-cur"))
		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, map[string]string{"pod": testVMKey}).Return(nil, errors.New("nb down"))
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "nb down")
	})

	t.Run("external vpc flag reaches the lsp lookup", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.ctrl.config.EnableExternalVpc = true
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationRunning)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": testVMKey}).Return(nil, nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("phase without handling performs no port writes", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationRunning)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})
}

// The terminal branch resets the migrate options. Its guards are the subject of
// several past regressions, so each is pinned here.
func TestHandleAddOrUpdateVMIMigrationTerminal(t *testing.T) {
	t.Run("succeeded resets every port of the vm", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, "uid-cur"))
		f.expectPorts("vm1.default", "vm1.default.1")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, false).Return(nil)
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default.1", testSrcNode, testTgtNode, false).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("failed rolls the port back to the source node", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, true).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("reset failure is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, "uid-cur"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, false).Return(errors.New("reset failed"))
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "reset failed")
	})

	// Regression: a terminal migration whose nodes can no longer be determined must
	// not fall through with empty source/target names.
	t.Run("stale vmi migration state stops the reset", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, "uid-someone-else"))
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("nil vmi migration state drops the options", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, ""))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions("vm1.default").Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// A migration that fails before the target pod is ready never reaches the VMI
	// migration state, so its nodes come from the target pod instead.
	t.Run("failure before handoff is reset from the target pod", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		// the VM never left the source node, and the state still belongs to an older migration
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default", "vm1.default.1")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, true).Return(nil)
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default.1", testSrcNode, testTgtNode, true).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// The recovered pair must match what the scheduling phase wrote, so it has to ignore
	// the hotplug attachment pods that share the job label just the same.
	t.Run("failure before handoff is reset from the launcher pod, not a hotplug attachment pod", func(t *testing.T) {
		attachment := newTestMigrationAttachmentPod("hp-volume-vm1", "node-hotplug", "uid-cur")
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{attachment, pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, true).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("failure before handoff is reset with a nil vmi migration state", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, ""))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, true).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// The recovered pair must reproduce what the scheduling phase wrote, so the cases
	// where that phase wrote nothing must recover nothing either.
	t.Run("failure before handoff without a target pod drops the options", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions("vm1.default").Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("failure before handoff on an unscheduled target pod drops the options", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", "", "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions("vm1.default").Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("failure before handoff on the same node drops the options", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testSrcNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions("vm1.default").Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("failure before handoff without a vmi node drops the options", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		f.expectMigrationAndVMI(m, newTestVMI("", "uid-previous"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions("vm1.default").Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// A succeeded migration always handed off, so a stale state means a newer migration
	// owns the ports; its nodes must never be recovered from a pod.
	t.Run("success with a stale state is never recovered from the target pod", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("recovered failure still yields to a newer active migration", func(t *testing.T) {
		pod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-cur")
		newPod := newTestMigrationTargetPod("virt-launcher-vm1-new", testTgtNode, "uid-new")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod, newPod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationFailed)
		require.NoError(t, f.vmimIndexer.Add(m))
		require.NoError(t, f.vmimIndexer.Add(newTestVMIMigration("migration-new", "uid-new", testBaseTime.Add(time.Minute), kubevirtv1.MigrationScheduling)))
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// Regression: replaying a terminal migration on controller restart must not strip
	// the options a newer migration has already installed on the same ports.
	t.Run("newer active migration keeps the reset from running", func(t *testing.T) {
		newPod := newTestMigrationTargetPod("virt-launcher-vm1-new", testTgtNode, "uid-new")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{newPod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		require.NoError(t, f.vmimIndexer.Add(m))
		require.NoError(t, f.vmimIndexer.Add(newTestVMIMigration("migration-new", "uid-new", testBaseTime.Add(time.Minute), kubevirtv1.MigrationScheduling)))
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, "uid-cur"))
		// the ports are still listed, but no reset may be issued against them
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("newer finished migration still allows the reset", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
		require.NoError(t, f.vmimIndexer.Add(m))
		require.NoError(t, f.vmimIndexer.Add(newTestVMIMigration("migration-new", "uid-new", testBaseTime.Add(time.Minute), kubevirtv1.MigrationSucceeded)))
		f.expectMigrationAndVMI(m, newTestVMI(testTgtNode, "uid-cur"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode, false).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})
}

func TestHandleAddOrUpdateVMIMigrationScheduling(t *testing.T) {
	const targetPodName = "virt-launcher-vm1-target"

	t.Run("sets migrate options on every port from the vmi migration state", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default", "vm1.default.1")
		f.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode).Return(nil)
		f.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions("vm1.default.1", testSrcNode, testTgtNode).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// Re-asserted on every phase up to the migration, so a scheduling event that arrived before
	// the target pod existed is not the only chance to set them
	t.Run("sets migrate options on every phase leading up to the migration", func(t *testing.T) {
		for _, phase := range []kubevirtv1.VirtualMachineInstanceMigrationPhase{
			kubevirtv1.MigrationScheduled,
			kubevirtv1.MigrationPreparingTarget,
			kubevirtv1.MigrationTargetReady,
		} {
			t.Run(string(phase), func(t *testing.T) {
				pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
				f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
				m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, phase)
				f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
				f.expectPorts("vm1.default")
				f.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode).Return(nil)
				require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
			})
		}
	})

	// The guest RARP that activates the target port lands around the end of MigrationRunning,
	// so re-adding activation-strategy=rarp from then on would block the port for good.
	t.Run("does not re-assert once the migration is running", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationRunning)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// Regression: while scheduling, the VMI migration state usually still belongs to
	// the previous migration, so the source node comes from vmi.Status.NodeName.
	t.Run("falls back to the vmi node name when the migration state is stale", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("set failure is retried", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode).Return(errors.New("set failed"))
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "set failed")
	})

	t.Run("waits without error until the target pod exists", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// Regression: an unscheduled target pod produces no further phase-change event, so
	// the handler must return an error to get itself requeued.
	t.Run("requeues while the target pod is unscheduled", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, "", "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "migration setup deferred")
	})

	t.Run("requeues while the source node is unknown", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		// stale state leaves srcNodeName empty and the VMI has no node name to fall back on
		f.expectMigrationAndVMI(m, newTestVMI("", "uid-previous"))
		f.expectPorts("vm1.default")
		require.ErrorContains(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration), "migration setup deferred")
	})

	// A same-node "migration" would make requested-chassis src==target, which OVN rejects.
	t.Run("skips without error when source and target are the same node", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testSrcNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-previous"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// Only pods carrying this migration's job label may be treated as its target.
	t.Run("ignores a target pod belonging to another migration", func(t *testing.T) {
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-other")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	// Regression: hotplug attachment pods share the job label and sort before the launcher,
	// so an unfiltered lookup pins the ports to whichever node the attachment landed on.
	t.Run("takes the target node from the launcher pod, not a hotplug attachment pod", func(t *testing.T) {
		attachment := newTestMigrationAttachmentPod("hp-volume-vm1", "node-hotplug", "uid-cur")
		pod := newTestMigrationTargetPod(targetPodName, testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{attachment, pod}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		f.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions("vm1.default", testSrcNode, testTgtNode).Return(nil)
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})

	t.Run("waits while only a hotplug attachment pod exists", func(t *testing.T) {
		attachment := newTestMigrationAttachmentPod("hp-volume-vm1", testTgtNode, "uid-cur")
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: []*corev1.Pod{attachment}})
		m := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.expectMigrationAndVMI(m, newTestVMI(testSrcNode, "uid-cur"))
		f.expectPorts("vm1.default")
		require.NoError(t, f.ctrl.handleAddOrUpdateVMIMigration(testMigration))
	})
}

// Only the pods the VM is live on may unpin the shared ports: a defunct pod that kubevirt or a
// user removes must leave them alone, whether or not a migration is running.
func TestHandleDeletePodMigrateOptionsCleanup(t *testing.T) {
	const (
		portName = "vm1.default"
		podKey   = metav1.NamespaceDefault + "/virt-launcher-vm1-source"
	)

	setup := func(t *testing.T, jobUID, node string, migrations ...*kubevirtv1.VirtualMachineInstanceMigration) *kubevirtFixture {
		t.Helper()
		pod, subnet := podEventFixture()
		pod.Name = "virt-launcher-vm1-source"
		pod.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: kubevirtv1.SchemeGroupVersion.String(),
			Kind:       util.KindVirtualMachineInstance,
			Name:       testVMName,
		}}
		pod.Spec.NodeName = node
		if jobUID != "" {
			pod.Labels = map[string]string{kubevirtv1.MigrationJobLabel: jobUID}
		}

		f := newKubevirtFixture(t, &FakeControllerOptions{
			Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet},
		})
		f.ctrl.config.EnableKeepVMIP = true
		for _, m := range migrations {
			require.NoError(t, f.vmimIndexer.Add(m))
		}

		// the VM runs on testSrcNode, so a pod on testTgtNode is not the pod it runs on
		vmi := newTestVMI(testSrcNode, "")
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(vmi, nil).AnyTimes()

		f.ctrl.deletingPodObjMap = xsync.NewMap[string, *corev1.Pod]()
		f.ctrl.deletingPodObjMap.Store(podKey, pod)
		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": testVMKey}).
			Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil).AnyTimes()
		// the port teardown that follows the cleanup is not what these cases assert on
		f.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Return(nil).AnyTimes()
		return f
	}

	t.Run("cleans the options when the vm is live on the pod", func(t *testing.T) {
		f := setup(t, "", testSrcNode)
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions(portName).Return(nil)
		require.NoError(t, f.ctrl.handleDeletePod(podKey))
	})

	t.Run("cleans the options when the pod is the target of a running migration", func(t *testing.T) {
		f := setup(t, "uid-new", testTgtNode,
			newTestVMIMigration("migration-new", "uid-new", testBaseTime, kubevirtv1.MigrationScheduling))
		f.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions(portName).Return(nil)
		require.NoError(t, f.ctrl.handleDeletePod(podKey))
	})

	// no CleanLogicalSwitchPortMigrateOptions is expected below; gomock fails on any call
	t.Run("keeps the options for a defunct pod while a migration runs", func(t *testing.T) {
		f := setup(t, "uid-old", testTgtNode,
			newTestVMIMigration("migration-old", "uid-old", testBaseTime, kubevirtv1.MigrationSucceeded),
			newTestVMIMigration("migration-new", "uid-new", testBaseTime.Add(time.Minute), kubevirtv1.MigrationScheduling),
		)
		require.NoError(t, f.ctrl.handleDeletePod(podKey))
	})

	t.Run("keeps the options for a defunct pod with nothing in flight", func(t *testing.T) {
		f := setup(t, "uid-old", testTgtNode,
			newTestVMIMigration("migration-old", "uid-old", testBaseTime, kubevirtv1.MigrationSucceeded))
		require.NoError(t, f.ctrl.handleDeletePod(podKey))
	})

	t.Run("keeps the options for a pod the vm never ran", func(t *testing.T) {
		f := setup(t, "", testTgtNode)
		require.NoError(t, f.ctrl.handleDeletePod(podKey))
	})
}

func TestDeletedPodOwnsMigrateOptions(t *testing.T) {
	newPod := func(name, node string, created time.Time, jobUID string) *corev1.Pod {
		pod := &corev1.Pod{
			Name:              name,
			Namespace:         metav1.NamespaceDefault,
			UID:               types.UID(name),
			CreationTimestamp: metav1.NewTime(created),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: kubevirtv1.SchemeGroupVersion.String(),
				Kind:       util.KindVirtualMachineInstance,
				Name:       testVMName,
			}},
			Spec: corev1.PodSpec{NodeName: node},
		}
		if jobUID != "" {
			pod.Labels = map[string]string{kubevirtv1.MigrationJobLabel: jobUID}
		}
		return pod
	}

	// the VM has migrated src -> tgt -> src, so a defunct pod shares the current node
	oldOnSrc := newPod("virt-launcher-old-src", testSrcNode, testBaseTime.Add(-3*time.Hour), "")
	oldOnTgt := newPod("virt-launcher-old-tgt", testTgtNode, testBaseTime.Add(-2*time.Hour), "uid-old")
	current := newPod("virt-launcher-current", testSrcNode, testBaseTime.Add(-time.Hour), "uid-old2")
	target := newPod("virt-launcher-target", testTgtNode, testBaseTime, "uid-new")
	allPods := []*corev1.Pod{oldOnSrc, oldOnTgt, current, target}

	migrations := []*kubevirtv1.VirtualMachineInstanceMigration{
		newTestVMIMigration("m-old", "uid-old", testBaseTime.Add(-2*time.Hour), kubevirtv1.MigrationSucceeded),
		newTestVMIMigration("m-old2", "uid-old2", testBaseTime.Add(-time.Hour), kubevirtv1.MigrationSucceeded),
		newTestVMIMigration("m-new", "uid-new", testBaseTime, kubevirtv1.MigrationScheduling),
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		owns bool
	}{
		{name: "the pod the vm runs on", pod: current, owns: true},
		{name: "target pod of the running migration", pod: target, owns: true},
		{name: "defunct pod on the node the vm left", pod: oldOnTgt, owns: false},
		{name: "defunct pod on the node the vm is back on", pod: oldOnSrc, owns: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newKubevirtFixture(t, &FakeControllerOptions{Pods: allPods})
			for _, m := range migrations {
				require.NoError(t, f.vmimIndexer.Add(m))
			}
			vmi := newTestVMI(testSrcNode, "")
			f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(vmi, nil)

			owns, err := f.ctrl.deletedPodOwnsMigrateOptions(tt.pod, testVMName)
			require.NoError(t, err)
			require.Equal(t, tt.owns, owns)
		})
	}
}

func TestDeletedPodOwnsMigrateOptionsVMIErrors(t *testing.T) {
	pod := &corev1.Pod{Name: "virt-launcher-vm1", Namespace: metav1.NamespaceDefault, UID: "pod-uid"}

	podOfVMI := func(vmiUID types.UID) *corev1.Pod {
		p := pod.DeepCopy()
		p.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: kubevirtv1.SchemeGroupVersion.String(),
			Kind:       util.KindVirtualMachineInstance,
			Name:       testVMName,
			UID:        vmiUID,
		}}
		return p
	}
	newVMIWithUID := func(uid types.UID) *kubevirtv1.VirtualMachineInstance {
		v := newTestVMI(testSrcNode, "")
		v.UID = uid
		return v
	}

	// a stopped or restarting VM has no vmi, while its shared lsp outlives it and has to be
	// unpinned for the VM to come back on another node
	t.Run("a gone vmi releases the options", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).
			Return(nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "virtualmachineinstances"}, testVMName))
		owns, err := f.ctrl.deletedPodOwnsMigrateOptions(pod, testVMName)
		require.NoError(t, err)
		require.True(t, owns)
	})

	// A deletion handled after the VM restarted refers to the previous vmi, so its options are
	// released unless the new vmi has already started migrating and taken them over.
	t.Run("a pod of an earlier vmi releases the options", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(newVMIWithUID("new-vmi"), nil)
		owns, err := f.ctrl.deletedPodOwnsMigrateOptions(podOfVMI("old-vmi"), testVMName)
		require.NoError(t, err)
		require.True(t, owns)
	})

	// This branch guards an unconditional CleanLogicalSwitchPortMigrateOptions, so it is the only
	// thing standing between a restart and the ports of a migration that has already pinned them.
	yieldsToMigration := func(t *testing.T, phase kubevirtv1.VirtualMachineInstanceMigrationPhase, pods ...*corev1.Pod) bool {
		t.Helper()
		f := newKubevirtFixture(t, &FakeControllerOptions{Pods: pods})
		require.NoError(t, f.vmimIndexer.Add(newTestVMIMigration("m", "uid-m", testBaseTime, phase)))
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(newVMIWithUID("new-vmi"), nil)
		owns, err := f.ctrl.deletedPodOwnsMigrateOptions(podOfVMI("old-vmi"), testVMName)
		require.NoError(t, err)
		return owns
	}
	targetPod := newTestMigrationTargetPod("virt-launcher-vm1-target", testTgtNode, "uid-m")

	t.Run("a pod of an earlier vmi yields to a running migration", func(t *testing.T) {
		require.False(t, yieldsToMigration(t, kubevirtv1.MigrationScheduling, targetPod))
	})

	// early Pending: the target launcher does not exist, so no migration can have pinned the
	// ports and the stale options of the previous vmi have to be released
	t.Run("a pod of an earlier vmi releases the options while a pending migration has no target", func(t *testing.T) {
		require.True(t, yieldsToMigration(t, kubevirtv1.MigrationPending))
	})

	// late Pending: the target launcher is up and may already be pinned, so the options stay
	t.Run("a pod of an earlier vmi yields to a pending migration that has its target", func(t *testing.T) {
		require.False(t, yieldsToMigration(t, kubevirtv1.MigrationPending, targetPod))
	})

	t.Run("a pod of an earlier vmi releases the options when the target pod is gone", func(t *testing.T) {
		require.True(t, yieldsToMigration(t, kubevirtv1.MigrationScheduling))
	})

	t.Run("a pod of the current vmi is not treated as stale", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(newVMIWithUID("new-vmi"), nil)
		// on no node while the vmi is on testSrcNode, so only the earlier-vmi rule could match
		owns, err := f.ctrl.deletedPodOwnsMigrateOptions(podOfVMI("new-vmi"), testVMName)
		require.NoError(t, err)
		require.False(t, owns)
	})

	t.Run("a failed vmi lookup is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockVMIs.EXPECT().Get(gomock.Any(), testVMName, gomock.Any()).Return(nil, errors.New("apiserver down"))
		_, err := f.ctrl.deletedPodOwnsMigrateOptions(pod, testVMName)
		require.ErrorContains(t, err, "apiserver down")
	})
}

// The ownership rule keys on the existence of a target launcher pod rather than on the phase:
// kubevirt always creates that pod during Pending and needs it to leave Pending, so the phase
// reports no target while one is already running.
func TestHasNewerActiveVMIMigration(t *testing.T) {
	current := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationSucceeded)
	target := func(uid types.UID) *corev1.Pod {
		return newTestMigrationTargetPod("virt-launcher-"+string(uid), testTgtNode, uid)
	}
	attachment := func(uid types.UID) *corev1.Pod {
		return newTestMigrationAttachmentPod("hp-volume-"+string(uid), testTgtNode, uid)
	}
	migration := func(uid types.UID, created time.Time, phase kubevirtv1.VirtualMachineInstanceMigrationPhase) *kubevirtv1.VirtualMachineInstanceMigration {
		return newTestVMIMigration("m-"+string(uid), uid, created, phase)
	}
	later := testBaseTime.Add(time.Minute)

	tests := []struct {
		name   string
		others []*kubevirtv1.VirtualMachineInstanceMigration
		pods   []*corev1.Pod
		expect bool
	}{
		{
			name:   "no other migration",
			expect: false,
		},
		{
			name:   "newer migration is still running",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", later, kubevirtv1.MigrationScheduling)},
			pods:   []*corev1.Pod{target("uid-2")},
			expect: true,
		},
		{
			// timestamps only have a one second resolution, so a tie counts as newer
			name:   "newer migration created within the same second",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", testBaseTime, kubevirtv1.MigrationScheduling)},
			pods:   []*corev1.Pod{target("uid-2")},
			expect: true,
		},
		{
			// early Pending: kubevirt has not created the target launcher yet, so nothing is pinned
			name:   "newer pending migration without a target pod",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", later, kubevirtv1.MigrationPending)},
			expect: false,
		},
		{
			// late Pending: with hotplug volumes the target launcher runs for the whole time the
			// attachment pod is being created and made ready, while the phase still reads Pending
			name:   "newer pending migration that already has its target pod",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", later, kubevirtv1.MigrationPending)},
			pods:   []*corev1.Pod{target("uid-2")},
			expect: true,
		},
		{
			// the phase alone is not enough in the other direction either
			name:   "newer migration whose target pod is gone",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", later, kubevirtv1.MigrationScheduling)},
			expect: false,
		},
		{
			name:   "a hotplug attachment pod is not a target pod",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", later, kubevirtv1.MigrationPending)},
			pods:   []*corev1.Pod{attachment("uid-2")},
			expect: false,
		},
		{
			name:   "newer migration already finished",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", later, kubevirtv1.MigrationSucceeded)},
			pods:   []*corev1.Pod{target("uid-2")},
			expect: false,
		},
		{
			name:   "older migration is still running",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{migration("uid-2", testBaseTime.Add(-time.Minute), kubevirtv1.MigrationScheduling)},
			pods:   []*corev1.Pod{target("uid-2")},
			expect: false,
		},
		{
			name: "running migration of another vmi",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{func() *kubevirtv1.VirtualMachineInstanceMigration {
				m := migration("uid-2", later, kubevirtv1.MigrationScheduling)
				m.Spec.VMIName = "vm2"
				return m
			}()},
			pods:   []*corev1.Pod{target("uid-2")},
			expect: false,
		},
		{
			name: "a finished newer migration does not mask a running one",
			others: []*kubevirtv1.VirtualMachineInstanceMigration{
				migration("uid-2", later, kubevirtv1.MigrationSucceeded),
				migration("uid-3", testBaseTime.Add(2*time.Minute), kubevirtv1.MigrationScheduling),
			},
			pods:   []*corev1.Pod{target("uid-3")},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newKubevirtFixture(t, &FakeControllerOptions{Pods: tt.pods})
			require.NoError(t, f.vmimIndexer.Add(current))
			for _, m := range tt.others {
				require.NoError(t, f.vmimIndexer.Add(m))
			}

			got, err := f.ctrl.hasNewerActiveVMIMigration(current)
			require.NoError(t, err)
			require.Equal(t, tt.expect, got)
		})
	}
}

func TestEnqueueVMIMigration(t *testing.T) {
	t.Run("add always enqueues", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.ctrl.enqueueAddVMIMigration(newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationPending))
		assert.Equal(t, 1, f.ctrl.addOrUpdateVMIMigrationQueue.Len())
	})

	t.Run("update without a phase change is dropped", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		old := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		f.ctrl.enqueueUpdateVMIMigration(old, old.DeepCopy())
		assert.Zero(t, f.ctrl.addOrUpdateVMIMigrationQueue.Len())
	})

	t.Run("update with a phase change is enqueued", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		old := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		updated := old.DeepCopy()
		updated.Status.Phase = kubevirtv1.MigrationSucceeded
		f.ctrl.enqueueUpdateVMIMigration(old, updated)
		assert.Equal(t, 1, f.ctrl.addOrUpdateVMIMigrationQueue.Len())
	})

	t.Run("deletion is enqueued even without a phase change", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		old := newTestVMIMigration("migration-cur", "uid-cur", testBaseTime, kubevirtv1.MigrationScheduling)
		updated := old.DeepCopy()
		updated.DeletionTimestamp = new(metav1.NewTime(testBaseTime))
		f.ctrl.enqueueUpdateVMIMigration(old, updated)
		assert.Equal(t, 1, f.ctrl.addOrUpdateVMIMigrationQueue.Len())
	})
}

func TestEnqueueDeleteVM(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{Name: testVMName, Namespace: metav1.NamespaceDefault}

	tests := []struct {
		name   string
		obj    any
		expect int
	}{
		{name: "virtual machine", obj: vm, expect: 1},
		{name: "tombstone wrapping a virtual machine", obj: cache.DeletedFinalStateUnknown{Key: testVMKey, Obj: vm}, expect: 1},
		{name: "tombstone wrapping something else", obj: cache.DeletedFinalStateUnknown{Key: testVMKey, Obj: &corev1.Pod{}}, expect: 0},
		{name: "unexpected type", obj: &corev1.Pod{}, expect: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newKubevirtFixture(t, nil)
			require.NotPanics(t, func() { f.ctrl.enqueueDeleteVM(tt.obj) })
			assert.Equal(t, tt.expect, f.ctrl.deleteVMQueue.Len())
		})
	}
}

func TestHandleDeleteVM(t *testing.T) {
	const portName = "vm1.default"

	newIP := func() *kubeovnv1.IP {
		return &kubeovnv1.IP{Name: portName}
	}

	t.Run("invalid key is dropped without error", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		require.NoError(t, f.ctrl.handleDeleteVM("a/b/c"))
	})

	t.Run("lsp list failure is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": testVMKey}).Return(nil, errors.New("nb down"))
		require.ErrorContains(t, f.ctrl.handleDeleteVM(testVMKey), "nb down")
	})

	t.Run("releases the ip, the ipam address and the port", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			Name: "test-subnet",
			Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24", Gateway: "10.0.0.1", Protocol: kubeovnv1.ProtocolIPv4},
		}
		f := newKubevirtFixture(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}, IPs: []*kubeovnv1.IP{newIP()}})
		ctrl := f.ctrl
		require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
		_, _, _, err := ctrl.ipam.GetStaticAddress(testVMKey, portName, "10.0.0.2", nil, subnet.Name, false)
		require.NoError(t, err)
		require.NotEmpty(t, ctrl.ipam.GetPodAddress(testVMKey))

		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": testVMKey}).
			Return([]ovnnb.LogicalSwitchPort{{Name: portName, ExternalIDs: map[string]string{"ls": subnet.Name}}}, nil)
		f.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)

		require.NoError(t, ctrl.handleDeleteVM(testVMKey))
		assert.Empty(t, ctrl.ipam.GetPodAddress(testVMKey))
		_, err = ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(t.Context(), portName, metav1.GetOptions{})
		assert.True(t, k8serrors.IsNotFound(err))
	})

	t.Run("a missing ip does not stop the port cleanup", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": testVMKey}).
			Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
		f.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)
		require.NoError(t, f.ctrl.handleDeleteVM(testVMKey))
	})

	t.Run("lsp delete failure is retried", func(t *testing.T) {
		f := newKubevirtFixture(t, nil)
		f.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": testVMKey}).
			Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
		f.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(errors.New("delete failed"))
		require.ErrorContains(t, f.ctrl.handleDeleteVM(testVMKey), "delete failed")
	})
}
