package controller

import (
	"errors"
	"fmt"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"
	"k8s.io/utils/set"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func requireRecorderEvent(t *testing.T, recorder *record.FakeRecorder) string {
	t.Helper()
	select {
	case event := <-recorder.Events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return ""
	}
}

func TestRecordVpcEgressGatewayEvent(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
	}

	c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, "ReconcileWorkloadFailed", "boom")

	require.Equal(t, "Warning ReconcileWorkloadFailed boom", requireRecorderEvent(t, recorder))
}

func TestRecordVpcEgressGatewayError(t *testing.T) {
	tests := []struct {
		reason string
		err    error
	}{
		{reason: "UpdateStatusFailed", err: errors.New("status update failed")},
		{reason: "GetVpcFailed", err: errors.New("vpc lookup failed")},
		{reason: "GetPodSelectorFailed", err: errors.New("selector invalid")},
		{reason: "ListWorkloadPodsFailed", err: errors.New("pod list failed")},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			recorder := record.NewFakeRecorder(1)
			c := &Controller{recorder: recorder}
			gw := &kubeovnv1.VpcEgressGateway{}

			err := c.recordVpcEgressGatewayError(gw, tt.reason, tt.err)

			require.ErrorIs(t, err, tt.err)
			require.Equal(t, "Warning "+tt.reason+" "+tt.err.Error(), requireRecorderEvent(t, recorder))
		})
	}
}

func TestRecordVpcEgressGatewayKeyError(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	sourceErr := errors.New("cache unavailable")

	err := c.recordVpcEgressGatewayKeyError("default", "egress-gw", "GetVpcEgressGatewayFailed", sourceErr)

	require.ErrorIs(t, err, sourceErr)
	require.Equal(t, "Warning GetVpcEgressGatewayFailed cache unavailable", requireRecorderEvent(t, recorder))
}

func TestVpcEgressGatewayReadyConditionChanged(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	gw.Generation = 2
	gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", "waiting", gw.Generation)

	require.False(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, "Processing", "waiting"))
	require.True(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, "Processing", "still waiting"))
	require.True(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionTrue, "ReconcileSuccess", ""))
}

func TestSetVpcEgressGatewayNotReadyClearsStaleReady(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	gw.Generation = 2
	gw.Status.Ready = true
	gw.Status.Phase = kubeovnv1.PhaseCompleted
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)

	changed := setVpcEgressGatewayNotReady(gw, "ReconcileOVNRoutesFailed", "route failed")

	require.True(t, changed)
	require.False(t, gw.Status.Ready)
	require.Equal(t, kubeovnv1.PhaseProcessing, gw.Status.Phase)
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.Ready)
	require.NotNil(t, condition)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "ReconcileOVNRoutesFailed", condition.Reason)
	require.Equal(t, "route failed", condition.Message)
}

func TestFailVpcEgressGatewayReconcilePreservesErrors(t *testing.T) {
	reconcileErr := errors.New("route failed")
	statusErr := errors.New("status update failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
		Status: kubeovnv1.VpcEgressGatewayStatus{
			Ready: true,
			Phase: kubeovnv1.PhaseCompleted,
		},
	}
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)
	client := kubeovnfake.NewSimpleClientset(gw)
	client.PrependReactor("update", "vpc-egress-gateways", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, statusErr
	})
	c := &Controller{
		config:   &Configuration{KubeOvnClient: client},
		recorder: record.NewFakeRecorder(2),
	}

	err := c.failVpcEgressGatewayReconcile(gw, "ReconcileOVNRoutesFailed", reconcileErr)
	require.ErrorIs(t, err, reconcileErr)
	require.ErrorIs(t, err, statusErr)
	require.False(t, gw.Status.Ready)
	require.Equal(t, kubeovnv1.PhaseProcessing, gw.Status.Phase)
}

