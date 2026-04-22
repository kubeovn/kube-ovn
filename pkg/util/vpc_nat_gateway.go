package util

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// VpcNatGwNameDefaultPrefix is the default prefix appended to the name of the NAT gateways
const VpcNatGwNameDefaultPrefix = "vpc-nat-gw"

// VpcNatGwNamePrefix is appended to the name of the StatefulSet and Pods for NAT gateways
var VpcNatGwNamePrefix = VpcNatGwNameDefaultPrefix

const (
	// StatefulSet controller appends "-<10-char-hash>" to controller-revision-hash label value.
	statefulSetRevisionHashSuffixLength = 11
	// To keep controller-revision-hash valid, statefulset name must not exceed 52 chars.
	NatGwStatefulSetNameMaxLength = validation.LabelValueMaxLength - statefulSetRevisionHashSuffixLength
)

// GenNatGwName returns the full name of a NAT gateway StatefulSet/Deployment
func GenNatGwName(name string) string {
	return GenNatGwNameWithPrefix(VpcNatGwNamePrefix, name)
}

// GenNatGwNameWithPrefix returns the full name of a NAT gateway StatefulSet/Deployment
// with an explicit name prefix.
func GenNatGwNameWithPrefix(prefix, name string) string {
	if prefix == "" {
		prefix = VpcNatGwNameDefaultPrefix
	}
	return fmt.Sprintf("%s-%s", prefix, name)
}

// GenNatGwPodName returns the full name of the NAT gateway pod within a StatefulSet
func GenNatGwPodName(name string) string {
	return GenNatGwPodNameWithPrefix(VpcNatGwNamePrefix, name)
}

// GenNatGwPodNameWithPrefix returns the full name of the NAT gateway pod within a StatefulSet
// with an explicit name prefix.
func GenNatGwPodNameWithPrefix(prefix, name string) string {
	if prefix == "" {
		prefix = VpcNatGwNameDefaultPrefix
	}
	return fmt.Sprintf("%s-%s-0", prefix, name)
}

// ValidateNatGwStatefulSetNameLength validates generated NAT GW StatefulSet name length.
// This check is stricter than the plain 63-char label value limit because StatefulSet
// controller appends a hash suffix to `controller-revision-hash` label values.
func ValidateNatGwStatefulSetNameLength(prefix, gwName string) error {
	statefulSetName := GenNatGwNameWithPrefix(prefix, gwName)
	if len(statefulSetName) > NatGwStatefulSetNameMaxLength {
		return fmt.Errorf("generated NAT gateway statefulset name %q length %d exceeds max %d; choose a shorter NAT gateway name",
			statefulSetName, len(statefulSetName), NatGwStatefulSetNameMaxLength)
	}
	return nil
}

// GetNatGwExternalNetwork returns the external network attached to a NAT gateway
func GetNatGwExternalNetwork(externalNets []string) string {
	if len(externalNets) == 0 {
		return vpcExternalNet
	}
	return externalNets[0]
}

// GenNatGwLabels returns the labels to set on a NAT gateway
func GenNatGwLabels(gwName string) map[string]string {
	return map[string]string{
		"app":              GenNatGwName(gwName),
		VpcNatGatewayLabel: "true",
	}
}

