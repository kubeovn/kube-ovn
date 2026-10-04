package controller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestMakeOvnEipStatusPatchOmitsStaleAllocationFields(t *testing.T) {
	patch, err := makeOvnEipStatusPatch(true, "")
	require.NoError(t, err)

	var body struct {
		Status map[string]any `json:"status"`
	}
	require.NoError(t, json.Unmarshal(patch, &body))
	require.Equal(t, true, body.Status["ready"])
	require.Equal(t, "", body.Status["nat"])
	_, hasV4IP := body.Status["v4Ip"]
	_, hasV6IP := body.Status["v6Ip"]
	_, hasMAC := body.Status["macAddress"]
	require.False(t, hasV4IP)
	require.False(t, hasV6IP)
	require.False(t, hasMAC)
}

func TestMakeOvnEipStatusPatchIncludesReadyAndNat(t *testing.T) {
	patch, err := makeOvnEipStatusPatch(true, util.SnatUsingEip)
	require.NoError(t, err)

	var body struct {
		Status kubeovnv1.OvnEipStatus `json:"status"`
	}
	require.NoError(t, json.Unmarshal(patch, &body))
	require.True(t, body.Status.Ready)
	require.Equal(t, util.SnatUsingEip, body.Status.Nat)
	require.Empty(t, body.Status.Type)
	require.Empty(t, body.Status.V4Ip)
	require.Empty(t, body.Status.V6Ip)
	require.Empty(t, body.Status.MacAddress)
}

func TestMakeOvnEipStatusPatchPreservesReadyState(t *testing.T) {
	patch, err := makeOvnEipStatusPatch(false, "")
	require.NoError(t, err)

	var body struct {
		Status map[string]any `json:"status"`
	}
	require.NoError(t, json.Unmarshal(patch, &body))
	_, hasReady := body.Status["ready"]
	require.False(t, hasReady)
}

