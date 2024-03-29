package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"slices"
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
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add securityGroup %s", key)
	c.addOrUpdateSgQueue.Add(key)
}

func (c *Controller) enqueueUpdateSg(oldObj, newObj interface{}) {
	oldSg := oldObj.(*kubeovnv1.SecurityGroup)
	newSg := newObj.(*kubeovnv1.SecurityGroup)
	if !reflect.DeepEqual(oldSg.Spec, newSg.Spec) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.V(3).Infof("enqueue update securityGroup %s", key)
		c.addOrUpdateSgQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteSg(obj interface{}) {
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
	pgName := ovs.GetSgPortGroupName(util.DenyAllSecurityGroup)
	if err := c.OVNNbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  util.DenyAllSecurityGroup,
	}); err != nil {
		klog.Errorf("create port group for sg %s: %v", util.DenyAllSecurityGroup, err)
		return err
	}

	if err := c.OVNNbClient.CreateSgDenyAllACL(util.DenyAllSecurityGroup); err != nil {
		klog.Errorf("create deny all acl for sg %s: %v", util.DenyAllSecurityGroup, err)
		return err
	}

	c.addOrUpdateSgQueue.Add(util.DenyAllSecurityGroup)
	return nil
}

// updateDenyAllSgPorts set lsp to deny which security_groups is not empty
func (c *Controller) updateDenyAllSgPorts() error {
	// list all lsp which security_groups is not empty
	lsps, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(true, map[string]string{sgsKey: ""})
	if err != nil {
		klog.Errorf("list logical switch ports with security_groups is not empty: %v", err)
		return err
	}

	addPorts := make([]string, 0, len(lsps))
	for _, lsp := range lsps {
		// skip lsp which only have mac addresses,address is in port.PortSecurity[0]
		if len(lsp.PortSecurity) == 0 || len(strings.Split(lsp.PortSecurity[0], " ")) < 2 {
			continue
		}

		/* skip lsp which security_group does not exist */
		// sgs format: sg1/sg2/sg3
		sgs := strings.Split(lsp.ExternalIDs[sgsKey], "/")
		allNotExist, err := c.securityGroupAllNotExist(sgs)
		if err != nil {
			klog.Error(err)
			return err
		}

		if allNotExist {
			continue
		}

		addPorts = append(addPorts, lsp.Name)
	}
	pgName := ovs.GetSgPortGroupName(util.DenyAllSecurityGroup)

	klog.V(6).Infof("setting ports of port group %s to %v", pgName, addPorts)
	if err = c.OVNNbClient.PortGroupSetPorts(pgName, addPorts); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) handleAddOrUpdateSg(key string) error {
	c.sgKeyMutex.LockKey(key)
	defer func() { _ = c.sgKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update security group %s", key)

	// set 'deny all' for port associated with security group
	if key == util.DenyAllSecurityGroup {
		if err := c.updateDenyAllSgPorts(); err != nil {
			klog.Errorf("update sg deny all policy failed. %v", err)
			return err
		}
	}

	cachedSg, err := c.sgsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	sg := cachedSg.DeepCopy()

	if err = c.validateSgRule(sg); err != nil {
		klog.Error(err)
		return err
	}

	pgName := ovs.GetSgPortGroupName(sg.Name)
	if err := c.OVNNbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  sg.Name,
	}); err != nil {
		klog.Errorf("create port group for sg %s: %v", sg.Name, err)
		return err
	}

	v4AsName := ovs.GetSgV4AssociatedName(sg.Name)
	v6AsName := ovs.GetSgV6AssociatedName(sg.Name)
	externalIDs := map[string]string{
		sgKey: sg.Name,
	}

	if err = c.OVNNbClient.CreateAddressSet(v4AsName, externalIDs); err != nil {
		klog.Errorf("create address set %s for sg %s: %v", v4AsName, key, err)
		return err
	}

	if err = c.OVNNbClient.CreateAddressSet(v6AsName, externalIDs); err != nil {
		klog.Errorf("create address set %s for sg %s: %v", v6AsName, key, err)
		return err
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
		if err = c.OVNNbClient.UpdateSgACL(sg, ovnnb.ACLDirectionToLport); err != nil {
			sg.Status.IngressLastSyncSuccess = false
			c.patchSgStatus(sg)
			klog.Error(err)
			return err
		}

		if err := c.OVNNbClient.CreateSgBaseACL(sg.Name, ovnnb.ACLDirectionToLport); err != nil {
			klog.Error(err)
			return err
		}
		sg.Status.IngressMd5 = newIngressMd5
		sg.Status.IngressLastSyncSuccess = true
		c.patchSgStatus(sg)
	}
	if egressNeedUpdate {
		if err = c.OVNNbClient.UpdateSgACL(sg, ovnnb.ACLDirectionFromLport); err != nil {
			sg.Status.IngressLastSyncSuccess = false
			c.patchSgStatus(sg)
			klog.Error(err)
			return err
		}

		if err := c.OVNNbClient.CreateSgBaseACL(sg.Name, ovnnb.ACLDirectionFromLport); err != nil {
			klog.Error(err)
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
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().SecurityGroups().Patch(context.Background(), sg.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Error("patch security group status failed", err)
	}
}

func (c *Controller) handleDeleteSg(key string) error {
	c.sgKeyMutex.LockKey(key)
	defer func() { _ = c.sgKeyMutex.UnlockKey(key) }()
	klog.Infof("handle delete security group %s", key)

	if err := c.OVNNbClient.DeleteSecurityGroup(key); err != nil {
		klog.Errorf("delete sg %s: %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) syncSgLogicalPort(key string) error {
	c.sgKeyMutex.LockKey(key)
	defer func() { _ = c.sgKeyMutex.UnlockKey(key) }()
	klog.Infof("sync lsp for security group %s", key)

	sgPorts, err := c.OVNNbClient.ListLogicalSwitchPorts(false, map[string]string{fmt.Sprintf("associated_sg_%s", key): "true"}, nil)
	if err != nil {
		klog.Errorf("failed to find logical port, %v", err)
		return err
	}

	var ports, v4s, v6s []string
	for _, lsp := range sgPorts {
		if len(lsp.PortSecurity) == 0 {
			continue
		}
		ports = append(ports, lsp.Name)
		for _, ps := range lsp.PortSecurity {
			fields := strings.Fields(ps)
			if len(fields) < 2 {
				continue
			}
			for _, address := range fields[1:] {
				if strings.Contains(address, ":") {
					v6s = append(v6s, address)
				} else {
					v4s = append(v4s, address)
				}
			}
		}
	}

	sg, err := c.sgsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("sg '%s' not found.", key)
			return nil
		}
		klog.Errorf("failed to get sg '%s'. %v", key, err)
		return err
	}

	if err = c.OVNNbClient.PortGroupSetPorts(sg.Status.PortGroup, ports); err != nil {
		klog.Errorf("add ports to port group %s: %v", sg.Status.PortGroup, err)
		return err
	}

	v4AsName := ovs.GetSgV4AssociatedName(key)
	if err := c.OVNNbClient.AddressSetUpdateAddress(v4AsName, v4s...); err != nil {
		klog.Errorf("set ips to address set %s: %v", v4AsName, err)
		return err
	}

	v6AsName := ovs.GetSgV6AssociatedName(key)
	if err := c.OVNNbClient.AddressSetUpdateAddress(v6AsName, v6s...); err != nil {
		klog.Errorf("set ips to address set %s: %v", v6AsName, err)
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
	port, err := c.OVNNbClient.GetLogicalSwitchPort(portName, false)
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
		if slices.Contains(newSgList, sgName) {
			needAssociated = "true"
		}

		if err = c.OVNNbClient.SetLogicalSwitchPortExternalIDs(portName, map[string]string{fmt.Sprintf("associated_sg_%s", sgName): needAssociated}); err != nil {
			klog.Errorf("set logical switch port %s external_ids: %v", portName, err)
			return err
		}
		c.syncSgPortsQueue.Add(sgName)
	}

	if err = c.OVNNbClient.SetLogicalSwitchPortExternalIDs(portName, map[string]string{"security_groups": strings.ReplaceAll(securityGroups, ",", "/")}); err != nil {
		klog.Errorf("set logical switch port %s external_ids: %v", portName, err)
		return err
	}

	return nil
}

// securityGroupAllNotExist return true if all sgs does not exist
func (c *Controller) securityGroupAllNotExist(sgs []string) (bool, error) {
	if len(sgs) == 0 {
		return true, nil
	}

	notExistsCount := 0
	// sgs format: sg1/sg2/sg3
	for _, sg := range sgs {
		ok, err := c.OVNNbClient.PortGroupExists(ovs.GetSgPortGroupName(sg))
		if err != nil {
			klog.Error(err)
			return true, err
		}

		if !ok {
			notExistsCount++
		}
	}

	return notExistsCount == len(sgs), nil
}
