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
		_, _ = fmt.Fprintln(stdout, check.name)
		if err := check.fn(ctx, stdout, stderr); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "environment check failed: %v\n", err); writeErr != nil {
				return writeErr
			}
		}
	}
	return nil
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
			_, _ = fmt.Fprintf(stdout, "Check CNI config file %s; remove it if it is not required\n", entry.Name())
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
		_, _ = fmt.Fprintln(stdout, "tcp_mtu_probing is 0; verify that this is intended")
	}
	if recycle, err := os.ReadFile("/proc/sys/net/ipv4/tcp_tw_recycle"); err == nil && strings.TrimSpace(string(recycle)) == "1" {
		_, _ = fmt.Fprintln(stdout, "tcp_tw_recycle is enabled and may affect NodePort traffic")
	}
	return nil
}

func checkChecksumCounters(_ context.Context, stdout, _ io.Writer) error {
	file, err := os.Open("/proc/net/snmp")
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Tcp:") && strings.Contains(line, "InCsumErrors") {
			_, _ = fmt.Fprintln(stdout, "Found InCsumErrors in /proc/net/snmp; watch whether the counter increases")
			break
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
		_, _ = fmt.Fprintln(stdout, "DNS search configuration contains .com; verify /etc/resolv.conf")
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
			continue
		}
		process := strings.ToLower(strings.TrimSpace(string(name)))
		if slices.ContainsFunc(wanted, func(value string) bool { return strings.Contains(process, value) }) {
			_, _ = fmt.Fprintf(stdout, "Found process %q; verify it does not affect traffic\n", process)
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
		_, _ = fmt.Fprintf(stdout, "Geneve UDP/6081 probe is %s; a UDP probe cannot prove service readiness\n", state)
	}
	return nil
}

func probeUDP(ctx context.Context, address string) (string, error) {
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "closed", nil
		}
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		return "unknown", nil
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return "unknown", nil
	}
	if _, err := conn.Write([]byte{0}); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "closed", nil
		}
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		return "unknown", nil
	}
	var response [1]byte
	if _, err := conn.Read(response[:]); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return "closed", nil
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return "unknown", nil
		}
		if ctx.Err() != nil {
			return "unknown", ctx.Err()
		}
		return "unknown", nil
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
