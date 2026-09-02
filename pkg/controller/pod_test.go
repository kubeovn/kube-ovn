package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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
				Name:      "test-pod",
				Namespace: "default",
				Annotations: map[string]string{
					util.VpcNatGatewayAnnotation: "test-nat-gw",
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
				Name:      "test-pod",
				Namespace: "default",
				Annotations: map[string]string{
					// Network attachment annotation to indicate this pod uses net1
					nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
					// Custom provider VPC NAT gateway annotation
					util.VpcNatGatewayAnnotation: "test-nat-gw",
					// Kube-OVN annotations for net1 provider
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
					fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net1.default.ovn"): "net1-vpc",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
				},
			},
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{
				{
					Name:      "net1",
					Namespace: "default",
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
					Name: "net1-subnet",
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
				Name:      "test-pod",
				Namespace: "default",
				Annotations: map[string]string{
					"other.annotation": "value",
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
			Name:        "test-pod",
			Namespace:   "default",
			Annotations: map[string]string{util.VpcNatGatewayAnnotation: ""},
		}
		isVpcNatGw, vpcGwName = controller.checkIsPodVpcNatGw(podWithEmptyGw)
		assert.False(t, isVpcNatGw, "Pod with empty gateway name should not be VPC NAT gateway")
		assert.Equal(t, "", vpcGwName, "Pod with empty gateway name should return empty")

		// Test pod with no annotations
		podNoAnnotations := &corev1.Pod{
			Name:        "test-pod",
			Namespace:   "default",
			Annotations: nil,
		}
		isVpcNatGw, vpcGwName = controller.checkIsPodVpcNatGw(podNoAnnotations)
		assert.False(t, isVpcNatGw, "Pod with no annotations should not be VPC NAT gateway")
		assert.Equal(t, "", vpcGwName, "Pod with no annotations should return empty")
	})
}

func TestBackfillVpcNatGwLanIPFromPod(t *testing.T) {
	const (
		gwName    = "test-nat-gw"
		subnet    = "nat-subnet"
		provider  = "net1.default.ovn"
		lanIP     = "10.244.0.10"
		namespace = "default"
	)

	tests := []struct {
		name                   string
		gwSpecLanIP            string
		subnetProtocol         string
		givenGwName            string
		podOwnerName           string
		podNamespace           string
		controllerPodNamespace string
		podAnnotation          map[string]string
		expectedLanIP          string
	}{
		{
			name:                   "backfill lanIP from pod annotation",
			gwSpecLanIP:            "",
			subnetProtocol:         kubeovnv1.ProtocolIPv4,
			givenGwName:            gwName,
			podOwnerName:           util.GenNatGwName(gwName),
			podNamespace:           namespace,
			controllerPodNamespace: namespace,
			podAnnotation: map[string]string{
				fmt.Sprintf(util.IPAddressAnnotationTemplate, provider): lanIP,
			},
			expectedLanIP: lanIP,
		},
		{
			name:                   "derive gateway name from owner reference",
			gwSpecLanIP:            "",
			subnetProtocol:         kubeovnv1.ProtocolIPv4,
			givenGwName:            "",
			podOwnerName:           util.GenNatGwName(gwName),
			podNamespace:           namespace,
			controllerPodNamespace: namespace,
			podAnnotation: map[string]string{
				fmt.Sprintf(util.IPAddressAnnotationTemplate, provider): lanIP,
			},
			expectedLanIP: lanIP,
		},
		{
			name:                   "skip when spec lanIP already set",
			gwSpecLanIP:            "10.244.0.99",
			subnetProtocol:         kubeovnv1.ProtocolIPv4,
			givenGwName:            gwName,
			podOwnerName:           util.GenNatGwName(gwName),
			podNamespace:           namespace,
			controllerPodNamespace: namespace,
			podAnnotation: map[string]string{
				fmt.Sprintf(util.IPAddressAnnotationTemplate, provider): lanIP,
			},
			expectedLanIP: "10.244.0.99",
		},
		{
			name:                   "skip when pod namespace is different from controller namespace",
			gwSpecLanIP:            "",
			subnetProtocol:         kubeovnv1.ProtocolIPv4,
			givenGwName:            gwName,
			podOwnerName:           util.GenNatGwName(gwName),
			podNamespace:           "other-ns",
			controllerPodNamespace: namespace,
			podAnnotation: map[string]string{
				fmt.Sprintf(util.IPAddressAnnotationTemplate, provider): lanIP,
			},
			expectedLanIP: "",
		},
		{
			name:                   "skip when lanIP annotation is invalid",
			gwSpecLanIP:            "",
			subnetProtocol:         kubeovnv1.ProtocolIPv4,
			givenGwName:            gwName,
			podOwnerName:           util.GenNatGwName(gwName),
			podNamespace:           namespace,
			controllerPodNamespace: namespace,
			podAnnotation: map[string]string{
				fmt.Sprintf(util.IPAddressAnnotationTemplate, provider): "not-an-ip",
			},
			expectedLanIP: "",
		},
		{
			name:                   "prefer IPv6 address for IPv6 subnet",
			gwSpecLanIP:            "",
			subnetProtocol:         kubeovnv1.ProtocolIPv6,
			givenGwName:            gwName,
			podOwnerName:           util.GenNatGwName(gwName),
			podNamespace:           namespace,
			controllerPodNamespace: namespace,
			podAnnotation: map[string]string{
				fmt.Sprintf(util.IPAddressAnnotationTemplate, provider): "10.244.0.10,fd00:10:16::10",
			},
			expectedLanIP: "fd00:10:16::10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcNatGateway{
				Name: gwName,
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Vpc:    "vpc-a",
					Subnet: subnet,
					LanIP:  tt.gwSpecLanIP,
				},
			}
			pod := &corev1.Pod{
				Name:        util.GenNatGwPodName(gwName),
				Namespace:   tt.podNamespace,
				Annotations: tt.podAnnotation,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: appsv1.SchemeGroupVersion.String(),
						Kind:       util.KindStatefulSet,
						Name:       tt.podOwnerName,
					},
				},
			}

			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{
					{
						Name: subnet,
						Spec: kubeovnv1.SubnetSpec{
							Provider: provider,
							Protocol: tt.subnetProtocol,
						},
					},
				},
				VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
			})
			require.NoError(t, err)

			controller := fakeController.fakeController
			controller.config.PodNamespace = tt.controllerPodNamespace
			err = controller.backfillVpcNatGwLanIPFromPod(pod, tt.givenGwName)
			require.NoError(t, err)

			gotGw, err := controller.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Get(
				context.Background(), gwName, metav1.GetOptions{},
			)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedLanIP, gotGw.Spec.LanIP)
		})
	}
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
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{
				{
					Name:      "net1",
					Namespace: "default",
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
					Name: "net1-subnet",
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
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{
				{
					Name:      "net1",
					Namespace: "default",
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
					Name: "net1-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.1.0/24",
						Provider:  "net1.default.ovn",
					},
				},
				{
					Name: "ovn-default",
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

func TestGetPodKubeovnNetsReturnsErrorWhenAttachmentProviderHasNoSubnet(t *testing.T) {
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: "default",
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot: `[{"name": "attachnet-a"}]`,
		},
	}
	nad := &nadv1.NetworkAttachmentDefinition{
		Name:      "attachnet-a",
		Namespace: "default",
		Spec: nadv1.NetworkAttachmentDefinitionSpec{
			Config: `{
				"cniVersion": "0.3.1",
				"name": "attachnet-a",
				"type": "kube-ovn",
				"server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
				"provider": "attachnet-a.default.ovn"
			}`,
		},
	}
	subnets := []*kubeovnv1.Subnet{
		{
			Name: util.DefaultSubnet,
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "10.3.0.0/16",
				Provider:  util.OvnProvider,
				Default:   true,
			},
		},
		{
			Name: "mismatch-subnet",
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "10.244.0.0/24",
				Provider:  "attachnet-b.default.ovn",
			},
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{nad},
		Subnets:            subnets,
		Pods:               []*corev1.Pod{pod},
	})
	require.NoError(t, err)

	nets, err := fakeController.fakeController.getPodKubeovnNets(pod)

	require.Error(t, err)
	require.Nil(t, nets)
	require.Contains(t, err.Error(), "provider attachnet-a.default.ovn is not bound to any subnet")
}

