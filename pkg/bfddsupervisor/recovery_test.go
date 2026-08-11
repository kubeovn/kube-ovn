package bfddsupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

type fakeControl struct {
	sessions  []Session
	statusErr error
}

func (c *fakeControl) Status(context.Context) ([]Session, error) {
	return append([]Session(nil), c.sessions...), c.statusErr
}

func (c *fakeControl) Configure(context.Context, DaemonConfig) error {
	return nil
}

func (c *fakeControl) Reset(context.Context, SessionPair) error {
	return nil
}

type fakeChild struct {
	running  bool
	restarts int
}

type blockingChild struct {
	fakeChild
	started chan struct{}
	release chan struct{}
}

type failingRestartChild struct{ fakeChild }

func (c *failingRestartChild) Restart(context.Context) error {
	return errors.New("control never became ready")
}

type boundedFailingChild struct {
	fakeChild
	attempts int
}

func (c *boundedFailingChild) Restart(context.Context) error {
	c.attempts++
	return errors.New("child failed to start")
}

func (c *blockingChild) Restart(context.Context) error {
	close(c.started)
	<-c.release
	return nil
}

func (c *fakeChild) Running() bool {
	return c.running
}

func (c *fakeChild) Restart(context.Context) error {
	c.running = true
	c.restarts++
	return nil
}

func TestSupervisorStaysNotLiveUntilChildBootstrapCompletes(t *testing.T) {
	clock := &fakeClock{now: time.Unix(500, 0)}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{
			MinTXMilliseconds: 100,
			MinRXMilliseconds: 100,
			Multiplier:        3,
			PeerIPs:           []string{"10.255.255.255"},
		},
		PodIPs:       []string{"10.16.0.2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:  time.Hour,
	}, &fakeControl{}, child, clock)
	require.NoError(t, err)
	require.False(t, supervisor.Status().Live)

	require.NoError(t, supervisor.StartChild(context.Background()))
	require.True(t, supervisor.Status().Live)
}

