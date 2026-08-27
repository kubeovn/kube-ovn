//go:build !linux

package aclsampling

import (
	"context"
	"errors"
)

// ListenPSamples is unavailable outside Linux because psample is a Linux
// generic netlink family.
func ListenPSamples(context.Context, uint32, func(PacketSample) error) error {
	return errors.New("Linux psample is unsupported on this operating system")
}
