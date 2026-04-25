package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnv1lister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestGenGatewayBFDDContainer(t *testing.T) {
	image := "kube-ovn/kube-ovn:v1.12.0"
	bfdIP := "10.0.1.1,fd00::1"
	minTX := int32(100)
	minRX := int32(200)
	multiplier := int32(3)

	container := genGatewayBFDDContainer(image, bfdIP, minTX, minRX, multiplier)

	assert.Equal(t, "bfdd", container.Name)
	assert.Equal(t, image, container.Image)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.Equal(t, []string{"bash", "/kube-ovn/start-bfdd.sh"}, container.Command)

	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}
	assert.Equal(t, bfdIP, envMap["BFD_PEER_IPS"])
	assert.Equal(t, "100", envMap["BFD_MIN_TX"])
	assert.Equal(t, "200", envMap["BFD_MIN_RX"])
	assert.Equal(t, "3", envMap["BFD_MULTI"])

	assert.NotNil(t, container.StartupProbe)
	assert.NotNil(t, container.LivenessProbe)
	assert.NotNil(t, container.ReadinessProbe)

	assert.Equal(t, gwBFDDResourceCPU, container.Resources.Requests[corev1.ResourceCPU])
	assert.Equal(t, gwBFDDResourceMemory, container.Resources.Requests[corev1.ResourceMemory])
	assert.Equal(t, gwResourceEphemeralStorage, container.Resources.Limits[corev1.ResourceEphemeralStorage])

	assert.False(t, *container.SecurityContext.Privileged)
	assert.Equal(t, int64(65534), *container.SecurityContext.RunAsUser)
}

func TestGenGatewaySleepContainer(t *testing.T) {
	image := "kube-ovn/kube-ovn:v1.12.0"
	container := genGatewaySleepContainer(image)

	assert.Equal(t, "gateway", container.Name)
	assert.Equal(t, image, container.Image)
	assert.Equal(t, []string{"sleep", "infinity"}, container.Command)
	assert.Equal(t, gwSleepResourceCPU, container.Resources.Requests[corev1.ResourceCPU])
	assert.Equal(t, gwSleepResourceMemory, container.Resources.Requests[corev1.ResourceMemory])
}

func TestGenGatewayPodAntiAffinity(t *testing.T) {
	labels := map[string]string{"app": "vpc-nat-gw", "vpc": "test-vpc"}
	affinity := genGatewayPodAntiAffinity(labels)

	assert.NotNil(t, affinity.PodAntiAffinity)
	terms := affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	assert.Len(t, terms, 1)
	assert.Equal(t, labels, terms[0].LabelSelector.MatchLabels)
	assert.Equal(t, corev1.LabelHostname, terms[0].TopologyKey)
}

func TestGenGatewayDeploymentStrategy(t *testing.T) {
	strategy := genGatewayDeploymentStrategy()

	assert.Equal(t, appsv1.RollingUpdateDeploymentStrategyType, strategy.Type)
	assert.NotNil(t, strategy.RollingUpdate)
	assert.Equal(t, intstr.FromInt(1), *strategy.RollingUpdate.MaxUnavailable)
	assert.Equal(t, intstr.FromInt(0), *strategy.RollingUpdate.MaxSurge)
}

func TestMergeGatewayAffinity(t *testing.T) {
	aff1 := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key:      "kubernetes.io/hostname",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"node1"},
					}},
				}},
			},
		},
	}
	aff2 := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				TopologyKey: corev1.LabelHostname,
			}},
		},
	}

	merged := mergeGatewayAffinity(aff1, aff2)
	assert.NotNil(t, merged.NodeAffinity)
	assert.NotNil(t, merged.PodAntiAffinity)
	assert.Nil(t, merged.PodAffinity)

	// Test precedence
	aff3 := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key:      "kubernetes.io/hostname",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"node2"},
					}},
				}},
			},
		},
	}
	merged2 := mergeGatewayAffinity(aff1, aff3)
	assert.Equal(t, aff3.NodeAffinity, merged2.NodeAffinity)
}

type mockOvnNbClient struct {
	ovs.NbClient
	mock.Mock
}

func (m *mockOvnNbClient) FindBFD(externalIDs map[string]string) ([]ovnnb.BFD, error) {
	args := m.Called(externalIDs)
	return args.Get(0).([]ovnnb.BFD), args.Error(1)
}

