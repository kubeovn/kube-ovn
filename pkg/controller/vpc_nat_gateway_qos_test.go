package controller

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNatGwRedoTokenUsesContainerInstancesPerGateway(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	started := metav1.NewTime(created.Add(time.Minute))
	gateway := func(name string) *kubeovnv1.VpcNatGateway {
		return &kubeovnv1.VpcNatGateway{Name: name}
	}
	pod := func(gateway, containerID string) *corev1.Pod {
		return &corev1.Pod{
			Name: gateway + "-0", Namespace: metav1.NamespaceSystem, UID: types.UID(gateway + "-uid"),
			Labels:            map[string]string{"app": "vpc-nat-gw-" + gateway, "ovn.kubernetes.io/vpc-nat-gw": "true"},
			CreationTimestamp: created,
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "vpc-nat-gw", ContainerID: containerID,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
				}},
			},
		}
	}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gateway("gw1"), gateway("gw2")},
		Pods:           []*corev1.Pod{pod("gw1", "containerd://one"), pod("gw2", "containerd://two")},
	})
	require.NoError(t, err)

	token1, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	token2, err := fakeCtrl.fakeController.natGwRedoToken("gw2")
	require.NoError(t, err)
	require.Len(t, token1, sha256.Size*2)
	require.Len(t, token2, sha256.Size*2)
	require.NotEqual(t, token1, token2)

	restarted := pod("gw1", "containerd://restarted")
	_, err = fakeCtrl.fakeController.config.KubeClient.CoreV1().Pods(metav1.NamespaceSystem).Update(
		context.Background(), restarted, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	restartedToken, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	require.Len(t, restartedToken, sha256.Size*2)
	require.NotEqual(t, token1, restartedToken)

	// A replica whose container is not running carries no instance to identify and
	// must not stop the other replicas from being restored.
	waiting := pod("gw1", "")
	waiting.Name = "gw1-1"
	waiting.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "vpc-nat-gw",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason: "CrashLoopBackOff",
		}},
	}}
	_, err = fakeCtrl.fakeController.config.KubeClient.CoreV1().Pods(metav1.NamespaceSystem).Create(
		context.Background(), waiting, metav1.CreateOptions{},
	)
	require.NoError(t, err)
	withoutWaitingReplica, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	require.Equal(t, restartedToken, withoutWaitingReplica)

	// Starting that container adds its instance to the token, which asks for a redo.
	waiting.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "vpc-nat-gw", ContainerID: "containerd://second-replica",
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
	}}
	_, err = fakeCtrl.fakeController.config.KubeClient.CoreV1().Pods(metav1.NamespaceSystem).Update(
		context.Background(), waiting, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	withWaitingReplica, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	require.NotEqual(t, withoutWaitingReplica, withWaitingReplica)
}

func TestNatGwInitTargets(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	live := &corev1.Pod{Name: "gw-0"}
	terminating := &corev1.Pod{Name: "gw-old", DeletionTimestamp: &now}

	// A terminating Pod cannot be initialized, so its replacement has to be waited for
	// instead of reporting a successful initialization of the gateway.
	require.Equal(t, []*corev1.Pod{live}, natGwInitTargets([]*corev1.Pod{terminating, live}))
	require.Empty(t, natGwInitTargets([]*corev1.Pod{terminating}))
	require.Empty(t, natGwInitTargets(nil))

	// Only a gateway that still exists waits for its replacement Pod: a gateway already
	// marked for deletion has nothing left to initialize, and a gateway with a Pod has a
	// target to run the init command on.
	gateway := &kubeovnv1.VpcNatGateway{Name: "gw"}
	require.True(t, natGwInitPending(gateway, nil))
	require.False(t, natGwInitPending(gateway, []*corev1.Pod{live}))
	terminatingGateway := gateway.DeepCopy()
	terminatingGateway.DeletionTimestamp = &now
	require.False(t, natGwInitPending(terminatingGateway, nil))
}

