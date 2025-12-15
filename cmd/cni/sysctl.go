package main

import (
	"fmt"
	"os"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
)

var ipv6SysctlSettings = []struct {
	key   string
	value string
}{
	{"disable_ipv6", "0"},
	{"accept_ra", "0"},
}

// For docker version >=17.x the "none" network will disable ipv6 by default.
// We have to enable ipv6 here to add v6 address and gateway.
// See https://github.com/containernetworking/cni/issues/531
func sysctlEnableIPv6(nsPath string) error {
	return ns.WithNetNSPath(nsPath, func(_ ns.NetNS) error {
		for _, conf := range [...]string{"all", "default"} {
			for _, settings := range ipv6SysctlSettings {
				name := fmt.Sprintf("net.ipv6.conf.%s.%s", conf, settings.key)
				value, err := sysctl.Sysctl(name)
				if err != nil {
					if os.IsNotExist(err) {
						// The sysctl variable doesn't exist, so we can't set it
						continue
					}
					return fmt.Errorf("failed to get sysctl variable %s: %w", name, err)
				}
				if value != settings.value {
					if _, err = sysctl.Sysctl(name, settings.value); err != nil {
						if os.IsPermission(err) {
							// We don't have permission to set the sysctl variable, so we can't set it
							continue
						}
						return fmt.Errorf("failed to set sysctl variable %s to %s: %w", name, settings.value, err)
					}
				}
			}
		}
		return nil
	})
}
