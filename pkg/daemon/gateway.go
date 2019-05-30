package daemon

import (
	"os"
	"strings"
	"time"

	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/projectcalico/felix/ipsets"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

const (
	SubnetSet   = "subnets"
	LocalPodSet = "local-pod-ip-nat"
	IPSetPrefix = "ovn"
)

var (
	natRule = util.IPTableRule{
		Table: "nat",
		Chain: "POSTROUTING",
		Rule:  strings.Split("-m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE", " "),
	}
	forwardAcceptRule1 = util.IPTableRule{
		Table: "filter",
		Chain: "FORWARD",
		Rule:  strings.Split("-i ovn0 -j ACCEPT", " "),
	}
	forwardAcceptRule2 = util.IPTableRule{
		Table: "filter",
		Chain: "FORWARD",
		Rule:  strings.Split(`-o ovn0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT`, " "),
	}
)

func (c *Controller) runGateway(stopCh <-chan struct{}) error {
	klog.Info("start gateway")
	subnets, err := c.getSubnets()
	if err != nil {
		klog.Errorf("get subnets failed, %+v", err)
		return err
	}
	localPodIPs, err := c.getLocalPodIPsNeedNAT()
	if err != nil {
		klog.Errorf("get local pod ips failed, %+v", err)
		return err
	}
	c.ipSetsMgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
		MaxSize: 1048576,
		SetID:   SubnetSet,
		Type:    ipsets.IPSetTypeHashNet,
	}, subnets)
	c.ipSetsMgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
		MaxSize: 1048576,
		SetID:   LocalPodSet,
		Type:    ipsets.IPSetTypeHashIP,
	}, localPodIPs)
	c.ipSetsMgr.ApplyUpdates()

	for _, iptRule := range []util.IPTableRule{forwardAcceptRule1, forwardAcceptRule2, natRule} {
		exists, err := c.iptablesMgr.Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
		if err != nil {
			klog.Errorf("check iptable rule exist failed, %+v", err)
			return err
		}
		if !exists {
			err := c.iptablesMgr.Insert(iptRule.Table, iptRule.Chain, 1, iptRule.Rule...)
			if err != nil {
				klog.Errorf("insert iptable rule exist failed, %+v", err)
				return err
			}
		}
	}

	ticker := time.NewTicker(3 * time.Second)
LOOP:
	for {
		select {
		case <-stopCh:
			klog.Info("exit gateway")
			break LOOP
		case <-ticker.C:
			klog.V(3).Info("tick")
		}
		subnets, err := c.getSubnets()
		if err != nil {
			klog.Errorf("get subnets failed, %+v", err)
			continue
		}
		localPodIPs, err := c.getLocalPodIPsNeedNAT()
		if err != nil {
			klog.Errorf("get local pod ips failed, %+v", err)
			continue
		}

		c.ipSetsMgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnets)
		c.ipSetsMgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   LocalPodSet,
			Type:    ipsets.IPSetTypeHashIP,
		}, localPodIPs)
		c.ipSetsMgr.ApplyUpdates()
	}
	return nil
}

func (c *Controller) getLocalPodIPsNeedNAT() ([]string, error) {
	var localPodIPs []string
	hostname, _ := os.Hostname()
	allPods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list pods failed, %+v", err)
		return nil, err
	}
	for _, pod := range allPods {
		if pod.Spec.HostNetwork == true || pod.Status.PodIP == "" {
			continue
		}
		ns, err := c.namespacesLister.Get(pod.Namespace)
		if err != nil {
			klog.Errorf("get ns %s failed, %+v", pod.Namespace, err)
			continue
		}
		nsGWType := ns.Annotations[util.GWTypeAnnotation]
		nsGWNat := ns.Annotations[util.GWNat]
		if nsGWNat == "" || nsGWNat == "true" {
			switch nsGWType {
			case "", util.GWDistributedMode:
				if pod.Spec.NodeName == hostname {
					localPodIPs = append(localPodIPs, pod.Status.PodIP)
				}
			case util.GWCentralizedMode:
				gwNode := ns.Annotations[util.GWNode]
				if gwNode == hostname {
					localPodIPs = append(localPodIPs, pod.Status.PodIP)
				}
			}
		}
	}
	klog.V(3).Infof("local pod ips %v", localPodIPs)
	return localPodIPs, nil
}

func (c *Controller) getSubnets() ([]string, error) {
	var subnets = []string{c.config.ServiceClusterIPRange}
	allNamespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list namespaces failed, %+v", err)
		return nil, err
	}
	for _, namespace := range allNamespaces {
		if subnet := namespace.Annotations[util.CidrAnnotation]; subnet != "" {
			subnets = append(subnets, subnet)
		}
	}
	klog.V(3).Infof("subnets %v", subnets)
	return subnets, nil
}
