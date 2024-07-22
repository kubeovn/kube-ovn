package speaker

import (
	"context"
	"fmt"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	bgpapi "github.com/osrg/gobgp/v3/api"
	bgpapiutil "github.com/osrg/gobgp/v3/pkg/apiutil"
	"github.com/osrg/gobgp/v3/pkg/packet/bgp"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/anypb"
	"k8s.io/klog/v2"
	"net"
)

var (
	maskMap = map[string]int{kubeovnv1.ProtocolIPv4: 32, kubeovnv1.ProtocolIPv6: 128}
)

// reconciliateRoutes configures the BGP speaker to announce only the routes we are expected to announce
// and to withdraw the ones that should not be announced anymore
func (c *Controller) reconciliateRoutes(expectedPrefixes prefixMap) error {
	if len(c.config.NeighborAddresses) != 0 {
		err := c.reconciliateIPFamily(kubeovnv1.ProtocolIPv4, expectedPrefixes)
		if err != nil {
			return fmt.Errorf("failed to reconciliate IPv4 routes: %w", err)
		}
	}

	if len(c.config.NeighborIPv6Addresses) != 0 {
		err := c.reconciliateIPFamily(kubeovnv1.ProtocolIPv6, expectedPrefixes)
		if err != nil {
			return fmt.Errorf("failed to reconciliate IPv6 routes: %w", err)
		}
	}

	return nil
}

// reconciliateIPFamily announces prefixes we are not currently announcing and withdraws prefixes we should
// not be announcing for a given IP family (IPv4/IPv6)
func (c *Controller) reconciliateIPFamily(ipFamily string, expectedPrefixes prefixMap) error {
	// Get the address family associated with the Kube-OVN family
	afi, err := kubeOvnFamilyToAFI(ipFamily)
	if err != nil {
		return fmt.Errorf("couldn't convert family to afi: %w", err)
	}

	// Craft a BGP path listing request for this AFI
	listPathRequest := &bgpapi.ListPathRequest{
		TableType: bgpapi.TableType_GLOBAL,
		Family:    &bgpapi.Family{Afi: afi, Safi: bgpapi.Family_SAFI_UNICAST},
	}

	// Anonymous function that stores the prefixes we are announcing for this AFI
	var existingPrefixes []string
	fn := func(d *bgpapi.Destination) {
		for _, path := range d.Paths {
			attrInterfaces, _ := bgpapiutil.UnmarshalPathAttributes(path.Pattrs)
			nextHop := getNextHopFromPathAttributes(attrInterfaces)
			klog.V(5).Infof("announcing route with prefix %s and nexthop: %s", d.Prefix, nextHop.String())

			route, _ := netlink.RouteGet(nextHop)
			if len(route) == 1 && route[0].Type == unix.RTN_LOCAL || nextHop.String() == c.config.RouterID {
				existingPrefixes = append(existingPrefixes, d.Prefix)
				return
			}
		}
	}

	// Ask the BGP speaker what routes we're announcing for the IP family selected
	if err := c.config.BgpServer.ListPath(context.Background(), listPathRequest, fn); err != nil {
		return fmt.Errorf("failed to list existing %s routes: %w", ipFamily, err)
	}

	klog.V(5).Infof("currently announcing %s routes: %v", ipFamily, existingPrefixes)

	// Announce routes we should be announcing and withdraw the ones that are no longer valid
	c.announceAndWithdraw(routeDiff(expectedPrefixes[ipFamily], existingPrefixes))
	return nil
}

// announceAndWithdraw commands the BGP speaker to start announcing new routes and to withdraw others
func (c *Controller) announceAndWithdraw(toAdd, toDel []string) {
	// Announce routes that need to be added
	klog.V(5).Infof("new routes we will announce: %v", toAdd)
	for _, route := range toAdd {
		if err := c.addRoute(route); err != nil {
			klog.Error(err)
		}
	}

	// Withdraw routes that should be deleted
	klog.V(5).Infof("announced routes we will withdraw: %v", toDel)
	for _, route := range toDel {
		if err := c.delRoute(route); err != nil {
			klog.Error(err)
		}
	}
}

// addRoute adds a new route to advertise from our BGP speaker
func (c *Controller) addRoute(route string) error {
	// Determine the Address Family Indicator (IPv6/IPv4)
	routeAfi := bgpapi.Family_AFI_IP
	if util.CheckProtocol(route) == kubeovnv1.ProtocolIPv6 {
		routeAfi = bgpapi.Family_AFI_IP6
	}

	// Get NLRI and attributes to announce all the next hops possible
	nlri, attrs, err := c.getNlriAndAttrs(route)
	if err != nil {
		return fmt.Errorf("failed to get NLRI and attributes: %w", nlri)
	}

	// Announce every next hop we have
	for _, attr := range attrs {
		_, err = c.config.BgpServer.AddPath(context.Background(), &bgpapi.AddPathRequest{
			Path: &bgpapi.Path{
				Family: &bgpapi.Family{Afi: routeAfi, Safi: bgpapi.Family_SAFI_UNICAST},
				Nlri:   nlri,
				Pattrs: attr,
			},
		})

		if err != nil {
			return err
		}
	}

	return nil
}