// Keep the informer snapshot independent of the API tracker so status races are deterministic.
func newOvnEipStatusTestController(t *testing.T, cachedEip, apiEip *kubeovnv1.OvnEip) (*Controller, *kubeovnfake.Clientset) {
	t.Helper()
	client := kubeovnfake.NewSimpleClientset()
	if apiEip != nil {
		_, err := client.KubeovnV1().OvnEips().Create(t.Context(), apiEip, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if cachedEip != nil {
		require.NoError(t, indexer.Add(cachedEip.DeepCopy()))
	}
	emptyIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	c := &Controller{
		config:                  &Configuration{KubeOvnClient: client},
		ovnEipsLister:           kubeovnlisters.NewOvnEipLister(indexer),
		ovnDnatRulesLister:      kubeovnlisters.NewOvnDnatRuleLister(emptyIndexer),
		ovnFipsLister:           kubeovnlisters.NewOvnFipLister(emptyIndexer),
		ovnSnatRulesLister:      kubeovnlisters.NewOvnSnatRuleLister(emptyIndexer),
		updateSubnetStatusQueue: newTypedRateLimitingQueue[string]("UpdateSubnetStatus", nil),
	}
	t.Cleanup(c.updateSubnetStatusQueue.ShutDown)
	// Like the API server, discard status on creates of a resource with a status subresource.
	client.PrependReactor("create", "ovn-eips", func(action clienttesting.Action) (bool, runtime.Object, error) {
		eip := action.(clienttesting.CreateAction).GetObject().(*kubeovnv1.OvnEip).DeepCopy()
		eip.Status = kubeovnv1.OvnEipStatus{}
		err := client.Tracker().Create(action.GetResource(), eip, action.GetNamespace())
		return true, eip, err
	})
	return c, client
}

func TestCreateOrUpdateOvnEipCRPersistsAllocatedStatus(t *testing.T) {
	for _, tt := range []struct {
		name, v4, v6 string
	}{
		{name: "IPv4", v4: "172.19.0.17"},
		{name: "IPv6", v6: "fd00::17"},
		{name: "dual stack", v4: "172.19.0.17", v6: "fd00::17"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, client := newOvnEipStatusTestController(t, nil, nil)
			const mac = "fa:7c:96:7b:1a:58"
			require.NoError(t, c.createOrUpdateOvnEipCR("allocated-eip", "external", tt.v4, tt.v6, mac, util.OvnEipTypeNAT))
			eip, err := client.KubeovnV1().OvnEips().Get(t.Context(), "allocated-eip", metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, tt.v4, eip.Status.V4Ip)
			require.Equal(t, tt.v6, eip.Status.V6Ip)
			require.Equal(t, mac, eip.Status.MacAddress)
			require.Equal(t, util.OvnEipTypeNAT, eip.Status.Type)
		})
	}
}

func TestCreateOrUpdateOvnEipCRPreservesConcurrentReadyAndNat(t *testing.T) {
	c, client := newOvnEipStatusTestController(t, nil, nil)
	client.PrependReactor("patch", "ovn-eips", func(action clienttesting.Action) (bool, runtime.Object, error) {
		// The add worker finishes after Create, before the creator's allocation patch arrives.
		require.Equal(t, "status", action.GetSubresource())
		obj, err := client.Tracker().Get(action.GetResource(), "", "allocated-eip")
		require.NoError(t, err)
		eip := obj.(*kubeovnv1.OvnEip).DeepCopy()
		eip.Status.Ready = true
		eip.Status.Nat = util.SnatUsingEip
		require.NoError(t, client.Tracker().Update(action.GetResource(), eip, ""))
		return false, nil, nil
	})
	require.NoError(t, c.createOrUpdateOvnEipCR("allocated-eip", "external", "172.19.0.17", "", "fa:7c:96:7b:1a:58", util.OvnEipTypeLRP))
	eip, err := client.KubeovnV1().OvnEips().Get(t.Context(), "allocated-eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, eip.Status.Ready)
	require.Equal(t, util.SnatUsingEip, eip.Status.Nat)
	require.Equal(t, "172.19.0.17", eip.Status.V4Ip)
}

func TestHandleAddOvnEipWithAllocatedStatus(t *testing.T) {
	for _, tt := range []struct {
		name, usageType                          string
		terminating, allocated, ready, wantReady bool
	}{
		{name: "LRP becomes ready", usageType: util.OvnEipTypeLRP, allocated: true, wantReady: true},
		{name: "NAT becomes ready", usageType: util.OvnEipTypeNAT, allocated: true, wantReady: true},
		{name: "default NAT becomes ready", allocated: true, wantReady: true},
		{name: "LSP waits for node", usageType: util.OvnEipTypeLSP, allocated: true},
		{name: "ready LRP is unchanged", usageType: util.OvnEipTypeLRP, allocated: true, ready: true, wantReady: true},
		{name: "terminating LRP stays unready", usageType: util.OvnEipTypeLRP, allocated: true, terminating: true},
		{name: "terminating unallocated EIP is skipped", terminating: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			eip := &kubeovnv1.OvnEip{
				Name:   "allocated-eip",
				Spec:   kubeovnv1.OvnEipSpec{ExternalSubnet: "external", Type: tt.usageType, V4Ip: "172.19.0.17"},
				Status: kubeovnv1.OvnEipStatus{Ready: tt.ready},
			}
			if tt.allocated {
				eip.Status.V4Ip = eip.Spec.V4Ip
				eip.Status.MacAddress = "fa:7c:96:7b:1a:58"
				eip.Status.Type = tt.usageType
			}
			if tt.terminating {
				eip.DeletionTimestamp = new(metav1.Now())
			}
			c, client := newOvnEipStatusTestController(t, eip, eip)
			client.ClearActions()
			// No IPAM or OVN client is installed: an allocated EIP must not be allocated again.
			require.NoError(t, c.handleAddOvnEip(eip.Name))
			actions := client.Actions()
			updated, err := client.KubeovnV1().OvnEips().Get(t.Context(), eip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, tt.wantReady, updated.Status.Ready)
			if tt.wantReady && !tt.ready {
				require.Len(t, actions, 1)
				require.Equal(t, "patch", actions[0].GetVerb())
				require.Equal(t, "status", actions[0].GetSubresource())
			} else {
				require.Empty(t, actions)
			}
			expectedStatus := eip.Status
			expectedStatus.Ready = tt.wantReady
			require.Equal(t, expectedStatus, updated.Status)
		})
	}
}

func TestPatchOvnEipStatusDoesNotClearReadyOrAddress(t *testing.T) {
	for _, markReady := range []bool{false, true} {
		t.Run(map[bool]string{false: "NAT reset", true: "mark ready"}[markReady], func(t *testing.T) {
			cached := &kubeovnv1.OvnEip{
				Name:   "ready-eip",
				Spec:   kubeovnv1.OvnEipSpec{V4Ip: "172.19.0.17", V6Ip: "fd00::17"},
				Status: kubeovnv1.OvnEipStatus{Nat: util.SnatUsingEip},
			}
			apiEip := cached.DeepCopy()
			apiEip.Status = kubeovnv1.OvnEipStatus{
				Ready: true, Nat: util.SnatUsingEip, V4Ip: cached.Spec.V4Ip, V6Ip: cached.Spec.V6Ip,
				MacAddress: "fa:7c:96:7b:1a:58", Type: util.OvnEipTypeNAT,
			}
			c, client := newOvnEipStatusTestController(t, cached, apiEip)
			require.NoError(t, c.patchOvnEipStatus(cached.Name, markReady))
			eip, err := client.KubeovnV1().OvnEips().Get(t.Context(), cached.Name, metav1.GetOptions{})
			require.NoError(t, err)
			expectedStatus := apiEip.Status
			expectedStatus.Nat = ""
			require.Equal(t, expectedStatus, eip.Status)
		})
	}
}

func TestCreateOrUpdateOvnEipCRPatchesOnlyMissingAllocation(t *testing.T) {
	for _, missingField := range []string{"v4Ip", "v6Ip", "macAddress", "type", "none"} {
		t.Run(missingField, func(t *testing.T) {
			eip := &kubeovnv1.OvnEip{
				Name:   "allocated-eip",
				Spec:   kubeovnv1.OvnEipSpec{V4Ip: "172.19.0.17", V6Ip: "fd00::17", MacAddress: "fa:7c:96:7b:1a:58", Type: util.OvnEipTypeLRP},
				Status: kubeovnv1.OvnEipStatus{Ready: true, Nat: util.SnatUsingEip, V4Ip: "172.19.0.17", V6Ip: "fd00::17", MacAddress: "fa:7c:96:7b:1a:58", Type: util.OvnEipTypeLRP},
			}
			cached := eip.DeepCopy()
			fields := map[string]*string{
				"v4Ip": &cached.Status.V4Ip, "v6Ip": &cached.Status.V6Ip,
				"macAddress": &cached.Status.MacAddress, "type": &cached.Status.Type,
			}
			if missingField != "none" {
				*fields[missingField] = ""
			}
			cached.Status.Ready = false
			cached.Status.Nat = ""
			c, client := newOvnEipStatusTestController(t, cached, eip)
			client.ClearActions()
			require.NoError(t, c.createOrUpdateOvnEipCR(eip.Name, "external", eip.Spec.V4Ip, eip.Spec.V6Ip, eip.Spec.MacAddress, eip.Spec.Type))
			actions := client.Actions()
			if missingField == "none" {
				require.Empty(t, actions)
			} else {
				require.Len(t, actions, 1)
				require.Equal(t, "status", actions[0].GetSubresource())
				var body struct {
					Status map[string]any `json:"status"`
				}
				require.NoError(t, json.Unmarshal(actions[0].(clienttesting.PatchAction).GetPatch(), &body))
				require.Equal(t, map[string]any{missingField: *map[string]*string{
					"v4Ip": &eip.Status.V4Ip, "v6Ip": &eip.Status.V6Ip,
					"macAddress": &eip.Status.MacAddress, "type": &eip.Status.Type,
				}[missingField]}, body.Status)
			}
			updated, err := client.KubeovnV1().OvnEips().Get(t.Context(), eip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, eip.Status, updated.Status)
		})
	}
}

func Test_getOvnEipNat(t *testing.T) {
	// NAT rules always carry an eip_v4_ip label, so a pure-IPv6 rule still has
	// eip_v4_ip="" together with its own eip_v6_ip label.
	ipv6Dnat := &kubeovnv1.OvnDnatRule{
		Name: "dnat-v6",
		Labels: map[string]string{
			util.EipV4IpLabel: "",
			util.EipV6IpLabel: util.IPv6ToLabelValue("fc00:1::a"),
		},
	}
	ipv6Snat := &kubeovnv1.OvnSnatRule{
		Name: "snat-v6",
		Labels: map[string]string{
			util.EipV4IpLabel: "",
			util.EipV6IpLabel: util.IPv6ToLabelValue("fc00:1::a"),
		},
	}
	ipv4Dnat := &kubeovnv1.OvnDnatRule{
		Name: "dnat-v4",
		Labels: map[string]string{
			util.EipV4IpLabel: "192.168.0.5",
			util.EipV6IpLabel: "",
		},
	}

	// Regression: a pure-IPv6 EIP (V4Ip="") must not be considered "in use" by
	// an unrelated IPv6 NAT rule just because both carry an empty eip_v4_ip
	// label. Previously getOvnEipNat queried with {eip_v4_ip: ""}, matching
	// every IPv6-only NAT rule and blocking the EIP from ever being deleted.
	t.Run("pure IPv6 EIP does not match unrelated IPv6 NAT via empty v4 label", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv6Dnat},
			OvnSnatRules: []*kubeovnv1.OvnSnatRule{ipv6Snat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("", "fc00:1::7")
		require.NoError(t, err)
		require.Empty(t, nat)
	})

	t.Run("pure IPv6 EIP matches NAT rules that actually use its v6 ip", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv6Dnat},
			OvnSnatRules: []*kubeovnv1.OvnSnatRule{ipv6Snat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("", "fc00:1::a")
		require.NoError(t, err)
		require.Equal(t, util.DnatUsingEip+","+util.SnatUsingEip, nat)
	})

	t.Run("IPv4 EIP matches a NAT rule that uses its v4 ip", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv4Dnat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("192.168.0.5", "")
		require.NoError(t, err)
		require.Equal(t, util.DnatUsingEip, nat)
	})

	t.Run("IPv4 EIP does not match a different v4 NAT rule", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv4Dnat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("192.168.0.99", "")
		require.NoError(t, err)
		require.Empty(t, nat)
	})

	t.Run("EIP with no ip queries nothing", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv6Dnat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("", "")
		require.NoError(t, err)
		require.Empty(t, nat)
	})
}

