package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateHAChassisGroup adds or updates the ha chassis group
func (c *OVNNbClient) CreateHAChassisGroup(name string, chassises []string, externalIDs map[string]string) error {
	group, err := c.GetHAChassisGroup(name, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	var ops []ovsdb.Operation
	if group == nil {
		group = &ovnnb.HAChassisGroup{
			UUID:        ovsclient.NamedUUID(),
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}
		maps.Insert(group.ExternalIDs, maps.All(externalIDs))
		createOps, err := c.Create(group)
		if err != nil {
			klog.Error(err)
			return err
		}
		ops = append(ops, createOps...)
	} else {
		group.ExternalIDs = map[string]string{"vendor": util.CniTypeName}
		maps.Insert(group.ExternalIDs, maps.All(externalIDs))
		updateOps, err := c.Where(group).Update(group, &group.ExternalIDs)
		if err != nil {
			klog.Error(err)
			return err
		}
		ops = append(ops, updateOps...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	haChassises := make([]*ovnnb.HAChassis, 0, max(len(group.HaChassis), len(chassises)))
	if len(group.HaChassis) != 0 {
		if err = c.ovsDbClient.WhereCache(func(c *ovnnb.HAChassis) bool {
			return slices.Contains(group.HaChassis, c.UUID)
		}).List(ctx, &haChassises); err != nil {
			klog.Error(err)
			return err
		}
	}

	priorityMap := make(map[string]int, len(chassises))
	for i, chassis := range chassises {
		priorityMap[chassis] = 100 - i
	}

	uuids := make([]string, 0, len(group.HaChassis))
	for _, chassis := range haChassises {
		if priority, ok := priorityMap[chassis.ChassisName]; ok {
			delete(priorityMap, chassis.ChassisName)
			if chassis.Priority != priority {
				// update ha chassis priority
				chassis.Priority = priority
				updateOps, err := c.Where(chassis).Update(chassis, &chassis.Priority)
				if err != nil {
					klog.Error(err)
					return err
				}
				ops = append(ops, updateOps...)
			}
		} else {
			uuids = append(uuids, chassis.UUID)
		}
	}
	if len(uuids) != 0 {
		// delete ha chassis from the group
		deleteOps, err := c.Where(group).Mutate(group, model.Mutation{
			Field:   &group.HaChassis,
			Value:   uuids,
			Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			klog.Error(err)
			return err
		}
		ops = append(ops, deleteOps...)
	}

	// add new ha chassis to the group
	for chassis, priority := range priorityMap {
		haChassis := &ovnnb.HAChassis{
			UUID:        ovsclient.NamedUUID(),
			ChassisName: chassis,
			Priority:    priority,
			ExternalIDs: map[string]string{"group": name, "vendor": util.CniTypeName},
		}
		createOps, err := c.Create(haChassis)
		if err != nil {
			klog.Error(err)
			return err
		}
		insertOps, err := c.Where(group).Mutate(group, model.Mutation{
			Field:   &group.HaChassis,
			Value:   []string{haChassis.UUID},
			Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			klog.Error(err)
			return err
		}
		ops = append(ops, createOps...)
		ops = append(ops, insertOps...)
	}

	if err = c.Transact("ha-chassis-group-add", ops); err != nil {
		err := fmt.Errorf("failed to add/update HA chassis group %s: %w", name, err)
		klog.Error(err)
		return err
	}
	return nil
}

// GetHAChassisGroup gets the ha chassis group
func (c *OVNNbClient) GetHAChassisGroup(name string, ignoreNotFound bool) (*ovnnb.HAChassisGroup, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	group := &ovnnb.HAChassisGroup{Name: name}
	if err := c.Get(ctx, group); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get HA chassis group %q: %w", name, err)
	}

	return group, nil
}

// DeleteHAChassisGroup deletes the ha chassis group
func (c *OVNNbClient) DeleteHAChassisGroup(name string) error {
	group, err := c.GetHAChassisGroup(name, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if group == nil {
		return nil
	}

	ops, err := c.Where(group).Delete()
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("ha-chassis-group-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to delete HA chassis group %q: %w", name, err)
	}

	return nil
}
