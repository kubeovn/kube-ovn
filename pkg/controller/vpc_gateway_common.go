package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnv1lister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	// Default resource requirements for gateway containers
	gwSleepResourceCPU         = resource.MustParse("10m")
	gwSleepResourceMemory      = resource.MustParse("10Mi")
	gwBFDDResourceCPU          = resource.MustParse("50m")
	gwBFDDResourceMemory       = resource.MustParse("50Mi")
	gwResourceEphemeralStorage = resource.MustParse("1Gi")
)

// genGatewayBFDDContainer creates a BFD daemon container for VPC gateways (both Egress and NAT).
// The container runs OpenBFDD to establish BFD sessions with the VPC's BFD port for health monitoring.
//
// Parameters:
//   - image: Container image to use
//   - bfdIP: IP address(es) of the BFD peer (VPC BFD port), comma-separated for dual-stack
//   - minTX: BFD minimum transmit interval in milliseconds
//   - minRX: BFD minimum receive interval in milliseconds
//   - multiplier: BFD detection multiplier
//
// Returns a container specification ready to be added to a pod template.
func genGatewayBFDDContainer(image, bfdIP string, minTX, minRX, multiplier int32) corev1.Container {
	return corev1.Container{
		Name:            "bfdd",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"bash", "/kube-ovn/start-bfdd.sh"},
		Env: []corev1.EnvVar{
			{
				Name: "POD_IPS",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIPs",
					},
				},
			},
			{
				Name:  "BFD_PEER_IPS",
				Value: bfdIP,
			},
			{
				Name:  "BFD_MIN_TX",
				Value: strconv.Itoa(int(minTX)),
			},
			{
				Name:  "BFD_MIN_RX",
				Value: strconv.Itoa(int(minRX)),
			},
			{
				Name:  "BFD_MULTI",
				Value: strconv.Itoa(int(multiplier)),
			},
		},
		// Wait for the BFD process to be running and initialize the BFD configuration
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bash", "/kube-ovn/bfdd-prestart.sh"},
				},
			},
			InitialDelaySeconds: 1,
			FailureThreshold:    1,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bfdd-control", "status"},
				},
			},
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bfdd-control", "status"},
				},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       3,
			FailureThreshold:    1,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    gwBFDDResourceCPU,
				corev1.ResourceMemory: gwBFDDResourceMemory,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              gwBFDDResourceCPU,
				corev1.ResourceMemory:           gwBFDDResourceMemory,
				corev1.ResourceEphemeralStorage: gwResourceEphemeralStorage,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: new(false),
			RunAsUser:  ptr.To[int64](65534),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "usr-local-sbin",
				MountPath: "/usr/local/sbin",
			},
		},
	}
}

// genGatewaySleepContainer creates a minimal sleep container for gateways.
// This container runs indefinitely and is used as the main container when the gateway
// only needs to run BFD or other sidecar containers.
func genGatewaySleepContainer(image string) corev1.Container {
	return corev1.Container{
		Name:            "gateway",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sleep", "infinity"},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    gwSleepResourceCPU,
				corev1.ResourceMemory: gwSleepResourceMemory,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              gwSleepResourceCPU,
				corev1.ResourceMemory:           gwSleepResourceMemory,
				corev1.ResourceEphemeralStorage: gwResourceEphemeralStorage,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: new(false),
			RunAsUser:  ptr.To[int64](65534),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "usr-local-sbin",
				MountPath: "/usr/local/sbin",
			},
		},
	}
}

// genGatewayPodAntiAffinity creates pod anti-affinity rules to ensure gateway instances
// run on different nodes. This is essential for HA deployments.
func genGatewayPodAntiAffinity(labels map[string]string) *corev1.Affinity {
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				TopologyKey: corev1.LabelHostname,
			}},
		},
	}
}

// genGatewayDeploymentStrategy creates the standard rolling update strategy for gateway deployments.
// MaxUnavailable=1 ensures only one instance is updated at a time.
// MaxSurge=0 ensures no extra instances are created during updates.
func genGatewayDeploymentStrategy() appsv1.DeploymentStrategy {
	return appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: new(intstr.FromInt(1)),
			MaxSurge:       new(intstr.FromInt(0)),
		},
	}
}

