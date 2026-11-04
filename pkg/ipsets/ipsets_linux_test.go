package ipsets

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type fakeNetlink struct {
	sets       map[string]*netlink.IPSetResult
	calls      []string
	fail       string
	failure    error
	dump       []netlink.IPSetResult
	failAfter  int
	referenced map[string]bool
}

func (f *fakeNetlink) call(operation, name string) error {
	f.calls = append(f.calls, operation+" "+name)
	if f.fail == operation {
		if f.failAfter == 0 {
			return f.failure
		}
		f.failAfter--
	}
	return nil
}

func (f *fakeNetlink) IpsetCreate(name, kind string, options netlink.IpsetCreateOptions) error {
	if err := f.call("create", name); err != nil {
		return err
	}
	if existing := f.sets[name]; existing != nil {
		if existing.TypeName != kind || existing.Family != options.Family {
			return unix.EEXIST
		}
		return nil
	}
	f.sets[name] = &netlink.IPSetResult{
		SetName: name, TypeName: kind, Family: options.Family,
		MaxElements: options.MaxElements, Revision: options.Revision,
	}
	return nil
}

func (f *fakeNetlink) IpsetList(name string) (*netlink.IPSetResult, error) {
	if err := f.call("list", name); err != nil {
		return nil, err
	}
	if existing := f.sets[name]; existing != nil {
		result := *existing
		result.Entries = slices.Clone(existing.Entries)
		return &result, nil
	}
	return nil, unix.ENOENT
}

func (f *fakeNetlink) IpsetListAll() ([]netlink.IPSetResult, error) {
	if err := f.call("list-all", ""); err != nil {
		return nil, err
	}
	if f.dump != nil {
		return f.dump, nil
	}
	results := make([]netlink.IPSetResult, 0, len(f.sets))
	for _, set := range f.sets {
		results = append(results, *set)
	}
	return results, nil
}

func (f *fakeNetlink) IpsetFlush(name string) error {
	if err := f.call("flush", name); err != nil {
		return err
	}
	f.sets[name].Entries = nil
	return nil
}

func (f *fakeNetlink) IpsetAdd(name string, entry *netlink.IPSetEntry) error {
	if err := f.call("add", name); err != nil {
		return err
	}
	f.sets[name].Entries = append(f.sets[name].Entries, *entry)
	return nil
}

func (f *fakeNetlink) IpsetSwap(a, b string) error {
	if err := f.call("swap", b); err != nil {
		return err
	}
	if f.sets[a].TypeName != f.sets[b].TypeName || f.sets[a].Family != f.sets[b].Family {
		return unix.EINVAL
	}
	f.sets[a].Entries, f.sets[b].Entries = f.sets[b].Entries, f.sets[a].Entries
	return nil
}

func (f *fakeNetlink) IpsetDestroy(name string) error {
	if err := f.call("destroy", name); err != nil {
		return err
	}
	if f.sets[name] == nil {
		return unix.ENOENT
	}
	if f.referenced[name] {
		return unix.EBUSY
	}
	delete(f.sets, name)
	return nil
}

func newTestIPSets(family int) (*IPSets, *fakeNetlink) {
	manager := NewIPSets(family, "ovn")
	fake := &fakeNetlink{
		sets: make(map[string]*netlink.IPSetResult), failure: errors.New("netlink failure"),
		referenced: make(map[string]bool),
	}
	manager.netlink = fake
	return manager, fake
}

func TestIPSetMembers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		family  int
		kind    string
		members []string
		want    []string
	}{
		{
			name: "IPv4 networks", family: netlink.FAMILY_V4, kind: IPSetTypeHashNet,
			members: []string{"10.0.0.7/24", "10.0.0.0/24", "10.1.0.1", "2001:db8::/64"},
			want:    []string{"10.0.0.0/24", "10.1.0.1/32"},
		},
		{
			name: "IPv6 networks", family: netlink.FAMILY_V6, kind: IPSetTypeHashNet,
			members: []string{"2001:db8::1/64", "2001:0db8::/64", "2001:db8:1::1", "10.0.0.1"},
			want:    []string{"2001:db8:1::1/128", "2001:db8::/64"},
		},
		{
			name: "IPv4 addresses", family: netlink.FAMILY_V4, kind: IPSetTypeHashIP,
			members: []string{"10.0.0.1", "10.0.0.1", "2001:db8::1"}, want: []string{"10.0.0.1/32"},
		},
		{
			name: "IPv6 addresses", family: netlink.FAMILY_V6, kind: IPSetTypeHashIP,
			members: []string{"2001:db8::1", "10.0.0.1"}, want: []string{"2001:db8::1/128"},
		},
		{
			name: "IPv4 default route", family: netlink.FAMILY_V4, kind: IPSetTypeHashNet,
			members: []string{"0.0.0.0/0"}, want: []string{"0.0.0.0/1", "128.0.0.0/1"},
		},
		{
			name: "IPv6 default route", family: netlink.FAMILY_V6, kind: IPSetTypeHashNet,
			members: []string{"::/0"}, want: []string{"8000::/1", "::/1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, fake := newTestIPSets(tc.family)
			manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "test", Type: tc.kind, MaxSize: 1048576}, tc.members)
			require.NoError(t, manager.ApplyUpdates())
			set := fake.sets[manager.name("test")]
			require.EqualValues(t, tc.family, set.Family)
			require.EqualValues(t, 1048576, set.MaxElements)
			got := make([]string, 0, len(set.Entries))
			for _, entry := range set.Entries {
				got = append(got, entryKey(entry))
			}
			require.ElementsMatch(t, tc.want, got)
			fake.calls = nil
			require.NoError(t, manager.ApplyUpdates())
			for _, call := range fake.calls {
				require.False(t, strings.HasPrefix(call, "swap "), call)
			}
		})
	}
}

