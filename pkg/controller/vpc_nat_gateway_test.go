package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIsVpcNatGwChanged(t *testing.T) {
	tests := []struct {
		name     string
		gw       *kubeovnv1.VpcNatGateway
		expected bool
	}{
		{
			name: "no changes returns false",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: false,
		},
		{
			name: "ExternalSubnets changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet2"},
					Selector:        []string{"node=worker1"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "Selector changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker2"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "Tolerations changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
					Tolerations:     []corev1.Toleration{{Key: "new-key"}},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "Affinity changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
					Affinity: corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "InternalSubnets changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					InternalSubnets: []string{"subnet1"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					InternalSubnets: []string{"subnet2"},
				},
			},
			expected: true,
		},
		{
			name: "InternalCIDRs changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					InternalCIDRs: []string{"10.0.0.0/24"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					InternalCIDRs: []string{"10.0.1.0/24"},
				},
			},
			expected: true,
		},
		{
			name: "Replicas changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Replicas: 2,
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					Replicas: 1,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVpcNatGwChanged(tt.gw)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleAddOrUpdateVpcNatGwUpdatesStatefulSetLanIP(t *testing.T) {
	const (
		gwName       = "persisted-gw"
		subnetName   = "nat-subnet"
		externalName = "ovn-vpc-external-network"
		lanIP        = "10.20.0.10"
	)
	namespace := metav1.NamespaceSystem
	stsName := util.GenNatGwName(gwName)
	gwLabels := util.GenNatGwLabels(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Vpc:      util.DefaultVpc,
			Subnet:   subnetName,
			Replicas: 1,
			// spec.lanIp has just been persisted from the observed Pod address.
			LanIP: lanIP,
		},
		Status: kubeovnv1.VpcNatGatewayStatus{Replicas: 1},
	}
	// The existing StatefulSet still carries the template generated while the LAN IP
	// was allocated dynamically, i.e. without an IP address annotation.
	sts := &appsv1.StatefulSet{
		Name: stsName, Namespace: namespace, UID: "sts-uid",
		Labels:      map[string]string{"example.com/managed-by": "gitops"},
		Annotations: map[string]string{"example.com/managed-by": "gitops"},
		OwnerReferences: []metav1.OwnerReference{
			{APIVersion: "example.com/v1", Kind: "Other", Name: "other", UID: "other-uid"},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: gwLabels},
			Template: corev1.PodTemplateSpec{Labels: gwLabels, Annotations: map[string]string{util.VpcNatGatewayAnnotation: gwName}},
		},
	}

	// A Running Pod holding the persisted address: it keeps the reconcile on the
	// normal path instead of the 5s back-off getNatGwPods applies when no Pod is active.
	pod := &corev1.Pod{
		Name: stsName + "-0", Namespace: namespace, Labels: gwLabels,
		Annotations:     map[string]string{util.IPAddressAnnotation: lanIP},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, sts.UID)},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:           []*kubeovnv1.Vpc{{Name: util.DefaultVpc}},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{
			{Name: subnetName, Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Gateway: "10.20.0.1"}},
			{Name: externalName, Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Gateway: "192.168.0.1"}},
		},
		StatefulSets: []*appsv1.StatefulSet{sts},
		Pods:         []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController
	// The missing controller label removes the StatefulSet from the filtered production
	// informer. Mirror that state here while keeping the owner-less object available through
	// the API, so one reconcile must restore both watch and ownership metadata.
	controller.statefulSetsLister = appsv1listers.NewStatefulSetLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, nil))

	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = "unknown" })
	controller.serviceCIDRStore = util.NewServiceCIDRStore("10.96.0.0/12")
	require.NoError(t, controller.handleAddOrUpdateVpcNatGw(gwName))

	updated, err := controller.config.KubeClient.AppsV1().StatefulSets(namespace).Get(context.Background(), stsName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, lanIP, updated.Spec.Template.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider)],
		"persisted spec.lanIp must be propagated to the existing StatefulSet template")
	require.Len(t, updated.OwnerReferences, 2, "unrelated owner references must survive the update")
	require.Equal(t, gw.UID, metav1.GetControllerOf(updated).UID, "controller owner must be restored")
	require.Equal(t, "true", updated.Labels[util.VpcNatGatewayLabel], "controller label must be restored")
	require.Equal(t, "gitops", updated.Labels["example.com/managed-by"], "third-party labels must survive the update")
	require.Equal(t, "gitops", updated.Annotations["example.com/managed-by"], "third-party metadata must survive the update")
}

