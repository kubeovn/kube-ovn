package daemon

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Annotation suffixes derived from util templates ("%s.kubernetes.io/...").
// Used to discover all per-provider annotations by suffix matching.
var (
	sgSGAnnotationSuffix = strings.TrimPrefix(util.SecurityGroupAnnotationTemplate, "%s")
	sgIPAnnotationSuffix = strings.TrimPrefix(util.IPAddressAnnotationTemplate, "%s")
)

const (
	// sgCTSweepInterval is the pause between successive CT sweep iterations.
	sgCTSweepInterval = 500 * time.Millisecond
	// sgCTMinRunDuration is how long we keep sweeping even after finding 0 entries.
	// OVN propagation (NB→SB→ovn-controller→OVS) is async with no barrier API.
	// A clean sweep only means "no stale entries RIGHT NOW" — if OVN hasn't
	// programmed the deny rule yet, the very next packet will re-create one.
	// We keep sweeping for this duration to cover the propagation window.
	sgCTMinRunDuration = 5 * time.Second
	// sgCTMaxRunDuration is a hard cap to prevent infinite spinning.
	sgCTMaxRunDuration = 30 * time.Second
)

// podInfo holds parsed IPs and SG membership for one network attachment of a pod.
type podInfo struct {
	ips     []net.IP
	sgNames []string
}

// reconcileSGConntrack is called when a SecurityGroup's rules change.
// It loops, flushing conntrack entries for flows that are no longer permitted
// by the current SG rules, and exits once a full sweep finds nothing to flush.
//
// Loop rationale: OVN propagates ACL changes asynchronously (NB→SB→ovn-controller→OVS).
// There is no barrier API.  Entries flushed before OVS has the new deny flow get
// re-created by the next packet.  We keep sweeping until a clean pass confirms no
// stale entries remain — which only happens after OVN has programmed the deny rule
// (new packets are then dropped before CT commit, so no new stale entries appear).
func (c *Controller) reconcileSGConntrack(sgName string) error {
	sg, err := c.sgsLister.Get(sgName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(3).Infof("sg %s not found (deleted), skipping conntrack reconcile", sgName)
			return nil
		}
		return err
	}

	ctZones, err := ovs.GetCTZones()
	if err != nil {
		return fmt.Errorf("failed to get ct zones: %w", err)
	}
	klog.V(4).Infof("sg %s: CT zones: %v", sgName, ctZones)

	localPods, err := c.getLocalPodsWithSG(sgName)
	if err != nil {
		return fmt.Errorf("failed to list local pods for sg %s: %w", sgName, err)
	}
	if len(localPods) == 0 {
		klog.V(4).Infof("sg %s: no local pods found", sgName)
		return nil
	}
	klog.V(4).Infof("sg %s: found %d local pod(s)", sgName, len(localPods))

	podsByZone := buildPodsByZone(localPods, ctZones)
	if len(podsByZone) == 0 {
		klog.V(4).Infof("sg %s: no pods mapped to CT zones", sgName)
		return nil
	}

	start := time.Now()
	for iter := 1; ; iter++ {
		flushed := c.sweepStaleCTEntries(sgName, sg, podsByZone)

		elapsed := time.Since(start)
		if flushed == 0 && elapsed >= sgCTMinRunDuration {
			// Clean sweep after the minimum propagation window has elapsed.
			// OVN must have programmed the deny rules by now.
			klog.Infof("sg %s: CT reconcile complete after %d iteration(s) (%.1fs)", sgName, iter, elapsed.Seconds())
			return nil
		}
		if elapsed >= sgCTMaxRunDuration {
			klog.Warningf("sg %s: CT reconcile stopping after %.1fs (max duration), some stale entries may persist", sgName, elapsed.Seconds())
			return nil
		}

		if flushed > 0 {
			klog.Infof("sg %s: flushed %d stale CT entries in iteration %d (%.1fs elapsed), retrying in %s",
				sgName, flushed, iter, elapsed.Seconds(), sgCTSweepInterval)
		}
		time.Sleep(sgCTSweepInterval)
	}
}