// mergeGatewayAffinity merges multiple affinity configurations into one.
// Later affinities take precedence over earlier ones.
func mergeGatewayAffinity(affinities ...*corev1.Affinity) *corev1.Affinity {
	result := &corev1.Affinity{}
	for _, aff := range affinities {
		if aff == nil {
			continue
		}
		if aff.NodeAffinity != nil {
			result.NodeAffinity = aff.NodeAffinity
		}
		if aff.PodAffinity != nil {
			result.PodAffinity = aff.PodAffinity
		}
		if aff.PodAntiAffinity != nil {
			result.PodAntiAffinity = aff.PodAntiAffinity
		}
	}
	return result
}

// reconcileGatewayBFD reconciles OVN BFD entries for a gateway.
// It creates BFD sessions for new nexthops, identifies stale sessions for cleanup,
// and returns the mapping of nexthop IPs to BFD UUIDs.
//
// Parameters:
//   - ovnClient: OVN northbound client for BFD operations
//   - bfdIP: BFD port IP (empty string disables BFD)
//   - lrpName: Logical router port name for BFD sessions
//   - nextHops: Map of node names to nexthop IPs
//   - minTX, minRX, multiplier: BFD timing parameters
//   - externalIDs: External IDs for tagging BFD sessions
//
// Returns:
//   - bfdIDs: Set of active BFD UUIDs
//   - bfdMap: Map of nexthop IP to BFD UUID
//   - staleBFDIDs: Set of stale BFD UUIDs to delete
func reconcileGatewayBFD(
	ovnClient ovnNbClient,
	bfdIP string,
	lrpName string,
	nextHops map[string]string,
	minTX, minRX, multiplier int32,
	externalIDs map[string]string,
) (bfdIDs set.Set[string], bfdMap map[string]string, staleBFDIDs set.Set[string], err error) {
	bfdList, err := ovnClient.FindBFD(externalIDs)
	if err != nil {
		klog.Error(err)
		return nil, nil, nil, err
	}

	bfdIDs = set.New[string]()
	staleBFDIDs = set.New[string]()
	bfdDstIPs := set.New(slices.Collect(maps.Values(nextHops))...)
	bfdMap = make(map[string]string, bfdDstIPs.Len())

	// Process existing BFD sessions
	for i := range bfdList {
		bfd := &bfdList[i]
		if bfdIP == "" || bfd.LogicalPort != lrpName || !bfdDstIPs.Has(bfd.DstIP) {
			// Mark stale: either BFD disabled, wrong port, or nexthop no longer exists
			staleBFDIDs.Insert(bfd.UUID)
		}
		if bfdIP == "" || (bfd.LogicalPort == lrpName && bfdDstIPs.Has(bfd.DstIP)) {
			// TODO: update min_rx, min_tx and multiplier if changed
			if bfdIP != "" {
				bfdIDs.Insert(bfd.UUID)
				bfdMap[bfd.DstIP] = bfd.UUID
			}
			bfdDstIPs.Delete(bfd.DstIP)
		}
	}

	// Create BFD sessions for new nexthops
	if bfdIP != "" {
		for _, dstIP := range bfdDstIPs.UnsortedList() {
			// Note: minRX/minTX values are swapped intentionally - the BFD daemon in the pod
			// uses opposite values (what we TX, the daemon RX, and vice versa)
			bfd, err := ovnClient.CreateBFD(lrpName, dstIP, int(minTX), int(minRX), int(multiplier), externalIDs)
			if err != nil {
				klog.Error(err)
				return nil, nil, nil, err
			}
			klog.V(3).Infof("created BFD session for nexthop %s: UUID %s", dstIP, bfd.UUID)
			bfdIDs.Insert(bfd.UUID)
			bfdMap[bfd.DstIP] = bfd.UUID
		}
	}

	return bfdIDs, bfdMap, staleBFDIDs, nil
}

// cleanupStaleBFD deletes stale BFD sessions that are no longer needed.
func cleanupStaleBFD(ovnClient ovnNbClient, staleBFDIDs set.Set[string]) error {
	for _, bfdID := range staleBFDIDs.UnsortedList() {
		if err := ovnClient.DeleteBFD(bfdID); err != nil {
			klog.Errorf("failed to delete bfd %s: %v", bfdID, err)
			return err
		}
		klog.V(3).Infof("deleted stale BFD session %s", bfdID)
	}
	return nil
}

