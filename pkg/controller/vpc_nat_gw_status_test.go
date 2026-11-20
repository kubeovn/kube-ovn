package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNatRedoPreservesAppliedStatusWithStaleCache(t *testing.T) {
	// Keep the informer behind the API server as it can be when replay overlaps
	// with rule creation or an update to an existing rule.
	const redo = "new-gateway-generation"
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip",
		Spec: kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10"},
		// The stale informer still records the EIP as used by a DNAT, while its
		// stale NAT-rule listers below calculate no users. Replay must preserve
		// this applied status instead of clearing Nat from the API object.
		Status: kubeovnv1.IptablesEIPStatus{Nat: util.DnatUsingEip},
	}

	fip := &kubeovnv1.IptablesFIPRule{Name: "fip"}
	dnat := &kubeovnv1.IptablesDnatRule{Name: "dnat"}
	snat := &kubeovnv1.IptablesSnatRule{Name: "snat"}

	liveEip := eip.DeepCopy()
	liveEip.Status = kubeovnv1.IptablesEIPStatus{Ready: true, IP: "192.0.2.10", Nat: util.DnatUsingEip, QoSPolicy: "qos"}
	liveFip := fip.DeepCopy()
	liveFip.Status = kubeovnv1.IptablesFIPRuleStatus{
		Ready: true, V4ip: "192.0.2.10", V6ip: "2001:db8::10", NatGwDp: "gw", InternalIP: "10.0.0.10",
	}
	liveDnat := dnat.DeepCopy()
	liveDnat.Status = kubeovnv1.IptablesDnatRuleStatus{
		Ready: true, V4ip: "192.0.2.10", NatGwDp: "gw",
		Protocol: "tcp", InternalIP: "10.0.0.20", InternalPort: "8080", ExternalPort: "80",
	}
	liveSnat := snat.DeepCopy()
	liveSnat.Status = kubeovnv1.IptablesSnatRuleStatus{
		Ready: true, V4ip: "192.0.2.10", NatGwDp: "gw", InternalCIDR: "10.0.1.0/24",
	}

	index := func(obj metav1.Object) cache.Indexer {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		require.NoError(t, indexer.Add(obj))
		return indexer
	}
	clientset := kubeovnfake.NewSimpleClientset()
	client := clientset.KubeovnV1()
	_, err := client.IptablesEIPs().Create(t.Context(), liveEip, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.IptablesFIPRules().Create(t.Context(), liveFip, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.IptablesDnatRules().Create(t.Context(), liveDnat, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.IptablesSnatRules().Create(t.Context(), liveSnat, metav1.CreateOptions{})
	require.NoError(t, err)
	c := &Controller{
		config:                  &Configuration{KubeOvnClient: clientset},
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(index(eip)),
		iptablesFipsLister:      kubeovnlister.NewIptablesFIPRuleLister(index(fip)),
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(index(dnat)),
		iptablesSnatRulesLister: kubeovnlister.NewIptablesSnatRuleLister(index(snat)),
	}

	t.Run("Ready restoration with stale cache", func(t *testing.T) {
		// The first call has already patched the API object to Ready=false, but
		// these informer objects remain Ready=true for the second call.
		eip.Status.Ready = true
		fip.Status.Ready = true
		dnat.Status.Ready = true
		snat.Status.Ready = true
		require.NoError(t, c.patchEipStatus(eip.Name, "", "ready-restoration", "", false))
		require.NoError(t, c.patchEipStatus(eip.Name, "", "ready-restoration", "", true))
		require.NoError(t, c.patchFipStatus(fip.Name, "", "", "", "", false))
		require.NoError(t, c.patchFipStatus(fip.Name, "", "", "", "", true))
		require.NoError(t, c.patchDnatStatus(dnat.Name, "", "", "", "", false))
		require.NoError(t, c.patchDnatStatus(dnat.Name, "", "", "", "", true))
		require.NoError(t, c.patchSnatStatus(snat.Name, "", "", "", "", false))
		require.NoError(t, c.patchSnatStatus(snat.Name, "", "", "", "", true))

		gotEip, err := client.IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.True(t, gotEip.Status.Ready)
		gotFip, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.True(t, gotFip.Status.Ready)
		gotDnat, err := client.IptablesDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.True(t, gotDnat.Status.Ready)
		gotSnat, err := client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.True(t, gotSnat.Status.Ready)
	})

	t.Run("FIP", func(t *testing.T) {
		require.NoError(t, c.redoFip(fip.Name, redo, true))
		got, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := liveFip.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
	t.Run("DNAT", func(t *testing.T) {
		require.NoError(t, c.redoDnat(dnat.Name, redo, true))
		got, err := client.IptablesDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := liveDnat.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
	t.Run("SNAT", func(t *testing.T) {
		require.NoError(t, c.redoSnat(snat.Name, redo, true))
		got, err := client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := liveSnat.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
	t.Run("EIP", func(t *testing.T) {
		require.NoError(t, c.patchEipStatus(eip.Name, "", redo, "", false))
		got, err := client.IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := liveEip.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
}

func TestNatStatusPatchNoop(t *testing.T) {
	require.Empty(t, natStatusPatch(true, "", true, "", false))
	require.Equal(t, util.KVPatch{"ready": true}, natStatusPatch(true, "", true, "", true))
	require.Equal(t, util.KVPatch{"redo": "generation", "ready": false},
		natStatusPatch(true, "", false, "generation", false))
}