func (m *mockOvnNbClient) CreateBFD(lrp, dstIP string, minRX, minTX, detectMult int, externalIDs map[string]string) (*ovnnb.BFD, error) {
	args := m.Called(lrp, dstIP, minRX, minTX, detectMult, externalIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ovnnb.BFD), args.Error(1)
}

func (m *mockOvnNbClient) DeleteBFD(uuid string) error {
	args := m.Called(uuid)
	return args.Error(0)
}

func (m *mockOvnNbClient) DeleteLogicalRouterStaticRoute(lrName string, routeTable, policy *string, ipPrefix, nextHop string) error {
	args := m.Called(lrName, routeTable, policy, ipPrefix, nextHop)
	return args.Error(0)
}

func (m *mockOvnNbClient) ListLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string, ignoreExtIDEmptyValue bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	args := m.Called(lrName, priority, externalIDs, ignoreExtIDEmptyValue)
	return args.Get(0).([]*ovnnb.LogicalRouterPolicy), args.Error(1)
}

func (m *mockOvnNbClient) UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error {
	args := m.Called(policy, fields)
	return args.Error(0)
}

func (m *mockOvnNbClient) DeleteLogicalRouterPolicyByUUID(lrName, uuid string) error {
	args := m.Called(lrName, uuid)
	return args.Error(0)
}

func (m *mockOvnNbClient) AddLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops, bfdSessions []string, externalIDs map[string]string) error {
	args := m.Called(lrName, priority, match, action, nextHops, bfdSessions, externalIDs)
	return args.Error(0)
}

func (m *mockOvnNbClient) DeleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error {
	args := m.Called(lrName, priority, externalIDs)
	return args.Error(0)
}

func (m *mockOvnNbClient) ListLogicalRouterStaticRoutes(lrName string, routeTable, policy *string, ipPrefix string, externalIDs map[string]string) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	args := m.Called(lrName, routeTable, policy, ipPrefix, externalIDs)
	return args.Get(0).([]*ovnnb.LogicalRouterStaticRoute), args.Error(1)
}

func (m *mockOvnNbClient) AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix string, bfdID *string, externalIDs map[string]string, nexthops ...string) error {
	args := m.Called(lrName, routeTable, policy, ipPrefix, bfdID, externalIDs, nexthops)
	return args.Error(0)
}

func (m *mockOvnNbClient) DeleteLogicalRouterStaticRouteByExternalIDs(lrName string, externalIDs map[string]string) error {
	args := m.Called(lrName, externalIDs)
	return args.Error(0)
}

func TestReconcileGatewayBFD(t *testing.T) {
	lrpName := "test-lrp"
	externalIDs := map[string]string{"vpc": "test-vpc"}
	minTX, minRX, multiplier := int32(100), int32(200), int32(3)
	bfdIP := "10.0.1.1"

	t.Run("no existing BFD sessions, create new", func(t *testing.T) {
		m := new(mockOvnNbClient)
		nextHops := map[string]string{"node1": "10.0.1.10"}

		m.On("FindBFD", externalIDs).Return([]ovnnb.BFD{}, nil)
		m.On("CreateBFD", lrpName, "10.0.1.10", int(minTX), int(minRX), int(multiplier), externalIDs).Return(&ovnnb.BFD{UUID: "uuid-1", DstIP: "10.0.1.10"}, nil)

		bfdIDs, bfdMap, staleBFDIDs, err := reconcileGatewayBFD(m, bfdIP, lrpName, nextHops, minTX, minRX, multiplier, externalIDs)

		assert.NoError(t, err)
		assert.True(t, bfdIDs.Equal(set.New("uuid-1")))
		assert.Equal(t, map[string]string{"10.0.1.10": "uuid-1"}, bfdMap)
		assert.Equal(t, 0, staleBFDIDs.Len())
		m.AssertExpectations(t)
	})

	t.Run("existing valid and stale BFD sessions", func(t *testing.T) {
		m := new(mockOvnNbClient)
		nextHops := map[string]string{"node1": "10.0.1.10"}
		existingBFDs := []ovnnb.BFD{
			{UUID: "uuid-valid", DstIP: "10.0.1.10", LogicalPort: lrpName},
			{UUID: "uuid-stale-ip", DstIP: "10.0.1.11", LogicalPort: lrpName},
			{UUID: "uuid-stale-port", DstIP: "10.0.1.10", LogicalPort: "other-port"},
		}

		m.On("FindBFD", externalIDs).Return(existingBFDs, nil)

		bfdIDs, bfdMap, staleBFDIDs, err := reconcileGatewayBFD(m, bfdIP, lrpName, nextHops, minTX, minRX, multiplier, externalIDs)

		assert.NoError(t, err)
		assert.True(t, bfdIDs.Equal(set.New("uuid-valid")))
		assert.Equal(t, map[string]string{"10.0.1.10": "uuid-valid"}, bfdMap)
		assert.True(t, staleBFDIDs.Equal(set.New("uuid-stale-ip", "uuid-stale-port")))
		m.AssertExpectations(t)
	})

	t.Run("bfdIP is empty, disable BFD", func(t *testing.T) {
		m := new(mockOvnNbClient)
		nextHops := map[string]string{"node1": "10.0.1.10"}
		existingBFDs := []ovnnb.BFD{
			{UUID: "uuid-any", DstIP: "10.0.1.10", LogicalPort: lrpName},
		}

		m.On("FindBFD", externalIDs).Return(existingBFDs, nil)

		bfdIDs, bfdMap, staleBFDIDs, err := reconcileGatewayBFD(m, "", lrpName, nextHops, minTX, minRX, multiplier, externalIDs)

		assert.NoError(t, err)
		assert.Equal(t, 0, bfdIDs.Len())
		assert.Equal(t, 0, len(bfdMap))
		assert.True(t, staleBFDIDs.Equal(set.New("uuid-any")))
		m.AssertExpectations(t)
	})

	t.Run("FindBFD error", func(t *testing.T) {
		m := new(mockOvnNbClient)
		m.On("FindBFD", externalIDs).Return([]ovnnb.BFD{}, errors.New("find error"))

		_, _, _, err := reconcileGatewayBFD(m, bfdIP, lrpName, nil, minTX, minRX, multiplier, externalIDs)
		assert.Error(t, err)
	})
}