// GenNatGwSelectors returns the selectors of a NAT gateway
func GenNatGwSelectors(selectors []string) map[string]string {
	s := make(map[string]string, len(selectors))
	for _, v := range selectors {
		parts := strings.Split(strings.TrimSpace(v), ":")
		if len(parts) != 2 {
			continue
		}
		s[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	return s
}

// GenNatGwPodAnnotations generates StatefulSet Pod template annotations for a NAT gateway.
// userAnnotations contains user-defined annotations from gw.Spec.Annotations. System annotations
// are set on top of it, overwriting any conflicts. additionalNetworks is optional, used when
// users specify extra NADs in gw.Annotations. enableNonPrimaryCNI indicates whether Kube-OVN is
// running as a non-primary CNI; in that mode eth0 must stay on the cluster's primary CNI pod
// network (e.g. Calico) so the gateway pod can reach the Kubernetes control plane, so the
// v1.multus-cni.io/default-network override is skipped.
func GenNatGwPodAnnotations(userAnnotations map[string]string, gw *kubeovnv1.VpcNatGateway, externalNadNamespace, externalNadName, provider, additionalNetworks string, enableNonPrimaryCNI bool) (map[string]string, error) {
	p := provider
	if p == "" {
		p = OvnProvider
	}

	attachedNetworks := fmt.Sprintf("%s/%s", externalNadNamespace, externalNadName)
	if additionalNetworks != "" {
		attachedNetworks = additionalNetworks + ", " + attachedNetworks
	}

	// Create a new map to avoid modifying the input map (which may be from informer cache)
	result := make(map[string]string, len(userAnnotations)+5)
	maps.Copy(result, userAnnotations)

	// Set system annotations (overwrites any conflicting user annotations)
	result[nadv1.NetworkAttachmentAnnot] = attachedNetworks
	result[VpcNatGatewayAnnotation] = gw.Name
	result[fmt.Sprintf(LogicalSwitchAnnotationTemplate, p)] = gw.Spec.Subnet

	// Use LanIP for IP address annotation only in non-HA mode (replicas = 1)
	// In HA mode, IPs are dynamically allocated and don't need static assignment
	replicas := gw.Spec.Replicas
	if replicas == 0 {
		replicas = 1
	}
	if replicas == 1 && gw.Spec.LanIP != "" {
		result[fmt.Sprintf(IPAddressAnnotationTemplate, p)] = gw.Spec.LanIP
	}

	// Validate the custom provider string whenever it isn't the built-in ovn one, regardless of
	// the CNI mode, so that malformed providers are caught early rather than producing bogus
	// annotation keys for LogicalSwitch/IPAddress.
	if p != OvnProvider {
		// Subdivide the provider so we can infer the namespace/name of the NetworkAttachmentDefinition
		providerSplit := strings.Split(provider, ".")
		if len(providerSplit) != 3 || providerSplit[2] != OvnProvider {
			return nil, fmt.Errorf("name of the provider must have syntax 'name.namespace.ovn', got %s", provider)
		}

		// Override the default network of the pod only under primary CNI mode, so the default
		// VPC/Subnet of the cluster isn't accidentally injected. In non-primary CNI mode eth0
		// belongs to the cluster's primary CNI and overriding it with a tenant NAD would break
		// pod/control-plane connectivity (see issue #6632).
		if !enableNonPrimaryCNI {
			name, namespace := providerSplit[0], providerSplit[1]
			result[DefaultNetworkAnnotation] = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	return result, nil
}

// GenNatGwBgpSpeakerContainer crafts a BGP speaker container for a VPC gateway
func GenNatGwBgpSpeakerContainer(speakerParams kubeovnv1.VpcBgpSpeaker, speakerImage, gatewayName string) (*corev1.Container, error) {
	// We need a speaker image configured in the NAT GW ConfigMap
	if speakerImage == "" {
		return nil, fmt.Errorf("%s should have bgp speaker image field if bgp enabled", VpcNatConfig)
	}

	args := []string{
		"--nat-gw-mode", // Force speaker to run in NAT GW mode, we're not announcing Pod IPs or Services, only EIPs
	}

	if speakerParams.RouterID != "" { // Override default auto-selected RouterID
		args = append(args, "--router-id="+speakerParams.RouterID)
	}

	if speakerParams.Password != "" { // Password for TCP MD5 BGP
		args = append(args, "--auth-password="+speakerParams.Password)
	}

	if speakerParams.EnableGracefulRestart { // Enable graceful restart
		args = append(args, "--graceful-restart")
	}

	if speakerParams.HoldTime != (metav1.Duration{}) { // Hold time
		args = append(args, "--holdtime="+speakerParams.HoldTime.Duration.String())
	}

	if speakerParams.ASN == 0 { // The ASN we use to speak
		return nil, errors.New("ASN not set, but must be non-zero value")
	}

	if speakerParams.RemoteASN == 0 { // The ASN we speak to
		return nil, errors.New("remote ASN not set, but must be non-zero value")
	}

	args = append(args, fmt.Sprintf("--cluster-as=%d", speakerParams.ASN))
	args = append(args, fmt.Sprintf("--neighbor-as=%d", speakerParams.RemoteASN))

	if len(speakerParams.Neighbors) == 0 {
		return nil, errors.New("no BGP neighbors specified")
	}

	var neighIPv4 []string
	var neighIPv6 []string
	for _, neighbor := range speakerParams.Neighbors {
		switch CheckProtocol(neighbor) {
		case kubeovnv1.ProtocolIPv4:
			neighIPv4 = append(neighIPv4, neighbor)
		case kubeovnv1.ProtocolIPv6:
			neighIPv6 = append(neighIPv6, neighbor)
		default:
			return nil, fmt.Errorf("unsupported protocol for peer %s", neighbor)
		}
	}

	argNeighIPv4 := strings.Join(neighIPv4, ",")
	argNeighIPv6 := strings.Join(neighIPv6, ",")
	argNeighIPv4 = "--neighbor-address=" + argNeighIPv4
	argNeighIPv6 = "--neighbor-ipv6-address=" + argNeighIPv6

	if len(neighIPv4) > 0 {
		args = append(args, argNeighIPv4)
	}

	if len(neighIPv6) > 0 {
		args = append(args, argNeighIPv6)
	}

	// Extra args to start the speaker with, for example, logging levels...
	args = append(args, speakerParams.ExtraArgs...)

	bgpSpeakerContainer := &corev1.Container{
		Name:            "vpc-nat-gw-speaker",
		Image:           speakerImage,
		Command:         []string{"/kube-ovn/kube-ovn-speaker"},
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{
				Name:  EnvGatewayName,
				Value: gatewayName,
			},
			{
				Name: EnvPodIP,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIP",
					},
				},
			},
			{
				Name: EnvPodIPs,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIPs",
					},
				},
			},
		},
		Args: args,
	}

	return bgpSpeakerContainer, nil
}

