package util

import (
	"fmt"
	"net"
)

func Uint32ToIPv4(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", n>>24, n&0xff0000>>16, n&0xff00>>8, n&0xff)
}

func IPv4ToUint32(ip net.IP) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func Uint32ToIPv6(n [4]uint32) string {
	return fmt.Sprintf(
		"%04x:%04x:%04x:%04x:%04x:%04x:%04x:%04x",
		n[0]>>16, n[0]&0xffff,
		n[1]>>16, n[1]&0xffff,
		n[2]>>16, n[2]&0xffff,
		n[3]>>16, n[3]&0xffff,
	)
}