func TestCleanupStaleBFD(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := new(mockOvnNbClient)
		staleIDs := set.New("uuid-1", "uuid-2")

		m.On("DeleteBFD", "uuid-1").Return(nil)
		m.On("DeleteBFD", "uuid-2").Return(nil)

		err := cleanupStaleBFD(m, staleIDs)
		assert.NoError(t, err)
		m.AssertExpectations(t)
	})

	t.Run("delete error", func(t *testing.T) {
		m := new(mockOvnNbClient)
		staleIDs := set.New("uuid-1")
		m.On("DeleteBFD", "uuid-1").Return(errors.New("delete error"))

		err := cleanupStaleBFD(m, staleIDs)
		assert.Error(t, err)
	})
}

func TestReconcileGatewayRoutes(t *testing.T) {
	m := new(mockOvnNbClient)
	gwName := "test-gw"
	lrName := "test-lr"
	bfdEnabled := true
	bfdIP := "10.0.1.1"
	bfdIDs := set.New("bfd-uuid-1")
	bfdMap := map[string]string{"10.0.1.10": "bfd-uuid-1"}
	internalCIDRs := []string{"10.0.1.0/24"}
	nextHops := map[string]string{"node1": "10.0.1.10"}
	externalIDs := map[string]string{"vendor": "kube-ovn"}

	policySrcIP := ovnnb.LogicalRouterStaticRoutePolicySrcIP
	m.On("AddLogicalRouterStaticRoute", lrName, "", policySrcIP, "10.0.1.0/24", mock.Anything, mock.Anything, []string{"10.0.1.10"}).Return(nil)

	existingRoutes := []*ovnnb.LogicalRouterStaticRoute{
		{IPPrefix: "10.0.1.0/24", RouteTable: "", Policy: &policySrcIP},
		{IPPrefix: "10.0.2.0/24", RouteTable: "", Policy: &policySrcIP}, // Stale
	}
	m.On("ListLogicalRouterStaticRoutes", lrName, (*string)(nil), (*string)(nil), "", externalIDs).Return(existingRoutes, nil)
	m.On("DeleteLogicalRouterStaticRoute", lrName, mock.Anything, mock.Anything, "10.0.2.0/24", mock.Anything).Return(nil)

	err := reconcileGatewayRoutes(m, gwName, lrName, bfdEnabled, bfdIP, bfdIDs, bfdMap, internalCIDRs, nextHops, externalIDs)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestGetWorkloadNodes(t *testing.T) {
	t.Parallel()

	namespace := "test-ns"
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test-app"},
	}

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: namespace,
				Labels:    map[string]string{"app": "test-app"},
			},
			Spec: corev1.PodSpec{
				NodeName: "node1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: namespace,
				Labels:    map[string]string{"app": "test-app"},
			},
			Spec: corev1.PodSpec{
				NodeName: "node2",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: namespace,
				Labels:    map[string]string{"app": "other-app"},
			},
			Spec: corev1.PodSpec{
				NodeName: "node3",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod4",
				Namespace: namespace,
				Labels:    map[string]string{"app": "test-app"},
			},
			Spec: corev1.PodSpec{
				NodeName: "", // No node assigned
			},
		},
	}

	client := fake.NewSimpleClientset()
	for _, pod := range pods {
		_, err := client.CoreV1().Pods(namespace).Create(context.Background(), &pod, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podLister := informerFactory.Core().V1().Pods().Lister()

	// Fill the cache
	_ = informerFactory.Core().V1().Pods().Informer()
	stopCh := make(chan struct{})
	defer close(stopCh)
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	nodes, err := getWorkloadNodes(podLister, namespace, selector)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"node1", "node2"}, nodes)
}

