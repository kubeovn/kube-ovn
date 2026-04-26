package util

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestObjectMatchesLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		object   metav1.Object
		selector *metav1.LabelSelector
		expected bool
	}{
		{
			name: "Match Labels",
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			selector: metav1.SetAsLabelSelector(labels.Set{"app": "nginx"}),
			expected: true,
		},
		{
			name: "No Match Labels",
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "apache"},
				},
			},
			selector: metav1.SetAsLabelSelector(labels.Set{"app": "nginx"}),
			expected: false,
		},
		{
			name: "Invalid Selector",
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			selector: metav1.SetAsLabelSelector(labels.Set{"app": "nginx,"}),
			expected: false,
		},
		{
			name: "Nil Selector",
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			expected: false,
		},
		{
			name: "No Labels",
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			selector: metav1.SetAsLabelSelector(labels.Set{"app": "nginx"}),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ObjectMatchesLabelSelector(tt.object, tt.selector)
			if result != tt.expected {
				t.Errorf("ObjectMatchesLabelSelector(%q, %v) = %v, want %v", tt.selector, tt.object, result, tt.expected)
			}
		})
	}
}

func TestDialTCP(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		timeout  time.Duration
		verbose  bool
		expected error
	}{
		{"Valid HTTP Host", "http://localhost:8080", 1 * time.Second, false, nil},
		{"Valid HTTP Host", "http://localhost:8080", 1 * time.Second, true, nil},
		{"Valid HTTPS Host", "https://localhost:8443", 1 * time.Second, false, nil},
		{"Valid TCP Host", "tcp://localhost:8081", 1 * time.Second, false, nil},
		{"Invalid Host", "https://localhost%:8443", 1 * time.Second, false, errors.New(`invalid URL escape`)},
		{"Unsupported Scheme", "ftp://localhost:8080", 1 * time.Second, false, errors.New(`unsupported scheme "ftp"`)},
		{"Timeout", "http://localhost:23456", 1 * time.Second, false, errors.New(`timed out dialing host`)},
	}

	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	httpServer.StartTLS()
	defer httpServer.Close()

	tcpListener, err := net.Listen("tcp", "localhost:8081")
	if err != nil {
		t.Fatalf("failed to start tcp server: %v", err)
	}
	defer tcpListener.Close()

	go func() {
		for {
			conn, err := tcpListener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	// Update tests with dynamic URLs
	for i, tc := range tests {
		if tc.host == "http://localhost:8080" || tc.host == "https://localhost:8443" {
			tests[i].host = httpServer.URL
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DialTCP(tt.host, tt.timeout, tt.verbose)

			// Dynamically generate expected message for timeout
			if tt.expected != nil && strings.Contains(tt.expected.Error(), "timed out dialing host") {
				tt.expected = errors.New(`timed out dialing host "` + tt.host + `"`)
			}

			if (err != nil && tt.expected == nil) || (err == nil && tt.expected != nil) || (err != nil && tt.expected != nil && !strings.Contains(err.Error(), tt.expected.Error())) {
				t.Errorf("DialTCP(%q) got %v, want %v", tt.host, err, tt.expected)
			}

			if tt.verbose {
				klog.Flush()
			}
		})
	}
}

func TestDialAPIServer(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() (string, func())
		expected error
	}{
		{
			name: "Successful Dial",
			setup: func() (string, func()) {
				server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
				return server.URL, server.Close
			},
			expected: nil,
		},
		{
			name: "Successful TLS Dial",
			setup: func() (string, func()) {
				server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
				return server.URL, server.Close
			},
			expected: nil,
		},
		{
			name: "Failed Dial",
			setup: func() (string, func()) {
				return "http://localhost:12345", func() {}
			},
			expected: errors.New("timed out dialing apiserver"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, cleanup := tt.setup()
			defer cleanup()

			err := DialAPIServer(host, 1*time.Second, 1)

			if tt.expected == nil && err != nil {
				t.Errorf("expected no error, got %v", err)
			} else if tt.expected != nil {
				if err == nil {
					t.Errorf("expected error containing %v, got nil", tt.expected)
				} else if !strings.Contains(err.Error(), tt.expected.Error()) {
					t.Errorf("expected error containing %v, got %v", tt.expected.Error(), err.Error())
				}
			}
		})
	}
}

