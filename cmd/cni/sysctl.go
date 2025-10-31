//go:build !windows

package main

import (
	"fmt"
	"os"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
)

// For docker version >=17.x the "none" network will disable ipv6 by default.
// We have to enable ipv6 here to add v6 address and gateway.
// See https://github.com/containernetworking/cni/issues/531
func sysctlEnableIPv6(nsPath string) error {
	return ns.WithNetNSPath(nsPath, func(_ ns.NetNS) error {
		for _, conf := range [...]string{"all", "default"} {
			name := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", conf)
			value, err := sysctl.Sysctl(name)
			if err != nil {
				if os.IsNotExist(err) {
					// The sysctl variable doesn't exist, so we can't set it
					continue
				}
				return fmt.Errorf("failed to get sysctl variable %s: %w", name, err)
			}
			if value != "0" {
				if _, err = sysctl.Sysctl(name, "0"); err != nil {
					if os.IsPermission(err) {
						// We don't have permission to set the sysctl variable, so we can't set it
						continue
					}
					return fmt.Errorf("failed to set sysctl variable %s to 0: %w", name, err)
				}
			}
		}
		return nil
	})
}
