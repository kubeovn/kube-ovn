//nolint:staticcheck
package speaker

import (
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	// announcePolicyCluster makes the Pod IPs/Subnet CIDRs be announced from every speaker, whether there's Pods
	// that have that specific IP or that are part of the Subnet CIDR on that node. In other words, traffic may enter from
	// any node hosting a speaker, and then be internally routed in the cluster to the actual Pod. In this configuration
	// extra hops might be used. This is the default policy to Pods and Subnets.
	announcePolicyCluster = "cluster"
	// announcePolicyLocal makes the Pod IPs be announced only from speakers on nodes that are actively hosting
	// them. In other words, traffic will only enter from nodes hosting Pods marked as needing BGP advertisement,
	// or Pods with an IP belonging to a Subnet marked as needing BGP advertisement. This makes the network path shorter.
	announcePolicyLocal = "local"
)

func (c *Controller) syncSubnetRoutes() {
	bgpExpected := make(prefixMap)

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets, %v", err)
		return
	}
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return
	}

	if c.config.AnnounceClusterIP {
		services, err := c.servicesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list services, %v", err)
			return
		}
		for _, svc := range services {
			if svc.Annotations != nil && svc.Annotations[util.BgpAnnotation] == "true" && isClusterIPService(svc) {
				for _, clusterIP := range svc.Spec.ClusterIPs {
					addExpectedPrefix(clusterIP, bgpExpected)
				}
			}
		}
	}

	localSubnets := make(map[string]string, 2)
	for _, subnet := range subnets {
		if subnet.Status.IsReady() && subnet.Annotations != nil {
			ips := strings.Split(subnet.Spec.CIDRBlock, ",")
			policy := subnet.Annotations[util.BgpAnnotation]
			if policy == "" {
				continue
			}

			switch policy {
			case "true":
				fallthrough
			case announcePolicyCluster:
				for _, cidr := range ips {
					ipFamily := util.CheckProtocol(cidr)
					bgpExpected[ipFamily] = append(bgpExpected[ipFamily], cidr)
				}
			case announcePolicyLocal:
				localSubnets[subnet.Name] = subnet.Spec.CIDRBlock
			default:
				klog.Warningf("invalid subnet annotation %s=%s", util.BgpAnnotation, policy)
			}
		}
	}

	for _, pod := range pods {
		if pod.Spec.HostNetwork || pod.Status.PodIP == "" || len(pod.Annotations) == 0 || !isPodAlive(pod) {
			continue
		}

		ips := make(map[string]string, 2)
		if policy := pod.Annotations[util.BgpAnnotation]; policy != "" {
			switch policy {
			case "true":
				fallthrough
			case announcePolicyCluster:
				for _, podIP := range pod.Status.PodIPs {
					ips[util.CheckProtocol(podIP.IP)] = podIP.IP
				}
			case announcePolicyLocal:
				if pod.Spec.NodeName == c.config.NodeName {
					for _, podIP := range pod.Status.PodIPs {
						ips[util.CheckProtocol(podIP.IP)] = podIP.IP
					}
				}
			default:
				klog.Warningf("invalid pod annotation %s=%s", util.BgpAnnotation, policy)
			}
		} else if pod.Spec.NodeName == c.config.NodeName {
			cidrBlock := localSubnets[pod.Annotations[util.LogicalSwitchAnnotation]]
			if cidrBlock != "" {
				for _, podIP := range pod.Status.PodIPs {
					if util.CIDRContainIP(cidrBlock, podIP.IP) {
						ips[util.CheckProtocol(podIP.IP)] = podIP.IP
					}
				}
			}
		}

		for _, ip := range ips {
			addExpectedPrefix(ip, bgpExpected)
		}
	}

	if err := c.reconcileRoutes(bgpExpected); err != nil {
		klog.Errorf("failed to reconcile routes: %s", err.Error())
	}
}