func TestGetNodeInternalIP(t *testing.T) {
	tests := []struct {
		name string
		node corev1.Node
		exp4 string
		exp6 string
	}{
		{
			name: "correct",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{{
						Type:    "InternalIP",
						Address: "192.168.0.2",
					}, {
						Type:    "ExternalIP",
						Address: "192.188.0.4",
					}, {
						Type:    "InternalIP",
						Address: "ffff:ffff:ffff:ffff:ffff::23",
					}},
				},
			},
			exp4: "192.168.0.2",
			exp6: "ffff:ffff:ffff:ffff:ffff::23",
		},
		{
			name: "correctWithDiff",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{{
						Type:    "InternalIP",
						Address: "ffff:ffff:ffff:ffff:ffff::23",
					}, {
						Type:    "ExternalIP",
						Address: "192.188.0.4",
					}, {
						Type:    "InternalIP",
						Address: "192.188.0.43",
					}},
				},
			},
			exp4: "192.188.0.43",
			exp6: "ffff:ffff:ffff:ffff:ffff::23",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret4, ret6 := GetNodeInternalIP(tt.node); ret4 != tt.exp4 || ret6 != tt.exp6 {
				t.Errorf("got %v, %v, want %v, %v", ret4, ret6, tt.exp4, tt.exp6)
			}
		})
	}
}

func TestPodIPs(t *testing.T) {
	tests := []struct {
		name string
		pod  corev1.Pod
		exp  []string
	}{
		{
			name: "pod_with_one_pod_ipv4_ip",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					PodIPs: []corev1.PodIP{{IP: "192.168.1.100"}},
					PodIP:  "192.168.1.100",
				},
			},
			exp: []string{"192.168.1.100"},
		},
		{
			name: "pod_with_one_pod_dual_ip",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					PodIPs: []corev1.PodIP{{IP: "192.168.1.100"}, {IP: "fd00:10:16::8"}},
					PodIP:  "192.168.1.100",
				},
			},
			exp: []string{"192.168.1.100", "fd00:10:16::8"},
		},
		{
			name: "pod_with_no_pod_ip",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					PodIPs: []corev1.PodIP{},
					PodIP:  "",
				},
			},
			exp: []string{},
		},
		{
			name: "pod_with_podip",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					PodIPs: []corev1.PodIP{},
					PodIP:  "192.168.1.100",
				},
			},
			exp: []string{"192.168.1.100"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := PodIPs(tt.pod); len(ret) != len(tt.exp) {
				t.Errorf("got %v, want %v", ret, tt.exp)
			}
		})
	}
}

func TestServiceClusterIPs(t *testing.T) {
	tests := []struct {
		name string
		svc  corev1.Service
		exp  []string
	}{
		{
			name: "service_with_one_cluster_ip",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1"},
				},
			},
			exp: []string{"10.96.0.1"},
		},
		{
			name: "service_with_two_cluster_ip",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1", "fd00:10:16::1"},
				},
			},
			exp: []string{"10.96.0.1", "fd00:10:16::1"},
		},
		{
			name: "service_with_no_cluster_ip",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP:  "",
					ClusterIPs: []string{},
				},
			},
			exp: []string{},
		},
		{
			name: "service_with_no_clusterips",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{},
				},
			},
			exp: []string{"10.96.0.1"},
		},
		{
			name: "service_with_invalid_cluster_ip",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP:  "",
					ClusterIPs: []string{"10.96.0.1", "invalid ip"},
				},
			},
			exp: []string{"10.96.0.1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := ServiceClusterIPs(tt.svc); len(ret) != len(tt.exp) {
				t.Errorf("got %v, want %v", ret, tt.exp)
			}
		})
	}
}

