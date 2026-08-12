package bfddsupervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"k8s.io/klog/v2"
)

type ProcessConfig struct {
	Path        string
	Args        []string
	StopTimeout time.Duration
	Stdout      io.Writer
	Stderr      io.Writer
}

type ProcessController struct {
	config ProcessConfig

	mutex    sync.RWMutex
	cmd      *exec.Cmd
	done     chan error
	stopping *exec.Cmd
}

func NewProcessController(config ProcessConfig) *ProcessController {
	if config.StopTimeout <= 0 {
		config.StopTimeout = 5 * time.Second
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	return &ProcessController{config: config}
}

func (c *ProcessController) Start() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.cmd != nil {
		return errors.New("child process is already running")
	}
	if c.config.Path == "" {
		return errors.New("child process path is empty")
	}

	cmd := exec.Command(c.config.Path, c.config.Args...) // #nosec G204 -- child path and arguments come from fixed supervisor configuration
	cmd.Stdout = c.config.Stdout
	cmd.Stderr = c.config.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start child process: %w", err)
	}
	done := make(chan error, 1)
	c.cmd = cmd
	c.done = done
	go c.wait(cmd, done)
	return nil
}

func (c *ProcessController) Running() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.cmd != nil
}

func (c *ProcessController) Restart(ctx context.Context) error {
	if err := c.Stop(ctx); err != nil {
		return err
	}
	return c.Start()
}

func (c *ProcessController) Stop(ctx context.Context) error {
	c.mutex.Lock()
	cmd, done := c.cmd, c.done
	if cmd != nil {
		c.stopping = cmd
	}
	c.mutex.Unlock()
	if cmd == nil {
		return nil
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("failed to terminate child process group: %w", err)
	}
	timer := time.NewTimer(c.config.StopTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return unexpectedExitError(err, syscall.SIGTERM)
	case <-ctx.Done():
	case <-timer.C:
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("failed to kill child process group: %w", err)
	}
	select {
	case err := <-done:
		return unexpectedExitError(err, syscall.SIGKILL)
	case <-ctx.Done():
		return fmt.Errorf("waiting for child process exit: %w", ctx.Err())
	case <-time.After(time.Second):
		return errors.New("timed out waiting for killed child process")
	}
}

func unexpectedExitError(err error, expectedSignal syscall.Signal) error {
	if err == nil {
		return nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() && status.Signal() == expectedSignal {
			return nil
		}
	}
	return fmt.Errorf("child process exited unexpectedly: %w", err)
}

func (c *ProcessController) wait(cmd *exec.Cmd, done chan error) {
	err := cmd.Wait()
	c.mutex.Lock()
	expectedStop := c.stopping == cmd
	if c.cmd == cmd {
		c.cmd = nil
		c.done = nil
	}
	if expectedStop {
		c.stopping = nil
	}
	c.mutex.Unlock()
	if !expectedStop {
		if err != nil {
			klog.Errorf("OpenBFDD child exited unexpectedly: %v", err)
		} else {
			klog.Error("OpenBFDD child exited unexpectedly without an error")
		}
	}

	done <- err
	close(done)
}
