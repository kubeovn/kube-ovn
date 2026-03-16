package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/scylladb/go-set/strset"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) InitOVN() error {
	var err error

	// migrate vendor externalIDs to kube-ovn resources created in versions prior to v1.15.0
	// this must run before ACL cleanup to ensure existing resources are properly tagged
	if err = c.OVNNbClient.MigrateVendorExternalIDs(); err != nil {
		klog.Errorf("failed to migrate vendor externalIDs: %v", err)
		return err
	}

	// migrate tier field of ACL rules created in versions prior to v1.13.0
	// after upgrading, the tier field has a default value of zero, which is not the value used in versions >= v1.13.0
	// we need to migrate the tier field to the correct value
	if err = c.OVNNbClient.MigrateACLTier(); err != nil {
		klog.Errorf("failed to migrate ACL tier: %v", err)
		return err
	}

	// clean all no parent key acls
	if err = c.OVNNbClient.CleanNoParentKeyAcls(); err != nil {
		klog.Errorf("failed to clean all no parent key acls: %v", err)
		return err
	}

	if err = c.InitDefaultVpc(); err != nil {
		klog.Errorf("init default vpc failed: %v", err)
		return err
	}

	if err = c.initClusterRouter(); err != nil {
		klog.Errorf("init cluster router failed: %v", err)
		return err
	}

	if c.config.EnableLb {
		if err = c.initLoadBalancer(); err != nil {
			klog.Errorf("init load balancer failed: %v", err)
			return err
		}
	}

	if err = c.initDefaultVlan(); err != nil {
		klog.Errorf("init default vlan failed: %v", err)
		return err
	}

	if err = c.initNodeSwitch(); err != nil {
		klog.Errorf("init node switch failed: %v", err)
		return err
	}

	if err = c.initDefaultLogicalSwitch(); err != nil {
		klog.Errorf("init default switch failed: %v", err)
		return err
	}

	return nil
}

func (c *Controller) InitDefaultVpc() error {
	cachedVpc, err := c.vpcsLister.Get(c.config.ClusterRouter)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get default vpc %q: %v", c.config.ClusterRouter, err)
			return err
		}
		// create default vpc
		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: c.config.ClusterRouter},
		}
		cachedVpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("failed to create default vpc %q: %v", c.config.ClusterRouter, err)
			return err
		}
	}

	// update default vpc status
	vpc := cachedVpc.DeepCopy()
	if !vpc.Status.Default || !vpc.Status.Standby ||
		vpc.Status.Router != c.config.ClusterRouter ||
		vpc.Status.DefaultLogicalSwitch != c.config.DefaultLogicalSwitch {
		vpc.Status.Standby = true
		vpc.Status.Default = true
		vpc.Status.Router = c.config.ClusterRouter
		vpc.Status.DefaultLogicalSwitch = c.config.DefaultLogicalSwitch

		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().UpdateStatus(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update default vpc %q: %v", c.config.ClusterRouter, err)
			return err
		}
	}

	return nil
}