func TestLabelSelectorNotEquals(t *testing.T) {
	selector, err := LabelSelectorNotEquals("key", "value")
	require.NoError(t, err)
	require.Equal(t, "key!=value", selector.String())
	// Test error case
	selector, err = LabelSelectorNotEquals("", "")
	require.Error(t, err)
	require.Nil(t, selector)
}

func TestLabelSelectorNotEmpty(t *testing.T) {
	selector, err := LabelSelectorNotEmpty("key")
	require.NoError(t, err)
	require.Equal(t, "key!=", selector.String())
	// Test error case
	selector, err = LabelSelectorNotEmpty("")
	require.Error(t, err)
	require.Nil(t, selector)
}

func TestGetTruncatedUID(t *testing.T) {
	uid := "12345678-1234-1234-1234-123456789012"
	require.Equal(t, "123456789012", GetTruncatedUID(uid))
}

func TestSetOwnerReference(t *testing.T) {
	tests := []struct {
		name    string
		owner   metav1.Object
		object  metav1.Object
		wantErr bool
	}{
		{
			name: "base",
			owner: &kubeovnv1.VpcEgressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("veg-%05d", rand.IntN(10000)),
					UID:  uuid.NewUUID(),
				},
			},
			object: &corev1.Pod{},
		},
		{
			name: "not registered",
			owner: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("veg-%05d", rand.IntN(10000)),
					UID:  uuid.NewUUID(),
				},
			},
			object:  &corev1.Pod{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetOwnerReference(tt.owner, tt.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetOwnerReference() error = %#v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			refer := tt.object.GetOwnerReferences()
			require.Len(t, refer, 1)
			require.Equal(t, tt.owner.GetName(), refer[0].Name)
			require.Equal(t, tt.owner.GetUID(), refer[0].UID)
		})
	}
}

func TestPodAttachmentIPs(t *testing.T) {
	tests := []struct {
		name    string
		pod     *corev1.Pod
		network string
		wantErr bool
		ips     []string
	}{
		{
			name: "ipv4",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						nadv1.NetworkStatusAnnot: `[{"name": "default/ipv4", "ips": ["1.1.1.1"]}]`,
					},
				},
			},
			network: "default/ipv4",
			ips:     []string{"1.1.1.1"},
		},
		{
			name: "ipv6",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						nadv1.NetworkStatusAnnot: `[{"name": "default/ipv6", "ips": ["fd00::1"]}]`,
					},
				},
			},
			network: "default/ipv6",
			ips:     []string{"fd00::1"},
		},
		{
			name: "dual-stack",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						nadv1.NetworkStatusAnnot: `[{"name": "default/dual", "ips": ["1.1.1.1", "fd00::1"]}]`,
					},
				},
			},
			network: "default/dual",
			ips:     []string{"1.1.1.1", "fd00::1"},
		},
		{
			name:    "nil pod",
			pod:     nil,
			wantErr: true,
		},
		{
			name:    "no network status annotation",
			pod:     &corev1.Pod{},
			wantErr: true,
		},
		{
			name: "unexpected network status annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						nadv1.NetworkStatusAnnot: `foo_bar`,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty network name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						nadv1.NetworkStatusAnnot: `[{"name": "default/xxx", "ips": ["1.1.1.1", "fd00::1"]}]`,
					},
				},
			},
			network: "",
			wantErr: true,
		},
		{
			name: "network status not found",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						nadv1.NetworkStatusAnnot: `[{"name": "default/xyz", "ips": ["1.1.1.1", "fd00::1"]}]`,
					},
				},
			},
			network: "default/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips, err := PodAttachmentIPs(tt.pod, tt.network)
			if (err != nil) != tt.wantErr {
				t.Errorf("PodAttachmentIPs() error = %#v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			require.ElementsMatch(t, tt.ips, ips)
		})
	}
}

