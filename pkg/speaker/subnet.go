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

// ipAddrAnnotationSuffix is the suffix of pod annotation keys that carry IP addresses
// (e.g. "ovn.kubernetes.io/ip_address"). Computed once at startup to avoid repeated
// fmt.Sprintf calls on the hot syncSubnetRoutes path.
var ipAddrAnnotationSuffix = fmt.Sprintf(util.IPAddressAnnotationTemplate, "")

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

	if c.config.EnableLbSvcAnnounce {
		// Service VIP plane: announce LoadBalancer ingress IPs for Services bound by
		// ovn.kubernetes.io/bgp-vip. This is independent from EIP resources:
		// - Service VIP plane (this block): Service.status.loadBalancer.ingress on nodes.
		// - EIP plane (nat-gw / node-route): IptablesEIP resources for NAT gateway traffic.
		//
		// Keep a dedicated set for Service VIP prefixes, then merge into the final expected
		// prefixes to preserve composability with other announcement sources.
		node := c.getLocalNode()
		if shouldAnnounceLbSvcIngressOnNode(node) {
			services, err := c.servicesLister.List(labels.Everything())
			if err != nil {
				klog.Errorf("failed to list services for bgp-lb-eip, %v", err)
				return
			}
			expectedBgpLbServiceEip := make(prefixMap)
			collectSvcBgpPrefixes(services, node, expectedBgpLbServiceEip)
			mergePrefixMap(expectedBgpLbServiceEip, bgpExpected)
		}
	}

	if err := c.reconcileRoutes(bgpExpected); err != nil {
		klog.Errorf("failed to reconcile routes: %s", err.Error())
	}
}

func mergePrefixMap(src, dst prefixMap) {
	for afi, prefixes := range src {
		if len(prefixes) == 0 {
			continue
		}
		if dst[afi] == nil {
			dst[afi] = set.New[string]()
		}
		for prefix := range prefixes {
			dst[afi].Insert(prefix)
		}
	}
}

func (c *Controller) getLocalNode() *corev1.Node {
	// The local node cache is only wired when Service VIP announcement is enabled.
	if c.config.NodeName == "" || c.nodesLister == nil {
		return nil
	}
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Warningf("failed to get local node %s for LB service announcement: %v", c.config.NodeName, err)
		return nil
	}
	return node
}

func shouldAnnounceLbSvcIngressOnNode(node *corev1.Node) bool {
	return node != nil && node.Labels[util.BgpSpeakLbVipLabel] == "true"
}

