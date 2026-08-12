package bfddsupervisor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMetricsHandlerServesSupervisorHealth(t *testing.T) {
	reporter := NewMetricsReporter()

	tests := []struct {
		name       string
		path       string
		status     SupervisorStatus
		statusCode int
	}{
		{name: "live", path: "/livez", status: SupervisorStatus{Live: true, Ready: false}, statusCode: http.StatusOK},
		{name: "not live", path: "/livez", status: SupervisorStatus{Live: false}, statusCode: http.StatusServiceUnavailable},
		{name: "metrics", path: "/metrics", statusCode: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := reporter.handler(func() SupervisorStatus { return test.status })
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.statusCode, response.Code)
		})
	}
}

func TestMetricsReporterPublishesSessionAndCircuitState(t *testing.T) {
	reporter := NewMetricsReporter()
	reporter.Update(time.Unix(12_000, 0), SupervisorStatus{
		Live: false, Ready: false, ChildRestarts: 1, ChildCircuitOpen: true,
		Sessions: []SessionRecoveryStatus{
			{Pair: SessionPair{Local: "10.16.0.2", Remote: "10.255.255.255"}, State: SessionDown, Phase: RecoveryCircuitOpen, Attempts: 3, LastAction: RecoveryRestartChild, LastResult: "failed"},
			{Pair: SessionPair{Local: "fd00::2", Remote: "fd00::ffff"}, State: SessionUp, Phase: RecoveryUp},
		},
	})

	expected := `
# HELP kube_ovn_bfdd_circuit_open Whether automatic recovery is circuit-open for a BFD session.
# TYPE kube_ovn_bfdd_circuit_open gauge
kube_ovn_bfdd_circuit_open{local="10.16.0.2",remote="10.255.255.255"} 1
kube_ovn_bfdd_circuit_open{local="fd00::2",remote="fd00::ffff"} 0
# HELP kube_ovn_bfdd_child_circuit_open Whether automatic OpenBFDD child recovery is circuit-open.
# TYPE kube_ovn_bfdd_child_circuit_open gauge
kube_ovn_bfdd_child_circuit_open 1
# HELP kube_ovn_bfdd_expected_sessions Number of BFD sessions expected by the supervisor.
# TYPE kube_ovn_bfdd_expected_sessions gauge
kube_ovn_bfdd_expected_sessions 2
# HELP kube_ovn_bfdd_up_sessions Number of expected BFD sessions currently Up.
# TYPE kube_ovn_bfdd_up_sessions gauge
kube_ovn_bfdd_up_sessions 1
`
	err := testutil.GatherAndCompare(reporter.Registry(), strings.NewReader(expected),
		"kube_ovn_bfdd_child_circuit_open", "kube_ovn_bfdd_circuit_open", "kube_ovn_bfdd_expected_sessions", "kube_ovn_bfdd_up_sessions")
	require.NoError(t, err)
}