// InitDefaultLogicalSwitch init the default logical switch for ovn network
func (c *Controller) initDefaultLogicalSwitch() error {
	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err == nil {
		if subnet != nil && util.CheckProtocol(c.config.DefaultCIDR) != util.CheckProtocol(subnet.Spec.CIDRBlock) {
			// single-stack upgrade to dual-stack
			if util.CheckProtocol(c.config.DefaultCIDR) == kubeovnv1.ProtocolDual {
				subnet := subnet.DeepCopy()
				subnet.Spec.CIDRBlock = c.config.DefaultCIDR
				if _, err = c.formatSubnet(subnet); err != nil {
					klog.Errorf("init format subnet %s failed: %v", c.config.DefaultLogicalSwitch, err)
					return err
				}
			}
		}
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		klog.Errorf("get default subnet %s failed: %v", c.config.DefaultLogicalSwitch, err)
		return err
	}

	defaultSubnet := kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: c.config.DefaultLogicalSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:                 c.config.ClusterRouter,
			Default:             true,
			Provider:            util.OvnProvider,
			CIDRBlock:           c.config.DefaultCIDR,
			Gateway:             c.config.DefaultGateway,
			GatewayNode:         "",
			DisableGatewayCheck: !c.config.DefaultGatewayCheck,
			ExcludeIps:          strings.Split(c.config.DefaultExcludeIps, ","),
			NatOutgoing:         true,
			GatewayType:         kubeovnv1.GWDistributedType,
			Protocol:            util.CheckProtocol(c.config.DefaultCIDR),
			EnableLb:            &c.config.EnableLb,
		},
	}
	if c.config.NetworkType == util.NetworkTypeVlan {
		defaultSubnet.Spec.Vlan = c.config.DefaultVlanName
		if c.config.DefaultLogicalGateway && c.config.DefaultU2OInterconnection {
			err = errors.New("logicalGateway and u2oInterconnection can't be opened at the same time")
			klog.Error(err)
			return err
		}
		defaultSubnet.Spec.LogicalGateway = c.config.DefaultLogicalGateway
		defaultSubnet.Spec.U2OInterconnection = c.config.DefaultU2OInterconnection
	}

	if _, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &defaultSubnet, metav1.CreateOptions{}); err != nil {
		klog.Errorf("failed to create default subnet %q: %v", c.config.DefaultLogicalSwitch, err)
		return err
	}
	return nil
}

// InitNodeSwitch init node switch to connect host and pod
func (c *Controller) initNodeSwitch() error {
	subnet, err := c.subnetsLister.Get(c.config.NodeSwitch)
	if err == nil {
		if util.CheckProtocol(c.config.NodeSwitchCIDR) == kubeovnv1.ProtocolDual && util.CheckProtocol(subnet.Spec.CIDRBlock) != kubeovnv1.ProtocolDual {
			// single-stack upgrade to dual-stack
			subnet := subnet.DeepCopy()
			subnet.Spec.CIDRBlock = c.config.NodeSwitchCIDR
			if _, err = c.formatSubnet(subnet); err != nil {
				klog.Errorf("init format subnet %s failed: %v", c.config.NodeSwitch, err)
				return err
			}
		} else {
			c.config.NodeSwitchCIDR = subnet.Spec.CIDRBlock
		}
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		klog.Errorf("get node subnet %s failed: %v", c.config.NodeSwitch, err)
		return err
	}

	nodeSubnet := kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: c.config.NodeSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:                    c.config.ClusterRouter,
			Default:                false,
			Provider:               util.OvnProvider,
			CIDRBlock:              c.config.NodeSwitchCIDR,
			Gateway:                c.config.NodeSwitchGateway,
			GatewayNode:            "",
			ExcludeIps:             strings.Split(c.config.NodeSwitchGateway, ","),
			Protocol:               util.CheckProtocol(c.config.NodeSwitchCIDR),
			DisableInterConnection: true,
		},
	}

	if _, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &nodeSubnet, metav1.CreateOptions{}); err != nil {
		klog.Errorf("failed to create node subnet %q: %v", c.config.NodeSwitch, err)
		return err
	}
	return nil
}

// InitClusterRouter init cluster router to connect different logical switches
func (c *Controller) initClusterRouter() error {
	if err := c.OVNNbClient.CreateLogicalRouter(c.config.ClusterRouter); err != nil {
		klog.Errorf("create logical router %s failed: %v", c.config.ClusterRouter, err)
		return err
	}

	lr, err := c.OVNNbClient.GetLogicalRouter(c.config.ClusterRouter, false)
	if err != nil {
		klog.Errorf("get logical router %s failed: %v", c.config.ClusterRouter, err)
		return err
	}

	lrOptions := make(map[string]string, len(lr.Options))
	maps.Copy(lrOptions, lr.Options)
	lrOptions["mac_binding_age_threshold"] = "300"
	lrOptions["dynamic_neigh_routers"] = "true"
	if !maps.Equal(lr.Options, lrOptions) {
		lr.Options = lrOptions
		if err = c.OVNNbClient.UpdateLogicalRouter(lr, &lr.Options); err != nil {
			klog.Errorf("update logical router %s failed: %v", c.config.ClusterRouter, err)
			return err
		}
	}

	return nil
}

