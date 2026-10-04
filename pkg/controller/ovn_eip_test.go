package controller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

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
