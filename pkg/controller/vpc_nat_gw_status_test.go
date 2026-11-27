package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type natReplayStatusFixture struct {
	controller *Controller
	client     *kubeovnfake.Clientset
	eip        *kubeovnv1.IptablesEIP
	fip        *kubeovnv1.IptablesFIPRule
	dnat       *kubeovnv1.IptablesDnatRule
	snat       *kubeovnv1.IptablesSnatRule
}

func newNatReplayStatusFixture(t *testing.T) natReplayStatusFixture {
	t.Helper()
	// Keep the informer behind the API server as it can be when replay overlaps
	// with rule creation or an update to an existing rule.
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip",
		Spec: kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10", NatGwDp: "gw"},
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

	return natReplayStatusFixture{controller: c, client: clientset, eip: liveEip, fip: liveFip, dnat: liveDnat, snat: liveSnat}
}

func TestCompletedNatReplayStaysReadyWithStaleCache(t *testing.T) {
	fixture := newNatReplayStatusFixture(t)
	c, client := fixture.controller, fixture.client.KubeovnV1()
	// The stale listers still describe the rules from before this replay.
	const token = "completed-replay"
	_, err := c.markGatewayEipNotReady(fixture.eip.Name, "gw", token)
	require.NoError(t, err)
	require.NoError(t, c.redoFip(fixture.fip.Name, "gw", token))
	require.NoError(t, c.redoDnat(fixture.dnat.Name, "gw", token))
	require.NoError(t, c.redoSnat(fixture.snat.Name, "gw", token))

	require.NoError(t, c.completeEipReplay(fixture.eip.Name, token, ""))
	require.NoError(t, c.completeFipReplay(fixture.fip.Name, token))
	require.NoError(t, c.completeDnatReplay(fixture.dnat.Name, token))
	require.NoError(t, c.completeSnatReplay(fixture.snat.Name, token))

	// A later gateway worker observed the same pod instances. This token
	// must not invalidate the completion, even with those stale listers.
	_, err = c.markGatewayEipNotReady(fixture.eip.Name, "gw", token)
	require.NoError(t, err)
	require.NoError(t, c.redoFip(fixture.fip.Name, "gw", token))
	require.NoError(t, c.redoDnat(fixture.dnat.Name, "gw", token))
	require.NoError(t, c.redoSnat(fixture.snat.Name, "gw", token))

	gotEip, err := client.IptablesEIPs().Get(t.Context(), fixture.eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, gotEip.Status.Ready)
	gotFip, err := client.IptablesFIPRules().Get(t.Context(), fixture.fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, gotFip.Status.Ready)
	gotDnat, err := client.IptablesDnatRules().Get(t.Context(), fixture.dnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, gotDnat.Status.Ready)
	gotSnat, err := client.IptablesSnatRules().Get(t.Context(), fixture.snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, gotSnat.Status.Ready)
}

