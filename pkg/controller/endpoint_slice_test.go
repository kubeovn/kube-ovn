package controller

import (
	"fmt"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestFindServiceKey(t *testing.T) {
	tests := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		expectedKey   string
	}{
		{
			name: "valid endpoint slice with service name",
			endpointSlice: &discoveryv1.EndpointSlice{
				Namespace: "default",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "test-service",
				},
			},
			expectedKey: "default/test-service",
		},
		{
			name: "endpoint slice with empty service name",
			endpointSlice: &discoveryv1.EndpointSlice{
				Namespace: "default",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "",
				},
			},
			expectedKey: "",
		},
		{
			name: "endpoint slice with no labels",
			endpointSlice: &discoveryv1.EndpointSlice{
				Namespace: "default",
			},
			expectedKey: "",
		},
		{
			name:          "nil endpoint slice",
			endpointSlice: nil,
			expectedKey:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findServiceKey(tt.endpointSlice)
			if result != tt.expectedKey {
				t.Errorf("findServiceKey() = %q, want %q", result, tt.expectedKey)
			}
		})
	}
}

func TestEnqueueEndpointSliceServicePriority(t *testing.T) {
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	tests := []struct {
		name     string
		service  *corev1.Service
		priority bool
	}{
		{
			name: "traffic distribution",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Type:                corev1.ServiceTypeClusterIP,
				TrafficDistribution: &trafficDistribution,
			}},
			priority: true,
		},
		{
			name: "regular service",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regularQueue := newTypedRateLimitingQueue[string]("test-endpoint-slice", nil)
			priorityQueue := newTypedRateLimitingQueue[string]("test-priority-endpoint-slice", nil)
			controller := &Controller{
				addOrUpdateEndpointSliceQueue: regularQueue,
				priorityEndpointSliceQueue:    priorityQueue,
			}

			controller.enqueueEndpointSliceService("default/service", tt.service)
			if tt.priority {
				require.Equal(t, 1, priorityQueue.Len())
				require.Equal(t, 0, regularQueue.Len())
			} else {
				require.Equal(t, 0, priorityQueue.Len())
				require.Equal(t, 1, regularQueue.Len())
			}
			regularQueue.ShutDown()
			priorityQueue.ShutDown()
		})
	}
}

