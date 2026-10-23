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
// keeps skipping.
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
				m.EXPECT().GetKubeOvnChassises().Return(&[]ovnsb.Chassis{{Name: "chassis-1", Hostname: "node-1"}}, nil)
				m.EXPECT().GetChassis("chassis-1", true).Return(&ovnsb.Chassis{Name: "chassis-1", Hostname: "node-1", ExternalIDs: kubeOvnTag}, nil)
			},
			wantGate: true,
		},
		{
			name:  "chassis not registered yet is tolerated and opens the gate",
			nodes: []*v1.Node{annotatedNode},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().GetKubeOvnChassises().Return(&[]ovnsb.Chassis{}, nil)
				// The reactive gcChassis inside UpdateChassisTag no-ops
				// because the gate is still closed, so no ListChassis.
				m.EXPECT().GetChassis("chassis-1", true).Return(nil, nil)
			},
			wantGate: true,
		},
		{
			name:  "chassis tag error keeps the gate closed",
			nodes: []*v1.Node{annotatedNode},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().GetKubeOvnChassises().Return(&[]ovnsb.Chassis{}, nil)
				m.EXPECT().GetChassis("chassis-1", true).Return(nil, errBoom)
			},
			wantErr: true,
		},
		{
			name: "list kube-ovn chassis error keeps the gate closed",
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().GetKubeOvnChassises().Return(nil, errBoom)
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
