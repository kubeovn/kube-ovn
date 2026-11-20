package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelister "k8s.io/client-go/listers/core/v1"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNatReplayUsesEachGatewayPodCreationTime(t *testing.T) {
	enabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = enabled })

	const namespace = "kube-system"
	pods := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	eips := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	client := kubeovnfake.NewSimpleClientset()
	c := &Controller{
		config:             &Configuration{PodNamespace: namespace, KubeOvnClient: client},
		podsLister:         corelister.NewPodLister(pods),
		iptablesEipsLister: kubeovnlister.NewIptablesEIPLister(eips),
		vpcNatGwKeyMutex:   keymutex.NewHashed(0),
	}
	gateways := []struct {
		name string
		redo string
	}{
		{name: "gw-a", redo: "2026-11-20T08:00:00"},
		{name: "gw-b", redo: "2026-11-20T08:03:00"},
	}
	for _, gw := range gateways {
		created, err := time.Parse(time.RFC3339, gw.redo+"Z")
		require.NoError(t, err)
		require.NoError(t, pods.Add(&corev1.Pod{
			Namespace: namespace, Name: gw.name,
			Labels:            map[string]string{"app": util.GenNatGwName(gw.name), util.VpcNatGatewayLabel: "true"},
			CreationTimestamp: metav1.NewTime(created),
			Status:            corev1.PodStatus{Phase: corev1.PodRunning},
		}))
		eip := &kubeovnv1.IptablesEIP{
			Name:   gw.name,
			Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: gw.name},
			Status: kubeovnv1.IptablesEIPStatus{Ready: true},
		}
		require.NoError(t, eips.Add(eip))
		_, err = client.KubeovnV1().IptablesEIPs().Create(t.Context(), eip, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	for _, gw := range gateways {
		require.NoError(t, c.handleUpdateVpcEip(gw.name))
	}
	for _, gw := range gateways {
		eip, err := client.KubeovnV1().IptablesEIPs().Get(t.Context(), gw.name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, gw.redo, eip.Status.Redo)
		require.False(t, eip.Status.Ready)
	}
}

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

	t.Run("completed replay stays ready with stale cache", func(t *testing.T) {
		// The stale listers still describe the rules from before this replay.
		const token = "completed-replay"
		require.NoError(t, c.markEipNotReady(eip.Name, token))
		require.NoError(t, c.redoFip(fip.Name, token, true))
		require.NoError(t, c.redoDnat(dnat.Name, token, true))
		require.NoError(t, c.redoSnat(snat.Name, token, true))

		require.NoError(t, c.completeEipReplay(eip.Name, token, ""))
		require.NoError(t, c.completeFipReplay(fip.Name, token))
		require.NoError(t, c.completeDnatReplay(dnat.Name, token))
		require.NoError(t, c.completeSnatReplay(snat.Name, token))

		// A later gateway worker observed the same pod instances. This token
		// must not invalidate the completion, even with those stale listers.
		require.NoError(t, c.markEipNotReady(eip.Name, token))
		require.NoError(t, c.redoFip(fip.Name, token, true))
		require.NoError(t, c.redoDnat(dnat.Name, token, true))
		require.NoError(t, c.redoSnat(snat.Name, token, true))

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
		require.NoError(t, c.markEipNotReady(eip.Name, redo))
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
		Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10"},
		Status: kubeovnv1.IptablesEIPStatus{Ready: true, Redo: "gateway-generation"},
	}
	dnat := &kubeovnv1.IptablesDnatRule{
		Name:   "dnat",
		Labels: map[string]string{util.EipV4IpLabel: eip.Spec.V4ip},
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
	require.NoError(t, c.refreshEipNat(eip.Name))

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

func TestRecordIptablesRuleStatusRespectsReplayToken(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		Spec:   kubeovnv1.IptablesEIPSpec{V6ip: "2001:db8::5", NatGwDp: "gw"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "192.0.2.5"},
	}
	for _, tt := range []struct {
		name       string
		workerRedo string
		liveRedo   string
		wantReady  bool
	}{
		{name: "new rule", wantReady: true},
		{name: "spec update after replay", workerRedo: "prior-replay", liveRedo: "prior-replay", wantReady: true},
		{name: "new replay during rule creation", liveRedo: "pending-replay"},
		{name: "new replay during spec update", workerRedo: "prior-replay", liveRedo: "pending-replay"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clientset := kubeovnfake.NewSimpleClientset()
			client := clientset.KubeovnV1()
			c := &Controller{config: &Configuration{KubeOvnClient: clientset}}

			fip := &kubeovnv1.IptablesFIPRule{
				Name: "fip", Spec: kubeovnv1.IptablesFIPRuleSpec{InternalIP: "10.0.0.5"},
				Status: kubeovnv1.IptablesFIPRuleStatus{Ready: tt.workerRedo != "", Redo: tt.workerRedo},
			}
			dnat := &kubeovnv1.IptablesDnatRule{
				Name: "dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					Protocol: "tcp", InternalIP: "10.0.0.5", InternalPort: "8080", ExternalPort: "80",
				},
				Status: kubeovnv1.IptablesDnatRuleStatus{Ready: tt.workerRedo != "", Redo: tt.workerRedo},
			}
			snat := &kubeovnv1.IptablesSnatRule{
				Name: "snat", Spec: kubeovnv1.IptablesSnatRuleSpec{InternalCIDR: "10.0.0.5"},
				Status: kubeovnv1.IptablesSnatRuleStatus{Ready: tt.workerRedo != "", Redo: tt.workerRedo},
			}
			_, err := client.IptablesFIPRules().Create(t.Context(), fip, metav1.CreateOptions{})
			require.NoError(t, err)
			_, err = client.IptablesDnatRules().Create(t.Context(), dnat, metav1.CreateOptions{})
			require.NoError(t, err)
			_, err = client.IptablesSnatRules().Create(t.Context(), snat, metav1.CreateOptions{})
			require.NoError(t, err)

			redo := tt.liveRedo
			if redo == tt.workerRedo {
				// The spec worker temporarily clears readiness without changing Redo.
				redo = ""
			}
			require.NoError(t, c.markFipNotReady(fip.Name, redo))
			require.NoError(t, c.markDnatNotReady(dnat.Name, redo))
			require.NoError(t, c.markSnatNotReady(snat.Name, redo))
			require.NoError(t, c.recordFipStatus(fip, eip))
			require.NoError(t, c.recordDnatStatus(dnat, eip))
			require.NoError(t, c.recordSnatStatus(snat, eip))

			gotFip, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, kubeovnv1.IptablesFIPRuleStatus{
				Redo: tt.liveRedo, Ready: tt.wantReady, V4ip: eip.Status.IP, V6ip: eip.Spec.V6ip,
				NatGwDp: eip.Spec.NatGwDp, InternalIP: fip.Spec.InternalIP,
			}, gotFip.Status)
			gotDnat, err := client.IptablesDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, kubeovnv1.IptablesDnatRuleStatus{
				Redo: tt.liveRedo, Ready: tt.wantReady, V4ip: eip.Status.IP, V6ip: eip.Spec.V6ip,
				NatGwDp: eip.Spec.NatGwDp, Protocol: dnat.Spec.Protocol,
				InternalIP: dnat.Spec.InternalIP, InternalPort: dnat.Spec.InternalPort, ExternalPort: dnat.Spec.ExternalPort,
			}, gotDnat.Status)
			gotSnat, err := client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, kubeovnv1.IptablesSnatRuleStatus{
				Redo: tt.liveRedo, Ready: tt.wantReady, V4ip: eip.Status.IP, V6ip: eip.Spec.V6ip,
				NatGwDp: eip.Spec.NatGwDp, InternalCIDR: snat.Spec.InternalCIDR,
			}, gotSnat.Status)
		})
	}
}