func TestNatRedoPreservesAppliedStatusWithStaleCache(t *testing.T) {
	fixture := newNatReplayStatusFixture(t)
	c, client := fixture.controller, fixture.client.KubeovnV1()
	const redo = "new-gateway-generation"

	t.Run("FIP", func(t *testing.T) {
		require.NoError(t, c.redoFip(fixture.fip.Name, "gw", redo))
		got, err := client.IptablesFIPRules().Get(t.Context(), fixture.fip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := fixture.fip.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
	t.Run("DNAT", func(t *testing.T) {
		require.NoError(t, c.redoDnat(fixture.dnat.Name, "gw", redo))
		got, err := client.IptablesDnatRules().Get(t.Context(), fixture.dnat.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := fixture.dnat.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
	t.Run("SNAT", func(t *testing.T) {
		require.NoError(t, c.redoSnat(fixture.snat.Name, "gw", redo))
		got, err := client.IptablesSnatRules().Get(t.Context(), fixture.snat.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := fixture.snat.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
	t.Run("EIP", func(t *testing.T) {
		_, err := c.markGatewayEipNotReady(fixture.eip.Name, "gw", redo)
		require.NoError(t, err)
		got, err := client.IptablesEIPs().Get(t.Context(), fixture.eip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		want := fixture.eip.Status
		want.Ready, want.Redo = false, redo
		require.Equal(t, want, got.Status)
	})
}

func TestNatGatewayReplayDuringMigration(t *testing.T) {
	enabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = enabled })

	const source, destination, namespace = "gw", "destination", "kube-system"
	fixture, eip, fipIndexer, destinationPod := newNatGatewayMigrationFixture(t, source, destination, namespace)
	c, client := fixture.controller, fixture.client.KubeovnV1()
	fip, dnat, snat := fixture.fip, fixture.dnat, fixture.snat

	// During installation on the destination, the FIP still records its old
	// applied gateway. Its EIP already belongs exclusively to the destination.
	require.NoError(t, c.handleUpdateVpcFloatingIP(source))
	marked, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	sourceRedo, err := c.natGwRedoToken(source)
	require.NoError(t, err)
	require.Equal(t, sourceRedo, marked.Status.Redo)
	require.False(t, marked.Status.Ready)
	gotEip, err := client.IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, eip.Status, gotEip.Status)

	migrationRedo, err := c.natRuleMigrationRedo(source, destination)
	require.NoError(t, err)
	require.NotEmpty(t, migrationRedo)
	// The destination's replacement starts before the rule publishes its new
	// applied binding. The new label lets the worker find the rule.
	destClaim := marked.DeepCopy()
	destClaim.Labels = map[string]string{util.VpcNatGatewayNameLabel: destination}
	_, err = client.IptablesFIPRules().Update(t.Context(), destClaim, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, fipIndexer.Update(destClaim))
	destinationPod.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
	_, err = c.config.KubeClient.CoreV1().Pods(namespace).UpdateStatus(t.Context(), destinationPod, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.handleUpdateVpcFloatingIP(destination))
	stillSource, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, marked.Status, stillSource.Status)

	require.NoError(t, c.recordFipStatus(fip, eip, migrationRedo))
	require.NoError(t, c.recordDnatStatus(dnat, eip, migrationRedo))
	require.NoError(t, c.recordSnatStatus(snat, eip, migrationRedo))
	require.NoError(t, fipIndexer.Update(fip))

	// The source workers still select rules and the EIP from the stale cache.
	require.NoError(t, c.handleUpdateVpcEip(source))
	require.NoError(t, c.handleUpdateVpcFloatingIP(source))
	require.NoError(t, c.handleUpdateVpcDnat(source))
	require.NoError(t, c.handleUpdateVpcSnat(source))

	gotEip, err = client.IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, eip.Status, gotEip.Status)
	gotFip, err := client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, destination, gotFip.Status.NatGwDp)
	require.False(t, gotFip.Status.Ready)
	require.Equal(t, migrationRedo, gotFip.Status.Redo)
	gotDnat, err := client.IptablesDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, destination, gotDnat.Status.NatGwDp)
	require.False(t, gotDnat.Status.Ready)
	require.Equal(t, migrationRedo, gotDnat.Status.Redo)
	gotSnat, err := client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, destination, gotSnat.Status.NatGwDp)
	require.False(t, gotSnat.Status.Ready)
	require.Equal(t, migrationRedo, gotSnat.Status.Redo)

	// A binding change queues the replay even if the token value stays the same.
	destClaim.Status.Redo = migrationRedo
	c.enqueueUpdateIptablesFip(destClaim, gotFip)
	require.Equal(t, 1, c.updateIptablesFipQueue.Len())

	// When the destination worker publishes a newer token, neither a replay
	// completion nor a migration-status retry can overwrite it.
	require.NoError(t, fipIndexer.Update(gotFip))
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(eip.DeepCopy()))
	c.iptablesEipsLister = kubeovnlister.NewIptablesEIPLister(eipIndexer)
	require.NoError(t, c.handleUpdateVpcFloatingIP(destination))
	replacementRedo, err := c.natGwRedoToken(destination)
	require.NoError(t, err)
	require.NotEqual(t, migrationRedo, replacementRedo)
	require.NoError(t, c.completeFipReplay(fip.Name, migrationRedo))
	require.NoError(t, c.recordFipStatus(fip, eip, migrationRedo))
	gotFip, err = client.IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotFip.Status.Ready)
	require.Equal(t, replacementRedo, gotFip.Status.Redo)
}