func ovnPortName(pod *v1.Pod, provider string) string {
	if provider == "ovn" {
		return pod.Name + "." + pod.Namespace
	}
	return pod.Name + "." + pod.Namespace + "." + provider
}

// buildPodsByZone maps each CT zone ID to the list of pod network attachments
// in that zone. The CT zone key is the OVN port name (not the logical switch),
// and it scans ALL per-provider annotations so secondary networks work correctly.
func buildPodsByZone(pods []*v1.Pod, ctZones map[string]int) map[int][]podInfo {
	podsByZone := make(map[int][]podInfo)
	for _, pod := range pods {
		for k, v := range pod.Annotations {
			if !strings.HasSuffix(k, sgSGAnnotationSuffix) || v == "" {
				continue
			}
			provider := strings.TrimSuffix(k, sgSGAnnotationSuffix)
			portName := ovnPortName(pod, provider)
			zoneID, ok := ctZones[portName]
			if !ok {
				klog.V(4).Infof("pod %s/%s: no CT zone for port %q", pod.Namespace, pod.Name, portName)
				continue
			}
			ips := parseAnnotationIPs(pod.Annotations[provider+sgIPAnnotationSuffix])
			if len(ips) == 0 {
				continue
			}
			podsByZone[zoneID] = append(podsByZone[zoneID], podInfo{
				ips:     ips,
				sgNames: splitAnnotationValues(v),
			})
		}
	}
	return podsByZone
}

// buildIPToSGsCache builds a map from pod IP to SG names for all pods in the
// cluster. This is used to avoid listing all pods repeatedly for every CT entry.
func (c *Controller) buildIPToSGsCache() (map[string][]string, error) {
	allPods, err := c.podsLister.Pods(v1.NamespaceAll).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	cache := make(map[string][]string, len(allPods))
	for _, pod := range allPods {
		sgs := getPodAllSGNames(pod)
		if len(sgs) == 0 {
			continue
		}
		for _, ip := range getPodAllIPs(pod) {
			cache[ip.String()] = sgs
		}
	}
	return cache, nil
}

// sweepStaleCTEntries performs one dump+flush cycle across all affected CT zones.
// Returns the number of entries flushed.
func (c *Controller) sweepStaleCTEntries(sgName string, sg *kubeovnv1.SecurityGroup, podsByZone map[int][]podInfo) int {
	// Build once per sweep to avoid listing all pods for every CT entry.
	ipToSGsCache, err := c.buildIPToSGsCache()
	if err != nil {
		klog.Errorf("sg %s: failed to build IP→SGs cache: %v", sgName, err)
		return 0
	}

	allPodIPSet := make(map[string]*podInfo)
	for _, pods := range podsByZone {
		for i := range pods {
			for _, ip := range pods[i].ips {
				allPodIPSet[ip.String()] = &pods[i]
			}
		}
	}

	flushed := 0
	for zoneID, pods := range podsByZone {
		entries, err := ovs.DumpCTEntriesForZone(zoneID)
		if err != nil {
			klog.Errorf("sg %s: failed to dump CT entries for zone %d: %v", sgName, zoneID, err)
			continue
		}
		klog.V(4).Infof("sg %s: zone %d has %d CT entries", sgName, zoneID, len(entries))

		// zonePodIPSet: IPs of pods whose OVN port maps to this zone.
		zonePodIPSet := make(map[string]struct{}, len(pods))
		for i := range pods {
			for _, ip := range pods[i].ips {
				zonePodIPSet[ip.String()] = struct{}{}
			}
		}

		for _, entry := range entries {
			srcStr := entry.SrcIP.String()
			dstStr := entry.DstIP.String()

			// Only process entries where one side belongs to this zone's pod(s).
			_, zoneHasSrc := zonePodIPSet[srcStr]
			_, zoneHasDst := zonePodIPSet[dstStr]
			if !zoneHasSrc && !zoneHasDst {
				continue
			}

			// Use the global IP set to find podInfo for both sides.
			srcPi := allPodIPSet[srcStr]
			dstPi := allPodIPSet[dstStr]
			if srcPi == nil && dstPi == nil {
				continue
			}

			stale := false

			// Check from the destination pod's ingress perspective.
			if dstPi != nil {
				allowed, err := c.isEntryAllowedBySGs(entry, dstPi.sgNames, false, true, sg, ipToSGsCache)
				if err != nil {
					klog.V(4).Infof("sg %s: error evaluating CT entry (dst): %v", sgName, err)
				} else if !allowed {
					stale = true
				}
			}

			// Check from the source pod's egress perspective.
			if !stale && srcPi != nil {
				allowed, err := c.isEntryAllowedBySGs(entry, srcPi.sgNames, true, false, sg, ipToSGsCache)
				if err != nil {
					klog.V(4).Infof("sg %s: error evaluating CT entry (src): %v", sgName, err)
				} else if !allowed {
					stale = true
				}
			}

			if !stale {
				continue
			}

			if err := ovs.FlushCTEntry(entry); err != nil {
				klog.Warningf("sg %s: failed to flush CT entry: %v", sgName, err)
				continue
			}
			flushed++
		}
	}
	return flushed
}

