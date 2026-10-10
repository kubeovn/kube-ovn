package ko

import (
	"errors"
	"fmt"
	"strconv"
)

const defaultCaptureSnaplen = 262144

type packetCaptureOptions struct {
	iface   string
	count   int
	snaplen int
	pcap    bool
}

// parseCaptureArguments accepts the stable subset of tcpdump flags that the
// native node-agent capture implementation can provide without shipping a
// packet-debugger binary in the Kube-OVN image.
func parseCaptureArguments(args []string) (packetCaptureOptions, error) {
	options := packetCaptureOptions{snaplen: defaultCaptureSnaplen}
	remaining := args
	for len(remaining) != 0 {
		arg := remaining[0]
		remaining = remaining[1:]
		switch {
		case arg == "-nn" || arg == "-n":
			continue
		case arg == "-c" || arg == "-s":
			value, rest, err := captureArgumentValue(arg, remaining)
			if err != nil {
				return packetCaptureOptions{}, err
			}
			remaining = rest
			if err := setCaptureNumber(&options, arg, value); err != nil {
				return packetCaptureOptions{}, err
			}
		case len(arg) > 2 && arg[:2] == "-c":
			if err := setCaptureNumber(&options, "-c", arg[2:]); err != nil {
				return packetCaptureOptions{}, err
			}
		case len(arg) > 2 && arg[:2] == "-s":
			if err := setCaptureNumber(&options, "-s", arg[2:]); err != nil {
				return packetCaptureOptions{}, err
			}
		case arg == "-w":
			value, rest, err := captureArgumentValue(arg, remaining)
			if err != nil {
				return packetCaptureOptions{}, err
			}
			remaining = rest
			if value != "-" {
				return packetCaptureOptions{}, errors.New("capture only supports -w -; save files locally")
			}
			options.pcap = true
		case arg == "-i":
			value, rest, err := captureArgumentValue(arg, remaining)
			if err != nil {
				return packetCaptureOptions{}, err
			}
			remaining = rest
			options.iface = value
		case len(arg) > 2 && arg[:2] == "-i":
			options.iface = arg[2:]
			if options.iface == "" {
				return packetCaptureOptions{}, errors.New("-i requires an interface name")
			}
		default:
			return packetCaptureOptions{}, fmt.Errorf("unsupported capture argument %q; use -c, -s, -nn, -i, or -w -", arg)
		}
	}
	return options, nil
}

func captureArgumentValue(flag string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("%s requires a value", flag)
	}
	return args[0], args[1:], nil
}

func setCaptureNumber(options *packetCaptureOptions, flag, value string) error {
	number, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid %s value %q", flag, value)
	}
	if flag == "-c" {
		if number < 1 {
			return errors.New("-c must be greater than zero")
		}
		options.count = number
		return nil
	}
	if number < 64 || number > 1<<20 {
		return errors.New("-s must be between 64 and 1048576")
	}
	options.snaplen = number
	return nil
}
