package util

import (
	"fmt"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

// AddrList lists addresses on link using a per-call netlink handle with
// NETLINK_GET_STRICT_CHK enabled so the kernel filters results by ifindex
// server-side (Linux >= 4.20) instead of dumping the entire namespace and
// filtering in userspace. On older kernels that reject the sockopt it falls
// back transparently to the package-level netlink.AddrList.
//
// When link is nil a full dump is the intent, so strict check is skipped
// and the call is forwarded directly to the package-level implementation.
func AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	if link == nil {
		return netlink.AddrList(link, family)
	}

	h, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("failed to create netlink handle: %w", err)
	}
	defer h.Close()

	if err := h.SetStrictCheck(true); err != nil {
		klog.V(5).Infof("NETLINK_GET_STRICT_CHK not supported (%v); falling back to package-level AddrList", err)
		return netlink.AddrList(link, family)
	}
	return h.AddrList(link, family)
}