type NbClient interface {
	FindBFD(externalIDs map[string]string) ([]ovnnb.BFD, error)
	CreateBFD(lrp, dstIP string, minRX, minTX, detectMult int, externalIDs map[string]string) (*ovnnb.BFD, error)
	DeleteBFD(uuid string) error
	ListLogicalRouterPolicies(lr string, priority int, externalIDs map[string]string, ignoreNotFound bool) ([]*ovnnb.LogicalRouterPolicy, error)
	AddLogicalRouterPolicy(lr string, priority int, match, action string, nexthops, bfdSessions []string, externalIDs map[string]string) error
	UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error
	DeleteLogicalRouterPolicyByUUID(lr, uuid string) error
	DeleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error
}

// CleanupNatGwRoutesAF deletes OVN logical router policies and BFD sessions associated with a VPC NAT Gateway for a specific address family.
func CleanupNatGwRoutesAF(nbClient NbClient, gwName, vpcName string, af int) error {
	externalIDsAF := map[string]string{
		"vendor":     CniTypeName,
		"vpc-nat-gw": gwName,
		"af":         strconv.Itoa(af),
	}

	// Delete routing policies and drop policies for the given address family
	if err := nbClient.DeleteLogicalRouterPolicies(vpcName, NatGatewayPolicyPriority, externalIDsAF); err != nil {
		klog.Errorf("failed to delete policies for nat gw %s af %d: %v", gwName, af, err)
		return err
	}
	if err := nbClient.DeleteLogicalRouterPolicies(vpcName, NatGatewayDropPolicyPriority, externalIDsAF); err != nil {
		klog.Errorf("failed to delete drop policies for nat gw %s af %d: %v", gwName, af, err)
		return err
	}

	// Delete BFD sessions for the given address family
	bfds, err := nbClient.FindBFD(externalIDsAF)
	if err != nil {
		klog.Errorf("failed to find BFD sessions for nat gw %s af %d: %v", gwName, af, err)
		return err
	}
	for _, bfd := range bfds {
		if err := nbClient.DeleteBFD(bfd.UUID); err != nil {
			klog.Errorf("failed to delete BFD session %s for nat gw %s af %d: %v", bfd.UUID, gwName, af, err)
			return err
		}
	}

	klog.V(3).Infof("deleted policies and BFD sessions for nat gw %s af %d", gwName, af)
	return nil
}

