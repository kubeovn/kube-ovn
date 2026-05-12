package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ovs "github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestKubeOvnAnnotationsChanged(t *testing.T) {
	tests := []struct {
		name           string
		oldAnnotations map[string]string
		newAnnotations map[string]string
		expected       bool
	}{
		{
			name:           "no annotations",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{},
			expected:       false,
		},
		{
			name:           "kube-ovn annotation added",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{
				util.AllocatedAnnotation: "true",
			},
			expected: true,
		},
		{
			name: "kube-ovn annotation removed",
			oldAnnotations: map[string]string{
				util.AllocatedAnnotation: "true",
			},
			newAnnotations: map[string]string{},
			expected:       true,
		},
		{
			name: "kube-ovn annotation value changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.2",
			},
			expected: true,
		},
		{
			name: "kube-ovn annotation unchanged",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
			},
			expected: false,
		},
		{
			name: "non-kube-ovn annotation changed",
			oldAnnotations: map[string]string{
				"other.io/annotation": "value1",
			},
			newAnnotations: map[string]string{
				"other.io/annotation": "value2",
			},
			expected: false,
		},
		{
			name: "mixed annotations, only non-kube-ovn changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"other.io/annotation":    "value1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"other.io/annotation":    "value2",
			},
			expected: false,
		},
		{
			name: "mixed annotations, kube-ovn changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"other.io/annotation":    "value1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.2",
				"other.io/annotation":    "value2",
			},
			expected: true,
		},
		{
			name: "multiple kube-ovn annotations unchanged",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
				util.AllocatedAnnotation:  "true",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
				util.AllocatedAnnotation:  "true",
			},
			expected: false,
		},
		{
			name: "multiple kube-ovn annotations, one changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:56",
			},
			expected: true,
		},
		{
			name: "provider network annotation changed",
			oldAnnotations: map[string]string{
				fmt.Sprintf(util.ProviderNetworkTemplate, "net1"): "provider1",
			},
			newAnnotations: map[string]string{
				fmt.Sprintf(util.ProviderNetworkTemplate, "net1"): "provider2",
			},
			expected: true,
		},
		{
			name: "annotation with kubernetes.io in value not key",
			oldAnnotations: map[string]string{
				"some.annotation": "value.kubernetes.io",
			},
			newAnnotations: map[string]string{
				"some.annotation": "changed.kubernetes.io",
			},
			expected: false,
		},
		{
			name:           "empty to kube-ovn annotations",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
				util.ChassisAnnotation:    "node1",
			},
			expected: true,
		},
		{
			name: "kube-ovn annotations to empty",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
			},
			newAnnotations: map[string]string{},
			expected:       true,
		},
		{
			name: "non-kube-ovn added and removed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"old.annotation":         "value",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"new.annotation":         "value",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kubeOvnAnnotationsChanged(tt.oldAnnotations, tt.newAnnotations)
			if result != tt.expected {
				t.Errorf("kubeOvnAnnotationsChanged() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCleanDuplicatedChassis(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
	}

	t.Run("single chassis exists, no cleanup needed", func(t *testing.T) {
		fakeCtrl := newFakeController(t)
		ctrl := fakeCtrl.fakeController
		mockSb := fakeCtrl.mockOvnSbClient

		mockSb.EXPECT().GetChassisByHost("test-node").Return(&ovnsb.Chassis{Name: "chassis-1"}, nil)

		err := ctrl.cleanDuplicatedChassis(node)
		require.NoError(t, err)
	})

	t.Run("multiple chassis detected, cleanup succeeds", func(t *testing.T) {
		fakeCtrl := newFakeController(t)
		ctrl := fakeCtrl.fakeController
		mockSb := fakeCtrl.mockOvnSbClient

		mockSb.EXPECT().GetChassisByHost("test-node").Return(nil, ovs.ErrOneNodeMultiChassis)
		mockSb.EXPECT().DeleteChassisByHost("test-node").Return(nil)

		err := ctrl.cleanDuplicatedChassis(node)
		require.NoError(t, err)
	})

	t.Run("multiple chassis detected, cleanup fails", func(t *testing.T) {
		fakeCtrl := newFakeController(t)
		ctrl := fakeCtrl.fakeController
		mockSb := fakeCtrl.mockOvnSbClient

		mockSb.EXPECT().GetChassisByHost("test-node").Return(nil, ovs.ErrOneNodeMultiChassis)
		mockSb.EXPECT().DeleteChassisByHost("test-node").Return(errors.New("delete failed"))

		err := ctrl.cleanDuplicatedChassis(node)
		require.ErrorContains(t, err, "delete failed")
	})

	t.Run("non-multi-chassis error is propagated", func(t *testing.T) {
		fakeCtrl := newFakeController(t)
		ctrl := fakeCtrl.fakeController
		mockSb := fakeCtrl.mockOvnSbClient

		mockSb.EXPECT().GetChassisByHost("test-node").Return(nil, errors.New("connection refused"))
		// DeleteChassisByHost should NOT be called
		mockSb.EXPECT().DeleteChassisByHost(gomock.Any()).Times(0)

		err := ctrl.cleanDuplicatedChassis(node)
		require.ErrorContains(t, err, "connection refused")
	})
}

func TestCheckAndUpdateNodePortGroup_EmptyPgName(t *testing.T) {
	initializedNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "initialized-node",
			Annotations: map[string]string{
				util.PortNameAnnotation:  "node-initialized-node",
				util.IPAddressAnnotation: "100.64.0.2",
			},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.1"},
			},
		},
	}
	uninitializedNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "uninitialized-node",
			Annotations: map[string]string{},
		},
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{initializedNode, uninitializedNode},
	})
	require.NoError(t, err)

	ctrl := fakeCtrl.fakeController
	ctrl.config.EnableNP = false
	mockNb := fakeCtrl.mockOvnClient

	// The initialized node should still be processed; the uninitialized node should be skipped.
	mockNb.EXPECT().PortGroupSetPorts("node.initialized.node", gomock.Any()).Return(nil)
	mockNb.EXPECT().DeleteAcls("node.initialized.node", portGroupKey, "", nil).Return(nil)

	// PortGroupSetPorts should NOT be called for empty pgName
	mockNb.EXPECT().PortGroupSetPorts("", gomock.Any()).Times(0)

	err = ctrl.checkAndUpdateNodePortGroup()
	require.NoError(t, err)
}

