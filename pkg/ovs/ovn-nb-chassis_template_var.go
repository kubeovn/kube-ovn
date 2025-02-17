package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *OVNNbClient) ListChassisTemplateVar() ([]ovnnb.ChassisTemplateVar, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var ctvList []ovnnb.ChassisTemplateVar
	if err := c.ovsDbClient.List(ctx, &ctvList); err != nil {
		err := fmt.Errorf("failed to list ChassisTemplateVar: %w", err)
		klog.Error(err)
		return nil, err
	}

	return ctvList, nil
}

func (c *OVNNbClient) GetChassisTemplateVar(chassis string, ignoreNotFound bool) (*ovnnb.ChassisTemplateVar, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	ctv := &ovnnb.ChassisTemplateVar{Chassis: chassis}
	if err := c.ovsDbClient.Get(ctx, ctv); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		err := fmt.Errorf("failed to get ChassisTemplateVar %s: %w", chassis, err)
		klog.Error(err)
		return nil, err
	}

	return ctv, nil
}

func (c *OVNNbClient) GetChassisTemplateVarByNodeName(node string, ignoreNotFound bool) (*ovnnb.ChassisTemplateVar, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	ctvList := make([]ovnnb.ChassisTemplateVar, 0, 1)
	if err := c.ovsDbClient.WhereCache(func(v *ovnnb.ChassisTemplateVar) bool {
		return v.ExternalIDs["vendor"] == util.CniTypeName && v.ExternalIDs["node"] == node
	}).List(ctx, &ctvList); err != nil {
		err := fmt.Errorf("failed to get ChassisTemplateVar of node %s: %w", node, err)
		klog.Error(err)
		return nil, err
	}

	if len(ctvList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("ChassisTemplateVar of node %s not found", node)
	}
	if len(ctvList) > 1 {
		return nil, fmt.Errorf("found more than one ChassisTemplateVar of node %s", node)
	}

	return &ctvList[0], nil
}

func (c *OVNNbClient) CreateChassisTemplateVar(node, chassis string, variables map[string]string) error {
	ctv, err := c.GetChassisTemplateVar(chassis, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if ctv != nil {
		return nil
	}

	ctv = &ovnnb.ChassisTemplateVar{
		Chassis:   chassis,
		Variables: variables,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
			"node":   node,
		},
	}
	ops, err := c.ovsDbClient.Create(ctv)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating ChassisTemplateVar %s: %w", chassis, err)
	}

	if err = c.Transact("chassis-template-var-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to create ChassisTemplateVar %s: %w", chassis, err)
	}

	return nil
}