func TestEndpointReady(t *testing.T) {
	trueVal := true
	falseVal := false
	tests := []struct {
		name     string
		endpoint discoveryv1.Endpoint
		want     bool
	}{
		{
			name: "Ready is nil (should be ready)",
			endpoint: discoveryv1.Endpoint{
				Conditions: discoveryv1.EndpointConditions{
					Ready: nil,
				},
			},
			want: true,
		},
		{
			name: "Ready is true",
			endpoint: discoveryv1.Endpoint{
				Conditions: discoveryv1.EndpointConditions{
					Ready: &trueVal,
				},
			},
			want: true,
		},
		{
			name: "Ready is false",
			endpoint: discoveryv1.Endpoint{
				Conditions: discoveryv1.EndpointConditions{
					Ready: &falseVal,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointReady(tt.endpoint)
			if got != tt.want {
				t.Errorf("endpointReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetHealthCheckVipHandlesConcurrentCreate(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	vip := &kubeovnv1.Vip{
		Name:   "ovn-default",
		Spec:   kubeovnv1.VipSpec{Subnet: "ovn-default"},
		Status: kubeovnv1.VipStatus{V4ip: "10.16.0.2"},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
	require.NoError(t, err)

	// Keep the informer cache stale to reproduce two workers observing NotFound
	// before one of them creates the shared health-check VIP.
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	got, err := ctrl.getHealthCheckVip("ovn-default", "10.96.0.10")
	require.NoError(t, err)
	require.Equal(t, "10.16.0.2", got)
}

func TestGetHealthCheckVipRefreshesStaleInformerStatus(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	vip := &kubeovnv1.Vip{
		Name:   "ovn-default",
		Spec:   kubeovnv1.VipSpec{Subnet: "ovn-default"},
		Status: kubeovnv1.VipStatus{V4ip: "10.16.0.2"},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
	require.NoError(t, err)

	// The informer can briefly contain the object before its status update.
	stale := vip.DeepCopy()
	stale.Status = kubeovnv1.VipStatus{}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(stale))
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)

	got, err := ctrl.getHealthCheckVip("ovn-default", "10.96.0.10")
	require.NoError(t, err)
	require.Equal(t, "10.16.0.2", got)
}

func TestGetHealthCheckVipWaitsForStatusAllocation(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	vip := &kubeovnv1.Vip{
		Name: "ovn-default",
		Spec: kubeovnv1.VipSpec{Subnet: "ovn-default"},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
	require.NoError(t, err)

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
	statusUpdateErr := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		updated := vip.DeepCopy()
		updated.Status.V4ip = "10.16.0.2"
		_, updateErr := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Update(t.Context(), updated, metav1.UpdateOptions{})
		statusUpdateErr <- updateErr
	}()

	got, err := ctrl.getHealthCheckVip("ovn-default", "10.96.0.10")
	require.NoError(t, err)
	require.Equal(t, "10.16.0.2", got)
	require.NoError(t, <-statusUpdateErr)
}

func TestTopologyBackendSubset(t *testing.T) {
	backends := []topologyBackend{
		{backend: "10.0.0.1:80", hints: &discoveryv1.EndpointHints{ForNodes: []discoveryv1.ForNode{{Name: "node-a"}}, ForZones: []discoveryv1.ForZone{{Name: "zone-a"}}}},
		{backend: "10.0.0.2:80", hints: &discoveryv1.EndpointHints{ForNodes: []discoveryv1.ForNode{{Name: "node-b"}}, ForZones: []discoveryv1.ForZone{{Name: "zone-a"}}}},
		{backend: "10.0.0.3:80", hints: &discoveryv1.EndpointHints{ForNodes: []discoveryv1.ForNode{{Name: "node-c"}}, ForZones: []discoveryv1.ForZone{{Name: "zone-b"}}}},
	}
	assert.ElementsMatch(t, []string{"10.0.0.1:80"}, topologyBackendSubset(backends, "node-a", "zone-a", corev1.ServiceTrafficDistributionPreferSameNode))
	assert.ElementsMatch(t, []string{"10.0.0.1:80", "10.0.0.2:80"}, topologyBackendSubset(backends, "node-x", "zone-a", corev1.ServiceTrafficDistributionPreferSameNode))
	assert.ElementsMatch(t, []string{"10.0.0.1:80", "10.0.0.2:80", "10.0.0.3:80"}, topologyBackendSubset(backends, "node-x", "zone-x", corev1.ServiceTrafficDistributionPreferSameNode))
	assert.ElementsMatch(t, []string{"10.0.0.1:80", "10.0.0.2:80"}, topologyBackendSubset(backends, "node-a", "zone-a", corev1.ServiceTrafficDistributionPreferSameZone))
	assert.ElementsMatch(t, []string{"10.0.0.1:80", "10.0.0.2:80"}, topologyBackendSubset(backends, "node-a", "zone-a", corev1.ServiceTrafficDistributionPreferClose))
	assert.ElementsMatch(t, []string{"10.0.0.1:80", "10.0.0.2:80", "10.0.0.3:80"}, topologyBackendSubset(backends, "node-a", "zone-a", "unknown"))

	missingNodeHint := append([]topologyBackend(nil), backends...)
	missingNodeHint[1].hints = &discoveryv1.EndpointHints{ForZones: []discoveryv1.ForZone{{Name: "zone-a"}}}
	assert.ElementsMatch(t, []string{"10.0.0.1:80", "10.0.0.2:80"}, topologyBackendSubset(missingNodeHint, "node-a", "zone-a", corev1.ServiceTrafficDistributionPreferSameNode))
}

func TestGetEndpointTargetLSPNameFromProvider(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		provider string
		expected string
	}{
		{
			name: "No provider, faulty pod",
			pod: &corev1.Pod{
				Name:      "",
				Namespace: "",
			},
			provider: "",
			expected: ".",
		},
		{
			name: "Endpoint empty with default provider and faulty pod",
			pod: &corev1.Pod{
				Name:      "",
				Namespace: "",
			},
			provider: util.OvnProvider,
			expected: ".",
		},
		{
			name: "Pod target with no provider",
			pod: &corev1.Pod{
				Name:      "some-pod-1d8fn",
				Namespace: "default",
			},
			provider: "",
			expected: "some-pod-1d8fn.default",
		},
		{
			name: "Pod target with default provider",
			pod: &corev1.Pod{
				Name:      "some-pod-1d8fn",
				Namespace: "default",
			},
			provider: util.OvnProvider,
			expected: "some-pod-1d8fn.default",
		},
		{
			name: "Pod target with custom provider",
			pod: &corev1.Pod{
				Name:      "some-pod-1d8fn",
				Namespace: "default",
			},
			provider: "custom.provider",
			expected: "some-pod-1d8fn.default.custom.provider",
		},
		{
			name: "VM target with no provider",
			pod: &corev1.Pod{
				Name:      "virt-launcher-some-vm-67jd3",
				Namespace: "default",
				Annotations: map[string]string{
					fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider): "some-vm",
				},
			},
			provider: "",
			expected: "some-vm.default",
		},
		{
			name: "VM target with default provider",
			pod: &corev1.Pod{
				Name:      "virt-launcher-some-vm-67jd3",
				Namespace: "default",
				Annotations: map[string]string{
					fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider): "some-vm",
				},
			},
			provider: util.OvnProvider,
			expected: "some-vm.default",
		},
		{
			name: "VM target with custom provider",
			pod: &corev1.Pod{
				Name:      "virt-launcher-some-vm-67jd3",
				Namespace: "default",
				Annotations: map[string]string{
					fmt.Sprintf(util.VMAnnotationTemplate, "custom.provider"): "some-vm",
				},
			},
			provider: "custom.provider",
			expected: "some-vm.default.custom.provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEndpointTargetLSPNameFromProvider(tt.pod, tt.provider)
			if result != tt.expected {
				t.Errorf("getEndpointTargetLSPName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetMatchingProviderForAddress(t *testing.T) {
	tests := []struct {
		name      string
		pod       *corev1.Pod
		providers []string
		address   string
		expected  string
	}{
		{
			name: "IP is on default provider and single stack",
			pod: &corev1.Pod{
				Annotations: map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "1.1.1.1",
				},
			},
			providers: []string{util.OvnProvider},
			address:   "1.1.1.1",
			expected:  util.OvnProvider,
		},
		{
			name: "IP is on default provider and dual stack",
			pod: &corev1.Pod{
				Annotations: map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "1.1.1.1,fd00::a",
				},
			},
			providers: []string{util.OvnProvider},
			address:   "fd00::a",
			expected:  util.OvnProvider,
		},
		{
			name: "IP is on custom provider and dual stack",
			pod: &corev1.Pod{
				Annotations: map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "custom.provider"): "1.1.1.1,fd00::a",
				},
			},
			providers: []string{"custom.provider"},
			address:   "fd00::a",
			expected:  "custom.provider",
		},
		{
			name: "IP is on custom provider with multiple other providers present on the pod",
			pod: &corev1.Pod{
				Annotations: map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):  "10.0.0.1,fd10:0:0::1",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "first.provider"):  "1.1.1.1,fd00::a",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "second.provider"): "2.2.2.2,fd00::b",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "third.provider"):  "3.3.3.3,fd00::c",
				},
			},
			providers: []string{util.OvnProvider, "first.provider", "second.provider", "third.provider"},
			address:   "fd00::b",
			expected:  "second.provider",
		},
		{
			name: "No provider is matching",
			pod: &corev1.Pod{
				Annotations: map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.0.0.1,fd10:0:0::1",
				},
			},
			providers: []string{util.OvnProvider},
			address:   "fd00::b",
			expected:  "",
		},
		{
			name: "No annotation is present",
			pod: &corev1.Pod{
				Annotations: nil,
			},
			providers: []string{},
			address:   "fd00::b",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMatchingProviderForAddress(tt.pod, tt.providers, tt.address)
			if result != tt.expected {
				t.Errorf("getMatchingProviderForAddress() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestServiceHasSelector(t *testing.T) {
	tests := []struct {
		name     string
		service  *corev1.Service
		expected bool
	}{
		{
			name: "Service has no selector",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: nil,
			}},
			expected: false,
		},
		{
			name: "Service has empty selectors",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{},
			}},
			expected: false,
		},
		{
			name: "Service has one selector",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"a": "b",
				},
			}},
			expected: true,
		},
		{
			name: "Service has multiple selectors",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"a": "b",
					"c": "d",
				},
			}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceHasSelector(tt.service)
			if result != tt.expected {
				t.Errorf("serviceHasSelector() = %t, want %t", result, tt.expected)
			}
		})
	}
}

