package controller

import (
	"fmt"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCheckIsPodVpcNatGw(t *testing.T) {
	tests := []struct {
		name                string
		pod                 *corev1.Pod
		networkAttachments  []*nadv1.NetworkAttachmentDefinition
		subnets             []*kubeovnv1.Subnet
		enableNonPrimaryCNI bool
		expectedIsVpcNatGw  bool
		expectedVpcGwName   string
		description         string
	}{
		{
			name: "Pod with default provider VPC NAT gateway annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						util.VpcNatGatewayAnnotation: "test-nat-gw",
					},
				},
			},
			networkAttachments:  []*nadv1.NetworkAttachmentDefinition{},
			subnets:             []*kubeovnv1.Subnet{},
			enableNonPrimaryCNI: false,
			expectedIsVpcNatGw:  true,
			expectedVpcGwName:   "test-nat-gw",
			description:         "Should detect VPC NAT gateway with default provider",
		},
		{
			name: "Pod with custom provider VPC NAT gateway annotation in non-primary CNI mode",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						// Network attachment annotation to indicate this pod uses net1
						nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
						// Custom provider VPC NAT gateway annotation
						fmt.Sprintf(util.VpcNatGatewayAnnotationTemplate, "net1.default.ovn"): "test-nat-gw",
						// Kube-OVN annotations for net1 provider
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net1.default.ovn"): "net1-vpc",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					},
				},
			},
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "net1",
						Namespace: "default",
					},
					Spec: nadv1.NetworkAttachmentDefinitionSpec{
						Config: `{
							"cniVersion": "0.3.1",
							"name": "net1",
							"type": "kube-ovn",
							"server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
							"provider": "net1.default.ovn"
						}`,
					},
				},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "net1-subnet",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.1.0/24",
						Provider:  "net1.default.ovn",
					},
				},
			},
			enableNonPrimaryCNI: true,
			expectedIsVpcNatGw:  true,
			expectedVpcGwName:   "test-nat-gw",
			description:         "Should detect VPC NAT gateway with custom provider in non-primary CNI mode",
		},
		{
			name: "Pod without VPC NAT gateway annotation or with empty name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"other.annotation": "value",
					},
				},
			},
			networkAttachments:  []*nadv1.NetworkAttachmentDefinition{},
			subnets:             []*kubeovnv1.Subnet{},
			enableNonPrimaryCNI: false,
			expectedIsVpcNatGw:  false,
			expectedVpcGwName:   "",
			description:         "Should not detect VPC NAT gateway when annotation is missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create controller with proper setup
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				NetworkAttachments: tt.networkAttachments,
				Subnets:            tt.subnets,
				Pods:               []*corev1.Pod{tt.pod},
			})
			require.NoError(t, err, "Failed to create fake controller")
			controller := fakeController.fakeController
			// Set the non-primary CNI mode
			controller.config.EnableNonPrimaryCNI = tt.enableNonPrimaryCNI

			// Call the method under test
			isVpcNatGw, vpcGwName := controller.checkIsPodVpcNatGw(tt.pod)

			// Verify results
			assert.Equal(t, tt.expectedIsVpcNatGw, isVpcNatGw, "IsVpcNatGw mismatch: %s", tt.description)
			assert.Equal(t, tt.expectedVpcGwName, vpcGwName, "VpcGwName mismatch: %s", tt.description)
		})
	}

	// Test additional edge cases in a single sub-test for efficiency
	t.Run("Edge cases", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		controller := fakeController.fakeController
		// Test nil pod
		isVpcNatGw, vpcGwName := controller.checkIsPodVpcNatGw(nil)
		assert.False(t, isVpcNatGw, "Nil pod should not be VPC NAT gateway")
		assert.Equal(t, "", vpcGwName, "Nil pod should have empty gateway name")

		// Test pod with empty VPC NAT gateway name
		podWithEmptyGw := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-pod",
				Namespace:   "default",
				Annotations: map[string]string{util.VpcNatGatewayAnnotation: ""},
			},
		}
		isVpcNatGw, vpcGwName = controller.checkIsPodVpcNatGw(podWithEmptyGw)
		assert.False(t, isVpcNatGw, "Pod with empty gateway name should not be VPC NAT gateway")
		assert.Equal(t, "", vpcGwName, "Pod with empty gateway name should return empty")

		// Test pod with no annotations
		podNoAnnotations := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-pod",
				Namespace:   "default",
				Annotations: nil,
			},
		}
		isVpcNatGw, vpcGwName = controller.checkIsPodVpcNatGw(podNoAnnotations)
		assert.False(t, isVpcNatGw, "Pod with no annotations should not be VPC NAT gateway")
		assert.Equal(t, "", vpcGwName, "Pod with no annotations should return empty")
	})
}

