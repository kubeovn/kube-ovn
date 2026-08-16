package bgp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type frrPath struct {
	PeerID   string `json:"peerId"`
	Valid    bool   `json:"valid"`
	Nexthops []struct {
		IP string `json:"ip"`
	} `json:"nexthops"`
}

type frrRoute struct {
	Paths []frrPath `json:"paths"`
}

type frrRIB struct {
	Routes map[string][]frrPath `json:"routes"`
}

type frrRoutePath struct {
	PeerID  string
	NextHop string
	Valid   bool
}

func validNodeRoutePath(address string) frrRoutePath {
	return frrRoutePath{PeerID: address, NextHop: address, Valid: true}
}

func sortRoutePaths(paths []frrRoutePath) {
	slices.SortFunc(paths, func(a, b frrRoutePath) int {
		if result := strings.Compare(a.PeerID, b.PeerID); result != 0 {
			return result
		}
		return strings.Compare(a.NextHop, b.NextHop)
	})
}

func readFRRRouteWithRunner(prefix string, runner func(string) ([]byte, error)) (*frrRoute, error) {
	output, err := runner("show bgp ipv4 unicast json")
	if err != nil {
		return nil, err
	}
	var rib frrRIB
	if err = json.Unmarshal(output, &rib); err != nil {
		return nil, fmt.Errorf("failed to parse FRR RIB output for %s: %w: %s", prefix, err, output)
	}
	return &frrRoute{Paths: rib.Routes[prefix]}, nil
}

func routePathsFromRoute(route *frrRoute) []frrRoutePath {
	paths := make([]frrRoutePath, 0, len(route.Paths))
	for _, path := range route.Paths {
		for _, nextHop := range path.Nexthops {
			if nextHop.IP != "" {
				paths = append(paths, frrRoutePath{PeerID: path.PeerID, NextHop: nextHop.IP, Valid: path.Valid})
			}
		}
	}
	sortRoutePaths(paths)
	return paths
}
