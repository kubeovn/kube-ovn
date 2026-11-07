package controllerruntime

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
)

func TestVlanReconcilerUsesExistingCoreEventPermissions(t *testing.T) {
	type eventRequest struct {
		path string
		body []byte
		err  error
	}
	requests := make(chan eventRequest, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- eventRequest{path: r.URL.Path, body: body, err: err}
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/namespaces/default/events" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	mgr, err := NewManager(&controller.Configuration{KubeRestConfig: &rest.Config{Host: server.URL}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	managerDone := make(chan error, 1)
	go func() { managerDone <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-managerDone:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("manager did not stop")
		}
	})
	r := newVlanReconciler(mgr, &recordingVlanOVN{}, "provider")
	vlan := &kubeovnv1.Vlan{Name: "vlan-a", UID: "vlan-uid"}
	r.recordEvent(vlan, corev1.EventTypeNormal, "ReconcileSuccess", "Vlan reconciled successfully")
	select {
	case request := <-requests:
		require.NoError(t, request.err)
		require.Equal(t, "/api/v1/namespaces/default/events", request.path)
		var event corev1.Event
		require.NoError(t, json.Unmarshal(request.body, &event))
		require.Equal(t, "Vlan", event.InvolvedObject.Kind)
		require.Equal(t, vlan.Name, event.InvolvedObject.Name)
		require.Equal(t, vlan.UID, event.InvolvedObject.UID)
		require.Equal(t, "kube-ovn-vlan", event.Source.Component)
		require.Equal(t, "ReconcileSuccess", event.Reason)
		require.Equal(t, corev1.EventTypeNormal, event.Type)
		require.Equal(t, "Vlan reconciled successfully", event.Message)
	case <-time.After(5 * time.Second):
		t.Fatal("VLAN event did not reach the API server")
	}
}