func TestServiceHealthChecksDisabled(t *testing.T) {
	tests := []struct {
		name     string
		svc      *corev1.Service
		expected bool
	}{
		{
			name: "no annotation on the service",
			svc: &corev1.Service{
				Namespace: "default",
			},
			expected: false,
		},
		{
			name: "unrelated annotation on the service",
			svc: &corev1.Service{
				Namespace:   "default",
				Annotations: map[string]string{"key": "value"},
			},
			expected: false,
		},
		{
			name: "annotation to enable checks",
			svc: &corev1.Service{
				Namespace:   "default",
				Annotations: map[string]string{util.ServiceHealthCheck: "true"},
			},
			expected: false,
		},
		{
			name: "malformed annotation to enable checks (will be ignored)",
			svc: &corev1.Service{
				Namespace:   "default",
				Annotations: map[string]string{util.ServiceHealthCheck: "invalid"},
			},
			expected: false,
		},
		{
			name: "annotation to disable checks",
			svc: &corev1.Service{
				Namespace:   "default",
				Annotations: map[string]string{util.ServiceHealthCheck: "false"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceHealthChecksDisabled(tt.svc)
			if result != tt.expected {
				t.Errorf("findServiceKey() = %t, want %t", result, tt.expected)
			}
		})
	}
}

// TestReplaceEndpointAddressesWithSecondaryIPs tests the real controller method
func TestReplaceEndpointAddressesWithSecondaryIPs(t *testing.T) {
	tests := []struct {
		name               string
		endpointSlices     []*discoveryv1.EndpointSlice
		pods               []*corev1.Pod
		networkAttachments []*nadv1.NetworkAttachmentDefinition
		subnets            []*kubeovnv1.Subnet
		expectedChanges    map[string]string // map of original IP to expected new IP
		expectError        bool
		description        string
	}{
		{
			name: "Replace primary IP with secondary IP from network attachment",
			endpointSlices: []*discoveryv1.EndpointSlice{
				{
					Name:      "test-service-slice",
					Namespace: "default",
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses: []string{"10.244.0.5"}, // Primary IP
							TargetRef: &corev1.ObjectReference{
								Kind: util.KindPod,
								Name: "test-pod-1",
							},
						},
					},
				},
			},
			pods: []*corev1.Pod{
				{
					Name:      "test-pod-1",
					Namespace: "default",
					Annotations: map[string]string{
						// Network attachment annotation to indicate this pod uses net1
						nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
						// Kube-OVN annotations for net1 provider
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net1.default.ovn"): "net1-vpc",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					},
					Status: corev1.PodStatus{
						PodIP: "10.244.0.5",
					},
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
					},
				},
			},
			expectedChanges: map[string]string{
				"10.244.0.5": "192.168.1.10",
			},
			expectError: false,
			description: "Should replace primary IP with secondary IP from first provider",
		},
		{
			name: "Replace primary IP with secondary IP from network attachment with format <namespace>/<name>",
			endpointSlices: []*discoveryv1.EndpointSlice{
				{
					Name:      "test-service-slice",
					Namespace: "default",
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses: []string{"10.244.0.5"}, // Primary IP
							TargetRef: &corev1.ObjectReference{
								Kind: util.KindPod,
								Name: "test-pod-1",
							},
						},
					},
				},
			},
			pods: []*corev1.Pod{
				{
					Name:      "test-pod-1",
					Namespace: "default",
					Annotations: map[string]string{
						// Network attachment annotation to indicate this pod uses net1
						nadv1.NetworkAttachmentAnnot: "default/net1",
						// Kube-OVN annotations for net1 provider
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net1.default.ovn"): "net1-vpc",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					},
					Status: corev1.PodStatus{
						PodIP: "10.244.0.5",
					},
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
					},
				},
			},
			expectedChanges: map[string]string{
				"10.244.0.5": "192.168.1.10",
			},
			expectError: false,
			description: "Should replace primary IP with secondary IP from first provider",
		},
		{
			name: "Pod without network attachment - no changes",
			endpointSlices: []*discoveryv1.EndpointSlice{
				{
					Name:      "test-service-slice",
					Namespace: "default",
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses: []string{"10.244.0.5"},
							TargetRef: &corev1.ObjectReference{
								Kind: util.KindPod,
								Name: "test-pod-1",
							},
						},
					},
				},
			},
			pods: []*corev1.Pod{
				{
					Name:      "test-pod-1",
					Namespace: "default",
					// No network attachment annotations
					Annotations: map[string]string{
						// Only default provider annotations
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "default-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, util.OvnProvider): "ovn-cluster",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.244.0.5",
					},
					Status: corev1.PodStatus{
						PodIP: "10.244.0.5",
					},
				},
			},
			networkAttachments: []*nadv1.NetworkAttachmentDefinition{},
			subnets: []*kubeovnv1.Subnet{
				{
					Name: "default-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
					},
				},
			},
			expectedChanges: map[string]string{},
			expectError:     false,
			description:     "Should not change endpoints for pods without network attachments",
		},
		{
			name: "Pod with multiple network attachments",
			endpointSlices: []*discoveryv1.EndpointSlice{
				{
					Name:      "test-service-slice",
					Namespace: "default",
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses: []string{"10.244.0.5"},
							TargetRef: &corev1.ObjectReference{
								Kind: util.KindPod,
								Name: "test-pod-1",
							},
						},
					},
				},
			},
			pods: []*corev1.Pod{
				{
					Name:      "test-pod-1",
					Namespace: "default",
					Annotations: map[string]string{
						// Network attachment annotation to indicate this pod uses net1, net2
						nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}, {"name": "net2"}]`,
						// Kube-OVN annotations for net1 provider
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "net1-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net1.default.ovn"): "net1-vpc",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
						// Kube-OVN annotations for net2 provider
						fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net2.default.ovn"): "net2-subnet",
						fmt.Sprintf(util.LogicalRouterAnnotationTemplate, "net2.default.ovn"): "net2-vpc",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "net2.default.ovn"):     "192.168.2.10",
					},
					Status: corev1.PodStatus{
						PodIP: "10.244.0.5",
					},
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
				{
					Name:      "net2",
					Namespace: "default",
					Spec: nadv1.NetworkAttachmentDefinitionSpec{
						Config: `{
							"cniVersion": "0.3.1",
							"name": "net2",
							"type": "kube-ovn",
							"server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
							"provider": "net2.default.ovn"
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
					Name: "net2-subnet",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.2.0/24",
						Provider:  "net2.default.ovn",
					},
				},
				{
					Name: "ovn-default",
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
					},
				},
			},
			// First provider is net1.default.ovn. We expect changes for its IPs
			expectedChanges: map[string]string{
				"10.244.0.5": "192.168.1.10",
			},
			expectError: false,
			description: "Should replace primary IP with secondary IP from first provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create controller with proper setup
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				NetworkAttachments: tt.networkAttachments,
				Subnets:            tt.subnets,
				Pods:               tt.pods,
			})
			require.NoError(t, err, "Failed to create fake controller")

			controller := fakeController.fakeController

			// Store original addresses for comparison
			originalAddresses := make(map[string][]string)
			for i, slice := range tt.endpointSlices {
				for j, endpoint := range slice.Endpoints {
					key := fmt.Sprintf("%d-%d", i, j)
					originalAddresses[key] = make([]string, len(endpoint.Addresses))
					copy(originalAddresses[key], endpoint.Addresses)
				}
			}

			// Call the real controller method
			err = controller.replaceEndpointAddressesWithSecondaryIPs(tt.endpointSlices, tt.pods)

			// Check for errors
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
				return
			}
			require.NoError(t, err, "Unexpected error from replaceEndpointAddressesWithSecondaryIPs")

			// Verify the changes
			changesFound := make(map[string]string)
			for i, slice := range tt.endpointSlices {
				for j, endpoint := range slice.Endpoints {
					key := fmt.Sprintf("%d-%d", i, j)
					originalAddrs := originalAddresses[key]

					for k, newAddr := range endpoint.Addresses {
						if k < len(originalAddrs) && originalAddrs[k] != newAddr {
							changesFound[originalAddrs[k]] = newAddr
						}
					}
				}
			}

			// Compare expected changes with actual changes
			assert.Equal(t, len(tt.expectedChanges), len(changesFound),
				"Number of changes mismatch. Expected: %v, Got: %v", tt.expectedChanges, changesFound)

			for originalIP, expectedNewIP := range tt.expectedChanges {
				actualNewIP, exists := changesFound[originalIP]
				assert.True(t, exists, "Expected change from %s to %s, but no change found", originalIP, expectedNewIP)
				if exists {
					assert.Equal(t, expectedNewIP, actualNewIP, "Expected change from %s to %s, but got %s", originalIP, expectedNewIP, actualNewIP)
				}
			}
		})
	}
}