func TestFailVpcEgressGatewayReconcileRecordsRepeatedFailure(t *testing.T) {
	reconcileErr := errors.New("route failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Generation: 2,
		},
	}
	gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "ReconcileOVNRoutesFailed", reconcileErr.Error(), gw.Generation)
	client := kubeovnfake.NewSimpleClientset(gw)
	client.PrependReactor("update", "vpc-egress-gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, action.(ktesting.UpdateAction).GetObject(), nil
	})
	recorder := record.NewFakeRecorder(1)
	c := &Controller{
		config:   &Configuration{KubeOvnClient: client},
		recorder: recorder,
	}

	err := c.failVpcEgressGatewayReconcile(gw, "ReconcileOVNRoutesFailed", reconcileErr)
	require.ErrorIs(t, err, reconcileErr)
	require.Equal(t, "Warning ReconcileOVNRoutesFailed route failed", requireRecorderEvent(t, recorder))
}

func newVpcEgressGatewayDeleteController(t *testing.T, gw *kubeovnv1.VpcEgressGateway) (*Controller, *kubeovnfake.Clientset, *record.FakeRecorder) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(gw))
	kubeOvnClient := kubeovnfake.NewSimpleClientset(gw)
	// The generated client uses a hyphenated resource name that the object tracker cannot infer from the kind.
	kubeOvnClient.PrependReactor("update", "vpc-egress-gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, action.(ktesting.UpdateAction).GetObject(), nil
	})

	mockCtrl := gomock.NewController(t)
	mockOvnClient := mockovs.NewMockNbClient(mockCtrl)
	mockOvnClient.EXPECT().FindBFD(gomock.Any()).Return(nil, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(util.DefaultVpc, -1, gomock.Any()).Return(nil)
	mockOvnClient.EXPECT().DeletePortGroup(gomock.Any()).Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSet(gomock.Any()).Return(nil).Times(2)
	recorder := record.NewFakeRecorder(2)

	c := &Controller{
		config: &Configuration{
			ClusterRouter: util.DefaultVpc,
			KubeOvnClient: kubeOvnClient,
		},
		OVNNbClient:              mockOvnClient,
		recorder:                 recorder,
		vpcEgressGatewayKeyMutex: keymutex.NewHashed(0),
		vpcEgressGatewayLister:   kubeovnlister.NewVpcEgressGatewayLister(indexer),
	}
	return c, kubeOvnClient, recorder
}

func TestHandleDelVpcEgressGatewayReturnsFinalizerUpdateError(t *testing.T) {
	updateErr := errors.New("update failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
	}
	c, kubeOvnClient, recorder := newVpcEgressGatewayDeleteController(t, gw)
	kubeOvnClient.PrependReactor("update", "vpc-egress-gateways", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, updateErr
	})

	err := c.handleDelVpcEgressGateway("default/egress-gw")
	require.ErrorIs(t, err, updateErr)
	require.Equal(t, "Warning DeleteFailed failed to remove finalizer from vpc-egress-gateway default/egress-gw: update failed", requireRecorderEvent(t, recorder))
}

func TestHandleDelVpcEgressGatewayDoesNotRecordSuccessWithoutFinalizer(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
	}
	c, _, recorder := newVpcEgressGatewayDeleteController(t, gw)

	require.NoError(t, c.handleDelVpcEgressGateway("default/egress-gw"))
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event %q", event)
	default:
	}
}

func TestHandleDelVpcEgressGatewayRecordsSuccessAfterFinalizerUpdate(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
	}
	c, _, recorder := newVpcEgressGatewayDeleteController(t, gw)

	require.NoError(t, c.handleDelVpcEgressGateway("default/egress-gw"))
	require.Equal(t, "Normal DeleteSuccess VpcEgressGateway default/egress-gw deleted successfully", requireRecorderEvent(t, recorder))
}