func (c *Controller) initLB(name, protocol string, sessionAffinity bool) error {
	protocol = strings.ToLower(protocol)

	var (
		selectFields string
		err          error
	)

	if sessionAffinity {
		selectFields = ovnnb.LoadBalancerSelectionFieldsIPSrc
	}

	if err = c.OVNNbClient.CreateLoadBalancer(name, protocol, selectFields); err != nil {
		klog.Errorf("create load balancer %s: %v", name, err)
		return err
	}

	if sessionAffinity {
		if err = c.OVNNbClient.SetLoadBalancerAffinityTimeout(name, util.DefaultServiceSessionStickinessTimeout); err != nil {
			klog.Errorf("failed to set affinity timeout of %s load balancer %s: %v", protocol, name, err)
			return err
		}
	}

	err = c.OVNNbClient.SetLoadBalancerPreferLocalBackend(name, c.config.EnableOVNLBPreferLocal)
	if err != nil {
		klog.Errorf("failed to set prefer local backend for load balancer %s: %v", name, err)
		return err
	}

	return nil
}

// InitLoadBalancer init the default tcp and udp cluster loadbalancer
func (c *Controller) initLoadBalancer() error {
	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc: %v", err)
		return err
	}

	for _, cachedVpc := range vpcs {
		vpc := cachedVpc.DeepCopy()
		vpcLb := c.GenVpcLoadBalancer(vpc.Name)
		if err = c.initLB(vpcLb.TCPLoadBalancer, string(v1.ProtocolTCP), false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.TCPSessLoadBalancer, string(v1.ProtocolTCP), true); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.UDPLoadBalancer, string(v1.ProtocolUDP), false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.UDPSessLoadBalancer, string(v1.ProtocolUDP), true); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.SctpLoadBalancer, string(v1.ProtocolSCTP), false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.SctpSessLoadBalancer, string(v1.ProtocolSCTP), true); err != nil {
			klog.Error(err)
			return err
		}

		vpc.Status.TCPLoadBalancer = vpcLb.TCPLoadBalancer
		vpc.Status.TCPSessionLoadBalancer = vpcLb.TCPSessLoadBalancer
		vpc.Status.UDPLoadBalancer = vpcLb.UDPLoadBalancer
		vpc.Status.UDPSessionLoadBalancer = vpcLb.UDPSessLoadBalancer
		vpc.Status.SctpLoadBalancer = vpcLb.SctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = vpcLb.SctpSessLoadBalancer
		bytes, err := vpc.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) InitIPAM() error {
	start := time.Now()
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet: %v", err)
		return err
	}
	subnetProviderMaps := make(map[string]string, len(subnets))
	for _, subnet := range subnets {
		klog.Infof("Init subnet %s", subnet.Name)
		subnetProviderMaps[subnet.Name] = subnet.Spec.Provider
		if err := c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps); err != nil {
			klog.Errorf("failed to init subnet %s: %v", subnet.Name, err)
		}

		u2oInterconnName := fmt.Sprintf(util.U2OInterconnName, subnet.Spec.Vpc, subnet.Name)
		u2oInterconnLrpName := fmt.Sprintf("%s-%s", subnet.Spec.Vpc, subnet.Name)
		if subnet.Status.U2OInterconnectionIP != "" {
			var mac *string
			klog.Infof("Init U2O for subnet %s", subnet.Name)
			if subnet.Status.U2OInterconnectionMAC != "" {
				mac = ptr.To(subnet.Status.U2OInterconnectionMAC)
			} else {
				lrp, err := c.OVNNbClient.GetLogicalRouterPort(u2oInterconnLrpName, true)
				if err != nil {
					klog.Errorf("failed to get logical router port %s: %v", u2oInterconnLrpName, err)
					return err
				}
				if lrp != nil {
					mac = ptr.To(lrp.MAC)
				}
			}
			if _, _, _, err = c.ipam.GetStaticAddress(u2oInterconnName, u2oInterconnLrpName, subnet.Status.U2OInterconnectionIP, mac, subnet.Name, true); err != nil {
				klog.Errorf("failed to init subnet %q u2o interconnection ip to ipam %v", subnet.Name, err)
			}
		}
	}

	ippools, err := c.ippoolLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ippool: %v", err)
		return err
	}
	for _, ippool := range ippools {
		if err = c.ipam.AddOrUpdateIPPool(ippool.Spec.Subnet, ippool.Name, ippool.Spec.IPs); err != nil {
			klog.Errorf("failed to init ippool %s: %v", ippool.Name, err)
		}
	}

	klog.Infof("Init IPAM from StatefulSet or VM IP CR")
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list IPs: %v", err)
		return err
	}

	for _, ip := range ips {
		if !ip.DeletionTimestamp.IsZero() {
			klog.Infof("enqueue update for removing finalizer to delete ip %s", ip.Name)
			c.updateIPQueue.Add(ip.Name)
			continue
		}
		// recover sts and kubevirt vm ip, other ip recover in later pod loop
		if ip.Spec.PodType != util.KindStatefulSet &&
			ip.Spec.PodType != util.KindVirtualMachine {
			continue
		}

		var ipamKey string
		if ip.Spec.Namespace != "" {
			ipamKey = fmt.Sprintf("%s/%s", ip.Spec.Namespace, ip.Spec.PodName)
		} else {
			ipamKey = util.NodeLspName(ip.Spec.PodName)
		}
		if _, _, _, err = c.ipam.GetStaticAddress(ipamKey, ip.Name, ip.Spec.IPAddress, &ip.Spec.MacAddress, ip.Spec.Subnet, true); err != nil {
			klog.Errorf("failed to init IPAM from IP CR %s: %v", ip.Name, err)
		}
	}

	klog.Infof("Init IPAM from pod")
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods: %v", err)
		return err
	}
	for _, pod := range pods {
		if pod.Spec.HostNetwork {
			continue
		}

		isAlive := isPodAlive(pod)
		isStsPod, _, _ := isStatefulSetPod(pod)
		if !isAlive && !isStsPod {
			continue
		}

		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to get pod kubeovn nets %s.%s address %s: %v", pod.Name, pod.Namespace, pod.Annotations[util.IPAddressAnnotation], err)
			continue
		}

		podType := getPodType(pod)
		podName := c.getNameByPod(pod)
		key := fmt.Sprintf("%s/%s", pod.Namespace, podName)
		for _, podNet := range podNets {
			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
				mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
				if ip == "" {
					klog.Warningf("pod %s/%s has empty IP annotation for provider %s, skip IPAM init", pod.Namespace, podName, podNet.ProviderName)
					continue
				}
				_, _, _, err := c.ipam.GetStaticAddress(key, portName, ip, &mac, podNet.Subnet.Name, true)
				if err != nil {
					klog.Errorf("failed to init pod %s.%s address %s: %v", podName, pod.Namespace, ip, err)
				} else {
					err = c.createOrUpdateIPCR(portName, podName, ip, mac, podNet.Subnet.Name, pod.Namespace, pod.Spec.NodeName, podType)
					if err != nil {
						klog.Errorf("failed to create/update ips CR %s.%s with ip address %s: %v", podName, pod.Namespace, ip, err)
					}
				}

				// Append ExternalIds is added in v1.7, used for upgrading from v1.6.3. It should be deleted now since v1.7 is not used anymore.
			}
		}
	}

	klog.Infof("Init IPAM from vip CR")
	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vips: %v", err)
		return err
	}
	for _, vip := range vips {
		provider, ok := subnetProviderMaps[vip.Spec.Subnet]
		if !ok {
			klog.Errorf("failed to find subnet %s for vip %s", vip.Spec.Subnet, vip.Name)
			continue
		}
		portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, provider)
		if _, _, _, err = c.ipam.GetStaticAddress(vip.Name, portName, vip.Status.V4ip, &vip.Status.Mac, vip.Spec.Subnet, true); err != nil {
			klog.Errorf("failed to init ipam from vip cr %s: %v", vip.Name, err)
		}
	}

	klog.Infof("Init IPAM from iptables EIP CR")
	eips, err := c.iptablesEipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list EIPs: %v", err)
		return err
	}
	for _, eip := range eips {
		externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
		if _, _, _, err = c.ipam.GetStaticAddress(eip.Name, eip.Name, eip.Status.IP, &eip.Spec.MacAddress, externalNetwork, true); err != nil {
			klog.Errorf("failed to init ipam from iptables eip cr %s: %v", eip.Name, err)
		}
	}

	klog.Infof("Init IPAM from ovn EIP CR")
	oeips, err := c.ovnEipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ovn eips: %v", err)
		return err
	}
	for _, oeip := range oeips {
		if _, _, _, err = c.ipam.GetStaticAddress(oeip.Name, oeip.Name, oeip.Status.V4Ip, &oeip.Status.MacAddress, oeip.Spec.ExternalSubnet, true); err != nil {
			klog.Errorf("failed to init ipam from ovn eip cr %s: %v", oeip.Name, err)
		}
	}

	klog.Infof("Init IPAM from node annotation")
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] == "true" {
			portName := util.NodeLspName(node.Name)
			mac := node.Annotations[util.MacAddressAnnotation]
			v4IP, v6IP, _, err := c.ipam.GetStaticAddress(portName, portName,
				node.Annotations[util.IPAddressAnnotation], &mac,
				node.Annotations[util.LogicalSwitchAnnotation], true)
			if err != nil {
				klog.Errorf("failed to init node %s.%s address %s: %v", node.Name, node.Namespace, node.Annotations[util.IPAddressAnnotation], err)
			}
			if v4IP != "" && v6IP != "" {
				node.Annotations[util.IPAddressAnnotation] = util.GetStringIP(v4IP, v6IP)
			}
		}
	}

	klog.Infof("take %.2f seconds to initialize IPAM", time.Since(start).Seconds())
	return nil
}