func TestAcquireAddressWithSpecifiedSubnet(t *testing.T) {
	tests := []struct {
		name           string
		pod            *corev1.Pod
		namespaces     []*corev1.Namespace
		subnets        []*kubeovnv1.Subnet
		setupIPAM      func(*Controller)
		expectError    bool
		expectedSubnet string
		description    string
	}{
		{
			name: "User specifies subnet - should succeed",
			pod: &corev1.Pod{
				Name:      "test-pod",
				Namespace: "default",
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: "subnet1",
					util.IPAddressAnnotation:     "10.0.1.10",
				},
			},
			namespaces: []*corev1.Namespace{
				{
					Name: "default",
					Annotations: map[string]string{
						util.LogicalSwitchAnnotation: "subnet1,subnet2",
					},
				},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "subnet1",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.1.0/24",
						Protocol:  kubeovnv1.ProtocolIPv4,
						Provider:  util.OvnProvider,
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(100)},
				},
				{
					Name: "subnet2",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.1.0/24",
						Protocol:  kubeovnv1.ProtocolIPv4,
						Provider:  util.OvnProvider,
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(100)},
				},
			},
			expectError:    false,
			expectedSubnet: "subnet1",
			description:    "Should allocate from specified subnet",
		},
		{
			name: "User specifies subnet but IP occupied - should NOT fallback",
			pod: &corev1.Pod{
				Name:      "test-pod",
				Namespace: "default",
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: "subnet1",
					util.IPAddressAnnotation:     "10.0.1.10",
				},
			},
			namespaces: []*corev1.Namespace{
				{
					Name: "default",
					Annotations: map[string]string{
						util.LogicalSwitchAnnotation: "subnet1,subnet2",
					},
				},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "subnet1",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.1.0/24",
						Protocol:  kubeovnv1.ProtocolIPv4,
						Provider:  util.OvnProvider,
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(100)},
				},
				{
					Name: "subnet2",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.1.0/24",
						Protocol:  kubeovnv1.ProtocolIPv4,
						Provider:  util.OvnProvider,
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(100)},
				},
			},
			setupIPAM: func(c *Controller) {
				_, _, _, _ = c.ipam.GetStaticAddress("other-pod.default", "other-pod.default", "10.0.1.10", nil, "subnet1", true)
			},
			expectError: true,
			description: "Should NOT fallback to subnet2 when IP is occupied in specified subnet1",
		},
		{
			name: "No subnet specified - should try all namespace subnets",
			pod: &corev1.Pod{
				Name:      "test-pod",
				Namespace: "default",
				Annotations: map[string]string{
					util.IPAddressAnnotation: "10.0.2.10",
				},
			},
			namespaces: []*corev1.Namespace{
				{
					Name: "default",
					Annotations: map[string]string{
						util.LogicalSwitchAnnotation: "subnet1,subnet2",
					},
				},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "subnet1",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.1.0/24",
						Protocol:  kubeovnv1.ProtocolIPv4,
						Provider:  util.OvnProvider,
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(100)},
				},
				{
					Name: "subnet2",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.0.2.0/24",
						Protocol:  kubeovnv1.ProtocolIPv4,
						Provider:  util.OvnProvider,
					},
					Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(100)},
				},
			},
			expectError:    false,
			expectedSubnet: "subnet2",
			description:    "Should try all subnets and find matching one when no subnet specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Namespaces: tt.namespaces,
				Subnets:    tt.subnets,
				Pods:       []*corev1.Pod{tt.pod},
			})
			require.NoError(t, err)
			controller := fakeController.fakeController
			controller.ipam = newIPAMForTest(tt.subnets)

			if tt.setupIPAM != nil {
				tt.setupIPAM(controller)
			}

			podNets, err := controller.getPodKubeovnNets(tt.pod)
			require.NoError(t, err)
			require.Greater(t, len(podNets), 0)

			_, _, _, subnet, err := controller.acquireAddress(tt.pod, podNets[0])

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
				assert.Equal(t, tt.expectedSubnet, subnet.Name, tt.description)
			}
		})
	}
}

func TestAcquireStaticAddressHelperPerInterfaceIPAMKey(t *testing.T) {
	// This test verifies that when acquireStaticAddressHelper allocates a static IP
	// for a per-interface NAD (with NadName, NadNamespace, and InterfaceName all set),
	// the IP is registered in IPAM under the original pod key ("namespace/podName"),
	// NOT under the annotation key ("nadName.nadNs.kubernetes.io/ip_address.ifaceName").
	//
	// If the IPAM key is wrong, ReleaseAddressByNic (called on pod deletion with the pod key)
	// will fail to find and release the IP, causing an IP leak.

	subnetName := "test-subnet"
	testSubnet := &kubeovnv1.Subnet{
		Name: subnetName,
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock:  "10.0.0.0/24",
			Protocol:   kubeovnv1.ProtocolIPv4,
			ExcludeIps: []string{"10.0.0.1"},
		},
	}

	nadName := "my-nad"
	nadNamespace := "default"
	ifaceName := "net1"
	staticIP := "10.0.0.10"
	annotationKey := perInterfaceIPAnnotationKey(nadName, nadNamespace, ifaceName)

	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: "default",
		Annotations: map[string]string{
			annotationKey: staticIP,
		},
	}

	podNet := &kubeovnNet{
		Subnet:        testSubnet,
		ProviderName:  nadName + "." + nadNamespace + ".ovn",
		NadName:       nadName,
		NadNamespace:  nadNamespace,
		InterfaceName: ifaceName,
	}

	nsNets := []*kubeovnNet{podNet}
	podKey := "default/test-pod"
	portName := podKey

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{testSubnet},
		Pods:    []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController
	ctrl.ipam = newIPAMForTest([]*kubeovnv1.Subnet{testSubnet})

	// Allocate static IP via the per-interface path
	v4IP, _, _, subnet, err := ctrl.acquireStaticAddressHelper(pod, podNet, portName, nil, "", nsNets, false, podKey)
	require.NoError(t, err)
	assert.Equal(t, staticIP, v4IP)
	assert.Equal(t, subnetName, subnet.Name)

	// Verify: IPAM should have the IP registered under the pod key, not the annotation key
	ipamSubnet := ctrl.ipam.Subnets[subnetName]
	require.NotNil(t, ipamSubnet)

	// PodToNicList should have an entry for podKey
	assert.NotEmpty(t, ipamSubnet.PodToNicList[podKey],
		"IPAM should register the IP under pod key %q, but PodToNicList has no entry for it", podKey)

	// PodToNicList should NOT have an entry for the annotation key
	assert.Empty(t, ipamSubnet.PodToNicList[annotationKey],
		"IPAM should NOT register the IP under annotation key %q, but PodToNicList has an entry for it (variable shadowing bug)", annotationKey)

	// Verify that ReleaseAddressByNic with pod key actually releases the IP
	ctrl.ipam.ReleaseAddressByNic(podKey, portName, subnetName)

	// After release, the IP should no longer be tracked
	assert.Empty(t, ipamSubnet.PodToNicList[podKey],
		"After ReleaseAddressByNic with pod key, PodToNicList should be empty for %q", podKey)
	assert.Empty(t, ipamSubnet.V4IPToPod[staticIP],
		"After ReleaseAddressByNic, V4IPToPod should not map %q to any pod", staticIP)
}

func TestGetPodDefaultSubnetUsesNamedIPPoolSubnet(t *testing.T) {
	const (
		namespaceName   = "test-ns"
		namespaceSubnet = "namespace-subnet"
		poolName        = "pool-a"
		poolSubnet      = "pool-subnet"
	)

	ippool := &kubeovnv1.IPPool{
		Name: poolName,
		Spec: kubeovnv1.IPPoolSpec{Subnet: poolSubnet},
	}
	ctrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Namespaces: []*corev1.Namespace{{
			Name: namespaceName,
			Annotations: map[string]string{
				util.LogicalSwitchAnnotation: namespaceSubnet,
			},
		}},
		Subnets: []*kubeovnv1.Subnet{
			{Name: namespaceSubnet},
			{Name: poolSubnet},
		},
	})
	require.NoError(t, err)
	ippoolIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, ippoolIndexer.Add(ippool))
	ctrl.fakeController.ippoolLister = kubeovnlister.NewIPPoolLister(ippoolIndexer)

	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: namespaceName,
		Annotations: map[string]string{
			util.IPPoolAnnotation: poolName,
		},
	}

	subnet, err := ctrl.fakeController.getPodDefaultSubnet(pod)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	assert.Equal(t, poolSubnet, subnet.Name)

	for _, staticPool := range []string{
		"10.0.0.10",
		"10.0.0.10,10.0.0.11",
		"10.0.0.10;10.0.0.11",
	} {
		t.Run("legacy static pool "+staticPool, func(t *testing.T) {
			pod.Annotations[util.IPPoolAnnotation] = staticPool
			subnet, err := ctrl.fakeController.getPodDefaultSubnet(pod)
			require.NoError(t, err)
			require.NotNil(t, subnet)
			assert.Equal(t, namespaceSubnet, subnet.Name)
		})
	}
}

