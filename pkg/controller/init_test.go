package controller

import (
	"encoding/json"
	"errors"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHasAllocatedAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name: "default provider allocated",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
		{
			name: "custom provider allocated",
			annotations: map[string]string{
				"my-provider.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
		{
			name: "allocated is false",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated": "false",
			},
			expected: false,
		},
		{
			name: "unrelated annotations only",
			annotations: map[string]string{
				"app":                          "test",
				"ovn.kubernetes.io/ip_address": "10.0.0.1",
			},
			expected: false,
		},
		{
			name: "multiple providers with one allocated",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated":         "false",
				"my-provider.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				Annotations: tt.annotations,
			}
			require.Equal(t, tt.expected, hasAllocatedAnnotation(pod))
		})
	}
}

// TestBuildVpcLBStatusPatch_PreservesInitDefaultVpcFields is the regression
// guard for the bootstrap deadlock fix: initLoadBalancer's status patch must
// only set the six LB-name fields, and must never carry Standby/Default/
// Router/DefaultLogicalSwitch. If someone reintroduces a whole-VpcStatus
// serialization here, the merge-patch body would overwrite fields that
// InitDefaultVpc wrote moments earlier, reproducing the race that bricked
// fresh-install HA E2E runs.
func TestBuildVpcLBStatusPatch_PreservesInitDefaultVpcFields(t *testing.T) {
	t.Parallel()

	vpcLb := &VpcLoadBalancer{
		TCPLoadBalancer:      "cluster-tcp-loadbalancer",
		TCPSessLoadBalancer:  "cluster-tcp-session-loadbalancer",
		UDPLoadBalancer:      "cluster-udp-loadbalancer",
		UDPSessLoadBalancer:  "cluster-udp-session-loadbalancer",
		SctpLoadBalancer:     "cluster-sctp-loadbalancer",
		SctpSessLoadBalancer: "cluster-sctp-session-loadbalancer",
	}

	body, err := buildVpcLBStatusPatch(vpcLb)
	require.NoError(t, err)

	// The patch body must not reference any non-LB field. A regression that
	// reintroduces vpc.Status.Bytes() would immediately trip these asserts
	// because VpcStatus has non-omitempty booleans/strings that always get
	// serialized.
	var raw struct {
		Status map[string]json.RawMessage `json:"status"`
	}
	require.NoError(t, json.Unmarshal(body, &raw))
	require.ElementsMatch(t,
		[]string{
			"tcpLoadBalancer", "tcpSessionLoadBalancer",
			"udpLoadBalancer", "udpSessionLoadBalancer",
			"sctpLoadBalancer", "sctpSessionLoadBalancer",
		},
		keysOf(raw.Status),
	)

	// Simulate the etcd state right after InitDefaultVpc UpdateStatus and
	// verify the merge patch keeps Standby/Default/Router/DefaultLogicalSwitch
	// intact while writing the six LB fields.
	target, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": "ovn-cluster"},
		"status": map[string]any{
			"standby":              true,
			"default":              true,
			"router":               "ovn-cluster",
			"defaultLogicalSwitch": "ovn-default",
		},
	})
	require.NoError(t, err)

	merged, err := jsonpatch.MergePatch(target, body)
	require.NoError(t, err)

	var got struct {
		Status map[string]any `json:"status"`
	}
	require.NoError(t, json.Unmarshal(merged, &got))

	require.Equal(t, true, got.Status["standby"], "Standby must survive the LB patch")
	require.Equal(t, true, got.Status["default"], "Default must survive the LB patch")
	require.Equal(t, "ovn-cluster", got.Status["router"], "Router must survive the LB patch")
	require.Equal(t, "ovn-default", got.Status["defaultLogicalSwitch"], "DefaultLogicalSwitch must survive the LB patch")

	require.Equal(t, vpcLb.TCPLoadBalancer, got.Status["tcpLoadBalancer"])
	require.Equal(t, vpcLb.TCPSessLoadBalancer, got.Status["tcpSessionLoadBalancer"])
	require.Equal(t, vpcLb.UDPLoadBalancer, got.Status["udpLoadBalancer"])
	require.Equal(t, vpcLb.UDPSessLoadBalancer, got.Status["udpSessionLoadBalancer"])
	require.Equal(t, vpcLb.SctpLoadBalancer, got.Status["sctpLoadBalancer"])
	require.Equal(t, vpcLb.SctpSessLoadBalancer, got.Status["sctpSessionLoadBalancer"])
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestInitNodeChassisGatesGcChassis covers the nodeChassisInitialized gate:
// it opens only after a full pass over every node, tolerating a chassis that
// isn't registered yet, and stays closed on any other failure so gcChassis
// keeps skipping. It also covers the flush that runs immediately once the
// gate opens, so a reactive gcChassis call that no-op'd during the loop
// (chassis not registered yet) doesn't sit stale until the next periodic
// tick - see TestInitNodeChassisFlushesPendingGcAfterGateOpens for a case
// where that flush actually has something to clean up. The fake controller
// defaults to GC enabled (GCInterval != 0), since this test is about the
// gate, not the GCInterval contract - see
// TestInitNodeChassisSkipsFlushWhenGcDisabled for that.
func TestInitNodeChassisGatesGcChassis(t *testing.T) {
	errBoom := errors.New("boom")
	kubeOvnTag := map[string]string{"vendor": util.CniTypeName}
	annotatedNode := &v1.Node{Name: "node-1", Annotations: map[string]string{util.ChassisAnnotation: "chassis-1"}}

	tests := []struct {
		name     string
		nodes    []*v1.Node
		setup    func(*mockovs.MockSbClient)
		wantErr  bool
		wantGate bool
	}{
		{
			name:  "clean pass opens the gate",
			nodes: []*v1.Node{annotatedNode, {Name: "node-not-ready"}},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().GetChassis("chassis-1", true).Return(&ovnsb.Chassis{Name: "chassis-1", Hostname: "node-1", ExternalIDs: kubeOvnTag}, nil)
				// Gate-open flush: chassis-1 is already claimed by its live,
				// correctly-annotated node, so nothing more happens.
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-1", Hostname: "node-1", ExternalIDs: kubeOvnTag}}, nil)
			},
			wantGate: true,
		},
		{
			name:  "chassis not registered yet is tolerated and opens the gate",
			nodes: []*v1.Node{annotatedNode},
			setup: func(m *mockovs.MockSbClient) {
				// The reactive gcChassis inside UpdateChassisTag no-ops
				// because the gate is still closed, so no ListChassis here.
				m.EXPECT().GetChassis("chassis-1", true).Return(nil, nil)
				// Gate-open flush: chassis-1 still isn't registered, so the
				// flush's own ListChassis legitimately comes back empty too.
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{}, nil)
			},
			wantGate: true,
		},
		{
			name:  "chassis tag error keeps the gate closed",
			nodes: []*v1.Node{annotatedNode},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().GetChassis("chassis-1", true).Return(nil, errBoom)
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: tt.nodes})
			require.NoError(t, err)
			ctrl := fakeController.fakeController
			ctrl.nodeChassisInitialized.Store(false)
			tt.setup(fakeController.mockOvnSbClient)

			err = ctrl.initNodeChassis()
			if tt.wantErr {
				require.ErrorIs(t, err, errBoom)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantGate, ctrl.nodeChassisInitialized.Load())
		})
	}
}

