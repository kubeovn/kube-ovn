// Package kubectlko adapts legacy E2E fixtures to the current command interface.
package kubectlko

import (
	"net"
	"strings"
)

// Args preserves argument boundaries while translating a legacy test invocation.
func Args(args ...string) []string {
	if len(args) < 2 || args[0] != "ko" {
		return args
	}
	command, rest := args[1], args[2:]
	switch command {
	case "nbctl", "sbctl", "icnbctl", "icsbctl":
		tool := strings.Replace(command, "ic", "ic-", 1)
		return append([]string{"ko", "exec", tool, "--"}, rest...)
	case "vsctl", "ofctl", "dpctl", "appctl":
		return append([]string{"ko", "exec", command, "--node", rest[0], "--"}, rest[1:]...)
	case "nb", "sb":
		if rest[0] == "dbstatus" {
			return []string{"ko", "db", "health"}
		}
		return append([]string{"ko", "db", command}, rest...)
	case "tcpdump":
		return append([]string{"ko", "capture", "--pod", rest[0], "--"}, rest[1:]...)
	case "trace", "ovn-trace":
		return traceFixtureArgs(command, rest)
	case "log":
		return []string{"ko", "logs", "--component", rest[0]}
	case "diagnose":
		if len(rest) == 0 || rest[0] == "all" {
			return []string{"ko", "diagnose", "cluster"}
		}
		if rest[0] == "IPPorts" {
			result := []string{"ko", "diagnose", "connectivity"}
			for target := range strings.SplitSeq(rest[1], ",") {
				protocol, endpoint, _ := strings.Cut(target, "-")
				ip, port, _ := strings.CutLast(endpoint, "-")
				result = append(result, "--target", protocol+"://"+net.JoinHostPort(ip, port))
			}
			return result
		}
	case "acl-sample":
		return append([]string{"ko", "acl"}, rest...)
	}
	return args
}

func traceFixtureArgs(command string, args []string) []string {
	result := []string{"ko", "trace"}
	if node, ok := strings.CutPrefix(args[0], "node//"); ok {
		result = append(result, "--node", node)
	} else {
		result = append(result, "--pod", args[0])
	}
	result = append(result, "--dst-ip", args[1])
	if command == "ovn-trace" {
		result = append(result, "--engine", "ovn")
	}
	args = args[2:]
	if mac, err := net.ParseMAC(args[0]); err == nil && len(mac) == 6 {
		result = append(result, "--dst-mac", args[0])
		args = args[1:]
	}
	result = append(result, "--protocol", args[0])
	if len(args) > 1 {
		flag := "--dst-port"
		if args[0] == "arp" {
			flag = "--arp-op"
		}
		result = append(result, flag, args[1])
	}
	return result
}