func TestVpcEgressGatewayContainerBFDDDefaultResources(t *testing.T) {
	container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5, false)

	require.Equal(t, "200m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "300m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "50Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "64Mi", container.Resources.Limits.Memory().String())
	ephemeralStorage := container.Resources.Limits[corev1.ResourceEphemeralStorage]
	require.Equal(t, "1Gi", ephemeralStorage.String())

	require.NotNil(t, container.StartupProbe)
	require.NotNil(t, container.LivenessProbe)
	require.NotNil(t, container.ReadinessProbe)
	require.Equal(t, []string{"/kube-ovn/kube-ovn-bfdd-supervisor", "run"}, container.Command)
	require.Equal(t, []string{"/kube-ovn/kube-ovn-bfdd-supervisor", "live"}, container.StartupProbe.Exec.Command)
	require.EqualValues(t, 30, container.StartupProbe.FailureThreshold)
	require.Equal(t, []string{"/kube-ovn/kube-ovn-bfdd-supervisor", "live"}, container.LivenessProbe.Exec.Command)
	require.EqualValues(t, 10, container.LivenessProbe.TimeoutSeconds)
	require.Equal(t, []string{"/kube-ovn/kube-ovn-bfdd-supervisor", "live"}, container.ReadinessProbe.Exec.Command)
	require.EqualValues(t, 10, container.ReadinessProbe.TimeoutSeconds)
	require.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "bfdd-supervisor-state", MountPath: "/var/run/kube-ovn/bfdd-supervisor"})
	require.Contains(t, container.Ports, corev1.ContainerPort{Name: "bfdd-metrics", ContainerPort: 10669, Protocol: corev1.ProtocolTCP})
}

func TestVpcEgressGatewayBFDDRuntimeProbeTransport(t *testing.T) {
	t.Run("default VPC uses HTTP", func(t *testing.T) {
		container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5, true)

		require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, container.StartupProbe.Exec.Command)
		for _, probe := range []*corev1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			require.Nil(t, probe.Exec)
			require.Equal(t, "/livez", probe.HTTPGet.Path)
			require.Equal(t, "bfdd-metrics", probe.HTTPGet.Port.StrVal)
		}
	})

	t.Run("custom VPC falls back to exec", func(t *testing.T) {
		container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5, false)

		for _, probe := range []*corev1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			require.Nil(t, probe.HTTPGet)
			require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, probe.Exec.Command)
		}
	})
}

func TestConfigureVpcEgressGatewayBFDWorkload(t *testing.T) {
	deploy := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "sidecar"}, {Name: "gateway"}},
		InitContainers: []corev1.Container{
			{Name: "other", Command: []string{"bash", "-c", "unchanged"}},
			{Name: "init", Command: []string{"bash", "-c", "old"}},
		},
	}}}}
	container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 3, false)

	require.NoError(t, configureVpcEgressGatewayBFDWorkload(deploy, container))

	require.Equal(t, "sidecar", deploy.Spec.Template.Spec.Containers[0].Name)
	require.Equal(t, "bfdd", deploy.Spec.Template.Spec.Containers[1].Name)
	require.Equal(t, "unchanged", deploy.Spec.Template.Spec.InitContainers[0].Command[2])
	require.Equal(t, vegBFDInitCommand, deploy.Spec.Template.Spec.InitContainers[1].Command[2])
	require.Contains(t, deploy.Spec.Template.Spec.InitContainers[1].VolumeMounts,
		corev1.VolumeMount{Name: "bfdd-supervisor-state", MountPath: "/var/run/kube-ovn/bfdd-supervisor"})
	require.Contains(t, deploy.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "bfdd-supervisor-state",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	require.EqualValues(t, 30, *deploy.Spec.Template.Spec.TerminationGracePeriodSeconds)
	resources := corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}
	require.NoError(t, setVpcEgressGatewayWorkloadResources(deploy, resources))
	require.Empty(t, deploy.Spec.Template.Spec.Containers[0].Resources)
	require.Equal(t, resources, deploy.Spec.Template.Spec.Containers[1].Resources)

	require.Error(t, configureVpcEgressGatewayBFDWorkload(&appsv1.Deployment{}, container))
	require.Error(t, setVpcEgressGatewayWorkloadResources(&appsv1.Deployment{}, resources))
}

func TestFlattenVpcEgressGatewayNexthops(t *testing.T) {
	nextHops := flattenVpcEgressGatewayNexthops(map[string]set.Set[string]{
		"node-1": set.New("10.16.1.10", "10.16.1.11"),
		"node-2": set.New("10.16.2.10"),
	})
	require.Equal(t, set.New("10.16.1.10", "10.16.1.11", "10.16.2.10"), nextHops)
}

