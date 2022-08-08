package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) CreateNbGlobal(nbGlobal *ovnnb.NBGlobal) error {
	op, err := c.ovnNbClient.Create(nbGlobal)
	if err != nil {
		return fmt.Errorf("generate create operations for nb global: %v", err)
	}

	return c.Transact("nb-global-create", op)
}

func (c OvnClient) DeleteNbGlobal() error {
	nbGlobal, err := c.GetNbGlobal()
	if nil != err {
		return err
	}

	op, err := c.Where(nbGlobal).Delete()
	if err != nil {
		return err
	}

	return c.Transact("nb-global-delete", op)
}

func (c OvnClient) GetNbGlobal() (*ovnnb.NBGlobal, error) {
	nbGlobalList := make([]ovnnb.NBGlobal, 0, 1)

	// there is only one nb_global in OVN_Northbound, so return true and it will work
	err := c.WhereCache(func(config *ovnnb.NBGlobal) bool {
		return true
	}).List(context.TODO(), &nbGlobalList)

	if nil != err {
		return nil, fmt.Errorf("list nbGlobal: %v", err)
	}

	if 0 == len(nbGlobalList) {
		return nil, fmt.Errorf("not found nb_global")
	}

	return &nbGlobalList[0], nil
}

func (c OvnClient) UpdateNbGlobal(newNbGlobal *ovnnb.NBGlobal) error {
	/* 	// list nb_global which connections != nil
	   	op, err := c.Where(nbGlobal, model.Condition{
	   		Field:    &nbGlobal.Connections,
	   		Function: ovsdb.ConditionNotEqual,
	   		Value:    []string{""},
	   	}).Update(nbGlobal) */

	oldNbGlobal, err := c.GetNbGlobal()
	if nil != err {
		return err
	}

	op, err := c.Where(oldNbGlobal).Update(newNbGlobal)
	if nil != err {
		return fmt.Errorf("generate update operations for nb global: %v", err)
	}

	return c.Transact("set", op)
}

func (c OvnClient) SetICAutoRoute(enable bool, blackList []string) error {
	var update ovnnb.NBGlobal
	if enable {
		update = ovnnb.NBGlobal{Options: map[string]string{
			"ic-route-adv":       "true",
			"ic-route-learn":     "true",
			"ic-route-blacklist": strings.Join(blackList, ","),
		}}
	} else {
		update = ovnnb.NBGlobal{Options: map[string]string{
			"ic-route-adv":   "false",
			"ic-route-learn": "false",
		}}
	}

	if err := c.UpdateNbGlobal(&update); nil != err {
		return fmt.Errorf("enable ovn-ic auto route, %v", err)
	}
	return nil
}
