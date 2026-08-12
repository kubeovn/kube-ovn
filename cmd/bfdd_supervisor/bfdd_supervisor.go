package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/bfddsupervisor"
)

const (
	defaultSocketPath     = "/var/run/kube-ovn/bfdd-supervisor/control.sock"
	defaultStatePath      = "/var/run/kube-ovn/bfdd-supervisor/state.json"
	defaultMetricsAddress = ":10669"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	defer klog.Flush()
	if err := runCommand(ctx, os.Args[1:], os.Stdout); err != nil {
		klog.Errorf("BFD supervisor failed: %v", err)
		return 1
	}
	return 0
}

func runCommand(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: kube-ovn-bfdd-supervisor <run|live|ready|status>")
	}
	command := args[0]
	if command == "run" {
		return runSupervisor(ctx)
	}
	if command != "live" && command != "ready" && command != "status" {
		return fmt.Errorf("unknown BFD supervisor command %q", command)
	}
	status, err := bfddsupervisor.Probe(ctx, environmentOrDefault("BFDD_SUPERVISOR_SOCKET", defaultSocketPath), command)
	if err != nil {
		return err
	}
	if command == "status" {
		if err := json.NewEncoder(output).Encode(status); err != nil {
			return fmt.Errorf("failed to write BFD supervisor status: %w", err)
		}
	}
	return nil
}

func runSupervisor(ctx context.Context) error {
	config, processConfig, err := configFromEnvironment()
	if err != nil {
		return err
	}
	socketPath := environmentOrDefault("BFDD_SUPERVISOR_SOCKET", defaultSocketPath)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return fmt.Errorf("failed to create BFD supervisor socket directory: %w", err)
	}
	control := bfddsupervisor.NewControlClient(environmentOrDefault("BFDD_CONTROL_PATH", "bfdd-control"), 5*time.Second)
	process := bfddsupervisor.NewProcessController(processConfig)
	child := bfddsupervisor.NewManagedChild(process, control, config.Daemon, 30*time.Second)
	supervisor, err := bfddsupervisor.NewSupervisor(config, control, child, wallClock{})
	if err != nil {
		return err
	}

	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := child.Stop(stopCtx); err != nil {
			klog.Errorf("failed to stop BFD child: %v", err)
		}
	}()
	serverErr := make(chan error, 1)
	go func() { serverErr <- supervisor.Serve(ctx, socketPath) }()
	if err := supervisor.StartChild(ctx); err != nil {
		klog.Errorf("failed initial OpenBFDD start: %v", err)
	}
	if err := supervisor.Reconcile(ctx); err != nil {
		klog.Errorf("failed initial BFD reconciliation: %v", err)
	}
	reporter := bfddsupervisor.NewMetricsReporter()
	reporter.Update(time.Now(), supervisor.Status())
	metricsErr := make(chan error, 1)
	go func() {
		metricsErr <- reporter.Serve(ctx, environmentOrDefault("BFDD_METRICS_ADDRESS", defaultMetricsAddress), supervisor.Status)
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-serverErr:
			if err != nil {
				return fmt.Errorf("BFD supervisor control server failed: %w", err)
			}
			return nil
		case err := <-metricsErr:
			if err != nil {
				return fmt.Errorf("BFD supervisor metrics server failed: %w", err)
			}
			return nil
		case <-ticker.C:
			if err := supervisor.Reconcile(ctx); err != nil {
				klog.Errorf("failed to reconcile BFD sessions: %v", err)
			}
			reporter.Update(time.Now(), supervisor.Status())
		}
	}
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

func configFromEnvironment() (bfddsupervisor.SupervisorConfig, bfddsupervisor.ProcessConfig, error) {
	podIPs, err := parseIPList(os.Getenv("POD_IPS"))
	if err != nil {
		return bfddsupervisor.SupervisorConfig{}, bfddsupervisor.ProcessConfig{}, fmt.Errorf("invalid POD_IPS: %w", err)
	}
	peerIPs, err := parseIPList(os.Getenv("BFD_PEER_IPS"))
	if err != nil {
		return bfddsupervisor.SupervisorConfig{}, bfddsupervisor.ProcessConfig{}, fmt.Errorf("invalid BFD_PEER_IPS: %w", err)
	}
	minTX, err := positiveEnvironmentInt("BFD_MIN_TX", 1000)
	if err != nil {
		return bfddsupervisor.SupervisorConfig{}, bfddsupervisor.ProcessConfig{}, err
	}
	minRX, err := positiveEnvironmentInt("BFD_MIN_RX", 1000)
	if err != nil {
		return bfddsupervisor.SupervisorConfig{}, bfddsupervisor.ProcessConfig{}, err
	}
	multiplier, err := positiveEnvironmentInt("BFD_MULTI", 3)
	if err != nil || multiplier > 255 {
		return bfddsupervisor.SupervisorConfig{}, bfddsupervisor.ProcessConfig{}, fmt.Errorf("invalid BFD_MULTI %q", os.Getenv("BFD_MULTI"))
	}

	detectionTime := time.Duration(multiplier*max(minTX, minRX)) * time.Millisecond
	gracePeriod := max(time.Minute, 10*detectionTime)
	stablePeriod := max(time.Minute, 2*detectionTime)
	daemon := bfddsupervisor.DaemonConfig{
		MinTXMilliseconds: minTX,
		MinRXMilliseconds: minRX,
		Multiplier:        multiplier,
		PeerIPs:           peerIPs,
	}
	config := bfddsupervisor.SupervisorConfig{
		Daemon:        daemon,
		PodIPs:        podIPs,
		GracePeriod:   gracePeriod,
		StablePeriod:  stablePeriod,
		Backoffs:      []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute},
		ChildBackoffs: []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second},
		CircuitOpen:   time.Hour,
		StatePath:     environmentOrDefault("BFDD_SUPERVISOR_STATE", defaultStatePath),
	}
	args := []string{"--nofork", "--tee"}
	for _, ip := range podIPs {
		args = append(args, "--listen="+ip)
	}
	process := bfddsupervisor.ProcessConfig{
		Path:        environmentOrDefault("BFDD_BEACON_PATH", "bfdd-beacon"),
		Args:        args,
		StopTimeout: 5 * time.Second,
	}
	return config, process, nil
}

func parseIPList(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	addresses := make([]string, 0, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, address.String())
	}
	return addresses, nil
}

func positiveEnvironmentInt(name string, defaultValue int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return parsed, nil
}

func environmentOrDefault(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}