func TestEndpointSliceServiceProfileDistributedLocalDoesNotPreferLocalBackend(t *testing.T) {
	local := corev1.ServiceInternalTrafficPolicyLocal
	svc := &corev1.Service{
		Namespace: "default",
		Name:      "web",
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeClusterIP,
			ClusterIP:             "10.96.0.10",
			InternalTrafficPolicy: &local,
		},
	}

	profile, err := (&Controller{}).endpointSliceServiceProfile(svc)
	require.NoError(t, err)
	require.True(t, profile.distributedLocal)
	require.True(t, profile.ignoreHealthCheck)
}

func TestEndpointSliceServiceProfileLoadBalancerETPLocalKeepsClusterIPInternal(t *testing.T) {
	svc := &corev1.Service{
		Namespace: "default",
		Name:      "web",
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			ClusterIP:             "10.96.0.10",
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "172.19.0.100"}}}},
	}

	profile, err := (&Controller{}).endpointSliceServiceProfile(svc)
	require.NoError(t, err)
	require.True(t, profile.ignoreHealthCheck)
	require.Equal(t, serviceLBInternalTraffic, profile.trafficClasses["10.96.0.10"])
	require.Equal(t, serviceLBExternalTraffic, profile.trafficClasses["172.19.0.100"])
}

