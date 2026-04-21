package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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

func (m *mockOvnNbClient) ListLogicalRouterPolicies(lr string, priority int, externalIDs map[string]string, ignoreNotFound bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	args := m.Called(lr, priority, externalIDs, ignoreNotFound)
	return args.Get(0).([]*ovnnb.LogicalRouterPolicy), args.Error(1)
}

func (m *mockOvnNbClient) AddLogicalRouterPolicy(lr string, priority int, match, action string, nexthops, bfdSessions []string, externalIDs map[string]string) error {
	args := m.Called(lr, priority, match, action, nexthops, bfdSessions, externalIDs)
	return args.Error(0)
}

func (m *mockOvnNbClient) UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error {
	args := m.Called(policy, fields)
	return args.Error(0)
}

func (m *mockOvnNbClient) DeleteLogicalRouterPolicyByUUID(lr, uuid string) error {
	args := m.Called(lr, uuid)
	return args.Error(0)
}

func (m *mockOvnNbClient) DeleteLogicalRouterPolicies(lr string, priority int, externalIDs map[string]string) error {
	args := m.Called(lr, priority, externalIDs)
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
		// UnsortedList makes order non-deterministic, but mock handles it if we don't care about order
		// or if we use UnsortedList to set up expectations.
		// For simplicity, we just check if expectations are met.
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