func (c *Controller) initDefaultProviderNetwork() error {
	_, err := c.providerNetworksLister.Get(c.config.DefaultProviderName)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get default provider network %s: %v", c.config.DefaultProviderName, err)
		return err
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get nodes: %v", err)
		return err
	}

	pn := kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.config.DefaultProviderName,
		},
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface: c.config.DefaultHostInterface,
			ExchangeLinkName: c.config.DefaultExchangeLinkName,
		},
	}

	excludeAnno := fmt.Sprintf(util.ProviderNetworkExcludeTemplate, c.config.DefaultProviderName)
	interfaceAnno := fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, c.config.DefaultProviderName)
	patchNodes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if len(node.Annotations) == 0 {
			continue
		}

		if node.Annotations[excludeAnno] == "true" {
			pn.Spec.ExcludeNodes = append(pn.Spec.ExcludeNodes, node.Name)
			patchNodes = append(patchNodes, node.Name)
		} else if s := node.Annotations[interfaceAnno]; s != "" {
			var index *int
			for i := range pn.Spec.CustomInterfaces {
				if pn.Spec.CustomInterfaces[i].Interface == s {
					index = &i
					break
				}
			}
			if index != nil {
				pn.Spec.CustomInterfaces[*index].Nodes = append(pn.Spec.CustomInterfaces[*index].Nodes, node.Name)
			} else {
				ci := kubeovnv1.CustomInterface{Interface: s, Nodes: []string{node.Name}}
				pn.Spec.CustomInterfaces = append(pn.Spec.CustomInterfaces, ci)
			}
			patchNodes = append(patchNodes, node.Name)
		}
	}

	defer func() {
		if err != nil {
			return
		}

		// update nodes only when provider network has been created successfully
		patch := util.KVPatch{excludeAnno: nil, interfaceAnno: nil}
		for _, node := range patchNodes {
			if err := util.PatchAnnotations(c.config.KubeClient.CoreV1().Nodes(), node, patch); err != nil {
				klog.Errorf("failed to patch node %s: %v", node, err)
			}
		}
	}()

	_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Create(context.Background(), &pn, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create provider network %s: %v", c.config.DefaultProviderName, err)
		return err
	}
	return nil
}

