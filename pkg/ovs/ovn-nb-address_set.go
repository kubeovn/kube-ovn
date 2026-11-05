package ovs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateAddressSet create address set with external ids
func (c *OVNNbClient) CreateAddressSet(asName string, externalIDs map[string]string) error {
	// ovn acl doesn't support address_set name with '-'
	if matched := matchAddressSetName(asName); !matched {
		return fmt.Errorf("address set %s must match `[a-zA-Z_.][a-zA-Z_.0-9]*`", asName)
	}

	as := &ovnnb.AddressSet{
		Name:        asName,
		ExternalIDs: clonedVendorIDs(externalIDs),
	}
	return logErr(c.addressSetTable().createIfAbsent(asName, "as-add", as))
}

// normalizeAddresses formats CIDR addresses to keep them the same in both nb and sb,
// and drops duplicate elements which would make the update fail. An address whose CIDR
// cannot be parsed is kept as is. The returned set is order independent, so it can be
// compared against the addresses currently stored in the database.
// The given slice is never modified: it may be owned by the caller.
func normalizeAddresses(addresses []string) *strset.Set {
	result := strset.NewWithSize(len(addresses))
	for _, addr := range addresses {
		if strings.ContainsRune(addr, '/') {
			if _, ipNet, err := net.ParseCIDR(addr); err != nil {
				klog.Warningf("failed to parse CIDR %q: %v", addr, err)
			} else {
				addr = ipNet.String()
			}
		}
		result.Add(addr)
	}
	return result
}

// AddressSetUpdateAddress update addresses,
// clear addresses when addresses is empty
func (c *OVNNbClient) AddressSetUpdateAddress(asName string, addresses ...string) error {
	as, err := c.GetAddressSet(asName, false)
	if err != nil {
		return logWrap(err, wrapErr("get address set %s: %w", asName))
	}

	// the current addresses are read from the local ovsdb cache, so comparing them
	// costs nothing and saves a needless nb transaction when nothing has changed
	expected := normalizeAddresses(addresses)
	if expected.IsEqual(strset.New(as.Addresses...)) {
		return nil
	}

	// clear addresses when addresses is empty
	as.Addresses = expected.List()

	if err := c.UpdateAddressSet(as, &as.Addresses); err != nil {
		return logWrap(err, wrapErr("set address set %s addresses %v: %w", asName, as.Addresses))
	}

	return nil
}

// UpdateAddressSet update address set
func (c *OVNNbClient) UpdateAddressSet(as *ovnnb.AddressSet, fields ...any) error {
	if as == nil {
		return errors.New("address_set is nil")
	}

	return c.updateModelLogged("as-update", as, func(err error) error {
		return fmt.Errorf("update address set %s: %w", as.Name, err)
	}, fields...)
}

func (c *OVNNbClient) DeleteAddressSet(asName ...string) error {
	return deleteNamedRows(c, asName, c.GetAddressSet, &ovnnb.AddressSet{}, "as-del",
		"get address set %s when delete: %w", "delete address set %s: %w")
}

// BatchDeleteAddressSetByAsName batch delete address set by names
func (c *OVNNbClient) BatchDeleteAddressSetByNames(asNames []string) error {
	asNameMap := make(map[string]struct{}, len(asNames))
	for _, name := range asNames {
		asNameMap[name] = struct{}{}
	}
	asList, err := filterLogged(c.Database, &ovnnb.AddressSet{}, func(as *ovnnb.AddressSet) bool {
		_, exist := asNameMap[as.Name]
		return exist
	}, func(err error) error {
		return fmt.Errorf("batch delete address set %d list failed: %w", len(asNames), err)
	})
	if err != nil {
		return err
	}
	// not found, skip
	if len(asList) == 0 {
		return nil
	}
	if err := c.Database.Table(&ovnnb.AddressSet{}).Delete(context.Background(), "as-del", modelsFrom(asList)...); err != nil {
		return fmt.Errorf("batch delete address set %d failed: %w", len(asList), err)
	}

	return nil
}

// DeleteAddressSets delete several address set once
func (c *OVNNbClient) DeleteAddressSets(externalIDs map[string]string) error {
	// it's dangerous when externalIDs is empty, it will delete all address set
	if len(externalIDs) == 0 {
		return nil
	}

	if err := c.Database.Table(&ovnnb.AddressSet{}).DeleteFilter(context.Background(), "ass-del", addressSetFilter(externalIDs)); err != nil {
		return logWrap(err, wrapErr("delete address sets with external IDs %v: %w", externalIDs))
	}

	return nil
}

// GetAddressSet get address set by name
func (c *OVNNbClient) GetAddressSet(asName string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	return getIndexedWrap(c.Database, &ovnnb.AddressSet{Name: asName}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("get address set %s: %w", asName, err)
	})
}

func (c *OVNNbClient) AddressSetExists(name string) (bool, error) {
	return existsByGet(c.GetAddressSet, name)
}

// ListAddressSets list address set by external_ids
func (c *OVNNbClient) ListAddressSets(externalIDs map[string]string) ([]ovnnb.AddressSet, error) {
	return filterLogged(c.Database, &ovnnb.AddressSet{}, addressSetFilter(externalIDs), func(err error) error {
		return fmt.Errorf("list address set: %w", err)
	})
}

// addressSetFilter filter address set which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
func addressSetFilter(externalIDs map[string]string) func(as *ovnnb.AddressSet) bool {
	return func(as *ovnnb.AddressSet) bool {
		return matchExternalIDs(as.ExternalIDs, externalIDs)
	}
}

func (c *OVNNbClient) addressSetTable() namedTable[ovnnb.AddressSet] {
	return newNamedTable(c.Database, &ovnnb.AddressSet{}, "address set",
		func(as *ovnnb.AddressSet) string { return as.Name },
		func(as *ovnnb.AddressSet) map[string]string { return as.ExternalIDs })
}