func TestRecordIptablesRuleStatusRetriesAfterReplayInvalidation(t *testing.T) {
	const (
		resource = "iptables-fip-rules"
		oldRedo  = "old-replay"
		newRedo  = "new-replay"
	)
	fip := &kubeovnv1.IptablesFIPRule{
		Name: "fip", ResourceVersion: "10",
		Spec:   kubeovnv1.IptablesFIPRuleSpec{InternalIP: "10.0.0.6"},
		Status: kubeovnv1.IptablesFIPRuleStatus{Redo: oldRedo, InternalIP: "10.0.0.5"},
	}
	eip := &kubeovnv1.IptablesEIP{
		Spec: kubeovnv1.IptablesEIPSpec{NatGwDp: "gw"}, Status: kubeovnv1.IptablesEIPStatus{IP: "192.0.2.5"},
	}
	clientset := kubeovnfake.NewSimpleClientset()
	client := clientset.KubeovnV1().IptablesFIPRules()
	_, err := client.Create(t.Context(), fip, metav1.CreateOptions{})
	require.NoError(t, err)

	attempts := 0
	clientset.PrependReactor("update", resource, func(action ktesting.Action) (bool, runtime.Object, error) {
		require.Equal(t, "status", action.GetSubresource())
		attempt := action.(ktesting.UpdateAction).GetObject().(*kubeovnv1.IptablesFIPRule)
		attempts++
		if attempts > 1 {
			require.Equal(t, "11", attempt.ResourceVersion)
			require.False(t, attempt.Status.Ready)
			require.Equal(t, newRedo, attempt.Status.Redo)
			require.Equal(t, fip.Spec.InternalIP, attempt.Status.InternalIP)
			return false, nil, nil
		}
		require.True(t, attempt.Status.Ready)
		require.Equal(t, fip.ResourceVersion, attempt.ResourceVersion)
		// The tracker has no resourceVersion check. Model the API accepting a
		// new invalidation and spec edit after GET, then rejecting this write.
		invalidated := fip.DeepCopy()
		invalidated.ResourceVersion = "11"
		invalidated.Status.Redo = newRedo
		invalidated.Spec.InternalIP = "10.0.0.7"
		gvr := action.GetResource()
		require.NoError(t, clientset.Tracker().Update(gvr, invalidated, ""))
		return true, nil, k8serrors.NewConflict(gvr.GroupResource(), fip.Name, nil)
	})
	c := &Controller{config: &Configuration{KubeOvnClient: clientset}}
	require.NoError(t, c.recordFipStatus(fip, eip))
	got, err := client.Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, got.Status.Ready)
	require.Equal(t, newRedo, got.Status.Redo)
	require.Equal(t, fip.Spec.InternalIP, got.Status.InternalIP)
	require.Equal(t, "10.0.0.7", got.Spec.InternalIP)

	require.NoError(t, c.recordFipStatus(fip, eip))
	require.Equal(t, 2, attempts, "an already published snapshot needs no further write")
}