// delRoute removes a route we are currently advertising from our BGP speaker
func (c *Controller) delRoute(route string) error {
	// Determine the Address Family Indicator (IPv6/IPv4)
	routeAfi := bgpapi.Family_AFI_IP
	if util.CheckProtocol(route) == kubeovnv1.ProtocolIPv6 {
		routeAfi = bgpapi.Family_AFI_IP6
	}

	// Get NLRI and attributes to announce all the next hops possible
	nlri, attrs, err := c.getNlriAndAttrs(route)
	if err != nil {
		return fmt.Errorf("failed to get NLRI and attributes: %w", nlri)
	}

	// Withdraw every next hop we have
	for _, attr := range attrs {
		err = c.config.BgpServer.DeletePath(context.Background(), &bgpapi.DeletePathRequest{
			Path: &bgpapi.Path{
				Family: &bgpapi.Family{Afi: routeAfi, Safi: bgpapi.Family_SAFI_UNICAST},
				Nlri:   nlri,
				Pattrs: attr,
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// getNlriAndAttrs returns the Network Layer Reachability Information (NLRI) and the BGP attributes for a particular route
func (c *Controller) getNlriAndAttrs(route string) (*anypb.Any, [][]*anypb.Any, error) {
	// Should this route be advertised to IPv4 or IPv6 peers
	// GoBGP supports extended-nexthop, we should be able to advertise IPv4 NLRI to IPv6 peer and the opposite to
	// Is this check really necessary?
	neighborAddresses := c.config.NeighborAddresses
	if util.CheckProtocol(route) == kubeovnv1.ProtocolIPv6 {
		neighborAddresses = c.config.NeighborIPv6Addresses
	}

	// Get the route we're about to advertise and transform it to an NLRI
	prefix, prefixLen, err := parseRoute(route)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse route: %w", err)
	}

	// Marshal the NLRI
	nlri, err := anypb.New(&bgpapi.IPAddressPrefix{
		Prefix:    prefix,
		PrefixLen: prefixLen,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal NLRI: %w", err)
	}

	// Create attributes for each neighbor to advertise the correct next hop
	attrs := make([][]*anypb.Any, 0, len(neighborAddresses))
	for _, addr := range neighborAddresses {
		a1, _ := anypb.New(&bgpapi.OriginAttribute{
			Origin: 0,
		})
		a2, _ := anypb.New(&bgpapi.NextHopAttribute{
			NextHop: c.getNextHopAttribute(addr),
		})
		attrs = append(attrs, []*anypb.Any{a1, a2})
	}

	return nlri, attrs, err
}

// getNextHopAttribute returns the next hop we should advertise for a specific BGP neighbor
func (c *Controller) getNextHopAttribute(neighborAddress string) string {
	nextHop := c.config.RouterID // If no route is found, fallback to router ID

	// Retrieve the route we use to speak to this neighbor and consider the source as next hop
	routes, err := netlink.RouteGet(net.ParseIP(neighborAddress))
	if err == nil && len(routes) == 1 && routes[0].Src != nil {
		nextHop = routes[0].Src.String()
	}

	proto := util.CheckProtocol(nextHop) // Is next hop IPv4 or IPv6
	nextHopIP := net.ParseIP(nextHop)    // Convert next hop to an IP

	// This takes care of a special case where the speaker might not be running in host mode
	// If this happens, the nextHopIP will be the IP of a Pod (probably unreachable for the neighbours)
	// For this case, the configuration allows for manually specifying the IPs to use as next hop (per protocol)
	nodeIP := c.config.NodeIPs[proto]
	if nodeIP != nil && nextHopIP != nil && nextHopIP.Equal(c.config.PodIPs[proto]) {
		nextHop = nodeIP.String()
	}

	return nextHop
}

// getNextHopFromPathAttributes returns the next hop from BGP path attributes
func getNextHopFromPathAttributes(attrs []bgp.PathAttributeInterface) net.IP {
	for _, attr := range attrs {
		switch a := attr.(type) {
		case *bgp.PathAttributeNextHop:
			return a.Value
		case *bgp.PathAttributeMpReachNLRI:
			return a.Nexthop
		}
	}
	return nil
}
