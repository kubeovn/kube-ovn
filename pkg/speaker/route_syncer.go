package speaker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"k8s.io/klog/v2"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

// RTPROT_BGP is the protocol number for BGP routes in the kernel's routing table.
// Get from https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtnetlink.h#L312
const (
	rtprotKernel = 2   // Route installed by kernel
	rtprotBgp    = 186 // BGP Routes
)

// RouteSyncer is responsible for watching BGP events and handling them.
// It listens for BGP updates, injects routes into the local route table, and syncs them to the kernel's routing table.
// RouteSyncer is a struct that holds all of the information needed for syncing routes to the kernel's routing table
type RouteSyncer struct {
	config                   *Configuration
	routeTableStateMap       map[string]*netlink.Route
	injectedRoutesSyncPeriod time.Duration
	mutex                    sync.Mutex
	routeReplacer            func(route *netlink.Route) error
}

// NewRouteSyncer creates a new routeSyncer that, when run,
// will sync routes kept in its local state table every syncPeriod
func NewRouteSyncer(syncPeriod time.Duration, config *Configuration) *RouteSyncer {
	rs := RouteSyncer{}
	rs.config = config
	rs.routeTableStateMap = make(map[string]*netlink.Route)
	rs.injectedRoutesSyncPeriod = syncPeriod
	rs.mutex = sync.Mutex{}
	// We substitute the RouteReplace function here so that we can easily monkey patch it in our unit tests
	rs.routeReplacer = netlink.RouteReplace
	// Start to watch updates from the GoBGP server
	rs.watchBgpUpdates()

	return &rs
}

// update path cache when a new path is added or an existing path is updated from gobgp server
func (rs *RouteSyncer) watchBgpUpdates() {
	pathWatch := func(r *gobgpapi.WatchEventResponse) {
		if table := r.GetTable(); table != nil {
			for _, path := range table.Paths {
				if path.Family.Afi == gobgpapi.Family_AFI_IP ||
					path.Family.Afi == gobgpapi.Family_AFI_IP6 ||
					path.Family.Safi == gobgpapi.Family_SAFI_UNICAST {
					if path.NeighborIp == "<nil>" {
						return
					}
					klog.Infof("Processing bgp route advertisement from peer: %s", path.NeighborIp)
					if err := rs.injectRoute(path); err != nil {
						klog.Errorf("failed to inject routes due to: %v", err)
					}
				}
			}
		}
	}
	err := rs.config.BgpServer.WatchEvent(context.Background(), &gobgpapi.WatchEventRequest{
		Table: &gobgpapi.WatchEventRequest_Table{
			Filters: []*gobgpapi.WatchEventRequest_Table_Filter{
				{
					Type: gobgpapi.WatchEventRequest_Table_Filter_BEST,
				},
			},
		},
	}, pathWatch)
	if err != nil {
		klog.Errorf("failed to register monitor global routing table callback due to: %v", err)
	}
}

// addInjectedRoute adds a route to the route map that is regularly synced to the kernel's routing table
func (rs *RouteSyncer) AddInjectedRoute(dst *net.IPNet, route *netlink.Route) {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()
	klog.Infof("Adding route for destination: %s", dst)
	rs.routeTableStateMap[dst.String()] = route
}

// delInjectedRoute delete a route from the route map that is regularly synced to the kernel's routing table
func (rs *RouteSyncer) DelInjectedRoute(dst *net.IPNet) {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()
	if _, ok := rs.routeTableStateMap[dst.String()]; ok {
		klog.Infof("Removing route for destination: %s", dst)
		delete(rs.routeTableStateMap, dst.String())
	}
}

// syncLocalRouteTable iterates over the local route state map and syncs all routes to the kernel's routing table
func (rs *RouteSyncer) SyncLocalRouteTable() (*netlink.Route, error) {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()
	klog.Infof("Running local route table synchronization")
	for _, route := range rs.routeTableStateMap {
		klog.Infof("Syncing route: %s -> %s via %s", route.Src, route.Dst, route.Gw)
		err := rs.routeReplacer(route)
		if err != nil {
			return route, err
		}
	}
	return nil, nil
}

