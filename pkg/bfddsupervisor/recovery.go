package bfddsupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

type RecoveryPhase string

const (
	RecoveryUp          RecoveryPhase = "Up"
	RecoveryGrace       RecoveryPhase = "Grace"
	RecoveryBackoff     RecoveryPhase = "Backoff"
	RecoveryCircuitOpen RecoveryPhase = "CircuitOpen"
)

type RecoveryAction string

const (
	RecoveryNone         RecoveryAction = "None"
	RecoveryReplay       RecoveryAction = "ReplayConfiguration"
	RecoveryReset        RecoveryAction = "ResetSession"
	RecoveryRestartChild RecoveryAction = "RestartChild"
)

type SupervisorConfig struct {
	Daemon        DaemonConfig
	PodIPs        []string
	GracePeriod   time.Duration
	StablePeriod  time.Duration
	Backoffs      []time.Duration
	ChildBackoffs []time.Duration
	CircuitOpen   time.Duration
	StatePath     string
}

type ControlAdapter interface {
	Status(context.Context) ([]Session, error)
	Configure(context.Context, DaemonConfig) error
	Reset(context.Context, SessionPair) error
}

type ChildController interface {
	Running() bool
	Restart(context.Context) error
}

type Clock interface {
	Now() time.Time
}

type SessionRecoveryStatus struct {
	Pair       SessionPair
	State      SessionState
	Phase      RecoveryPhase
	Attempts   int
	LastAction RecoveryAction
	LastResult string
	NextRetry  time.Time
}

type SupervisorStatus struct {
	Live                  bool
	Ready                 bool
	Sessions              []SessionRecoveryStatus
	ControlFailures       int
	LastControlError      string
	ChildRestarts         int
	ChildRecoveryAttempts int
	ChildNextRetry        time.Time
	ChildCircuitOpen      bool
}

type recoveryEpisode struct {
	SessionRecoveryStatus
	failingSince time.Time
	stableSince  time.Time
	circuitUntil time.Time
}

type persistentEpisode struct {
	Status       SessionRecoveryStatus `json:"status"`
	FailingSince time.Time             `json:"failingSince,omitzero"`
	StableSince  time.Time             `json:"stableSince,omitzero"`
	CircuitUntil time.Time             `json:"circuitUntil,omitzero"`
}

type persistentState struct {
	Episodes              map[string]persistentEpisode `json:"episodes"`
	ChildRestarts         int                          `json:"childRestarts"`
	ChildRecoveryAttempts int                          `json:"childRecoveryAttempts,omitempty"`
	ChildNextRetry        time.Time                    `json:"childNextRetry,omitzero"`
	ChildCircuitUntil     time.Time                    `json:"childCircuitUntil,omitzero"`
}

type Supervisor struct {
	config   SupervisorConfig
	control  ControlAdapter
	child    ChildController
	clock    Clock
	expected []SessionPair

	mutex           sync.RWMutex
	reconcileMutex  sync.Mutex
	episodes        map[string]*recoveryEpisode
	status          SupervisorStatus
	controlFailures int

	childRecoveryAttempts   int
	childNextRetry          time.Time
	childCircuitUntil       time.Time
	childBootstrapAttempted bool
	inheritedCircuitUntil   time.Time
}

type pendingRecoveryAction struct {
	episode       *recoveryEpisode
	pair          SessionPair
	action        RecoveryAction
	childHalfOpen bool
}

