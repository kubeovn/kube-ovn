package bfddsupervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

const probeIOTimeout = 5 * time.Second

type probeResponse struct {
	Status SupervisorStatus `json:"status"`
	Error  string           `json:"error,omitempty"`
}

func (s *Supervisor) Serve(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		return errors.New("supervisor socket path is empty")
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale supervisor socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on supervisor socket: %w", err)
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			klog.Errorf("failed to close BFD supervisor listener: %v", err)
		}
	}()
	defer func() {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			klog.Errorf("failed to remove BFD supervisor socket: %v", err)
		}
	}()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("failed to set supervisor socket permissions: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			klog.Errorf("failed to close BFD supervisor listener: %v", err)
		}
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("failed to accept supervisor connection: %w", err)
		}
		go s.handleConnection(connection)
	}
}

func (s *Supervisor) handleConnection(connection net.Conn) {
	defer func() {
		if err := connection.Close(); err != nil {
			klog.Errorf("failed to close BFD supervisor connection: %v", err)
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(probeIOTimeout)); err != nil {
		klog.Errorf("failed to set BFD supervisor connection deadline: %v", err)
		return
	}
	scanner := bufio.NewScanner(connection)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			klog.Errorf("failed to read BFD supervisor request: %v", err)
		}
		return
	}
	request := strings.TrimSpace(scanner.Text())
	status := s.Status()
	response := probeResponse{Status: status}
	switch request {
	case "live":
		if !status.Live {
			response.Error = "BFD supervisor is not live"
		}
	case "ready":
		if !status.Ready {
			response.Error = "expected BFD sessions are not ready"
		}
	case "status":
	default:
		response.Error = fmt.Sprintf("unknown supervisor request %q", request)
	}
	if err := json.NewEncoder(connection).Encode(response); err != nil {
		klog.Errorf("failed to write BFD supervisor response: %v", err)
	}
}

func Probe(ctx context.Context, socketPath, request string) (SupervisorStatus, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return SupervisorStatus{}, fmt.Errorf("failed to connect to BFD supervisor: %w", err)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			klog.Errorf("failed to close BFD supervisor probe connection: %v", err)
		}
	}()
	deadline := time.Now().Add(probeIOTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return SupervisorStatus{}, fmt.Errorf("failed to set BFD supervisor probe deadline: %w", err)
	}
	if _, err := fmt.Fprintln(connection, request); err != nil {
		return SupervisorStatus{}, fmt.Errorf("failed to send BFD supervisor request: %w", err)
	}
	var response probeResponse
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return SupervisorStatus{}, fmt.Errorf("failed to decode BFD supervisor response: %w", err)
	}
	if response.Error != "" {
		return response.Status, errors.New(response.Error)
	}
	return response.Status, nil
}