func TestGetPolicyRouteParams_ClonedExternalIDs(t *testing.T) {
	fakeCtrl := newFakeController(t)
	ctrl := fakeCtrl.fakeController
	mockNb := fakeCtrl.mockOvnClient

	originalExternalIDs := map[string]string{
		"node-1": "10.0.0.1",
		"node-2": "10.0.0.2",
	}

	policy := &ovnnb.LogicalRouterPolicy{
		ExternalIDs: originalExternalIDs,
		Nexthops:    []string{"10.0.0.1", "10.0.0.2"},
	}

	mockNb.EXPECT().
		GetLogicalRouterPolicy(ctrl.config.ClusterRouter, util.GatewayRouterPolicyPriority, "ip4.src == 10.244.0.0/16", true).
		Return([]*ovnnb.LogicalRouterPolicy{policy}, nil)

	_, returnedMap, err := ctrl.getPolicyRouteParams("10.244.0.0/16", util.GatewayRouterPolicyPriority)
	require.NoError(t, err)

	// Mutate the returned map (as callers do)
	delete(returnedMap, "node-1")

	// The original ExternalIDs in the policy object must remain unchanged
	require.Equal(t, map[string]string{
		"node-1": "10.0.0.1",
		"node-2": "10.0.0.2",
	}, originalExternalIDs)
	require.Contains(t, policy.ExternalIDs, "node-1")
}

