package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateAddressSet create address set with external ids
func (c OvnClient) CreateAddressSet(asName string, externalIDs map[string]string) error {
	as, err := c.GetAddressSet(asName, true)
	if err != nil {
		return err
	}

	// found, ingore
	if as != nil {
		return nil
	}

	as = &ovnnb.AddressSet{
		Name:        asName,
		ExternalIDs: externalIDs,
	}

	ops, err := c.ovnNbClient.Create(as)
	if err != nil {
		return fmt.Errorf("generate operations for creating address set %s: %v", asName, err)
	}

	if err = c.Transact("as-add", ops); err != nil {
		return fmt.Errorf("create address set %s: %v", asName, err)
	}

	return nil
}

// AddressSetUpdateAddress update addresses,
// clear addresses when addresses is empty
func (c OvnClient) AddressSetUpdateAddress(asName string, addresses ...string) error {
	as, err := c.GetAddressSet(asName, false)
	if err != nil {
		return fmt.Errorf("get address set %s: %v", asName, err)
	}

	// clear addresses when addresses is empty
	as.Addresses = addresses

	if err := c.UpdateAddressSet(as, &as.Addresses); err != nil {
		return fmt.Errorf("set address set %s addresses %v: %v", asName, addresses, err)
	}

	return nil
}

// UpdateAddressSet update address set
func (c OvnClient) UpdateAddressSet(as *ovnnb.AddressSet, fields ...interface{}) error {
	if as == nil {
		return fmt.Errorf("address_set is nil")
	}

	op, err := c.Where(as).Update(as, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating address set %s: %v", as.Name, err)
	}

	if err = c.Transact("as-update", op); err != nil {
		return fmt.Errorf("update address set %s: %v", as.Name, err)
	}

	return nil
}

func (c OvnClient) DeleteAddressSet(asName string) error {
	as, err := c.GetAddressSet(asName, true)
	if err != nil {
		return fmt.Errorf("get address set %s: %v", asName, err)
	}

	// not found, skip
	if as == nil {
		return nil
	}

	op, err := c.Where(as).Delete()
	if err != nil {
		return err
	}

	if err := c.Transact("as-del", op); err != nil {
		return fmt.Errorf("delete address set %s: %v", asName, err)
	}

	return nil
}

// GetAddressSet get address set by name
func (c OvnClient) GetAddressSet(asName string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	as := &ovnnb.AddressSet{Name: asName}
	if err := c.ovnNbClient.Get(context.TODO(), as); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get address set %s: %v", asName, err)
	}

	return as, nil
}

// ListAddressSets list address set by external_ids
func (c OvnClient) ListAddressSets(externalIDs map[string]string) ([]ovnnb.AddressSet, error) {
	asList := make([]ovnnb.AddressSet, 0)

	if err := c.WhereCache(func(as *ovnnb.AddressSet) bool {
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
	}).List(context.TODO(), &asList); err != nil {
		return nil, fmt.Errorf("list address set: %v", err)
	}

	return asList, nil
}