func TestSupervisorWaitsForGraceBeforeReplayingConfiguration(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	control := &fakeControl{}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{
			MinTXMilliseconds: 100,
			MinRXMilliseconds: 100,
			Multiplier:        3,
			PeerIPs:           []string{"10.255.255.255"},
		},
		PodIPs:       []string{"10.16.0.2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:  time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	status := supervisor.Status()
	require.True(t, status.Live)
	require.False(t, status.Ready)
	require.Equal(t, RecoveryGrace, status.Sessions[0].Phase)
	require.Equal(t, RecoveryNone, status.Sessions[0].LastAction)

	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	status = supervisor.Status()
	require.Equal(t, RecoveryBackoff, status.Sessions[0].Phase)
	require.Equal(t, RecoveryReplay, status.Sessions[0].LastAction)
	require.Equal(t, 1, status.Sessions[0].Attempts)
	require.Equal(t, clock.now.Add(time.Minute), status.Sessions[0].NextRetry)
}

func TestSupervisorResetsOnlyThePersistentlyDownSession(t *testing.T) {
	clock := &fakeClock{now: time.Unix(2_000, 0)}
	control := &fakeControl{sessions: []Session{{
		ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown,
	}}}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{
			MinTXMilliseconds: 100,
			MinRXMilliseconds: 100,
			Multiplier:        3,
			PeerIPs:           []string{"10.255.255.255"},
		},
		PodIPs:       []string{"10.16.0.2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:  time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))

	status := supervisor.Status().Sessions[0]
	require.Equal(t, RecoveryReset, status.LastAction)
	require.Equal(t, 2, status.Attempts)
	require.Equal(t, RecoveryBackoff, status.Phase)
	require.Equal(t, clock.now.Add(5*time.Minute), status.NextRetry)
}

func TestSupervisorRestartsChildOnlyAfterLighterActionsFailForAllSessions(t *testing.T) {
	clock := &fakeClock{now: time.Unix(3_000, 0)}
	control := &fakeControl{sessions: []Session{{
		ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown,
	}}}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{
			MinTXMilliseconds: 100,
			MinRXMilliseconds: 100,
			Multiplier:        3,
			PeerIPs:           []string{"10.255.255.255"},
		},
		PodIPs:       []string{"10.16.0.2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:  time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(5 * time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))

	status := supervisor.Status()
	require.Equal(t, 1, status.ChildRestarts)
	require.Equal(t, RecoveryRestartChild, status.Sessions[0].LastAction)
}

func TestSupervisorRestartsChildWhenAllSessionsRemainMissingAfterReplay(t *testing.T) {
	clock := &fakeClock{now: time.Unix(3_500, 0)}
	control := &fakeControl{}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))

	status := supervisor.Status()
	require.Equal(t, 1, status.ChildRestarts)
	require.Equal(t, RecoveryRestartChild, status.Sessions[0].LastAction)
}

func TestSupervisorCircuitsFailedPeerWithoutRestartingHealthyAddressFamily(t *testing.T) {
	clock := &fakeClock{now: time.Unix(4_000, 0)}
	control := &fakeControl{sessions: []Session{
		{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown},
		{ID: 2, Local: "fd00::2", Remote: "fd00::ffff", State: SessionUp},
	}}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{
			MinTXMilliseconds: 100,
			MinRXMilliseconds: 100,
			Multiplier:        3,
			PeerIPs:           []string{"10.255.255.255", "fd00::ffff"},
		},
		PodIPs:       []string{"10.16.0.2", "fd00::2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:  time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(5 * time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	status := supervisor.Status()
	require.Equal(t, RecoveryBackoff, status.Sessions[0].Phase)
	require.Equal(t, clock.now.Add(30*time.Minute), status.Sessions[0].NextRetry)
	clock.now = status.Sessions[0].NextRetry
	require.NoError(t, supervisor.Reconcile(context.Background()))

	status = supervisor.Status()
	require.Equal(t, 0, status.ChildRestarts)
	require.Equal(t, RecoveryCircuitOpen, status.Sessions[0].Phase)
	require.Equal(t, 4, status.Sessions[0].Attempts)
	require.Equal(t, RecoveryUp, status.Sessions[1].Phase)
}

func TestSupervisorAllowsOneHalfOpenAttemptPerCircuitInterval(t *testing.T) {
	clock := &fakeClock{now: time.Unix(5_000, 0)}
	control := &fakeControl{sessions: []Session{{
		ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown,
	}}}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{
			MinTXMilliseconds: 100,
			MinRXMilliseconds: 100,
			Multiplier:        3,
			PeerIPs:           []string{"10.255.255.255"},
		},
		PodIPs:       []string{"10.16.0.2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:  time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	for _, advance := range []time.Duration{0, time.Minute, time.Minute, 5 * time.Minute} {
		clock.now = clock.now.Add(advance)
		require.NoError(t, supervisor.Reconcile(context.Background()))
	}
	before := supervisor.Status().Sessions[0]
	require.Equal(t, 3, before.Attempts)
	require.Equal(t, RecoveryBackoff, before.Phase)
	require.Equal(t, clock.now.Add(30*time.Minute), before.NextRetry)

	clock.now = before.NextRetry
	require.NoError(t, supervisor.Reconcile(context.Background()))
	before = supervisor.Status().Sessions[0]
	require.Equal(t, 4, before.Attempts)
	require.Equal(t, RecoveryCircuitOpen, before.Phase)
	require.Equal(t, clock.now.Add(time.Hour), before.NextRetry)

	clock.now = before.NextRetry
	require.NoError(t, supervisor.Reconcile(context.Background()))
	after := supervisor.Status().Sessions[0]
	require.Equal(t, 5, after.Attempts)
	require.Equal(t, RecoveryReplay, after.LastAction)
	require.Equal(t, RecoveryCircuitOpen, after.Phase)
	require.Equal(t, clock.now.Add(time.Hour), after.NextRetry)
}

func TestSupervisorClearsOnlyTheRecoveredPeerBudgetAfterStableUp(t *testing.T) {
	clock := &fakeClock{now: time.Unix(6_000, 0)}
	control := &fakeControl{sessions: []Session{
		{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown},
		{ID: 2, Local: "fd00::2", Remote: "fd00::ffff", State: SessionUp},
	}}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255", "fd00::ffff"}},
		PodIPs: []string{"10.16.0.2", "fd00::2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, supervisor.Status().Sessions[0].Attempts)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, supervisor.Status().Sessions[0].Attempts, "healthy IPv6 must not clear the IPv4 recovery budget")

	control.sessions[0] = Session{ID: 3, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionUp}
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	status := supervisor.Status().Sessions[0]
	require.Equal(t, 0, status.Attempts)
	require.Equal(t, RecoveryNone, status.LastAction)
}

func TestSupervisorRestoresRecoveryBudgetAcrossContainerRestart(t *testing.T) {
	clock := &fakeClock{now: time.Unix(7_000, 0)}
	control := &fakeControl{}
	child := &fakeChild{running: true}
	config := SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
		StatePath: filepath.Join(t.TempDir(), "state.json"),
	}

	first, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, first.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, first.Reconcile(context.Background()))
	require.Equal(t, 1, first.Status().Sessions[0].Attempts)

	second, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, second.Reconcile(context.Background()))
	require.Equal(t, 1, second.Status().Sessions[0].Attempts)
}

