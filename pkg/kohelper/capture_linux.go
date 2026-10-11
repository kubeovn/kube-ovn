//go:build linux

package kohelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/mdlayher/packet"
	"golang.org/x/sys/unix"
)

func RunCapture(ctx context.Context, args []string, stdout, _ io.Writer) error {
	options, err := parseCaptureArgs(args)
	if err != nil {
		return err
	}
	if options.netns == "" || (!options.list && options.iface == "") {
		return errors.New("capture requires --netns and --interface, except when listing interfaces")
	}
	result := make(chan error, 1)
	go func() {
		result <- captureInNamespace(ctx, options, stdout)
	}()
	return <-result
}

// Run on a dedicated goroutine so a failed namespace restoration retires the
// still-locked OS thread instead of returning it to the Go scheduler.
func captureInNamespace(ctx context.Context, options captureOptions, stdout io.Writer) (resultErr error) {
	runtime.LockOSThread()
	restored := true
	defer func() {
		if restored {
			runtime.UnlockOSThread()
		}
	}()
	originalNS, err := unix.Open("/proc/thread-self/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open current network namespace: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Close(originalNS)) }()
	if err := enterNetworkNamespace(options.netns); err != nil {
		return err
	}
	defer func() {
		if err := unix.Setns(originalNS, unix.CLONE_NEWNET); err != nil {
			restored = false
			resultErr = errors.Join(resultErr, fmt.Errorf("restore current network namespace: %w", err))
		}
	}()
	if options.list {
		return listCaptureInterfaces(stdout)
	}
	return capturePackets(ctx, options, stdout)
}

func listCaptureInterfaces(stdout io.Writer) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("list capture interfaces: %w", err)
	}
	for index, iface := range interfaces {
		if _, err := fmt.Fprintf(stdout, "%d.%s\n", index+1, iface.Name); err != nil {
			return err
		}
	}
	return nil
}

func capturePackets(ctx context.Context, options captureOptions, stdout io.Writer) (resultErr error) {
	iface, err := net.InterfaceByName(options.iface)
	if err != nil {
		return fmt.Errorf("find capture interface %q: %w", options.iface, err)
	}
	if options.pcap && len(iface.HardwareAddr) != 6 {
		return fmt.Errorf("pcap output requires an Ethernet interface; %q has hardware address length %d", iface.Name, len(iface.HardwareAddr))
	}
	conn, err := packet.Listen(iface, packet.Raw, unix.ETH_P_ALL, nil)
	if err != nil {
		return fmt.Errorf("open packet socket on %s: %w", options.iface, err)
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	if err := conn.SetPromiscuous(true); err != nil {
		return fmt.Errorf("enable promiscuous capture on %s: %w", options.iface, err)
	}
	defer func() { resultErr = errors.Join(resultErr, conn.SetPromiscuous(false)) }()

	var writer *pcapgo.Writer
	if options.pcap {
		writer = pcapgo.NewWriter(stdout)
		if err := writer.WriteFileHeader(uint32(options.snaplen), layers.LinkTypeEthernet); err != nil { // #nosec G115 -- parseCaptureArgs bounds snaplen to 1 MiB.
			return fmt.Errorf("write pcap header: %w", err)
		}
	}

	// Closing wakes a blocked read; the cleanup defer registered above reports close errors.
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	raw, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("access capture socket: %w", err)
	}
	buffer := make([]byte, options.snaplen)
	packets := 0
	for options.count == 0 || packets < options.count {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return fmt.Errorf("set capture deadline: %w", err)
		}
		length, err := readCapturePacket(raw, buffer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return fmt.Errorf("read packet: %w", err)
		}
		captureLength := min(length, options.snaplen)
		capture := buffer[:captureLength]
		info := gopacket.CaptureInfo{Timestamp: time.Now(), CaptureLength: captureLength, Length: length}
		if writer != nil {
			if err := writer.WritePacket(info, capture); err != nil {
				return fmt.Errorf("write pcap packet: %w", err)
			}
		} else if _, err := fmt.Fprintf(stdout, "%s packet=%d length=%d interface=%s\n", info.Timestamp.UTC().Format(time.RFC3339Nano), packets+1, length, options.iface); err != nil {
			return err
		}
		packets++
	}
	if writer != nil {
		if flusher, ok := stdout.(interface{ Flush() error }); ok {
			return flusher.Flush()
		}
	}
	return nil
}

// MSG_TRUNC reports the original frame size even when snaplen truncates data.
// RawConn.Read preserves the packet socket's poller deadlines and cancellation.
func readCapturePacket(raw syscall.RawConn, buffer []byte) (int, error) {
	var length int
	var readErr error
	if err := raw.Read(func(fd uintptr) bool {
		length, _, readErr = unix.Recvfrom(int(fd), buffer, unix.MSG_TRUNC|unix.MSG_DONTWAIT)
		return !errors.Is(readErr, unix.EAGAIN) && !errors.Is(readErr, unix.EWOULDBLOCK)
	}); err != nil {
		return 0, err
	}
	return length, readErr
}

func enterNetworkNamespace(path string) (resultErr error) {
	if !strings.HasPrefix(path, "/proc/") && !strings.HasPrefix(path, "/var/run/netns/") {
		return fmt.Errorf("network namespace path %q is outside the agent mounts", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open network namespace %q: %w", path, err)
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Close(fd)) }()
	if err := unix.Setns(fd, unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("enter network namespace %q: %w", path, err)
	}
	return nil
}
