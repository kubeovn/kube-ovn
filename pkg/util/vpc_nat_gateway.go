package util

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// VpcNatGwNameDefaultPrefix is the default prefix appended to the name of the NAT gateways
const VpcNatGwNameDefaultPrefix = "vpc-nat-gw"

// VpcNatGwNamePrefix is appended to the name of the StatefulSet and Pods for NAT gateways
var VpcNatGwNamePrefix = VpcNatGwNameDefaultPrefix

// GenNatGwName returns the full name of a NAT gateway StatefulSet/Deployment
func GenNatGwName(name string) string {
	return fmt.Sprintf("%s-%s", VpcNatGwNamePrefix, name)
}

// GenNatGwPodName returns the full name of the NAT gateway pod within a StatefulSet
func GenNatGwPodName(name string) string {
	return fmt.Sprintf("%s-%s-0", VpcNatGwNamePrefix, name)
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
		"app":                  GenNatGwName(gwName),
		VpcNatGatewayLabel:     "true",
		VpcNatGatewayNameLabel: gwName,
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
// users specify extra NADs in gw.Annotations.
func GenNatGwPodAnnotations(userAnnotations map[string]string, gw *kubeovnv1.VpcNatGateway, externalNadNamespace, externalNadName, provider, additionalNetworks string) (map[string]string, error) {
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
	for k, v := range userAnnotations {
		result[k] = v
	}

	// Set system annotations (overwrites any conflicting user annotations)
	result[nadv1.NetworkAttachmentAnnot] = attachedNetworks
	result[VpcNatGatewayAnnotation] = gw.Name
	result[fmt.Sprintf(LogicalSwitchAnnotationTemplate, p)] = gw.Spec.Subnet
	result[fmt.Sprintf(IPAddressAnnotationTemplate, p)] = gw.Spec.LanIP

	// We're using a custom provider, we need to override the default network of the pod so that the
	// default VPC/Subnet of the cluster isn't accidentally injected.
	if p != OvnProvider {
		// Subdivide the provider so we can infer the namespace/name of the NetworkAttachmentDefinition
		providerSplit := strings.Split(provider, ".")
		if len(providerSplit) != 3 || providerSplit[2] != OvnProvider {
			return nil, fmt.Errorf("name of the provider must have syntax 'name.namespace.ovn', got %s", provider)
		}

		name, namespace := providerSplit[0], providerSplit[1]
		result[DefaultNetworkAnnotation] = fmt.Sprintf("%s/%s", namespace, name)
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

const (
	// MacvlanLinkPrefix is the prefix for macvlan sub-interfaces created for node local EIP access.
	// Format: "mac" (3 chars) + master interface name or FNV hash (up to 12 chars) = max 15 chars.
	MacvlanLinkPrefix = "mac"
)

// GenMacvlanIfaceName generates macvlan sub-interface name from master interface name.
// Format: "mac" (3 chars) + master name or FNV hash (up to 12 chars) = max 15 chars.
// Linux interface names are limited to 15 characters.
//
// For short master names (<=12 chars), appends master name directly.
// For longer names, uses FNV-1a 32-bit hash to ensure uniqueness.
//
// Examples:
//
//	master "eth0"        -> "maceth0"
//	master "bond0"       -> "macbond0"
//	master "enp0s25"     -> "macenp0s25"
//	master "very-long-interface-name" -> "mac" + hash
func GenMacvlanIfaceName(master string) (string, error) {
	if master == "" {
		return "", errors.New("master interface name is empty")
	}

	maxMasterLen := 15 - len(MacvlanLinkPrefix) // 12 chars for master part

	// If master name fits, use it directly
	if len(master) <= maxMasterLen {
		return MacvlanLinkPrefix + master, nil
	}

	// For longer names, use FNV-1a 32-bit hash (8 hex chars)
	h := fnv.New32a()
	h.Write([]byte(master))
	return fmt.Sprintf("%s%08x", MacvlanLinkPrefix, h.Sum32()), nil
}