func TestDeploymentIsReady(t *testing.T) {
	tests := []struct {
		name   string
		deploy *appsv1.Deployment
		ready  bool
	}{
		{
			name: "ready",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           1,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
				},
			},
			ready: true,
		},
		{
			name: "generation mismatch",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 1,
				},
			},
			ready: false,
		},
		{
			name: "condition Processing",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           1,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type: appsv1.DeploymentProgressing,
						},
					},
				},
			},
			ready: true,
		},
		{
			name: "ProgressDeadlineExceeded",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:   appsv1.DeploymentProgressing,
							Reason: "ProgressDeadlineExceeded",
						},
					},
				},
			},
			ready: false,
		},
		{
			name: "updated replicas less than desired replicas",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](2),
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           2,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
				},
			},
			ready: false,
		},
		{
			name: "updated replicas less than current replicas",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           2,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
				},
			},
			ready: false,
		},
		{
			name: "available replicas less than updated replicas",
			deploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](2),
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           2,
					UpdatedReplicas:    2,
					AvailableReplicas:  1,
				},
			},
			ready: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready := DeploymentIsReady(tt.deploy)
			require.Equal(t, tt.ready, ready)
		})
	}
}

func TestStatefulSetIsReady(t *testing.T) {
	tests := []struct {
		name string
		sts  *appsv1.StatefulSet
		want bool
	}{
		{
			name: "ready",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 2,
					Replicas:           3,
					ReadyReplicas:      3,
					CurrentReplicas:    3,
					UpdatedReplicas:    3,
				},
			},
			want: true,
		},
		{
			name: "generation mismatch",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 1,
				},
			},
			want: false,
		},
		{
			name: "ready replicas less than desired replicas",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 2,
					Replicas:           3,
					ReadyReplicas:      2,
					CurrentReplicas:    2,
					UpdatedReplicas:    2,
				},
			},
			want: false,
		},
		{
			name: "current replicas greater than ready replicas",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 2,
					Replicas:           3,
					ReadyReplicas:      2,
					CurrentReplicas:    3,
					UpdatedReplicas:    2,
				},
			},
			want: false,
		},
		{
			name: "updated replicas less than ready replicas",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 2,
					Replicas:           3,
					ReadyReplicas:      3,
					CurrentReplicas:    3,
					UpdatedReplicas:    2,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StatefulSetIsReady(tt.sts)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestInjectedServiceVariables(t *testing.T) {
	tests := []struct {
		name         string
		serviceName  string
		injectedEnv  map[string]string
		expectedHost string
		expectedPort string
	}{
		{
			name:        "simple service name",
			serviceName: "foo",
			injectedEnv: map[string]string{
				"FOO_SERVICE_HOST": "1.1.1.1",
				"FOO_SERVICE_PORT": "8080",
			},
			expectedHost: "1.1.1.1",
			expectedPort: "8080",
		},
		{
			name:        "service name with dashes",
			serviceName: "example-service-name",
			injectedEnv: map[string]string{
				"EXAMPLE_SERVICE_NAME_SERVICE_HOST": "::1",
			},
			expectedHost: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.injectedEnv {
				t.Setenv(k, v)
			}
			hostVar, portVar := InjectedServiceVariables(tt.serviceName)
			if hostVar != tt.expectedHost {
				t.Errorf("InjectedServiceVariables(%q) host = %q, want %q", tt.serviceName, hostVar, tt.expectedHost)
			}
			if portVar != tt.expectedPort {
				t.Errorf("InjectedServiceVariables(%q) port = %q, want %q", tt.serviceName, portVar, tt.expectedPort)
			}
		})
	}
}

func TestObjectKind(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		expected string
	}{
		{
			name:     "Pod object",
			result:   ObjectKind[*corev1.Pod](),
			expected: "Pod",
		},
		{
			name:     "Service object",
			result:   ObjectKind[*corev1.Service](),
			expected: "Service",
		},
		{
			name:     "DaemonSet object",
			result:   ObjectKind[*appsv1.DaemonSet](),
			expected: "DaemonSet",
		},
		{
			name:     "Custom Resource object",
			result:   ObjectKind[*kubeovnv1.Subnet](),
			expected: "Subnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result != tt.expected {
				t.Errorf("ObjectKind() = %q, want %q", tt.result, tt.expected)
			}
		})
	}
}

