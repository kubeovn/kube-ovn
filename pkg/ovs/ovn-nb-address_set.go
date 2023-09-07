package ovs

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/ovn-org/libovsdb/client"
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
		ExternalIDs: externalIDs,
	}

	ops, err := c.ovsDbClient.Create(as)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating address set %s: %v", asName, err)
	}

	if err = c.Transact("as-add", ops); err != nil {
		return fmt.Errorf("create address set %s: %v", asName, err)
	}

	return nil
}

// AddressSetUpdateAddress update addresses,
// clear addresses when addresses is empty
func (c *OVNNbClient) AddressSetUpdateAddress(asName string, addresses ...string) error {
	as, err := c.GetAddressSet(asName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get address set %s: %v", asName, err)
	}

	// format CIDR to keep addresses the same in both nb and sb
	for i, addr := range addresses {
		if strings.ContainsRune(addr, '/') {
			_, ipNet, err := net.ParseCIDR(addr)
			if err != nil {
				klog.Warningf("failed to parse CIDR %q: %v", addr, err)
				continue
			}
			addresses[i] = ipNet.String()
		}
	}

	// update will failed when slice contains duplicate elements
	addresses = strset.New(addresses...).List()

	// clear addresses when addresses is empty
	as.Addresses = addresses

	if err := c.UpdateAddressSet(as, &as.Addresses); err != nil {
		return fmt.Errorf("set address set %s addresses %v: %v", asName, addresses, err)
	}

	return nil
}

// UpdateAddressSet update address set
func (c *OVNNbClient) UpdateAddressSet(as *ovnnb.AddressSet, fields ...interface{}) error {
	if as == nil {
		return fmt.Errorf("address_set is nil")
	}

	op, err := c.Where(as).Update(as, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for updating address set %s: %v", as.Name, err)
	}

	if err = c.Transact("as-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("update address set %s: %v", as.Name, err)
	}

	return nil
}

func (c *OVNNbClient) DeleteAddressSet(asName string) error {
	as, err := c.GetAddressSet(asName, true)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get address set %s: %v", asName, err)
	}

	// not found, skip
	if as == nil {
		return nil
	}

	op, err := c.Where(as).Delete()
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := c.Transact("as-del", op); err != nil {
		return fmt.Errorf("delete address set %s: %v", asName, err)
	}

	return nil
}

// DeleteAddressSets delete several address set once
func (c *OVNNbClient) DeleteAddressSets(externalIDs map[string]string) error {
	// it's dangerous when externalIDs is empty, it will delete all address set
	if len(externalIDs) == 0 {
		return nil
	}

	op, err := c.WhereCache(addressSetFilter(externalIDs)).Delete()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operation for deleting address sets with external IDs %v: %v", externalIDs, err)
	}

	if err := c.Transact("ass-del", op); err != nil {
		return fmt.Errorf("delete address sets with external IDs %v: %v", externalIDs, err)
	}

	return nil
}

// GetAddressSet get address set by name
func (c *OVNNbClient) GetAddressSet(asName string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	as := &ovnnb.AddressSet{Name: asName}
	if err := c.ovsDbClient.Get(ctx, as); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		klog.Error(err)
		return nil, fmt.Errorf("get address set %s: %v", asName, err)
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

	if err := c.WhereCache(addressSetFilter(externalIDs)).List(ctx, &asList); err != nil {
		return nil, fmt.Errorf("list address set: %v", err)
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