func TestAcquireStaticAddressHelperReturnsConflictForGatewayLiteralIPPool(t *testing.T) {
	subnetName := "test-subnet"
	staticIP := "10.0.0.2"
	testSubnet := &kubeovnv1.Subnet{
		Name: subnetName,
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock:  "10.0.0.0/30",
			Gateway:    staticIP,
			Protocol:   kubeovnv1.ProtocolIPv4,
			Provider:   util.OvnProvider,
			ExcludeIps: []string{staticIP},
		},
	}
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: "default",
		Annotations: map[string]string{
			util.IPPoolAnnotation: staticIP,
		},
	}
	podNet := &kubeovnNet{
		Subnet:       testSubnet,
		ProviderName: util.OvnProvider,
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{testSubnet},
		Pods:    []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController
	ctrl.ipam = newIPAMForTest([]*kubeovnv1.Subnet{testSubnet})
	ctrl.ipam.Subnets[subnetName].V4Gw = staticIP

	_, _, _, _, err = ctrl.acquireStaticAddressHelper(
		pod,
		podNet,
		"default.test-pod",
		nil,
		staticIP,
		[]*kubeovnNet{podNet},
		false,
		"default/test-pod",
	)
	require.ErrorIs(t, err, ipam.ErrConflict)
}

func newIPAMForTest(subnets []*kubeovnv1.Subnet) *ipam.IPAM {
	ipamInstance := ipam.NewIPAM()
	for _, subnet := range subnets {
		excludeIPs := subnet.Spec.ExcludeIps
		if len(excludeIPs) == 0 {
			excludeIPs = []string{}
		}
		s, err := ipam.NewSubnet(subnet.Name, subnet.Spec.CIDRBlock, excludeIPs)
		if err != nil {
			panic(err)
		}
		ipamInstance.Subnets[subnet.Name] = s
	}
	return ipamInstance
}

func TestGetNamedPortByNsReturnsCopy(t *testing.T) {
	np := NewNamedPort()
	pod := &corev1.Pod{
		Namespace: "test-ns",
		Name:      "test-pod",
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: 80},
					},
				},
			},
		},
	}

	np.AddNamedPortByPod(pod)

	result := np.GetNamedPortByNs("test-ns")
	require.NotNil(t, result)
	assert.Contains(t, result, "http")

	// Mutating the returned map should not affect internal state
	delete(result, "http")

	result2 := np.GetNamedPortByNs("test-ns")
	require.NotNil(t, result2)
	assert.Contains(t, result2, "http", "internal map should not be affected by mutation of returned copy")
}

func TestDeleteNamedPortByPodWithRestartableInitContainers(t *testing.T) {
	restartAlways := corev1.ContainerRestartPolicyAlways
	np := NewNamedPort()
	pod := &corev1.Pod{
		Namespace: "test-ns",
		Name:      "test-pod",
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:          "sidecar",
					RestartPolicy: &restartAlways,
					Ports: []corev1.ContainerPort{
						{Name: "metrics", ContainerPort: 9090},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: 80},
					},
				},
			},
		},
	}

	np.AddNamedPortByPod(pod)
	result := np.GetNamedPortByNs("test-ns")
	require.NotNil(t, result)
	assert.Contains(t, result, "http")
	assert.Contains(t, result, "metrics")

	np.DeleteNamedPortByPod(pod)
	result = np.GetNamedPortByNs("test-ns")
	assert.Empty(t, result, "both regular and sidecar init container named ports should be deleted")
}

