package ovs

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c Client) CreateAddressSet(name string, addresses []string, externalIDs map[string]string) error {
	as, err := c.GetAddressSet(name, true)
	if err != nil {
		return err
	}

	var ops []ovsdb.Operation
	if as != nil {
		fields := make([]interface{}, 0, 2)
		sort.Strings(addresses)
		sort.Strings(as.Addresses)
		if !reflect.DeepEqual(addresses, as.Addresses) {
			as.Addresses = addresses
			fields = append(fields, &as.Addresses)
		}
		if !reflect.DeepEqual(externalIDs, as.ExternalIDs) {
			as.ExternalIDs = externalIDs
			fields = append(fields, &as.ExternalIDs)
		}
		if ops, err = c.nbClient.Where(as).Update(as, fields...); err != nil {
			return fmt.Errorf("failed to generate update operations for address set %s", name)
		}
	} else {
		as = &ovnnb.AddressSet{Name: name, Addresses: addresses, ExternalIDs: externalIDs}
		if ops, err = c.nbClient.Create(as); err != nil {
			return fmt.Errorf("failed to generate create operations for address set %s", name)
		}
	}

	if err = Transact(c.nbClient, "create", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create address set %s: %v", name, err)
	}

	return nil
}

func (c Client) CreateNpAddressSet(name, npNamespace, npName, direction string) error {
	return c.CreateAddressSet(name, nil, map[string]string{"np": fmt.Sprintf("%s/%s/%s", npNamespace, npName, direction)})
}

func (c Client) CreateSgAssociatedAddressSet(sgName string) error {
	if err := c.CreateAddressSet(SgV4AssociatedName(sgName), nil, map[string]string{"sg": sgName}); err != nil {
		klog.Errorf("failed to create v4 address_set for sg %s: %v", sgName, err)
		return err
	}
	if err := c.CreateAddressSet(SgV6AssociatedName(sgName), nil, map[string]string{"sg": sgName}); err != nil {
		klog.Errorf("failed to create v6 address_set for sg %s: %v", sgName, err)
		return err
	}
	return nil
}

func (c Client) ListNpAddressSet(npNamespace, npName, direction string) ([]string, error) {
	asList := make([]ovnnb.AddressSet, 0, 1)
	s := fmt.Sprintf("%s/%s/%s", npNamespace, npName, direction)
	if err := c.nbClient.WhereCache(func(as *ovnnb.AddressSet) bool { return len(as.ExternalIDs) != 0 && as.ExternalIDs["np"] == s }).List(context.TODO(), &asList); err != nil {
		return nil, fmt.Errorf("failed to list address set: %v", err)
	}

	result := make([]string, 0, len(asList))
	for _, as := range asList {
		result = append(result, as.Name)
	}
	return result, nil
}

func (c Client) ListSgRuleAddressSet(sgName string, direction ovnnb.ACLDirection) ([]string, error) {
	var msg string
	var predicate interface{}
	if direction == "" {
		predicate = func(as *ovnnb.AddressSet) bool {
			return len(as.ExternalIDs) != 0 && as.ExternalIDs["sg"] == sgName
		}
	} else {
		msg = fmt.Sprintf(" and external_ids:direction=%s", direction)
		predicate = func(as *ovnnb.AddressSet) bool {
			return len(as.ExternalIDs) != 0 && as.ExternalIDs["sg"] == sgName && as.ExternalIDs["direction"] == string(direction)
		}
	}

	asList := make([]ovnnb.AddressSet, 0)
	if err := c.nbClient.WhereCache(predicate).List(context.TODO(), &asList); err != nil {
		return nil, fmt.Errorf("failed to list address set with external_ids:sg=%s%s: %v", sgName, msg, err)
	}

	result := make([]string, 0, len(asList))
	for _, as := range asList {
		result = append(result, as.Name)
	}
	return result, nil
}

func (c Client) UpdateAddressSetAddresses(name string, addresses []string) error {
	as, err := c.GetAddressSet(name, false)
	if err != nil {
		return err
	}

	sort.Strings(addresses)
	sort.Strings(as.Addresses)
	if reflect.DeepEqual(addresses, as.Addresses) {
		return nil
	}

	as.Addresses = addresses
	ops, err := c.nbClient.Where(as).Update(as, as.Addresses)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for address set %s: %v", as, err)
	}
	if err = Transact(c.nbClient, "update", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to update addresses of address set %s: %v", as, err)
	}

	return nil
}

func (c Client) AddAddressSetAddresses(name string, address string) error {
	as, err := c.GetAddressSet(name, false)
	if err != nil {
		return err
	}
	if util.ContainsString(as.Addresses, address) {
		return nil
	}

	as.Addresses = append(as.Addresses, address)
	ops, err := c.nbClient.Where(as).Update(as, as.Addresses)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for address set %s: %v", as, err)
	}
	if err = Transact(c.nbClient, "update", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to update addresses of address set %s: %v", as, err)
	}

	return nil
}

func (c Client) RemoveAddressSetAddresses(name string, address string) error {
	as, err := c.GetAddressSet(name, false)
	if err != nil {
		return err
	}
	if !util.ContainsString(as.Addresses, address) {
		return nil
	}

	as.Addresses = util.RemoveString(as.Addresses, address)
	ops, err := c.nbClient.Where(as).Update(as, as.Addresses)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for address set %s: %v", as, err)
	}
	if err = Transact(c.nbClient, "update", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to update addresses of address set %s: %v", as, err)
	}

	return nil
}

func (c Client) DeleteAddressSet(name string) error {
	as, err := c.GetAddressSet(name, true)
	if err != nil {
		return err
	}
	if as == nil {
		return nil
	}

	ops, err := c.nbClient.Where(as).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for address set %s", name)
	}
	if err = Transact(c.nbClient, "destroy", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete address set %s: %v", name, err)
	}

	return nil
}

func (c Client) GetAddressSet(name string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	asList := make([]ovnnb.AddressSet, 0, 1)
	if err := c.nbClient.WhereCache(func(as *ovnnb.AddressSet) bool { return as.Name == name }).List(context.TODO(), &asList); err != nil {
		return nil, fmt.Errorf("failed to list address set with name %s: %v", name, err)
	}
	if len(asList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("address set %s does not exist", name)
	}
	if len(asList) != 1 {
		return nil, fmt.Errorf("found multiple address sets with the same name %s", name)
	}

	return &asList[0], nil
}
