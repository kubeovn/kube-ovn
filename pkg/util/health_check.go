package util

import (
	"net/http"

	"k8s.io/klog/v2"
)

func DefaultHealthCheckHandler(w http.ResponseWriter, _ *http.Request) {
	if _, err := w.Write([]byte("ok")); err != nil {
		klog.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
