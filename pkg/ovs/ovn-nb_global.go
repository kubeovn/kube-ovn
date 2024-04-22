package ovs

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/exp/maps"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) CreateNbGlobal(nbGlobal *ovnnb.NBGlobal) error {
	op, err := c.ovsDbClient.Create(nbGlobal)
	if err != nil {
		return fmt.Errorf("generate operations for creating nb global: %v", err)
	}

	return c.Transact("nb-global-create", op)
}

func (c *OVNNbClient) DeleteNbGlobal() error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return err
	}

	op, err := c.Where(nbGlobal).Delete()
	if err != nil {
		klog.Error(err)
		return err
	}

	return c.Transact("nb-global-delete", op)
}

func (c *OVNNbClient) GetNbGlobal() (*ovnnb.NBGlobal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	nbGlobalList := make([]ovnnb.NBGlobal, 0, 1)

	// there is only one nb_global in OVN_Northbound, so return true and it will work
	err := c.WhereCache(func(_ *ovnnb.NBGlobal) bool {
		return true
	}).List(ctx, &nbGlobalList)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list nbGlobal: %v", err)
	}

	if len(nbGlobalList) == 0 {
		return nil, fmt.Errorf("not found nb_global")
	}

	return &nbGlobalList[0], nil
}

func (c *OVNNbClient) UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...interface{}) error {
	op, err := c.Where(nbGlobal).Update(nbGlobal, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating nb global: %v", err)
	}

	if err := c.Transact("nb-global-update", op); err != nil {
		return fmt.Errorf("update nb global: %v", err)
	}

	return nil
}

func (c *OVNNbClient) SetAzName(azName string) error {
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

func (c *OVNNbClient) SetICAutoRoute(enable bool, blackList []string) error {
	options := map[string]string{
		"ic-route-adv":       "",
		"ic-route-learn":     "",
		"ic-route-blacklist": "",
	}
	if enable {
		options["ic-route-adv"] = "true"
		options["ic-route-learn"] = "true"
		options["ic-route-blacklist"] = strings.Join(blackList, ",")
	}

	return c.SetNBGlobalOptions(options)
}

func (c *OVNNbClient) SetNBGlobalOptions(options map[string]string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return fmt.Errorf("failed to get nb global: %v", err)
	}

	newOptions := maps.Clone(nbGlobal.Options)
	if newOptions == nil {
		newOptions = make(map[string]string, len(options))
	}
	for k, v := range options {
		if v == "" {
			delete(nbGlobal.Options, k)
		} else {
			nbGlobal.Options[k] = v
		}
	}

	if !maps.Equal(nbGlobal.Options, newOptions) {
		if err = c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
			return fmt.Errorf("failed to update nb global options: %v", err)
		}
	}

	return nil
}