// TestInitNodeChassisFlushesPendingGcAfterGateOpens covers a reviewer finding:
// node.go's UpdateChassisTag calls gcChassis reactively when a node's
// ChassisAnnotation names a chassis that isn't registered, but gcChassis
// no-ops while nodeChassisInitialized is still false - which it always is
// during initNodeChassis's own node loop, since the gate only opens after
// that loop finishes. Without a flush right after the gate opens, that
// reactive cleanup (and any periodic tick that fired during the same window)
// would stay stranded until the next periodic tick, GCInterval later (360s
// by default) instead of running immediately.
//
// node-1 triggers exactly that reactive no-op (its annotation names a
// not-yet-registered chassis). A second, unrelated orphan chassis
// (chassis-orphan, belonging to a node that no longer exists) is only
// cleaned up if the post-loop flush actually runs a real gcChassis pass -
// proving the flush happened here, synchronously, rather than being left for
// a separate periodic tick. The fake controller defaults to GC enabled
// (GCInterval != 0), since this test is about the flush timing, not the
// GCInterval contract - see TestInitNodeChassisSkipsFlushWhenGcDisabled for
// that.
func TestInitNodeChassisFlushesPendingGcAfterGateOpens(t *testing.T) {
	kubeOvnTag := map[string]string{"vendor": util.CniTypeName}
	nodes := []*v1.Node{
		{Name: "node-1", Annotations: map[string]string{util.ChassisAnnotation: "chassis-missing"}},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: nodes})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.nodeChassisInitialized.Store(false)
	m := fakeController.mockOvnSbClient

	// node-1's own loop iteration: chassis-missing isn't registered yet, so
	// UpdateChassisTag calls gcChassis reactively; the gate is still closed
	// at this point, so it must no-op (no ListChassis/DeleteChassis here).
	m.EXPECT().GetChassis("chassis-missing", true).Return(nil, nil)

	// The flush after the gate opens: a real gcChassis pass that finds and
	// deletes the unrelated orphan, proving it ran synchronously instead of
	// being deferred to the next periodic tick.
	m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
		{Name: "chassis-orphan", Hostname: "deleted-node", ExternalIDs: kubeOvnTag},
	}, nil)
	m.EXPECT().DeleteChassis("chassis-orphan").Return(nil)

	err = ctrl.initNodeChassis()
	require.NoError(t, err)
	require.True(t, ctrl.nodeChassisInitialized.Load())
}

// TestInitNodeChassisSkipsFlushWhenGcDisabled covers a reviewer finding:
// pkg/controller/config.go documents GCInterval=0 as disabling GC entirely,
// and gc() honors that contract, but initNodeChassis's post-loop flush
// called gcChassis directly with no such guard - so a controller started
// with --gc-interval=0 would still list and could delete stale chassis
// during startup initialization. gcChassis itself now checks GCInterval==0
// first, before even consulting nodeChassisInitialized, so this flush (and
// every other gcChassis call site) no-ops instead.
//
// node-1's annotation names a chassis that was never registered - exactly
// the condition that fed a real gcChassis pass in
// TestInitNodeChassisFlushesPendingGcAfterGateOpens - but with GC disabled
// neither the reactive call from UpdateChassisTag nor the post-loop flush
// may call ListChassis or DeleteChassis.
func TestInitNodeChassisSkipsFlushWhenGcDisabled(t *testing.T) {
	nodes := []*v1.Node{
		{Name: "node-1", Annotations: map[string]string{util.ChassisAnnotation: "chassis-missing"}},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: nodes})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.nodeChassisInitialized.Store(false)
	ctrl.config.GCInterval = 0
	m := fakeController.mockOvnSbClient

	m.EXPECT().GetChassis("chassis-missing", true).Return(nil, nil)
	m.EXPECT().ListChassis().Times(0)
	m.EXPECT().DeleteChassis(gomock.Any()).Times(0)

	err = ctrl.initNodeChassis()
	require.NoError(t, err)
	require.True(t, ctrl.nodeChassisInitialized.Load())
}
