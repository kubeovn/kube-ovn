//go:build !windows
// +build !windows

package util

// IPTableRule wraps iptables rule
type IPTableRule struct {
	Table string
	Chain string
	Rule  []string
}
