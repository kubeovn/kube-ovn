package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"

	"github.com/cnf/structhash"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddSg(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add securityGroup %s", key)
	c.addOrUpdateSgQueue.Add(key)
}

func (c *Controller) enqueueUpdateSg(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldSg := old.(*kubeovnv1.SecurityGroup)
	newSg := new.(*kubeovnv1.SecurityGroup)
	if !reflect.DeepEqual(oldSg.Spec, newSg.Spec) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.V(3).Infof("enqueue update securityGroup %s", key)
		c.addOrUpdateSgQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteSg(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete securityGroup %s", key)
	c.delSgQueue.Add(key)
}

func (c *Controller) runAddSgWorker() {
	for c.processNextAddOrUpdateSgWorkItem() {
	}
}

func (c *Controller) runDelSgWorker() {
	for c.processNextDeleteSgWorkItem() {
	}
}

func (c *Controller) runSyncSgPortsWorker() {
	for c.processNextSyncSgPortsWorkItem() {
	}
}

func (c *Controller) processNextSyncSgPortsWorkItem() bool {
	obj, shutdown := c.syncSgPortsQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.syncSgPortsQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.syncSgPortsQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncSgLogicalPort(key); err != nil {
			c.syncSgPortsQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.syncSgPortsQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextAddOrUpdateSgWorkItem() bool {
	obj, shutdown := c.addOrUpdateSgQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateSgQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateSgQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateSg(key); err != nil {
			c.addOrUpdateSgQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateSgQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteSgWorkItem() bool {
	obj, shutdown := c.delSgQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delSgQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delSgQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteSg(key); err != nil {
			c.delSgQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delSgQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) initDenyAllSecurityGroup() error {
	if err := c.ovnLegacyClient.CreateSgPortGroup(util.DenyAllSecurityGroup); err != nil {
		return err
	}
	if err := c.ovnLegacyClient.CreateSgDenyAllACL(); err != nil {
		return err
	}
	c.addOrUpdateSgQueue.Add(util.DenyAllSecurityGroup)
	return nil
}

// updateDenyAllSgPorts set lsp to deny which security_groups is not empty
func (c *Controller) updateDenyAllSgPorts() error {
	sgKey := "security_groups"
	// list all lsp which security_groups is not empty
	ports, err := c.ovnClient.ListLogicalSwitchPorts(true, map[string]string{sgKey: ""})
	if err != nil {
		klog.Errorf("failed to find logical port, %v", err)
		return err
	}

	matchPorts := make([]string, 0, len(ports))
	for _, port := range ports {
		// skip lsp which only have mac addresses,
		// data store in port.PortSecurity[0], em...
		if len(strings.Split(port.PortSecurity[0], " ")) < 2 {
			continue
		}

		/*  skip lsp which security_group does not exist */
		// sgs format: sg1/sg2/sg3
		sgs := strings.Split(port.ExternalIDs[sgKey], "/")
		allNotExist, err := c.securityGroupALLNotExist(sgs)
		if err != nil {
			return err
		}

		if allNotExist {
			klog.Infof("all sgs of lsp: %s does't exist", port.Name)
			continue
		}

		matchPorts = append(matchPorts, port.Name)
	}

	return c.ovnLegacyClient.SetPortsToPortGroup(ovs.GetSgPortGroupName(util.DenyAllSecurityGroup), matchPorts)
}

func (c *Controller) handleAddOrUpdateSg(key string) error {
	c.sgKeyMutex.Lock(key)
	defer c.sgKeyMutex.Unlock(key)

	// set 'deny all' for port associated with security group
	if key == util.DenyAllSecurityGroup {
		if err := c.updateDenyAllSgPorts(); err != nil {
			klog.Errorf("update sg deny all policy failed. %v", err)
			return err
		}
	}

	orisg, err := c.sgsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	sg := orisg.DeepCopy()

	if err = c.validateSgRule(sg); err != nil {
		return err
	}

	if err = c.ovnLegacyClient.CreateSgPortGroup(sg.Name); err != nil {
		return fmt.Errorf("failed to create sg port_group %s, %v", key, err.Error())
	}
	if err = c.ovnLegacyClient.CreateSgAssociatedAddressSet(sg.Name); err != nil {
		return fmt.Errorf("failed to create sg associated address_set %s, %v", key, err.Error())
	}

	ingressNeedUpdate := false
	egressNeedUpdate := false

	// check md5
	newIngressMd5 := fmt.Sprintf("%x", structhash.Md5(sg.Spec.IngressRules, 1))
	if !sg.Status.IngressLastSyncSuccess || newIngressMd5 != sg.Status.IngressMd5 {
		klog.Infof("ingress need update, sg:%s", sg.Name)
		ingressNeedUpdate = true
	}
	newEgressMd5 := fmt.Sprintf("%x", structhash.Md5(sg.Spec.EgressRules, 1))
	if !sg.Status.EgressLastSyncSuccess || newEgressMd5 != sg.Status.EgressMd5 {
		klog.Infof("egress need update, sg:%s", sg.Name)
		egressNeedUpdate = true
	}

	// check allowSameGroupTraffic switch
	if sg.Status.AllowSameGroupTraffic != sg.Spec.AllowSameGroupTraffic {
		klog.Infof("both ingress && egress need update, sg:%s", sg.Name)
		ingressNeedUpdate = true
		egressNeedUpdate = true
	}

	// update sg rule
	if ingressNeedUpdate {
		if err = c.ovnLegacyClient.UpdateSgACL(sg, ovs.SgAclIngressDirection); err != nil {
			sg.Status.IngressLastSyncSuccess = false
			c.patchSgStatus(sg)
			return err
		}
		sg.Status.IngressMd5 = newIngressMd5
		sg.Status.IngressLastSyncSuccess = true
		c.patchSgStatus(sg)
	}
	if egressNeedUpdate {
		if err = c.ovnLegacyClient.UpdateSgACL(sg, ovs.SgAclEgressDirection); err != nil {
			sg.Status.EgressLastSyncSuccess = false
			c.patchSgStatus(sg)
			return err
		}
		sg.Status.EgressMd5 = newEgressMd5
		sg.Status.EgressLastSyncSuccess = true
		c.patchSgStatus(sg)
	}

	// update status
	sg.Status.PortGroup = ovs.GetSgPortGroupName(sg.Name)
	sg.Status.AllowSameGroupTraffic = sg.Spec.AllowSameGroupTraffic
	c.patchSgStatus(sg)
	c.syncSgPortsQueue.Add(key)
	return nil
}

func (c *Controller) validateSgRule(sg *kubeovnv1.SecurityGroup) error {
	// check sg rules
	allRules := append(sg.Spec.IngressRules, sg.Spec.EgressRules...)
	for _, rule := range allRules {
		if rule.IPVersion != "ipv4" && rule.IPVersion != "ipv6" {
			return fmt.Errorf("IPVersion should be 'ipv4' or 'ipv6'")
		}

		if rule.Priority < 1 || rule.Priority > 200 {
			return fmt.Errorf("priority '%d' is not in the range of 1 to 200", rule.Priority)
		}

		switch rule.RemoteType {
		case kubeovnv1.SgRemoteTypeAddress:
			if strings.Contains(rule.RemoteAddress, "/") {
				if _, _, err := net.ParseCIDR(rule.RemoteAddress); err != nil {
					return fmt.Errorf("invalid CIDR '%s'", rule.RemoteAddress)
				}
			} else {
				if net.ParseIP(rule.RemoteAddress) == nil {
					return fmt.Errorf("invalid ip address '%s'", rule.RemoteAddress)
				}
			}
		case kubeovnv1.SgRemoteTypeSg:
			_, err := c.sgsLister.Get(rule.RemoteSecurityGroup)
			if err != nil {
				return fmt.Errorf("failed to get remote sg '%s', %v", rule.RemoteSecurityGroup, err)
			}
		default:
			return fmt.Errorf("not support sgRemoteType '%s'", rule.RemoteType)
		}

		if rule.Protocol == kubeovnv1.ProtocolTCP || rule.Protocol == kubeovnv1.ProtocolUDP {
			if rule.PortRangeMin < 1 || rule.PortRangeMin > 65535 || rule.PortRangeMax < 1 || rule.PortRangeMax > 65535 {
				return fmt.Errorf("portRange is out of range")
			}
			if rule.PortRangeMin > rule.PortRangeMax {
				return fmt.Errorf("portRange err, range Minimum value greater than maximum value")
			}
		}
	}
	return nil
}

func (c *Controller) patchSgStatus(sg *kubeovnv1.SecurityGroup) {
	bytes, err := sg.Status.Bytes()
	if err != nil {
		klog.Error(err)
		return
	} else {
		if _, err = c.config.KubeOvnClient.KubeovnV1().SecurityGroups().Patch(context.Background(), sg.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Error("patch security group status failed", err)
		}
	}
}

func (c *Controller) handleDeleteSg(key string) error {
	c.sgKeyMutex.Lock(key)
	defer c.sgKeyMutex.Unlock(key)
	return c.ovnLegacyClient.DeleteSgPortGroup(key)
}

func (c *Controller) syncSgLogicalPort(key string) error {
	c.sgKeyMutex.Lock(key)
	defer c.sgKeyMutex.Unlock(key)

	sg, err := c.sgsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("sg '%s' not found.", key)
			return nil
		}
		klog.Errorf("failed to get sg '%s'. %v", key, err)
		return err
	}

	results, err := c.ovnLegacyClient.CustomFindEntity("logical_switch_port", []string{"_uuid", "name", "port_security"}, fmt.Sprintf("external_ids:associated_sg_%s=true", key))
	if err != nil {
		klog.Errorf("failed to find logical port, %v", err)
		return err
	}

	var v4s, v6s []string
	var ports []string
	for _, ret := range results {
		if len(ret["port_security"]) < 2 {
			continue
		}
		ports = append(ports, ret["name"][0])
		for _, address := range ret["port_security"][1:] {
			if strings.Contains(address, ":") {
				v6s = append(v6s, address)
			} else {
				v4s = append(v4s, address)
			}
		}
	}

	if err = c.ovnLegacyClient.SetPortsToPortGroup(sg.Status.PortGroup, ports); err != nil {
		klog.Errorf("failed to set port to sg, %v", err)
		return err
	}
	if err = c.ovnLegacyClient.SetAddressesToAddressSet(v4s, ovs.GetSgV4AssociatedName(key)); err != nil {
		klog.Errorf("failed to set address_set, %v", err)
		return err
	}
	if err = c.ovnLegacyClient.SetAddressesToAddressSet(v6s, ovs.GetSgV6AssociatedName(key)); err != nil {
		klog.Errorf("failed to set address_set, %v", err)
		return err
	}
	c.addOrUpdateSgQueue.Add(util.DenyAllSecurityGroup)
	return nil
}

func (c *Controller) getPortSg(port *ovnnb.LogicalSwitchPort) ([]string, error) {
	var sgList []string
	for key, value := range port.ExternalIDs {
		if strings.HasPrefix(key, "associated_sg_") && value == "true" {
			sgName := strings.ReplaceAll(key, "associated_sg_", "")
			sgList = append(sgList, sgName)
		}
	}
	return sgList, nil
}

func (c *Controller) reconcilePortSg(portName, securityGroups string) error {
	port, err := c.ovnClient.GetLogicalSwitchPort(portName, false)
	if err != nil {
		klog.Errorf("failed to get logical switch port %s: %v", portName, err)
		return err
	}
	oldSgList, err := c.getPortSg(port)
	if err != nil {
		klog.Errorf("get port sg failed, %v", err)
		return err
	}
	klog.Infof("reconcile port sg, port='%s', oldSgList='%s', newSgList='%s'", portName, oldSgList, securityGroups)

	newSgList := strings.Split(securityGroups, ",")
	diffSgList := util.DiffStringSlice(oldSgList, newSgList)
	for _, sgName := range diffSgList {
		if sgName == "" {
			continue
		}
		needAssociated := "false"
		if util.ContainsString(newSgList, sgName) {
			needAssociated = "true"
		}
		if err = c.ovnLegacyClient.SetPortExternalIds(portName, fmt.Sprintf("associated_sg_%s", sgName), needAssociated); err != nil {
			klog.Errorf("set port '%s' external_ids failed, %v", portName, err)
			return err
		}
		c.syncSgPortsQueue.Add(sgName)
	}

	if err = c.ovnLegacyClient.SetPortExternalIds(portName, "security_groups", strings.ReplaceAll(securityGroups, ",", "/")); err != nil {
		klog.Errorf("set port '%s' external_ids failed, %v", portName, err)
		return err
	}
	return nil
}

// securityGroupALLNotExist return true if all sgs does not exist
func (c *Controller) securityGroupALLNotExist(sgs []string) (bool, error) {
	if len(sgs) == 0 {
		return true, nil
	}

	notExistsCount := 0
	// sgs format: sg1/sg2/sg3
	for _, sg := range sgs {
		ok, err := c.ovnClient.PortGroupExists(ovs.GetSgPortGroupName(sg))
		if err != nil {
			return true, err
		}

		if !ok {
			notExistsCount++
		}
	}

	return notExistsCount == len(sgs), nil
}
