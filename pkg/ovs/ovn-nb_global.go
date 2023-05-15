package ovs

import (
	"context"
	"fmt"
	"reflect"
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
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	nbGlobalList := make([]ovnnb.NBGlobal, 0, 1)

	// there is only one nb_global in OVN_Northbound, so return true and it will work
	err := c.WhereCache(func(config *ovnnb.NBGlobal) bool {
		return true
	}).List(ctx, &nbGlobalList)

	if err != nil {
		return nil, fmt.Errorf("list nbGlobal: %v", err)
	}

	if len(nbGlobalList) == 0 {
		return nil, fmt.Errorf("not found nb_global")
	}

	return &nbGlobalList[0], nil
}

func (c *ovnClient) UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...interface{}) error {
	op, err := c.Where(nbGlobal).Update(nbGlobal, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating nb global: %v", err)
	}

	if err := c.Transact("nb-global-update", op); err != nil {
		return fmt.Errorf("update nb global: %v", err)
	}

	return nil
}

func (c *ovnClient) SetAzName(azName string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return fmt.Errorf("get nb global: %v", err)
	}
	if azName == nbGlobal.Name {
		return nil // no need to update
	}

	nbGlobal.Name = azName
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Name); err != nil {
		return fmt.Errorf("set nb_global az name %s: %v", azName, err)
	}

	return nil
}

func (c *ovnClient) SetNbGlobalOptions(key string, value interface{}) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return fmt.Errorf("failed to get nb global: %v", err)
	}

	v := fmt.Sprintf("%v", value)
	if len(nbGlobal.Options) != 0 && nbGlobal.Options[key] == v {
		return nil
	}

	options := make(map[string]string, len(nbGlobal.Options)+1)
	for k, v := range nbGlobal.Options {
		options[k] = v
	}
	nbGlobal.Options[key] = v
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		return fmt.Errorf("failed to set nb global option %s to %v: %v", key, value, err)
	}

	return nil
}

func (c *ovnClient) SetUseCtInvMatch() error {
	return c.SetNbGlobalOptions("use_ct_inv_match", false)
}

func (c *ovnClient) SetICAutoRoute(enable bool, blackList []string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return fmt.Errorf("get nb global: %v", err)
	}

	options := make(map[string]string, len(nbGlobal.Options)+3)
	for k, v := range nbGlobal.Options {
		options[k] = v
	}
	if enable {
		options["ic-route-adv"] = "true"
		options["ic-route-learn"] = "true"
		options["ic-route-blacklist"] = strings.Join(blackList, ",")
	} else {
		delete(options, "ic-route-adv")
		delete(options, "ic-route-learn")
		delete(options, "ic-route-blacklist")
	}
	if reflect.DeepEqual(options, nbGlobal.Options) {
		nbGlobal.Options = options
		return nil
	}

	nbGlobal.Options = options
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		return fmt.Errorf("enable ovn-ic auto route, %v", err)
	}
	return nil
}

func (c *ovnClient) SetLBCIDR(serviceCIDR string) error {
	return c.SetNbGlobalOptions("svc_ipv4_cidr", serviceCIDR)
}

func (c *ovnClient) SetLsDnatModDlDst(enabled bool) error {
	return c.SetNbGlobalOptions("ls_dnat_mod_dl_dst", enabled)
}