// run starts a goroutine that calls syncLocalRouteTable on interval injectedRoutesSyncPeriod
func (rs *RouteSyncer) Run(stopCh <-chan struct{}) {
	// Start route synchronization routine
	go func(stopCh <-chan struct{}) {
		t := time.NewTicker(rs.injectedRoutesSyncPeriod)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_, err := rs.SyncLocalRouteTable()
				if err != nil {
					klog.Errorf("route could not be replaced due to: %v", err)
				}
			case <-stopCh:
				klog.Infof("Shutting down local route synchronization")
				return
			}
		}
	}(stopCh)
}

// Delete the route from the kernel's routing table, and remove it from the local state map
func (rs *RouteSyncer) injectRoute(path *gobgpapi.Path) error {
	klog.Infof("injectRoute Path Looks Like: %s", path.String())
	var route *netlink.Route
	var link netlink.Link

	// TODO: This is a hardcoded link name, which is not ideal.
	// Find the link by name, which is hardcoded to "net1" in this example.
	// In a real-world scenario, you might want to make this configurable or discover it dynamically
	// based on your network setup.
	link, linkErr := netlink.LinkByName("net1")
	if linkErr != nil {
		klog.Fatalf("Failed to find interface: %v", linkErr)
		return linkErr
	}

	dst, nextHop, pathErr := rs.ParsePath(path)
	if pathErr != nil {
		return pathErr
	}

	// If path is same with external interface subnet do not add
	if err := rs.checkExistingKernelRoute(dst); err != nil {
		// If it's already a kernel route, just log and skip
		klog.Infof("Skipping BGP route injection for %s: %v", dst.String(), err)
		return nil
	}

	// If the path we've received from GoBGP is a withdrawal, we should clean up any lingering routes that may exist
	// on the host (rather than creating a new one or updating an existing one), and then return.
	if path.IsWithdraw {
		klog.Infof("Removing route: '%s via %s' from peer in the routing table", dst, nextHop)

		// Delete route from state map so that it doesn't get re-synced after deletion
		rs.DelInjectedRoute(dst)
		return rs.DeleteByDestination(dst)
	}

	selectBestAddress := func(addrs []netlink.Addr) net.IP {
		var bestAddr net.IP
		bestScope := 1000 // Set a high initial scope value
		for _, addr := range addrs {
			// Scope values are defined as follows:
			// RT_SCOPE_UNIVERSE(0) > RT_SCOPE_SITE(200) > RT_SCOPE_LINK(253) > RT_SCOPE_HOST(254)
			if addr.Scope < bestScope {
				bestScope = addr.Scope
				bestAddr = addr.IP
			}
		}
		// If no best address was found, return the first address in the list
		if bestAddr == nil && len(addrs) > 0 {
			bestAddr = addrs[0].IP
		}
		return bestAddr
	}
	// Use external interface for destination routing
	// Determine the address family based on the destination IP
	family := netlink.FAMILY_V4
	if dst.IP.To4() == nil {
		family = netlink.FAMILY_V6
	}
	// Get the list of addresses for the link
	addrs, addrsErr := netlink.AddrList(link, family)
	if addrsErr != nil {
		klog.Errorf("failed to get addresses for interface %s: %s",
			link.Attrs().Name, addrsErr)
		return addrsErr
	}
	if len(addrs) == 0 {
		klog.Errorf("no addresses found on interface %s",
			link.Attrs().Name)
		return errors.New("no addresses found on interface")
	}
	// If we have multiple addresses, we need to select the best one
	bestIPForFamily := selectBestAddress(addrs)

	route = &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Src:       bestIPForFamily,
		Dst:       dst,
		Protocol:  rtprotBgp,
		Gw:        nextHop,
	}

	// We have our route configured, let's add it to the host's routing table
	klog.Infof("Inject route: '%s via %s' from peer to routing table", dst, nextHop)
	rs.AddInjectedRoute(dst, route)
	// Immediately sync the local route table regardless of timer
	_, syncLocalRouteTableErr := rs.SyncLocalRouteTable()
	return syncLocalRouteTableErr
}

