package kohelper

import (
	"fmt"
	"strconv"
)

type captureOptions struct {
	netns   string
	iface   string
	count   int
	snaplen int
	pcap    bool
	list    bool
}

const captureDefaultSnaplen = 262144

func parseCaptureArgs(args []string) (captureOptions, error) {
	options := captureOptions{snaplen: captureDefaultSnaplen}
	remaining := args
	for len(remaining) != 0 {
		arg := remaining[0]
		remaining = remaining[1:]
		switch arg {
		case "--netns", "--interface", "--count", "--snaplen":
			value, rest, err := ConsumeArgumentValue(arg, remaining)
			if err != nil {
				return captureOptions{}, err
			}
			remaining = rest
			switch arg {
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
		case "--list-interfaces":
			options.list = true
		default:
			return captureOptions{}, fmt.Errorf("unsupported capture option %q", arg)
		}
	}
	return options, nil
}