func (c *Controller) initDefaultVlan() error {
	if c.config.NetworkType != util.NetworkTypeVlan {
		return nil
	}

	if err := c.initDefaultProviderNetwork(); err != nil {
		klog.Error(err)
		return err
	}

	_, err := c.vlansLister.Get(c.config.DefaultVlanName)
	if err == nil {
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		klog.Errorf("get default vlan %s failed: %v", c.config.DefaultVlanName, err)
		return err
	}

	if c.config.DefaultVlanID < 0 || c.config.DefaultVlanID > 4095 {
		return errors.New("the default vlan id is not between 1-4095")
	}

	defaultVlan := kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: c.config.DefaultVlanName},
		Spec: kubeovnv1.VlanSpec{
			ID:       c.config.DefaultVlanID,
			Provider: c.config.DefaultProviderName,
		},
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Create(context.Background(), &defaultVlan, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create vlan %s: %v", defaultVlan.Name, err)
		return err
	}
	return nil
}

func (c *Controller) syncIPCR() error {
	klog.Info("start to sync ips")
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	ipMap := strset.New(c.getVMLsps()...)
	for _, ip := range ips {
		if !ip.DeletionTimestamp.IsZero() {
			klog.Infof("enqueue update for removing finalizer to delete ip %s", ip.Name)
			c.updateIPQueue.Add(ip.Name)
			continue
		}
		changed := false
		ip = ip.DeepCopy()
		if ipMap.Has(ip.Name) && ip.Spec.PodType == "" {
			ip.Spec.PodType = util.KindVirtualMachine
			changed = true
		}

		v4IP, v6IP := util.SplitStringIP(ip.Spec.IPAddress)
		if ip.Spec.V4IPAddress == v4IP && ip.Spec.V6IPAddress == v6IP && !changed {
			continue
		}

		ip.Spec.V4IPAddress = v4IP
		ip.Spec.V6IPAddress = v6IP
		_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ip, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to sync crd ip %s: %v", ip.Spec.IPAddress, err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncSubnetCR() error {
	klog.Info("start to sync subnets")
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	for _, cachedSubnet := range subnets {
		subnet := cachedSubnet.DeepCopy()
		if !subnet.Status.IsReady() {
			klog.Warningf("subnet %s is not ready", subnet.Name)
			continue
		}
		subnet, err = c.calcSubnetStatusIP(subnet)
		if err != nil {
			klog.Errorf("failed to calculate subnet %s used ip: %v", cachedSubnet.Name, err)
			return err
		}

		// only sync subnet spec enableEcmp when subnet.Spec.EnableEcmp is false and c.config.EnableEcmp is true
		if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType && !subnet.Spec.EnableEcmp && subnet.Spec.EnableEcmp != c.config.EnableEcmp {
			subnet, err = c.subnetsLister.Get(subnet.Name)
			if err != nil {
				klog.Errorf("failed to get subnet %s: %v", subnet.Name, err)
				return err
			}

			subnet.Spec.EnableEcmp = c.config.EnableEcmp
			if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), subnet, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to sync subnet spec enableEcmp with kube-ovn-controller config enableEcmp %s: %v", subnet.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) syncVpcNatGatewayCR() error {
	klog.Info("start to sync crd vpc nat gw")
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc nat gateway, %v", err)
		return err
	}
	if len(gws) == 0 {
		return nil
	}
	// get vpc nat gateway enable state
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get config map %s, %v", util.VpcNatGatewayConfig, err)
		return err
	}
	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-nat-gw"] == "false" {
		return nil
	}
	// get vpc nat gateway image
	cm, err = c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("should set config map for vpc-nat-gateway %s, %v", util.VpcNatConfig, err)
			return err
		}
		klog.Errorf("failed to get config map %s, %v", util.VpcNatConfig, err)
		return err
	}

	if cm.Data["image"] == "" {
		err = errors.New("should set image for vpc-nat-gateway pod")
		klog.Error(err)
		return err
	}

	for _, gw := range gws {
		if err := c.updateCrdNatGwLabels(gw.Name, ""); err != nil {
			klog.Errorf("failed to update nat gw %s: %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncVlanCR() error {
	klog.Info("start to sync vlans")
	vlans, err := c.vlansLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	for _, vlan := range vlans {
		var needUpdate bool
		newVlan := vlan.DeepCopy()
		if newVlan.Spec.VlanID != 0 && newVlan.Spec.ID == 0 {
			newVlan.Spec.ID = newVlan.Spec.VlanID
			newVlan.Spec.VlanID = 0
			needUpdate = true
		}
		if newVlan.Spec.ProviderInterfaceName != "" && newVlan.Spec.Provider == "" {
			newVlan.Spec.Provider = newVlan.Spec.ProviderInterfaceName
			newVlan.Spec.ProviderInterfaceName = ""
			needUpdate = true
		}
		if needUpdate {
			if _, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), newVlan, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to update spec of vlan %s: %v", newVlan.Name, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) batchMigrateNodeRoute(nodes []*v1.Node) error {
	start := time.Now()
	addPolicies := make([]*kubeovnv1.PolicyRoute, 0)
	delPolicies := make([]*kubeovnv1.PolicyRoute, 0)
	staticRoutes := make([]*kubeovnv1.StaticRoute, 0)
	externalIDsMap := make(map[string]map[string]string)
	delAsNames := make([]string, 0)
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] != "true" {
			continue
		}
		nodeName := node.Name
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
		joinAddrV4, joinAddrV6 := util.SplitStringIP(node.Annotations[util.IPAddressAnnotation])
		if nodeIPv4 != "" && joinAddrV4 != "" {
			buildNodeRoute(4, nodeName, joinAddrV4, nodeIPv4, &addPolicies, &delPolicies, &staticRoutes, externalIDsMap, &delAsNames)
		}
		if nodeIPv6 != "" && joinAddrV6 != "" {
			buildNodeRoute(6, nodeName, joinAddrV6, nodeIPv6, &addPolicies, &delPolicies, &staticRoutes, externalIDsMap, &delAsNames)
		}
	}

	if err := c.batchAddPolicyRouteToVpc(c.config.ClusterRouter, addPolicies, externalIDsMap); err != nil {
		klog.Errorf("failed to batch add logical router policy for lr %s nodes %d: %v", c.config.ClusterRouter, len(nodes), err)
		return err
	}
	if err := c.batchDeleteStaticRouteFromVpc(c.config.ClusterRouter, staticRoutes); err != nil {
		klog.Errorf("failed to batch delete  obsolete logical router static route for lr %s nodes %d: %v", c.config.ClusterRouter, len(nodes), err)
		return err
	}
	if err := c.batchDeletePolicyRouteFromVpc(c.config.ClusterRouter, delPolicies); err != nil {
		klog.Errorf("failed to batch delete obsolete logical router policy for lr %s nodes %d: %v", c.config.ClusterRouter, len(nodes), err)
		return err
	}
	if err := c.OVNNbClient.BatchDeleteAddressSetByNames(delAsNames); err != nil {
		klog.Errorf("failed to batch delete obsolete address set for asNames %v nodes %d: %v", delAsNames, len(nodes), err)
		return err
	}
	klog.V(3).Infof("take to %v batch migrate node route for router: %s priority: %d add policy len: %d extrenalID len: %d del policy len: %d del address set len: %d",
		time.Since(start), c.config.ClusterRouter, util.NodeRouterPolicyPriority, len(addPolicies), len(externalIDsMap), len(delPolicies), len(delAsNames))

	return nil
}

func buildNodeRoute(af int, nodeName, nexthop, ip string, addPolicies, delPolicies *[]*kubeovnv1.PolicyRoute, staticRoutes *[]*kubeovnv1.StaticRoute, externalIDsMap map[string]map[string]string, delAsNames *[]string) {
	var (
		match       = fmt.Sprintf("ip%d.dst == %s", af, ip)
		action      = kubeovnv1.PolicyRouteActionReroute
		externalIDs = map[string]string{
			"vendor": util.CniTypeName,
			"node":   nodeName,
		}
	)
	*addPolicies = append(*addPolicies, &kubeovnv1.PolicyRoute{
		Priority:  util.NodeRouterPolicyPriority,
		Match:     match,
		Action:    action,
		NextHopIP: nexthop,
	})
	externalIDsMap[buildExternalIDsMapKey(match, string(action), util.NodeRouterPolicyPriority)] = externalIDs
	*staticRoutes = append(*staticRoutes, &kubeovnv1.StaticRoute{
		Policy:     kubeovnv1.PolicyDst,
		RouteTable: util.MainRouteTable,
		NextHopIP:  "",
		CIDR:       ip,
	})
	asName := nodeUnderlayAddressSetName(nodeName, af)
	obsoleteMatch := fmt.Sprintf("ip%d.dst == %s && ip%d.src != $%s", af, ip, af, asName)
	*delPolicies = append(*delPolicies, &kubeovnv1.PolicyRoute{
		Match:    obsoleteMatch,
		Priority: util.NodeRouterPolicyPriority,
	})
	*delAsNames = append(*delAsNames, asName)
}

func (c *Controller) syncNodeRoutes() error {
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}

	if err := c.batchMigrateNodeRoute(nodes); err != nil {
		klog.Errorf("failed to batch migrate node routes: %v", err)
		return err
	}

	if err := c.addNodeGatewayStaticRoute(); err != nil {
		klog.Errorf("failed to add static route for node gateway")
		return err
	}
	return nil
}

