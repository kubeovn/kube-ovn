package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *ovnClient) CreateNbGlobal(nbGlobal *ovnnb.NBGlobal) error {
	op, err := c.ovnNbClient.Create(nbGlobal)
	if err != nil {
		return fmt.Errorf("generate operations for creating nb global: %v", err)
	}

	return c.Transact("nb-global-create", op)
}

func (c *ovnClient) DeleteNbGlobal() error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return err
	}

	op, err := c.Where(nbGlobal).Delete()
	if err != nil {
		return err
	}

	return c.Transact("nb-global-delete", op)
}

func (c *ovnClient) GetNbGlobal() (*ovnnb.NBGlobal, error) {
	nbGlobalList := make([]ovnnb.NBGlobal, 0, 1)

	// there is only one nb_global in OVN_Northbound, so return true and it will work
	err := c.WhereCache(func(config *ovnnb.NBGlobal) bool {
		return true
	}).List(context.TODO(), &nbGlobalList)

	if err != nil {
		return nil, fmt.Errorf("list nbGlobal: %v", err)
	}

	if len(nbGlobalList) == 0 {
		return nil, fmt.Errorf("not found nb_global")
	}

	return &nbGlobalList[0], nil
}

func (c *ovnClient) UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...interface{}) error {
	/* 	// list nb_global which connections != nil
	   	op, err := c.Where(nbGlobal, model.Condition{
	   		Field:    &nbGlobal.Connections,
	   		Function: ovsdb.ConditionNotEqual,
	   		Value:    []string{""},
	   	}).Update(nbGlobal) */

	op, err := c.Where(nbGlobal).Update(nbGlobal, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating nb global: %v", err)
	}

	if err := c.Transact("nb-global-update", op); err != nil {
		return fmt.Errorf("update nb global: %v", err)
	}

	return nil
}

func (c *ovnClient) SetICAutoRoute(enable bool, blackList []string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return fmt.Errorf("get nb global: %v", err)
	}

	if enable {
		nbGlobal.Options = map[string]string{
			"ic-route-adv":       "true",
			"ic-route-learn":     "true",
			"ic-route-blacklist": strings.Join(blackList, ","),
		}
	} else {
		nbGlobal.Options = map[string]string{
			"ic-route-adv":   "false",
			"ic-route-learn": "false",
		}
	}

	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		return fmt.Errorf("enable ovn-ic auto route, %v", err)
	}
	return nil
}
