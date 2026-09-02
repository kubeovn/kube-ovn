package util

import (
	"errors"
	"fmt"
	"net"
	"slices"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

// GatewayOnLinkRoute describes a SCOPE_LINK host route to a gateway IP
// for routed subnet mode (/32 or /128 on-link).
type GatewayOnLinkRoute struct {
	Gateway net.IP
	Dst     *net.IPNet
}

// GatewayOnLinkRoutes builds on-link host routes for each gateway address.
// linkIndex is accepted for callers that install via netlink; the returned
// destinations are independent of the index.
func GatewayOnLinkRoutes(gateway string, _ int) ([]GatewayOnLinkRoute, error) {
	routes := make([]GatewayOnLinkRoute, 0)
	for _, gw := range SplitTrimmed(gateway, ",") {
		gwIP := net.ParseIP(gw)
		if gwIP == nil {
			return nil, fmt.Errorf("invalid gateway %s for routed subnet", gw)
		}
		bits := 32
		if gwIP.To4() == nil {
			bits = 128
		}
		routes = append(routes, GatewayOnLinkRoute{
			Gateway: gwIP,
			Dst:     &net.IPNet{IP: gwIP, Mask: net.CIDRMask(bits, bits)},
		})
	}
	return routes, nil
}

// ValidateRoutedAnnotationRoutes ensures pod route annotations are compatible
// with routed (/32|/128) mode. Next hops must be the subnet gateway because
// OVN ACLs only allow ARP/ND for that gateway and IP frames to the LRP MAC.
func ValidateRoutedAnnotationRoutes(routes []request.Route, subnetGateway string) error {
	if len(routes) == 0 {
		return nil
	}
	allowed := SplitTrimmed(subnetGateway, ",")
	if len(allowed) == 0 {
		return errors.New("routed subnet route annotations require a subnet gateway")
	}
	for _, r := range routes {
		if r.Destination != "" {
			if _, _, err := net.ParseCIDR(r.Destination); err != nil {
				return fmt.Errorf("invalid route destination %s: %w", r.Destination, err)
			}
		}
		if r.Gateway == "" {
			return fmt.Errorf("routed subnet route annotations must set gateway to the subnet gateway %q (got destination %q with empty gateway)", subnetGateway, r.Destination)
		}
		if net.ParseIP(r.Gateway) == nil {
			return fmt.Errorf("invalid route gateway %s", r.Gateway)
		}
		if !slices.Contains(allowed, r.Gateway) {
			return fmt.Errorf("routed subnet route annotation gateway %s must be the subnet gateway %s", r.Gateway, subnetGateway)
		}
	}
	return nil
}