// isEntryAllowedBySGs returns true if the CT entry is still permitted by any
// of the pod's SGs.  The changed SG's current (already updated) spec is passed
// directly to avoid a stale cache read.
func (c *Controller) isEntryAllowedBySGs(
	entry ovs.CTEntry,
	podSGNames []string,
	podIsSrc, podIsDst bool,
	changedSG *kubeovnv1.SecurityGroup,
	ipToSGsCache map[string][]string,
) (bool, error) {
	for _, sgName := range podSGNames {
		var sg *kubeovnv1.SecurityGroup
		if sgName == changedSG.Name {
			sg = changedSG
		} else {
			var err error
			sg, err = c.sgsLister.Get(sgName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				return false, err
			}
		}

		if sg.Spec.AllowSameGroupTraffic {
			remoteIP := entry.SrcIP
			if podIsSrc {
				remoteIP = entry.DstIP
			}
			if slices.Contains(ipToSGsCache[remoteIP.String()], sgName) {
				return true, nil
			}
		}

		if podIsDst {
			for _, rule := range sg.Spec.IngressRules {
				if rule.Policy != kubeovnv1.SgPolicyAllow {
					continue
				}
				if c.sgRuleMatchesEntry(rule, entry, entry.SrcIP, ipToSGsCache) {
					return true, nil
				}
			}
		}

		if podIsSrc {
			for _, rule := range sg.Spec.EgressRules {
				if rule.Policy != kubeovnv1.SgPolicyAllow {
					continue
				}
				if c.sgRuleMatchesEntry(rule, entry, entry.DstIP, ipToSGsCache) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// sgRuleMatchesEntry returns true if the rule covers the given CT entry.
// remoteIP is the IP of the peer (not the pod itself).
func (c *Controller) sgRuleMatchesEntry(rule kubeovnv1.SecurityGroupRule, entry ovs.CTEntry, remoteIP net.IP, ipToSGsCache map[string][]string) bool {
	isIPv6 := remoteIP.To4() == nil
	if isIPv6 && rule.IPVersion != "ipv6" {
		return false
	}
	if !isIPv6 && rule.IPVersion != "ipv4" {
		return false
	}

	if !sgProtoMatchesEntry(rule.Protocol, entry.Proto) {
		return false
	}

	if entry.DstPort != 0 && (rule.Protocol == kubeovnv1.SgProtocolTCP || rule.Protocol == kubeovnv1.SgProtocolUDP) {
		if rule.PortRangeMin != 0 || rule.PortRangeMax != 0 {
			if entry.DstPort < rule.PortRangeMin || entry.DstPort > rule.PortRangeMax {
				return false
			}
		}
	}

	switch rule.RemoteType {
	case kubeovnv1.SgRemoteTypeAddress:
		if rule.RemoteAddress == "" || rule.RemoteAddress == "0.0.0.0/0" || rule.RemoteAddress == "::/0" {
			return true
		}
		_, cidr, err := net.ParseCIDR(rule.RemoteAddress)
		if err != nil {
			ruleIP := net.ParseIP(rule.RemoteAddress)
			return ruleIP != nil && ruleIP.Equal(remoteIP)
		}
		return cidr.Contains(remoteIP)
	case kubeovnv1.SgRemoteTypeSg:
		return slices.Contains(ipToSGsCache[remoteIP.String()], rule.RemoteSecurityGroup)
	}
	return false
}

func sgProtoMatchesEntry(proto kubeovnv1.SgProtocol, protoNum int) bool {
	switch proto {
	case kubeovnv1.SgProtocolALL, "":
		return true
	case kubeovnv1.SgProtocolICMP:
		// SG "icmp" covers both ICMPv4 (proto 1) and ICMPv6 (proto 58).
		return protoNum == ovs.CTProtoNumber("icmp") || protoNum == ovs.CTProtoNumber("icmpv6")
	default:
		return ovs.CTProtoNumber(string(proto)) == protoNum
	}
}

// getLocalPodsWithSG returns all pods on this node that have sgName in any of
// their per-provider SG annotations.
func (c *Controller) getLocalPodsWithSG(sgName string) ([]*v1.Pod, error) {
	allPods, err := c.podsLister.Pods(v1.NamespaceAll).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	var result []*v1.Pod
	for _, pod := range allPods {
		if pod.Spec.NodeName != c.config.NodeName {
			continue
		}
		if slices.Contains(getPodAllSGNames(pod), sgName) {
			result = append(result, pod)
		}
	}
	return result, nil
}

// getPodAllIPs returns IPs from ALL per-provider ip_address annotations.
func getPodAllIPs(pod *v1.Pod) []net.IP {
	var ips []net.IP
	for k, v := range pod.Annotations {
		if strings.HasSuffix(k, sgIPAnnotationSuffix) {
			ips = append(ips, parseAnnotationIPs(v)...)
		}
	}
	return ips
}

// getPodAllSGNames returns SG names from ALL per-provider security_groups annotations.
func getPodAllSGNames(pod *v1.Pod) []string {
	seen := make(map[string]struct{})
	var result []string
	for k, v := range pod.Annotations {
		if strings.HasSuffix(k, sgSGAnnotationSuffix) {
			for _, sg := range splitAnnotationValues(v) {
				if _, ok := seen[sg]; !ok {
					seen[sg] = struct{}{}
					result = append(result, sg)
				}
			}
		}
	}
	return result
}

// parseAnnotationIPs parses a comma-separated IP string from a pod annotation.
func parseAnnotationIPs(raw string) []net.IP {
	var ips []net.IP
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if ip := net.ParseIP(part); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

// splitAnnotationValues splits a comma-separated annotation value.
func splitAnnotationValues(raw string) []string {
	var result []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (c *Controller) runSGCTFlushWorker() {
	for c.processNextSGCTFlushWorkItem() {
	}
}

func (c *Controller) processNextSGCTFlushWorkItem() bool {
	key, shutdown := c.sgCTFlushQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.sgCTFlushQueue.Done(key)
		if err := c.reconcileSGConntrack(key); err != nil {
			return fmt.Errorf("error reconciling conntrack for sg %q: %w, requeuing", key, err)
		}
		c.sgCTFlushQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		c.sgCTFlushQueue.AddRateLimited(key)
		return true
	}
	return true
}