func TestUpdateNatGwWorkloadStatus(t *testing.T) {
	namespace := "test-ns"
	gwName := "test-gw"
	workloadName := util.GenNatGwName(gwName)

	t.Run("HA mode (Deployment)", func(t *testing.T) {
		gw := &kubeovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: gwName},
			Spec:       kubeovnv1.VpcNatGatewaySpec{Replicas: 2},
		}

		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workloadName,
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": workloadName},
				},
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-1",
				Namespace: namespace,
				Labels:    map[string]string{"app": workloadName},
			},
			Spec: corev1.PodSpec{NodeName: "node-1"},
		}

		client := fake.NewSimpleClientset(deploy, pod)
		informerFactory := informers.NewSharedInformerFactory(client, 0)
		podLister := informerFactory.Core().V1().Pods().Lister()
		deployLister := informerFactory.Apps().V1().Deployments().Lister()

		// Start informers and wait for sync
		stopCh := make(chan struct{})
		defer close(stopCh)
		informerFactory.Start(stopCh)
		informerFactory.WaitForCacheSync(stopCh)

		changed := updateNatGwWorkloadStatus(gw, podLister, deployLister, client, namespace)
		assert.True(t, changed)
		assert.Equal(t, util.KindDeployment, gw.Status.Workload.Kind)
		assert.Equal(t, "apps/v1", gw.Status.Workload.APIVersion)
		assert.Equal(t, workloadName, gw.Status.Workload.Name)
		assert.Equal(t, []string{"node-1"}, gw.Status.Workload.Nodes)

		// Test no change
		changed = updateNatGwWorkloadStatus(gw, podLister, deployLister, client, namespace)
		assert.False(t, changed)
	})

	t.Run("Legacy mode (StatefulSet)", func(t *testing.T) {
		gw := &kubeovnv1.VpcNatGateway{
			ObjectMeta: metav1.ObjectMeta{Name: gwName},
			Spec:       kubeovnv1.VpcNatGatewaySpec{Replicas: 1},
		}

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workloadName,
				Namespace: namespace,
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": workloadName},
				},
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workloadName + "-0",
				Namespace: namespace,
				Labels:    map[string]string{"app": workloadName},
			},
			Spec: corev1.PodSpec{NodeName: "node-1"},
		}

		client := fake.NewSimpleClientset(sts, pod)
		informerFactory := informers.NewSharedInformerFactory(client, 0)
		podLister := informerFactory.Core().V1().Pods().Lister()
		deployLister := informerFactory.Apps().V1().Deployments().Lister()

		// Start informers and wait for sync
		stopCh := make(chan struct{})
		defer close(stopCh)
		informerFactory.Start(stopCh)
		informerFactory.WaitForCacheSync(stopCh)

		changed := updateNatGwWorkloadStatus(gw, podLister, deployLister, client, namespace)
		assert.True(t, changed)
		assert.Equal(t, util.KindStatefulSet, gw.Status.Workload.Kind)
		assert.Equal(t, "apps/v1", gw.Status.Workload.APIVersion)
		assert.Equal(t, workloadName, gw.Status.Workload.Name)
		assert.Equal(t, []string{"node-1"}, gw.Status.Workload.Nodes)
	})
}

func TestResolveInternalCIDRs(t *testing.T) {
	subnets := []*kubeovnv1.Subnet{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "subnet1"},
			Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.1.0/24"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "subnet2"},
			Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.2.0/24,fd00:10:16::/64"},
		},
	}

	fakeSubnetLister := &mockSubnetLister{subnets: subnets}

	t.Run("resolve subnets and direct CIDRs", func(t *testing.T) {
		subnetNames := []string{"subnet1", "subnet2", "non-existent"}
		directCIDRs := []string{"192.168.1.0/24"}

		result := resolveInternalCIDRs(fakeSubnetLister, subnetNames, directCIDRs)

		expected := []string{"10.0.1.0/24", "10.0.2.0/24", "fd00:10:16::/64", "192.168.1.0/24"}
		assert.ElementsMatch(t, expected, result)
	})
}