// ParseNextHop takes in a GoBGP Path and parses out the destination's next hop from its attributes. If it
// can't parse a next hop IP from the GoBGP Path, it returns an error.
func (rs *RouteSyncer) ParseNextHop(path *gobgpapi.Path) (net.IP, error) {
	for _, pAttr := range path.GetPattrs() {
		unmarshalNew, err := pAttr.UnmarshalNew()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal path attribute: %w", err)
		}
		switch t := unmarshalNew.(type) {
		case *gobgpapi.NextHopAttribute:
			// This is the primary way that we receive NextHops and happens when both the client and the server exchange
			// next hops on the same IP family that they negotiated BGP on
			nextHopIP := net.ParseIP(t.NextHop)
			if nextHopIP != nil && (nextHopIP.To4() != nil || nextHopIP.To16() != nil) {
				return nextHopIP, nil
			}
			return nil, fmt.Errorf("invalid nextHop address: %s", t.NextHop)
		case *gobgpapi.MpReachNLRIAttribute:
			// in the case where the server and the client are exchanging next-hops that don't relate to their primary
			// IP family, we get MpReachNLRIAttribute instead of NextHopAttributes
			// TODO: here we only take the first next hop, at some point in the future it would probably be best to
			// consider multiple next hops
			nextHopIP := net.ParseIP(t.NextHops[0])
			if nextHopIP != nil && (nextHopIP.To4() != nil || nextHopIP.To16() != nil) {
				return nextHopIP, nil
			}
			return nil, fmt.Errorf("invalid nextHop address: %s", t.NextHops[0])
		}
	}
	return nil, fmt.Errorf("could not parse next hop received from GoBGP for path: %s", path)
}

// ParsePath takes in a GoBGP Path and parses out the destination subnet and the next hop from its attributes.
// If successful, it will return the destination of the BGP path as a subnet form and the next hop. If it
// can't parse the destination or the next hop IP, it returns an error.
func (rs *RouteSyncer) ParsePath(path *gobgpapi.Path) (*net.IPNet, net.IP, error) {
	nextHop, err := rs.ParseNextHop(path)
	if err != nil {
		return nil, nil, err
	}

	nlri := path.GetNlri()
	var prefix gobgpapi.IPAddressPrefix
	err = nlri.UnmarshalTo(&prefix)
	if err != nil {
		return nil, nil, errors.New("invalid nlri in advertised path")
	}
	dstSubnet, err := netlink.ParseIPNet(prefix.Prefix + "/" + strconv.FormatUint(uint64(prefix.PrefixLen), 10))
	if err != nil {
		return nil, nil, errors.New("couldn't parse IP subnet from nlri advertised path")
	}
	return dstSubnet, nextHop, nil
}

// DeleteByDestination attempts to safely find all routes based upon its destination subnet and delete them
func (rs *RouteSyncer) DeleteByDestination(destinationSubnet *net.IPNet) error {
	routes, err := netlink.RouteListFiltered(nl.FAMILY_ALL, &netlink.Route{
		Dst: destinationSubnet, Protocol: rtprotBgp,
	}, netlink.RT_FILTER_DST|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return fmt.Errorf("failed to get routes from netlink: %w", err)
	}
	for i, r := range routes {
		klog.Infof("Found route to remove: %s", r.String())
		if err = netlink.RouteDel(&routes[i]); err != nil {
			return fmt.Errorf("failed to remove route due to %w", err)
		}
	}
	return nil
}

// checks if a route with the same destination already exists
// with protocol "kernel" and returns an error if found
func (rs *RouteSyncer) checkExistingKernelRoute(dst *net.IPNet) error {
	// Get existing routes for the destination
	routes, err := netlink.RouteListFiltered(nl.FAMILY_ALL, &netlink.Route{
		Dst:      dst,
		Protocol: rtprotKernel,
	}, netlink.RT_FILTER_DST|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		klog.Errorf("Failed to get existing routes for destination %s: %v", dst.String(), err)
		return nil // Don't block route injection on query failure
	}

	// If we found any kernel routes with the same destination, skip injection
	if len(routes) > 0 {
		for _, existingRoute := range routes {
			klog.Infof("Found existing kernel route: %s", existingRoute.String())
		}
		return fmt.Errorf("destination %s already exists as kernel protocol route", dst.String())
	}

	return nil
}