func TestSupervisorStatusRemainsResponsiveDuringPlannedChildRestart(t *testing.T) {
	clock := &fakeClock{now: time.Unix(8_000, 0)}
	control := &fakeControl{sessions: []Session{{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown}}}
	child := &blockingChild{fakeChild: fakeChild{running: true}, started: make(chan struct{}), release: make(chan struct{})}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(5 * time.Minute)

	reconcileDone := make(chan error, 1)
	go func() { reconcileDone <- supervisor.Reconcile(context.Background()) }()
	<-child.started
	statusDone := make(chan SupervisorStatus, 1)
	go func() { statusDone <- supervisor.Status() }()
	select {
	case status := <-statusDone:
		require.True(t, status.Live)
	case <-time.After(100 * time.Millisecond):
		close(child.release)
		<-reconcileDone
		t.Fatal("status blocked during planned child restart")
	}
	close(child.release)
	require.NoError(t, <-reconcileDone)
}

func TestSupervisorKeepsLivenessDuringPlannedChildRestartBackoff(t *testing.T) {
	clock := &fakeClock{now: time.Unix(9_000, 0)}
	control := &fakeControl{sessions: []Session{{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionDown}}}
	child := &failingRestartChild{fakeChild{running: true}}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(5 * time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	status := supervisor.Status()
	require.True(t, status.Live)
	require.Equal(t, 1, status.ChildRecoveryAttempts)
	require.Equal(t, clock.now.Add(time.Minute), status.ChildNextRetry)
}

func TestSupervisorRestartsChildAfterRepeatedControlFailures(t *testing.T) {
	clock := &fakeClock{now: time.Unix(10_000, 0)}
	control := &fakeControl{statusErr: errors.New("control unavailable")}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 0, child.restarts)
	require.True(t, supervisor.Status().Live)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, child.restarts)
	require.True(t, supervisor.Status().Live)
}

func TestSupervisorFailsLivenessAfterExhaustingCurrentGenerationRecovery(t *testing.T) {
	clock := &fakeClock{now: time.Unix(11_000, 0)}
	control := &fakeControl{statusErr: errors.New("control unavailable")}
	child := &boundedFailingChild{fakeChild: fakeChild{running: true}}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Error(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, child.attempts)
	require.True(t, supervisor.Status().Live)

	clock.now = clock.now.Add(time.Minute)
	require.Error(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 2, child.attempts)
	require.True(t, supervisor.Status().Live)

	clock.now = clock.now.Add(5 * time.Minute)
	require.Error(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 3, child.attempts)
	require.True(t, supervisor.Status().Live)
	require.Equal(t, clock.now.Add(30*time.Minute), supervisor.Status().ChildNextRetry)

	clock.now = clock.now.Add(30 * time.Minute)
	require.Error(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 4, child.attempts)
	require.False(t, supervisor.Status().Live)

	circuitUntil := supervisor.Status().ChildNextRetry
	require.Error(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 4, child.attempts, "open circuit must suppress repeated child restarts")
	clock.now = circuitUntil
	require.Error(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 5, child.attempts, "half-open must allow one child restart")
	require.False(t, supervisor.Status().Live)
}