type mockSubnetLister struct {
	kubeovnv1lister.SubnetLister
	subnets []*kubeovnv1.Subnet
}

func (m *mockSubnetLister) List(selector labels.Selector) (ret []*kubeovnv1.Subnet, err error) {
	return m.subnets, nil
}

func (m *mockSubnetLister) Get(name string) (*kubeovnv1.Subnet, error) {
	for _, s := range m.subnets {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, errors.New("not found")
}

func TestReconcileNatGatewayPolicies(t *testing.T) {
	m := new(mockOvnNbClient)

	gwName := "test-gw"
	lrName := "test-lr"
	af := 4
	externalIDs := map[string]string{"vendor": "kube-ovn", "af": "4"}
	internalCIDRs := []string{"10.0.1.0/24"}
	nextHops := map[string]string{"node1": "10.0.1.10"}
	bfdIDs := set.New("bfd-uuid-1")

	t.Run("create new policy", func(t *testing.T) {
		m.On("ListLogicalRouterPolicies", lrName, util.NatGatewayPolicyPriority, externalIDs, false).Return([]*ovnnb.LogicalRouterPolicy{}, nil).Once()
		m.On("AddLogicalRouterPolicy", lrName, util.NatGatewayPolicyPriority, "ip4.src == 10.0.1.0/24", ovnnb.LogicalRouterPolicyActionReroute, mock.Anything, []string{"bfd-uuid-1"}, externalIDs).Return(nil).Once()
		m.On("DeleteLogicalRouterPolicies", lrName, util.NatGatewayDropPolicyPriority, externalIDs).Return(nil).Once()

		err := reconcileNatGatewayPolicies(m, gwName, lrName, af, false, bfdIDs, internalCIDRs, nextHops, externalIDs)
		assert.NoError(t, err)
		m.AssertExpectations(t)
	})

	t.Run("update existing policy", func(t *testing.T) {
		existing := []*ovnnb.LogicalRouterPolicy{
			{
				UUID:        "policy-uuid",
				Priority:    util.NatGatewayPolicyPriority,
				Match:       "ip4.src == 10.0.1.0/24",
				Action:      ovnnb.LogicalRouterPolicyActionReroute,
				Nexthops:    []string{"10.0.1.11"}, // Old nexthop
				BFDSessions: []string{"old-bfd"},
			},
		}
		m.On("ListLogicalRouterPolicies", lrName, util.NatGatewayPolicyPriority, externalIDs, false).Return(existing, nil).Once()
		m.On("UpdateLogicalRouterPolicy", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
		m.On("DeleteLogicalRouterPolicies", lrName, util.NatGatewayDropPolicyPriority, externalIDs).Return(nil).Once()

		err := reconcileNatGatewayPolicies(m, gwName, lrName, af, false, bfdIDs, internalCIDRs, nextHops, externalIDs)
		assert.NoError(t, err)
		m.AssertExpectations(t)
	})

	t.Run("cleanup stale policy", func(t *testing.T) {
		existing := []*ovnnb.LogicalRouterPolicy{
			{
				UUID:     "stale-uuid",
				Priority: util.NatGatewayPolicyPriority,
				Match:    "ip4.src == 10.0.2.0/24",
			},
		}
		m.On("ListLogicalRouterPolicies", lrName, util.NatGatewayPolicyPriority, externalIDs, false).Return(existing, nil).Once()
		m.On("DeleteLogicalRouterPolicyByUUID", lrName, "stale-uuid").Return(nil).Once()
		m.On("AddLogicalRouterPolicy", lrName, util.NatGatewayPolicyPriority, "ip4.src == 10.0.1.0/24", ovnnb.LogicalRouterPolicyActionReroute, mock.Anything, []string{"bfd-uuid-1"}, externalIDs).Return(nil).Once()
		m.On("DeleteLogicalRouterPolicies", lrName, util.NatGatewayDropPolicyPriority, externalIDs).Return(nil).Once()

		err := reconcileNatGatewayPolicies(m, gwName, lrName, af, false, bfdIDs, internalCIDRs, nextHops, externalIDs)
		assert.NoError(t, err)
		m.AssertExpectations(t)
	})
}