func TestHasAliveSiblingVMPod(t *testing.T) {
	vmiOwner := func(vmName string) []metav1.OwnerReference {
		return []metav1.OwnerReference{
			{
				APIVersion: kubevirtv1.SchemeGroupVersion.String(),
				Kind:       util.KindVirtualMachineInstance,
				Name:       vmName,
			},
		}
	}
	vmPod := func(name, vmName string, phase corev1.PodPhase, deleted bool) *corev1.Pod {
		p := &corev1.Pod{
			Namespace:       "ns",
			Name:            name,
			OwnerReferences: vmiOwner(vmName),
			Status:          corev1.PodStatus{Phase: phase},
		}
		if deleted {
			now := metav1.Now()
			p.DeletionTimestamp = &now
			grace := int64(0)
			p.DeletionGracePeriodSeconds = &grace
		}
		return p
	}

	tests := []struct {
		name           string
		pods           []*corev1.Pod
		vmName         string
		excludePodName string
		want           bool
	}{
		{
			name:           "no siblings",
			pods:           []*corev1.Pod{vmPod("virt-launcher-vm-aaa", "vm", corev1.PodRunning, false)},
			vmName:         "vm",
			excludePodName: "virt-launcher-vm-aaa",
			want:           false,
		},
		{
			name: "alive sibling exists",
			pods: []*corev1.Pod{
				vmPod("virt-launcher-vm-aaa", "vm", corev1.PodSucceeded, true),
				vmPod("virt-launcher-vm-bbb", "vm", corev1.PodRunning, false),
			},
			vmName:         "vm",
			excludePodName: "virt-launcher-vm-aaa",
			want:           true,
		},
		{
			name: "only completed siblings",
			pods: []*corev1.Pod{
				vmPod("virt-launcher-vm-aaa", "vm", corev1.PodSucceeded, true),
				vmPod("virt-launcher-vm-bbb", "vm", corev1.PodSucceeded, false),
			},
			vmName:         "vm",
			excludePodName: "virt-launcher-vm-aaa",
			want:           false,
		},
		{
			name: "sibling belongs to different vm",
			pods: []*corev1.Pod{
				vmPod("virt-launcher-other-xxx", "other", corev1.PodRunning, false),
			},
			vmName:         "vm",
			excludePodName: "virt-launcher-vm-aaa",
			want:           false,
		},
		{
			name: "non-vm pod ignored",
			pods: []*corev1.Pod{
				{
					Namespace: "ns", Name: "plain-pod",
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			vmName:         "vm",
			excludePodName: "virt-launcher-vm-aaa",
			want:           false,
		},
		{
			name: "excluded pod ignored even when alive",
			pods: []*corev1.Pod{
				vmPod("virt-launcher-vm-aaa", "vm", corev1.PodRunning, false),
			},
			vmName:         "vm",
			excludePodName: "virt-launcher-vm-aaa",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAliveSiblingVMPod(tt.pods, tt.vmName, tt.excludePodName)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGetPodAttachmentNetDefaultSubnetGone guards against a nil pointer panic:
// for a terminating pod whose OVN attachment network resolves to no subnet
// (no per-provider logical_switch annotation and no matching Subnet.Spec.Provider),
// getPodAttachmentNet falls back to getPodDefaultSubnet, which returns (nil, nil)
// when the pod's top-level logical_switch annotation points at an already-deleted
// subnet. The attachment must be skipped so gc can clean its ip cr.
func TestGetPodAttachmentNetDefaultSubnetGone(t *testing.T) {
	now := metav1.Now()
	grace := int64(0)
	pod := &corev1.Pod{
		Name:                       "test-pod",
		Namespace:                  metav1.NamespaceDefault,
		DeletionTimestamp:          &now,
		DeletionGracePeriodSeconds: &grace,
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
			// top-level default subnet points at a subnet that no longer exists
			util.LogicalSwitchAnnotation: "deleted-subnet",
			// no per-provider net1.default.ovn logical_switch annotation, so the
			// attachment cannot resolve a subnet and falls back to the default
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		// NAD is an OVN network but no Subnet has Provider == net1.default.ovn
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{
			{
				Name:      "net1",
				Namespace: metav1.NamespaceDefault,
				Spec: nadv1.NetworkAttachmentDefinitionSpec{
					Config: `{"cniVersion": "0.3.1", "name": "net1", "type": "kube-ovn"}`,
				},
			},
		},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	// must not panic; the unresolvable attachment is skipped
	nets, err := controller.getPodAttachmentNet(pod)
	require.NoError(t, err)
	assert.Empty(t, nets)
}

// TestGetPodAttachmentNetIPAMOnlyNADGone reproduces issue #6943: when an IPAM-only
// attachment (e.g. ipvlan with ipam.type kube-ovn) is deleted after its NAD has
// already been removed, the cleanup path must still resolve the attachment to its
// subnet so the IP is released. The subnet provider is "<nad>.<namespace>" without
// the ".ovn" suffix, so the returned net must carry that exact provider name so the
// released IP CR name matches what was allocated.
func TestGetPodAttachmentNetIPAMOnlyNADGone(t *testing.T) {
	now := metav1.Now()
	grace := int64(0)
	pod := &corev1.Pod{
		Name:                       "test-pod",
		Namespace:                  metav1.NamespaceDefault,
		DeletionTimestamp:          &now,
		DeletionGracePeriodSeconds: &grace,
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		// NAD intentionally absent: it was deleted before the pod.
		Subnets: []*kubeovnv1.Subnet{
			{
				Name: "ipam-net1",
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock: "10.1.0.0/16",
					// IPAM-only provider: no ".ovn" suffix
					Provider: "net1.default",
				},
			},
		},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	nets, err := controller.getPodAttachmentNet(pod)
	require.NoError(t, err)
	require.Len(t, nets, 1)
	assert.Equal(t, "net1.default", nets[0].ProviderName)
	assert.Equal(t, "ipam-net1", nets[0].Subnet.Name)
}

func TestHandleAddOrUpdatePodRecordsIPAMSubnetMissingEvent(t *testing.T) {
	controller := newIPAMSubnetMissingController(t, ipamNADConfig(util.CniTypeName))

	err := controller.handleAddOrUpdatePod("default/test-pod")
	require.Error(t, err)

	assertPodEvent(t, controller, "Warning PodNetworkUpdateFailed", "stage=getPodKubeovnNets", "provider net1.default is not bound to any subnet")
}

func TestHandleAddOrUpdatePodSyncFailureRecordsOriginalPod(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	injectedErr := errors.New("list ports failed")
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, injectedErr)
	events := useRealPodEventRecorder(t, fc.fakeController)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.ErrorIs(t, err, injectedErr)
	assertRecordedPodEvent(t, events, pod, "PodNetworkUpdateFailed", "stage=syncKubeOvnNet")
}

func TestEnqueueUpdatePodRecordsIPAMSubnetMissingEvent(t *testing.T) {
	controller := newIPAMSubnetMissingController(t, ipamNADConfig(util.CniTypeName))
	oldPod := &corev1.Pod{
		Name:            "test-pod",
		Namespace:       metav1.NamespaceDefault,
		ResourceVersion: "1",
		Annotations:     map[string]string{},
	}
	newPod, err := controller.podsLister.Pods(metav1.NamespaceDefault).Get("test-pod")
	require.NoError(t, err)
	newPod = newPod.DeepCopy()
	newPod.ResourceVersion = "2"

	controller.enqueueUpdatePod(oldPod, newPod)

	assertPodEvent(t, controller, "Warning PodNetworkUpdateFailed", "stage=getPodKubeovnNets", "provider net1.default is not bound to any subnet")
}

func TestHandleUpdatePodSecurityRecordsIPAMSubnetMissingEvent(t *testing.T) {
	controller := newIPAMSubnetMissingController(t, ipamNADConfig(util.CniTypeName))

	err := controller.handleUpdatePodSecurity("default/test-pod")
	require.Error(t, err)

	assertPodEvent(t, controller, "Warning PodSecurityUpdateFailed", "stage=getPodKubeovnNets", "provider net1.default is not bound to any subnet")
}

func TestPodNetworkEventDetailsIncludesMultipleNetworks(t *testing.T) {
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: metav1.NamespaceDefault,
		Annotations: map[string]string{
			util.IPAddressAnnotation:  "10.0.0.2",
			util.MacAddressAnnotation: "00:00:00:00:00:01",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):  "10.1.0.2",
			fmt.Sprintf(util.MacAddressAnnotationTemplate, "net1.default.ovn"): "00:00:00:00:00:02",
		},
	}
	controller := newFakeController(t).fakeController
	nets := []*kubeovnNet{
		{ProviderName: util.OvnProvider, Subnet: &kubeovnv1.Subnet{Name: "subnet-a"}},
		{ProviderName: "net1.default.ovn", Subnet: &kubeovnv1.Subnet{Name: "subnet-b"}},
	}

	details := controller.podNetworkEventDetails(pod, nets)

	for _, part := range []string{
		"provider=ovn", "subnet=subnet-a", "ip=10.0.0.2", "mac=00:00:00:00:00:01", "logicalSwitchPort=test-pod.default",
		"provider=net1.default.ovn", "subnet=subnet-b", "ip=10.1.0.2", "mac=00:00:00:00:00:02", "logicalSwitchPort=test-pod.default.net1.default.ovn",
		"; ",
	} {
		assert.Contains(t, details, part)
	}
}

func TestSyncKubeOvnNetReportsAnnotationChange(t *testing.T) {
	pod := &corev1.Pod{Name: "test-pod", Namespace: metav1.NamespaceDefault, Annotations: map[string]string{}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)

	updatedPod, details, err := fc.fakeController.syncKubeOvnNet(pod, []*kubeovnNet{{
		ProviderName: util.OvnProvider,
		IPRequest:    "10.0.0.2",
		Subnet:       &kubeovnv1.Subnet{Name: "subnet-a"},
	}})

	require.NoError(t, err)
	assert.NotEmpty(t, details)
	assert.Equal(t, "10.0.0.2", updatedPod.Annotations[util.IPAddressAnnotation])
}

func TestSyncKubeOvnNetReportsNoChange(t *testing.T) {
	pod := &corev1.Pod{
		Name:        "test-pod",
		Namespace:   metav1.NamespaceDefault,
		Annotations: map[string]string{util.IPAddressAnnotation: "10.0.0.2"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)

	_, details, err := fc.fakeController.syncKubeOvnNet(pod, []*kubeovnNet{{
		ProviderName: util.OvnProvider,
		IPRequest:    "10.0.0.2",
	}})

	require.NoError(t, err)
	assert.Empty(t, details)
	assertNoPodEvent(t, fc.fakeController)
}

func TestSyncKubeOvnNetParsesStalePortProviders(t *testing.T) {
	const (
		provider = "net1.default.ovn"
		keepKey  = "example.com/keep"
	)

	t.Run("default OVN port", func(t *testing.T) {
		pod := &corev1.Pod{
			Name: "test-pod", Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{util.AllocatedAnnotation: "true", keepKey: "true"},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		require.NoError(t, err)
		portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
		fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)

		updatedPod, details, err := fc.fakeController.syncKubeOvnNet(pod, nil)

		require.NoError(t, err)
		assert.Equal(t, "true", updatedPod.Annotations[util.AllocatedAnnotation])
		assert.Equal(t, "true", updatedPod.Annotations[keepKey])
		assert.Contains(t, details, "provider="+util.OvnProvider)
		assert.Contains(t, details, "logicalSwitchPort="+portName)
	})

	t.Run("attachment provider containing dots", func(t *testing.T) {
		providerKey := fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)
		pod := &corev1.Pod{
			Name: "test-pod", Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{providerKey: "true", keepKey: "true"},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		require.NoError(t, err)
		portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
		fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)

		updatedPod, details, err := fc.fakeController.syncKubeOvnNet(pod, nil)

		require.NoError(t, err)
		assert.NotContains(t, updatedPod.Annotations, providerKey)
		assert.Equal(t, "true", updatedPod.Annotations[keepKey])
		assert.Contains(t, details, "provider="+provider)
		assert.Contains(t, details, "logicalSwitchPort="+portName)
	})

	t.Run("VM name containing dots", func(t *testing.T) {
		providerKey := fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)
		pod := &corev1.Pod{
			Name: "virt-launcher-test", Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{providerKey: "true", keepKey: "true"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: kubevirtv1.SchemeGroupVersion.String(), Kind: util.KindVirtualMachineInstance, Name: "test.vm",
			}},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}})
		require.NoError(t, err)
		fc.fakeController.config.EnableKeepVMIP = true
		portName := ovs.PodNameToPortName("test.vm", pod.Namespace, provider)
		fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)

		updatedPod, details, err := fc.fakeController.syncKubeOvnNet(pod, nil)

		require.NoError(t, err)
		assert.NotContains(t, updatedPod.Annotations, providerKey)
		assert.Equal(t, "true", updatedPod.Annotations[keepKey])
		assert.Contains(t, details, "provider="+provider)
		assert.Contains(t, details, "logicalSwitchPort="+portName)
	})

	t.Run("unmatched pod prefix", func(t *testing.T) {
		providerKey := fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)
		pod := &corev1.Pod{
			Name: "test-pod", Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{providerKey: "true", keepKey: "true"},
		}
		_, subnet := podEventFixture()
		portName := "legacy-port-name"
		ipCR := &kubeovnv1.IP{Name: portName, Spec: kubeovnv1.IPSpec{Subnet: subnet.Name}}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}, IPs: []*kubeovnv1.IP{ipCR},
		})
		require.NoError(t, err)
		fc.fakeController.config.EnableNonPrimaryCNI = true
		require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
		_, _, _, err = fc.fakeController.ipam.GetStaticAddress("default/test-pod", portName, "10.0.0.2", nil, subnet.Name, false)
		require.NoError(t, err)
		fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{
			Name: portName,
			ExternalIDs: map[string]string{
				"pod": "default/test-pod",
				"ls":  subnet.Name,
			},
		}}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)

		err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

		require.NoError(t, err)
		updatedPod, err := fc.fakeController.config.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "true", updatedPod.Annotations[providerKey])
		assert.Equal(t, "true", updatedPod.Annotations[keepKey])
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), portName, metav1.GetOptions{})
		assert.True(t, k8serrors.IsNotFound(err))
		assert.Empty(t, fc.fakeController.ipam.GetPodAddress("default/test-pod"))
		event := assertPodEvent(t, fc.fakeController, "Normal PodNetworkUpdated", "provider=unknown", "subnet="+subnet.Name, "logicalSwitchPort="+portName)
		assert.NotContains(t, event, "provider="+provider)
		assert.NotContains(t, event, "provider= ")
		assertNoPodEvent(t, fc.fakeController)
	})
}

