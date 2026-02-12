package controller

import (
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newLogicalRouterPort(lrName, lrpName, mac string, networks []string) *ovnnb.LogicalRouterPort {
	return &ovnnb.LogicalRouterPort{
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
		ExternalIDs: map[string]string{
			"lr":     lrName,
			"vendor": util.CniTypeName,
		},
	}
}

func Test_logicalRouterPortFilter(t *testing.T) {
	t.Parallel()

	exceptPeerPorts := strset.New(
		"except-lrp-0",
		"except-lrp-1",
	)

	lrpNames := []string{"other-0", "other-1", "other-2", "except-lrp-0", "except-lrp-1"}
	lrps := make([]*ovnnb.LogicalRouterPort, 0)
	for _, lrpName := range lrpNames {
		lrp := newLogicalRouterPort("", lrpName, "", nil)
		peer := lrpName + "-peer"
		lrp.Peer = &peer
		lrps = append(lrps, lrp)
	}

	filterFunc := logicalRouterPortFilter(exceptPeerPorts)

	for _, lrp := range lrps {
		if exceptPeerPorts.Has(lrp.Name) {
			require.False(t, filterFunc(lrp))
		} else {
			require.True(t, filterFunc(lrp))
		}
	}
}

func Test_gcOvnLb(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	fakeCtrl, err := newFakeControllerWithOptions(t, nil)
	if err != nil {
		t.Fatalf("failed to create fake controller: %v", err)
	}

	t.Run("cleanup stale ip_port_mappings", func(t *testing.T) {
		lb1 := &ovnnb.LoadBalancer{
			Name: "lb1",
			ExternalIDs: map[string]string{
				"vendor": util.CniTypeName,
			},
			Vips: map[string]string{
				"10.96.0.1:80": "192.168.1.1:80,192.168.1.2:80",
			},
			IPPortMappings: map[string]string{
				"192.168.1.1": "node1", // active
				"192.168.1.2": "node2", // active
				"192.168.1.3": "node3", // stale
			},
		}

		lb2 := &ovnnb.LoadBalancer{
			Name: "lb2",
			ExternalIDs: map[string]string{
				"vendor": util.CniTypeName,
			},
			Vips: map[string]string{
				"10.96.0.2:443": "192.168.2.10:443",
			},
			IPPortMappings: map[string]string{
				"192.168.2.1": "node1", // stale
				"192.168.2.2": "node2", // stale
			},
		}

		// IPv6 test case
		lb3 := &ovnnb.LoadBalancer{
			Name: "lb3",
			ExternalIDs: map[string]string{
				"vendor": util.CniTypeName,
			},
			Vips: map[string]string{
				"[fd00::1]:80": "[fd00::101]:80",
			},
			IPPortMappings: map[string]string{
				"fd00::101": "node1", // active
				"fd00::102": "node2", // stale
			},
		}

		fakeCtrl.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{*lb1, *lb2, *lb3}, nil)

		// Expect deletions for stale entries
		fakeCtrl.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping("lb1", "192.168.1.3").Return(nil)
		fakeCtrl.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping("lb2", "192.168.2.1").Return(nil)
		fakeCtrl.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping("lb2", "192.168.2.2").Return(nil)
		fakeCtrl.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping("lb3", "fd00::102").Return(nil)

		err := fakeCtrl.fakeController.gcIpPortMapping()
		if err != nil {
			t.Errorf("gcIpPortMapping() error = %v", err)
		}
	})

	t.Run("no stale mappings", func(t *testing.T) {
		lb := &ovnnb.LoadBalancer{
			Name: "lb-clean",
			ExternalIDs: map[string]string{
				"vendor": util.CniTypeName,
			},
			Vips: map[string]string{
				"10.96.0.1:80": "192.168.1.1:80",
			},
			IPPortMappings: map[string]string{
				"192.168.1.1": "node1",
			},
		}

		fakeCtrl.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{*lb}, nil)
		// No LoadBalancerDeleteIPPortMapping expected

		err := fakeCtrl.fakeController.gcIpPortMapping()
		if err != nil {
			t.Errorf("gcIpPortMapping() error = %v", err)
		}
	})
}
