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
	"strconv"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/mdlayher/packet"
	"golang.org/x/sys/unix"
)

type captureOptions struct {
	netns   string
	iface   string
	count   int
	snaplen int
	pcap    bool
}

const captureDefaultSnaplen = 262144

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
	defer conn.SetPromiscuous(false)

	var writer *pcapgo.Writer
	if options.pcap {
		writer = pcapgo.NewWriter(stdout)
		if err := writer.WriteFileHeader(uint32(options.snaplen), layers.LinkTypeEthernet); err != nil {
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

func parseCaptureArgs(args []string) (captureOptions, error) {
	options := captureOptions{snaplen: captureDefaultSnaplen}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--netns", "--interface", "--count", "--snaplen":
			if i+1 >= len(args) {
				return captureOptions{}, fmt.Errorf("%s requires a value", args[i])
			}
			value := args[i+1]
			i++
			switch args[i-1] {
			case "--netns":
				options.netns = value
			case "--interface":
				options.iface = value
			case "--count":
				count, err := strconv.Atoi(value)
				if err != nil || count < 1 {
					return captureOptions{}, fmt.Errorf("invalid --count %q", value)
				}
				options.count = count
			case "--snaplen":
				snaplen, err := strconv.Atoi(value)
				if err != nil || snaplen < 64 || snaplen > 1<<20 {
					return captureOptions{}, fmt.Errorf("invalid --snaplen %q", value)
				}
				options.snaplen = snaplen
			}
		case "--pcap":
			options.pcap = true
		default:
			return captureOptions{}, fmt.Errorf("unsupported capture option %q", args[i])
		}
	}
	return options, nil
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
