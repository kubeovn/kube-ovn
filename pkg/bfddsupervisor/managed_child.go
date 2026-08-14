package bfddsupervisor

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type processLifecycle interface {
	Start() error
	Running() bool
	Restart(context.Context) error
	Stop(context.Context) error
}

type ManagedChild struct {
	process      processLifecycle
	control      ControlAdapter
	config       DaemonConfig
	startTimeout time.Duration
}

func NewManagedChild(process processLifecycle, control ControlAdapter, config DaemonConfig, startTimeout time.Duration) *ManagedChild {
	return &ManagedChild{process: process, control: control, config: config, startTimeout: startTimeout}
}

func (c *ManagedChild) Start(ctx context.Context) error {
	if err := c.process.Start(); err != nil {
		return err
	}
	if err := c.waitAndConfigure(ctx); err != nil {
		return c.stopAfterStartFailure(err)
	}
	return nil
}

func (c *ManagedChild) Running() bool {
	return c.process.Running()
}

func (c *ManagedChild) Restart(ctx context.Context) error {
	if err := c.process.Restart(ctx); err != nil {
		return err
	}
	if err := c.waitAndConfigure(ctx); err != nil {
		return c.stopAfterStartFailure(err)
	}
	return nil
}

func (c *ManagedChild) Stop(ctx context.Context) error {
	return c.process.Stop(ctx)
}

func (c *ManagedChild) waitAndConfigure(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, c.startTimeout)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastStatusErr error
	for {
		_, statusErr := c.control.Status(waitCtx)
		if statusErr == nil {
			configureCtx, configureCancel := context.WithTimeout(ctx, c.startTimeout)
			err := c.control.Configure(configureCtx, c.config)
			configureCancel()
			if err != nil {
				return fmt.Errorf("failed to configure OpenBFDD: %w", err)
			}
			return nil
		}
		lastStatusErr = statusErr
		select {
		case <-waitCtx.Done():
			return errors.Join(
				fmt.Errorf("OpenBFDD control did not become ready: %w", waitCtx.Err()),
				fmt.Errorf("last OpenBFDD control status error: %w", lastStatusErr),
			)
		case <-ticker.C:
		}
	}
}

func (c *ManagedChild) stopAfterStartFailure(startErr error) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), c.startTimeout)
	defer cancel()
	if err := c.process.Stop(stopCtx); err != nil {
		return errors.Join(startErr, fmt.Errorf("failed to stop OpenBFDD after start failure: %w", err))
	}
	return startErr
}
