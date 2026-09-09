package ovs

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateHAChassisGroup adds or updates the ha chassis group
func (c *OVNNbClient) CreateHAChassisGroup(name string, chassises []string, externalIDs map[string]string) error {
	group, err := c.GetHAChassisGroup(name, true)
	if err != nil {
		return logErr(err)
	}

	var ops []ovsdb.Operation
	if group == nil {
		group = &ovnnb.HAChassisGroup{
			UUID:        ovsclient.NamedUUID(),
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}
		maps.Insert(group.ExternalIDs, maps.All(externalIDs))
		createOps, err := c.Database.Table(&ovnnb.HAChassisGroup{}).CreateOps(group)
		if err != nil {
			return logErr(err)
		}
		ops = append(ops, createOps...)
	} else {
		group.ExternalIDs = map[string]string{"vendor": util.CniTypeName}
		maps.Insert(group.ExternalIDs, maps.All(externalIDs))
		updateOps, err := c.Database.Table(&ovnnb.HAChassisGroup{}).UpdateOps(group, group, &group.ExternalIDs)
		if err != nil {
			return logErr(err)
		}
		ops = append(ops, updateOps...)
	}

	var haChassises []*ovnnb.HAChassis
	if len(group.HaChassis) != 0 {
		rows, err := filterLogged(c.Database, &ovnnb.HAChassis{}, func(chassis *ovnnb.HAChassis) bool {
			return slices.Contains(group.HaChassis, chassis.UUID)
		}, nil)
		if err != nil {
			return err
		}
		haChassises = pointersOf(rows)
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
				updateOps, err := c.Database.Table(&ovnnb.HAChassis{}).UpdateOps(chassis, chassis, &chassis.Priority)
				if err != nil {
					return logErr(err)
				}
				ops = append(ops, updateOps...)
			}
		} else {
			uuids = append(uuids, chassis.UUID)
		}
	}
	if len(uuids) != 0 {
		// delete ha chassis from the group
		deleteOps, err := c.Database.Table(&ovnnb.HAChassisGroup{}).MutateOps(group, model.Mutation{
			Field:   &group.HaChassis,
			Value:   uuids,
			Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return logErr(err)
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
		createOps, err := c.Database.Table(&ovnnb.HAChassis{}).CreateOps(haChassis)
		if err != nil {
			return logErr(err)
		}
		insertOps, err := c.Database.Table(&ovnnb.HAChassisGroup{}).MutateOps(group, model.Mutation{
			Field:   &group.HaChassis,
			Value:   []string{haChassis.UUID},
			Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return logErr(err)
		}
		ops = append(ops, createOps...)
		ops = append(ops, insertOps...)
	}

	return c.transactGenerated("ha-chassis-group-add", ops, nil, nil,
		wrapErr("failed to add/update HA chassis group %s: %w", name),
	)
}

// GetHAChassisGroup gets the ha chassis group
func (c *OVNNbClient) GetHAChassisGroup(name string, ignoreNotFound bool) (*ovnnb.HAChassisGroup, error) {
	return getIndexedFmt(c.Database, &ovnnb.HAChassisGroup{Name: name}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("failed to get HA chassis group %q: %w", name, err)
	})
}

// DeleteHAChassisGroup deletes the ha chassis group
func (c *OVNNbClient) DeleteHAChassisGroup(name string) error {
	group, err := c.GetHAChassisGroup(name, true)
	if err != nil {
		return logErr(err)
	}
	if group == nil {
		return nil
	}

	ops, err := c.Database.Table(&ovnnb.HAChassisGroup{}).DeleteOps(group)
	return c.transactGenerated("ha-chassis-group-del", ops, err, nil,
		wrapErr("failed to delete HA chassis group %q: %w", name),
	)
}
