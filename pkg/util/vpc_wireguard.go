package util

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	VpcWireGuardNamePrefix = "vpc-wg"
	VpcWireGuardContainer  = "vpc-wireguard"
)

// GenVpcWireGuardName returns the StatefulSet name for a VpcWireGuard.
func GenVpcWireGuardName(name string) string {
	return fmt.Sprintf("%s-%s", VpcWireGuardNamePrefix, name)
}

// GenVpcWireGuardPodName returns the first StatefulSet pod name.
func GenVpcWireGuardPodName(name string) string {
	return fmt.Sprintf("%s-%s-0", VpcWireGuardNamePrefix, name)
}

// GenVpcWireGuardServerSecretName is the generated server key Secret.
func GenVpcWireGuardServerSecretName(name string) string {
	return fmt.Sprintf("%s-%s-server", VpcWireGuardNamePrefix, name)
}

// GenVpcWireGuardPeerSecretName is the generated client config Secret.
func GenVpcWireGuardPeerSecretName(name string) string {
	return fmt.Sprintf("%s-peer-%s", VpcWireGuardNamePrefix, name)
}

// GenVpcWireGuardDnatName is the owned IptablesDnatRule name.
func GenVpcWireGuardDnatName(name string) string {
	return fmt.Sprintf("%s-dnat-%s", VpcWireGuardNamePrefix, name)
}

// GenVpcWireGuardFipName is the owned IptablesFIPRule name.
func GenVpcWireGuardFipName(name string) string {
	return fmt.Sprintf("%s-fip-%s", VpcWireGuardNamePrefix, name)
}

// VpcWireGuardPeerIPAMName is the IPAM nic/pod name for a peer.
func VpcWireGuardPeerIPAMName(peerName string) string {
	return fmt.Sprintf("vpc-wg-peer.%s", peerName)
}

// VpcWireGuardServerIPAMName is the IPAM nic/pod name for the wg0 address.
func VpcWireGuardServerIPAMName(gwName string) string {
	return fmt.Sprintf("vpc-wg-server.%s", gwName)
}

// ValidateVpcWireGuardStatefulSetNameLength validates generated STS name length.
func ValidateVpcWireGuardStatefulSetNameLength(name string) error {
	stsName := GenVpcWireGuardName(name)
	if len(stsName) > NatGwStatefulSetNameMaxLength {
		return fmt.Errorf("generated WireGuard statefulset name %q length %d exceeds max %d; choose a shorter name",
			stsName, len(stsName), NatGwStatefulSetNameMaxLength)
	}
	if errs := validation.IsDNS1123Subdomain(stsName); len(errs) > 0 {
		return fmt.Errorf("generated WireGuard statefulset name %q is invalid: %s", stsName, strings.Join(errs, ", "))
	}
	return nil
}

// GenerateWireGuardKeyPair returns a base64-encoded X25519 private/public key pair.
func GenerateWireGuardKeyPair() (privateKey, publicKey string, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv.Bytes()),
		base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()),
		nil
}

// ParseWireGuardPublicKey decodes and validates a WireGuard public key.
func ParseWireGuardPublicKey(key string) error {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("invalid wireguard public key encoding: %w", err)
	}
	if len(raw) != 32 {
		return fmt.Errorf("invalid wireguard public key length %d, want 32", len(raw))
	}
	return nil
}

// GenVpcWireGuardLabels returns labels for the server workload.
func GenVpcWireGuardLabels(name string) map[string]string {
	return map[string]string{
		"app":             GenVpcWireGuardName(name),
		VpcWireGuardLabel: "true",
	}
}

// DefaultVpcWireGuardListenPort returns the listen port, defaulting to 51820.
func DefaultVpcWireGuardListenPort(port int32) int32 {
	if port <= 0 {
		return 51820
	}
	return port
}

// GenVpcWireGuardPodAnnotations builds Multus/IP annotations for the server pod.
func GenVpcWireGuardPodAnnotations(gw *kubeovnv1.VpcWireGuard, externalNadNamespace, externalNadName, provider string, enableNonPrimaryCNI bool) (map[string]string, error) {
	p := provider
	if p == "" {
		p = OvnProvider
	}

	result := map[string]string{
		VpcWireGuardAnnotation:                          gw.Name,
		fmt.Sprintf(LogicalSwitchAnnotationTemplate, p): gw.Spec.Subnet,
	}
	if gw.Spec.LanIP != "" {
		result[fmt.Sprintf(IPAddressAnnotationTemplate, p)] = gw.Spec.LanIP
	}

	if gw.Spec.Exposure.Type == kubeovnv1.VpcWireGuardExposureDualNIC {
		if externalNadNamespace == "" || externalNadName == "" {
			return nil, fmt.Errorf("dual-nic WireGuard %s requires an external NetworkAttachmentDefinition", gw.Name)
		}
		result[nadv1.NetworkAttachmentAnnot] = fmt.Sprintf("%s/%s", externalNadNamespace, externalNadName)
	}

	if p != OvnProvider {
		providerSplit := strings.Split(provider, ".")
		if len(providerSplit) != 3 || providerSplit[2] != OvnProvider {
			return nil, fmt.Errorf("name of the provider must have syntax 'name.namespace.ovn', got %s", provider)
		}
		if !enableNonPrimaryCNI {
			name, namespace := providerSplit[0], providerSplit[1]
			result[DefaultNetworkAnnotation] = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	return result, nil
}

// RenderWireGuardServerConfig builds a wg-quick config for the server.
func RenderWireGuardServerConfig(privateKey, address string, listenPort, mtu int32, peers []WireGuardPeerConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\n", privateKey, address, listenPort)
	if mtu > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", mtu)
	}
	b.WriteString("\n")
	for _, peer := range peers {
		fmt.Fprintf(&b, "[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", peer.PublicKey, peer.AllowedIPs)
		if peer.PresharedKey != "" {
			fmt.Fprintf(&b, "PresharedKey = %s\n", peer.PresharedKey)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// RenderWireGuardClientConfig builds a wg-quick config for a client.
func RenderWireGuardClientConfig(privateKey, address, dns, serverPublicKey, endpoint, allowedIPs, psk string, keepalive int32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\n", privateKey, address)
	if dns != "" {
		fmt.Fprintf(&b, "DNS = %s\n", dns)
	}
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = %s\n", serverPublicKey, endpoint, allowedIPs)
	if psk != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", psk)
	}
	if keepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", keepalive)
	}
	return b.String()
}

// WireGuardPeerConfig is one [Peer] stanza for the server.
type WireGuardPeerConfig struct {
	PublicKey    string
	AllowedIPs   string
	PresharedKey string
}