// reconcileGatewayBFDWithCleanup reconciles OVN BFD sessions for a gateway and cleans up stale sessions.
// This is a convenience wrapper around reconcileGatewayBFD that also handles cleanup.
//
// Parameters:
//   - ovnClient: OVN northbound client for BFD operations
//   - bfdIP: BFD port IP (empty string disables BFD)
//   - lrpName: Logical router port name for BFD sessions
//   - nextHops: Map of node names to nexthop IPs
//   - minTX, minRX, multiplier: BFD timing parameters
//   - externalIDs: External IDs for tagging BFD sessions (should include gateway-specific identifiers)
//
// Returns:
//   - bfdIDs: Set of active BFD UUIDs
//   - error: Any error encountered during reconciliation or cleanup
func reconcileGatewayBFDWithCleanup(
	ovnClient ovnNbClient,
	bfdIP string,
	lrpName string,
	nextHops map[string]string,
	minTX, minRX, multiplier int32,
	externalIDs map[string]string,
) (set.Set[string], error) {
	if len(nextHops) == 0 || bfdIP == "" {
		return nil, nil
	}

	// Reconcile OVN BFD entries
	bfdIDs, _, staleBFDIDs, err := reconcileGatewayBFD(
		ovnClient,
		bfdIP,
		lrpName,
		nextHops,
		minTX,
		minRX,
		multiplier,
		externalIDs,
	)
	if err != nil {
		return nil, err
	}

	// Cleanup stale BFD sessions
	if err = cleanupStaleBFD(ovnClient, staleBFDIDs); err != nil {
		return nil, err
	}

	return bfdIDs, nil
}

// getWorkloadNodes returns the list of nodes where the workload's pods are currently running.
func getWorkloadNodes(podLister v1.PodLister, namespace string, selector *metav1.LabelSelector) ([]string, error) {
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		klog.Errorf("failed to create label selector: %v", err)
		return nil, err
	}

	pods, err := podLister.Pods(namespace).List(s)
	if err != nil {
		klog.Errorf("failed to list pods for namespace %s: %v", namespace, err)
		return nil, err
	}

	nodes := make([]string, 0, len(pods))
	for _, pod := range pods {
		if pod.Spec.NodeName != "" {
			nodes = append(nodes, pod.Spec.NodeName)
		}
	}
	return nodes, nil
}

// isNatGwHAMode returns true if the NAT gateway should use HA mode (Deployment with replicas > 1).
func isNatGwHAMode(gw *kubeovnv1.VpcNatGateway) bool {
	return gw.Spec.Replicas > 1
}

// updateNatGwWorkloadStatus updates the workload information in the VPC NAT Gateway status.
func updateNatGwWorkloadStatus(
	gw *kubeovnv1.VpcNatGateway,
	podLister v1.PodLister,
	deployLister appsv1listers.DeploymentLister,
	kubeClient kubernetes.Interface,
	natGwNamespace string,
) bool {
	workloadName := util.GenNatGwName(gw.Name)
	var workloadKind, workloadAPIVersion string
	var workloadNodes []string
	var err error

	if isNatGwHAMode(gw) {
		workloadKind = util.KindDeployment
		workloadAPIVersion = "apps/v1"
		var deploy *appsv1.Deployment
		if deployLister != nil {
			deploy, err = deployLister.Deployments(natGwNamespace).Get(workloadName)
		}
		if err == nil && deploy != nil {
			workloadNodes, err = getWorkloadNodes(podLister, natGwNamespace, deploy.Spec.Selector)
		}
	} else {
		workloadKind = util.KindStatefulSet
		workloadAPIVersion = "apps/v1"
		var sts *appsv1.StatefulSet
		if kubeClient != nil {
			sts, err = kubeClient.AppsV1().StatefulSets(natGwNamespace).Get(context.Background(), workloadName, metav1.GetOptions{})
		}
		if err == nil && sts != nil {
			workloadNodes, err = getWorkloadNodes(podLister, natGwNamespace, sts.Spec.Selector)
		}
	}

	if err != nil {
		klog.Errorf("failed to get workload nodes for %s %s/%s: %v", workloadKind, natGwNamespace, workloadName, err)
	}

	var changed bool
	if gw.Status.Workload.Kind != workloadKind {
		gw.Status.Workload.Kind = workloadKind
		changed = true
	}
	if gw.Status.Workload.APIVersion != workloadAPIVersion {
		gw.Status.Workload.APIVersion = workloadAPIVersion
		changed = true
	}
	if gw.Status.Workload.Name != workloadName {
		gw.Status.Workload.Name = workloadName
		changed = true
	}
	slices.Sort(workloadNodes)
	if !slices.Equal(gw.Status.Workload.Nodes, workloadNodes) {
		gw.Status.Workload.Nodes = workloadNodes
		changed = true
	}

	return changed
}