func TestUpdateVpcEgressGatewayPolicyNexthops(t *testing.T) {
	policy := &ovnnb.LogicalRouterPolicy{
		Nexthops:    []string{"10.16.1.10", "10.16.1.11"},
		BFDSessions: []string{"bfd-1", "bfd-2"},
	}

	changed := updateVpcEgressGatewayPolicyNexthops(policy, set.New("10.16.1.10"), set.New("bfd-1"))
	require.True(t, changed)
	require.Equal(t, set.New("10.16.1.10"), set.New(policy.Nexthops...))
	require.Equal(t, set.New("bfd-1"), set.New(policy.BFDSessions...))

	changed = updateVpcEgressGatewayPolicyNexthops(policy, set.New("10.16.1.10", "10.16.1.11"), set.New[string]())
	require.True(t, changed)
	require.Equal(t, set.New("10.16.1.10", "10.16.1.11"), set.New(policy.Nexthops...))
	require.Empty(t, policy.BFDSessions)

	require.False(t, updateVpcEgressGatewayPolicyNexthops(
		policy,
		set.New("10.16.1.11", "10.16.1.10"),
		set.New[string](),
	))
}

func TestLocalGatewayPolicyBFDSessions(t *testing.T) {
	bfdMap := map[string]string{
		"10.16.1.10": "bfd-1",
		"10.16.1.11": "bfd-2",
		"10.16.2.10": "bfd-3",
	}
	require.Equal(
		t,
		set.New("bfd-1", "bfd-2"),
		localGatewayPolicyBFDSessions(bfdMap, set.New("10.16.1.10", "10.16.1.11")),
	)
	require.Empty(t, localGatewayPolicyBFDSessions(nil, set.New("10.16.1.10")))
}

type vegOVNRouteFixture struct {
	fc                               *fakeController
	gw                               *kubeovnv1.VpcEgressGateway
	nodeName, lrName, lrpName, bfdIP string
	externalIDs                      map[string]string
	pgName, asName                   string
	localPolicies                    []*ovnnb.LogicalRouterPolicy
	clusterPolicies                  []*ovnnb.LogicalRouterPolicy
	dropPolicies                     []*ovnnb.LogicalRouterPolicy
	allPolicies                      []*ovnnb.LogicalRouterPolicy
}

func newVegOVNRouteFixture(t *testing.T) *vegOVNRouteFixture {
	const nodeName = "node-1"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: []*corev1.Node{{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Annotations: map[string]string{util.PortNameAnnotation: "node-node-1"},
		},
	}}})
	require.NoError(t, err)

	f := &vegOVNRouteFixture{
		fc:       fc,
		nodeName: nodeName,
		lrName:   "vpc-router",
		lrpName:  "bfd-lrp",
		bfdIP:    "10.0.0.1",
		gw: &kubeovnv1.VpcEgressGateway{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "veg"},
			Spec: kubeovnv1.VpcEgressGatewaySpec{
				TrafficPolicy: kubeovnv1.TrafficPolicyLocal,
				BFD: kubeovnv1.VpcEgressGatewayBFDConfig{
					Enabled:    true,
					MinTX:      100,
					MinRX:      200,
					Multiplier: 3,
				},
			},
		},
		externalIDs: map[string]string{
			ovs.ExternalIDVendor:           util.CniTypeName,
			ovs.ExternalIDVpcEgressGateway: "default/veg",
			"af":                           "4",
		},
		pgName: vegPortGroupName("default/veg"),
		asName: vegAddressSetName("default/veg", 4),
	}
	localPgName := "node.node.1"
	localMatches := []string{
		fmt.Sprintf("ip4.src == $%s_ip4 && ip4.src == $%s_ip4", localPgName, f.pgName),
		fmt.Sprintf("ip4.src == $%s_ip4 && ip4.src == $%s", localPgName, f.asName),
	}
	clusterMatches := []string{
		fmt.Sprintf("ip4.src == $%s_ip4", f.pgName),
		fmt.Sprintf("ip4.src == $%s", f.asName),
	}
	f.localPolicies = []*ovnnb.LogicalRouterPolicy{
		{UUID: "local-1", Match: localMatches[0], Nexthops: []string{"10.16.1.10", "10.16.1.11"}, BFDSessions: []string{"bfd-1", "bfd-2"}},
		{UUID: "local-2", Match: localMatches[1], Nexthops: []string{"10.16.1.10", "10.16.1.11"}, BFDSessions: []string{"bfd-1", "bfd-2"}},
	}
	f.clusterPolicies = []*ovnnb.LogicalRouterPolicy{
		{UUID: "cluster-1", Match: clusterMatches[0], Nexthops: []string{"10.16.1.10", "10.16.1.11"}, BFDSessions: []string{"bfd-1", "bfd-2"}},
		{UUID: "cluster-2", Match: clusterMatches[1], Nexthops: []string{"10.16.1.10", "10.16.1.11"}, BFDSessions: []string{"bfd-1", "bfd-2"}},
	}
	f.dropPolicies = []*ovnnb.LogicalRouterPolicy{
		{UUID: "drop-1", Match: clusterMatches[0]},
		{UUID: "drop-2", Match: clusterMatches[1]},
	}
	f.allPolicies = append(append([]*ovnnb.LogicalRouterPolicy{}, f.localPolicies...), f.clusterPolicies...)
	return f
}

