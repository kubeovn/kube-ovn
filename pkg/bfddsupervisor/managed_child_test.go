package bfddsupervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeProcessLifecycle struct {
	running   bool
	stopCalls int
}

func (p *fakeProcessLifecycle) Start() error {
	p.running = true
	return nil
}

func (p *fakeProcessLifecycle) Running() bool { return p.running }

func (p *fakeProcessLifecycle) Restart(context.Context) error {
	p.running = true
	return nil
}

func (p *fakeProcessLifecycle) Stop(context.Context) error {
	p.running = false
	p.stopCalls++
	return nil
}

type configuringControl struct {
	configured   bool
	configureErr error
	statusErr    error
}

func (c *configuringControl) Status(context.Context) ([]Session, error) { return nil, c.statusErr }
func (c *configuringControl) Reset(context.Context, SessionPair) error  { return nil }
func (c *configuringControl) Configure(context.Context, DaemonConfig) error {
	c.configured = true
	return c.configureErr
}

func TestManagedChildReplaysConfigurationAfterRestart(t *testing.T) {
	process := &fakeProcessLifecycle{running: true}
	control := &configuringControl{}
	child := NewManagedChild(process, control, DaemonConfig{
		MinTXMilliseconds: 100,
		MinRXMilliseconds: 100,
		Multiplier:        3,
		PeerIPs:           []string{"10.255.255.255"},
	}, time.Second)

	require.NoError(t, child.Restart(context.Background()))
	require.True(t, child.Running())
	require.True(t, control.configured)
}

func TestManagedChildStopsProcessWhenInitialConfigurationFails(t *testing.T) {
	process := &fakeProcessLifecycle{}
	control := &configuringControl{configureErr: errors.New("configuration rejected")}
	child := NewManagedChild(process, control, DaemonConfig{
		MinTXMilliseconds: 100,
		MinRXMilliseconds: 100,
		Multiplier:        3,
		PeerIPs:           []string{"10.255.255.255"},
	}, time.Second)

	err := child.Start(context.Background())
	require.ErrorContains(t, err, "configuration rejected")
	require.False(t, child.Running())
	require.Equal(t, 1, process.stopCalls)
}

func TestManagedChildReturnsLastStatusErrorAtStartupTimeout(t *testing.T) {
	process := &fakeProcessLifecycle{}
	control := &configuringControl{statusErr: errors.New("malformed control response")}
	child := NewManagedChild(process, control, DaemonConfig{}, 10*time.Millisecond)

	err := child.Start(context.Background())
	require.ErrorContains(t, err, "OpenBFDD control did not become ready")
	require.ErrorContains(t, err, "malformed control response")
	require.False(t, child.Running())
	require.Equal(t, 1, process.stopCalls)
}