// GroupInternalCIDRsAndNextHops groups internal CIDRs and next hops by address family.
func GroupInternalCIDRsAndNextHops(internalCIDRs []string, nextHops map[string]string) (map[int][]string, map[int]map[string]string) {
	cidrsByAF := map[int][]string{4: {}, 6: {}}
	for _, cidr := range internalCIDRs {
		af := 4
		if CheckProtocol(cidr) == kubeovnv1.ProtocolIPv6 {
			af = 6
		}
		cidrsByAF[af] = append(cidrsByAF[af], cidr)
	}

	nextHopsByAF := map[int]map[string]string{4: {}, 6: {}}
	for node, ip := range nextHops {
		af := 4
		if CheckProtocol(ip) == kubeovnv1.ProtocolIPv6 {
			af = 6
		}
		nextHopsByAF[af][node] = ip
	}

	return cidrsByAF, nextHopsByAF
}

type ReconcileNatGwRoutesAFArgs struct {
	NbClient        NbClient
	GwName          string
	VpcName         string
	LrpName         string
	BfdIP           string
	Af              int
	MinTX           int32
	MinRX           int32
	Multiplier      int32
	BfdEnabled      bool
	InternalCIDRsAF []string
	NextHopsAF      map[string]string
	ExternalIDsAF   map[string]string
	ReconcileBFD    func(NbClient, string, string, map[string]string, int32, int32, int32, map[string]string) (set.Set[string], map[string]string, set.Set[string], error)
	ReconcilePolicy func(NbClient, string, string, int, bool, set.Set[string], []string, map[string]string, map[string]string) error
	CleanupStaleBFD func(NbClient, set.Set[string]) error
}

func ReconcileNatGwRoutesAF(args ReconcileNatGwRoutesAFArgs) error {
	if len(args.InternalCIDRsAF) > 0 && len(args.NextHopsAF) > 0 {
		// Reconcile BFD sessions for this address family
		bfdIDs, _, staleBFDIDs, err := args.ReconcileBFD(
			args.NbClient,
			args.BfdIP,
			args.LrpName,
			args.NextHopsAF,
			args.MinTX,
			args.MinRX,
			args.Multiplier,
			args.ExternalIDsAF,
		)
		if err != nil {
			klog.Errorf("failed to reconcile BFD for nat gw %s af %d: %v", args.GwName, args.Af, err)
			return err
		}

		// Reconcile logical router policies (PBR) for this address family
		if err := args.ReconcilePolicy(
			args.NbClient,
			args.GwName,
			args.VpcName,
			args.Af,
			args.BfdEnabled,
			bfdIDs,
			args.InternalCIDRsAF,
			args.NextHopsAF,
			args.ExternalIDsAF,
		); err != nil {
			klog.Errorf("failed to reconcile policies for nat gw %s af %d: %v", args.GwName, args.Af, err)
			return err
		}

		// Clean up any BFD sessions that are no longer needed
		if err := args.CleanupStaleBFD(args.NbClient, staleBFDIDs); err != nil {
			klog.Errorf("failed to cleanup stale BFD for nat gw %s af %d: %v", args.GwName, args.Af, err)
			return err
		}
	} else {
		// No routes needed for this address family, ensure any existing ones are removed
		if err := CleanupNatGwRoutesAF(args.NbClient, args.GwName, args.VpcName, args.Af); err != nil {
			return err
		}
	}
	return nil
}