func TestSupervisorFailsLivenessWhenSuccessfulRestartExhaustsChildCircuit(t *testing.T) {
	clock := &fakeClock{now: time.Unix(11_250, 0)}
	control := &fakeControl{statusErr: errors.New("control unavailable")}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon:        DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs:        []string{"10.16.0.2"},
		GracePeriod:   time.Minute,
		StablePeriod:  time.Minute,
		Backoffs:      []time.Duration{time.Minute},
		ChildBackoffs: []time.Duration{time.Second},
		CircuitOpen:   time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	for range 3 {
		require.NoError(t, supervisor.Reconcile(context.Background()))
	}
	clock.now = clock.now.Add(time.Second)
	for range 3 {
		require.NoError(t, supervisor.Reconcile(context.Background()))
	}
	require.Equal(t, 2, child.restarts)
	require.True(t, supervisor.Status().ChildCircuitOpen)

	control.statusErr = nil
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.False(t, supervisor.Status().Live, "current generation must fail liveness once after exhausting child recovery")
}

func TestSupervisorKeepsRuntimeLiveWhenContainerInheritsOpenChildCircuit(t *testing.T) {
	clock := &fakeClock{now: time.Unix(11_500, 0)}
	control := &fakeControl{statusErr: errors.New("control unavailable")}
	child := &boundedFailingChild{fakeChild: fakeChild{running: false}}
	config := SupervisorConfig{
		Daemon:        DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs:        []string{"10.16.0.2"},
		GracePeriod:   time.Minute,
		StablePeriod:  time.Minute,
		Backoffs:      []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		ChildBackoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		CircuitOpen:   time.Hour,
		StatePath:     filepath.Join(t.TempDir(), "state.json"),
	}

	first, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.Error(t, first.StartChild(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, first.Reconcile(context.Background()))
	require.NoError(t, first.Reconcile(context.Background()))
	require.Error(t, first.Reconcile(context.Background()))
	clock.now = clock.now.Add(5 * time.Minute)
	require.Error(t, first.Reconcile(context.Background()))
	clock.now = clock.now.Add(30 * time.Minute)
	require.Error(t, first.Reconcile(context.Background()))
	require.True(t, first.Status().ChildCircuitOpen)
	require.False(t, first.Status().Live, "new circuit in the current container generation must request one kubelet recovery")

	second, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.Error(t, second.StartChild(context.Background()))
	require.Equal(t, 5, child.attempts, "new container generation gets one bootstrap attempt")
	require.True(t, second.Status().Live, "child circuit must not fail supervisor runtime liveness")
	require.NoError(t, second.StartChild(context.Background()))
	require.Equal(t, 5, child.attempts, "same container generation must not bootstrap twice")

	require.NoError(t, second.Reconcile(context.Background()))
	require.NoError(t, second.Reconcile(context.Background()))
	require.NoError(t, second.Reconcile(context.Background()))
	require.True(t, second.Status().Live)
	require.Equal(t, 5, child.attempts, "inherited circuit must suppress same-interval retries")

	clock.now = second.Status().ChildNextRetry
	require.Error(t, second.Reconcile(context.Background()))
	require.Equal(t, 6, child.attempts, "expired circuit must allow one half-open retry")
	require.False(t, second.Status().Live, "failed half-open retry creates a new circuit for kubelet recovery")
}

func TestSupervisorSerializesChildRestartsAcrossFailedPeers(t *testing.T) {
	clock := &fakeClock{now: time.Unix(12_000, 0)}
	control := &fakeControl{}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255", "10.255.255.254"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.NoError(t, supervisor.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, child.restarts)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, child.restarts, "per-pod budget must prevent two peers from restarting the child at once")
}

func TestSupervisorKeepsChildRecoveryBudgetAcrossContainerRestart(t *testing.T) {
	clock := &fakeClock{now: time.Unix(13_000, 0)}
	control := &fakeControl{}
	child := &fakeChild{running: true}
	config := SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255", "10.255.255.254"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
		StatePath: filepath.Join(t.TempDir(), "state.json"),
	}

	first, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, first.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, first.Reconcile(context.Background()))
	require.NoError(t, first.Reconcile(context.Background()))
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, first.Reconcile(context.Background()))
	require.Equal(t, 1, child.restarts)

	second, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, second.Reconcile(context.Background()))
	require.Equal(t, 1, child.restarts, "container restart must not reset the per-pod child recovery budget")
}

