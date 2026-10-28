package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// newConntrackSyncCache returns a cache holding what ValidateVpcNatGW resolves: the NAT gateway
// configuration, the VPC and the subnet of the gateway, plus the gateways to check against.
func newConntrackSyncCache(gateways ...ovnv1.VpcNatGateway) *mockCache {
	return &mockCache{
		objects: map[string]runtime.Object{
			"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
				Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
				Data: map[string]string{"image": "test-image"},
			},
			"/test-vpc": &ovnv1.Vpc{Name: "test-vpc"},
			"/test-subnet": &ovnv1.Subnet{
				Name: "test-subnet",
				Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
			},
		},
		natGateways: gateways,
	}
}

func newConntrackSyncGateway(name string, replicas int32, conntrackSync ovnv1.VpcNatGatewayConntrackSyncConfig) ovnv1.VpcNatGateway {
	return ovnv1.VpcNatGateway{
		Name: name,
		UID:  types.UID("uid-" + name),
		Spec: ovnv1.VpcNatGatewaySpec{
			Vpc:           "test-vpc",
			Subnet:        "test-subnet",
			Replicas:      replicas,
			ConntrackSync: conntrackSync,
		},
	}
}

func TestValidateVpcNatGwConntrackSync(t *testing.T) {
	t.Run("allowed in HA mode", func(t *testing.T) {
		gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		v := &ValidatingHook{cache: newConntrackSyncCache()}
		require.NoError(t, v.ValidateVpcNatGW(context.Background(), &gw))
	})

	t.Run("rejected without HA", func(t *testing.T) {
		gw := newConntrackSyncGateway("test-gw", 1, ovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		v := &ValidatingHook{cache: newConntrackSyncCache()}
		require.ErrorContains(t, v.ValidateVpcNatGW(context.Background(), &gw), "conntrack synchronization requires HA mode")
	})

	t.Run("rejected without replicas", func(t *testing.T) {
		gw := newConntrackSyncGateway("test-gw", 0, ovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		v := &ValidatingHook{cache: newConntrackSyncCache()}
		require.ErrorContains(t, v.ValidateVpcNatGW(context.Background(), &gw), "has 1 replica")
	})

	t.Run("ignored when disabled", func(t *testing.T) {
		// An address left over from a previous configuration must not block a disabled gateway.
		gw := newConntrackSyncGateway("test-gw", 1, ovnv1.VpcNatGatewayConntrackSyncConfig{MulticastGroup: "10.0.0.1"})
		v := &ValidatingHook{cache: newConntrackSyncCache()}
		require.NoError(t, v.ValidateVpcNatGW(context.Background(), &gw))
	})

	groupCases := []struct {
		name   string
		group  string
		errMsg string
	}{
		{name: "unicast address", group: "10.0.0.1", errMsg: "is not an IPv4 multicast address"},
		{name: "not an address", group: "not-an-ip", errMsg: "is not an IPv4 multicast address"},
		{name: "IPv6 group", group: "ff02::1", errMsg: "is not an IPv4 multicast address"},
		{name: "link-local control group", group: "224.0.0.42", errMsg: "reserved for link-local control traffic"},
		{name: "administratively scoped group", group: "239.1.2.3"},
		{name: "other multicast group", group: "225.0.0.50"},
	}
	for _, tc := range groupCases {
		t.Run("group: "+tc.name, func(t *testing.T) {
			gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
				Enabled:        true,
				MulticastGroup: tc.group,
			})
			v := &ValidatingHook{cache: newConntrackSyncCache()}
			err := v.ValidateVpcNatGW(context.Background(), &gw)
			if tc.errMsg == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.errMsg)
		})
	}

	t.Run("rejected when another gateway of the subnet uses the same group and port", func(t *testing.T) {
		other := newConntrackSyncGateway("other-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
		})
		gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
		})
		v := &ValidatingHook{cache: newConntrackSyncCache(other)}
		require.ErrorContains(t, v.ValidateVpcNatGW(context.Background(), &gw), `NAT gateway "other-gw" already synchronizes`)
	})

	t.Run("allowed when the port differs", func(t *testing.T) {
		other := newConntrackSyncGateway("other-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
		})
		gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
			Port:           4000,
		})
		v := &ValidatingHook{cache: newConntrackSyncCache(other)}
		require.NoError(t, v.ValidateVpcNatGW(context.Background(), &gw))
	})

	t.Run("allowed when the other gateway is on another subnet", func(t *testing.T) {
		other := newConntrackSyncGateway("other-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
		})
		other.Spec.Subnet = "other-subnet"
		gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
		})
		v := &ValidatingHook{cache: newConntrackSyncCache(other)}
		require.NoError(t, v.ValidateVpcNatGW(context.Background(), &gw))
	})

	t.Run("allowed on update of the same gateway", func(t *testing.T) {
		gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{
			Enabled:        true,
			MulticastGroup: "239.1.2.3",
		})
		v := &ValidatingHook{cache: newConntrackSyncCache(gw)}
		require.NoError(t, v.ValidateVpcNatGW(context.Background(), &gw))
	})

	t.Run("derived groups of two gateways of a subnet do not collide", func(t *testing.T) {
		other := newConntrackSyncGateway("other-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		gw := newConntrackSyncGateway("test-gw", 2, ovnv1.VpcNatGatewayConntrackSyncConfig{Enabled: true})
		v := &ValidatingHook{cache: newConntrackSyncCache(other)}
		require.NoError(t, v.ValidateVpcNatGW(context.Background(), &gw))
	})
}