func (c *Controller) initNodeChassis() error {
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}
	chassises, err := c.OVNSbClient.GetKubeOvnChassisses()
	if err != nil {
		klog.Errorf("failed to get chassis nodes: %v", err)
		return err
	}
	chassisNodes := make(map[string]string, len(*chassises))
	for _, chassis := range *chassises {
		chassisNodes[chassis.Name] = chassis.Hostname
	}
	for _, node := range nodes {
		if err := c.UpdateChassisTag(node); err != nil {
			klog.Error(err)
			if _, ok := err.(*ErrChassisNotFound); !ok {
				return err
			}
		}
	}
	return nil
}

func migrateFinalizers(c client.Client, list client.ObjectList, getObjectItem func(int) (client.Object, client.Object)) error {
	if err := c.List(context.Background(), list); err != nil {
		klog.Errorf("failed to list objects: %v", err)
		return err
	}

	var i int
	var cachedObj, patchedObj client.Object
	for {
		if cachedObj, patchedObj = getObjectItem(i); cachedObj == nil {
			break
		}
		if !controllerutil.ContainsFinalizer(cachedObj, util.DepreciatedFinalizerName) {
			i++
			continue
		}
		controllerutil.RemoveFinalizer(patchedObj, util.DepreciatedFinalizerName)
		if cachedObj.GetDeletionTimestamp() == nil {
			// if the object is not being deleted, add the new finalizer
			controllerutil.AddFinalizer(patchedObj, util.KubeOVNControllerFinalizer)
		}
		if err := c.Patch(context.Background(), patchedObj, client.MergeFrom(cachedObj)); client.IgnoreNotFound(err) != nil {
			klog.Errorf("failed to sync finalizers for %s %s: %v",
				patchedObj.GetObjectKind().GroupVersionKind().Kind,
				cache.MetaObjectToName(patchedObj), err)
			return err
		}
		i++
	}

	return nil
}