func (f *vegOVNRouteFixture) expectResources() {
	f.fc.mockOvnClient.EXPECT().CreatePortGroup(f.pgName, f.externalIDs).Return(nil)
	f.fc.mockOvnClient.EXPECT().PortGroupSetPorts(f.pgName, gomock.Any()).Return(nil)
	f.fc.mockOvnClient.EXPECT().CreateAddressSet(f.asName, f.externalIDs).Return(nil)
	f.fc.mockOvnClient.EXPECT().AddressSetUpdateAddress(f.asName).Return(nil)
}

func (f *vegOVNRouteFixture) expectPolicyUpdates(bfdEnabled bool) {
	f.fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(f.lrName, util.EgressGatewayLocalPolicyPriority, f.externalIDs, false).Return(f.localPolicies, nil)
	f.fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(f.lrName, util.EgressGatewayPolicyPriority, f.externalIDs, false).Return(f.clusterPolicies, nil)
	f.fc.mockOvnClient.EXPECT().UpdateLogicalRouterPolicy(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(4)
	if bfdEnabled {
		f.fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(f.lrName, util.EgressGatewayDropPolicyPriority, f.externalIDs, false).Return(f.dropPolicies, nil)
	} else {
		f.fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(f.lrName, util.EgressGatewayDropPolicyPriority, f.externalIDs).Return(nil)
	}
}

func (f *vegOVNRouteFixture) requirePolicyState(t *testing.T, nextHops, bfdSessions set.Set[string]) {
	for _, policy := range f.allPolicies {
		require.Equal(t, nextHops, set.New(policy.Nexthops...))
		require.Equal(t, bfdSessions, set.New(policy.BFDSessions...))
	}
}

func TestReconcileVpcEgressGatewayOVNRoutesDeletesRoutesWithoutNexthops(t *testing.T) {
	f := newVegOVNRouteFixture(t)
	for _, policy := range f.allPolicies {
		policy.Nexthops = []string{"10.16.1.10"}
		policy.BFDSessions = []string{"bfd-1"}
	}

	f.expectResources()
	f.fc.mockOvnClient.EXPECT().FindBFD(f.externalIDs).Return([]ovnnb.BFD{{
		UUID: "bfd-1", DstIP: "10.16.1.10", LogicalPort: f.lrpName,
	}}, nil)
	f.fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(f.lrName, util.EgressGatewayLocalPolicyPriority, f.externalIDs, false).
		Return(f.localPolicies, nil)
	for _, policy := range f.localPolicies {
		f.fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID(f.lrName, policy.UUID).Return(nil)
	}
	f.fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(f.lrName, util.EgressGatewayPolicyPriority, f.externalIDs, false).
		Return(f.clusterPolicies, nil)
	for _, policy := range f.clusterPolicies {
		f.fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID(f.lrName, policy.UUID).Return(nil)
	}
	f.fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(f.lrName, util.EgressGatewayDropPolicyPriority, f.externalIDs, false).
		Return(f.dropPolicies, nil)
	f.fc.mockOvnClient.EXPECT().DeleteBFD("bfd-1").Return(nil)

	err := f.fc.fakeController.reconcileVpcEgressGatewayOVNRoutes(f.gw, 4, f.lrName, f.lrpName, f.bfdIP, nil, nil)
	require.NoError(t, err)
}

func TestReconcileVpcEgressGatewayOVNRoutesConvergesLocalBFDNexthops(t *testing.T) {
	f := newVegOVNRouteFixture(t)

	// Converge from two nexthops to one and delete the stale BFD row.
	f.expectResources()
	f.fc.mockOvnClient.EXPECT().FindBFD(f.externalIDs).Return([]ovnnb.BFD{
		{UUID: "bfd-1", DstIP: "10.16.1.10", LogicalPort: f.lrpName},
		{UUID: "bfd-2", DstIP: "10.16.1.11", LogicalPort: f.lrpName},
	}, nil)
	f.expectPolicyUpdates(true)
	f.fc.mockOvnClient.EXPECT().DeleteBFD("bfd-2").Return(nil)
	err := f.fc.fakeController.reconcileVpcEgressGatewayOVNRoutes(f.gw, 4, f.lrName, f.lrpName, f.bfdIP, map[string]set.Set[string]{
		f.nodeName: set.New("10.16.1.10"),
	}, nil)
	require.NoError(t, err)
	f.requirePolicyState(t, set.New("10.16.1.10"), set.New("bfd-1"))

	// Restore the second nexthop and its BFD row.
	f.expectResources()
	f.fc.mockOvnClient.EXPECT().FindBFD(f.externalIDs).Return([]ovnnb.BFD{
		{UUID: "bfd-1", DstIP: "10.16.1.10", LogicalPort: f.lrpName},
	}, nil)
	f.fc.mockOvnClient.EXPECT().CreateBFD(f.lrpName, "10.16.1.11", 100, 200, 3, f.externalIDs).
		Return(&ovnnb.BFD{UUID: "bfd-2", DstIP: "10.16.1.11"}, nil)
	f.expectPolicyUpdates(true)
	err = f.fc.fakeController.reconcileVpcEgressGatewayOVNRoutes(f.gw, 4, f.lrName, f.lrpName, f.bfdIP, map[string]set.Set[string]{
		f.nodeName: set.New("10.16.1.10", "10.16.1.11"),
	}, nil)
	require.NoError(t, err)
	f.requirePolicyState(t, set.New("10.16.1.10", "10.16.1.11"), set.New("bfd-1", "bfd-2"))

	// Disabling BFD keeps ECMP nexthops and removes policy sessions and BFD rows.
	f.gw.Spec.BFD.Enabled = false
	f.expectResources()
	f.fc.mockOvnClient.EXPECT().FindBFD(f.externalIDs).Return([]ovnnb.BFD{
		{UUID: "bfd-1", DstIP: "10.16.1.10", LogicalPort: f.lrpName},
		{UUID: "bfd-2", DstIP: "10.16.1.11", LogicalPort: f.lrpName},
	}, nil)
	f.expectPolicyUpdates(false)
	f.fc.mockOvnClient.EXPECT().DeleteBFD("bfd-1").Return(nil)
	f.fc.mockOvnClient.EXPECT().DeleteBFD("bfd-2").Return(nil)
	err = f.fc.fakeController.reconcileVpcEgressGatewayOVNRoutes(f.gw, 4, f.lrName, f.lrpName, "", map[string]set.Set[string]{
		f.nodeName: set.New("10.16.1.10", "10.16.1.11"),
	}, nil)
	require.NoError(t, err)
	f.requirePolicyState(t, set.New("10.16.1.10", "10.16.1.11"), set.New[string]())
}

func newVegWorkloadPod(name, node, podIP, attachment string) *corev1.Pod {
	annotations := map[string]string{}
	if attachment != "" {
		annotations[nadv1.NetworkStatusAnnot] = attachment
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: node,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIPs: []corev1.PodIP{{
				IP: podIP,
			}},
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func TestCollectVpcEgressGatewayWorkloadStatus(t *testing.T) {
	attachmentNetwork := "default/eth1"
	readyAttachment := `[{"name":"default/eth1","ips":["172.17.1.10"]}]`
	sameNodePod1 := newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment)
	sameNodePod1.Status.PodIPs = append(sameNodePod1.Status.PodIPs, corev1.PodIP{IP: "fd00:10::10"})
	sameNodePod2 := newVegWorkloadPod("veg-2", "node-1", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`)
	sameNodePod2.Status.PodIPs = append(sameNodePod2.Status.PodIPs, corev1.PodIP{IP: "fd00:10::11"})

	tests := []struct {
		name                string
		pods                []*corev1.Pod
		wantInternalIPs     []string
		wantExternalIPs     []string
		wantNodes           []string
		wantNodeNexthopIPv4 map[string]set.Set[string]
		wantNodeNexthopIPv6 map[string]set.Set[string]
		wantNotReadyCount   int
	}{
		{
			name: "all workload pods have attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`),
			},
			wantInternalIPs:     []string{"10.16.1.10", "10.16.1.11"},
			wantExternalIPs:     []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:           []string{"node-1", "node-2"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10"), "node-2": set.New("10.16.1.11")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{},
		},
		{
			name:                "workload pods on the same node",
			pods:                []*corev1.Pod{sameNodePod1, sameNodePod2},
			wantInternalIPs:     []string{"10.16.1.10,fd00:10::10", "10.16.1.11,fd00:10::11"},
			wantExternalIPs:     []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:           []string{"node-1"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10", "10.16.1.11")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{"node-1": set.New("fd00:10::10", "fd00:10::11")},
		},
		{
			name: "one workload pod misses attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"kube-ovn","ips":["10.16.1.11"]}]`),
			},
			wantInternalIPs:     []string{"10.16.1.10"},
			wantExternalIPs:     []string{"172.17.1.10"},
			wantNodes:           []string{"node-1"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{},
			wantNotReadyCount:   2,
		},
		{
			name: "one workload pod has attachment network without ip",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":[]}]`),
			},
			wantInternalIPs:     []string{"10.16.1.10"},
			wantExternalIPs:     []string{"172.17.1.10"},
			wantNodes:           []string{"node-1"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{},
			wantNotReadyCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcEgressGateway{
				Spec: kubeovnv1.VpcEgressGatewaySpec{
					Replicas: 2,
				},
			}

			nodeNexthopIPv4, nodeNexthopIPv6, messages := collectVpcEgressGatewayWorkloadStatus(gw, tt.pods, attachmentNetwork)

			require.Equal(t, tt.wantInternalIPs, gw.Status.InternalIPs)
			require.Equal(t, tt.wantExternalIPs, gw.Status.ExternalIPs)
			require.Equal(t, tt.wantNodes, gw.Status.Workload.Nodes)
			require.Equal(t, tt.wantNodeNexthopIPv4, nodeNexthopIPv4)
			require.Equal(t, tt.wantNodeNexthopIPv6, nodeNexthopIPv6)
			require.Len(t, messages, tt.wantNotReadyCount)
		})
	}
}