func NewSupervisor(config SupervisorConfig, control ControlAdapter, child ChildController, clock Clock) (*Supervisor, error) {
	if control == nil || child == nil || clock == nil {
		return nil, errors.New("supervisor dependencies must not be nil")
	}
	if len(config.ChildBackoffs) == 0 {
		config.ChildBackoffs = slices.Clone(config.Backoffs)
	}
	if config.GracePeriod <= 0 || config.StablePeriod <= 0 || config.CircuitOpen <= 0 || len(config.Backoffs) == 0 || len(config.ChildBackoffs) == 0 {
		return nil, errors.New("invalid supervisor recovery timing")
	}

	podIPs, err := parseAddresses(config.PodIPs, "pod")
	if err != nil {
		return nil, err
	}
	peerIPs, err := parseAddresses(config.Daemon.PeerIPs, "peer")
	if err != nil {
		return nil, err
	}
	expected := make([]SessionPair, 0, len(podIPs)*len(peerIPs))
	for _, local := range podIPs {
		for _, remote := range peerIPs {
			if local.Is4() == remote.Is4() {
				expected = append(expected, SessionPair{Local: local.String(), Remote: remote.String()})
			}
		}
	}
	if len(expected) == 0 {
		return nil, errors.New("no same-family BFD session pairs")
	}
	slices.SortFunc(expected, func(a, b SessionPair) int {
		if a.Local != b.Local {
			if a.Local < b.Local {
				return -1
			}
			return 1
		}
		if a.Remote < b.Remote {
			return -1
		}
		if a.Remote > b.Remote {
			return 1
		}
		return 0
	})

	episodes := make(map[string]*recoveryEpisode, len(expected))
	for _, pair := range expected {
		episodes[pairKey(pair)] = &recoveryEpisode{SessionRecoveryStatus: SessionRecoveryStatus{
			Pair:       pair,
			Phase:      RecoveryGrace,
			LastAction: RecoveryNone,
		}}
	}
	supervisor := &Supervisor{config: config, control: control, child: child, clock: clock, expected: expected, episodes: episodes}
	if err := supervisor.loadState(); err != nil {
		return nil, err
	}
	if supervisor.childCircuitOpenLocked(clock.Now()) {
		supervisor.inheritedCircuitUntil = supervisor.childCircuitUntil
	}
	supervisor.refreshStatusLocked()
	return supervisor, nil
}

func (s *Supervisor) Reconcile(ctx context.Context) error {
	s.reconcileMutex.Lock()
	defer s.reconcileMutex.Unlock()

	now := s.clock.Now()
	sessions, err := s.control.Status(ctx)
	if err != nil {
		return s.reconcileControlFailure(ctx, now, err)
	}
	s.mutex.Lock()
	s.controlFailures = 0
	s.status.ControlFailures = 0
	s.status.LastControlError = ""
	s.mutex.Unlock()

	pending, state := s.planSessionRecovery(now, sessions)
	if err := s.saveState(state); err != nil {
		return err
	}
	if pending == nil {
		return nil
	}

	actionErr := s.executeRecoveryAction(ctx, *pending)
	state = s.recordRecoveryResult(*pending, actionErr)
	saveErr := s.saveState(state)
	if pending.childHalfOpen && actionErr != nil {
		return errors.Join(saveErr, fmt.Errorf("failed to half-open OpenBFDD child recovery circuit: %w", actionErr))
	}
	if actionErr != nil {
		return errors.Join(saveErr, fmt.Errorf("failed to execute BFD recovery action %s: %w", pending.action, actionErr))
	}
	return saveErr
}

func (s *Supervisor) StartChild(ctx context.Context) error {
	s.reconcileMutex.Lock()
	defer s.reconcileMutex.Unlock()

	now := s.clock.Now()
	s.mutex.Lock()
	if s.childBootstrapAttempted {
		s.mutex.Unlock()
		return nil
	}
	s.childBootstrapAttempted = true
	bootstrapHalfOpen := s.childHalfOpenDueLocked(now)
	state := s.persistentStateLocked()
	s.mutex.Unlock()
	if err := s.saveState(state); err != nil {
		s.mutex.Lock()
		s.childBootstrapAttempted = false
		s.mutex.Unlock()
		return err
	}

	startErr := s.child.Restart(ctx)
	s.mutex.Lock()
	if startErr != nil && !s.childCircuitOpenLocked(now) &&
		(s.childNextRetry.IsZero() || !now.Before(s.childNextRetry)) {
		s.reserveChildRestartLocked(now)
	}
	if startErr == nil && bootstrapHalfOpen {
		s.reserveChildRestartLocked(now)
		s.inheritedCircuitUntil = s.childCircuitUntil
	}
	if startErr != nil {
		s.updateLivenessLocked(now)
	} else {
		s.updateChildRunningLivenessLocked(now)
	}
	s.status.Ready = false
	s.refreshStatusLocked()
	state = s.persistentStateLocked()
	s.mutex.Unlock()
	if err := s.saveState(state); err != nil {
		return err
	}
	if startErr != nil {
		return fmt.Errorf("failed to start OpenBFDD child: %w", startErr)
	}
	return nil
}

