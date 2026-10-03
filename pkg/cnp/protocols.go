package cnp

import (
	"errors"
	"fmt"

	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
)

// ProtocolPort returns the transport and numeric destination match.
// Named ports must be rejected before changing any existing OVN resources.
func ProtocolPort(p v1alpha2.ClusterNetworkPolicyProtocol) (string, *v1alpha2.Port, error) {
	count, transport := 0, ""
	var port *v1alpha2.Port
	if p.TCP != nil {
		count++
		transport, port = "tcp", p.TCP.DestinationPort
	}
	if p.UDP != nil {
		count++
		transport, port = "udp", p.UDP.DestinationPort
	}
	if p.SCTP != nil {
		count++
		transport, port = "sctp", p.SCTP.DestinationPort
	}
	if p.DestinationNamedPort != "" {
		return "", nil, errors.New("destinationNamedPort is not supported by Kube-OVN CNP")
	}
	if count != 1 || port == nil {
		return "", nil, errors.New("exactly one transport with destinationPort is required")
	}
	if port.Range == nil {
		if port.Number < 1 || port.Number > 65535 {
			return "", nil, errors.New("destination port must be between 1 and 65535")
		}
	} else if port.Number != 0 || port.Range.Start < 1 || port.Range.End > 65535 || port.Range.Start >= port.Range.End {
		return "", nil, errors.New("invalid destination port range")
	}
	return transport, port, nil
}

func ValidateProtocols(policy *v1alpha2.ClusterNetworkPolicy) error {
	for i, rule := range policy.Spec.Ingress {
		for _, p := range rule.Protocols {
			if _, _, err := ProtocolPort(p); err != nil {
				return fmt.Errorf("ingress[%d]: %w", i, err)
			}
		}
	}
	for i, rule := range policy.Spec.Egress {
		for _, p := range rule.Protocols {
			if _, _, err := ProtocolPort(p); err != nil {
				return fmt.Errorf("egress[%d]: %w", i, err)
			}
		}
	}
	return nil
}