// TestUpdateCrdNatGwLabelsQoSClaim pins the claim the gateway label carries: the UID of the
// policy generation it references. A terminating policy must not be taken by a new binding,
// while a gateway that already references it keeps the claim it had, so that the referenced
// policy generation is still recognized while the gateway cleans up.
// TestNatGwQoSClaimIsWrittenBeforeRestore pins that a gateway re-asserts the claim of the policy
// generation whose rules it re-applies when its pod is (re)initialized.
func TestNatGwQoSClaimIsWrittenBeforeRestore(t *testing.T) {
	rules := kubeovnv1.QoSPolicyBandwidthLimitRules{{Direction: kubeovnv1.QoSDirectionEgress, Interface: "net1"}}
	qos := &kubeovnv1.QoSPolicy{
		Name: "qos", UID: "qos-uid",
		Spec: kubeovnv1.QoSPolicySpec{
			Shared: true, BindingType: kubeovnv1.QoSBindingTypeNatGw, BandwidthLimitRules: rules,
		},
		Status: kubeovnv1.QoSPolicyStatus{
			Shared: true, BindingType: kubeovnv1.QoSBindingTypeNatGw, BandwidthLimitRules: rules,
		},
	}
	gw := &kubeovnv1.VpcNatGateway{
		Name: "gw",
		Labels: map[string]string{
			util.SubnetNameLabel: "test-subnet", util.VpcNameLabel: "test-vpc",
		},
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "test-vpc", Subnet: "test-subnet", QoSPolicy: "qos"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies: []*kubeovnv1.QoSPolicy{qos}, VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
	})
	require.NoError(t, err)

	// The gateway has no pod, so the rules cannot be re-applied and the call fails.
	require.Error(t, fc.fakeController.claimAndRestoreVpcNatGwQoS(gw))
	got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Get(
		context.Background(), "gw", metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.Equal(t, "qos-uid", got.Labels[util.QoSPolicyUIDLabel])
}

func TestUpdateCrdNatGwLabelsQoSClaim(t *testing.T) {
	now := metav1.Now()
	policy := func(name, uid string, terminating bool) *kubeovnv1.QoSPolicy {
		qos := &kubeovnv1.QoSPolicy{Name: name, UID: types.UID(uid)}
		if terminating {
			qos.DeletionTimestamp = &now
		}
		return qos
	}
	gateway := func(labels map[string]string, terminating bool) *kubeovnv1.VpcNatGateway {
		gw := &kubeovnv1.VpcNatGateway{
			Name: "gw", Labels: labels,
			Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "test-vpc", Subnet: "test-subnet"},
		}
		if terminating {
			gw.DeletionTimestamp = &now
		}
		return gw
	}
	bound := func() map[string]string {
		return map[string]string{
			util.SubnetNameLabel: "test-subnet", util.VpcNameLabel: "test-vpc",
			util.QoSLabel: "qos", util.QoSPolicyUIDLabel: "qos-uid",
		}
	}

	tests := []struct {
		name      string
		qos       string
		policies  []*kubeovnv1.QoSPolicy
		gateway   *kubeovnv1.VpcNatGateway
		wantErr   string
		wantClaim string
	}{
		{
			name: "healthy policy claims its generation", qos: "qos",
			policies:  []*kubeovnv1.QoSPolicy{policy("qos", "qos-uid", false)},
			gateway:   gateway(map[string]string{util.SubnetNameLabel: "test-subnet", util.VpcNameLabel: "test-vpc"}, false),
			wantClaim: "qos-uid",
		},
		{
			name: "terminating policy is rejected for a new binding", qos: "qos",
			policies: []*kubeovnv1.QoSPolicy{policy("qos", "qos-uid", true)},
			gateway:  gateway(nil, false), wantErr: "is terminating",
		},
		{
			name: "terminating policy keeps the claim of a bound gateway", qos: "qos",
			policies: []*kubeovnv1.QoSPolicy{policy("qos", "qos-uid", true)},
			gateway:  gateway(bound(), false), wantClaim: "qos-uid",
		},
		{
			name: "missing policy keeps the claim of a terminating gateway", qos: "qos",
			gateway: gateway(bound(), true), wantClaim: "qos-uid",
		},
		{
			name: "missing policy is reported for a live gateway", qos: "qos",
			gateway: gateway(bound(), false), wantErr: "failed to get qos policy",
		},
		{
			name: "unbinding drops the claim", qos: "",
			gateway: gateway(bound(), false), wantClaim: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				QoSPolicies: tt.policies, VpcNatGateways: []*kubeovnv1.VpcNatGateway{tt.gateway},
			})
			require.NoError(t, err)

			err = fc.fakeController.updateCrdNatGwLabels("gw", tt.qos)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Get(
				context.Background(), "gw", metav1.GetOptions{},
			)
			require.NoError(t, err)
			require.Equal(t, tt.wantClaim, got.Labels[util.QoSPolicyUIDLabel])
		})
	}
}