// resolveInternalCIDRs resolves internal CIDRs from subnet names and direct CIDRs.
func resolveInternalCIDRs(subnetLister kubeovnv1lister.SubnetLister, subnetNames, directCIDRs []string) []string {
	internalCIDRs := make([]string, 0, len(subnetNames)+len(directCIDRs))
	for _, subnetName := range subnetNames {
		subnet, err := subnetLister.Get(subnetName)
		if err != nil {
			klog.Warningf("failed to get subnet %s: %v", subnetName, err)
			continue
		}
		if subnet.Spec.CIDRBlock != "" {
			v4CIDR, v6CIDR := util.SplitStringIP(subnet.Spec.CIDRBlock)
			if v4CIDR != "" {
				internalCIDRs = append(internalCIDRs, v4CIDR)
			}
			if v6CIDR != "" {
				internalCIDRs = append(internalCIDRs, v6CIDR)
			}
		}
	}
	internalCIDRs = append(internalCIDRs, directCIDRs...)
	return internalCIDRs
}

// reconcileGatewayRoutes reconciles OVN static routes for a gateway.
func reconcileGatewayRoutes(
	ovnClient ovnNbClient,
	gwName string,
	lrName string,
	bfdEnabled bool,
	bfdIP string,
	bfdIDs set.Set[string],
	bfdMap map[string]string,
	internalCIDRs []string,
	nextHops map[string]string,
	externalIDs map[string]string,
) error {
	if len(internalCIDRs) == 0 {
		// No internal CIDRs configured, delete any existing routes
		if err := ovnClient.DeleteLogicalRouterStaticRouteByExternalIDs(lrName, externalIDs); err != nil {
			klog.Errorf("failed to delete static routes for gateway %s: %v", gwName, err)
			return err
		}
		return nil
	}

	// Group nexthops by address family
	v4NextHops := make([]string, 0, len(nextHops))
	v6NextHops := make([]string, 0, len(nextHops))
	for _, nexthop := range nextHops {
		if util.CheckProtocol(nexthop) == kubeovnv1.ProtocolIPv4 {
			v4NextHops = append(v4NextHops, nexthop)
		} else {
			v6NextHops = append(v6NextHops, nexthop)
		}
	}

	// Create/update static routes for each internal CIDR
	for _, internalCIDR := range internalCIDRs {
		routeNextHops := v4NextHops
		if util.CheckProtocol(internalCIDR) == kubeovnv1.ProtocolIPv6 {
			routeNextHops = v6NextHops
		}

		if len(routeNextHops) == 0 {
			klog.Warningf("no nexthops available for internal CIDR %s in gateway %s", internalCIDR, gwName)
			continue
		}

		// Use source-based routing policy
		policy := ovnnb.LogicalRouterStaticRoutePolicySrcIP
		routeTable := ""

		// Build external IDs for this specific route
		routeExternalIDs := maps.Clone(externalIDs)
		routeExternalIDs["internal-cidr"] = internalCIDR

		// Get BFD ID for the first nexthop (for ECMP routes with BFD)
		var bfdIDPtr *string
		if bfdEnabled && bfdIP != "" && len(bfdIDs) > 0 {
			if bfdID, exists := bfdMap[routeNextHops[0]]; exists {
				bfdIDPtr = &bfdID
			}
		}

		if err := ovnClient.AddLogicalRouterStaticRoute(
			lrName,
			routeTable,
			policy,
			internalCIDR,
			bfdIDPtr,
			routeExternalIDs,
			routeNextHops...,
		); err != nil {
			klog.Errorf("failed to add static route for internal CIDR %s in gateway %s: %v", internalCIDR, gwName, err)
			return err
		}
	}

	// Cleanup stale routes
	existingRoutes, err := ovnClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", externalIDs)
	if err != nil {
		klog.Errorf("failed to list static routes for gateway %s: %v", gwName, err)
		return err
	}

	internalCIDRSet := set.New(internalCIDRs...)
	for _, route := range existingRoutes {
		if !internalCIDRSet.Has(route.IPPrefix) {
			var policy *string
			if route.Policy != nil {
				p := *route.Policy
				policy = &p
			}
			if err := ovnClient.DeleteLogicalRouterStaticRoute(lrName, &route.RouteTable, policy, route.IPPrefix, route.Nexthop); err != nil {
				klog.Errorf("failed to delete stale static route %s for gateway %s: %v", route.IPPrefix, gwName, err)
				return err
			}
		}
	}

	return nil
}