func TestGetSubnetProvider(t *testing.T) {
	tests := []struct {
		name             string
		subnetName       string
		subnets          []*kubeovnv1.Subnet
		expectedProvider string
		expectError      bool
		description      string
	}{
		{
			name:       "Valid OVN subnets with different providers",
			subnetName: "ovn-default",
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "ovn-default",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
					},
				},
				{
					Name: "net1-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.1.0/24",
						Provider:  "net1.default.ovn",
					},
				},
			},
			expectedProvider: util.OvnProvider,
			expectError:      false,
			description:      "Should return correct provider for valid OVN subnet among multiple subnets",
		},
		{
			name:       "Non-existent subnet",
			subnetName: "non-existent",
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "ovn-default",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
					},
				},
			},
			expectedProvider: "",
			expectError:      true,
			description:      "Should return error for non-existent subnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create controller with subnets
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: tt.subnets,
			})
			require.NoError(t, err, "Failed to create fake controller")
			controller := fakeController.fakeController
			// Call the method under test
			provider, err := controller.GetSubnetProvider(tt.subnetName)

			// Check for errors
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none: %s", tt.description)
				return
			}
			require.NoError(t, err, "Unexpected error: %s", tt.description)

			// Verify provider
			assert.Equal(t, tt.expectedProvider, provider, "Provider mismatch: %s", tt.description)
		})
	}

	// Test multiple provider scenarios in a single comprehensive test
	t.Run("Multiple provider scenarios", func(t *testing.T) {
		subnets := []*kubeovnv1.Subnet{
			{
				Name: "default-subnet",
				Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
			},
			{
				Name: "custom-subnet",
				Spec: kubeovnv1.SubnetSpec{Provider: "custom.provider.ovn"},
			},
		}

		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: subnets,
		})
		require.NoError(t, err)
		controller := fakeController.fakeController
		// Test default provider
		provider, err := controller.GetSubnetProvider("default-subnet")
		require.NoError(t, err)
		assert.Equal(t, util.OvnProvider, provider)

		// Test custom provider
		provider, err = controller.GetSubnetProvider("custom-subnet")
		require.NoError(t, err)
		assert.Equal(t, "custom.provider.ovn", provider)

		// Test non-existent subnet
		_, err = controller.GetSubnetProvider("missing-subnet")
		assert.Error(t, err, "Should error for missing subnet")
	})
}

func TestGetExternalSubnetNad(t *testing.T) {
	tests := []struct {
		name              string
		gw                *kubeovnv1.VpcNatGateway
		subnets           []*kubeovnv1.Subnet
		podNamespace      string
		expectedNamespace string
		expectedName      string
		expectError       bool
	}{
		{
			name: "provider with 3 parts (name.namespace.ovn)",
			gw: &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"external-subnet"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "external-subnet",
					Spec: kubeovnv1.SubnetSpec{Provider: "real-eip.kube-system.ovn"},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "kube-system",
			expectedName:      "real-eip",
			expectError:       false,
		},
		{
			name: "provider with 2 parts (name.namespace)",
			gw: &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"external-subnet"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "external-subnet",
					Spec: kubeovnv1.SubnetSpec{Provider: "my-nad.default"},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "default",
			expectedName:      "my-nad",
			expectError:       false,
		},
		{
			name: "provider is ovn (fallback to subnet name)",
			gw: &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"ovn-vpc-external-network"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "ovn-vpc-external-network",
					Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "kube-system",
			expectedName:      "ovn-vpc-external-network",
			expectError:       false,
		},
		{
			name: "empty provider (fallback to subnet name)",
			gw: &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"my-external-subnet"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "my-external-subnet",
					Spec: kubeovnv1.SubnetSpec{Provider: ""},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "kube-system",
			expectedName:      "my-external-subnet",
			expectError:       false,
		},
		{
			name: "empty ExternalSubnets (use default)",
			gw: &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "ovn-vpc-external-network",
					Spec: kubeovnv1.SubnetSpec{Provider: "external.default.ovn"},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "default",
			expectedName:      "external",
			expectError:       false,
		},
		{
			name: "subnet not found",
			gw: &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"non-existent-subnet"}},
			},
			subnets:      []*kubeovnv1.Subnet{},
			podNamespace: "kube-system",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: tt.subnets,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController
			controller.config.PodNamespace = tt.podNamespace

			namespace, name, err := controller.getExternalSubnetNad(tt.gw)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedNamespace, namespace, "namespace mismatch")
			assert.Equal(t, tt.expectedName, name, "name mismatch")
		})
	}
}