func (s *Supervisor) reconcileControlFailure(_ context.Context, now time.Time, statusErr error) error {
	s.mutex.Lock()
	s.controlFailures++
	s.status.ControlFailures = s.controlFailures
	s.status.LastControlError = statusErr.Error()
	if s.controlFailures == 1 || s.controlFailures == 3 {
		klog.Errorf("failed to query OpenBFDD status (%d consecutive failures): %v", s.controlFailures, statusErr)
	}
	s.status.Ready = false
	s.updateControlFailureLivenessLocked(now)
	s.refreshStatusLocked()
	s.mutex.Unlock()
	return nil
}

func (s *Supervisor) planSessionRecovery(now time.Time, sessions []Session) (*pendingRecoveryAction, persistentState) {
	observed := make(map[string]Session, len(sessions))
	for _, session := range sessions {
		observed[pairKey(SessionPair{Local: session.Local, Remote: session.Remote})] = session
	}
	allUnhealthy := true
	for _, pair := range s.expected {
		if session, exists := observed[pairKey(pair)]; exists && session.State == SessionUp {
			allUnhealthy = false
			break
		}
	}

	s.mutex.Lock()
	s.status.Ready = true
	var pending *pendingRecoveryAction

	if allUnhealthy && s.childHalfOpenDueLocked(now) {
		s.status.Ready = false
		if s.reserveChildRestartLocked(now) {
			pending = &pendingRecoveryAction{action: RecoveryRestartChild, childHalfOpen: true}
		}
	} else {
		for _, pair := range s.expected {
			session, exists := observed[pairKey(pair)]
			pending = s.planEpisodeRecoveryLocked(now, pair, session, exists, allUnhealthy)
			if pending != nil {
				break
			}
		}
	}
	if !allUnhealthy && s.allExpectedSessionsStableLocked(now) {
		s.childRecoveryAttempts = 0
		s.childNextRetry = time.Time{}
		s.childCircuitUntil = time.Time{}
		s.inheritedCircuitUntil = time.Time{}
	}
	s.updateChildRunningLivenessLocked(now)
	s.refreshStatusLocked()
	state := s.persistentStateLocked()
	s.mutex.Unlock()
	return pending, state
}

func (s *Supervisor) planEpisodeRecoveryLocked(now time.Time, pair SessionPair, session Session, exists, allUnhealthy bool) *pendingRecoveryAction {
	episode := s.episodes[pairKey(pair)]
	episode.State = ""
	if exists {
		episode.State = session.State
	}
	if exists && session.State == SessionUp {
		s.markEpisodeUpLocked(now, pair, episode)
		return nil
	}

	s.status.Ready = false
	episode.stableSince = time.Time{}
	if episode.failingSince.IsZero() {
		episode.failingSince = now
		episode.Phase = RecoveryGrace
		return nil
	}
	if now.Before(episode.failingSince.Add(s.config.GracePeriod)) {
		episode.Phase = RecoveryGrace
		return nil
	}
	if !episode.NextRetry.IsZero() && now.Before(episode.NextRetry) {
		s.markEpisodeWaitingLocked(now, episode)
		return nil
	}

	action := s.selectRecoveryAction(episode.Attempts, exists, allUnhealthy)
	if action == RecoveryNone {
		return nil
	}
	if action == RecoveryRestartChild && !s.reserveChildRestartLocked(now) {
		episode.NextRetry = s.childNextRetry
		s.markEpisodeWaitingLocked(now, episode)
		return nil
	}
	s.scheduleEpisodeActionLocked(now, episode, action)
	return &pendingRecoveryAction{episode: episode, pair: pair, action: action}
}

func (s *Supervisor) markEpisodeUpLocked(now time.Time, pair SessionPair, episode *recoveryEpisode) {
	episode.Phase = RecoveryUp
	if episode.stableSince.IsZero() {
		episode.stableSince = now
		return
	}
	if now.Before(episode.stableSince.Add(s.config.StablePeriod)) {
		return
	}
	episode.SessionRecoveryStatus = SessionRecoveryStatus{
		Pair:       pair,
		State:      SessionUp,
		Phase:      RecoveryUp,
		LastAction: RecoveryNone,
	}
	episode.failingSince = time.Time{}
	episode.circuitUntil = time.Time{}
}

func (s *Supervisor) markEpisodeWaitingLocked(now time.Time, episode *recoveryEpisode) {
	if !episode.circuitUntil.IsZero() && now.Before(episode.circuitUntil) {
		episode.Phase = RecoveryCircuitOpen
	} else {
		episode.Phase = RecoveryBackoff
	}
}

