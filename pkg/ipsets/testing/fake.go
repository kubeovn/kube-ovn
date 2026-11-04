//go:build linux

package testing

import (
	"errors"
	"slices"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// FakeNetlink provides an in-memory implementation of NetlinkIPSets for testing.
type FakeNetlink struct {
	Sets       map[string]*netlink.IPSetResult
	Calls      []string
	Fail       string
	Failure    error
	Dump       []netlink.IPSetResult
	FailAfter  int
	Referenced map[string]bool
}

func NewFakeNetlink() *FakeNetlink {
	return &FakeNetlink{
		Sets:       make(map[string]*netlink.IPSetResult),
		Failure:    errors.New("netlink failure"),
		Referenced: make(map[string]bool),
	}
}

func (f *FakeNetlink) call(operation, name string) error {
	f.Calls = append(f.Calls, operation+" "+name)
	if f.Fail == operation {
		if f.FailAfter == 0 {
			return f.Failure
		}
		f.FailAfter--
	}
	return nil
}

func (f *FakeNetlink) IpsetCreate(name, kind string, options netlink.IpsetCreateOptions) error {
	if err := f.call("create", name); err != nil {
		return err
	}
	if existing := f.Sets[name]; existing != nil {
		if existing.TypeName != kind || existing.Family != options.Family {
			return unix.EEXIST
		}
		return nil
	}
	f.Sets[name] = &netlink.IPSetResult{
		SetName: name, TypeName: kind, Family: options.Family,
		MaxElements: options.MaxElements, Revision: options.Revision,
	}
	return nil
}

func (f *FakeNetlink) IpsetList(name string) (*netlink.IPSetResult, error) {
	if err := f.call("list", name); err != nil {
		return nil, err
	}
	if existing := f.Sets[name]; existing != nil {
		result := *existing
		result.Entries = slices.Clone(existing.Entries)
		return &result, nil
	}
	return nil, unix.ENOENT
}

func (f *FakeNetlink) IpsetListAll() ([]netlink.IPSetResult, error) {
	if err := f.call("list-all", ""); err != nil {
		return nil, err
	}
	if f.Dump != nil {
		return f.Dump, nil
	}
	results := make([]netlink.IPSetResult, 0, len(f.Sets))
	for _, set := range f.Sets {
		results = append(results, *set)
	}
	return results, nil
}

func (f *FakeNetlink) IpsetFlush(name string) error {
	if err := f.call("flush", name); err != nil {
		return err
	}
	f.Sets[name].Entries = nil
	return nil
}

func (f *FakeNetlink) IpsetAdd(name string, entry *netlink.IPSetEntry) error {
	if err := f.call("add", name); err != nil {
		return err
	}
	f.Sets[name].Entries = append(f.Sets[name].Entries, *entry)
	return nil
}

func (f *FakeNetlink) IpsetSwap(a, b string) error {
	if err := f.call("swap", b); err != nil {
		return err
	}
	if f.Sets[a].TypeName != f.Sets[b].TypeName || f.Sets[a].Family != f.Sets[b].Family {
		return unix.EINVAL
	}
	f.Sets[a].Entries, f.Sets[b].Entries = f.Sets[b].Entries, f.Sets[a].Entries
	f.Sets[a].MaxElements, f.Sets[b].MaxElements = f.Sets[b].MaxElements, f.Sets[a].MaxElements
	return nil
}

func (f *FakeNetlink) IpsetDestroy(name string) error {
	if err := f.call("destroy", name); err != nil {
		return err
	}
	if f.Sets[name] == nil {
		return unix.ENOENT
	}
	if f.Referenced[name] {
		return unix.EBUSY
	}
	delete(f.Sets, name)
	return nil
}