func TestEndpointSliceServiceProfileClusterIPDoesNotEnableHealthCheck(t *testing.T) {
	svc := &corev1.Service{
		Namespace: "default",
		Name:      "web",
		Spec: corev1.ServiceSpec{
			Type:       corev1.ServiceTypeClusterIP,
			ClusterIP:  "10.96.0.10",
			ClusterIPs: []string{"10.96.0.10", "fd00::10"},
		},
	}

	profile, err := (&Controller{}).endpointSliceServiceProfile(svc)
	require.NoError(t, err)
	require.True(t, profile.ignoreHealthCheck)
	require.Equal(t, []string{"10.96.0.10", "fd00::10"}, profile.lbVips)
}

func TestEndpointSliceServiceProfileSwitchLBRuleEnablesHealthCheck(t *testing.T) {
	svc := &corev1.Service{
		Namespace: "default",
		Name:      "web",
		Annotations: map[string]string{
			util.SwitchLBRuleVipsAnnotation: "10.0.0.20",
		},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.10"},
	}

	profile, err := (&Controller{}).endpointSliceServiceProfile(svc)
	require.NoError(t, err)
	require.False(t, profile.ignoreHealthCheck)
	require.Equal(t, []string{"10.0.0.20"}, profile.lbVips)
}

func TestEndpointSliceServiceProfileRouterLBRuleHealthCheck(t *testing.T) {
	t.Parallel()

	t.Run("disabled by default", func(t *testing.T) {
		t.Parallel()
		svc := &corev1.Service{
			Namespace: "default",
			Name:      "rlr-web",
			Annotations: map[string]string{
				util.RouterLBRuleVipsAnnotation: "192.0.2.10",
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: corev1.ClusterIPNone},
		}
		profile, err := (&Controller{}).endpointSliceServiceProfile(svc)
		require.NoError(t, err)
		require.True(t, profile.ignoreHealthCheck)
		require.Equal(t, []string{"192.0.2.10"}, profile.lbVips)
	})

	t.Run("enabled by annotation", func(t *testing.T) {
		t.Parallel()
		svc := &corev1.Service{
			Namespace: "default",
			Name:      "rlr-web",
			Annotations: map[string]string{
				util.RouterLBRuleVipsAnnotation: "192.0.2.10",
				util.ServiceHealthCheck:         "true",
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: corev1.ClusterIPNone},
		}
		profile, err := (&Controller{}).endpointSliceServiceProfile(svc)
		require.NoError(t, err)
		require.False(t, profile.ignoreHealthCheck)
		require.Equal(t, []string{"192.0.2.10"}, profile.lbVips)
	})
}