func TestGenNatGwStatefulSetImagePullSecrets(t *testing.T) {
	tests := []struct {
		name           string
		pullSecret     string
		expectedResult []corev1.LocalObjectReference
	}{
		{
			name:           "empty pull secret results in nil",
			pullSecret:     "",
			expectedResult: nil,
		},
		{
			name:       "non-empty pull secret is injected",
			pullSecret: "pull-secret",
			expectedResult: []corev1.LocalObjectReference{
				{Name: "pull-secret"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Vpc:             "test-vpc",
					Subnet:          "internal-subnet",
					ExternalSubnets: []string{"external-subnet"},
					Replicas:        1,
				},
			}

			subnets := []*kubeovnv1.Subnet{
				{
					Name: "internal-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.0.0/24",
						Gateway:   "10.0.0.1",
						Provider:  "internal.default.ovn",
					},
				},
				{
					Name: "external-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.0.0/24",
						Gateway:   "192.168.0.1",
						Provider:  "external.default.ovn",
					},
				},
			}

			vpcs := []*kubeovnv1.Vpc{
				{
					Name: "test-vpc",
					Status: kubeovnv1.VpcStatus{
						Subnets: []string{"internal-subnet"},
					},
				},
			}

			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: subnets,
				Vpcs:    vpcs,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController

			oldPullSecret := vpcNatImagePullSecret
			vpcNatImagePullSecret = tt.pullSecret
			t.Cleanup(func() { vpcNatImagePullSecret = oldPullSecret })

			sts, err := controller.genNatGwStatefulSet(gw, nil, 0)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedResult, sts.Spec.Template.Spec.ImagePullSecrets)
		})
	}
}

func TestGenNatGwDeploymentImagePullSecrets(t *testing.T) {
	tests := []struct {
		name           string
		pullSecret     string
		expectedResult []corev1.LocalObjectReference
	}{
		{
			name:           "empty pull secret results in nil",
			pullSecret:     "",
			expectedResult: nil,
		},
		{
			name:       "non-empty pull secret is injected",
			pullSecret: "pull-secret",
			expectedResult: []corev1.LocalObjectReference{
				{Name: "pull-secret"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Vpc:             "test-vpc",
					Subnet:          "internal-subnet",
					ExternalSubnets: []string{"external-subnet"},
					Replicas:        2, // HA mode
				},
			}

			subnets := []*kubeovnv1.Subnet{
				{
					Name: "internal-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.0.0/24",
						Gateway:   "10.0.0.1",
						Provider:  "internal.default.ovn",
					},
				},
				{
					Name: "external-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.0.0/24",
						Gateway:   "192.168.0.1",
						Provider:  "external.default.ovn",
					},
				},
			}

			vpcs := []*kubeovnv1.Vpc{
				{
					Name: "test-vpc",
					Status: kubeovnv1.VpcStatus{
						Subnets: []string{"internal-subnet"},
					},
				},
			}

			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: subnets,
				Vpcs:    vpcs,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController

			oldPullSecret := vpcNatImagePullSecret
			vpcNatImagePullSecret = tt.pullSecret
			t.Cleanup(func() { vpcNatImagePullSecret = oldPullSecret })

			deploy, err := controller.genNatGwDeployment(gw)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedResult, deploy.Spec.Template.Spec.ImagePullSecrets)
		})
	}
}