func TestHandleUpdatePodSecurityRecordsSuccess(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[util.MacAddressAnnotation] = "00:00:00:00:00:01"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortSecurity(false, portName, "00:00:00:00:00:01", "10.0.0.2", "").Return(nil)
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, false).Return(&ovnnb.LogicalSwitchPort{Name: portName, ExternalIDs: map[string]string{}}, nil)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortExternalIDs(portName, map[string]string{"security_groups": ""}).Return(nil)

	err = fc.fakeController.handleUpdatePodSecurity("default/test-pod")

	require.NoError(t, err)
	assertPodEvent(t, fc.fakeController, "Normal PodSecurityUpdated", "provider=ovn", "subnet=subnet-a", "ip=10.0.0.2", "mac=00:00:00:00:00:01", "logicalSwitchPort=test-pod.default")
}

func TestHandleUpdatePodSecurityRecordsFailure(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortSecurity(false, portName, "", "", "").Return(errors.New("set security failed"))

	err = fc.fakeController.handleUpdatePodSecurity("default/test-pod")

	require.EqualError(t, err, "set security failed")
	assertPodEvent(t, fc.fakeController, "Warning PodSecurityUpdateFailed", "stage=setLogicalSwitchPortSecurity", "set security failed")
}

func TestHandleUpdatePodSecuritySkipsNonOVNNetworkWithoutSuccess(t *testing.T) {
	controller := newIPAMNetworkController(t)

	err := controller.handleUpdatePodSecurity("default/test-pod")

	require.NoError(t, err)
	assertNoPodEvent(t, controller)
}

func TestHandleUpdatePodSecurityReportsOnlyProcessedOVNNetworks(t *testing.T) {
	const ipamProvider = "net1.default"
	pod, ovnSubnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[util.MacAddressAnnotation] = "00:00:00:00:00:01"
	pod.Annotations[nadv1.NetworkAttachmentAnnot] = `[{"name":"net1"}]`
	pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, ipamProvider)] = "true"
	pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, ipamProvider)] = "10.1.0.2"
	ipamSubnet := &kubeovnv1.Subnet{Name: "ipam-subnet", Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.1.0.0/24", Protocol: kubeovnv1.ProtocolIPv4, Provider: ipamProvider,
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:    []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{ovnSubnet, ipamSubnet},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: metav1.NamespaceDefault,
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: ipamNADConfig(util.CniTypeName)},
		}},
	})
	require.NoError(t, err)
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortSecurity(false, portName, "00:00:00:00:00:01", "10.0.0.2", "").Return(nil)
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, false).Return(&ovnnb.LogicalSwitchPort{Name: portName, ExternalIDs: map[string]string{}}, nil)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortExternalIDs(portName, map[string]string{"security_groups": ""}).Return(nil)

	err = fc.fakeController.handleUpdatePodSecurity("default/test-pod")

	require.NoError(t, err)
	event := assertPodEvent(t, fc.fakeController, "Normal PodSecurityUpdated", "provider=ovn", "logicalSwitchPort=test-pod.default")
	assert.NotContains(t, event, "provider="+ipamProvider)
	assert.NotContains(t, event, "logicalSwitchPort=test-pod.default.net1.default")
}

func TestHandleDeletePodRecordsReleasedPort(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": "default/test-pod"}).Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(nil)
	storeDeletingPod(fc.fakeController, pod)

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.NoError(t, err)
	assertPodEvent(t, fc.fakeController, "Normal PodNetworkReleased", "provider=ovn", "subnet=subnet-a", "logicalSwitchPort=test-pod.default")
}

func TestHandleDeletePodRecordsFailure(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": "default/test-pod"}).Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).Return(errors.New("delete lsp failed"))
	storeDeletingPod(fc.fakeController, pod)

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.EqualError(t, err, "delete lsp failed")
	assertPodEvent(t, fc.fakeController, "Warning PodNetworkReleaseFailed", "stage=deleteLogicalSwitchPort", "delete lsp failed")
}

func TestHandleDeletePodRetriesOrphanedVMPortIPLookupFailure(t *testing.T) {
	pod, subnet := podEventFixture()
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: kubevirtv1.SchemeGroupVersion.String(),
		Kind:       util.KindVirtualMachineInstance,
		Name:       "test-vm",
	}}
	portName := ovs.PodNameToPortName("test-vm", pod.Namespace, "old.default.ovn")
	ipCR := &kubeovnv1.IP{Name: portName, Spec: kubeovnv1.IPSpec{
		PodName: "test-vm", Namespace: pod.Namespace, Subnet: subnet.Name,
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}, IPs: []*kubeovnv1.IP{ipCR},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableKeepVMIP = true
	ipLister := &errorOnceIPLister{ip: ipCR, err: errors.New("get ip failed")}
	fc.fakeController.ipsLister = ipLister
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	_, _, _, err = fc.fakeController.ipam.GetStaticAddress("default/test-vm", portName, "10.0.0.2", nil, subnet.Name, false)
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	mockVMIs := kubecli.NewMockVirtualMachineInstanceInterface(mockCtrl)
	mockVMs := kubecli.NewMockVirtualMachineInterface(mockCtrl)
	mockKubevirt := kubecli.NewMockKubevirtClient(mockCtrl)
	mockKubevirt.EXPECT().VirtualMachineInstance(metav1.NamespaceDefault).Return(mockVMIs).Times(2)
	mockVMIs.EXPECT().Get(gomock.Any(), "test-vm", gomock.Any()).Return(&kubevirtv1.VirtualMachineInstance{}, nil).Times(2)
	mockKubevirt.EXPECT().VirtualMachine(metav1.NamespaceDefault).Return(mockVMs).Times(4)
	vm := &kubevirtv1.VirtualMachine{Spec: kubevirtv1.VirtualMachineSpec{Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.LogicalSwitchAnnotation: subnet.Name}},
		Spec: kubevirtv1.VirtualMachineInstanceSpec{Networks: []kubevirtv1.Network{{
			Name: "net1",
			Multus: &kubevirtv1.MultusNetwork{
				NetworkName: "default/net1",
			},
		}}},
	}}}
	mockVMs.EXPECT().Get(gomock.Any(), "test-vm", gomock.Any()).Return(vm, nil).Times(4)
	fc.fakeController.config.KubevirtClient = mockKubevirt

	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": "default/test-vm"}).Return([]ovnnb.LogicalSwitchPort{{Name: portName}}, nil).Times(2)
	fc.mockOvnClient.EXPECT().CleanLogicalSwitchPortMigrateOptions(portName).Return(nil).Times(2)
	deleteCalls := 0
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(portName).DoAndReturn(func(string) error {
		deleteCalls++
		return nil
	}).AnyTimes()
	storeDeletingPod(fc.fakeController, pod)

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.EqualError(t, err, "get ip failed")
	assertPodEvent(t, fc.fakeController, "Warning PodNetworkReleaseFailed", "stage=getIPCR", "get ip failed")
	assertNoPodEvent(t, fc.fakeController)
	assert.Zero(t, deleteCalls)
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), portName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.NotEmpty(t, fc.fakeController.ipam.GetPodAddress("default/test-vm"))

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.NoError(t, err)
	assert.Equal(t, 1, deleteCalls)
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), portName, metav1.GetOptions{})
	assert.True(t, k8serrors.IsNotFound(err))
	assert.Empty(t, fc.fakeController.ipam.GetPodAddress("default/test-vm"))
	event := assertPodEvent(t, fc.fakeController, "Normal PodNetworkReleased", "logicalSwitchPort="+portName, "ipCR="+portName, "ipam="+portName)
	assert.NotContains(t, event, "provider=ovn")
	assertNoPodEvent(t, fc.fakeController)
}