// UpdateChassisTemplateVar creates the ChassisTemplateVar if it does not exist, and then adds/updates the provided variables.
// If the provided value is nil, the variable will be deleted.
func (c *OVNNbClient) UpdateChassisTemplateVar(node, chassis string, variables map[string]*string) error {
	if len(variables) == 0 {
		return nil
	}

	ctv, err := c.GetChassisTemplateVar(chassis, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	if ctv == nil {
		// create a new ChassisTemplateVar
		kvs := make(map[string]string, len(variables))
		for k, v := range variables {
			if v != nil {
				kvs[k] = *v
			}
		}
		if err = c.CreateChassisTemplateVar(node, chassis, kvs); err != nil {
			klog.Error(err)
			return err
		}
		return nil
	}

	// update the existing ChassisTemplateVar
	mutations := make([]model.Mutation, 0, len(variables)*2)
	for k, v := range variables {
		if v == nil {
			if _, ok := ctv.Variables[k]; ok {
				mutations = append(mutations, model.Mutation{
					Field:   &ctv.Variables,
					Value:   map[string]string{k: ctv.Variables[k]},
					Mutator: ovsdb.MutateOperationDelete,
				})
			}
			continue
		}
		if value, ok := ctv.Variables[k]; ok {
			if value == *v {
				continue
			}
			mutations = append(mutations, model.Mutation{
				Field:   &ctv.Variables,
				Value:   map[string]string{k: ctv.Variables[k]},
				Mutator: ovsdb.MutateOperationDelete,
			})
		}
		mutations = append(mutations, model.Mutation{
			Field:   &ctv.Variables,
			Value:   map[string]string{k: *v},
			Mutator: ovsdb.MutateOperationInsert,
		})
	}
	if len(mutations) == 0 {
		return nil
	}

	ops, err := c.ovsDbClient.Where(ctv).Mutate(ctv, mutations...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate operations for mutating ChassisTemplateVar %s: %w", chassis, err)
	}

	if err = c.Transact("chassis-template-var-update", ops); err != nil {
		err = fmt.Errorf("failed to update ChassisTemplateVar %s: %w", chassis, err)
		klog.Error(err)
		return err
	}
	return nil
}

// UpdateChassisTemplateVarVariables adds/updates the provided variables to the ChassisTemplateVar of the nodes.
// The parameter variables is a map of variable name to a map of node name to variable value.
// If a node is not in the map, the variable will be set to an empty string.
func (c *OVNNbClient) UpdateChassisTemplateVarVariables(variable string, nodeValues map[string]string) error {
	ctvList, err := c.ListChassisTemplateVar()
	if err != nil {
		klog.Error(err)
		return err
	}

	ctvMap := make(map[string]*ovnnb.ChassisTemplateVar, len(ctvList))
	for _, ctv := range ctvList {
		node := ctv.ExternalIDs["node"]
		ctvMap[node] = &ctv
		if _, ok := nodeValues[node]; !ok {
			// set to empty string for the rest nodes
			nodeValues[node] = ""
		}
	}

	var ops []ovsdb.Operation
	for node, value := range nodeValues {
		ctv := ctvMap[node]
		if ctv == nil {
			err = fmt.Errorf("ChassisTemplateVar of node %s not found", node)
			klog.Error(err)
			return err
		}

		mutations := make([]model.Mutation, 0, 2)
		if v, ok := ctv.Variables[variable]; ok {
			if v == value {
				continue
			}
			mutations = append(mutations, model.Mutation{
				Field:   &ctv.Variables,
				Value:   map[string]string{variable: ctv.Variables[variable]},
				Mutator: ovsdb.MutateOperationDelete,
			})
		}
		mutations = append(mutations, model.Mutation{
			Field:   &ctv.Variables,
			Value:   map[string]string{variable: value},
			Mutator: ovsdb.MutateOperationInsert,
		})
		op, err := c.ovsDbClient.Where(ctv).Mutate(ctv, mutations...)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to generate operations for mutating ChassisTemplateVar %s: %w", ctv.Chassis, err)
		}
		ops = append(ops, op...)
		delete(ctvMap, node)
	}

	if err := c.Transact("chassis-template-var-update", ops); err != nil {
		err = fmt.Errorf("failed to update ChassisTemplateVar %s: %w", variable, err)
		klog.Error(err)
		return err
	}
	return nil
}

// DeleteChassisTemplateVarVariables deletes the provided variables from all ChassisTemplateVar records
func (c *OVNNbClient) DeleteChassisTemplateVarVariables(variables ...string) error {
	if len(variables) == 0 {
		return nil
	}

	ctvList, err := c.ListChassisTemplateVar()
	if err != nil {
		klog.Error(err)
		return err
	}

	var ops []ovsdb.Operation
	for _, ctv := range ctvList {
		mutations := make([]model.Mutation, 0, len(variables))
		for _, v := range variables {
			if _, ok := ctv.Variables[v]; !ok {
				continue
			}
			mutations = append(mutations, model.Mutation{
				Field:   &ctv.Variables,
				Value:   map[string]string{v: ctv.Variables[v]},
				Mutator: ovsdb.MutateOperationDelete,
			})
		}
		if len(mutations) == 0 {
			continue
		}
		op, err := c.ovsDbClient.Where(&ctv).Mutate(&ctv, mutations...)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to generate operations for mutating ChassisTemplateVar %s: %w", ctv.Chassis, err)
		}
		ops = append(ops, op...)
	}

	if err = c.Transact("chassis-template-var-update", ops); err != nil {
		err = fmt.Errorf("failed to delete ChassisTemplateVar variables %v: %w", variables, err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *OVNNbClient) DeleteChassisTemplateVar(chassis string) error {
	ops, err := c.Where(&ovnnb.ChassisTemplateVar{Chassis: chassis}).Delete()
	if err != nil {
		err = fmt.Errorf("failed to generate operations for deleting ChassisTemplateVar %s: %w", chassis, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("chassis-template-var-del", ops); err != nil {
		err = fmt.Errorf("failed to delete ChassisTemplateVar %s: %w", chassis, err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *OVNNbClient) DeleteChassisTemplateVarByNodeName(node string) error {
	ctv, err := c.GetChassisTemplateVarByNodeName(node, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if ctv == nil {
		return nil
	}

	ops, err := c.Where(ctv).Delete()
	if err != nil {
		err = fmt.Errorf("failed to generate operations for deleting ChassisTemplateVar of node %s: %w", node, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("chassis-template-var-del", ops); err != nil {
		err = fmt.Errorf("failed to delete ChassisTemplateVar of node %s: %w", node, err)
		klog.Error(err)
		return err
	}

	return nil
}
