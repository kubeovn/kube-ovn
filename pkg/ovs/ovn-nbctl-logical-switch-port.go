package ovs

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c Client) GetLogicalSwitchPort(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	lsp := &ovnnb.LogicalSwitchPort{Name: name}
	if err := c.nbClient.Get(context.TODO(), lsp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical switch port %s: %v", name, err)
	}

	return lsp, nil
}

func (c Client) createLSP(name, lspType, ls, mac, ip, vips string, tag *int, options, externalIDs map[string]string, liveMigration, portSecurity bool, securityGroups string) error {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	if err != nil {
		return err
	}

	var needCreate bool
	if lsp == nil {
		needCreate = true
		lsp = &ovnnb.LogicalSwitchPort{UUID: ovsclient.NamedUUID(), Name: name}
	}

	lsp.Type, lsp.Tag = lspType, tag
	if lsp.Options == nil {
		lsp.Options = options
	} else {
		for k, v := range options {
			lsp.Options[k] = v
		}
	}

	if externalIDs == nil {
		externalIDs = make(map[string]string)
	}
	externalIDs["vendor"] = util.CniTypeName

	if lsp.ExternalIDs == nil {
		lsp.ExternalIDs = externalIDs
	} else {
		for k, v := range externalIDs {
			lsp.ExternalIDs[k] = v
		}
	}

	var addrConflict bool
	addr := []string{mac}
	if ip != "" {
		addr = append(addr, strings.Split(ip, ",")...)
	}
	if liveMigration {
		eidIP := strings.ReplaceAll(ip, ",", "/")

		// add external_id info as the filter of 'live Migration vm port'
		lsp.ExternalIDs["ls"] = ls
		lsp.ExternalIDs["ip"] = eidIP

		lspList, err := c.ListLogicalSwitchPorts(true, map[string]string{"ls": ls, "ip": eidIP})
		if err != nil {
			klog.Errorf("failed to list logical switch ports : %v", err)
			return err
		}
		if len(lspList) != 0 {
			addrConflict = true
		}
	}
	if addrConflict {
		// only set mac, and set flag 'liveMigration'
		lsp.Addresses = []string{mac}
		lsp.ExternalIDs["liveMigration"] = "1"
	} else {
		// set mac and ip
		lsp.Addresses = []string{strings.Join(addr, " ")}
	}

	if portSecurity {
		if vips != "" {
			addr = append(addr, strings.Split(vips, ",")...)
		}
		lsp.PortSecurity = []string{strings.Join(addr, " ")}
		if securityGroups != "" {
			lsp.ExternalIDs["security_groups"] = securityGroups
			for _, sg := range strings.Split(securityGroups, ",") {
				if sg != "" {
					lsp.ExternalIDs["associated_sg_"+sg] = "true"
				}
			}
		}
	}

	var ops []ovsdb.Operation
	if needCreate {
		if ops, err = c.nbClient.Create(lsp); err != nil {
			return fmt.Errorf("failed to generate create operations for logical switch port %s: %v", name, err)
		}

		_ls, err := c.GetLogicalSwitch(ls, false, false)
		if err != nil {
			return err
		}

		insertOps, err := c.nbClient.Where(_ls).Mutate(_ls, model.Mutation{
			Field:   &_ls.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{lsp.UUID},
		})
		if err != nil {
			return fmt.Errorf("failed to generate operations for attaching logical switch port %s to logical switch %s: %v", name, ls, err)
		}
		ops = append(ops, insertOps...)
	} else if ops, err = c.nbClient.Where(lsp).Update(lsp, &lsp.Addresses, &lsp.Options, &lsp.ExternalIDs, &lsp.PortSecurity); err != nil {
		return fmt.Errorf("failed to generate update operations for logical switch port %s: %v", name, err)
	}

	if err = Transact(c.nbClient, "lsp-add", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create logical switch port %s: %v", name, err)
	}

	return nil
}

// CreateLogicalSwitchPort creates a logical switch port in ovn
func (c Client) CreateLogicalSwitchPort(ls, port, mac, ip, vips, pod, namespace string, liveMigration, portSecurity bool, securityGroups string) error {
	var externalIDs map[string]string
	if pod != "" && namespace != "" {
		externalIDs = map[string]string{"pod": fmt.Sprintf("%s/%s", namespace, pod)}
	}
	return c.createLSP(port, "", ls, mac, ip, vips, nil, nil, externalIDs, liveMigration, portSecurity, securityGroups)
}

func (c Client) SetPortTag(name string, vlanID int) error {
	lsp, err := c.GetLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}

	if vlanID > 0 && vlanID < 4096 {
		if lsp.Tag != nil && *lsp.Tag == vlanID {
			return nil
		}
		lsp.Tag = &vlanID
	} else {
		if lsp.Tag == nil {
			return nil
		}
		lsp.Tag = nil
	}

	ops, err := c.nbClient.Where(lsp).Update(lsp, &lsp.Tag)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for logical switch port %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "set", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to set tag of logical switch port %s: %v", name, err)
	}

	return nil
}

func (c Client) SetPortSecurity(enabled bool, port, mac, ips string) error {
	lsp, err := c.GetLogicalSwitchPort(port, false)
	if err != nil {
		return err
	}

	if !enabled && len(lsp.PortSecurity) == 0 {
		return nil
	}

	var portSecurity []string
	if enabled {
		addresses := []string{mac}
		if ips != "" {
			addresses = append(addresses, strings.Split(ips, ",")...)
		}
		portSecurity = []string{strings.Join(addresses, " ")}
	}
	if reflect.DeepEqual(portSecurity, lsp.PortSecurity) {
		return nil
	}

	ops, err := c.nbClient.Where(lsp).Update(lsp, &lsp.PortSecurity)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for logical switch port %s: %v", port, err)
	}
	if err = Transact(c.nbClient, "lsp-set-port-security", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to set port security of logical switch port %s: %v", port, err)
	}

	return nil
}