func TestCollectVpcEgressGatewayWorkloadStatusRetainsNetworkedNotReadyPod(t *testing.T) {
	const attachmentNetwork = "default/eth1"
	readyPod := newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", `[{"name":"default/eth1","ips":["172.17.1.10"]}]`)
	notReadyPod := newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`)
	notReadyPod.Status.Conditions[0].Status = corev1.ConditionFalse
	gw := &kubeovnv1.VpcEgressGateway{Spec: kubeovnv1.VpcEgressGatewaySpec{Replicas: 2}}

	ipv4, ipv6, messages := collectVpcEgressGatewayWorkloadStatus(gw, []*corev1.Pod{readyPod, notReadyPod}, attachmentNetwork)

	require.Equal(t, []string{"10.16.1.10", "10.16.1.11"}, gw.Status.InternalIPs)
	require.Equal(t, []string{"172.17.1.10", "172.17.1.11"}, gw.Status.ExternalIPs)
	require.Equal(t, []string{"node-1", "node-2"}, gw.Status.Workload.Nodes)
	require.Equal(t, map[string]set.Set[string]{"node-1": set.New("10.16.1.10"), "node-2": set.New("10.16.1.11")}, ipv4)
	require.Empty(t, ipv6)
	require.Len(t, messages, 1)
}
