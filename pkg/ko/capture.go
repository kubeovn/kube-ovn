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
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-nn" || arg == "-n":
			continue
		case arg == "-c" || arg == "-s":
			if i+1 >= len(args) {
				return packetCaptureOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			i++
			if err := setCaptureNumber(&options, arg, args[i]); err != nil {
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
			if i+1 >= len(args) {
				return packetCaptureOptions{}, errors.New("-w requires a path or -")
			}
			i++
			if args[i] != "-" {
				return packetCaptureOptions{}, errors.New("capture only supports -w -; save files locally")
			}
			options.pcap = true
		case arg == "-i":
			if i+1 >= len(args) {
				return packetCaptureOptions{}, errors.New("-i requires an interface name")
			}
			i++
			options.iface = args[i]
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
