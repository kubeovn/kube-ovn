package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
		require.NoError(t, c.patchEipReplayStatus(eip.Name, "ready-restoration", "", false))
		require.NoError(t, c.patchEipReplayStatus(eip.Name, "", "", true))
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
		require.NoError(t, c.patchEipReplayStatus(eip.Name, redo, "", false))
		got, err := client.IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := liveEip.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
}

func TestEipReplayCompletionPreservesAppliedNat(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Status: kubeovnv1.IptablesEIPStatus{
			Ready: true, Redo: "gateway-generation", Nat: util.DnatUsingEip,
		},
	}

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(eip))
	clientset := kubeovnfake.NewSimpleClientset()
	liveEip := eip.DeepCopy()
	liveEip.Status.Ready = false
	_, err := clientset.KubeovnV1().IptablesEIPs().Create(t.Context(), liveEip, metav1.CreateOptions{})
	require.NoError(t, err)
	c := &Controller{
		config:                  &Configuration{KubeOvnClient: clientset},
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(indexer),
		iptablesFipsLister:      kubeovnlister.NewIptablesFIPRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		iptablesSnatRulesLister: kubeovnlister.NewIptablesSnatRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}

	// Replay completion only restores readiness. The informer has not observed the DNAT yet,
	// so deriving Nat from its empty lister would incorrectly clear the applied status in the API.
	require.NoError(t, c.completeEipReplay(eip.Name, eip.Status.Redo, ""))

	got, err := clientset.KubeovnV1().IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, eip.Status.Nat, got.Status.Nat)
}

func TestEipNatRefreshPreservesPendingReplay(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Status: kubeovnv1.IptablesEIPStatus{Ready: true, Redo: "gateway-generation"},
	}
	dnat := &kubeovnv1.IptablesDnatRule{
		Name:   "dnat",
		Labels: map[string]string{util.EipUIDLabel: string(eip.UID)},
	}
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(eip))
	dnatIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, dnatIndexer.Add(dnat))
	clientset := kubeovnfake.NewSimpleClientset()
	liveEip := eip.DeepCopy()
	liveEip.Status.Ready = false
	_, err := clientset.KubeovnV1().IptablesEIPs().Create(t.Context(), liveEip, metav1.CreateOptions{})
	require.NoError(t, err)
	c := &Controller{
		config:                  &Configuration{KubeOvnClient: clientset},
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eipIndexer),
		iptablesFipsLister:      kubeovnlister.NewIptablesFIPRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(dnatIndexer),
		iptablesSnatRulesLister: kubeovnlister.NewIptablesSnatRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}

	// A normal NAT refresh observes the newly indexed DNAT but must leave replay readiness alone.
	require.NoError(t, c.patchEipStatus(eip.Name, "", "", "", true))

	got, err := clientset.KubeovnV1().IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, got.Status.Ready)
	require.Equal(t, eip.Status.Redo, got.Status.Redo)
	require.Equal(t, util.DnatUsingEip, got.Status.Nat)
}

func TestReplayCompletionDoesNotResurrectNewerToken(t *testing.T) {
	const (
		oldRedo = "replay-a"
		newRedo = "replay-b"
	)
	eip := &kubeovnv1.IptablesEIP{Name: "eip", Status: kubeovnv1.IptablesEIPStatus{Redo: oldRedo}}
	fip := &kubeovnv1.IptablesFIPRule{Name: "fip", Status: kubeovnv1.IptablesFIPRuleStatus{Redo: oldRedo}}
	dnat := &kubeovnv1.IptablesDnatRule{Name: "dnat", Status: kubeovnv1.IptablesDnatRuleStatus{Redo: oldRedo}}
	snat := &kubeovnv1.IptablesSnatRule{Name: "snat", Status: kubeovnv1.IptablesSnatRuleStatus{Redo: oldRedo}}

	clientset := kubeovnfake.NewSimpleClientset()
	client := clientset.KubeovnV1()
	liveEip := eip.DeepCopy()
	liveEip.Status.Ready, liveEip.Status.Redo = false, newRedo
	liveFip := fip.DeepCopy()
	liveFip.Status.Ready, liveFip.Status.Redo = false, newRedo
	liveDnat := dnat.DeepCopy()
	liveDnat.Status.Ready, liveDnat.Status.Redo = false, newRedo
	liveSnat := snat.DeepCopy()
	liveSnat.Status.Ready, liveSnat.Status.Redo = false, newRedo
	_, err := client.IptablesEIPs().Create(t.Context(), liveEip, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.IptablesFIPRules().Create(t.Context(), liveFip, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.IptablesDnatRules().Create(t.Context(), liveDnat, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.IptablesSnatRules().Create(t.Context(), liveSnat, metav1.CreateOptions{})
	require.NoError(t, err)
	index := func(obj metav1.Object) cache.Indexer {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		require.NoError(t, indexer.Add(obj))
		return indexer
	}
	c := &Controller{
		config:                  &Configuration{KubeOvnClient: clientset},
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(index(eip)),
		iptablesFipsLister:      kubeovnlister.NewIptablesFIPRuleLister(index(fip)),
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(index(dnat)),
		iptablesSnatRulesLister: kubeovnlister.NewIptablesSnatRuleLister(index(snat)),
	}

	require.NoError(t, c.completeEipReplay(eip.Name, oldRedo, ""))
	require.NoError(t, c.completeFipReplay(fip.Name, oldRedo))
	require.NoError(t, c.completeDnatReplay(dnat.Name, oldRedo))
	require.NoError(t, c.completeSnatReplay(snat.Name, oldRedo))

	gotEip, err := client.IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotEip.Status.Ready)
	require.Equal(t, newRedo, gotEip.Status.Redo)
	gotFip, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotFip.Status.Ready)
	require.Equal(t, newRedo, gotFip.Status.Redo)
	gotDnat, err := client.IptablesDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotDnat.Status.Ready)
	require.Equal(t, newRedo, gotDnat.Status.Redo)
	gotSnat, err := client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotSnat.Status.Ready)
	require.Equal(t, newRedo, gotSnat.Status.Redo)
}

