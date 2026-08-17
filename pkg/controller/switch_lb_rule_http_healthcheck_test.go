package controller

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNormalizeHTTPHealthCheckPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty defaults to slash", path: "", want: "/"},
		{name: "absolute path", path: "/healthz", want: "/healthz"},
		{name: "relative path", path: "healthz", want: "/healthz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeHTTPHealthCheckPath(tt.path))
		})
	}
}

func TestServiceHTTPHealthCheckEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		svc      *corev1.Service
		expected bool
	}{
		{name: "nil service", svc: nil, expected: false},
		{name: "no annotations", svc: &corev1.Service{}, expected: false},
		{
			name: "invalid port",
			svc: &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.ServiceHTTPHealthCheckPort: "abc"},
			}},
			expected: false,
		},
		{
			name: "zero port",
			svc: &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.ServiceHTTPHealthCheckPort: "0"},
			}},
			expected: false,
		},
		{
			name: "valid port",
			svc: &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.ServiceHTTPHealthCheckPort: "8081"},
			}},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, serviceHTTPHealthCheckEnabled(tt.svc))
		})
	}
}

func TestSetHTTPHealthCheckAnnotation(t *testing.T) {
	t.Parallel()

	t.Run("set port and default path", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		slr := &kubeovnv1.SwitchLBRule{Spec: kubeovnv1.SwitchLBRuleSpec{
			HealthCheck: &kubeovnv1.SwitchLBRuleHealthCheck{Port: 8081},
		}}
		setHTTPHealthCheckAnnotation(svc, slr)
		require.Equal(t, "8081", svc.Annotations[util.ServiceHTTPHealthCheckPort])
		require.Equal(t, "/", svc.Annotations[util.ServiceHTTPHealthCheckPath])
	})

	t.Run("set custom path", func(t *testing.T) {
		svc := &corev1.Service{}
		slr := &kubeovnv1.SwitchLBRule{Spec: kubeovnv1.SwitchLBRuleSpec{
			HealthCheck: &kubeovnv1.SwitchLBRuleHealthCheck{Port: 9090, Path: "ready"},
		}}
		setHTTPHealthCheckAnnotation(svc, slr)
		require.Equal(t, "9090", svc.Annotations[util.ServiceHTTPHealthCheckPort])
		require.Equal(t, "/ready", svc.Annotations[util.ServiceHTTPHealthCheckPath])
	})

	t.Run("clear when health check removed", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			util.ServiceHTTPHealthCheckPort: "8081",
			util.ServiceHTTPHealthCheckPath: "/healthz",
		}}}
		setHTTPHealthCheckAnnotation(svc, &kubeovnv1.SwitchLBRule{})
		require.Empty(t, svc.Annotations[util.ServiceHTTPHealthCheckPort])
		require.Empty(t, svc.Annotations[util.ServiceHTTPHealthCheckPath])
	})
}

func TestGenerateHeadlessServiceHTTPHealthCheck(t *testing.T) {
	t.Parallel()

	slr := &kubeovnv1.SwitchLBRule{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: kubeovnv1.SwitchLBRuleSpec{
			Vip:       "10.0.0.100",
			Namespace: "default",
			Ports: []kubeovnv1.SwitchLBRulePort{{
				Name:       "http",
				Port:       80,
				TargetPort: 8080,
				Protocol:   "TCP",
			}},
			HealthCheck: &kubeovnv1.SwitchLBRuleHealthCheck{Port: 8081, Path: "/healthz"},
		},
	}

	svc := generateHeadlessService(slr, nil)
	require.Equal(t, "8081", svc.Annotations[util.ServiceHTTPHealthCheckPort])
	require.Equal(t, "/healthz", svc.Annotations[util.ServiceHTTPHealthCheckPath])
	require.Equal(t, "10.0.0.100", svc.Annotations[util.SwitchLBRuleVipsAnnotation])
}

func TestProbeHTTPHealth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	host, portStr, err := net.SplitHostPort(mustURLHostPort(t, server.URL))
	require.NoError(t, err)
	port, err := strconv.ParseInt(portStr, 10, 32)
	require.NoError(t, err)

	require.True(t, probeHTTPHealth(server.Client(), host, int32(port), "/healthz", time.Second))
	require.False(t, probeHTTPHealth(server.Client(), host, int32(port), "/fail", time.Second))
	require.False(t, probeHTTPHealth(server.Client(), host, int32(port), "/missing", time.Second))
	require.False(t, probeHTTPHealth(server.Client(), host, 1, "/healthz", 50*time.Millisecond))
}

func TestFilterHTTPHealthCheckBackends(t *testing.T) {
	t.Parallel()

	c := &Controller{httpHealthCheckStatus: xsync.NewMap[string, bool]()}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "slr-demo",
			Namespace: "default",
			Annotations: map[string]string{
				util.ServiceHTTPHealthCheckPort: "8081",
			},
		},
	}
	backends := []string{"10.0.0.1:8080", "10.0.0.2:8080", "10.0.0.3:8080"}

	require.Empty(t, c.filterHTTPHealthCheckBackends(svc, backends))

	c.httpHealthCheckStatus.Store(httpHealthCheckCacheKey("default", "slr-demo", "10.0.0.2"), true)
	c.httpHealthCheckStatus.Store(httpHealthCheckCacheKey("default", "slr-demo", "10.0.0.3"), false)
	require.Equal(t, []string{"10.0.0.2:8080"}, c.filterHTTPHealthCheckBackends(svc, backends))

	plainSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"}}
	require.Equal(t, backends, c.filterHTTPHealthCheckBackends(plainSvc, backends))
}

func TestCheckSwitchLBRuleHTTPHealth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	host, portStr, err := net.SplitHostPort(mustURLHostPort(t, server.URL))
	require.NoError(t, err)
	port, err := strconv.ParseInt(portStr, 10, 32)
	require.NoError(t, err)

	fc := newFakeController(t)
	c := fc.fakeController
	slr := &kubeovnv1.SwitchLBRule{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: kubeovnv1.SwitchLBRuleSpec{
			Namespace: "default",
			Endpoints: []string{host, "192.0.2.1"},
			HealthCheck: &kubeovnv1.SwitchLBRuleHealthCheck{
				Port:            int32(port),
				Path:            "/healthz",
				IntervalSeconds: 1,
				TimeoutSeconds:  1,
			},
		},
	}

	require.NoError(t, c.checkSwitchLBRuleHTTPHealth(slr))
	require.True(t, c.isHTTPHealthCheckHealthy("default", generateSvcName("demo"), host))
	require.False(t, c.isHTTPHealthCheckHealthy("default", generateSvcName("demo"), "192.0.2.1"))
}

func mustURLHostPort(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return parsed.Host
}
