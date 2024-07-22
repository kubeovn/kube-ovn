//nolint:staticcheck
package speaker

import (
	"context"
	"fmt"
	"strings"

	bgpapi "github.com/osrg/gobgp/v3/api"
	bgpapiutil "github.com/osrg/gobgp/v3/pkg/apiutil"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
	maskMap := map[string]int{kubeovnv1.ProtocolIPv4: 32, kubeovnv1.ProtocolIPv6: 128}
	bgpExpected, bgpExists := make(map[string][]string), make(map[string][]string)

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
					ipFamily := util.CheckProtocol(clusterIP)
					bgpExpected[ipFamily] = append(bgpExpected[ipFamily], fmt.Sprintf("%s/%d", clusterIP, maskMap[ipFamily]))
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

		for ipFamily, ip := range ips {
			bgpExpected[ipFamily] = append(bgpExpected[ipFamily], fmt.Sprintf("%s/%d", ip, maskMap[ipFamily]))
		}
	}

	klog.V(5).Infof("expected announce ipv4 routes: %v, ipv6 routes: %v", bgpExpected[kubeovnv1.ProtocolIPv4], bgpExpected[kubeovnv1.ProtocolIPv6])

	fn := func(d *bgpapi.Destination) {
		for _, path := range d.Paths {
			attrInterfaces, _ := bgpapiutil.UnmarshalPathAttributes(path.Pattrs)
			nextHop := getNextHopFromPathAttributes(attrInterfaces)
			klog.V(5).Infof("the route Prefix is %s, NextHop is %s", d.Prefix, nextHop.String())
			ipFamily := util.CheckProtocol(nextHop.String())
			route, _ := netlink.RouteGet(nextHop)
			if len(route) == 1 && route[0].Type == unix.RTN_LOCAL || nextHop.String() == c.config.RouterID {
				bgpExists[ipFamily] = append(bgpExists[ipFamily], d.Prefix)
				return
			}
		}
	}

	if len(c.config.NeighborAddresses) != 0 {
		listPathRequest := &bgpapi.ListPathRequest{
			TableType: bgpapi.TableType_GLOBAL,
			Family:    &bgpapi.Family{Afi: bgpapi.Family_AFI_IP, Safi: bgpapi.Family_SAFI_UNICAST},
		}
		if err := c.config.BgpServer.ListPath(context.Background(), listPathRequest, fn); err != nil {
			klog.Errorf("failed to list exist route, %v", err)
			return
		}

		klog.V(5).Infof("exists ipv4 routes %v", bgpExists[kubeovnv1.ProtocolIPv4])
		toAdd, toDel := routeDiff(bgpExpected[kubeovnv1.ProtocolIPv4], bgpExists[kubeovnv1.ProtocolIPv4])
		klog.V(5).Infof("toAdd ipv4 routes %v", toAdd)
		for _, route := range toAdd {
			if err := c.addRoute(route); err != nil {
				klog.Error(err)
			}
		}

		klog.V(5).Infof("toDel ipv4 routes %v", toDel)
		for _, route := range toDel {
			if err := c.delRoute(route); err != nil {
				klog.Error(err)
			}
		}
	}

	if len(c.config.NeighborIPv6Addresses) != 0 {
		listIPv6PathRequest := &bgpapi.ListPathRequest{
			TableType: bgpapi.TableType_GLOBAL,
			Family:    &bgpapi.Family{Afi: bgpapi.Family_AFI_IP6, Safi: bgpapi.Family_SAFI_UNICAST},
		}

		if err := c.config.BgpServer.ListPath(context.Background(), listIPv6PathRequest, fn); err != nil {
			klog.Errorf("failed to list exist route, %v", err)
			return
		}

		klog.V(5).Infof("exists ipv6 routes %v", bgpExists[kubeovnv1.ProtocolIPv6])
		toAdd, toDel := routeDiff(bgpExpected[kubeovnv1.ProtocolIPv6], bgpExists[kubeovnv1.ProtocolIPv6])
		klog.V(5).Infof("toAdd ipv6 routes %v", toAdd)

		for _, route := range toAdd {
			if err := c.addRoute(route); err != nil {
				klog.Error(err)
			}
		}
		klog.V(5).Infof("toDel ipv6 routes %v", toDel)
		for _, route := range toDel {
			if err := c.delRoute(route); err != nil {
				klog.Error(err)
			}
		}
	}
}