func TestSupervisorBootstrapsChildOncePerContainerGenerationWithoutResettingBudget(t *testing.T) {
	clock := &fakeClock{now: time.Unix(14_000, 0)}
	control := &fakeControl{statusErr: errors.New("control unavailable")}
	child := &boundedFailingChild{fakeChild: fakeChild{running: false}}
	config := SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
		StatePath: filepath.Join(t.TempDir(), "state.json"),
	}

	first, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.Error(t, first.StartChild(context.Background()))
	require.Equal(t, 1, child.attempts)
	require.Equal(t, 1, first.Status().ChildRecoveryAttempts)
	require.True(t, first.Status().Live)
	require.NoError(t, first.Reconcile(context.Background()))
	require.True(t, first.Status().Live, "internal start backoff must not trigger kubelet recovery early")

	second, err := NewSupervisor(config, control, child, clock)
	require.NoError(t, err)
	require.Error(t, second.StartChild(context.Background()))
	require.Equal(t, 2, child.attempts, "each container generation must bootstrap its own child")
	require.Equal(t, 1, second.Status().ChildRecoveryAttempts, "bootstrap must not reset the persisted recovery budget")
	require.True(t, second.Status().Live, "persisted recovery state must not cause a kubelet restart loop")

	require.NoError(t, second.StartChild(context.Background()))
	require.Equal(t, 2, child.attempts, "one container generation must bootstrap the child only once")
}

func TestSupervisorCanRetryBootstrapAfterStatePersistenceFailure(t *testing.T) {
	clock := &fakeClock{now: time.Unix(14_500, 0)}
	child := &boundedFailingChild{fakeChild: fakeChild{running: false}}
	blockedPath := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.Mkdir(blockedPath, 0o750))
	config := SupervisorConfig{
		Daemon:       DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs:       []string{"10.16.0.2"},
		GracePeriod:  time.Minute,
		StablePeriod: time.Minute,
		Backoffs:     []time.Duration{time.Minute},
		CircuitOpen:  time.Hour,
		StatePath:    filepath.Join(blockedPath, "state.json"),
	}
	supervisor, err := NewSupervisor(config, &fakeControl{}, child, clock)
	require.NoError(t, err)
	require.NoError(t, os.Remove(blockedPath))
	require.NoError(t, os.WriteFile(blockedPath, []byte("not a directory"), 0o600))

	require.Error(t, supervisor.StartChild(context.Background()))
	require.Error(t, supervisor.StartChild(context.Background()))
	require.Equal(t, 0, child.attempts, "child must not start before recovery state can be persisted")
}

func TestSupervisorClearsChildBudgetOnlyAfterAllSessionsAreStable(t *testing.T) {
	clock := &fakeClock{now: time.Unix(15_000, 0)}
	control := &fakeControl{sessions: []Session{{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionUp}}}
	child := &boundedFailingChild{fakeChild: fakeChild{running: true}}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)
	require.Error(t, supervisor.StartChild(context.Background()))
	require.Equal(t, 1, supervisor.Status().ChildRecoveryAttempts)

	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 1, supervisor.Status().ChildRecoveryAttempts)
	clock.now = clock.now.Add(time.Minute)
	require.NoError(t, supervisor.Reconcile(context.Background()))
	require.Equal(t, 0, supervisor.Status().ChildRecoveryAttempts)
	require.True(t, supervisor.Status().ChildNextRetry.IsZero())
}

func TestSupervisorIsolatesCorruptRecoveryState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte("not-json"), 0o600))
	clock := &fakeClock{now: time.Unix(16_000, 0)}

	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
		StatePath: statePath,
	}, &fakeControl{}, &fakeChild{running: true}, clock)
	require.NoError(t, err)
	require.NotNil(t, supervisor)
	_, err = os.Stat(statePath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(statePath + ".corrupt")
	require.NoError(t, err)
}