func (s *Supervisor) selectRecoveryAction(attempts int, exists, allUnhealthy bool) RecoveryAction {
	switch {
	case attempts >= len(s.config.Backoffs), attempts == 0:
		return RecoveryReplay
	case attempts == 1 && exists:
		return RecoveryReset
	case attempts == 1 && allUnhealthy:
		return RecoveryRestartChild
	case attempts == 1:
		return RecoveryReplay
	case attempts == 2 && allUnhealthy:
		return RecoveryRestartChild
	case attempts == 2 && exists:
		return RecoveryReset
	case attempts == 2:
		return RecoveryReplay
	default:
		return RecoveryNone
	}
}

func (s *Supervisor) scheduleEpisodeActionLocked(now time.Time, episode *recoveryEpisode, action RecoveryAction) {
	episode.LastAction = action
	episode.LastResult = "in_progress"
	episode.Attempts++
	if episode.Attempts > len(s.config.Backoffs) {
		episode.circuitUntil = now.Add(s.config.CircuitOpen)
		episode.NextRetry = episode.circuitUntil
		episode.Phase = RecoveryCircuitOpen
		return
	}
	backoffIndex := min(episode.Attempts-1, len(s.config.Backoffs)-1)
	episode.NextRetry = now.Add(s.config.Backoffs[backoffIndex])
	episode.Phase = RecoveryBackoff
}

func (s *Supervisor) recordRecoveryResult(pending pendingRecoveryAction, actionErr error) persistentState {
	s.mutex.Lock()
	if pending.episode != nil {
		if actionErr != nil {
			pending.episode.LastResult = actionErr.Error()
		} else {
			pending.episode.LastResult = "success"
		}
	}
	if actionErr == nil {
		if pending.action == RecoveryRestartChild {
			s.status.ChildRestarts++
		}
	}
	if pending.action == RecoveryRestartChild && actionErr != nil {
		s.updateLivenessLocked(s.clock.Now())
	} else {
		s.updateChildRunningLivenessLocked(s.clock.Now())
	}
	s.refreshStatusLocked()
	state := s.persistentStateLocked()
	s.mutex.Unlock()
	return state
}

func (s *Supervisor) executeRecoveryAction(ctx context.Context, pending pendingRecoveryAction) error {
	switch pending.action {
	case RecoveryReplay:
		return s.control.Configure(ctx, s.config.Daemon)
	case RecoveryReset:
		return s.control.Reset(ctx, pending.pair)
	case RecoveryRestartChild:
		return s.child.Restart(ctx)
	default:
		return fmt.Errorf("unknown BFD recovery action %q", pending.action)
	}
}

func (s *Supervisor) Status() SupervisorStatus {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	status := s.status
	status.Sessions = append([]SessionRecoveryStatus(nil), s.status.Sessions...)
	return status
}

func (s *Supervisor) refreshStatusLocked() {
	s.status.Sessions = s.status.Sessions[:0]
	for _, pair := range s.expected {
		s.status.Sessions = append(s.status.Sessions, s.episodes[pairKey(pair)].SessionRecoveryStatus)
	}
	s.status.ChildRecoveryAttempts = s.childRecoveryAttempts
	s.status.ChildNextRetry = s.childNextRetry
	s.status.ChildCircuitOpen = s.childCircuitOpenLocked(s.clock.Now())
}

func (s *Supervisor) updateLivenessLocked(now time.Time) {
	s.status.Live = !s.childCircuitFailsLivenessLocked(now)
}

func (s *Supervisor) updateChildRunningLivenessLocked(now time.Time) {
	s.status.Live = s.child.Running() && !s.childCircuitFailsLivenessLocked(now)
}

func (s *Supervisor) updateControlFailureLivenessLocked(now time.Time) {
	if s.child.Running() {
		s.updateLivenessLocked(now)
		return
	}
	if s.childCircuitFailsLivenessLocked(now) {
		s.status.Live = false
		return
	}
	s.status.Live = !s.childNextRetry.IsZero() && now.Before(s.childNextRetry)
}

