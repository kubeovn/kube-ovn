// Copyright 2018 Paul Greenberg (greenpau@outlook.com)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ovsdb

import (
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"net"
)

func getNext(ip net.IP, count uint) net.IP {
	g := ip.To4()
	if g == nil {
		return nil
	}
	gb := uint(g[0])<<24 + uint(g[1])<<16 + uint(g[2])<<8 + uint(g[3])
	gb += count
	return net.IPv4(byte((gb>>24)&0xff), byte((gb>>16)&0xff), byte((gb>>8)&0xff), byte(gb&0xff))
}

// RouteFilterEntry TODO
type RouteFilterEntry struct {
	Network    *net.IPNet
	Exclusions []*net.IPNet
}

// NewRouteFilterEntry TODO
func NewRouteFilterEntry(network string, excludeGateway bool) (*RouteFilterEntry, error) {
	entry := &RouteFilterEntry{Exclusions: []*net.IPNet{}}
	ip, rfNet, err := net.ParseCIDR(network)
	if err != nil {
		return nil, err
	}
	entry.Network = rfNet
	if excludeGateway {
		gwIP := getNext(ip, 1)
		if gwIP != nil {
			_, gwNet, err := net.ParseCIDR(gwIP.String() + "/32")
			if err != nil {
				return nil, err
			}
			entry.Exclusions = append(entry.Exclusions, gwNet)
		}
	}
	return entry, nil
}

// Match TODO
func (entry *RouteFilterEntry) Match(ip net.IP) bool {
	if !entry.Network.Contains(ip) {
		return false
	}
	for _, e := range entry.Exclusions {
		if e.Contains(ip) {
			return false
		}
	}
	return true
}

// RouteFilter TODO
type RouteFilter struct {
	Entries []*RouteFilterEntry
}

// NewRouteFilter TODO
func NewRouteFilter(networks []string) (*RouteFilter, error) {
	rf := &RouteFilter{}
	for _, n := range networks {
		entry, err := NewRouteFilterEntry(n, false)
		if err != nil {
			return rf, err
		}
		rf.Entries = append(rf.Entries, entry)
	}
	return rf, nil
}

// NewRouteFilterExcludeGateway TODO
func NewRouteFilterExcludeGateway(networks []string) (*RouteFilter, error) {
	rf := &RouteFilter{}
	for _, n := range networks {
		entry, err := NewRouteFilterEntry(n, true)
		if err != nil {
			return rf, err
		}
		rf.Entries = append(rf.Entries, entry)
	}
	return rf, nil
}

// Match TODO
func (rf *RouteFilter) Match(ip net.IP) bool {
	for _, entry := range rf.Entries {
		if entry.Match(ip) {
			return true
		}
	}
	return false
}

// Add TODO
func (rf *RouteFilter) Add(network string) error {
	ip, ipNet, err := net.ParseCIDR(network)
	networkIndex := 0
	networkFound := false
	highestPrefix := 0
	if err != nil {
		return err
	}
	for i, entry := range rf.Entries {
		if entry.Network.Contains(ip) {
			_, networkPrefix := entry.Network.Mask.Size()
			if networkPrefix > highestPrefix {
				networkFound = true
				networkIndex = i
				highestPrefix = networkPrefix
			}
		}
	}
	if networkFound == true {
		entry := rf.Entries[networkIndex]
		entry.Exclusions = append(entry.Exclusions, ipNet)
	} else {
		return fmt.Errorf("failed to add %s to route filter (supernet not found)", network)
	}
	return nil
}
