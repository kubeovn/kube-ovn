// Package ipsets reconciles Kube-OVN IP sets through Netlink.
package ipsets

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	IPSetTypeHashIP  = "hash:ip"
	IPSetTypeHashNet = "hash:net"
	maxNameLength    = 31
)

type IPSetMetadata struct {
	SetID   string
	Type    string
	MaxSize uint32
}

type ipSet struct {
	metadata IPSetMetadata
	members  []string
}

type netlinkIPSets interface {
	IpsetCreate(string, string, netlink.IpsetCreateOptions) error
	IpsetList(string) (*netlink.IPSetResult, error)
	IpsetListAll() ([]netlink.IPSetResult, error)
	IpsetFlush(string) error
	IpsetAdd(string, *netlink.IPSetEntry) error
	IpsetSwap(string, string) error
	IpsetDestroy(string) error
}

// IPSets manages one address family. Call ApplyUpdates before changing iptables
// references, then ApplyDeletions after removing obsolete references.
// The caller must serialize access to a manager.
type IPSets struct {
	family    uint8
	prefix    string
	desired   map[string]ipSet
	deletions map[string]struct{}
	netlink   netlinkIPSets
}

func NewIPSets(family int, prefix string) *IPSets {
	version := "4"
	ipFamily := uint8(netlink.FAMILY_V4)
	if family == netlink.FAMILY_V6 {
		version = "6"
		ipFamily = netlink.FAMILY_V6
	}
	return &IPSets{
		family: ipFamily, prefix: prefix + version,
		desired: make(map[string]ipSet), deletions: make(map[string]struct{}),
		netlink: &netlink.Handle{},
	}
}

func (s *IPSets) name(id string) string {
	name := s.prefix + "0" + id
	return name[:min(len(name), maxNameLength)]
}

func (s *IPSets) AddOrReplaceIPSet(metadata IPSetMetadata, members []string) {
	name := s.name(metadata.SetID)
	s.desired[name] = ipSet{metadata: metadata, members: slices.Clone(members)}
	delete(s.deletions, name)
}

func (s *IPSets) RemoveIPSet(id string) {
	name := s.name(id)
	delete(s.desired, name)
	s.deletions[name] = struct{}{}
}

func (s *IPSets) ApplyUpdates() error {
	sets, err := s.netlink.IpsetListAll()
	if err != nil {
		return fmt.Errorf("list IP sets: %w", err)
	}
	current := make(map[string]*netlink.IPSetResult, len(s.desired))
	// A large set can span multiple dump messages. Combine its entries before
	// comparing, and include stale main/temporary sets from previous instances.
	for _, result := range sets {
		if !strings.HasPrefix(result.SetName, s.prefix) {
			continue
		}
		if _, wanted := s.desired[result.SetName]; !wanted {
			s.deletions[result.SetName] = struct{}{}
			continue
		}
		if previous := current[result.SetName]; previous != nil {
			previous.Entries = append(previous.Entries, result.Entries...)
		} else {
			result.Entries = slices.Clone(result.Entries)
			current[result.SetName] = &result
		}
	}
	for _, name := range slices.Sorted(maps.Keys(s.desired)) {
		if err := s.replace(name, s.desired[name], current[name]); err != nil {
			return fmt.Errorf("update IP set %s: %w", name, err)
		}
	}
	return nil
}

func (s *IPSets) replace(name string, desired ipSet, current *netlink.IPSetResult) error {
	entries, err := s.entries(desired)
	if err != nil {
		return err
	}
	options := netlink.IpsetCreateOptions{
		Family: s.family, MaxElements: desired.metadata.MaxSize, Replace: true,
	}
	if current == nil {
		if err = s.netlink.IpsetCreate(name, desired.metadata.Type, options); err != nil {
			return fmt.Errorf("create set: %w", err)
		}
		current, err = s.netlink.IpsetList(name)
		if err != nil {
			return fmt.Errorf("read set: %w", err)
		}
	}
	if current.TypeName != desired.metadata.Type || current.Family != s.family {
		return fmt.Errorf("incompatible existing set: type %s, family %d", current.TypeName, current.Family)
	}
	if equalEntries(current.Entries, entries) {
		return nil
	}

	// Reuse a family-specific temporary set. The main set remains intact if
	// filling the temporary set fails, including when a set is referenced.
	tempName := s.prefix + "t-replace"
	if err = s.destroy(tempName); err != nil {
		return err
	}
	options.Revision = current.Revision
	if err = s.netlink.IpsetCreate(tempName, desired.metadata.Type, options); err != nil {
		return fmt.Errorf("create temporary set: %w", err)
	}
	s.deletions[tempName] = struct{}{}
	if err = s.netlink.IpsetFlush(tempName); err != nil {
		return fmt.Errorf("flush temporary set: %w", err)
	}
	for i := range entries {
		if err = s.netlink.IpsetAdd(tempName, &entries[i]); err != nil {
			return fmt.Errorf("add member to temporary set: %w", err)
		}
	}
	if err = s.netlink.IpsetSwap(tempName, name); err != nil {
		return fmt.Errorf("swap temporary set: %w", err)
	}
	// Destroy immediately so a subsequent hash:ip/hash:net update can reuse
	// the name with its own type. GC retries a failed deletion.
	if err = s.destroy(tempName); err != nil {
		return err
	}
	delete(s.deletions, tempName)
	return nil
}