func TestGetPodKubeovnNetsNonPrimaryCNI(t *testing.T) {
	tests := []struct {
		name                string
		pod                 *corev1.Pod
		networkAttachments  []*nadv1.NetworkAttachmentDefinition
		subnets             []*kubeovnv1.Subnet
		enableNonPrimaryCNI bool
		expectedNetCount    int
		expectError         bool
		description         string
	}{
		{
			name: "Non-primary CNI mode with network attachments",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
						// Kube-OVN annotations for net1 provider
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net1.default.ovn"): "net1-vpc",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					},
				},
			},
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "net1",
						Namespace: "default",
					},
					Spec: nadv1.NetworkAttachmentDefinitionSpec{
						Config: `{
							"cniVersion": "0.3.1",
							"name": "net1",
							"type": "kube-ovn",
							"server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
							"provider": "net1.default.ovn"
						}`,
					},
				},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "net1-subnet",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.1.0/24",
						Provider:  "net1.default.ovn",
					},
				},
			},
			enableNonPrimaryCNI: true,
			expectedNetCount:    1,
			expectError:         false,
			description:         "Should return only network attachment definitions in non-primary CNI mode",
		},
		{
			name: "Primary CNI mode vs Non-primary CNI behavior",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
						// Both custom and default provider annotations
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider):   "ovn-default",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):       "10.244.0.5",
					},
				},
			},
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "net1",
						Namespace: "default",
					},
					Spec: nadv1.NetworkAttachmentDefinitionSpec{
						Config: `{
							"cniVersion": "0.3.1",
							"name": "net1",
							"type": "kube-ovn",
							"server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
							"provider": "net1.default.ovn"
						}`,
					},
				},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "net1-subnet",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.1.0/24",
						Provider:  "net1.default.ovn",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ovn-default",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
						Default:   true,
					},
				},
			},
			enableNonPrimaryCNI: false, // This test will verify both modes
			expectedNetCount:    2,     // Both networks in primary mode
			expectError:         false,
			description:         "Should handle both network attachments and default network differently in primary vs non-primary modes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create controller with proper setup
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				NetworkAttachments: tt.networkAttachments,
				Subnets:            tt.subnets,
				Pods:               []*corev1.Pod{tt.pod},
			})
			require.NoError(t, err, "Failed to create fake controller")
			controller := fakeController.fakeController

			// Set the non-primary CNI mode
			controller.config.EnableNonPrimaryCNI = tt.enableNonPrimaryCNI

			// Call the method under test
			nets, err := controller.getPodKubeovnNets(tt.pod)

			// Check for errors
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none: %s", tt.description)
				return
			}
			require.NoError(t, err, "Unexpected error: %s", tt.description)

			// Verify network count
			assert.Equal(t, tt.expectedNetCount, len(nets), "Network count mismatch: %s", tt.description)

			// For the comparison test, also test non-primary mode
			if tt.name == "Primary CNI mode vs Non-primary CNI behavior" {
				controller.config.EnableNonPrimaryCNI = true
				netsNonPrimary, err := controller.getPodKubeovnNets(tt.pod)
				require.NoError(t, err, "Unexpected error in non-primary mode")
				assert.Equal(t, 1, len(netsNonPrimary), "Non-primary mode should return only network attachments")
			}
		})
	}
}