func TestHandleDeletePodReportsStatefulSetOwnerCheckStage(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Name = "test-sts-0"
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: appsv1.SchemeGroupVersion.String(),
		Kind:       util.KindStatefulSet,
		Name:       "test-sts",
		UID:        "sts-uid",
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	fc.fakeController.config.KubeClient.(*k8sfake.Clientset).PrependReactor("get", "statefulsets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("get statefulset failed")
	})
	storeDeletingPod(fc.fakeController, pod)

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.EqualError(t, err, "get statefulset failed")
	assertPodEvent(t, fc.fakeController, "Warning PodNetworkReleaseFailed", "stage=checkStatefulSetOwner", "get statefulset failed")
}

func TestHandleDeletePodWithoutResourcesDoesNotRecordSuccess(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{"pod": "default/test-pod"}).Return(nil, nil)
	storeDeletingPod(fc.fakeController, pod)

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.NoError(t, err)
	assertNoPodEvent(t, fc.fakeController)
}

func TestHandleDeletePodWithReplacementDoesNotRecordSuccess(t *testing.T) {
	deletedPod, subnet := podEventFixture()
	livePod := deletedPod.DeepCopy()
	livePod.UID = "replacement-uid"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{livePod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	storeDeletingPod(fc.fakeController, deletedPod)

	err = fc.fakeController.handleDeletePod("default/test-pod")

	require.NoError(t, err)
	assertNoPodEvent(t, fc.fakeController)
}

func TestHandleAddOrUpdatePodRecordsAllocationSuccess(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Annotations[util.MacAddressAnnotation] = "00:00:00:00:00:01"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(subnet.Name, portName, gomock.Any(), subnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil).Times(2)
	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(subnet.Name, portName, gomock.Any(), gomock.Any(), pod.Name, pod.Namespace, false, "", "", false, gomock.Any(), util.DefaultVpc).Return(nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	assertPodEvent(t, fc.fakeController, "Normal PodNetworkAllocated", "provider=ovn", "subnet=subnet-a", "ip=10.0.0.", "mac=00:00:00:00:00:01", "logicalSwitchPort=test-pod.default")
}

func TestHandleAddOrUpdatePodReportsOnlyAllocatedNetworks(t *testing.T) {
	const provider = "net1.default.ovn"
	pod, defaultSubnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[nadv1.NetworkAttachmentAnnot] = `[{"name":"net1"}]`
	pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)] = "subnet-b"
	attachmentSubnet := &kubeovnv1.Subnet{Name: "subnet-b", Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.1.0.0/24", Gateway: "10.1.0.1", Protocol: kubeovnv1.ProtocolIPv4, Provider: provider, Vpc: util.DefaultVpc,
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:    []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{defaultSubnet, attachmentSubnet},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: metav1.NamespaceDefault,
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.3.1","name":"net1","type":"kube-ovn","provider":"net1.default.ovn"}`},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(attachmentSubnet.Name, attachmentSubnet.Spec.CIDRBlock, attachmentSubnet.Spec.Gateway, nil))
	defaultPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	attachmentPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: defaultPort}}, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(attachmentSubnet.Name, attachmentPort, gomock.Any(), attachmentSubnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil).Times(2)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(defaultSubnet.Name, defaultPort, gomock.Any(), defaultSubnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(attachmentSubnet.Name, attachmentPort, gomock.Any(), gomock.Any(), pod.Name, pod.Namespace, false, "", "", false, gomock.Any(), util.DefaultVpc).Return(nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	event := assertPodEvent(t, fc.fakeController, "Normal PodNetworkAllocated", "provider="+provider, "subnet=subnet-b", "logicalSwitchPort="+attachmentPort)
	assert.NotContains(t, event, "provider=ovn")
}

func TestHandleAddOrUpdatePodCombinesAllocatedAndRemovedNetworks(t *testing.T) {
	const (
		allocatedProvider = "net1.default.ovn"
		removedProvider   = "old.default.ovn"
	)
	pod, defaultSubnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[nadv1.NetworkAttachmentAnnot] = `[{"name":"net1"}]`
	pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, allocatedProvider)] = "subnet-b"
	pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, removedProvider)] = "true"
	pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, removedProvider)] = "subnet-old"
	attachmentSubnet := &kubeovnv1.Subnet{Name: "subnet-b", Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.1.0.0/24", Gateway: "10.1.0.1", Protocol: kubeovnv1.ProtocolIPv4, Provider: allocatedProvider, Vpc: util.DefaultVpc,
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:    []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{defaultSubnet, attachmentSubnet},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: metav1.NamespaceDefault,
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.3.1","name":"net1","type":"kube-ovn","provider":"net1.default.ovn"}`},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(attachmentSubnet.Name, attachmentSubnet.Spec.CIDRBlock, attachmentSubnet.Spec.Gateway, nil))
	defaultPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	allocatedPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, allocatedProvider)
	removedPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, removedProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{
		{Name: defaultPort},
		{Name: removedPort, ExternalIDs: map[string]string{"ls": "subnet-old"}},
	}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(removedPort).Return(nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(attachmentSubnet.Name, allocatedPort, gomock.Any(), attachmentSubnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil).Times(2)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(defaultSubnet.Name, defaultPort, gomock.Any(), defaultSubnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(attachmentSubnet.Name, allocatedPort, gomock.Any(), gomock.Any(), pod.Name, pod.Namespace, false, "", "", false, gomock.Any(), util.DefaultVpc).Return(nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	assertPodEvent(
		t, fc.fakeController,
		"Normal PodNetworkAllocated",
		"provider="+allocatedProvider,
		"logicalSwitchPort="+allocatedPort,
		"provider="+removedProvider,
		"logicalSwitchPort="+removedPort,
	)
	assertNoPodEvent(t, fc.fakeController)
}

func TestHandleAddOrUpdatePodReportsOnlyRoutedNetworks(t *testing.T) {
	const provider = "net1.default.ovn"
	pod, defaultSubnet := podEventFixture()
	pod.Spec.NodeName = "node-a"
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[nadv1.NetworkAttachmentAnnot] = `[{"name":"net1"}]`
	pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] = "true"
	pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)] = "subnet-b"
	pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)] = "10.1.0.2"
	attachmentSubnet := &kubeovnv1.Subnet{Name: "subnet-b", Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.1.0.0/24", Gateway: "10.1.0.1", Protocol: kubeovnv1.ProtocolIPv4, Provider: provider, Vpc: "other-vpc",
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:    []*corev1.Pod{pod},
		Nodes:   []*corev1.Node{{Name: pod.Spec.NodeName}},
		Subnets: []*kubeovnv1.Subnet{defaultSubnet, attachmentSubnet},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: metav1.NamespaceDefault,
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.3.1","name":"net1","type":"kube-ovn","provider":"net1.default.ovn"}`},
		}},
	})
	require.NoError(t, err)
	defaultPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	attachmentPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{{Name: defaultPort}, {Name: attachmentPort}}, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(defaultSubnet.Name, defaultPort, gomock.Any(), defaultSubnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(attachmentSubnet.Name, attachmentPort, gomock.Any(), attachmentSubnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
	fc.mockOvnClient.EXPECT().ListPortGroups(gomock.Any()).Return(nil, nil).Times(2)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	event := assertPodEvent(t, fc.fakeController, "Normal PodNetworkUpdated", "provider="+provider, "subnet=subnet-b", "logicalSwitchPort="+attachmentPort)
	assert.NotContains(t, event, "provider=ovn")
}

func TestHandleAddOrUpdatePodRecordsAllocationDHCPFailureOnce(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(subnet.Name, portName, gomock.Any(), subnet.Spec.CIDRBlock, "", "", "", 0).Return(nil, false, errors.New("allocate dhcp failed"))

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.EqualError(t, err, "allocate dhcp failed")
	assertPodEvent(t, fc.fakeController, "Warning PodNetworkAllocationFailed", "stage=reconcilePortDHCPOptions", "allocate dhcp failed")
	assertNoPodEvent(t, fc.fakeController)
}

func TestReconcileAllocateSubnetsSpecificFailureEventsIncludeStage(t *testing.T) {
	t.Run("acquire address", func(t *testing.T) {
		pod, subnet := podEventFixture()
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
		require.NoError(t, err)

		_, err = fc.fakeController.reconcileAllocateSubnets(pod, []*kubeovnNet{{Type: providerTypeOriginal, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})

		require.Error(t, err)
		assertPodEvent(t, fc.fakeController, "Warning AcquireAddressFailed", "stage=acquireAddress", err.Error())
	})

	t.Run("validate network broadcast", func(t *testing.T) {
		pod, subnet := podEventFixture()
		pod.Annotations[util.VipAnnotation] = "broadcast-vip"
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
		require.NoError(t, err)
		vip := &kubeovnv1.Vip{
			Name: "broadcast-vip", Labels: map[string]string{},
			Spec:   kubeovnv1.VipSpec{Subnet: subnet.Name},
			Status: kubeovnv1.VipStatus{V4ip: "10.0.0.0", Mac: "00:00:00:00:00:01"},
		}
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{})
		require.NoError(t, err)
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		require.NoError(t, indexer.Add(vip))
		fc.fakeController.virtualIpsLister = kubeovnlister.NewVipLister(indexer)

		_, err = fc.fakeController.reconcileAllocateSubnets(pod, []*kubeovnNet{{Type: providerTypeOriginal, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})

		require.Error(t, err)
		assertPodEvent(t, fc.fakeController, "Warning ValidatePodNetworkFailed", "stage=validateNetworkBroadcast", err.Error())
	})

	t.Run("get vlan info", func(t *testing.T) {
		pod, subnet := podEventFixture()
		subnet.Spec.Vlan = "missing-vlan"
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

		_, err = fc.fakeController.reconcileAllocateSubnets(pod, []*kubeovnNet{{Type: providerTypeOriginal, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})

		require.Error(t, err)
		assertPodEvent(t, fc.fakeController, "Warning GetVlanInfoFailed", "stage=getVlanInfo", err.Error())
	})

	t.Run("create logical switch port", func(t *testing.T) {
		pod, subnet := podEventFixture()
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
		fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
		fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("create port failed"))

		_, err = fc.fakeController.reconcileAllocateSubnets(pod, []*kubeovnNet{{Type: providerTypeOriginal, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})

		require.EqualError(t, err, "create port failed")
		assertPodEvent(t, fc.fakeController, "Warning CreateOVNPortFailed", "stage=createLogicalSwitchPort", err.Error())
	})

	t.Run("set logical switch port layer2 forward", func(t *testing.T) {
		pod, subnet := podEventFixture()
		pod.Annotations[fmt.Sprintf(util.Layer2ForwardAnnotationTemplate, util.OvnProvider)] = "true"
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
		fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)
		fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		fc.mockOvnClient.EXPECT().EnablePortLayer2forward(gomock.Any()).Return(errors.New("enable layer2 forward failed"))

		_, err = fc.fakeController.reconcileAllocateSubnets(pod, []*kubeovnNet{{Type: providerTypeOriginal, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true}})

		require.EqualError(t, err, "enable layer2 forward failed")
		assertPodEvent(t, fc.fakeController, "Warning SetOVNPortL2ForwardFailed", "stage=setLogicalSwitchPortLayer2Forward", err.Error())
	})
}

func TestReconcileAllocateSubnetsRefetchFailureRecordsOriginalPod(t *testing.T) {
	pod, subnet := podEventFixture()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	injectedErr := errors.New("get patched pod failed")
	fc.fakeController.config.KubeClient.(*k8sfake.Clientset).PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, injectedErr
	})
	events := useRealPodEventRecorder(t, fc.fakeController)

	updatedPod, err := fc.fakeController.reconcileAllocateSubnets(pod, []*kubeovnNet{{
		Type: providerTypeIPAM, ProviderName: util.OvnProvider, Subnet: subnet, IsDefault: true,
	}})

	require.Nil(t, updatedPod)
	require.ErrorIs(t, err, injectedErr)
	assertRecordedPodEvent(t, events, pod, "PodNetworkAllocationFailed", "stage=getPatchedPod")
}