func TestFilterIPPortMappingByVIP(t *testing.T) {
	mapping := IPPortMapping{
		"10.0.0.2":  "pod-a",
		"[fd00::2]": "pod-b",
		"fd00::3":   "pod-c",
	}

	got := filterIPPortMappingByVIP(mapping, "10.96.0.10")
	require.Equal(t, IPPortMapping{"10.0.0.2": "pod-a"}, got)

	got = filterIPPortMappingByVIP(mapping, "fd00::10")
	require.Equal(t, IPPortMapping{"[fd00::2]": "pod-b", "fd00::3": "pod-c"}, got)
}

func TestDeleteSubnetHealthCheckVip(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	vip := &kubeovnv1.Vip{
		Name: "subnet-health",
		Spec: kubeovnv1.VipSpec{Subnet: "subnet-health"},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
	require.NoError(t, err)

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)

	subnet := &kubeovnv1.Subnet{Name: "subnet-health"}
	require.NoError(t, ctrl.deleteSubnetHealthCheckVip(subnet))

	_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.Error(t, err)

	unrelated := &kubeovnv1.Vip{
		Name: "other-subnet",
		Spec: kubeovnv1.VipSpec{Subnet: "keep-me"},
	}
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), unrelated, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, indexer.Add(unrelated))
	require.NoError(t, ctrl.deleteSubnetHealthCheckVip(&kubeovnv1.Subnet{Name: "other-subnet"}))
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), unrelated.Name, metav1.GetOptions{})
	require.NoError(t, err)
}

func TestDeleteSubnetHealthCheckVipWithoutLister(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	vip := &kubeovnv1.Vip{
		Name: "subnet-api-only",
		Spec: kubeovnv1.VipSpec{Subnet: "subnet-api-only"},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
	require.NoError(t, err)
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	require.NoError(t, ctrl.deleteSubnetHealthCheckVip(&kubeovnv1.Subnet{Name: "subnet-api-only"}))
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.Error(t, err)
}

func TestServiceNeedsPriorityEndpointReconcileLoadBalancerETPLocal(t *testing.T) {
	svc := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		},
	}
	require.True(t, serviceNeedsPriorityEndpointReconcile(svc))

	svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
	require.False(t, serviceNeedsPriorityEndpointReconcile(svc))
}