func TestIsKruiseStatefulSetPod(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		expectResult  bool
		expectStsName string
		description   string
	}{
		{
			name: "Pod owned by standard Kubernetes StatefulSet",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       util.StatefulSet,
							Name:       "test-sts",
							UID:        "test-uid",
						},
					},
				},
			},
			expectResult:  false,
			expectStsName: "",
			description:   "Should return false for standard Kubernetes StatefulSet pod",
		},
		{
			name: "Pod owned by OpenKruise StatefulSet v1beta1",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps.kruise.io/v1beta1",
							Kind:       util.StatefulSet,
							Name:       "kruise-sts",
							UID:        "kruise-uid",
						},
					},
				},
			},
			expectResult:  true,
			expectStsName: "kruise-sts",
			description:   "Should return true for OpenKruise StatefulSet v1beta1 pod",
		},
		{
			name: "Pod owned by OpenKruise StatefulSet v1alpha1 (unsupported)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-1",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps.kruise.io/v1alpha1",
							Kind:       util.StatefulSet,
							Name:       "kruise-sts",
							UID:        "kruise-uid",
						},
					},
				},
			},
			expectResult:  false,
			expectStsName: "",
			description:   "Should return false for OpenKruise StatefulSet v1alpha1 pod (only v1beta1 is supported)",
		},
		{
			name: "Pod without any owner reference",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "standalone-pod",
					Namespace: "default",
				},
			},
			expectResult:  false,
			expectStsName: "",
			description:   "Should return false for standalone pod",
		},
		{
			name: "Pod owned by Deployment",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deployment-pod-abc123",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "ReplicaSet",
							Name:       "deployment-pod-abc123",
							UID:        "rs-uid",
						},
					},
				},
			},
			expectResult:  false,
			expectStsName: "",
			description:   "Should return false for pod owned by Deployment/ReplicaSet",
		},
		{
			name: "Pod name does not match owner name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-pod-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps.kruise.io/v1beta1",
							Kind:       util.StatefulSet,
							Name:       "kruise-sts",
							UID:        "kruise-uid",
						},
					},
				},
			},
			expectResult:  false,
			expectStsName: "",
			description:   "Should return false when pod name does not start with owner name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isKruiseSts, stsName, _ := isKruiseStatefulSetPod(tt.pod)
			assert.Equal(t, tt.expectResult, isKruiseSts, tt.description)
			assert.Equal(t, tt.expectStsName, stsName, tt.description)
		})
	}
}

func TestGetPodType(t *testing.T) {
	tests := []struct {
		name            string
		pod             *corev1.Pod
		expectedPodType string
		description     string
	}{
		{
			name: "Pod owned by standard Kubernetes StatefulSet",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       util.StatefulSet,
							Name:       "test-sts",
							UID:        "test-uid",
						},
					},
				},
			},
			expectedPodType: util.StatefulSet,
			description:     "Should return StatefulSet for standard Kubernetes StatefulSet pod",
		},
		{
			name: "Pod owned by OpenKruise StatefulSet",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps.kruise.io/v1beta1",
							Kind:       util.StatefulSet,
							Name:       "kruise-sts",
							UID:        "kruise-uid",
						},
					},
				},
			},
			expectedPodType: util.KruiseStatefulSet,
			description:     "Should return KruiseStatefulSet for OpenKruise StatefulSet pod",
		},
		{
			name: "Pod without any owner reference",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "standalone-pod",
					Namespace: "default",
				},
			},
			expectedPodType: "",
			description:     "Should return empty string for standalone pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podType := getPodType(tt.pod)
			assert.Equal(t, tt.expectedPodType, podType, tt.description)
		})
	}
}