func TestTrimManagedFields(t *testing.T) {
	tests := []struct {
		name    string
		arg     any
		wantErr bool
	}{{
		name: "trim managed fields from object",
		arg: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod",
				ManagedFields: []metav1.ManagedFieldsEntry{{
					Manager:   "controller",
					Operation: metav1.ManagedFieldsOperationApply,
				}},
			},
		},
	}, {
		name: "object without managed fields",
		arg: &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-subnet-no-managed-fields",
			},
		},
	}, {
		name:    "non-object input",
		arg:     "this is a string, not an object",
		wantErr: true,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := TrimManagedFields(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Errorf("TrimManagedFields() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				// check whether managed fields are trimmed
				accessor, err := meta.Accessor(ret)
				require.NoError(t, err)
				require.Empty(t, accessor.GetManagedFields())
			}
		})
	}
}

func fullyPopulatedPod() *corev1.Pod {
	alwaysRestart := corev1.ContainerRestartPolicyAlways
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "demo",
			Namespace:   "default",
			Labels:      map[string]string{"app": "demo"},
			Annotations: map[string]string{"foo": "bar"},
			Finalizers:  []string{"kubeovn.io/controller"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       "demo-sts",
				UID:        "uid-1",
			}},
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
		},
		Spec: corev1.PodSpec{
			NodeName:                     "node-1",
			HostNetwork:                  false,
			RestartPolicy:                corev1.RestartPolicyAlways,
			ServiceAccountName:           "demo-sa",
			SchedulerName:                "default-scheduler",
			PriorityClassName:            "system-cluster-critical",
			Priority:                     ptr.To[int32](2000000000),
			Hostname:                     "demo",
			Subdomain:                    "demo-svc",
			AutomountServiceAccountToken: new(true),
			SecurityContext:              &corev1.PodSecurityContext{RunAsUser: ptr.To[int64](1000)},
			NodeSelector:                 map[string]string{"kubernetes.io/os": "linux"},
			Tolerations: []corev1.Toleration{
				{Key: "node-role.kubernetes.io/control-plane", Operator: corev1.TolerationOpExists},
			},
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			InitContainers: []corev1.Container{{
				Name:            "sidecar",
				Image:           "busybox:1.36",
				Command:         []string{"sh", "-c", "echo hi"},
				Args:            []string{"a", "b"},
				Env:             []corev1.EnvVar{{Name: "X", Value: "1"}},
				VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
				LivenessProbe:   &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8080)}}},
				SecurityContext: &corev1.SecurityContext{RunAsNonRoot: new(true)},
				RestartPolicy:   &alwaysRestart,
				Ports:           []corev1.ContainerPort{{Name: "sidecar-http", ContainerPort: 9100}},
			}},
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           "nginx:1.27",
				Command:         []string{"nginx"},
				Args:            []string{"-g", "daemon off;"},
				WorkingDir:      "/",
				Env:             []corev1.EnvVar{{Name: "FOO", Value: "BAR"}},
				EnvFrom:         []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}}},
				VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/srv"}},
				LivenessProbe:   &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt(80)}}},
				ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt(80)}}},
				Lifecycle:       &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"sleep", "1"}}}},
				SecurityContext: &corev1.SecurityContext{Privileged: new(false)},
				ImagePullPolicy: corev1.PullIfNotPresent,
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
				},
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 80}},
			}},
		},
		Status: corev1.PodStatus{
			Phase:  corev1.PodRunning,
			HostIP: "10.0.0.1",
			PodIP:  "10.244.0.5",
			PodIPs: []corev1.PodIP{{IP: "10.244.0.5"}},
			Reason: "",
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "app",
				Image:        "nginx:1.27",
				ImageID:      "sha256:abc",
				ContainerID:  "containerd://abc",
				RestartCount: 3,
				Ready:        true,
				Started:      new(true),
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				}},
			}},
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:         "sidecar",
				RestartCount: 1,
			}},
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodReady,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				LastProbeTime:      metav1.Now(),
				Reason:             "Ready",
				Message:            "all good",
			}},
			Message:           "noisy",
			NominatedNodeName: "node-1",
			QOSClass:          corev1.PodQOSBurstable,
		},
	}
}

