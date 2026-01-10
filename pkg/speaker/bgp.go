package speaker

import (
	"fmt"
	"net"

	"github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// reconcileRoutes configures the BGP speaker to announce only the routes we are expected to announce
// and to withdraw the ones that should not be announced anymore
func (c *Controller) reconcileRoutes(expectedPrefixes prefixMap) error {
	if c.config.ExtendedNexthop || len(c.config.NeighborAddresses) != 0 {
		err := c.reconcileIPFamily(api.Family_AFI_IP, expectedPrefixes)
		if err != nil {
			return fmt.Errorf("failed to reconcile IPv4 routes: %w", err)
		}
	}

	if c.config.ExtendedNexthop || len(c.config.NeighborIPv6Addresses) != 0 {
		err := c.reconcileIPFamily(api.Family_AFI_IP6, expectedPrefixes)
		if err != nil {
			return fmt.Errorf("failed to reconcile IPv6 routes: %w", err)
		}
	}

	return nil
}

// reconcileIPFamily announces prefixes we are not currently announcing and withdraws prefixes we should
// not be announcing for a given IP family (IPv4/IPv6)
func (c *Controller) reconcileIPFamily(afi api.Family_Afi, expectedPrefixes prefixMap) error {
	// Craft a BGP path listing request for this AFI
	listPathRequest := apiutil.ListPathRequest{
		TableType: api.TableType_TABLE_TYPE_GLOBAL,
		Family:    apiutil.ToFamily(&api.Family{Afi: afi, Safi: api.Family_SAFI_UNICAST}),
	}

	// Anonymous function that stores the prefixes we are announcing for this AFI
	existingPrefixes := set.New[string]()
	fn := func(prefix bgp.NLRI, paths []*apiutil.Path) {
		for _, path := range paths {
			nextHop := getNextHopFromPathAttributes(path.Attrs)
			klog.V(5).Infof("announcing route with prefix %s and nexthop: %s", prefix, nextHop)

			route, _ := netlink.RouteGet(nextHop)
			if len(route) == 1 && route[0].Type == unix.RTN_LOCAL || nextHop.Equal(c.config.RouterID) {
				existingPrefixes.Insert(prefix.String())
				return
			}
		}
	}

	// Ask the BGP speaker what route we're announcing for the IP family selected
	if err := c.config.BgpServer.ListPath(listPathRequest, fn); err != nil {
		return fmt.Errorf("failed to list existing %s routes: %w", afi, err)
	}

	klog.V(5).Infof("currently announcing %s routes: %v", afi, existingPrefixes.SortedList())

	// Announce routes we should be announcing and withdraw the ones that are no longer valid
	c.announceAndWithdraw(expectedPrefixes[afi], existingPrefixes)
	return nil
}

// announceAndWithdraw commands the BGP speaker to start announcing new routes and to withdraw others
func (c *Controller) announceAndWithdraw(expected, existing set.Set[string]) {
	// Announce routes that need to be added
	toAdd := expected.Difference(existing)
	klog.V(5).Infof("new routes we will announce: %v", toAdd.SortedList())
	for route := range toAdd {
		if err := c.addRoute(route); err != nil {
			klog.Error(err)
		}
	}

	// Withdraw routes that should be deleted
	toDel := existing.Difference(expected)
	klog.V(5).Infof("announced routes we will withdraw: %v", toDel.SortedList())
	for route := range toDel {
		if err := c.delRoute(route); err != nil {
			klog.Error(err)
		}
	}
}

// addRoute adds a new route to advertise from our BGP speaker
func (c *Controller) addRoute(route string) error {
	// Get paths used to announce all the next hops possible
	paths, err := c.getPathRequest(route)
	if err != nil {
		return fmt.Errorf("failed to get NLRI and attributes: %w", err)
	}

	// Announce every next hop we have
	for _, p := range paths {
		if _, err = c.config.BgpServer.AddPath(apiutil.AddPathRequest{
			Paths: p,
		}); err != nil {
			return fmt.Errorf("failed to add paths %+v: %w", p, err)
		}
	}

	return nil
}

// delRoute removes a route we are currently advertising from our BGP speaker
func (c *Controller) delRoute(route string) error {
	// Get paths used to withdraw all the next hops possible
	paths, err := c.getPathRequest(route)
	if err != nil {
		return fmt.Errorf("failed to get NLRI and attributes: %w", err)
	}

	// Withdraw every next hop we have
	for _, p := range paths {
		if err = c.config.BgpServer.DeletePath(apiutil.DeletePathRequest{
			Paths: p,
		}); err != nil {
			return fmt.Errorf("failed to delete paths %+v: %w", p, err)
		}
	}

	return nil
}

// getPathRequest returns paths to be used in add/delete path requests for a given route
func (c *Controller) getPathRequest(route string) ([][]*apiutil.Path, error) {
	// Determine which peers should receive this route announcement
	// If extended-nexthop is enabled, advertise all routes to all peers (both IPv4 and IPv6)
	// Otherwise, advertise IPv4 routes to IPv4 peers and IPv6 routes to IPv6 peers
	neighborAddresses := c.config.NeighborAddresses
	if c.config.ExtendedNexthop {
		neighborAddresses = append(neighborAddresses, c.config.NeighborIPv6Addresses...)
	} else if util.CheckProtocol(route) == kubeovnv1.ProtocolIPv6 {
		neighborAddresses = c.config.NeighborIPv6Addresses
	}

	// Get the route we're about to advertise and transform it to an NLRI
	prefix, err := parsePrefix(route)
	if err != nil {
		return nil, fmt.Errorf("failed to parse route: %w", err)
	}

	// Create paths to be used in add/delete path request
	paths := make([][]*apiutil.Path, 0, len(neighborAddresses))
	family := &api.Family{Afi: prefixToAFI(prefix), Safi: api.Family_SAFI_UNICAST}
	for _, addr := range neighborAddresses {
		path := &api.Path{
			Family: family,
			Nlri: &api.NLRI{
				Nlri: &api.NLRI_Prefix{
					Prefix: &api.IPAddressPrefix{
						Prefix:    prefix.Addr().String(),
						PrefixLen: uint32(prefix.Bits()), // #nosec G115
					},
				},
			},
			Pattrs: []*api.Attribute{{
				Attr: &api.Attribute_Origin{
					Origin: &api.OriginAttribute{
						Origin: 0,
					},
				},
			}, {
				Attr: &api.Attribute_NextHop{
					NextHop: &api.NextHopAttribute{
						NextHop: c.getNextHopAttribute(addr).String(),
					},
				},
			}},
		}

		nativeNlri, err := apiutil.GetNativeNlri(path)
		if err != nil {
			return nil, fmt.Errorf("invalid nlri: %w", err)
		}
		nativeAttrs, err := apiutil.GetNativePathAttributes(path)
		if err != nil {
			return nil, fmt.Errorf("invalid path attributes: %w", err)
		}

		paths = append(paths, []*apiutil.Path{{
			Family: bgp.NewFamily(uint16(path.Family.Afi), uint8(path.Family.Safi)), // #nosec G115
			Nlri:   nativeNlri,
			Attrs:  nativeAttrs,
		}})
	}

	return paths, nil
}

// getNextHopAttribute returns the next hop we should advertise for a specific BGP neighbor
func (c *Controller) getNextHopAttribute(neighborAddress net.IP) net.IP {
	nextHop := c.config.RouterID // If no route is found, fallback to router ID

	// Retrieve the route we use to speak to this neighbor and consider the source as next hop
	routes, err := netlink.RouteGet(neighborAddress)
	if err == nil && len(routes) == 1 && routes[0].Src != nil {
		nextHop = routes[0].Src
	}

	proto := util.CheckProtocol(nextHop.String()) // Is next hop IPv4 or IPv6

	// This takes care of a special case where the speaker might not be running in host mode
	// If this happens, the nextHopIP will be the IP of a Pod (probably unreachable for the neighbors)
	// For this case, the configuration allows for manually specifying the IPs to use as next hop (per protocol)
	nodeIP := c.config.NodeIPs[proto]
	if nodeIP != nil && nextHop.Equal(c.config.PodIPs[proto]) {
		nextHop = nodeIP
	}

	return nextHop
}

// getNextHopFromPathAttributes returns the next hop from BGP path attributes
func getNextHopFromPathAttributes(attrs []bgp.PathAttributeInterface) net.IP {
	for _, attr := range attrs {
		switch a := attr.(type) {
		case *bgp.PathAttributeNextHop:
			return a.Value.AsSlice()
		case *bgp.PathAttributeMpReachNLRI:
			return a.Nexthop.AsSlice()
		}
	}
	return nil
}