func TestIPSetAtomicReplacementAndRecovery(t *testing.T) {
	for _, operation := range []string{"create", "list-all", "flush", "add", "swap", "destroy"} {
		t.Run(operation, func(t *testing.T) {
			manager, fake := newTestIPSets(netlink.FAMILY_V4)
			metadata := IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet, MaxSize: 1048576}
			manager.AddOrReplaceIPSet(metadata, []string{"10.0.0.0/24"})
			require.NoError(t, manager.ApplyUpdates())
			fake.referenced["ovn40services"] = true
			manager.AddOrReplaceIPSet(metadata, []string{"10.1.0.0/24"})
			fake.fail = operation
			require.ErrorIs(t, manager.ApplyUpdates(), fake.failure)
			require.Equal(t, "10.0.0.0/24", entryKey(fake.sets["ovn40services"].Entries[0]))
			fake.fail = ""
			require.NoError(t, manager.ApplyUpdates())
			require.Equal(t, "10.1.0.0/24", entryKey(fake.sets["ovn40services"].Entries[0]))
		})
	}
}

func TestIPSetGarbageCollection(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	for _, name := range []string{"ovn40stale", "ovn4t0", "ovn60services", "KUBE-CLUSTER-IP"} {
		fake.sets[name] = &netlink.IPSetResult{SetName: name}
	}
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, []string{"10.0.0.0/24"})
	require.NoError(t, manager.ApplyUpdates())
	require.Contains(t, fake.sets, "ovn40stale", "deletion must wait until iptables is reconciled")
	fake.referenced["ovn40stale"] = true
	require.ErrorIs(t, manager.ApplyDeletions(), unix.EBUSY)
	fake.referenced["ovn40stale"] = false
	require.NoError(t, manager.ApplyDeletions())
	require.NotContains(t, fake.sets, "ovn40stale")
	require.NotContains(t, fake.sets, "ovn4t0")
	require.Contains(t, fake.sets, "ovn60services")
	require.Contains(t, fake.sets, "KUBE-CLUSTER-IP")

	manager.RemoveIPSet("services")
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, nil)
	require.NoError(t, manager.ApplyUpdates())
	require.NoError(t, manager.ApplyDeletions())
	require.Contains(t, fake.sets, "ovn40services", "re-adding cancels deletion")
	require.Empty(t, fake.sets["ovn40services"].Entries)
	manager.RemoveIPSet("services")
	require.NoError(t, manager.ApplyDeletions())
	require.NotContains(t, fake.sets, "ovn40services")
	manager.RemoveIPSet("absent")
	require.NoError(t, manager.ApplyDeletions())
}

func TestIPSetTemporarySetRecovery(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	// A crash while writing a hash:ip temporary set must not prevent the
	// next reconciliation from replacing a hash:net set.
	fake.sets["ovn4t-replace"] = &netlink.IPSetResult{SetName: "ovn4t-replace", Family: netlink.FAMILY_V4, TypeName: IPSetTypeHashIP}
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, []string{"10.0.0.0/24"})
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "local-pod-ip-nat", Type: IPSetTypeHashIP}, []string{"10.1.0.1"})
	require.NoError(t, manager.ApplyUpdates())
	require.NoError(t, manager.ApplyDeletions())
	require.NotContains(t, fake.sets, "ovn4t-replace")
	// Recover external drift, including loss of the entire set.
	fake.sets["ovn40services"].Entries = nil
	delete(fake.sets, "ovn40local-pod-ip-nat")
	require.NoError(t, manager.ApplyUpdates())
	require.Len(t, fake.sets["ovn40services"].Entries, 1)
	require.Len(t, fake.sets["ovn40local-pod-ip-nat"].Entries, 1)
}

