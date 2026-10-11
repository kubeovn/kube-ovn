package ovs

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) CreateNbGlobal(nbGlobal *ovnnb.NBGlobal) error {
	if nbGlobal == nil {
		return logErr(errors.New("nb global is nil"))
	}

	op, err := c.Database.Table(&ovnnb.NBGlobal{}).CreateOps(nbGlobal)
	return c.transactGenerated("nb-global-create", op, err, wrapErr("failed to generate operations for creating nb global: %w"), nil)
}

func (c *OVNNbClient) DeleteNbGlobal() error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return logErr(err)
	}

	op, err := c.Database.Table(&ovnnb.NBGlobal{}).DeleteOps(nbGlobal)
	return c.transactGenerated("nb-global-delete", op, err, nil, nil)
}

func (c *OVNNbClient) GetNbGlobal() (*ovnnb.NBGlobal, error) {
	nbGlobalList, err := filterLogged(c.Database, &ovnnb.NBGlobal{}, func(*ovnnb.NBGlobal) bool { return true }, func(err error) error {
		return fmt.Errorf("failed to list NB_Global: %w", err)
	})
	if err != nil {
		return nil, err
	}
	if len(nbGlobalList) == 0 {
		return nil, errors.New("not found nb_global")
	}
	return &nbGlobalList[0], nil
}

func (c *OVNNbClient) UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...any) error {
	if nbGlobal == nil {
		return logErr(errors.New("nb global is nil"))
	}

	return c.updateModelLogged("nb-global-update", nbGlobal, func(err error) error {
		return fmt.Errorf("failed to update NB_Global: %w", err)
	}, fields...)
}

func (c *OVNNbClient) requireNbGlobal() (*ovnnb.NBGlobal, error) {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return nil, logWrap(err, wrapErr("failed to get nb global: %w"))
	}
	return nbGlobal, nil
}

func (c *OVNNbClient) deleteNbGlobalOption(key, wrapFmt string) error {
	nbGlobal, err := c.requireNbGlobal()
	if err != nil {
		return err
	}
	options := make(map[string]string, len(nbGlobal.Options))
	maps.Copy(options, nbGlobal.Options)
	delete(options, key)
	nbGlobal.Options = options
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Options); err != nil {
		return logWrap(err, wrapErr(wrapFmt))
	}
	return nil
}

func (c *OVNNbClient) SetAzName(azName string) error {
	nbGlobal, err := c.requireNbGlobal()
	if err != nil {
		return err
	}
	if azName == nbGlobal.Name {
		return nil // no need to update
	}

	nbGlobal.Name = azName
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Name); err != nil {
		return logWrap(err, wrapErr("set nb_global az name %s: %w", azName))
	}

	return nil
}

func (c *OVNNbClient) SetOVNIPSec(enable bool) error {
	nbGlobal, err := c.requireNbGlobal()
	if err != nil {
		return err
	}
	if enable == nbGlobal.Ipsec {
		return nil
	}

	nbGlobal.Ipsec = enable
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.Ipsec); err != nil {
		return logWrap(err, wrapErr("set nb_global ipsec %v: %w", enable))
	}

	return nil
}

func (c *OVNNbClient) SetNbGlobalOptions(key string, value any) error {
	nbGlobal, err := c.requireNbGlobal()
	if err != nil {
		return err
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
		return logWrap(err, wrapErr("failed to set nb global option %s to %v: %w", key, value))
	}

	return nil
}

func (c *OVNNbClient) SetUseCtInvMatch() error {
	return c.SetNbGlobalOptions("use_ct_inv_match", false)
}

func (c *OVNNbClient) SetICAutoRoute(enable bool, blackList []string) error {
	nbGlobal, err := c.requireNbGlobal()
	if err != nil {
		return err
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
		return logWrap(err, wrapErr("failed to enable ovn-ic auto route, %w"))
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
	return c.deleteNbGlobalOption("node_local_dns_ip", "failed to remove NB_Global option node_local_dns_ip, %w")
}

func (c *OVNNbClient) SetSkipConntrackCidrs(skipConntrackCidrs string) error {
	if skipConntrackCidrs != "" {
		return c.SetNbGlobalOptions("skip_conntrack_dst_cidrs", skipConntrackCidrs)
	}
	return c.deleteNbGlobalOption("skip_conntrack_dst_cidrs", "failed to remove NB_Global option skip_conntrack_dst_cidrs, %w")
}
