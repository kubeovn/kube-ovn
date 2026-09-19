package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/client-go/kubecli"

	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/informer"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func TestHandleAddOrUpdateVMIMigrationConfiguresPendingMigration(t *testing.T) {
	const (
		namespace  = "test"
		migration  = "test-migration"
		vmiName    = "test-vmi"
		sourceNode = "source-node"
		targetNode = "target-node"
		portName   = "test-vmi.test"
	)
	migrationUID := types.UID("migration-uid")
	targetPod := &corev1.Pod{
		Name:      "virt-launcher-test-vmi-target",
		Namespace: namespace,
		Labels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(migrationUID),
			kubevirtv1.AppLabel:          "virt-launcher",
		},
		Spec: corev1.PodSpec{NodeName: targetNode},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{targetPod}})
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	kubevirtClient := kubecli.NewMockKubevirtClient(mockCtrl)
	migrationClient := kubecli.NewMockVirtualMachineInstanceMigrationInterface(mockCtrl)
	vmiClient := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
	fc.fakeController.config.KubevirtClient = kubevirtClient

	vmiMigration := &kubevirtv1.VirtualMachineInstanceMigration{
		Name: migration, Namespace: namespace, UID: migrationUID,
		Spec: kubevirtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmiName},
		Status: kubevirtv1.VirtualMachineInstanceMigrationStatus{
			Phase: kubevirtv1.MigrationPending,
		},
	}
	vmi := &kubevirtv1.VirtualMachineInstance{
		Name: vmiName, Namespace: namespace,
		Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: sourceNode},
	}

	kubevirtClient.EXPECT().VirtualMachineInstanceMigration(namespace).Return(migrationClient)
	migrationClient.EXPECT().Get(gomock.Any(), migration, metav1.GetOptions{}).Return(vmiMigration, nil)
	kubevirtClient.EXPECT().VirtualMachineInstance(namespace).Return(vmiClient)
	vmiClient.EXPECT().Get(gomock.Any(), vmiName, metav1.GetOptions{}).Return(vmi, nil)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, map[string]string{"pod": namespace + "/" + vmiName}).
		Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions(portName, sourceNode, targetNode).Return(nil)

	require.NoError(t, fc.fakeController.handleAddOrUpdateVMIMigration(namespace+"/"+migration))
}

func TestHandleAddOrUpdateVMIMigrationIgnoresHotplugAttachmentPod(t *testing.T) {
	const (
		namespace = "test"
		migration = "test-migration"
		vmiName   = "test-vmi"
		portName  = "test-vmi.test"
	)
	migrationUID := types.UID("migration-uid")
	attachmentPod := &corev1.Pod{
		Name:      "hp-volume-test",
		Namespace: namespace,
		Labels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(migrationUID),
			kubevirtv1.AppLabel:          "hotplug-disk",
		},
		Spec: corev1.PodSpec{NodeName: "target-node"},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{attachmentPod}})
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	kubevirtClient := kubecli.NewMockKubevirtClient(mockCtrl)
	migrationClient := kubecli.NewMockVirtualMachineInstanceMigrationInterface(mockCtrl)
	vmiClient := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
	fc.fakeController.config.KubevirtClient = kubevirtClient

	vmiMigration := &kubevirtv1.VirtualMachineInstanceMigration{
		Name: migration, Namespace: namespace, UID: migrationUID,
		Spec: kubevirtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmiName},
		Status: kubevirtv1.VirtualMachineInstanceMigrationStatus{
			Phase: kubevirtv1.MigrationPending,
		},
	}
	vmi := &kubevirtv1.VirtualMachineInstance{
		Name: vmiName, Namespace: namespace,
		Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: "source-node"},
	}

	kubevirtClient.EXPECT().VirtualMachineInstanceMigration(namespace).Return(migrationClient)
	migrationClient.EXPECT().Get(gomock.Any(), migration, metav1.GetOptions{}).Return(vmiMigration, nil)
	kubevirtClient.EXPECT().VirtualMachineInstance(namespace).Return(vmiClient)
	vmiClient.EXPECT().Get(gomock.Any(), vmiName, metav1.GetOptions{}).Return(vmi, nil)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, map[string]string{"pod": namespace + "/" + vmiName}).
		Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)

	err = fc.fakeController.handleAddOrUpdateVMIMigration(namespace + "/" + migration)
	require.ErrorContains(t, err, "target launcher pod not yet created")
}

