package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIP(obj interface{}) {
	ipObj := obj.(*kubeovnv1.IP)
	if strings.HasPrefix(ipObj.Name, util.U2OInterconnName[0:19]) {
		return
	}
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(ipObj.Spec.Subnet)
	for _, as := range ipObj.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update attach status for subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add ip %s", key)
	c.addIPQueue.Add(key)
}

func (c *Controller) enqueueUpdateIP(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldIP := oldObj.(*kubeovnv1.IP)
	newIP := newObj.(*kubeovnv1.IP)
	// ip can not change these specs below
	if oldIP.Spec.Subnet != "" && newIP.Spec.Subnet != oldIP.Spec.Subnet {
		klog.Errorf("ip %s subnet can not change", newIP.Name)
	}
	if oldIP.Spec.Namespace != "" && newIP.Spec.Namespace != oldIP.Spec.Namespace {
		klog.Errorf("ip %s namespace can not change", newIP.Name)
	}
	if oldIP.Spec.PodName != "" && newIP.Spec.PodName != oldIP.Spec.PodName {
		klog.Errorf("ip %s podName can not change", newIP.Name)
	}
	if oldIP.Spec.PodType != "" && newIP.Spec.PodType != oldIP.Spec.PodType {
		klog.Errorf("ip %s podType can not change", newIP.Name)
	}
	if oldIP.Spec.MacAddress != "" && newIP.Spec.MacAddress != oldIP.Spec.MacAddress {
		klog.Errorf("ip %s macAddress can not change", newIP.Name)
	}
	if oldIP.Spec.V4IPAddress != "" && newIP.Spec.V4IPAddress != oldIP.Spec.V4IPAddress {
		klog.Errorf("ip %s v4IPAddress can not change", newIP.Name)
	}
	if oldIP.Spec.V6IPAddress != "" {
		// v6 ip address can not use upper case
		if util.ContainsUppercase(newIP.Spec.V6IPAddress) {
			err := fmt.Errorf("ip %s v6 ip address %s can not contain upper case", newIP.Name, newIP.Spec.V6IPAddress)
			klog.Error(err)
		}
		if newIP.Spec.V6IPAddress != oldIP.Spec.V6IPAddress {
			klog.Errorf("ip %s v6IPAddress can not change", newIP.Name)
		}
	}
	if !newIP.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update ip %s", key)
		c.updateIPQueue.Add(key)
		return
	}
	if !reflect.DeepEqual(oldIP.Spec.AttachSubnets, newIP.Spec.AttachSubnets) {
		klog.V(3).Infof("enqueue update status subnet %s", newIP.Spec.Subnet)
		for _, as := range newIP.Spec.AttachSubnets {
			klog.V(3).Infof("enqueue update status for attach subnet %s", as)
			c.updateSubnetStatusQueue.Add(as)
		}
	}
}

func (c *Controller) enqueueDelIP(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	ipObj := obj.(*kubeovnv1.IP)
	if strings.HasPrefix(ipObj.Name, util.U2OInterconnName[0:19]) {
		return
	}
	klog.V(3).Infof("enqueue del ip %s", key)
	c.delIPQueue.Add(ipObj)
}

