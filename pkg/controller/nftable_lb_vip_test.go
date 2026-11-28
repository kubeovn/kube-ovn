package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestServiceVipRoutesIgnoreUnobservedPeerRecords(t *testing.T) {
	t.Parallel()

	fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"))
	peer := &ovnnb.LogicalRouterPolicy{
		UUID: "peer-route", Match: "ip4.dst == 10.96.1.6",
		Action: "reroute", Nexthops: []string{"10.0.7.254"},
	}
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, natGwVipRouteExternalIDs("gw0"), false).
		Return([]*ovnnb.LogicalRouterPolicy{peer}, nil)
	added := expectVipRoutesAdded(fc, 2)
	rule := ownedShareRule("own", "eip0", "10.96.1.5")
	require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "default/web",
		nftableLbSvcVipsOf(map[string]*kubeovnv1.IptablesDnatRule{rule.Name: rule}, "172.20.0.5"), nil, nil))
	require.Contains(t, added, "ip4.dst == 10.96.1.5")
	require.NotContains(t, added, peer.Match)
}

func TestServiceVipCleanupRetainsSharedEipWithoutPeerRecords(t *testing.T) {
	t.Parallel()

	fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"))
	peer := &v1.Service{
		Namespace: "default", Name: "peer",
		Annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
		Spec:        v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer, Ports: []v1.ServicePort{{Port: 81, Protocol: v1.ProtocolTCP}}},
	}
	require.NoError(t, fc.fakeController.svcIndexer.Add(peer))
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, natGwVipRouteExternalIDs("gw0"), false).
		Return([]*ovnnb.LogicalRouterPolicy{
			{UUID: "own", Match: "ip4.dst == 10.96.1.5"},
			{UUID: "shared", Match: "ip4.dst == 172.20.0.5", Action: "reroute", Nexthops: []string{"10.0.7.254"}},
		}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "own").Return(nil)
	fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	own := peer.DeepCopy()
	own.Name, own.Spec.ClusterIP = "web", "10.96.1.5"
	eips, clusterIPs := fc.fakeController.nftableLbSvcSpecIdentities(own)
	require.Len(t, clusterIPs, 1)
	require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "default/web", nftableLbSvcVips{},
		[]string{clusterIPs[0].vip}, eips))
}

func TestServiceVipEipRetention(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(*v1.Service, *kubeovnv1.IptablesEIP, *Configuration)
		want   bool
	}{
		{name: "shared EIP on another port", want: true},
		{name: "address reused by another EIP", want: true, mutate: func(svc *v1.Service, eip *kubeovnv1.IptablesEIP, _ *Configuration) {
			svc.Annotations[util.EipAnnotation], eip.Name = "eip1", "eip1"
		}},
		{name: "terminating peer", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			now := metav1.Now()
			svc.DeletionTimestamp = &now
		}},
		{name: "other gateway", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			svc.Annotations[util.VpcNatGatewayAnnotation] = "gw1"
		}},
		{name: "opted out", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			delete(svc.Annotations, util.VpcNatGatewayAnnotation)
		}},
		{name: "unsupported port", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			svc.Spec.Ports[0].Protocol = v1.ProtocolSCTP
		}},
		{name: "no longer publishes EIP", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			svc.Spec.Type = v1.ServiceTypeClusterIP
		}},
		{name: "missing EIP", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			svc.Annotations[util.EipAnnotation] = "missing"
		}},
		{name: "EIP now has another address", mutate: func(_ *v1.Service, eip *kubeovnv1.IptablesEIP, _ *Configuration) {
			eip.Status.IP = "172.20.0.6"
		}},
		{name: "EIP bound to another gateway", mutate: func(_ *v1.Service, eip *kubeovnv1.IptablesEIP, _ *Configuration) {
			eip.Spec.NatGwDp = "gw1"
		}},
		{name: "self releases its previous port", mutate: func(svc *v1.Service, _ *kubeovnv1.IptablesEIP, _ *Configuration) {
			svc.Name = "web"
		}},
		{name: "gates disabled", mutate: func(_ *v1.Service, _ *kubeovnv1.IptablesEIP, config *Configuration) {
			config.EnableGwNftableLbSvc = false
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"))
			c := fc.fakeController
			svc := &v1.Service{
				Namespace: "default", Name: "peer",
				Annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
				Spec:        v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer, Ports: []v1.ServicePort{{Port: 81, Protocol: v1.ProtocolTCP}}},
			}
			eip := vipRouteEip.DeepCopy()
			if test.mutate != nil {
				test.mutate(svc, eip, c.config)
			}
			eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, eipIndexer.Add(eip))
			c.iptablesEipsLister = kubeovnlister.NewIptablesEIPLister(eipIndexer)
			require.NoError(t, c.svcIndexer.Add(svc))
			desired, managed, err := c.natGwVipRoutesForService("gw0", "default/web", nftableLbSvcVips{},
				[]string{"10.96.1.5"}, []nftableLbEipIdentityRef{{eipName: "eip0", eipIP: "172.20.0.5"}})
			require.NoError(t, err)
			require.Equal(t, test.want, desired["ip4.dst == 172.20.0.5"] == "172.20.0.5")
			require.NotContains(t, desired, "ip4.dst == 10.96.1.5", "cleanup releases its ClusterIP")
			require.ElementsMatch(t, []string{"ip4.dst == 10.96.1.5", "ip4.dst == 172.20.0.5"}, managed.SortedList())
		})
	}
}

func TestServiceVipRestartRemovesPreviousVipsOnly(t *testing.T) {
	t.Parallel()

	old := ownedShareRule("old", "eip0", "10.96.1.9")
	old.Status.V4ip = "172.20.0.4"
	ledger := []*kubeovnv1.IptablesDnatRule{old}
	fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"), ledger...)
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, natGwVipRouteExternalIDs("gw0"), false).
		Return([]*ovnnb.LogicalRouterPolicy{
			{UUID: "old-cluster", Match: "ip4.dst == 10.96.1.9"},
			{UUID: "old-eip", Match: "ip4.dst == 172.20.0.4"},
			{UUID: "peer-cluster", Match: "ip4.dst == 10.96.1.6"},
			{UUID: "peer-eip", Match: "ip4.dst == 172.20.0.7"},
		}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "old-cluster").Return(nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "old-eip").Return(nil)
	added := expectVipRoutesAdded(fc, 2)
	rule := ownedShareRule("new", "eip0", "10.96.1.5")
	require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "default/web",
		nftableLbSvcVipsOf(map[string]*kubeovnv1.IptablesDnatRule{rule.Name: rule}, "172.20.0.5"),
		nftableLbExistingClusterIPs(ledger), nftableLbEipIdentityRefsFromRecords(ledger)))
	require.Equal(t, map[string][]string{
		"ip4.dst == 10.96.1.5":  {"10.0.7.254"},
		"ip4.dst == 172.20.0.5": {"10.0.7.254"},
	}, added)
}

func TestServiceVipIndexErrorStopsRouteCleanup(t *testing.T) {
	t.Parallel()

	fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"))
	fc.fakeController.svcIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	// No OVN call is allowed after a failed retention query.
	require.Error(t, fc.fakeController.syncNatGwVipStateForService("gw0", "default/web", nftableLbSvcVips{},
		nil, []nftableLbEipIdentityRef{{eipName: "eip0", eipIP: "172.20.0.5"}}))
}
