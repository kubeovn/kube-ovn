package bfddsupervisor

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

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