func TestHandleAddOrUpdatePodReportsActualAllocatedSubnet(t *testing.T) {
	pod, subnetA := podEventFixture()
	pod.Annotations = map[string]string{
		util.IPAddressAnnotation:                                      "10.1.0.2",
		util.MacAddressAnnotation:                                     "00:00:00:00:00:01",
		fmt.Sprintf(util.PortVipAnnotationTemplate, util.OvnProvider): "10.1.0.100",
	}
	subnetB := subnetA.DeepCopy()
	subnetB.Name = "subnet-b"
	subnetB.Spec.CIDRBlock = "10.1.0.0/24"
	subnetB.Spec.Gateway = "10.1.0.1"
	namespace := &corev1.Namespace{
		Name: metav1.NamespaceDefault,
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: "subnet-a,subnet-b",
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:       []*corev1.Pod{pod},
		Subnets:    []*kubeovnv1.Subnet{subnetA, subnetB},
		Namespaces: []*corev1.Namespace{namespace},
	})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnetA.Name, subnetA.Spec.CIDRBlock, subnetA.Spec.Gateway, nil))
	require.NoError(t, fc.fakeController.ipam.AddOrUpdateSubnet(subnetB.Name, subnetB.Spec.CIDRBlock, subnetB.Spec.Gateway, nil))
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&ovs.DHCPOptionsUUIDs{}, false, nil).Times(2)
	fc.mockOvnClient.EXPECT().CreateLogicalSwitchPort("subnet-b", gomock.Any(), "10.1.0.2", "00:00:00:00:00:01", pod.Name, pod.Namespace, false, "", "10.1.0.100", false, gomock.Any(), util.DefaultVpc).Return(nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	event := assertPodEvent(t, fc.fakeController, "Normal PodNetworkAllocated", "provider=ovn", "subnet=subnet-b", "ip=10.1.0.2")
	assert.NotContains(t, event, "subnet=subnet-a")
}

func TestHandleAddOrUpdatePodRecordsHotplugUpdate(t *testing.T) {
	const provider = "net1.default.ovn"
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: metav1.NamespaceDefault,
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot:                                `[{"name":"net1","ips":["10.1.0.2"]}]`,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, provider):     "true",
			fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): "subnet-b",
			fmt.Sprintf(util.MacAddressAnnotationTemplate, provider):    "00:00:00:00:00:02",
			fmt.Sprintf(util.RoutedAnnotationTemplate, provider):        "true",
		},
	}
	subnet := &kubeovnv1.Subnet{Name: "subnet-b", Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.1.0.0/24", Gateway: "10.1.0.1", Protocol: kubeovnv1.ProtocolIPv4, Provider: provider, Vpc: util.DefaultVpc,
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:    []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{subnet},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: metav1.NamespaceDefault,
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.3.1","name":"net1","type":"kube-ovn","provider":"net1.default.ovn"}`},
		}},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableNonPrimaryCNI = true
	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(subnet.Name, portName, gomock.Any(), subnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	assertPodEvent(t, fc.fakeController, "Normal PodNetworkUpdated", "provider=net1.default.ovn", "subnet=subnet-b", "ip=10.1.0.2", "mac=00:00:00:00:00:02", "logicalSwitchPort=test-pod.default.net1.default.ovn")
}

