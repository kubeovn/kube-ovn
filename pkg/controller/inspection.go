package controller

import (
	"fmt"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
	"net"
	"strings"
)

func (c *Controller) inspectPod() error {
	klog.V(4).Infof("start inspection")
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ip, %v", err)
		return err
	}
	lsps, err := c.ovnClient.ListLogicalSwitchPort(c.config.EnableExternalVpc)
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}
	for _, pod := range pods {
		if pod.Spec.HostNetwork {
			continue
		}
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to list pod subnets, %v", err)
			return err
		}
		for _, podNet := range filterSubnets(pod, podNets) {
			if podNet.Type != providerTypeIPAM {
				portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, podNet.ProviderName)
				isLspExist := false
				for _, lsp := range lsps {
					if portName == lsp {
						isLspExist = true
					}
				}
				if !isLspExist {
					if err := c.ovnClient.DeleteLogicalSwitchPort(portName); err != nil {
						klog.Errorf("failed to delete lsp %s, %v", portName, err)
						return err
					}
					ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
					mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
					portSecurity := false
					if pod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)] == "true" {
						portSecurity = true
					}
					securityGroupAnnotation := pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
					vips := pod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
					for _, ip := range strings.Split(vips, ",") {
						if ip != "" && net.ParseIP(ip) == nil {
							klog.Errorf("invalid vip address '%s' for pod %s", ip, pod.Name)
							vips = ""
							break
						}
					}
					klog.Infof("start rebuild lsp %s with ip %s, mac %s", portName, ipStr, mac)
					if err := c.ovnClient.CreatePort(podNet.Subnet.Name, portName, ipStr, mac, pod.Name, pod.Namespace,
						portSecurity, securityGroupAnnotation, vips); err != nil {
						c.recorder.Eventf(pod, v1.EventTypeWarning, "CreateOVNPortFailed", err.Error())
						return err
					}
				}
			}
		}
	}
	return nil
}

func filterSubnets(pod *v1.Pod, nets []*kubeovnNet) []*kubeovnNet {

	if pod.Annotations == nil {
		return nets
	}
	result := make([]*kubeovnNet, 0, len(nets))
	for _, n := range nets {
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, n.ProviderName)] == "true" {
			result = append(result, n)
		}
	}
	return result
}
