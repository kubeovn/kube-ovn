package util

import (
	"net/http"

	"k8s.io/klog/v2"
)

func DefaultHealthCheckHandler(w http.ResponseWriter, _ *http.Request) {
	if _, err := w.Write([]byte("ok")); err != nil {
		klog.Errorf("failed to write health check response: %v", err)
	}
}