func TestIPSetInvalidMembersPreserveExistingSet(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	metadata := IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}
	manager.AddOrReplaceIPSet(metadata, []string{"10.0.0.0/24"})
	require.NoError(t, manager.ApplyUpdates())
	manager.AddOrReplaceIPSet(metadata, []string{"invalid"})
	require.ErrorContains(t, manager.ApplyUpdates(), "parse member")
	require.Equal(t, "10.0.0.0/24", entryKey(fake.sets["ovn40services"].Entries[0]))
	metadata.Type = "unsupported"
	manager.AddOrReplaceIPSet(metadata, nil)
	require.ErrorContains(t, manager.ApplyUpdates(), "unsupported set type")
}

func TestIPSetNameCompatibility(t *testing.T) {
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		manager, _ := newTestIPSets(family)
		version := "4"
		if family == netlink.FAMILY_V6 {
			version = "6"
		}
		require.Equal(t, "ovn"+version+"0subnets-distributed-gw", manager.name("subnets-distributed-gw"))
		require.Len(t, manager.name(strings.Repeat("a", 40)), 31)
	}
}

func TestIPSetHostMaskComparison(t *testing.T) {
	for _, address := range []string{"10.0.0.1", "2001:db8::1"} {
		ip := net.ParseIP(address)
		bits := uint8(128)
		if ip.To4() != nil {
			bits = 32
		}
		t.Run(fmt.Sprintf("%s/%d", address, bits), func(t *testing.T) {
			require.True(t, equalEntries([]netlink.IPSetEntry{{IP: ip}}, []netlink.IPSetEntry{{IP: ip, CIDR: bits}}))
		})
	}
}

func TestIPSetCleanup(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	for _, name := range []string{"ovn40services", "ovn4t0", "ovn4t-replace", "ovn60services", "KUBE-CLUSTER-IP"} {
		fake.sets[name] = &netlink.IPSetResult{SetName: name}
	}
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, nil)
	fake.fail = "list-all"
	require.ErrorIs(t, manager.Cleanup(), fake.failure)
	fake.fail = ""
	fake.referenced["ovn40services"] = true
	require.ErrorIs(t, manager.Cleanup(), unix.EBUSY)
	fake.referenced["ovn40services"] = false
	require.NoError(t, manager.Cleanup())
	require.Empty(t, manager.desired)
	require.Len(t, fake.sets, 2)
	require.Contains(t, fake.sets, "ovn60services")
	require.Contains(t, fake.sets, "KUBE-CLUSTER-IP")
	require.NoError(t, manager.Cleanup())
}

func TestIPSetInitialReadFailure(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, []string{"10.0.0.0/24"})
	fake.fail = "list"
	require.ErrorIs(t, manager.ApplyUpdates(), fake.failure)
	fake.fail = ""
	require.NoError(t, manager.ApplyUpdates())
	require.Equal(t, "10.0.0.0/24", entryKey(fake.sets["ovn40services"].Entries[0]))
}

func TestIPSetIncompatibleExistingSet(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	fake.sets["ovn40services"] = &netlink.IPSetResult{SetName: "ovn40services", Family: netlink.FAMILY_V6, TypeName: IPSetTypeHashIP}
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, nil)
	require.ErrorContains(t, manager.ApplyUpdates(), "incompatible existing set")
	require.EqualValues(t, netlink.FAMILY_V6, fake.sets["ovn40services"].Family)
}

func TestIPSetMultipartDump(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, []string{"10.0.0.0/24", "10.1.0.0/24"})
	require.NoError(t, manager.ApplyUpdates())
	first := *fake.sets["ovn40services"]
	second := first
	first.Entries = first.Entries[:1]
	second.Entries = second.Entries[1:]
	fake.dump = []netlink.IPSetResult{first, second}
	fake.calls = nil
	require.NoError(t, manager.ApplyUpdates())
	require.Equal(t, []string{"list-all "}, fake.calls, "unchanged sets need only one dump")
}

func TestIPSetCleanupFailureAfterSwap(t *testing.T) {
	manager, fake := newTestIPSets(netlink.FAMILY_V4)
	manager.AddOrReplaceIPSet(IPSetMetadata{SetID: "services", Type: IPSetTypeHashNet}, []string{"10.0.0.0/24"})
	fake.fail, fake.failAfter = "destroy", 1
	require.ErrorIs(t, manager.ApplyUpdates(), fake.failure)
	require.Equal(t, "10.0.0.0/24", entryKey(fake.sets["ovn40services"].Entries[0]))
	fake.fail = ""
	require.NoError(t, manager.ApplyUpdates())
	require.NoError(t, manager.ApplyDeletions())
	require.NotContains(t, fake.sets, "ovn4t-replace")
}
