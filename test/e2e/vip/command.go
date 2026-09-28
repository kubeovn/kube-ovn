package vip

import "fmt"

func virtualVipFindConditions(name, ip string) string {
	// Names and IPs cannot contain single quotes. Preserve the OVN string
	// quotes through the shell used by NBExec, including IPv6 and legacy dual-stack IPs.
	return fmt.Sprintf(`type=virtual name='%q' options:virtual-ip='%q'`, name, ip)
}