func (s *IPSets) entries(desired ipSet) ([]netlink.IPSetEntry, error) {
	if desired.metadata.Type != IPSetTypeHashIP && desired.metadata.Type != IPSetTypeHashNet {
		return nil, fmt.Errorf("unsupported set type %q", desired.metadata.Type)
	}
	unique := make(map[netip.Prefix]struct{}, len(desired.members))
	for _, member := range desired.members {
		var prefix netip.Prefix
		var err error
		if desired.metadata.Type == IPSetTypeHashNet && strings.Contains(member, "/") {
			prefix, err = netip.ParsePrefix(member)
		} else {
			var addr netip.Addr
			addr, err = netip.ParseAddr(member)
			if err == nil {
				addr = addr.Unmap()
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
		}
		if err != nil {
			return nil, fmt.Errorf("parse member %q: %w", member, err)
		}
		if prefix.Addr().Is4() != (s.family == netlink.FAMILY_V4) {
			continue
		}
		// hash:net cannot represent /0. Its equivalent is two /1 networks.
		if prefix.Bits() == 0 {
			unique[netip.PrefixFrom(prefix.Masked().Addr(), 1)] = struct{}{}
			if prefix.Addr().Is4() {
				unique[netip.MustParsePrefix("128.0.0.0/1")] = struct{}{}
			} else {
				unique[netip.MustParsePrefix("8000::/1")] = struct{}{}
			}
			continue
		}
		unique[prefix.Masked()] = struct{}{}
	}
	entries := make([]netlink.IPSetEntry, 0, len(unique))
	for prefix := range unique {
		entry := netlink.IPSetEntry{IP: net.IP(prefix.Addr().AsSlice()), Replace: true}
		if desired.metadata.Type == IPSetTypeHashNet {
			bits := prefix.Bits()
			if bits < 1 || bits > 128 {
				return nil, fmt.Errorf("invalid network mask %d", bits)
			}
			entry.CIDR = uint8(bits)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func equalEntries(a, b []netlink.IPSetEntry) bool {
	if len(a) != len(b) {
		return false
	}
	members := make(map[string]struct{}, len(a))
	for _, entry := range a {
		members[entryKey(entry)] = struct{}{}
	}
	for _, entry := range b {
		if _, ok := members[entryKey(entry)]; !ok {
			return false
		}
	}
	return true
}

func entryKey(entry netlink.IPSetEntry) string {
	// Netlink may omit the host mask when dumping a hash:ip set.
	bits := int(entry.CIDR)
	if bits == 0 {
		bits = 128
		if entry.IP.To4() != nil {
			bits = 32
		}
	}
	return fmt.Sprintf("%s/%d", entry.IP, bits)
}

func (s *IPSets) destroy(name string) error {
	if err := s.netlink.IpsetDestroy(name); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("destroy IP set %s: %w", name, err)
	}
	return nil
}

func (s *IPSets) ApplyDeletions() error {
	for _, name := range slices.Sorted(maps.Keys(s.deletions)) {
		if err := s.destroy(name); err != nil {
			return err
		}
		delete(s.deletions, name)
	}
	return nil
}

// Cleanup removes all sets owned by this family after iptables references have
// been removed, including temporary sets left by an interrupted replacement.
func (s *IPSets) Cleanup() error {
	sets, err := s.netlink.IpsetListAll()
	if err != nil {
		return fmt.Errorf("list IP sets for cleanup: %w", err)
	}
	clear(s.desired)
	for _, set := range sets {
		if strings.HasPrefix(set.SetName, s.prefix) {
			s.deletions[set.SetName] = struct{}{}
		}
	}
	return s.ApplyDeletions()
}
