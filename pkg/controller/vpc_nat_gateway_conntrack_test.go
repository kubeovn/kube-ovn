package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// newConntrackSyncTestController builds a controller whose subnets and VPC are the ones the
// generated gateway refers to, so that genNatGwDeployment can resolve them.
func newConntrackSyncTestController(t *testing.T) *Controller {
	t.Helper()

	subnets := []*kubeovnv1.Subnet{
		{
			Name: "internal-subnet",
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "10.0.0.0/24",
				Gateway:   "10.0.0.1",
				Provider:  "internal.default.ovn",
			},
		},
		{
			Name: "external-subnet",
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "192.168.0.0/24",
				Gateway:   "192.168.0.1",
				Provider:  "external.default.ovn",
			},
		},
	}
	vpcs := []*kubeovnv1.Vpc{
		{
			Name:   "test-vpc",
			Status: kubeovnv1.VpcStatus{Subnets: []string{"internal-subnet"}},
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: subnets,
		Vpcs:    vpcs,
	})
	require.NoError(t, err)
	return fakeController.fakeController
}

func newConntrackSyncTestGateway(replicas int32, conntrackSync kubeovnv1.VpcNatGatewayConntrackSyncConfig) *kubeovnv1.VpcNatGateway {
	return &kubeovnv1.VpcNatGateway{
		Name: "test-gw",
		UID:  "0a3b2f06-5f6e-4f5c-8f60-9f5f3a2b1c4d",
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Vpc:             "test-vpc",
			Subnet:          "internal-subnet",
			ExternalSubnets: []string{"external-subnet"},
			Replicas:        replicas,
			ConntrackSync:   conntrackSync,
		},
	}
}

func conntrackdContainer(t *testing.T, containers []corev1.Container) *corev1.Container {
	t.Helper()
	for i, container := range containers {
		if container.Name == "conntrackd" {
			return &containers[i]
		}
	}
	return nil
}

func TestGenNatGwDeploymentConntrackSync(t *testing.T) {
	t.Run("absent when synchronization is disabled", func(t *testing.T) {
		controller := newConntrackSyncTestController(t)
		gw := newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{})

		deploy, err := controller.genNatGwDeployment(gw)
		require.NoError(t, err)
		assert.Nil(t, conntrackdContainer(t, deploy.Spec.Template.Spec.Containers))
	})

	t.Run("group derived from the gateway uid", func(t *testing.T) {
		controller := newConntrackSyncTestController(t)
		gw := newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})

		deploy, err := controller.genNatGwDeployment(gw)
		require.NoError(t, err)
		container := conntrackdContainer(t, deploy.Spec.Template.Spec.Containers)
		require.NotNil(t, container)

		envMap := make(map[string]string, len(container.Env))
		for _, env := range container.Env {
			envMap[env.Name] = env.Value
		}
		assert.Equal(t, util.GenNatGwConntrackSyncGroup(string(gw.UID)), envMap["CONNTRACK_SYNC_GROUP"])
		assert.Equal(t, "3780", envMap["CONNTRACK_SYNC_PORT"])
		assert.Equal(t, vpcNatImage, container.Image)
	})

	t.Run("configured group and port are used", func(t *testing.T) {
		controller := newConntrackSyncTestController(t)
		gw := newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.9.9.9",
			Port:           4000,
		})

		deploy, err := controller.genNatGwDeployment(gw)
		require.NoError(t, err)
		container := conntrackdContainer(t, deploy.Spec.Template.Spec.Containers)
		require.NotNil(t, container)

		envMap := make(map[string]string, len(container.Env))
		for _, env := range container.Env {
			envMap[env.Name] = env.Value
		}
		assert.Equal(t, "239.9.9.9", envMap["CONNTRACK_SYNC_GROUP"])
		assert.Equal(t, "4000", envMap["CONNTRACK_SYNC_PORT"])
	})

	t.Run("enabling synchronization changes the workload hash", func(t *testing.T) {
		controller := newConntrackSyncTestController(t)

		disabled, err := controller.genNatGwDeployment(newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{}))
		require.NoError(t, err)
		enabled, err := controller.genNatGwDeployment(newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true}))
		require.NoError(t, err)

		assert.NotEqual(t, disabled.Annotations[util.GenerateHashAnnotation], enabled.Annotations[util.GenerateHashAnnotation],
			"the Pods have to be recreated to gain the conntrackd container")
	})
}

func TestNatGwConntrackSyncStatus(t *testing.T) {
	readyPod := func(name string, ready bool) *corev1.Pod {
		return &corev1.Pod{
			Name:      name,
			Namespace: "kube-system",
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "vpc-nat-gw", Ready: true},
					{Name: "conntrackd", Ready: ready},
				},
			},
		}
	}

	t.Run("reported empty when disabled", func(t *testing.T) {
		gw := newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{})
		status := natGwConntrackSyncStatus(gw, []*corev1.Pod{readyPod("gw-0", true), readyPod("gw-1", true)})
		assert.Equal(t, kubeovnv1.VpcNatGatewayConntrackSyncStatus{}, status)
	})

	t.Run("ignored without HA", func(t *testing.T) {
		gw := newConntrackSyncTestGateway(1, kubeovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		status := natGwConntrackSyncStatus(gw, []*corev1.Pod{readyPod("gw-0", true)})
		assert.Equal(t, kubeovnv1.VpcNatGatewayConntrackSyncStatus{}, status)
	})

	t.Run("ready once every replica synchronizes", func(t *testing.T) {
		gw := newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		status := natGwConntrackSyncStatus(gw, []*corev1.Pod{readyPod("gw-0", true), readyPod("gw-1", true)})
		assert.Equal(t, kubeovnv1.VpcNatGatewayConntrackSyncStatus{
			Enabled:        true,
			MulticastGroup: util.GenNatGwConntrackSyncGroup(string(gw.UID)),
			Port:           util.NatGwConntrackSyncDefaultPort,
			Ready:          true,
		}, status)
	})

	t.Run("not ready while a replica lags behind", func(t *testing.T) {
		gw := newConntrackSyncTestGateway(2, kubeovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})

		// One daemon is not up yet: that replica still keeps its own NAT state.
		status := natGwConntrackSyncStatus(gw, []*corev1.Pod{readyPod("gw-0", true), readyPod("gw-1", false)})
		assert.False(t, status.Ready)

		// A replica is missing altogether, e.g. while its Pod is being replaced.
		status = natGwConntrackSyncStatus(gw, []*corev1.Pod{readyPod("gw-0", true)})
		assert.False(t, status.Ready)

		// A terminating Pod cannot be counted as synchronized either.
		terminating := readyPod("gw-1", true)
		terminating.DeletionTimestamp = new(metav1.Now())
		status = natGwConntrackSyncStatus(gw, []*corev1.Pod{readyPod("gw-0", true), terminating})
		assert.False(t, status.Ready)
	})
}
