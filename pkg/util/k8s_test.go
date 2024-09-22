package util

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
)

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
		{"Timeout", "http://localhost:8080", 1 * time.Millisecond, false, errors.New(`timed out dialing host`)},
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
		node v1.Node
		exp4 string
		exp6 string
	}{
		{
			name: "correct",
			node: v1.Node{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.NodeSpec{},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    "InternalIP",
							Address: "192.168.0.2",
						},
						{
							Type:    "ExternalIP",
							Address: "192.188.0.4",
						},
						{
							Type:    "InternalIP",
							Address: "ffff:ffff:ffff:ffff:ffff::23",
						},
					},
				},
			},
			exp4: "192.168.0.2",
			exp6: "ffff:ffff:ffff:ffff:ffff::23",
		},
		{
			name: "correctWithDiff",
			node: v1.Node{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.NodeSpec{},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    "InternalIP",
							Address: "ffff:ffff:ffff:ffff:ffff::23",
						},
						{
							Type:    "ExternalIP",
							Address: "192.188.0.4",
						},
						{
							Type:    "InternalIP",
							Address: "192.188.0.43",
						},
					},
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

func TestGetPodIPs(t *testing.T) {
	tests := []struct {
		name string
		pod  v1.Pod
		exp  []string
	}{
		{
			name: "pod_with_one_pod_ip",
			pod: v1.Pod{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.PodSpec{},
				Status: v1.PodStatus{
					PodIPs: []v1.PodIP{{IP: "192.168.1.100"}},
					PodIP:  "192.168.1.100",
				},
			},
			exp: []string{"192.168.1.100"},
		},
		{
			name: "pod_with_one_pod_ip",
			pod: v1.Pod{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.PodSpec{},
				Status: v1.PodStatus{
					PodIPs: []v1.PodIP{{IP: "192.168.1.100"}, {IP: "fd00:10:16::8"}},
					PodIP:  "192.168.1.100",
				},
			},
			exp: []string{"192.168.1.100", "fd00:10:16::8"},
		},

		{
			name: "pod_with_no_pod_ip",
			pod: v1.Pod{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.PodSpec{},
				Status: v1.PodStatus{
					PodIPs: []v1.PodIP{},
					PodIP:  "",
				},
			},
			exp: []string{},
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
		svc  v1.Service
		exp  []string
	}{
		{
			name: "service_with_one_cluster_ip",
			svc: v1.Service{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec: v1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1"},
				},
			},
			exp: []string{"10.96.0.1"},
		},
		{
			name: "service_with_two_cluster_ip",
			svc: v1.Service{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec: v1.ServiceSpec{
					ClusterIP:  "10.96.0.1",
					ClusterIPs: []string{"10.96.0.1", "fd00:10:16::1"},
				},
			},
			exp: []string{"10.96.0.1", "fd00:10:16::1"},
		},
		{
			name: "service_with_no_cluster_ip",
			svc: v1.Service{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec: v1.ServiceSpec{
					ClusterIP:  "",
					ClusterIPs: []string{},
				},
			},
			exp: []string{},
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

func TestUpdateNodeLabels(t *testing.T) {
	client := fake.NewSimpleClientset()
	nodeClient := client.CoreV1().Nodes()
	tests := []struct {
		name   string
		cs     clientv1.NodeInterface
		node   string
		labels map[string]any
		exp    error
	}{
		{
			name: "node_with_labels",
			cs:   nodeClient,
			node: "node1",
			labels: map[string]any{
				"key1": "value1",
			},
			exp: nil,
		},
		{
			name:   "node_with_nil_labels",
			cs:     nodeClient,
			node:   "node2",
			labels: map[string]any{},
			exp:    nil,
		},
	}
	for _, tt := range tests {
		// create a node
		node, err := client.CoreV1().Nodes().Create(context.Background(), &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: tt.node,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NotNil(t, node)
		t.Run(tt.name, func(t *testing.T) {
			err := UpdateNodeLabels(tt.cs, tt.node, tt.labels)
			if tt.exp == nil {
				require.NoError(t, err)
				return
			}
			if errors.Is(err, tt.exp) {
				t.Errorf("got %v, want %v", err, tt.exp)
			}
		})
	}
}

func TestUpdateNodeAnnotations(t *testing.T) {
	client := fake.NewSimpleClientset()
	nodeClient := client.CoreV1().Nodes()
	tests := []struct {
		name        string
		cs          clientv1.NodeInterface
		node        string
		annotations map[string]any
		exp         error
	}{
		{
			name: "node_with_annotations",
			cs:   nodeClient,
			node: "node1",
			annotations: map[string]any{
				"key1": "value1",
			},
			exp: nil,
		},
		{
			name:        "node_with_nil_annotations",
			cs:          nodeClient,
			node:        "node2",
			annotations: map[string]any{},
			exp:         nil,
		},
	}
	for _, tt := range tests {
		// create a node
		node, err := client.CoreV1().Nodes().Create(context.Background(), &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: tt.node,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NotNil(t, node)
		t.Run(tt.name, func(t *testing.T) {
			err := UpdateNodeAnnotations(tt.cs, tt.node, tt.annotations)
			if tt.exp == nil {
				require.NoError(t, err)
				return
			}
			if errors.Is(err, tt.exp) {
				t.Errorf("got %v, want %v", err, tt.exp)
			}
		})
	}
}

func TestNodeMergePatch(t *testing.T) {
	client := fake.NewSimpleClientset()
	nodeClient := client.CoreV1().Nodes()
	tests := []struct {
		name  string
		cs    clientv1.NodeInterface
		node  string
		patch string
		exp   error
	}{
		{
			name:  "node_with_patch",
			cs:    nodeClient,
			node:  "node",
			patch: `{"metadata":{"labels":{"key1":"value1"}}}`,
			exp:   nil,
		},
	}
	for _, tt := range tests {
		// create a node
		node, err := client.CoreV1().Nodes().Create(context.Background(), &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: tt.node,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NotNil(t, node)
		t.Run(tt.name, func(t *testing.T) {
			err := nodeMergePatch(tt.cs, tt.node, tt.patch)
			if tt.exp == nil {
				require.NoError(t, err)
				return
			}
			if errors.Is(err, tt.exp) {
				t.Errorf("got %v, want %v", err, tt.exp)
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