func (c *Controller) handleAddReservedIP(key string) error {
	ip, err := c.ipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	klog.V(3).Infof("handle add reserved ip %s", ip.Name)
	if ip.Spec.Subnet == "" {
		err := errors.New("subnet parameter cannot be empty")
		klog.Error(err)
		return err
	}
	if ip.Spec.PodType != "" && ip.Spec.PodType != util.VM && ip.Spec.PodType != util.StatefulSet {
		err := fmt.Errorf("podType %s is not supported", ip.Spec.PodType)
		klog.Error(err)
		return err
	}

	subnet, err := c.subnetsLister.Get(ip.Spec.Subnet)
	if err != nil {
		err = fmt.Errorf("failed to get subnet %s: %w", ip.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	portName := ovs.PodNameToPortName(ip.Spec.PodName, ip.Spec.Namespace, subnet.Spec.Provider)
	if portName != ip.Name {
		// invalid ip or node ip, no need to handle it here
		klog.V(3).Infof("port name %s is not equal to ip name %s", portName, ip.Name)
		return nil
	}

	// not handle add the ip, which created in pod process, lsp created before ip
	lsp, err := c.OVNNbClient.GetLogicalSwitchPort(portName, true)
	if err != nil {
		klog.Errorf("failed to list logical switch ports %s, %v", portName, err)
		return err
	}
	if lsp != nil {
		// port already exists means the ip already created
		klog.V(3).Infof("ip %s is ready", portName)
		return nil
	}

	// v6 ip address can not use upper case
	if util.ContainsUppercase(ip.Spec.V6IPAddress) {
		err := fmt.Errorf("ip %s v6 ip address %s can not contain upper case", ip.Name, ip.Spec.V6IPAddress)
		klog.Error(err)
		return err
	}
	v4IP, v6IP, mac, err := c.ipAcquireAddress(ip, subnet)
	if err != nil {
		err = fmt.Errorf("failed to acquire ip address %w", err)
		klog.Error(err)
		return err
	}
	ipStr := util.GetStringIP(v4IP, v6IP)
	if err := c.createOrUpdateIPCR(ip.Name, ip.Spec.PodName, ipStr, mac, subnet.Name, ip.Spec.Namespace, ip.Spec.NodeName, ip.Spec.PodType); err != nil {
		err = fmt.Errorf("failed to create ips CR %s.%s: %w", ip.Spec.PodName, ip.Spec.Namespace, err)
		klog.Error(err)
		return err
	}
	if ip.Labels[util.IPReservedLabel] != "false" {
		cachedIP, err := c.ipsLister.Get(key)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Error(err)
			return err
		}
		ip = cachedIP.DeepCopy()
		ip.Labels[util.IPReservedLabel] = "true"
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, err := json.Marshal(ip.Labels)
		if err != nil {
			klog.Error(err)
			return err
		}
		op := "replace"
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IPs().Patch(context.Background(), ip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ip %s, %v", ip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdateIP(key string) error {
	cachedIP, err := c.ipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedIP.DeletionTimestamp.IsZero() {
		klog.Infof("handle deleting ip %s", cachedIP.Name)
		subnet, err := c.subnetsLister.Get(cachedIP.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", cachedIP.Spec.Subnet, err)
			return err
		}
		if isOvnSubnet(subnet) {
			portName := cachedIP.Name
			port, err := c.OVNNbClient.GetLogicalSwitchPort(portName, true)
			if err != nil {
				klog.Errorf("failed to get logical switch port %s: %v", portName, err)
				return err
			}
			if port != nil {
				klog.Infof("delete ip cr lsp %s from switch %s", portName, subnet.Name)
				if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
					klog.Errorf("failed to delete ip cr lsp %s from switch %s: %v", portName, subnet.Name, err)
					return err
				}
				klog.V(3).Infof("sync sg for deleted port %s", portName)
				sgList, err := c.getPortSg(port)
				if err != nil {
					klog.Errorf("get port sg failed, %v", err)
					return err
				}
				for _, sgName := range sgList {
					if sgName != "" {
						c.syncSgPortsQueue.Add(sgName)
					}
				}
			}
		}
		podKey := fmt.Sprintf("%s/%s", cachedIP.Spec.Namespace, cachedIP.Spec.PodName)
		klog.Infof("ip cr %s release ipam pod key %s from subnet %s", cachedIP.Name, podKey, cachedIP.Spec.Subnet)
		c.ipam.ReleaseAddressByPod(podKey, cachedIP.Spec.Subnet)
		if err = c.handleDelIPFinalizer(cachedIP); err != nil {
			klog.Errorf("failed to handle del ip finalizer %v", err)
			return err
		}
		c.updateSubnetStatusQueue.Add(cachedIP.Spec.Subnet)
	}
	return nil
}

func (c *Controller) handleDelIP(ip *kubeovnv1.IP) error {
	klog.Infof("deleting ip %s enqueue update status subnet %s", ip.Name, ip.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(ip.Spec.Subnet)
	for _, as := range ip.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update attach status for subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
	return nil
}

func (c *Controller) syncIPFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	ips := &kubeovnv1.IPList{}
	return migrateFinalizers(cl, ips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(ips.Items) {
			return nil, nil
		}
		return ips.Items[i].DeepCopy(), ips.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIPFinalizer(cachedIP *kubeovnv1.IP) error {
	if !cachedIP.DeletionTimestamp.IsZero() || len(cachedIP.GetFinalizers()) != 0 {
		return nil
	}
	newIP := cachedIP.DeepCopy()
	controllerutil.AddFinalizer(newIP, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIP, newIP)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ip %s, %v", cachedIP.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IPs().Patch(context.Background(), cachedIP.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ip %s, %v", cachedIP.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIPFinalizer(cachedIP *kubeovnv1.IP) error {
	if len(cachedIP.GetFinalizers()) == 0 {
		return nil
	}
	newIP := cachedIP.DeepCopy()
	controllerutil.RemoveFinalizer(newIP, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newIP, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIP, newIP)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ip %s, %v", cachedIP.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IPs().Patch(context.Background(), cachedIP.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ip %s, %v", cachedIP.Name, err)
		return err
	}
	return nil
}

func (c *Controller) acquireIPAddress(subnetName, name, nicName string) (string, string, string, error) {
	var skippedAddrs []string
	var v4ip, v6ip, mac string
	checkConflict := true
	var err error
	for {
		v4ip, v6ip, mac, err = c.ipam.GetRandomAddress(name, nicName, nil, subnetName, "", skippedAddrs, checkConflict)
		if err != nil {
			klog.Error(err)
			return "", "", "", err
		}

		ipv4OK, ipv6OK, err := c.validatePodIP(name, subnetName, v4ip, v6ip)
		if err != nil {
			klog.Error(err)
			return "", "", "", err
		}

		if ipv4OK && ipv6OK {
			return v4ip, v6ip, mac, nil
		}

		if !ipv4OK {
			skippedAddrs = append(skippedAddrs, v4ip)
		}
		if !ipv6OK {
			skippedAddrs = append(skippedAddrs, v6ip)
		}
	}
}

func (c *Controller) acquireStaticIPAddress(subnetName, name, nicName, ip string) (string, string, string, error) {
	checkConflict := true
	var v4ip, v6ip, mac string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse vip ip %s", ipStr)
		}
	}

	if v4ip, v6ip, mac, err = c.ipam.GetStaticAddress(name, nicName, ip, nil, subnetName, checkConflict); err != nil {
		klog.Errorf("failed to get static virtual ip '%s', mac '%s', subnet '%s', %v", ip, mac, subnetName, err)
		return "", "", "", err
	}
	return v4ip, v6ip, mac, nil
}

func (c *Controller) createOrUpdateIPCR(ipCRName, podName, ip, mac, subnetName, ns, nodeName, podType string) error {
	// `ipCRName`: pod or vm IP name must set ip CR name when creating ip CR
	var key, ipName string
	if ipCRName != "" {
		// pod IP
		key = podName
		ipName = ipCRName
	} else {
		// node IP or interconn IP
		switch {
		case subnetName == c.config.NodeSwitch:
			key = nodeName
			ipName = util.NodeLspName(nodeName)
		case strings.HasPrefix(podName, util.U2OInterconnName[0:19]):
			key = podName // interconn IP name
			ipName = podName
		}
	}

	var err error
	var ipCR *kubeovnv1.IP
	ipCR, err = c.ipsLister.Get(ipName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			err := fmt.Errorf("failed to get ip CR %s: %w", ipName, err)
			klog.Error(err)
			return err
		}
		// the returned pointer is not nil if the CR does not exist
		ipCR = nil
	}

	v4IP, v6IP := util.SplitStringIP(ip)
	if ipCR == nil {
		ipCR, err = c.config.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
			ObjectMeta: metav1.ObjectMeta{
				Name: ipName,
				Labels: map[string]string{
					util.SubnetNameLabel: subnetName,
					util.NodeNameLabel:   nodeName,
					subnetName:           "",
					util.IPReservedLabel: "false", // ip create with pod or node, ip not reserved
				},
			},
			Spec: kubeovnv1.IPSpec{
				PodName:       key,
				Subnet:        subnetName,
				NodeName:      nodeName,
				Namespace:     ns,
				IPAddress:     ip,
				V4IPAddress:   v4IP,
				V6IPAddress:   v6IP,
				MacAddress:    mac,
				AttachIPs:     []string{},
				AttachMacs:    []string{},
				AttachSubnets: []string{},
				PodType:       podType,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			errMsg := fmt.Errorf("failed to create ip CR %s: %w", ipName, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		newIPCR := ipCR.DeepCopy()
		if newIPCR.Labels != nil {
			newIPCR.Labels[util.SubnetNameLabel] = subnetName
			newIPCR.Labels[util.NodeNameLabel] = nodeName
		} else {
			newIPCR.Labels = map[string]string{
				util.SubnetNameLabel: subnetName,
				util.NodeNameLabel:   nodeName,
			}
			// update not touch IP Reserved Label
		}
		newIPCR.Spec.PodName = key
		newIPCR.Spec.Namespace = ns
		newIPCR.Spec.Subnet = subnetName
		newIPCR.Spec.NodeName = nodeName
		newIPCR.Spec.IPAddress = ip
		newIPCR.Spec.V4IPAddress = v4IP
		newIPCR.Spec.V6IPAddress = v6IP
		newIPCR.Spec.MacAddress = mac
		newIPCR.Spec.AttachIPs = []string{}
		newIPCR.Spec.AttachMacs = []string{}
		newIPCR.Spec.AttachSubnets = []string{}
		newIPCR.Spec.PodType = podType
		if reflect.DeepEqual(newIPCR.Labels, ipCR.Labels) && reflect.DeepEqual(newIPCR.Spec, ipCR.Spec) {
			return nil
		}

		ipCR, err = c.config.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), newIPCR, metav1.UpdateOptions{})
		if err != nil {
			err := fmt.Errorf("failed to update ip CR %s: %w", ipCRName, err)
			klog.Error(err)
			return err
		}
	}
	if err := c.handleAddIPFinalizer(ipCR); err != nil {
		klog.Errorf("failed to handle add ip finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) ipAcquireAddress(ip *kubeovnv1.IP, subnet *kubeovnv1.Subnet) (string, string, string, error) {
	key := fmt.Sprintf("%s/%s", ip.Spec.Namespace, ip.Spec.PodName)
	portName := ovs.PodNameToPortName(ip.Spec.PodName, ip.Spec.Namespace, subnet.Spec.Provider)
	ipStr := util.GetStringIP(ip.Spec.V4IPAddress, ip.Spec.V6IPAddress)

	var v4IP, v6IP, mac string
	var err error
	var macPtr *string
	if isOvnSubnet(subnet) {
		if ip.Spec.MacAddress != "" {
			macPtr = &ip.Spec.MacAddress
		}
	} else {
		macPtr = ptr.To("")
	}

	if ipStr == "" {
		// allocate address
		v4IP, v6IP, mac, err = c.acquireIPAddress(subnet.Name, ip.Name, portName)
		if err == nil {
			return v4IP, v6IP, mac, err
		}
		err = fmt.Errorf("failed to get random address for ip %s, %w", ip.Name, err)
	} else {
		// static address
		v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, ipStr, macPtr, subnet.Name, true)
		if err == nil {
			return v4IP, v6IP, mac, nil
		}
		err = fmt.Errorf("failed to get static address for ip %s, %w", ip.Name, err)
	}
	klog.Error(err)
	return "", "", "", err
}