func TestCheckKruiseStatefulSetState(t *testing.T) {
	scheme := runtime.NewScheme()

	tests := []struct {
		name           string
		pod            *corev1.Pod
		stsName        string
		stsUID         types.UID
		stsObject      *unstructured.Unstructured
		expectedResult kruiseStsCheckResult
		description    string
	}{
		{
			name: "StatefulSet deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
				},
			},
			stsName:        "kruise-sts",
			stsUID:         "original-uid",
			stsObject:      nil, // not found
			expectedResult: kruiseStsCheckResultDelete,
			description:    "Should return delete when statefulset is not found",
		},
		{
			name: "StatefulSet being deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":              "kruise-sts",
						"namespace":         "default",
						"uid":               "original-uid",
						"deletionTimestamp": "2024-01-01T00:00:00Z",
					},
					"spec": map[string]any{
						"replicas": int64(3),
					},
				},
			},
			expectedResult: kruiseStsCheckResultDelete,
			description:    "Should return delete when statefulset is being deleted",
		},
		{
			name: "StatefulSet recreated (UID mismatch)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "new-uid",
					},
					"spec": map[string]any{
						"replicas": int64(3),
					},
				},
			},
			expectedResult: kruiseStsCheckResultDelete,
			description:    "Should return delete when statefulset has different UID",
		},
		{
			name: "StatefulSet down scaled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-2",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(2), // scaled down from 3 to 2
					},
				},
			},
			expectedResult: kruiseStsCheckResultDownScale,
			description:    "Should return downscale when pod index >= replicas",
		},
		{
			name: "StatefulSet down scaled with custom ordinals.start",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-7",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(2), // replicas = 2
						"ordinals": map[string]any{
							"start": int64(5), // start at 5, so valid pods are 5, 6
						},
					},
				},
			},
			expectedResult: kruiseStsCheckResultDownScale,
			description:    "Should return downscale when pod index >= start + replicas",
		},
		{
			name: "StatefulSet keep (pod within replicas)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-1",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(3),
					},
				},
			},
			expectedResult: kruiseStsCheckResultKeep,
			description:    "Should return keep when pod index < replicas",
		},
		{
			name: "StatefulSet keep with custom ordinals.start",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-6",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(3), // replicas = 3
						"ordinals": map[string]any{
							"start": int64(5), // start at 5, so valid pods are 5, 6, 7
						},
					},
				},
			},
			expectedResult: kruiseStsCheckResultKeep,
			description:    "Should return keep when pod index < start + replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			if tt.stsObject != nil {
				objects = append(objects, tt.stsObject)
			}
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)

			result := checkKruiseStatefulSetState(dynamicClient, tt.pod, tt.stsName, tt.stsUID)
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

func TestIsKruiseStatefulSetPodToDel(t *testing.T) {
	scheme := runtime.NewScheme()

	tests := []struct {
		name        string
		pod         *corev1.Pod
		stsName     string
		stsUID      types.UID
		stsObject   *unstructured.Unstructured
		expected    bool
		description string
	}{
		{
			name: "Delete when StatefulSet is deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
				},
			},
			stsName:     "kruise-sts",
			stsUID:      "original-uid",
			stsObject:   nil,
			expected:    true,
			description: "Should return true when statefulset is deleted",
		},
		{
			name: "Delete when StatefulSet is down scaled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-2",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(2),
					},
				},
			},
			expected:    true,
			description: "Should return true when statefulset is down scaled",
		},
		{
			name: "Keep when pod within replicas",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-1",
					Namespace: "default",
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(3),
					},
				},
			},
			expected:    false,
			description: "Should return false when pod is within replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			if tt.stsObject != nil {
				objects = append(objects, tt.stsObject)
			}
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)

			result := isKruiseStatefulSetPodToDel(dynamicClient, tt.pod, tt.stsName, tt.stsUID)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestIsKruiseStatefulSetPodToGC(t *testing.T) {
	scheme := runtime.NewScheme()

	tests := []struct {
		name        string
		pod         *corev1.Pod
		stsName     string
		stsUID      types.UID
		stsObject   *unstructured.Unstructured
		expected    bool
		description string
	}{
		{
			name: "GC when StatefulSet is deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-0",
					Namespace: "default",
				},
			},
			stsName:     "kruise-sts",
			stsUID:      "original-uid",
			stsObject:   nil,
			expected:    true,
			description: "Should return true when statefulset is deleted",
		},
		{
			name: "GC when down scaled and pod not alive",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-2",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(2),
					},
				},
			},
			expected:    true,
			description: "Should return true when down scaled and pod is not alive",
		},
		{
			name: "No GC when down scaled but pod still alive",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-2",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(2),
					},
				},
			},
			expected:    false,
			description: "Should return false when down scaled but pod is still alive",
		},
		{
			name: "No GC when pod within replicas",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kruise-sts-1",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			stsName: "kruise-sts",
			stsUID:  "original-uid",
			stsObject: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps.kruise.io/v1beta1",
					"kind":       "StatefulSet",
					"metadata": map[string]any{
						"name":      "kruise-sts",
						"namespace": "default",
						"uid":       "original-uid",
					},
					"spec": map[string]any{
						"replicas": int64(3),
					},
				},
			},
			expected:    false,
			description: "Should return false when pod is within replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			if tt.stsObject != nil {
				objects = append(objects, tt.stsObject)
			}
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)

			result := isKruiseStatefulSetPodToGC(dynamicClient, tt.pod, tt.stsName, tt.stsUID)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}