func newNatGatewayMigrationFixture(t *testing.T, source, destination, namespace string) (natReplayStatusFixture, *kubeovnv1.IptablesEIP, cache.Indexer, *corev1.Pod) {
	t.Helper()
	fixture := newNatReplayStatusFixture(t)
	c, client := fixture.controller, fixture.client.KubeovnV1()
	labels := map[string]string{util.VpcNatGatewayNameLabel: source}
	fip, dnat, snat := fixture.fip, fixture.dnat, fixture.snat
	for _, rule := range []metav1.Object{fip, dnat, snat} {
		rule.SetLabels(labels)
	}
	eip := fixture.eip.DeepCopy()
	eip.Spec.NatGwDp = destination
	eip.Status.Ready = false
	eip.Status.Redo = "destination-replay"
	_, err := client.IptablesEIPs().Update(t.Context(), eip, metav1.UpdateOptions{})
	require.NoError(t, err)
	fip.Spec.EIP = eip.Name
	fip.Spec.InternalIP = fip.Status.InternalIP
	_, err = client.IptablesFIPRules().Update(t.Context(), fip, metav1.UpdateOptions{})
	require.NoError(t, err)
	dnat.Spec.EIP = eip.Name
	_, err = client.IptablesDnatRules().Update(t.Context(), dnat, metav1.UpdateOptions{})
	require.NoError(t, err)
	snat.Spec.EIP = eip.Name
	_, err = client.IptablesSnatRules().Update(t.Context(), snat, metav1.UpdateOptions{})
	require.NoError(t, err)

	index := func(obj runtime.Object) cache.Indexer {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		require.NoError(t, indexer.Add(obj.DeepCopyObject()))
		return indexer
	}
	c.iptablesDnatRulesLister = kubeovnlister.NewIptablesDnatRuleLister(index(dnat))
	c.iptablesSnatRulesLister = kubeovnlister.NewIptablesSnatRuleLister(index(snat))
	fipIndexer := index(fip)
	c.iptablesFipsLister = kubeovnlister.NewIptablesFIPRuleLister(fipIndexer)
	gateways := index(&kubeovnv1.VpcNatGateway{Name: source})
	require.NoError(t, gateways.Add(&kubeovnv1.VpcNatGateway{Name: destination}))
	c.vpcNatGatewayLister = kubeovnlister.NewVpcNatGatewayLister(gateways)
	c.vpcNatGwKeyMutex = keymutex.NewHashed(0)
	c.config.PodNamespace = namespace
	gatewayPod := func(gateway string) *corev1.Pod {
		return &corev1.Pod{
			Name: gateway, Namespace: namespace,
			Labels: map[string]string{"app": util.GenNatGwName(gateway), util.VpcNatGatewayLabel: "true"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "vpc-nat-gw", ContainerID: "containerd://" + gateway,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				}},
			},
		}
	}
	destinationPod := gatewayPod(destination)
	c.config.KubeClient = kubefake.NewClientset(gatewayPod(source), destinationPod)
	c.updateIptablesFipQueue = newTypedRateLimitingQueue[string]("fip-migration-test", nil)
	t.Cleanup(c.updateIptablesFipQueue.ShutDown)

	return fixture, eip, fipIndexer, destinationPod
}

func TestNatRedoDuringFirstInstallUsesLiveGatewayClaim(t *testing.T) {
	fixture := newNatReplayStatusFixture(t)
	c, client := fixture.controller, fixture.client.KubeovnV1()
	snat := fixture.snat.DeepCopy()
	snat.Status = kubeovnv1.IptablesSnatRuleStatus{}
	snat.Spec.EIP = fixture.eip.Name
	snat.Labels = map[string]string{util.VpcNatGatewayNameLabel: "stale"}
	_, err := client.IptablesSnatRules().Update(t.Context(), snat, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.NoError(t, c.redoSnat(snat.Name, "stale", "stale-replay"))
	got, err := client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, got.Status.Redo)

	snat.Labels[util.VpcNatGatewayNameLabel] = fixture.eip.Spec.NatGwDp
	_, err = client.IptablesSnatRules().Update(t.Context(), snat, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.redoSnat(snat.Name, fixture.eip.Spec.NatGwDp, "current-replay"))
	got, err = client.IptablesSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "current-replay", got.Status.Redo)
	require.Empty(t, got.Status.NatGwDp)
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
			require.NoError(t, c.recordFipStatus(fip, eip, ""))
			require.NoError(t, c.recordDnatStatus(dnat, eip, ""))
			require.NoError(t, c.recordSnatStatus(snat, eip, ""))

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
				NatGwDp: eip.Spec.NatGwDp, InternalCIDR: "10.0.0.5/32",
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
	require.NoError(t, c.recordFipStatus(fip, eip, ""))
	got, err := client.Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, got.Status.Ready)
	require.Equal(t, newRedo, got.Status.Redo)
	require.Equal(t, fip.Spec.InternalIP, got.Status.InternalIP)
	require.Equal(t, "10.0.0.7", got.Spec.InternalIP)

	require.NoError(t, c.recordFipStatus(fip, eip, ""))
	require.Equal(t, 2, attempts, "an already published snapshot needs no further write")
}
