package util

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestExternalLBInterface(t *testing.T) {
	subnet := kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
		ExternalLBInterface: "mlb0", EnableExternalLBAddress: true, Vlan: "vlan1", CIDRBlock: "192.0.2.0/24", Vpc: DefaultVpc,
	}}
	require.NoError(t, ValidateExternalLBInterface(subnet))
	tests := map[string]func(*kubeovnv1.Subnet){
		"reserved":         func(s *kubeovnv1.Subnet) { s.Spec.ExternalLBInterface = "br-int" },
		"path":             func(s *kubeovnv1.Subnet) { s.Spec.ExternalLBInterface = "../mlb0" },
		"too long":         func(s *kubeovnv1.Subnet) { s.Spec.ExternalLBInterface = "abcdefghijklmnop" },
		"IPv6":             func(s *kubeovnv1.Subnet) { s.Spec.CIDRBlock = "fd00::/64" },
		"dual stack":       func(s *kubeovnv1.Subnet) { s.Spec.CIDRBlock += ",fd00::/64" },
		"overlay":          func(s *kubeovnv1.Subnet) { s.Spec.Vlan = "" },
		"disabled":         func(s *kubeovnv1.Subnet) { s.Spec.EnableExternalLBAddress = false },
		"logical gateway":  func(s *kubeovnv1.Subnet) { s.Spec.LogicalGateway = true },
		"u2o":              func(s *kubeovnv1.Subnet) { s.Spec.U2OInterconnection = true },
		"custom VPC":       func(s *kubeovnv1.Subnet) { s.Spec.Vpc = "custom" },
		"routed":           func(s *kubeovnv1.Subnet) { s.Spec.Routed = true },
		"foreign provider": func(s *kubeovnv1.Subnet) { s.Spec.Provider = "example.net" },
		"LB disabled":      func(s *kubeovnv1.Subnet) { s.Spec.EnableLb = new(false) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := subnet.DeepCopy()
			mutate(changed)
			require.Error(t, ValidateExternalLBInterface(*changed))
			changed.Spec.ExternalLBInterface = ""
			require.NoError(t, ValidateExternalLBInterface(*changed))
		})
	}
	mac := ExternalLBInterfaceMAC("node1", "uid1")
	require.Equal(t, mac, ExternalLBInterfaceMAC("node1", "uid1"))
	require.NotEqual(t, mac, ExternalLBInterfaceMAC("node2", "uid1"))
	require.NotEqual(t, mac, ExternalLBInterfaceMAC("node1", "uid2"))
	require.Equal(t, byte(2), mac[0]&3, "locally administered unicast MAC")
}