func TestEnqueueUpdateNode(t *testing.T) {
	makeNode := func(readyStatus corev1.ConditionStatus, labels, annotations map[string]string, addresses []corev1.NodeAddress) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-node",
				Labels:      maps.Clone(labels),
				Annotations: maps.Clone(annotations),
			},
			Status: corev1.NodeStatus{
				Addresses: append([]corev1.NodeAddress(nil), addresses...),
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: readyStatus},
				},
			},
		}
	}

	baseAddresses := []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}}
	newAddresses := []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.2"}}
	baseLabels := map[string]string{"kube-ovn/role": "master"}
	baseAnnotations := map[string]string{util.AllocatedAnnotation: "true"}

	tests := []struct {
		name        string
		oldNode     *corev1.Node
		newNode     *corev1.Node
		expectQueue bool // true = something should be enqueued
	}{
		{
			name:        "no change",
			oldNode:     makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, baseAddresses),
			newNode:     makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, baseAddresses),
			expectQueue: false,
		},
		{
			name:        "readiness changed",
			oldNode:     makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, baseAddresses),
			newNode:     makeNode(corev1.ConditionFalse, baseLabels, baseAnnotations, baseAddresses),
			expectQueue: true,
		},
		{
			name:        "label changed",
			oldNode:     makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, baseAddresses),
			newNode:     makeNode(corev1.ConditionTrue, map[string]string{"kube-ovn/role": "worker"}, baseAnnotations, baseAddresses),
			expectQueue: true,
		},
		{
			name:        "address changed in-place",
			oldNode:     makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, baseAddresses),
			newNode:     makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, newAddresses),
			expectQueue: true,
		},
		{
			name: "address removed",
			oldNode: makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
				{Type: corev1.NodeInternalIP, Address: "fd00::1"},
			}),
			newNode: makeNode(corev1.ConditionTrue, baseLabels, baseAnnotations, []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
			}),
			expectQueue: true,
		},
		{
			name: "annotation changed",
			oldNode: makeNode(corev1.ConditionTrue, baseLabels, map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				util.AllocatedAnnotation: "true",
			}, baseAddresses),
			newNode: makeNode(corev1.ConditionTrue, baseLabels, map[string]string{
				util.IPAddressAnnotation: "10.0.0.2",
				util.AllocatedAnnotation: "true",
			}, baseAddresses),
			expectQueue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCtrl := newFakeController(t)
			ctrl := fakeCtrl.fakeController
			ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
			ctrl.updateNodeQueue = newTypedRateLimitingQueue[string]("UpdateNode", nil)
			t.Cleanup(func() {
				ctrl.addNodeQueue.ShutDown()
				ctrl.updateNodeQueue.ShutDown()
			})

			ctrl.enqueueUpdateNode(tt.oldNode, tt.newNode)

			totalLen := ctrl.addNodeQueue.Len() + ctrl.updateNodeQueue.Len()
			if tt.expectQueue {
				require.Equal(t, 1, totalLen, "expected one item to be enqueued")
			} else {
				require.Equal(t, 0, totalLen, "expected no items to be enqueued")
			}
		})
	}
}