func TestReplayInvalidationDoesNotOverwriteNewerToken(t *testing.T) {
	const staleRedo = "replay-a"
	gwName := "gw"
	liveEip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: gwName},
		Status: kubeovnv1.IptablesEIPStatus{Redo: "replay-b"},
	}
	liveFip := &kubeovnv1.IptablesFIPRule{
		Name: "fip", Status: kubeovnv1.IptablesFIPRuleStatus{NatGwDp: gwName, Redo: "replay-b"},
	}
	liveDnat := &kubeovnv1.IptablesDnatRule{
		Name: "dnat", Status: kubeovnv1.IptablesDnatRuleStatus{NatGwDp: gwName, Redo: "replay-b"},
	}
	liveSnat := &kubeovnv1.IptablesSnatRule{
		Name: "snat", Status: kubeovnv1.IptablesSnatRuleStatus{NatGwDp: gwName, Redo: "replay-b"},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw-pod",
			Namespace: metav1.NamespaceSystem,
			UID:       "pod-uid",
			Labels: map[string]string{
				"app": util.GenNatGwName(gwName), util.VpcNatGatewayLabel: "true",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "vpc-nat-gw", ContainerID: "container-b",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{fakeGw(gwName)},
		Pods:              []*corev1.Pod{pod},
		IptablesEips:      []*kubeovnv1.IptablesEIP{liveEip},
		IptablesFips:      []*kubeovnv1.IptablesFIPRule{liveFip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{liveDnat},
		IptablesSnatRules: []*kubeovnv1.IptablesSnatRule{liveSnat},
	})
	require.NoError(t, err)
	c := fc.fakeController
	currentRedo, err := c.natGwRedoToken(gwName)
	require.NoError(t, err)
	liveEip.Status.Redo = currentRedo
	liveFip.Status.Redo = currentRedo
	liveDnat.Status.Redo = currentRedo
	liveSnat.Status.Redo = currentRedo
	for _, obj := range []*kubeovnv1.IptablesEIP{liveEip} {
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().UpdateStatus(t.Context(), obj, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().UpdateStatus(t.Context(), liveFip, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().UpdateStatus(t.Context(), liveDnat, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().UpdateStatus(t.Context(), liveSnat, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Replace the rule listers with the stale worker snapshots. The API objects retain
	// the newer gateway token, so every old worker update must be ignored.
	eipStale := liveEip.DeepCopy()
	eipStale.Status.Redo = staleRedo
	fipStale := liveFip.DeepCopy()
	fipStale.Status.Redo = staleRedo
	dnatStale := liveDnat.DeepCopy()
	dnatStale.Status.Redo = staleRedo
	snatStale := liveSnat.DeepCopy()
	snatStale.Status.Redo = staleRedo
	makeIndexer := func(obj metav1.Object) cache.Indexer {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		require.NoError(t, indexer.Add(obj))
		return indexer
	}
	c.iptablesEipsLister = kubeovnlister.NewIptablesEIPLister(makeIndexer(eipStale))
	c.iptablesFipsLister = kubeovnlister.NewIptablesFIPRuleLister(makeIndexer(fipStale))
	c.iptablesDnatRulesLister = kubeovnlister.NewIptablesDnatRuleLister(makeIndexer(dnatStale))
	c.iptablesSnatRulesLister = kubeovnlister.NewIptablesSnatRuleLister(makeIndexer(snatStale))

	require.NoError(t, c.patchEipReplayStatus(liveEip.Name, staleRedo, "", false))
	require.NoError(t, c.patchFipStatus(liveFip.Name, "", "", "", staleRedo, false))
	require.NoError(t, c.patchDnatStatus(liveDnat.Name, "", "", "", staleRedo, false))
	require.NoError(t, c.patchSnatStatus(liveSnat.Name, "", "", "", staleRedo, false))
	// A normal add/update pass may still see these stale Ready=true snapshots. It must not
	// restore readiness while the API object carries the newer pending replay token.
	eipStale.Status.Ready = true
	fipStale.Status.Ready = true
	dnatStale.Status.Ready = true
	snatStale.Status.Ready = true
	require.NoError(t, c.patchFipStatus(liveFip.Name, "", "", "", "", true))
	require.NoError(t, c.patchDnatStatus(liveDnat.Name, "", "", "", "", true))
	require.NoError(t, c.patchSnatStatus(liveSnat.Name, "", "", "", "", true))

	gotEip, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(t.Context(), liveEip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, currentRedo, gotEip.Status.Redo)
	gotFip, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Get(t.Context(), liveFip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, currentRedo, gotFip.Status.Redo)
	gotDnat, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Get(t.Context(), liveDnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, currentRedo, gotDnat.Status.Redo)
	gotSnat, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Get(t.Context(), liveSnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, currentRedo, gotSnat.Status.Redo)
	require.False(t, gotFip.Status.Ready)
	require.False(t, gotDnat.Status.Ready)
	require.False(t, gotSnat.Status.Ready)
}

func TestNatStatusPatchNoop(t *testing.T) {
	require.Empty(t, natStatusPatch(true, "", true, "", false))
	require.Equal(t, util.KVPatch{"ready": true}, natStatusPatch(true, "", true, "", true))
	require.Equal(t, util.KVPatch{"redo": "generation", "ready": false},
		natStatusPatch(true, "", false, "generation", false))
	require.Empty(t, natStatusPatch(false, "pending", true, "", true))
}
