package util

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"regexp"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	ExternalLBPortRole  = "external-lb"
	ExternalLBARPOption = "localnet-arp-only"
)

var externalLBInterfacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,14}$`)

func ExternalLBLocalPortName(subnet string) string {
	return "external-lb." + subnet
}

func ExternalLBInterfaceMAC(node, subnetUID string) net.HardwareAddr {
	digest := sha256.Sum256([]byte(node + "\x00" + subnetUID))
	return net.HardwareAddr{0x02, digest[0], digest[1], digest[2], digest[3], digest[4]}
}

func ExternalLBReadyLabel(iface string) string {
	return "ovn.kubernetes.io/external-lb-" + iface
}

func ValidateExternalLBInterface(subnet kubeovnv1.Subnet) error {
	name := subnet.Spec.ExternalLBInterface
	if name == "" {
		return nil
	}
	if !externalLBInterfacePattern.MatchString(name) || name == "lo" || name == "br-int" || name == NodeNic {
		return fmt.Errorf("externalLBInterface %q must be a non-reserved interface name of 1-15 letters, digits, underscores or hyphens", name)
	}
	if !subnet.Spec.EnableExternalLBAddress || subnet.Spec.Vlan == "" || CheckProtocol(subnet.Spec.CIDRBlock) != kubeovnv1.ProtocolIPv4 {
		return errors.New("externalLBInterface requires enableExternalLBAddress and an IPv4-only underlay subnet")
	}
	if subnet.Spec.Routed || subnet.Spec.Provider != "" && subnet.Spec.Provider != CniTypeName || subnet.Spec.LogicalGateway || subnet.Spec.U2OInterconnection || subnet.Spec.Vpc != "" && subnet.Spec.Vpc != DefaultVpc || subnet.Spec.EnableLb != nil && !*subnet.Spec.EnableLb {
		return errors.New("externalLBInterface requires OVN load balancing in the default VPC without logicalGateway or u2oInterconnection")
	}
	return nil
}
