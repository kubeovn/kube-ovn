//go:build linux

package kohelper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RunEnvironment performs the node checks that used to be delegated to
// env-check.sh, netstat, ps and nmap. It deliberately reports warnings and
// keeps the command successful so one unavailable host counter does not hide
// the remaining diagnostics.
func RunEnvironment(ctx context.Context, stdout, stderr io.Writer) error {
	checks := []struct {
		name string
		fn   func(context.Context, io.Writer, io.Writer) error
	}{
		{"1) check cni configuration", checkCNIConfiguration},
		{"2) check system ipv4 config", checkSystemIPv4},
		{"3) check checksum value", checkChecksumCounters},
		{"4) check dns config", checkDNSConfiguration},
		{"5) check firewall config", checkFirewallProcesses},
		{"6) check geneve 6081 connection", checkGeneveUDP},
	}
	for _, check := range checks {
		if err := writeEnvironmentf(stdout, "%s\n", check.name); err != nil {
			return err
		}
		if err := check.fn(ctx, stdout, stderr); err != nil {
			if isEnvironmentWriteError(err) {
				return err
			}
			if _, writeErr := fmt.Fprintf(stderr, "environment check failed: %v\n", err); writeErr != nil {
				return writeErr
			}
		}
	}
	return nil
}

type environmentWriteError struct{ err error }

func (e *environmentWriteError) Error() string { return e.err.Error() }
func (e *environmentWriteError) Unwrap() error { return e.err }

func writeEnvironmentf(stdout io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(stdout, format, args...); err != nil {
		return &environmentWriteError{err: err}
	}
	return nil
}

func isEnvironmentWriteError(err error) bool {
	_, ok := errors.AsType[*environmentWriteError](err)
	return ok
}

func checkCNIConfiguration(_ context.Context, stdout, _ io.Writer) error {
	if _, err := os.Stat("/etc/cni/net.d"); err != nil {
		return fmt.Errorf("CNI config directory: %w", err)
	}
	entries, err := os.ReadDir("/etc/cni/net.d")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), "kube-ovn.conflist") {
			if err := writeEnvironmentf(stdout, "Check CNI config file %s; remove it if it is not required\n", entry.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSystemIPv4(_ context.Context, stdout, _ io.Writer) error {
	mtu, err := os.ReadFile("/proc/sys/net/ipv4/tcp_mtu_probing")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(mtu)) == "0" {
		if err := writeEnvironmentf(stdout, "tcp_mtu_probing is 0; verify that this is intended\n"); err != nil {
			return err
		}
	}
	recycle, err := os.ReadFile("/proc/sys/net/ipv4/tcp_tw_recycle")
	switch {
	case err == nil && strings.TrimSpace(string(recycle)) == "1":
		if err := writeEnvironmentf(stdout, "tcp_tw_recycle is enabled and may affect NodePort traffic\n"); err != nil {
			return err
		}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read tcp_tw_recycle: %w", err)
	}
	return nil
}

func checkChecksumCounters(_ context.Context, stdout, _ io.Writer) (resultErr error) {
	file, err := os.Open("/proc/net/snmp")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	return reportChecksumCounters(file, stdout)
}

func reportChecksumCounters(input io.Reader, stdout io.Writer) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		headers := strings.Fields(scanner.Text())
		if len(headers) == 0 || !slices.Contains([]string{"Tcp:", "Udp:"}, headers[0]) {
			continue
		}
		index := slices.Index(headers, "InCsumErrors")
		if !scanner.Scan() {
			return errors.Join(scanner.Err(), errors.New("missing protocol counters in /proc/net/snmp"))
		}
		if index == -1 {
			continue
		}
		values := strings.Fields(scanner.Text())
		if index >= len(values) || values[0] != headers[0] {
			return errors.New("invalid protocol counters in /proc/net/snmp")
		}
		value, err := strconv.ParseUint(values[index], 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s InCsumErrors: %w", headers[0], err)
		}
		if value > 0 {
			if err := writeEnvironmentf(stdout, "%s InCsumErrors=%d; watch whether the counter increases\n", headers[0], value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func checkDNSConfiguration(_ context.Context, stdout, _ io.Writer) error {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return err
	}
	if strings.Contains(string(data), ".com") {
		if err := writeEnvironmentf(stdout, "DNS search configuration contains .com; verify /etc/resolv.conf\n"); err != nil {
			return err
		}
	}
	return nil
}

func checkFirewallProcesses(_ context.Context, stdout, _ io.Writer) error {
	wanted := []string{"firewall", "security", "qax", "safe", "defence", "vmsec"}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "1" || !isPID(entry.Name()) {
			continue
		}
		name, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read process %s name: %w", entry.Name(), err)
		}
		process := strings.ToLower(strings.TrimSpace(string(name)))
		if slices.ContainsFunc(wanted, func(value string) bool { return strings.Contains(process, value) }) {
			if err := writeEnvironmentf(stdout, "Found process %q; verify it does not affect traffic\n", process); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkGeneveUDP(ctx context.Context, stdout, _ io.Writer) error {
	state, err := probeUDP(ctx, "127.0.0.1:6081")
	if err != nil {
		return err
	}
	if state != "open" {
		if err := writeEnvironmentf(stdout, "Geneve UDP/6081 probe is %s; a UDP probe cannot prove service readiness\n", state); err != nil {
			return err
		}
	}
	return nil
}

func probeUDP(ctx context.Context, address string) (state string, resultErr error) {
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "closed", nil
		}
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		return "unknown", fmt.Errorf("dial UDP probe: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		return "unknown", fmt.Errorf("set UDP probe deadline: %w", err)
	}
	if _, err := conn.Write([]byte{0}); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "closed", nil
		}
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		return "unknown", fmt.Errorf("write UDP probe: %w", err)
	}
	var response [1]byte
	if _, err := conn.Read(response[:]); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "closed", nil
		}
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return "unknown", nil
		}
		return "unknown", fmt.Errorf("read UDP probe: %w", err)
	}
	return "open", nil
}

func isPID(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}
