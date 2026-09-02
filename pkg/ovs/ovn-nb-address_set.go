package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateAddressSet create address set with external ids
func (c *OVNNbClient) CreateAddressSet(asName string, externalIDs map[string]string) error {
	// ovn acl doesn't support address_set name with '-'
	if matched := matchAddressSetName(asName); !matched {
		return fmt.Errorf("address set %s must match `[a-zA-Z_.][a-zA-Z_.0-9]*`", asName)
	}

	// Create new map with vendor tag to avoid modifying caller's map
	finalExternalIDs := make(map[string]string, len(externalIDs)+1)
	maps.Copy(finalExternalIDs, externalIDs)
	finalExternalIDs["vendor"] = util.CniTypeName

	exists, err := c.AddressSetExists(asName)
	if err != nil {
		klog.Error(err)
		return err
	}

	// found, ignore
	if exists {
		return nil
	}

	as := &ovnnb.AddressSet{
		Name:        asName,
		ExternalIDs: finalExternalIDs,
	}

	if err = c.callLayer().CreateAndTransact(context.Background(), "as-add", as); err != nil {
		klog.Error(err)
		return fmt.Errorf("create address set %s: %w", asName, err)
	}

	return nil
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
		klog.Error(err)
		return fmt.Errorf("get address set %s: %w", asName, err)
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
		klog.Error(err)
		return fmt.Errorf("set address set %s addresses %v: %w", asName, as.Addresses, err)
	}

	return nil
}

// UpdateAddressSet update address set
func (c *OVNNbClient) UpdateAddressSet(as *ovnnb.AddressSet, fields ...any) error {
	if as == nil {
		return errors.New("address_set is nil")
	}

	if err := c.callLayer().UpdateAndTransact(context.Background(), "as-update", as, as, fields...); err != nil {
		klog.Error(err)
		return fmt.Errorf("update address set %s: %w", as.Name, err)
	}

	return nil
}

func (c *OVNNbClient) DeleteAddressSet(asName ...string) error {
	delList := make([]*ovnnb.AddressSet, 0, len(asName))
	for _, name := range asName {
		// get address set
		as, err := c.GetAddressSet(name, true)
		if err != nil {
			return fmt.Errorf("get address set %s when delete: %w", name, err)
		}
		// not found, skip
		if as == nil {
			continue
		}
		delList = append(delList, as)
	}
	if len(delList) == 0 {
		return nil
	}

	modelList := make([]model.Model, len(delList))
	for i, as := range delList {
		modelList[i] = as
	}
	if err := c.callLayer().DeleteAndTransact(context.Background(), "as-del", modelList...); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete address set %s: %w", asName, err)
	}

	return nil
}

// BatchDeleteAddressSetByAsName batch delete address set by names
func (c *OVNNbClient) BatchDeleteAddressSetByNames(asNames []string) error {
	asNameMap := make(map[string]struct{}, len(asNames))
	for _, name := range asNames {
		asNameMap[name] = struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	asList := make([]ovnnb.AddressSet, 0)
	if err := c.callLayer().ListWhereCache(ctx, func(as *ovnnb.AddressSet) bool {
		_, exist := asNameMap[as.Name]
		return exist
	}, &asList); err != nil {
		klog.Error(err)
		return fmt.Errorf("batch delete address set %d list failed: %w", len(asNames), err)
	}

	// not found, skip
	if len(asList) == 0 {
		return nil
	}

	modelList := make([]model.Model, 0, len(asList))
	for _, as := range asList {
		modelList = append(modelList, &as)
	}
	if err := c.callLayer().DeleteAndTransact(context.Background(), "as-del", modelList...); err != nil {
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

	if err := c.callLayer().DeleteWhereCacheAndTransact(context.Background(), "ass-del", addressSetFilter(externalIDs)); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete address sets with external IDs %v: %w", externalIDs, err)
	}

	return nil
}

// GetAddressSet get address set by name
func (c *OVNNbClient) GetAddressSet(asName string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	as := &ovnnb.AddressSet{Name: asName}
	if err := c.callLayer().Get(ctx, as); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		klog.Error(err)
		return nil, fmt.Errorf("get address set %s: %w", asName, err)
	}

	return as, nil
}

func (c *OVNNbClient) AddressSetExists(name string) (bool, error) {
	as, err := c.GetAddressSet(name, true)
	return as != nil, err
}

// ListAddressSets list address set by external_ids
func (c *OVNNbClient) ListAddressSets(externalIDs map[string]string) ([]ovnnb.AddressSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	asList := make([]ovnnb.AddressSet, 0)

	if err := c.callLayer().ListWhereCache(ctx, addressSetFilter(externalIDs), &asList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list address set: %w", err)
	}

	return asList, nil
}

// addressSetFilter filter address set which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
func addressSetFilter(externalIDs map[string]string) func(as *ovnnb.AddressSet) bool {
	return func(as *ovnnb.AddressSet) bool {
		if len(as.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(as.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this lsp,
				// it's equal to shell command `ovn-nbctl --columns=xx find address_set external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(as.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if as.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		return true
	}
}