func (c Client) ListPodLogicalSwitchPorts(pod, namespace string) ([]string, error) {
	s := fmt.Sprintf("%s/%s", namespace, pod)
	lspList := make([]ovnnb.LogicalSwitchPort, 0)
	if err := c.nbClient.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		return len(lsp.ExternalIDs) != 0 && lsp.ExternalIDs["pod"] == s
	}).List(context.TODO(), &lspList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch ports of Pod %s: %v", s, err)
	}

	result := make([]string, 0, len(lspList))
	for _, lsp := range lspList {
		result = append(result, lsp.Name)
	}
	return result, nil
}

func (c Client) ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0)
	if err := c.nbClient.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		if lsp.Type != "" {
			return false
		}
		if needVendorFilter && (len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}
		if len(lsp.ExternalIDs) < len(externalIDs) {
			return false
		}
		if len(lsp.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				if lsp.ExternalIDs[k] != v {
					return false
				}
			}
		}
		return true
	}).List(context.TODO(), &lspList); err != nil {
		klog.Errorf("failed to list logical switch ports: %v", err)
		return nil, err
	}

	return lspList, nil
}

func (c Client) LogicalSwitchPortExists(name string) (bool, error) {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	return lsp != nil, err
}

func (c Client) ListRemoteLogicalSwitchPortAddress() ([]string, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0)
	if err := c.nbClient.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool { return lsp.Type == "remote" }).List(context.TODO(), &lspList); err != nil {
		klog.Errorf("failed to list remote logical switch ports: %v", err)
		return nil, err
	}

	result := make([]string, 0, len(lspList))
	for _, lsp := range lspList {
		for _, addr := range lsp.Addresses {
			fields := strings.Fields(addr)
			if len(fields) == 2 {
				result = append(result, strings.TrimSpace(fields[1]))
			}
		}
	}

	return result, nil
}

// GetPortAddr return port [mac, ip]
func (c Client) GetPortAddr(port string) ([]string, error) {
	return nil, nil
}

func (c Client) GetLogicalSwitchPortByLogicalSwitch(ls string) ([]string, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0, 1)
	if err := c.nbClient.WhereCache(func(_ls *ovnnb.LogicalSwitch) bool { return _ls.Name == ls }).List(context.TODO(), &lsList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch with name %s: %v", ls, err)
	}
	if len(lsList) == 0 {
		return nil, fmt.Errorf("logical switch %s does not exist", ls)
	}
	if len(lsList) != 1 {
		return nil, fmt.Errorf("found multiple logical switch with the same name %s", ls)
	}

	result := make([]string, 0, len(lsList[0].Ports))
	for _, port := range lsList[0].Ports {
		lsp := &ovnnb.LogicalSwitchPort{UUID: port}
		if err := c.nbClient.Get(context.TODO(), lsp); err != nil {
			return nil, fmt.Errorf("failed to get logical switch port with UUID %s: %v", port, err)
		}
		result = append(result, lsp.Name)
	}
	return result, nil
}

func (c Client) CreateLocalnetPort(name, ls, provider string, vlanID int) error {
	var tag *int
	if vlanID > 0 && vlanID < 4096 {
		tag = &vlanID
	}

	return c.createLSP(name, "localnet", ls, "unknown", "", "", tag, map[string]string{"network_name": provider}, nil, false, false, "")
}

func (c Client) SetPortExternalIDs(name string, externalIDs map[string]string) error {
	lsp, err := c.GetLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}
	if len(externalIDs) == 0 {
		return nil
	}

	var needUpdate bool
	if len(lsp.ExternalIDs) == 0 {
		needUpdate, lsp.ExternalIDs = true, externalIDs
	} else {
		for k, v := range externalIDs {
			if lsp.ExternalIDs[k] != v {
				needUpdate = true
				lsp.ExternalIDs[k] = v
			}
		}
	}
	if !needUpdate {
		return nil
	}

	ops, err := c.nbClient.Where(lsp).Update(lsp, &lsp.ExternalIDs)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for logical switch port %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "set", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to set external IDs of logical switch port %s: %v", name, err)
	}

	return nil
}

func (c Client) SetPortExternalID(name, key, value string) error {
	return c.SetPortExternalIDs(name, map[string]string{key: value})
}

// DeleteLogicalSwitchPort delete logical switch port in ovn
func (c Client) DeleteLogicalSwitchPort(name string) error {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	if err != nil {
		return err
	}
	if lsp == nil {
		return nil
	}

	ops, err := c.nbClient.Where(lsp).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for logical switch port %s: %v", name, err)
	}

	lsList := make([]ovnnb.LogicalSwitch, 0)
	if err := c.nbClient.List(context.TODO(), &lsList); err != nil {
		return fmt.Errorf("failed to list logical switch: %v", err)
	}

	var ls *ovnnb.LogicalSwitch
	for _, _ls := range lsList {
		if util.ContainsString(_ls.Ports, lsp.UUID) {
			ls = &_ls
			break
		}
	}

	if ls != nil {
		deleteOps, err := c.nbClient.Where(ls).Mutate(ls, model.Mutation{
			Field:   &ls.Ports,
			Mutator: ovsdb.MutateOperationDelete,
			Value:   []string{lsp.UUID},
		})
		if err != nil {
			return fmt.Errorf("failed to generate operations for detaching logical switch port %s from logical switch %s: %v", name, ls.Name, err)
		}
		ops = append(ops, deleteOps...)
	}

	if err = Transact(c.nbClient, "lsp-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete logical switch port %s: %v", name, err)
	}

	return nil
}