func TestHandleAddOrUpdatePodReportsRemovedHotplugNetwork(t *testing.T) {
	const removedProvider = "old.default.ovn"
	pod, subnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	pod.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, removedProvider)] = "true"
	pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, removedProvider)] = "true"
	pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, removedProvider)] = "subnet-old"
	pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, removedProvider)] = "10.2.0.2"
	pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, removedProvider)] = "00:00:00:00:00:03"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	defaultPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	removedPort := ovs.PodNameToPortName(pod.Name, pod.Namespace, removedProvider)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{
		{Name: defaultPort},
		{Name: removedPort, ExternalIDs: map[string]string{"ls": "subnet-old"}},
	}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(removedPort).Return(nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(subnet.Name, defaultPort, gomock.Any(), subnet.Spec.CIDRBlock, "", "", "", 0).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	event := assertPodEvent(t, fc.fakeController, "Normal PodNetworkUpdated", "provider="+removedProvider, "subnet=subnet-old", "logicalSwitchPort="+removedPort)
	assert.NotContains(t, event, "provider=ovn")
}

func TestHandleAddOrUpdatePodRecordsDHCPUpdateFailure(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, false, errors.New("update dhcp failed"))

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.EqualError(t, err, "update dhcp failed")
	assertPodEvent(t, fc.fakeController, "Warning PodNetworkUpdateFailed", "stage=reconcilePodDHCPOptions", "update dhcp failed")
}

func TestHandleAddOrUpdatePodRecordsRouteFailure(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Spec.NodeName = "missing-node"
	pod.Annotations[util.AllocatedAnnotation] = "true"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-node")
	assertPodEvent(t, fc.fakeController, "Warning PodNetworkUpdateFailed", "stage=reconcileRouteSubnets", "missing-node")
}

func TestHandleAddOrUpdatePodWithoutWorkDoesNotRecordSuccess(t *testing.T) {
	pod, subnet := podEventFixture()
	pod.Annotations[util.AllocatedAnnotation] = "true"
	pod.Annotations[util.RoutedAnnotation] = "true"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(nil, nil)
	fc.mockOvnClient.EXPECT().ReconcilePortDHCPOptions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&ovs.DHCPOptionsUUIDs{}, false, nil)

	err = fc.fakeController.handleAddOrUpdatePod("default/test-pod")

	require.NoError(t, err)
	assertNoPodEvent(t, fc.fakeController)
}

func podEventFixture() (*corev1.Pod, *kubeovnv1.Subnet) {
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: metav1.NamespaceDefault,
		UID:       "pod-uid",
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: "subnet-a",
		},
	}
	subnet := &kubeovnv1.Subnet{
		Name: "subnet-a",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24",
			Gateway:   "10.0.0.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       util.DefaultVpc,
			Default:   true,
		},
		Status: kubeovnv1.SubnetStatus{V4AvailableIPs: internal.NewBigInt(253)},
	}
	return pod, subnet
}

func storeDeletingPod(controller *Controller, pod *corev1.Pod) {
	controller.deletingPodObjMap = xsync.NewMap[string, *corev1.Pod]()
	controller.deletingPodObjMap.Store("default/test-pod", pod)
}

type errorOnceIPLister struct {
	ip    *kubeovnv1.IP
	err   error
	calls int
}

func (l *errorOnceIPLister) List(labels.Selector) ([]*kubeovnv1.IP, error) {
	return []*kubeovnv1.IP{l.ip}, nil
}

func (l *errorOnceIPLister) Get(string) (*kubeovnv1.IP, error) {
	l.calls++
	if l.calls == 1 {
		return nil, l.err
	}
	return l.ip, nil
}

func newIPAMNetworkController(t *testing.T) *Controller {
	t.Helper()
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: metav1.NamespaceDefault,
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot: `[{"name":"net1"}]`,
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{Name: "ipam-subnet", Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.1.0.0/24", Provider: "net1.default", Protocol: kubeovnv1.ProtocolIPv4,
		}}},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: metav1.NamespaceDefault,
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: ipamNADConfig(util.CniTypeName)},
		}},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableNonPrimaryCNI = true
	return fc.fakeController
}

func TestGetPodAttachmentNetIgnoresNonKubeOVNIPAMWithoutSubnet(t *testing.T) {
	controller := newIPAMSubnetMissingController(t, ipamNADConfig("host-local"))
	pod, err := controller.podsLister.Pods(metav1.NamespaceDefault).Get("test-pod")
	require.NoError(t, err)

	nets, err := controller.getPodAttachmentNet(pod)
	require.NoError(t, err)
	assert.Empty(t, nets)
}

func TestGetPodAttachmentNetIPAMConflistWithoutSubnet(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "Kube-OVN IPAM returns an error",
			config:  `{"cniVersion":"0.3.1","name":"net1","plugins":[{"type":"macvlan","ipam":{"type":"kube-ovn"}}]}`,
			wantErr: true,
		},
		{
			name:   "non-Kube-OVN IPAM is ignored",
			config: `{"cniVersion":"0.3.1","name":"net1","plugins":[{"type":"macvlan","ipam":{"type":"host-local"}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := newIPAMSubnetMissingController(t, tt.config)
			pod, err := controller.podsLister.Pods(metav1.NamespaceDefault).Get("test-pod")
			require.NoError(t, err)

			nets, err := controller.getPodAttachmentNet(pod)
			if tt.wantErr {
				require.EqualError(t, err, "provider net1.default is not bound to any subnet")
				return
			}
			require.NoError(t, err)
			assert.Empty(t, nets)
		})
	}
}

func TestGetPodAttachmentNetIgnoresMissingKubeOVNIPAMSubnetForDeletingPod(t *testing.T) {
	controller := newIPAMSubnetMissingController(t, ipamNADConfig(util.CniTypeName))
	pod, err := controller.podsLister.Pods(metav1.NamespaceDefault).Get("test-pod")
	require.NoError(t, err)
	pod = pod.DeepCopy()
	now := metav1.Now()
	pod.DeletionTimestamp = &now

	nets, err := controller.getPodAttachmentNet(pod)
	require.NoError(t, err)
	assert.Empty(t, nets)
}

func newIPAMSubnetMissingController(t *testing.T, config string) *Controller {
	t.Helper()

	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: metav1.NamespaceDefault,
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{
			{
				Name:      "net1",
				Namespace: metav1.NamespaceDefault,
				Spec: nadv1.NetworkAttachmentDefinitionSpec{
					Config: config,
				},
			},
		},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController
	controller.config.EnableNonPrimaryCNI = true
	return controller
}

func ipamNADConfig(ipamType string) string {
	return fmt.Sprintf(`{"cniVersion":"0.3.1","name":"net1","type":"macvlan","ipam":{"type":%q}}`, ipamType)
}

func assertPodEvent(t *testing.T, controller *Controller, parts ...string) string {
	t.Helper()

	recorder := controller.recorder.(*record.FakeRecorder)
	select {
	case event := <-recorder.Events:
		for _, part := range parts {
			assert.Contains(t, event, part)
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("expected pod event")
		return ""
	}
}

func assertNoPodEvent(t *testing.T, controller *Controller) {
	t.Helper()

	recorder := controller.recorder.(*record.FakeRecorder)
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected pod event: %s", event)
	default:
	}
}

func useRealPodEventRecorder(t *testing.T, controller *Controller) <-chan *corev1.Event {
	t.Helper()

	events := make(chan *corev1.Event, 1)
	broadcaster := record.NewBroadcaster()
	watcher := broadcaster.StartEventWatcher(func(event *corev1.Event) {
		events <- event
	})
	t.Cleanup(func() {
		watcher.Stop()
		broadcaster.Shutdown()
	})
	controller.recorder = broadcaster.NewRecorder(kubescheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	return events
}

func assertRecordedPodEvent(t *testing.T, events <-chan *corev1.Event, pod *corev1.Pod, reason string, messageParts ...string) {
	t.Helper()

	select {
	case event := <-events:
		assert.Equal(t, corev1.EventTypeWarning, event.Type)
		assert.Equal(t, reason, event.Reason)
		assert.Equal(t, "Pod", event.InvolvedObject.Kind)
		assert.Equal(t, pod.Namespace, event.InvolvedObject.Namespace)
		assert.Equal(t, pod.Name, event.InvolvedObject.Name)
		assert.Equal(t, pod.UID, event.InvolvedObject.UID)
		for _, part := range messageParts {
			assert.Contains(t, event.Message, part)
		}
	case <-time.After(time.Second):
		t.Fatal("expected pod event")
	}
}
