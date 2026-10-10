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
	if options.netns == "" || options.iface == "" {
		return errors.New("capture requires --netns and --interface")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := enterNetworkNamespace(options.netns); err != nil {
		return err
	}
	iface, err := net.InterfaceByName(options.iface)
	if err != nil {
		return fmt.Errorf("find capture interface %q: %w", options.iface, err)
	}
	conn, err := packet.Listen(iface, packet.Raw, unix.ETH_P_ALL, nil)
	if err != nil {
		return fmt.Errorf("open packet socket on %s: %w", options.iface, err)
	}
	defer conn.Close()
	if err := conn.SetPromiscuous(true); err != nil {
		return fmt.Errorf("enable promiscuous capture on %s: %w", options.iface, err)
	}
	defer func() { _ = conn.SetPromiscuous(false) }()

	var writer *pcapgo.Writer
	if options.pcap {
		writer = pcapgo.NewWriter(stdout)
		if err := writer.WriteFileHeader(uint32(options.snaplen), layers.LinkTypeEthernet); err != nil { // #nosec G115 -- parseCaptureArgs bounds snaplen to 1 MiB.
			return fmt.Errorf("write pcap header: %w", err)
		}
	}

	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	buffer := make([]byte, options.snaplen)
	packets := 0
	for options.count == 0 || packets < options.count {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return fmt.Errorf("set capture deadline: %w", err)
		}
		length, _, err := conn.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return fmt.Errorf("read packet: %w", err)
		}
		capture := buffer[:length]
		info := gopacket.CaptureInfo{Timestamp: time.Now(), CaptureLength: length, Length: length}
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

func enterNetworkNamespace(path string) error {
	if !strings.HasPrefix(path, "/proc/") && !strings.HasPrefix(path, "/var/run/netns/") {
		return fmt.Errorf("network namespace path %q is outside the agent mounts", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open network namespace %q: %w", path, err)
	}
	defer unix.Close(fd)
	if err := unix.Setns(fd, unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("enter network namespace %q: %w", path, err)
	}
	return nil
}
