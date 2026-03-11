//nolint:staticcheck
package speaker

import (
	"fmt"
	"net/netip"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

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

	subnetByName := make(map[string]*kubeovnv1.Subnet, len(subnets))
	for _, subnet := range subnets {
		if !subnet.Status.IsReady() || len(subnet.Annotations) == 0 {
			continue
		}

		subnetByName[subnet.Name] = subnet

		policy := subnet.Annotations[util.BgpAnnotation]
		switch policy {
		case "":
			continue
		case "true":
			fallthrough
		case announcePolicyCluster:
			for cidr := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
				prefix, err := netip.ParsePrefix(cidr)
				if err != nil {
					klog.Errorf("failed to parse subnet CIDR %q: %v", cidr, err)
					continue
				}

				if afi := prefixToAFI(prefix); bgpExpected[afi] == nil {
					bgpExpected[afi] = set.New(prefix.String())
				} else {
					bgpExpected[afi].Insert(prefix.String())
				}
			}
		default:
			if policy != announcePolicyLocal {
				klog.Warningf("invalid subnet annotation %s=%s", util.BgpAnnotation, policy)
			}
		}
	}

	collectPodExpectedPrefixes(pods, subnetByName, c.config.NodeName, bgpExpected)

	if err := c.reconcileRoutes(bgpExpected); err != nil {
		klog.Errorf("failed to reconcile routes: %s", err.Error())
	}
}

// collectPodExpectedPrefixes iterates over pods and collects IPs that should be announced via BGP.
// It reads IPs from pod annotations ({provider}.kubernetes.io/ip_address) instead of pod.Status.PodIPs,
// so that attachment network IPs and non-primary CNI IPs are correctly announced.
func collectPodExpectedPrefixes(pods []*corev1.Pod, subnetByName map[string]*kubeovnv1.Subnet, nodeName string, bgpExpected prefixMap) {
	ipAddrSuffix := fmt.Sprintf(util.IPAddressAnnotationTemplate, "")
	for _, pod := range pods {
		if len(pod.Annotations) == 0 || !isPodAlive(pod) {
			continue
		}

		podBgpPolicy := pod.Annotations[util.BgpAnnotation]

		for key, ipStr := range pod.Annotations {
			if ipStr == "" || !strings.HasSuffix(key, ipAddrSuffix) {
				continue
			}
			provider := strings.TrimSuffix(key, ipAddrSuffix)
			if provider == "" {
				continue
			}

			lsKey := fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)
			subnetName := pod.Annotations[lsKey]
			if subnetName == "" {
				continue
			}
			subnet := subnetByName[subnetName]
			if subnet == nil {
				continue
			}

			policy := podBgpPolicy
			if policy == "" {
				policy = subnet.Annotations[util.BgpAnnotation]
			}

			switch policy {
			case "true", announcePolicyCluster:
				for ip := range strings.SplitSeq(ipStr, ",") {
					addExpectedPrefix(strings.TrimSpace(ip), bgpExpected)
				}
			case announcePolicyLocal:
				if pod.Spec.NodeName == nodeName {
					for ip := range strings.SplitSeq(ipStr, ",") {
						addExpectedPrefix(strings.TrimSpace(ip), bgpExpected)
					}
				}
			}
		}
	}
}