func (s *Supervisor) childCircuitFailsLivenessLocked(now time.Time) bool {
	if !s.childCircuitOpenLocked(now) {
		return false
	}
	return s.inheritedCircuitUntil.IsZero() || !s.childCircuitUntil.Equal(s.inheritedCircuitUntil)
}

func (s *Supervisor) reserveChildRestartLocked(now time.Time) bool {
	if !s.childNextRetry.IsZero() && now.Before(s.childNextRetry) {
		return false
	}
	s.childRecoveryAttempts++
	if s.childRecoveryAttempts > len(s.config.ChildBackoffs) {
		s.childCircuitUntil = now.Add(s.config.CircuitOpen)
		s.childNextRetry = s.childCircuitUntil
		return true
	}
	backoffIndex := min(s.childRecoveryAttempts-1, len(s.config.ChildBackoffs)-1)
	s.childNextRetry = now.Add(s.config.ChildBackoffs[backoffIndex])
	s.childCircuitUntil = time.Time{}
	return true
}

func (s *Supervisor) childCircuitOpenLocked(now time.Time) bool {
	return !s.childCircuitUntil.IsZero() && now.Before(s.childCircuitUntil)
}

func (s *Supervisor) childHalfOpenDueLocked(now time.Time) bool {
	return s.childRecoveryAttempts > len(s.config.ChildBackoffs) &&
		!s.childNextRetry.IsZero() && !now.Before(s.childNextRetry)
}

func (s *Supervisor) allExpectedSessionsStableLocked(now time.Time) bool {
	for _, pair := range s.expected {
		episode := s.episodes[pairKey(pair)]
		if episode.State != SessionUp || episode.stableSince.IsZero() || now.Before(episode.stableSince.Add(s.config.StablePeriod)) {
			return false
		}
	}
	return true
}

func pairKey(pair SessionPair) string {
	return pair.Local + "|" + pair.Remote
}

func parseAddresses(values []string, kind string) ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s IP %q: %w", kind, value, err)
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

func (s *Supervisor) loadState() error {
	if s.config.StatePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.config.StatePath) // #nosec G304 -- state path is fixed by supervisor configuration
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read BFD recovery state: %w", err)
	}
	var state persistentState
	if err := json.Unmarshal(data, &state); err != nil {
		corruptPath := s.config.StatePath + ".corrupt"
		if renameErr := os.Rename(s.config.StatePath, corruptPath); renameErr != nil {
			return errors.Join(
				fmt.Errorf("failed to decode BFD recovery state: %w", err),
				fmt.Errorf("failed to isolate corrupt BFD recovery state: %w", renameErr),
			)
		}
		klog.Errorf("isolated corrupt BFD recovery state at %s: %v", corruptPath, err)
		return nil
	}
	for key, saved := range state.Episodes {
		episode, ok := s.episodes[key]
		if !ok || saved.Status.Pair != episode.Pair {
			continue
		}
		episode.SessionRecoveryStatus = saved.Status
		episode.failingSince = saved.FailingSince
		episode.stableSince = saved.StableSince
		episode.circuitUntil = saved.CircuitUntil
	}
	s.status.ChildRestarts = state.ChildRestarts
	s.childRecoveryAttempts = state.ChildRecoveryAttempts
	s.childNextRetry = state.ChildNextRetry
	s.childCircuitUntil = state.ChildCircuitUntil
	return nil
}

func (s *Supervisor) persistentStateLocked() persistentState {
	state := persistentState{
		Episodes:              make(map[string]persistentEpisode, len(s.episodes)),
		ChildRestarts:         s.status.ChildRestarts,
		ChildRecoveryAttempts: s.childRecoveryAttempts,
		ChildNextRetry:        s.childNextRetry,
		ChildCircuitUntil:     s.childCircuitUntil,
	}
	for key, episode := range s.episodes {
		state.Episodes[key] = persistentEpisode{
			Status:       episode.SessionRecoveryStatus,
			FailingSince: episode.failingSince,
			StableSince:  episode.stableSince,
			CircuitUntil: episode.circuitUntil,
		}
	}
	return state
}

func (s *Supervisor) saveState(state persistentState) error {
	if s.config.StatePath == "" {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to encode BFD recovery state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.config.StatePath), 0o750); err != nil {
		return fmt.Errorf("failed to create BFD recovery state directory: %w", err)
	}
	if err := fileutil.AtomicWriteFile(s.config.StatePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to persist BFD recovery state: %w", err)
	}
	return nil
}