func TestHandleAddOrUpdateVpcNatGwImagePullSecretsOnCreate(t *testing.T) {
	tests := []struct {
		name           string
		pullSecret     string
		replicas       int32
		expectedResult []corev1.LocalObjectReference
	}{
		{
			name:           "STS mode: no pull secret configured",
			pullSecret:     "",
			replicas:       1,
			expectedResult: nil,
		},
		{
			name:       "STS mode: pull secret configured",
			pullSecret: "pull-secret",
			replicas:   1,
			expectedResult: []corev1.LocalObjectReference{
				{Name: "pull-secret"},
			},
		},
		{
			name:           "Deployment mode (HA): no pull secret configured",
			pullSecret:     "",
			replicas:       2,
			expectedResult: nil,
		},
		{
			name:       "Deployment mode (HA): pull secret configured",
			pullSecret: "pull-secret",
			replicas:   2,
			expectedResult: []corev1.LocalObjectReference{
				{Name: "pull-secret"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcNatGateway{
				Name: "test-gw",
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Vpc:             "test-vpc",
					Subnet:          "internal-subnet",
					ExternalSubnets: []string{"external-subnet"},
					Replicas:        tt.replicas,
				},
			}

			subnets := []*kubeovnv1.Subnet{
				{
					Name: "internal-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.0.0/24",
						Gateway:   "10.0.0.1",
						Provider:  "internal.default.ovn",
					},
				},
				{
					Name: "external-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.0.0/24",
						Gateway:   "192.168.0.1",
						Provider:  "external.default.ovn",
					},
				},
			}

			vpcs := []*kubeovnv1.Vpc{
				{
					Name: "test-vpc",
					Status: kubeovnv1.VpcStatus{
						Subnets: []string{"internal-subnet"},
					},
				},
			}

			// patchNatGwStatus (called after Create) needs at least one active/running
			// pod for HA mode (Deployment) to compute LanIP via getNatGwPods.
			// Selector used by getNatGwPods: app=<GenNatGwName>, util.VpcNatGatewayLabel=true
			var pods []*corev1.Pod
			if tt.replicas > 1 {
				pods = []*corev1.Pod{
					{
						Name:      util.GenNatGwName(gw.Name) + "-0",
						Namespace: "kube-system",
						Labels: map[string]string{
							"app":                   util.GenNatGwName(gw.Name),
							util.VpcNatGatewayLabel: "true",
						},
						Annotations: map[string]string{
							fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.0.0.2",
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
							Conditions: []corev1.PodCondition{
								{Type: corev1.PodReady, Status: corev1.ConditionTrue},
							},
						},
					},
				}
			}

			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets:        subnets,
				Vpcs:           vpcs,
				VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
				Pods:           pods,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController

			// ensure the "iptables nat gw enabled" gate is set so handleAddOrUpdateVpcNatGw proceeds
			oldEnabled := vpcNatEnabled
			vpcNatEnabled = "true"
			t.Cleanup(func() { vpcNatEnabled = oldEnabled })

			oldSecret := vpcNatImagePullSecret
			vpcNatImagePullSecret = tt.pullSecret
			t.Cleanup(func() { vpcNatImagePullSecret = oldSecret })

			err = controller.handleAddOrUpdateVpcNatGw(gw.Name)
			require.NoError(t, err)

			if tt.replicas > 1 {
				deploy, err := controller.config.KubeClient.AppsV1().Deployments(controller.natGwNamespace(gw)).
					Get(context.Background(), util.GenNatGwName(gw.Name), metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, tt.expectedResult, deploy.Spec.Template.Spec.ImagePullSecrets)
			} else {
				sts, err := controller.config.KubeClient.AppsV1().StatefulSets(controller.natGwNamespace(gw)).
					Get(context.Background(), util.GenNatGwName(gw.Name), metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, tt.expectedResult, sts.Spec.Template.Spec.ImagePullSecrets)
			}
		})
	}
}