func TestReconcileMasterNodeIPs(t *testing.T) {
	const ns = metav1.NamespaceSystem

	makeDeployment := func(name, containerName, envName, envValue string) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Env:  []corev1.EnvVar{{Name: envName, Value: envValue}},
						}},
					},
				},
			},
		}
	}

	makeDaemonSet := func(name, containerName, envName, envValue string) *appsv1.DaemonSet {
		return &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Env:  []corev1.EnvVar{{Name: envName, Value: envValue}},
						}},
					},
				},
			},
		}
	}

	masterNode := func(name, ip string) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"kube-ovn/role": "master"},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ip}},
			},
		}
	}

	t.Run("patches stale value", func(t *testing.T) {
		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{masterNode("node1", "10.0.0.2")},
		})
		require.NoError(t, err)
		ctrl := fakeCtrl.fakeController
		kubeClient := ctrl.config.KubeClient

		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("ovn-central", "ovn-central", "NODE_IPS", "10.0.0.1"), metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("kube-ovn-controller", "kube-ovn-controller", "OVN_DB_IPS", "10.0.0.1"), metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = kubeClient.AppsV1().DaemonSets(ns).Create(context.Background(),
			makeDaemonSet("ovs-ovn", "openvswitch", "OVN_DB_IPS", "10.0.0.1"), metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, ctrl.reconcileMasterNodeIPs(context.Background()))

		dep, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "10.0.0.2", dep.Spec.Template.Spec.Containers[0].Env[0].Value)

		dep, err = kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "kube-ovn-controller", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "10.0.0.2", dep.Spec.Template.Spec.Containers[0].Env[0].Value)

		ds, err := kubeClient.AppsV1().DaemonSets(ns).Get(context.Background(), "ovs-ovn", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "10.0.0.2", ds.Spec.Template.Spec.Containers[0].Env[0].Value)
	})

	t.Run("patches all workloads across 3-node cluster", func(t *testing.T) {
		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{
				masterNode("node1", "10.0.0.3"),
				masterNode("node2", "10.0.0.1"),
				masterNode("node3", "10.0.0.2"),
			},
		})
		require.NoError(t, err)
		ctrl := fakeCtrl.fakeController
		kubeClient := ctrl.config.KubeClient

		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("ovn-central", "ovn-central", "NODE_IPS", "stale"), metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("kube-ovn-controller", "kube-ovn-controller", "OVN_DB_IPS", "stale"), metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = kubeClient.AppsV1().DaemonSets(ns).Create(context.Background(),
			makeDaemonSet("ovs-ovn", "openvswitch", "OVN_DB_IPS", "stale"), metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, ctrl.reconcileMasterNodeIPs(context.Background()))

		wantIPs := "10.0.0.1,10.0.0.2,10.0.0.3"
		dep, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, wantIPs, dep.Spec.Template.Spec.Containers[0].Env[0].Value)

		dep, err = kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "kube-ovn-controller", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, wantIPs, dep.Spec.Template.Spec.Containers[0].Env[0].Value)

		ds, err := kubeClient.AppsV1().DaemonSets(ns).Get(context.Background(), "ovs-ovn", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, wantIPs, ds.Spec.Template.Spec.Containers[0].Env[0].Value)
	})

	t.Run("skips patch when value already current", func(t *testing.T) {
		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{masterNode("node1", "10.0.0.1")},
		})
		require.NoError(t, err)
		ctrl := fakeCtrl.fakeController
		kubeClient := ctrl.config.KubeClient

		dep := makeDeployment("ovn-central", "ovn-central", "NODE_IPS", "10.0.0.1")
		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(), dep, metav1.CreateOptions{})
		require.NoError(t, err)

		// Capture the resource version before reconcile
		before, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)

		require.NoError(t, ctrl.reconcileMasterNodeIPs(context.Background()))

		after, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, before.ResourceVersion, after.ResourceVersion, "no patch should have been issued")
	})

	t.Run("skips absent workloads (e.g. ovs-ovn-dpdk not deployed)", func(t *testing.T) {
		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{masterNode("node1", "10.0.0.1")},
		})
		require.NoError(t, err)
		ctrl := fakeCtrl.fakeController
		kubeClient := ctrl.config.KubeClient

		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("ovn-central", "ovn-central", "NODE_IPS", "stale"), metav1.CreateOptions{})
		require.NoError(t, err)

		// No ovs-ovn-dpdk present — reconcile should not error, and present workloads should still be patched
		require.NoError(t, ctrl.reconcileMasterNodeIPs(context.Background()))

		dep, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", dep.Spec.Template.Spec.Containers[0].Env[0].Value)
	})

	t.Run("sorts IPs v4 before v6", func(t *testing.T) {
		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"kube-ovn/role": "master"}},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
							{Type: corev1.NodeInternalIP, Address: "fd00::2"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"kube-ovn/role": "master"}},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
							{Type: corev1.NodeInternalIP, Address: "fd00::1"},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		ctrl := fakeCtrl.fakeController
		kubeClient := ctrl.config.KubeClient

		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("ovn-central", "ovn-central", "NODE_IPS", "stale"), metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, ctrl.reconcileMasterNodeIPs(context.Background()))

		dep, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)
		// v4 IPs sorted first, then v6 IPs sorted
		require.Equal(t, "10.0.0.1,10.0.0.2,fd00::1,fd00::2", dep.Spec.Template.Spec.Containers[0].Env[0].Value)
	})

	t.Run("skips all patches when no master nodes found", func(t *testing.T) {
		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}}, // no master label
			},
		})
		require.NoError(t, err)
		ctrl := fakeCtrl.fakeController
		kubeClient := ctrl.config.KubeClient

		_, err = kubeClient.AppsV1().Deployments(ns).Create(context.Background(),
			makeDeployment("ovn-central", "ovn-central", "NODE_IPS", "10.0.0.1"), metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, ctrl.reconcileMasterNodeIPs(context.Background()))

		// Value should be unchanged since nothing was patched
		dep, err := kubeClient.AppsV1().Deployments(ns).Get(context.Background(), "ovn-central", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", dep.Spec.Template.Spec.Containers[0].Env[0].Value)
	})
}
