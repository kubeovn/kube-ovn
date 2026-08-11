package bfddsupervisor

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type SessionState string

const (
	SessionUp        SessionState = "Up"
	SessionDown      SessionState = "Down"
	SessionInit      SessionState = "Init"
	SessionAdminDown SessionState = "AdminDown"
)

type Session struct {
	ID     uint32
	Local  string
	Remote string
	State  SessionState
}

type SessionPair struct {
	Local  string
	Remote string
}

type ControlClient struct {
	path    string
	timeout time.Duration
}

type DaemonConfig struct {
	MinTXMilliseconds int
	MinRXMilliseconds int
	Multiplier        int
	PeerIPs           []string
}

func NewControlClient(path string, timeout time.Duration) *ControlClient {
	return &ControlClient{path: path, timeout: timeout}
}

func (c *ControlClient) Status(ctx context.Context) ([]Session, error) {
	output, err := c.run(ctx, "status")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "There are ") || !strings.HasSuffix(lines[0], " sessions:") {
		return nil, fmt.Errorf("unexpected bfdd status response %q", output)
	}
	countText := strings.TrimSuffix(strings.TrimPrefix(lines[0], "There are "), " sessions:")
	expected, err := strconv.Atoi(countText)
	if err != nil || expected < 0 {
		return nil, fmt.Errorf("invalid bfdd session count %q", countText)
	}

	sessions := make([]Session, 0, expected)
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Session ") {
			continue
		}
		var idText, local, remote string
		var state SessionState
		for field := range strings.FieldsSeq(line) {
			switch {
			case strings.HasPrefix(field, "id="):
				idText = strings.TrimPrefix(field, "id=")
			case strings.HasPrefix(field, "local="):
				local = strings.TrimPrefix(field, "local=")
			case strings.HasPrefix(field, "remote="):
				remote = strings.TrimPrefix(field, "remote=")
			case strings.HasPrefix(field, "state="):
				state = SessionState(strings.TrimPrefix(field, "state="))
			}
		}
		localAddr, localErr := netip.ParseAddr(local)
		remoteAddr, remoteErr := netip.ParseAddr(remote)
		id, idErr := strconv.ParseUint(idText, 10, 32)
		if idErr != nil || localErr != nil || remoteErr != nil || state == "" {
			return nil, fmt.Errorf("invalid bfdd session status line %q", line)
		}
		switch state {
		case SessionUp, SessionDown, SessionInit, SessionAdminDown:
		default:
			return nil, fmt.Errorf("invalid BFD session state %q", state)
		}
		sessions = append(sessions, Session{ID: uint32(id), Local: localAddr.String(), Remote: remoteAddr.String(), State: state})
	}
	if len(sessions) != expected {
		return nil, fmt.Errorf("bfdd reported %d sessions but returned %d", expected, len(sessions))
	}
	return sessions, nil
}

func (c *ControlClient) Reset(ctx context.Context, pair SessionPair) error {
	normalized, err := normalizePair(pair)
	if err != nil {
		return err
	}

	before, err := c.Status(ctx)
	if err != nil {
		return err
	}
	var previousID uint32
	for _, session := range before {
		if session.Local == normalized.Local && session.Remote == normalized.Remote {
			previousID = session.ID
			break
		}
	}
	if previousID == 0 {
		return fmt.Errorf("BFD session local=%s remote=%s does not exist", normalized.Local, normalized.Remote)
	}

	response, err := c.run(ctx, "session", "local", normalized.Local, "remote", normalized.Remote, "reset")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(response, "Attempting to reset session(s).") {
		return fmt.Errorf("unexpected bfdd reset response %q", response)
	}

	after, err := c.Status(ctx)
	if err != nil {
		return err
	}
	for _, session := range after {
		if session.Local == normalized.Local && session.Remote == normalized.Remote && session.ID == previousID {
			return fmt.Errorf("BFD session local=%s remote=%s did not reset", normalized.Local, normalized.Remote)
		}
	}
	return nil
}

func normalizePair(pair SessionPair) (SessionPair, error) {
	local, err := netip.ParseAddr(pair.Local)
	if err != nil {
		return SessionPair{}, fmt.Errorf("invalid local BFD IP %q: %w", pair.Local, err)
	}
	remote, err := netip.ParseAddr(pair.Remote)
	if err != nil {
		return SessionPair{}, fmt.Errorf("invalid remote BFD IP %q: %w", pair.Remote, err)
	}
	if local.Is4() != remote.Is4() {
		return SessionPair{}, fmt.Errorf("BFD address family mismatch local=%s remote=%s", local, remote)
	}
	return SessionPair{Local: local.String(), Remote: remote.String()}, nil
}

func (c *ControlClient) Configure(ctx context.Context, config DaemonConfig) error {
	if config.MinTXMilliseconds <= 0 || config.MinRXMilliseconds <= 0 {
		return fmt.Errorf("invalid BFD intervals mintx=%d minrx=%d", config.MinTXMilliseconds, config.MinRXMilliseconds)
	}
	if config.Multiplier < 1 || config.Multiplier > 255 {
		return fmt.Errorf("invalid BFD multiplier %d", config.Multiplier)
	}

	commands := []struct {
		args       []string
		wantPrefix string
	}{
		{[]string{"session", "new", "set", "mintx", strconv.Itoa(config.MinTXMilliseconds), "ms"}, "Attempting to set mintx to "},
		{[]string{"session", "new", "set", "minrx", strconv.Itoa(config.MinRXMilliseconds), "ms"}, "Attempting to set minrx to "},
		{[]string{"session", "new", "set", "multi", strconv.Itoa(config.Multiplier)}, "Attempting to set multi to "},
	}
	for _, peer := range config.PeerIPs {
		address, err := netip.ParseAddr(peer)
		if err != nil {
			return fmt.Errorf("invalid BFD peer IP %q: %w", peer, err)
		}
		commands = append(commands, struct {
			args       []string
			wantPrefix string
		}{[]string{"allow", address.String()}, "Allowing connections from " + address.String()})
	}
	commands = append(commands, struct {
		args       []string
		wantPrefix string
	}{[]string{"log", "type", "command", "no"}, "Log type command set to no"})

	for _, command := range commands {
		response, err := c.run(ctx, command.args...)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(response, command.wantPrefix) {
			return fmt.Errorf("unexpected bfdd-control %q response %q", strings.Join(command.args, " "), response)
		}
	}
	return nil
}

func (c *ControlClient) run(ctx context.Context, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := exec.CommandContext(commandCtx, c.path, args...).CombinedOutput() // #nosec G204 -- command path is fixed by supervisor configuration
	response := strings.TrimSpace(string(output))
	if commandCtx.Err() != nil {
		return response, fmt.Errorf("bfdd-control %q timed out: %w", strings.Join(args, " "), commandCtx.Err())
	}
	if err != nil {
		return response, fmt.Errorf("bfdd-control %q failed: %w: %s", strings.Join(args, " "), err, response)
	}
	for line := range strings.SplitSeq(response, "\n") {
		for _, prefix := range []string{"Unable to complete ", "Unknown command ", "No session ", "Failed to ", "Must "} {
			if strings.HasPrefix(line, prefix) {
				return response, fmt.Errorf("bfdd-control %q rejected: %s", strings.Join(args, " "), response)
			}
		}
	}
	return response, nil
}