func (c *Controller) syncFinalizers() error {
	cl, err := client.New(config.GetConfigOrDie(), client.Options{})
	if err != nil {
		klog.Errorf("failed to create client: %v", err)
		return err
	}

	// migrate depreciated finalizer to new finalizer
	klog.Info("start to sync finalizers")
	if err := c.syncIPFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ip finalizer: %v", err)
		return err
	}
	if err := c.syncIPPoolFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ippool finalizer: %v", err)
		return err
	}
	if err := c.syncOvnDnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn dnat finalizer: %v", err)
		return err
	}
	if err := c.syncOvnEipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn eip finalizer: %v", err)
		return err
	}
	if err := c.syncOvnFipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn fip finalizer: %v", err)
		return err
	}
	if err := c.syncOvnSnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn snat finalizer: %v", err)
		return err
	}
	if err := c.syncQoSPolicyFinalizer(cl); err != nil {
		klog.Errorf("failed to sync qos policy finalizer: %v", err)
		return err
	}
	if err := c.syncSubnetFinalizer(cl); err != nil {
		klog.Errorf("failed to sync subnet finalizer: %v", err)
		return err
	}
	if err := c.syncVipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync vip finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesEipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables eip finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesFipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables fip finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesDnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables dnat finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesSnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables snat finalizer: %v", err)
		return err
	}
	klog.Info("sync finalizers done")
	return nil
}