// collectPodExpectedPrefixes iterates over pods and collects IPs that should be announced via BGP.
// It reads IPs from pod annotations ({provider}.kubernetes.io/ip_address) instead of pod.Status.PodIPs,
// so that attachment network IPs and non-primary CNI IPs are correctly announced.
func collectPodExpectedPrefixes(pods []*corev1.Pod, subnetByName map[string]*kubeovnv1.Subnet, nodeName string, bgpExpected prefixMap) {
	for _, pod := range pods {
		if len(pod.Annotations) == 0 || !isPodAlive(pod) {
			continue
		}

		podBgpPolicy := pod.Annotations[util.BgpAnnotation]

		for key, ipStr := range pod.Annotations {
			if ipStr == "" || !strings.HasSuffix(key, ipAddrAnnotationSuffix) {
				continue
			}
			provider := strings.TrimSuffix(key, ipAddrAnnotationSuffix)
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

// collectSvcBgpPrefixes announces external IPs of LoadBalancer Services that carry
// the ovn.kubernetes.io/bgp annotation and have a non-empty status.loadBalancer.ingress.
// The current node must first pass the node-level gate
// ovn.kubernetes.io/bgp-speak-lb-vip=true before any Service-level policy is evaluated.
//
// Producer/consumer relationship with the controller:
//   - Controller side (--enable-bgp-lb-vip): binds a bgp_lb_vip VIP to the Service and
//     writes the VIP address into Service.status.loadBalancer.ingress.
//   - Speaker side (--enable-lb-svc-announce, this function): reads that ingress status
//     and announces the VIP prefix via BGP. The speaker never reads the Vip CR directly;
//     it only acts on the final Service state produced by the controller.
//
// Both flags must be enabled end-to-end for BGP LB VIP announcement to work:
//
//	controller --enable-bgp-lb-vip=true  →  Service.status.loadBalancer.ingress  →  speaker --enable-lb-svc-announce=true
//
// # BGP ANNOUNCEMENT MODEL
//
// The upstream router/switch only sees /32 prefixes and their BGP next-hops (node
// underlay IPs). It has zero visibility into pods, kube-proxy rules, or container
// ports — it simply forwards packets to whichever node announced the route.
//
// Announcement policy (two supported modes, evaluated in priority order):
//
// Test Mode — static single-node binding (ovn.kubernetes.io/bgp-speaker-node=<node>):
//
//	Only the named node announces the VIP; all other speaker nodes skip it.
//	The upstream router sees exactly ONE /32 route and forwards all traffic to
//	that node. kube-proxy/IPVS on that node then DNATs to the correct pod.
//
//	  Use case: testing, or upstream switches that do NOT support ECMP.
//	  NOT for production VM workloads: VMs are migratable, so pinning the
//	  announcement to a fixed node adds a cross-node hop when the VM moves.
//	  Failover is manual — update the annotation to redirect to another node.
//
// Default Mode — ECMP cluster mode (ovn.kubernetes.io/bgp=cluster / "true", default):
//
//	All speaker nodes announce the VIP simultaneously. The upstream router
//	performs equal-cost multipath (ECMP) across all nodes. kube-proxy/IPVS
//	on whichever node receives traffic routes to the correct pod.
//
//	  Use case: production IaaS / public-cloud LB. VMs are migratable —
//	  the VIP is never tied to a node, so VM migration is transparent.
//	  Requires the upstream switch/router to be configured for ECMP.
//
// bgp=local current limitation for LB VIPs:
//
//	In MetalLB, "local" means "announce only from nodes with a ready endpoint"
//	(ExternalTrafficPolicy: Local semantics, requires EndpointSlice awareness).
//
//	For VM/EIP workloads this degenerates to cluster mode because:
//	1. The bgp_lb_vip is a floating IP. kube-proxy/IPVS programs it on
//	   kube-ipvs0 on EVERY node, so every node is a "local endpoint".
//	   The local filter has no effect — all nodes still announce (ECMP).
//	2. On VM live migration the EIP follows the VM, but BGP paths do not
//	   automatically update to reflect the new node. The router keeps all
//	   N ECMP paths; the migrated VM is still reachable via kube-proxy DNAT
//	   but without a BGP path update tracking the migration.
//
//	TODO: add EndpointSlice-aware local announcement to automatically update
//	the BGP path on VM live migration. Use bgp=cluster for production until then.
func collectSvcBgpPrefixes(services []*corev1.Service, node *corev1.Node, bgpExpected prefixMap) {
	if !shouldAnnounceLbSvcIngressOnNode(node) {
		return
	}
	nodeName := node.Name

	for _, svc := range services {
		if len(svc.Annotations) == 0 {
			continue
		}
		// Announcement gate: Service must carry exactly one of the two VIP-binding annotations.
		// Both annotations carry the VIP CR name as their value; the controller resolves it
		// to an IP and writes status.loadBalancer.ingress. The speaker then announces that IP.
		// ovn.kubernetes.io/bgp-vip  — kube-ovn native path
		// metallb.universe.tf/allow-shared-ip — MetalLB compat (zero-annotation-change migration)
		hasMetalLBCompat := svc.Annotations[util.MetalLBAllowSharedIPAnnotation] != ""
		hasBgpVip := svc.Annotations[util.BgpVipAnnotation] != ""
		if !hasMetalLBCompat && !hasBgpVip {
			continue
		}
		// Only LoadBalancer Services participate in the BGP LB VIP flow;
		// annotations on other Service types are ignored.
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}

		// Test Mode: static single-node binding via bgp-speaker-node annotation.
		// Takes precedence over the bgp policy annotation.
		// For testing or non-ECMP upstreams only — not for production VM workloads.
		if pinnedNode := svc.Annotations[util.BgpSpeakerNodeAnnotation]; pinnedNode != "" {
			if pinnedNode != nodeName {
				klog.V(4).Infof("service %s/%s: bgp-speaker-node=%s (this node=%s), skipping",
					svc.Namespace, svc.Name, pinnedNode, nodeName)
				continue
			}
			// This node is the pinned speaker (Test Mode) — announce all ingress IPs.
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				if ingress.IP == "" {
					continue
				}
				klog.V(4).Infof("service %s/%s announcing VIP %s via BGP (bgp-speaker-node=%s)",
					svc.Namespace, svc.Name, ingress.IP, pinnedNode)
				addExpectedPrefix(ingress.IP, bgpExpected)
			}
			continue
		}

		// Default Mode (cluster/local) / unsupported: evaluate the bgp policy annotation.
		// bgp-vip and allow-shared-ip services need no explicit bgp= annotation;
		// absent policy defaults to cluster (ECMP), matching MetalLB semantics.
		policy := svc.Annotations[util.BgpAnnotation]
		if policy == "" {
			// Imply bgp=cluster (ECMP) when no explicit policy annotation.
			policy = announcePolicyCluster
		}
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP == "" {
				continue
			}
			switch policy {
			case "true", announcePolicyCluster:
				// Default Mode: ECMP — all speaker nodes announce the VIP simultaneously.
				// The upstream router performs ECMP across all nodes. This is the
				// recommended production mode for VM/IaaS workloads where VMs are
				// migratable and must not be tied to a specific node.
				// V(4): fires every 5s reconcile; actual BGP add/del logged in reconcileRoutes.
				klog.V(4).Infof("service %s/%s announcing VIP %s via BGP (policy=%s, ECMP)",
					svc.Namespace, svc.Name, ingress.IP, policy)
				addExpectedPrefix(ingress.IP, bgpExpected)
			case announcePolicyLocal:
				// bgp=local currently behaves identically to bgp=cluster for LB VIPs:
				// all speaker nodes announce the VIP (ECMP). This is because the
				// bgp_lb_vip is a floating IP — kube-proxy/IPVS programs it on
				// kube-ipvs0 on EVERY node, so there is no "node with a local endpoint"
				// to filter on.
				//
				// WARNING: in VM live-migration scenarios the EIP follows the VM to
				// the destination node, but all BGP speakers continue to announce the
				// VIP regardless. Traffic still reaches the VM correctly via ECMP +
				// kube-proxy DNAT, but the migration is NOT transparent at the BGP
				// layer: the router keeps N equal-cost paths instead of tracking the
				// VM's new location. If single-path post-migration routing is required,
				// update bgp-speaker-node to the destination node after migration.
				//
				// TODO: implement EndpointSlice-aware local announcement so that only
				// the node currently hosting the VM announces the VIP, enabling
				// automatic BGP path update on live migration without manual annotation
				// changes. Requires adding an EndpointSlice lister to the speaker
				// controller and wiring ExternalTrafficPolicy: Local semantics.
				// V(2): suppressed at default verbosity to avoid 5s-reconcile log spam.
				// Use -v=2 to surface this when debugging bgp=local services.
				klog.V(2).Infof("WARNING service %s/%s: bgp=local for LoadBalancer VIPs "+
					"currently announces on all nodes (ECMP); VM live-migration will NOT "+
					"automatically update the BGP path to the destination node — "+
					"see TODO in collectSvcBgpPrefixes",
					svc.Namespace, svc.Name)
				addExpectedPrefix(ingress.IP, bgpExpected)
			default:
				klog.Warningf("service %s/%s: invalid bgp annotation value %q",
					svc.Namespace, svc.Name, policy)
			}
		}
	}
}