func TestTrimPodForController(t *testing.T) {
	pod := fullyPopulatedPod()

	ret, err := TrimPodForController(pod)
	require.NoError(t, err)
	trimmed, ok := ret.(*corev1.Pod)
	require.True(t, ok)

	// preserved metadata and spec fields required by the controller
	require.Equal(t, "demo", trimmed.Name)
	require.Equal(t, map[string]string{"app": "demo"}, trimmed.Labels)
	require.Equal(t, map[string]string{"foo": "bar"}, trimmed.Annotations)
	require.Len(t, trimmed.OwnerReferences, 1)
	require.Equal(t, "StatefulSet", trimmed.OwnerReferences[0].Kind)
	require.Equal(t, []string{"kubeovn.io/controller"}, trimmed.Finalizers)
	require.Nil(t, trimmed.ManagedFields)
	require.Equal(t, "node-1", trimmed.Spec.NodeName)
	require.Equal(t, corev1.RestartPolicyAlways, trimmed.Spec.RestartPolicy)

	// container name and ports preserved (NamedPort lookup)
	require.Len(t, trimmed.Spec.Containers, 1)
	require.Equal(t, "app", trimmed.Spec.Containers[0].Name)
	require.Equal(t, "http", trimmed.Spec.Containers[0].Ports[0].Name)
	require.EqualValues(t, 80, trimmed.Spec.Containers[0].Ports[0].ContainerPort)

	// init-container RestartPolicy and ports preserved (restartable sidecar)
	require.Len(t, trimmed.Spec.InitContainers, 1)
	require.NotNil(t, trimmed.Spec.InitContainers[0].RestartPolicy)
	require.Equal(t, corev1.ContainerRestartPolicyAlways, *trimmed.Spec.InitContainers[0].RestartPolicy)
	require.Equal(t, "sidecar-http", trimmed.Spec.InitContainers[0].Ports[0].Name)

	// status fields required for pod alive / vpc-nat-gw restart checks
	require.Equal(t, corev1.PodRunning, trimmed.Status.Phase)
	require.Equal(t, "10.244.0.5", trimmed.Status.PodIP)
	require.Equal(t, []corev1.PodIP{{IP: "10.244.0.5"}}, trimmed.Status.PodIPs)
	require.Equal(t, "10.0.0.1", trimmed.Status.HostIP)
	require.Len(t, trimmed.Status.ContainerStatuses, 1)
	require.Equal(t, "app", trimmed.Status.ContainerStatuses[0].Name)
	require.EqualValues(t, 3, trimmed.Status.ContainerStatuses[0].RestartCount)

	// condition type/status/lastTransitionTime preserved
	require.Len(t, trimmed.Status.Conditions, 1)
	require.Equal(t, corev1.PodReady, trimmed.Status.Conditions[0].Type)
	require.Equal(t, corev1.ConditionTrue, trimmed.Status.Conditions[0].Status)
	require.False(t, trimmed.Status.Conditions[0].LastTransitionTime.IsZero())

	// trimmed: spec volumes / tolerations / affinity / node selector / scheduler name
	require.Nil(t, trimmed.Spec.Volumes)
	require.Nil(t, trimmed.Spec.Tolerations)
	require.Nil(t, trimmed.Spec.NodeSelector)
	require.Empty(t, trimmed.Spec.SchedulerName)
	require.Empty(t, trimmed.Spec.PriorityClassName)
	require.Nil(t, trimmed.Spec.Priority)
	require.Empty(t, trimmed.Spec.ServiceAccountName)
	require.Empty(t, trimmed.Spec.Hostname)
	require.Empty(t, trimmed.Spec.Subdomain)
	require.Nil(t, trimmed.Spec.SecurityContext)
	require.Nil(t, trimmed.Spec.AutomountServiceAccountToken)

	// trimmed: container command/args/env/volumeMounts/probes/securityContext
	c0 := trimmed.Spec.Containers[0]
	require.Empty(t, c0.Image)
	require.Nil(t, c0.Command)
	require.Nil(t, c0.Args)
	require.Empty(t, c0.WorkingDir)
	require.Nil(t, c0.Env)
	require.Nil(t, c0.EnvFrom)
	require.Nil(t, c0.VolumeMounts)
	require.Nil(t, c0.LivenessProbe)
	require.Nil(t, c0.ReadinessProbe)
	require.Nil(t, c0.Lifecycle)
	require.Nil(t, c0.SecurityContext)
	require.Empty(t, c0.ImagePullPolicy)
	require.Equal(t, corev1.ResourceRequirements{}, c0.Resources)

	// trimmed: init container fields except preserved ones
	ic0 := trimmed.Spec.InitContainers[0]
	require.Empty(t, ic0.Image)
	require.Nil(t, ic0.Command)
	require.Nil(t, ic0.Env)
	require.Nil(t, ic0.VolumeMounts)
	require.Nil(t, ic0.LivenessProbe)
	require.Nil(t, ic0.SecurityContext)

	// container status: State preserved (vpc-nat-gw redo path reads
	// State.Running.StartedAt); other heavy fields trimmed.
	cs0 := trimmed.Status.ContainerStatuses[0]
	require.NotNil(t, cs0.State.Running)
	require.False(t, cs0.State.Running.StartedAt.IsZero())
	require.Equal(t, corev1.ContainerState{}, cs0.LastTerminationState)
	require.False(t, cs0.Ready)
	require.Empty(t, cs0.Image)
	require.Empty(t, cs0.ImageID)
	require.Empty(t, cs0.ContainerID)
	require.Nil(t, cs0.Started)

	// trimmed: init container statuses / ephemeral / QOS / messages
	require.Nil(t, trimmed.Status.InitContainerStatuses)
	require.Empty(t, trimmed.Status.QOSClass)
	require.Empty(t, trimmed.Status.Message)
	require.Empty(t, trimmed.Status.NominatedNodeName)

	// trimmed: condition probe time / reason / message
	require.True(t, trimmed.Status.Conditions[0].LastProbeTime.IsZero())
	require.Empty(t, trimmed.Status.Conditions[0].Reason)
	require.Empty(t, trimmed.Status.Conditions[0].Message)
}

