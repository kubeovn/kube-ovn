package controller

import (
	"errors"
	"testing"

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

// TestInitNodeChassisGatesGcChassis covers the nodeChassisInitialized gate:
// it opens only after a full pass over every node, tolerating a chassis that
// isn't registered yet, and stays closed on any other failure so gcChassis
// keeps skipping. It also covers the flush that runs immediately once the
// gate opens, so a reactive gcChassis call that no-op'd during the loop
// (chassis not registered yet) doesn't sit stale until the next periodic
// tick - see TestInitNodeChassisFlushesPendingGcAfterGateOpens for a case
// where that flush actually has something to clean up.
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
// a separate periodic tick.
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
