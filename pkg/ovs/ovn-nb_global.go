package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) CreateNbGlobal(nbGlobal *ovnnb.NBGlobal) error {
	if nbGlobal == nil {
		err := errors.New("nb global is nil")
		klog.Error(err)
		return err
	}

	op, err := c.Create(nbGlobal)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate operations for creating nb global: %w", err)
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
		return nil, fmt.Errorf("failed to list NB_Global: %w", err)
	}

	if len(nbGlobalList) == 0 {
		return nil, errors.New("not found nb_global")
	}

	return &nbGlobalList[0], nil
}

func (c *OVNNbClient) UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...any) error {
	if nbGlobal == nil {
		err := errors.New("nb global is nil")
		klog.Error(err)
		return err
	}

	op, err := c.Where(nbGlobal).Update(nbGlobal, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate operations for updating nb global: %w", err)
	}

	if err := c.Transact("nb-global-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to update NB_Global: %w", err)
	}

	return nil
}

func (c *OVNNbClient) SetAzName(azName string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get nb global: %w", err)
	}
	if azName == nbGlobal.Name {
		return nil // no need to update
	}

	nbGlobal.Name = azName
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Name); err != nil {
		klog.Error(err)
		return fmt.Errorf("set nb_global az name %s: %w", azName, err)
	}

	return nil
}

func (c *OVNNbClient) SetOVNIPSec(enable bool) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get nb global: %w", err)
	}
	if enable == nbGlobal.Ipsec {
		return nil
	}

	nbGlobal.Ipsec = enable
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Ipsec); err != nil {
		klog.Error(err)
		return fmt.Errorf("set nb_global ipsec %v: %w", enable, err)
	}

	return nil
}

func (c *OVNNbClient) SetNbGlobalOptions(key string, value any) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get nb global: %w", err)
	}

	v := fmt.Sprintf("%v", value)
	if len(nbGlobal.Options) != 0 && nbGlobal.Options[key] == v {
		return nil
	}

	if nbGlobal.Options == nil {
		nbGlobal.Options = make(map[string]string)
	}
	nbGlobal.Options[key] = v

	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set nb global option %s to %v: %w", key, value, err)
	}

	return nil
}

func (c *OVNNbClient) SetUseCtInvMatch() error {
	return c.SetNbGlobalOptions("use_ct_inv_match", false)
}

func (c *OVNNbClient) SetICAutoRoute(enable bool, blackList []string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get nb global: %w", err)
	}

	options := make(map[string]string, len(nbGlobal.Options)+3)
	maps.Copy(options, nbGlobal.Options)
	if enable {
		options["ic-route-adv"] = "true"
		options["ic-route-learn"] = "true"
		options["ic-route-blacklist"] = strings.Join(blackList, ",")
	} else {
		delete(options, "ic-route-adv")
		delete(options, "ic-route-learn")
		delete(options, "ic-route-blacklist")
	}
	if maps.Equal(options, nbGlobal.Options) {
		return nil
	}

	nbGlobal.Options = options
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to enable ovn-ic auto route, %w", err)
	}
	return nil
}

func (c *OVNNbClient) SetLBCIDR(serviceCIDR string) error {
	return c.SetNbGlobalOptions("svc_ipv4_cidr", serviceCIDR)
}

func (c *OVNNbClient) SetLsDnatModDlDst(enabled bool) error {
	return c.SetNbGlobalOptions("ls_dnat_mod_dl_dst", enabled)
}

func (c *OVNNbClient) SetLsCtSkipDstLportIPs(enabled bool) error {
	return c.SetNbGlobalOptions("ls_ct_skip_dst_lport_ips", enabled)
}

func (c *OVNNbClient) SetNodeLocalDNSIP(nodeLocalDNSIP string) error {
	if nodeLocalDNSIP != "" {
		return c.SetNbGlobalOptions("node_local_dns_ip", nodeLocalDNSIP)
	}

	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get nb global: %w", err)
	}

	options := make(map[string]string, len(nbGlobal.Options))
	maps.Copy(options, nbGlobal.Options)

	delete(options, "node_local_dns_ip")

	nbGlobal.Options = options
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to remove NB_Global option node_local_dns_ip, %w", err)
	}

	return nil
}

func (c *OVNNbClient) SetSkipConntrackCidrs(skipConntrackCidrs string) error {
	if skipConntrackCidrs != "" {
		return c.SetNbGlobalOptions("skip_conntrack_dst_cidrs", skipConntrackCidrs)
	}

	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get nb global: %w", err)
	}

	options := make(map[string]string, len(nbGlobal.Options))
	maps.Copy(options, nbGlobal.Options)

	delete(options, "skip_conntrack_dst_cidrs")

	nbGlobal.Options = options
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to remove NB_Global option skip_conntrack_dst_cidrs, %w", err)
	}

	return nil
}
