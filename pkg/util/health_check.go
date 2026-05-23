package util

import (
	"net/http"
	"sync/atomic"

	"k8s.io/klog/v2"
)

type livenessProbe func() error

var livezProbe atomic.Pointer[livenessProbe]

// RegisterLivezProbe installs a custom liveness probe consulted by
// LivezHandler. A non-nil error returned by the probe causes the
// /livez endpoint to respond with HTTP 503. Passing nil clears any
// previously registered probe. Safe to call concurrently.
func RegisterLivezProbe(p func() error) {
	if p == nil {
		livezProbe.Store(nil)
		return
	}
	fn := livenessProbe(p)
	livezProbe.Store(&fn)
}

func DefaultHealthCheckHandler(w http.ResponseWriter, _ *http.Request) {
	if _, err := w.Write([]byte("ok")); err != nil {
		klog.Errorf("failed to write health check response: %v", err)
	}
}

// LivezHandler responds to /livez requests. When a probe has been
// installed via RegisterLivezProbe it is invoked on every request; a
// non-nil error becomes an HTTP 503. Otherwise the behaviour matches
// DefaultHealthCheckHandler.
func LivezHandler(w http.ResponseWriter, r *http.Request) {
	if p := livezProbe.Load(); p != nil {
		if err := (*p)(); err != nil {
			klog.Warningf("liveness probe failed: %v", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	DefaultHealthCheckHandler(w, r)
}
