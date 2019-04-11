package daemon

import (
	"os"
	"strings"
	"time"

	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
	"github.com/projectcalico/felix/ipsets"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

const (
	SubnetSet   = "subnets"
	LocalPodSet = "local-pod-ip-nat"
	IPSetPrefix = "ovn"
	NATRule     = "-m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE"
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

	exist, err := c.iptablesMgr.Exists("nat", "POSTROUTING", strings.Split(NATRule, " ")...)
	if err != nil {
		klog.Errorf("check iptable rule failed, %+v", err)
		return err
	}
	if !exist {
		err = c.iptablesMgr.AppendUnique("nat", "POSTROUTING", strings.Split(NATRule, " ")...)
		if err != nil {
			klog.Errorf("append iptable rule failed, %+v", err)
			return err
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
			klog.V(5).Info("tick")
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
		if pod.Spec.NodeName == hostname && pod.Spec.HostNetwork != true && pod.Status.PodIP != "" {
			ns, err := c.namespacesLister.Get(pod.Namespace)
			if err != nil {
				klog.Errorf("get ns %s failed, %+v", pod.Namespace, err)
				continue
			}
			nsGWType := ns.Annotations[util.GWTypeAnnotation]
			switch nsGWType {
			case "", util.GWDistributedMode:
				localPodIPs = append(localPodIPs, pod.Status.PodIP)
			case util.GWCentralizedMode:
				// TODO:
			}
		}
	}
	klog.V(5).Infof("local pod ips %v", localPodIPs)
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
	klog.V(5).Infof("subnets %v", subnets)
	return subnets, nil
}