// reconcileNatGatewayPolicies reconciles OVN logical router policies for NAT gateway routing.
// Uses policy-based routing with BFD sessions for automatic failover.
//
// Parameters:
//   - ovnClient: OVN northbound client for policy operations
//   - gwName: NAT gateway name (for logging)
//   - lrName: Logical router name
//   - af: Address family (4 for IPv4, 6 for IPv6)
//   - bfdEnabled: Whether BFD is enabled
//   - bfdIDs: Set of active BFD UUIDs
//   - internalCIDRs: List of internal CIDRs to route to the gateway
//   - nextHops: Map of node names to nexthop IPs
//   - externalIDs: External IDs for tagging policies
//
// Returns error if any OVN operation fails.
func reconcileNatGatewayPolicies(
	ovnClient ovnNbClient,
	gwName string,
	lrName string,
	af int,
	bfdEnabled bool,
	bfdIDs set.Set[string],
	internalCIDRs []string,
	nextHops map[string]string,
	externalIDs map[string]string,
) error {
	if len(internalCIDRs) == 0 || len(nextHops) == 0 {
		// No internal CIDRs/nexthops configured, delete any existing policies
		if err := ovnClient.DeleteLogicalRouterPolicies(lrName, util.NatGatewayPolicyPriority, externalIDs); err != nil {
			klog.Errorf("failed to delete policies for NAT gateway %s: %v", gwName, err)
			return err
		}
		klog.V(3).Infof("deleted policies for NAT gateway %s", gwName)
		if err := ovnClient.DeleteLogicalRouterPolicies(lrName, util.NatGatewayDropPolicyPriority, externalIDs); err != nil {
			klog.Errorf("failed to delete drop policies for NAT gateway %s: %v", gwName, err)
			return err
		}
		klog.V(3).Infof("deleted drop policies for NAT gateway %s", gwName)
		return nil
	}

	// Get existing main policies
	policies, err := ovnClient.ListLogicalRouterPolicies(lrName, util.NatGatewayPolicyPriority, externalIDs, false)
	if err != nil {
		klog.Error(err)
		return err
	}

	// Build match expressions for each internal CIDR
	matches := set.New[string]()
	for _, cidr := range internalCIDRs {
		matches.Insert(fmt.Sprintf("ip%d.src == %s", af, cidr))
	}

	bfdIPs := set.New(slices.Collect(maps.Values(nextHops))...)
	bfdSessions := bfdIDs.UnsortedList()

	// Update existing policies or mark for deletion
	for _, policy := range policies {
		if matches.Has(policy.Match) {
			// Policy exists, check if nexthops or BFD sessions need updating
			if !bfdIPs.Equal(set.New(policy.Nexthops...)) || !bfdIDs.Equal(set.New(policy.BFDSessions...)) {
				policy.Nexthops, policy.BFDSessions = bfdIPs.UnsortedList(), bfdSessions
				if err = ovnClient.UpdateLogicalRouterPolicy(policy, &policy.Nexthops, &policy.BFDSessions); err != nil {
					err = fmt.Errorf("failed to update bfd sessions of logical router policy %s: %w", policy.UUID, err)
					klog.Error(err)
					return err
				}
				klog.V(3).Infof("updated policy for NAT gateway %s: match %s, nexthops %v, bfdSessions %v", gwName, policy.Match, policy.Nexthops, policy.BFDSessions)
			}
			matches.Delete(policy.Match)
			continue
		}
		// Stale policy, delete it
		if err = ovnClient.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
			err = fmt.Errorf("failed to delete ovn lr policy %q: %w", policy.Match, err)
			klog.Error(err)
			return err
		}
		klog.V(3).Infof("deleted policy for NAT gateway %s: match %s", gwName, policy.Match)
	}

	// Create missing policies
	for _, match := range matches.UnsortedList() {
		if err = ovnClient.AddLogicalRouterPolicy(lrName, util.NatGatewayPolicyPriority, match,
			ovnnb.LogicalRouterPolicyActionReroute, bfdIPs.UnsortedList(), bfdSessions, externalIDs); err != nil {
			klog.Error(err)
			return err
		}
		klog.V(3).Infof("added policy for NAT gateway %s: match %s, nexthops %v, bfdSessions %v", gwName, match, bfdIPs.UnsortedList(), bfdSessions)
	}

	// Handle drop policies (only when BFD is enabled)
	if bfdEnabled {
		// drop traffic if no nexthop is available
		if policies, err = ovnClient.ListLogicalRouterPolicies(lrName, util.NatGatewayDropPolicyPriority, externalIDs, false); err != nil {
			klog.Error(err)
			return err
		}
		matches = set.New[string]()
		for _, cidr := range internalCIDRs {
			matches.Insert(fmt.Sprintf("ip%d.src == %s", af, cidr))
		}
		for _, policy := range policies {
			if matches.Has(policy.Match) {
				matches.Delete(policy.Match)
				continue
			}
			if err = ovnClient.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
				err = fmt.Errorf("failed to delete ovn lr policy %q: %w", policy.Match, err)
				klog.Error(err)
				return err
			}
			klog.V(3).Infof("deleted drop policy for NAT gateway %s: match %s", gwName, policy.Match)
		}
		for _, match := range matches.UnsortedList() {
			if err = ovnClient.AddLogicalRouterPolicy(lrName, util.NatGatewayDropPolicyPriority, match,
				ovnnb.LogicalRouterPolicyActionDrop, nil, nil, externalIDs); err != nil {
				klog.Error(err)
				return err
			}
			klog.V(3).Infof("added drop policy for NAT gateway %s: match %s", gwName, match)
		}
	} else if err = ovnClient.DeleteLogicalRouterPolicies(lrName, util.NatGatewayDropPolicyPriority, externalIDs); err != nil {
		klog.Error(err)
		return err
	} else {
		klog.V(3).Infof("deleted drop policies for NAT gateway %s (BFD disabled)", gwName)
	}

	return nil
}