func TestHandleAddOrUpdateVMIMigrationCleansFailedMigrationWithoutMigrationState(t *testing.T) {
	const (
		namespace  = "test"
		migration  = "test-migration"
		vmiName    = "test-vmi"
		sourceNode = "source-node"
		targetNode = "target-node"
		portName   = "test-vmi.test"
	)
	migrationUID := types.UID("migration-uid")
	targetLauncherPod := &corev1.Pod{
		Name:      "virt-launcher-test-vmi-target",
		Namespace: namespace,
		Labels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(migrationUID),
			kubevirtv1.AppLabel:          "virt-launcher",
		},
		Spec: corev1.PodSpec{NodeName: targetNode},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{targetLauncherPod}})
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	kubevirtClient := kubecli.NewMockKubevirtClient(mockCtrl)
	migrationClient := kubecli.NewMockVirtualMachineInstanceMigrationInterface(mockCtrl)
	vmiClient := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
	fc.fakeController.config.KubevirtClient = kubevirtClient

	vmiMigration := &kubevirtv1.VirtualMachineInstanceMigration{
		Name: migration, Namespace: namespace, UID: migrationUID,
		Spec: kubevirtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmiName},
		Status: kubevirtv1.VirtualMachineInstanceMigrationStatus{
			Phase: kubevirtv1.MigrationPending,
		},
	}
	vmi := &kubevirtv1.VirtualMachineInstance{
		Name: vmiName, Namespace: namespace,
		Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: sourceNode},
	}

	kubevirtClient.EXPECT().VirtualMachineInstanceMigration(namespace).Return(migrationClient).Times(2)
	migrationClient.EXPECT().Get(gomock.Any(), migration, metav1.GetOptions{}).Return(vmiMigration, nil).Times(2)
	kubevirtClient.EXPECT().VirtualMachineInstance(namespace).Return(vmiClient).Times(2)
	vmiClient.EXPECT().Get(gomock.Any(), vmiName, metav1.GetOptions{}).Return(vmi, nil).Times(2)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, map[string]string{"pod": namespace + "/" + vmiName}).
		Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil).Times(2)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions(portName, sourceNode, targetNode).Return(nil)
	fc.mockOvnClient.EXPECT().ResetLogicalSwitchPortMigrateOptions(portName, sourceNode, "", true).Return(nil)

	require.NoError(t, fc.fakeController.handleAddOrUpdateVMIMigration(namespace+"/"+migration))
	vmiMigration.Status.Phase = kubevirtv1.MigrationFailed
	require.NoError(t, fc.fakeController.handleAddOrUpdateVMIMigration(namespace+"/"+migration))
}

func TestHandleAddOrUpdateVMIMigrationSkipsStaleFailedMigrationCleanup(t *testing.T) {
	// Migration B has already installed its options when the delayed Failed event for migration A is processed.
	const (
		namespace  = "test"
		vmiName    = "test-vmi"
		sourceNode = "source-node"
		targetNode = "target-node"
		portName   = "test-vmi.test"
	)
	failedMigration := &kubevirtv1.VirtualMachineInstanceMigration{
		Name: "failed-migration", Namespace: namespace, UID: types.UID("failed-migration-uid"),
		Spec:   kubevirtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmiName},
		Status: kubevirtv1.VirtualMachineInstanceMigrationStatus{Phase: kubevirtv1.MigrationFailed},
	}
	pendingMigration := &kubevirtv1.VirtualMachineInstanceMigration{
		Name: "pending-migration", Namespace: namespace, UID: types.UID("pending-migration-uid"),
		Spec:   kubevirtv1.VirtualMachineInstanceMigrationSpec{VMIName: vmiName},
		Status: kubevirtv1.VirtualMachineInstanceMigrationStatus{Phase: kubevirtv1.MigrationPending},
	}
	targetLauncherPod := &corev1.Pod{
		Name:      "virt-launcher-test-vmi-target",
		Namespace: namespace,
		Labels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(pendingMigration.UID),
			kubevirtv1.AppLabel:          "virt-launcher",
		},
		Spec: corev1.PodSpec{NodeName: targetNode},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{targetLauncherPod}})
	require.NoError(t, err)
	fc.fakeController.vmiMigrationIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, informer.GetVirtualMachineInstanceMigrationInformerIndexers())
	require.NoError(t, fc.fakeController.vmiMigrationIndexer.Add(failedMigration))
	require.NoError(t, fc.fakeController.vmiMigrationIndexer.Add(pendingMigration))

	mockCtrl := gomock.NewController(t)
	kubevirtClient := kubecli.NewMockKubevirtClient(mockCtrl)
	migrationClient := kubecli.NewMockVirtualMachineInstanceMigrationInterface(mockCtrl)
	vmiClient := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
	fc.fakeController.config.KubevirtClient = kubevirtClient
	vmi := &kubevirtv1.VirtualMachineInstance{
		Name: vmiName, Namespace: namespace,
		Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: sourceNode},
	}

	kubevirtClient.EXPECT().VirtualMachineInstanceMigration(namespace).Return(migrationClient).Times(2)
	migrationClient.EXPECT().Get(gomock.Any(), pendingMigration.Name, metav1.GetOptions{}).Return(pendingMigration, nil)
	migrationClient.EXPECT().Get(gomock.Any(), failedMigration.Name, metav1.GetOptions{}).Return(failedMigration, nil)
	kubevirtClient.EXPECT().VirtualMachineInstance(namespace).Return(vmiClient).Times(2)
	vmiClient.EXPECT().Get(gomock.Any(), vmiName, metav1.GetOptions{}).Return(vmi, nil).Times(2)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, map[string]string{"pod": namespace + "/" + vmiName}).
		Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortMigrateOptions(portName, sourceNode, targetNode).Return(nil)

	require.NoError(t, fc.fakeController.handleAddOrUpdateVMIMigration(namespace+"/"+pendingMigration.Name))
	require.NoError(t, fc.fakeController.handleAddOrUpdateVMIMigration(namespace+"/"+failedMigration.Name))
}