// assertEnqueueAddRoutingWithFinalizer checks the OVN add-path routing: a live object goes to the
// add queue, a terminating object with a finalizer goes to the update queue (deletion cleanup), and
// a terminating object whose finalizer is already gone is skipped (cleanup done, awaiting deletion).
func assertEnqueueAddRoutingWithFinalizer(
	t *testing.T,
	addQueue, updateQueue workqueue.TypedRateLimitingInterface[string],
	enqueue func(any),
	live, terminatingWithFinalizer, terminatingNoFinalizer any,
) {
	t.Helper()
	enqueue(live)
	require.Equal(t, 1, addQueue.Len(), "live object should go to the add queue")
	require.Equal(t, 0, updateQueue.Len(), "live object must not go to the update queue")

	enqueue(terminatingWithFinalizer)
	require.Equal(t, 1, addQueue.Len(), "terminating object must not go to the add queue")
	require.Equal(t, 1, updateQueue.Len(), "terminating object with finalizer should go to the update queue")

	enqueue(terminatingNoFinalizer)
	require.Equal(t, 1, addQueue.Len(), "terminating object without finalizer must not go to the add queue")
	require.Equal(t, 1, updateQueue.Len(), "terminating object without finalizer must not be re-enqueued")
}

func TestEnqueueAddOvnEip(t *testing.T) {
	t.Parallel()
	c := &Controller{
		config:            &Configuration{},
		addOvnEipQueue:    newTypedRateLimitingQueue[string]("AddOvnEip", nil),
		updateOvnEipQueue: newTypedRateLimitingQueue[string]("UpdateOvnEip", nil),
	}
	t.Cleanup(c.addOvnEipQueue.ShutDown)
	t.Cleanup(c.updateOvnEipQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(
		t, c.addOvnEipQueue, c.updateOvnEipQueue, c.enqueueAddOvnEip,
		&kubeovnv1.OvnEip{Name: "live-eip"},
		&kubeovnv1.OvnEip{Name: "terminating-eip", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnEip{Name: "terminating-eip-no-finalizer", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddOvnFip(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addOvnFipQueue:    newTypedRateLimitingQueue[string]("AddOvnFip", nil),
		updateOvnFipQueue: newTypedRateLimitingQueue[string]("UpdateOvnFip", nil),
	}
	t.Cleanup(c.addOvnFipQueue.ShutDown)
	t.Cleanup(c.updateOvnFipQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(
		t, c.addOvnFipQueue, c.updateOvnFipQueue, c.enqueueAddOvnFip,
		&kubeovnv1.OvnFip{Name: "live-fip"},
		&kubeovnv1.OvnFip{Name: "terminating-fip", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnFip{Name: "terminating-fip-no-finalizer", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddOvnDnatRule(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addOvnDnatRuleQueue:    newTypedRateLimitingQueue[string]("AddOvnDnat", nil),
		updateOvnDnatRuleQueue: newTypedRateLimitingQueue[string]("UpdateOvnDnat", nil),
	}
	t.Cleanup(c.addOvnDnatRuleQueue.ShutDown)
	t.Cleanup(c.updateOvnDnatRuleQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(
		t, c.addOvnDnatRuleQueue, c.updateOvnDnatRuleQueue, c.enqueueAddOvnDnatRule,
		&kubeovnv1.OvnDnatRule{Name: "live-dnat"},
		&kubeovnv1.OvnDnatRule{Name: "terminating-dnat", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnDnatRule{Name: "terminating-dnat-no-finalizer", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddOvnSnatRule(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addOvnSnatRuleQueue:    newTypedRateLimitingQueue[string]("AddOvnSnat", nil),
		updateOvnSnatRuleQueue: newTypedRateLimitingQueue[string]("UpdateOvnSnat", nil),
	}
	t.Cleanup(c.addOvnSnatRuleQueue.ShutDown)
	t.Cleanup(c.updateOvnSnatRuleQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(
		t, c.addOvnSnatRuleQueue, c.updateOvnSnatRuleQueue, c.enqueueAddOvnSnatRule,
		&kubeovnv1.OvnSnatRule{Name: "live-snat"},
		&kubeovnv1.OvnSnatRule{Name: "terminating-snat", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnSnatRule{Name: "terminating-snat-no-finalizer", DeletionTimestamp: &now},
	)
}

// TestEnqueueAddOvnEipRequeuesRouterLBRules verifies that enqueueAddOvnEip re-queues an EIP's
// RouterLBRules even when the EIP is terminating (the side effect must run before the terminating
// route returns, so LB rules react to the EIP going away).
func TestEnqueueAddOvnEipRequeuesRouterLBRules(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		RouterLBRules: []*kubeovnv1.RouterLBRule{{
			Name: "rlr1",
			Spec: kubeovnv1.RouterLBRuleSpec{OvnEip: "eip1"},
		}},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableOvnLB = true
	c.addRouterLBRuleQueue = newTypedRateLimitingQueue[string]("AddRouterLBRule", nil)
	c.addOvnEipQueue = newTypedRateLimitingQueue[string]("AddOvnEip", nil)
	c.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(c.addRouterLBRuleQueue.ShutDown)
	t.Cleanup(c.addOvnEipQueue.ShutDown)
	t.Cleanup(c.updateOvnEipQueue.ShutDown)

	now := metav1.Now()
	c.enqueueAddOvnEip(&kubeovnv1.OvnEip{
		Name: "eip1", DeletionTimestamp: &now, Finalizers: []string{util.KubeOVNControllerFinalizer},
	})

	require.Equal(t, 1, c.updateOvnEipQueue.Len(), "terminating eip with finalizer goes to the update queue")
	require.Equal(t, 0, c.addOvnEipQueue.Len(), "terminating eip must not go to the add queue")
	require.Equal(t, 1, c.addRouterLBRuleQueue.Len(), "router lb rules must be re-queued even for a terminating eip")
}

func TestHandleDelOvnEip_BlocksWhileNatStillUsesIt(t *testing.T) {
	t.Parallel()

	eip := &kubeovnv1.OvnEip{
		Name: "test-eip",
		Spec: kubeovnv1.OvnEipSpec{ExternalSubnet: "pubnet", V4Ip: "10.0.0.10"},
	}
	fip := &kubeovnv1.OvnFip{
		Name:   "test-fip",
		Labels: map[string]string{util.EipV4IpLabel: eip.Spec.V4Ip},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		OvnEips:     []*kubeovnv1.OvnEip{eip},
		OvnFipRules: []*kubeovnv1.OvnFip{fip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	require.Error(t, ctrl.handleDelOvnEip(eip))
}

func TestHandleDelOvnEip_ReleasesWhenNoNatRemains(t *testing.T) {
	t.Parallel()

	eip := &kubeovnv1.OvnEip{
		Name: "test-eip",
		Spec: kubeovnv1.OvnEipSpec{ExternalSubnet: "pubnet", V4Ip: "10.0.0.10"},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{OvnEips: []*kubeovnv1.OvnEip{eip}})
	require.NoError(t, err)
	ctrl := fc.fakeController

	require.NoError(t, ctrl.handleDelOvnEip(eip))
}