// ovnNbClient defines the interface for OVN northbound operations needed by gateway BFD/routing.
// This interface allows for easier testing and abstraction.
type ovnNbClient interface {
	FindBFD(externalIDs map[string]string) ([]ovnnb.BFD, error)
	CreateBFD(lrp, dstIP string, minRX, minTX, detectMult int, externalIDs map[string]string) (*ovnnb.BFD, error)
	DeleteBFD(uuid string) error
	ListLogicalRouterPolicies(lr string, priority int, externalIDs map[string]string, ignoreNotFound bool) ([]*ovnnb.LogicalRouterPolicy, error)
	AddLogicalRouterPolicy(lr string, priority int, match, action string, nexthops, bfdSessions []string, externalIDs map[string]string) error
	UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error
	DeleteLogicalRouterPolicyByUUID(lr, uuid string) error
	DeleteLogicalRouterPolicies(lr string, priority int, externalIDs map[string]string) error
	ListLogicalRouterStaticRoutes(lrName string, routeTable, policy *string, ipPrefix string, externalIDs map[string]string) ([]*ovnnb.LogicalRouterStaticRoute, error)
	AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix string, bfdID *string, externalIDs map[string]string, nexthops ...string) error
	DeleteLogicalRouterStaticRouteByExternalIDs(lrName string, externalIDs map[string]string) error
	DeleteLogicalRouterStaticRoute(lrName string, routeTable, policy *string, ipPrefix, nextHop string) error
}