// BenchmarkPodInformerTrim estimates the per-pod in-memory retained size
// after each transform by building N typical pods, running the transform,
// forcing GC, and dividing HeapAlloc delta by N.
func BenchmarkPodInformerTrim(b *testing.B) {
	b.Run("TrimManagedFields", func(b *testing.B) {
		reportRetainedBytesPerPod(b, TrimManagedFields)
	})
	b.Run("TrimPodForController", func(b *testing.B) {
		reportRetainedBytesPerPod(b, TrimPodForController)
	})
}

func reportRetainedBytesPerPod(b *testing.B, transform func(any) (any, error)) {
	const N = 1000
	for b.Loop() {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		pods := make([]*corev1.Pod, N)
		for i := range N {
			p := fullyPopulatedPod()
			// Make names unique so strings actually allocate rather than getting interned.
			p.Name = fmt.Sprintf("pod-%05d", i)
			p.Namespace = fmt.Sprintf("ns-%03d", i%100)
			p.UID = uuid.NewUUID()
			ret, err := transform(p)
			if err != nil {
				b.Fatal(err)
			}
			pods[i] = ret.(*corev1.Pod)
		}

		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		// HeapAlloc is unsigned; clamp so a shrinking heap between snapshots
		// does not wrap around to a huge bogus retained size.
		retained := max(int64(after.HeapAlloc)-int64(before.HeapAlloc), 0)
		b.ReportMetric(float64(retained)/float64(N), "bytes/pod")
		runtime.KeepAlive(pods)
	}
}

func TestTrimPodForControllerNonPod(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:          "s1",
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "ctrl"}},
		},
	}
	ret, err := TrimPodForController(subnet)
	require.NoError(t, err)
	accessor, err := meta.Accessor(ret)
	require.NoError(t, err)
	require.Empty(t, accessor.GetManagedFields())

	_, err = TrimPodForController("not-an-object")
	require.Error(t, err)
}
