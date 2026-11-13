package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
)

func TestProjectSubnetStatusIPPreservesInputAndForeignStatus(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/29",
			Protocol:  kubeovnv1.ProtocolIPv4,
		},
		Status: kubeovnv1.SubnetStatus{
			V4AvailableIPs: internal.NewBigInt(99),
			Conditions: []kubeovnv1.Condition{{
				Type: kubeovnv1.ConditionType("Foreign"),
			}},
		},
	}
	subnet.Name = "subnet-a"

	projected, err := ProjectSubnetStatusIP(subnet, SubnetStatusIPProjectionInput{
		UsingIPs:           internal.NewBigInt(2),
		V4UsingIPRange:     "10.0.0.2",
		V4AvailableIPRange: "10.0.0.3-10.0.0.6",
	})
	require.NoError(t, err)
	require.True(t, projected.Status.V4AvailableIPs.EqualInt64(4))
	require.True(t, projected.Status.V4UsingIPs.EqualInt64(2))
	require.True(t, projected.Status.V6UsingIPs.EqualInt64(0))
	require.Equal(t, "10.0.0.2", projected.Status.V4UsingIPRange)
	require.Equal(t, "10.0.0.3-10.0.0.6", projected.Status.V4AvailableIPRange)
	require.Equal(t, subnet.Status.V4AvailableIPs.String(), "99")
	require.Len(t, projected.Status.Conditions, 1)
}

func TestProjectSubnetStatusIPSupportsDualStack(t *testing.T) {
	subnet := &kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.0.0.0/29,2001:db8::/125",
		Protocol:  kubeovnv1.ProtocolDual,
	}}

	projected, err := ProjectSubnetStatusIP(subnet, SubnetStatusIPProjectionInput{UsingIPs: internal.NewBigInt(2)})
	require.NoError(t, err)
	require.True(t, projected.Status.V4AvailableIPs.EqualInt64(4))
	require.True(t, projected.Status.V6AvailableIPs.EqualInt64(4))
	require.True(t, projected.Status.V4UsingIPs.EqualInt64(2))
	require.True(t, projected.Status.V6UsingIPs.EqualInt64(2))
}

func TestProjectSubnetStatusIPClampsNegativeAvailableCounts(t *testing.T) {
	subnet := &kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.0.0.0/29",
		Protocol:  kubeovnv1.ProtocolIPv4,
	}}

	projected, err := ProjectSubnetStatusIP(subnet, SubnetStatusIPProjectionInput{
		UsingIPs: internal.NewBigInt(99),
	})
	require.NoError(t, err)
	require.True(t, projected.Status.V4AvailableIPs.EqualInt64(0))
	require.True(t, projected.Status.V6AvailableIPs.EqualInt64(0))
}

func TestProjectSubnetStatusIPSupportsIPv6Only(t *testing.T) {
	subnet := &kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "2001:db8::/125",
		Protocol:  kubeovnv1.ProtocolIPv6,
	}}

	projected, err := ProjectSubnetStatusIP(subnet, SubnetStatusIPProjectionInput{
		UsingIPs:       internal.NewBigInt(2),
		V6UsingIPRange: "2001:db8::2",
	})
	require.NoError(t, err)
	require.True(t, projected.Status.V4AvailableIPs.EqualInt64(0))
	require.True(t, projected.Status.V4UsingIPs.EqualInt64(0))
	require.True(t, projected.Status.V6AvailableIPs.EqualInt64(4))
	require.True(t, projected.Status.V6UsingIPs.EqualInt64(2))
	require.Equal(t, "2001:db8::2", projected.Status.V6UsingIPRange)
}

func TestProjectSubnetStatusIPPreservesAnEquivalentStatus(t *testing.T) {
	subnet := &kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.0.0.0/29",
		Protocol:  kubeovnv1.ProtocolIPv4,
	}, Status: kubeovnv1.SubnetStatus{
		V4AvailableIPs:     internal.NewBigInt(4),
		V4UsingIPs:         internal.NewBigInt(2),
		V4UsingIPRange:     "10.0.0.2",
		V4AvailableIPRange: "10.0.0.3-10.0.0.6",
	}}

	projected, err := ProjectSubnetStatusIP(subnet, SubnetStatusIPProjectionInput{
		UsingIPs:           internal.NewBigInt(2),
		V4UsingIPRange:     "10.0.0.2",
		V4AvailableIPRange: "10.0.0.3-10.0.0.6",
	})
	require.NoError(t, err)
	require.True(t, projected.Status.V4AvailableIPs.Equal(subnet.Status.V4AvailableIPs))
	require.True(t, projected.Status.V4UsingIPs.Equal(subnet.Status.V4UsingIPs))
	require.Equal(t, subnet.Status.V4UsingIPRange, projected.Status.V4UsingIPRange)
	require.Equal(t, subnet.Status.V4AvailableIPRange, projected.Status.V4AvailableIPRange)
}

func TestProjectSubnetStatusIPRejectsInvalidInput(t *testing.T) {
	_, err := ProjectSubnetStatusIP(nil, SubnetStatusIPProjectionInput{})
	require.EqualError(t, err, "subnet status projection requires a Subnet")

	_, err = ProjectSubnetStatusIP(&kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{CIDRBlock: "invalid"}}, SubnetStatusIPProjectionInput{})
	require.Error(t, err)
}

func TestProjectSubnetStatusIPRejectsIncompleteDualStackCIDRs(t *testing.T) {
	_, err := ProjectSubnetStatusIP(&kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
		CIDRBlock: "10.0.0.0/29",
		Protocol:  kubeovnv1.ProtocolDual,
	}}, SubnetStatusIPProjectionInput{})
	require.EqualError(t, err, "subnet status projection CIDR count does not match protocol")
}

func TestProjectSubnetStatusIPRejectsCIDRShapeMismatches(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		cidr     string
		message  string
	}{
		{
			name:     "ipv4 multiple CIDRs",
			protocol: kubeovnv1.ProtocolIPv4,
			cidr:     "10.0.0.0/29,10.0.1.0/29",
			message:  "subnet status projection CIDR count does not match protocol",
		},
		{
			name:     "ipv6 multiple CIDRs",
			protocol: kubeovnv1.ProtocolIPv6,
			cidr:     "2001:db8::/125,2001:db8:1::/125",
			message:  "subnet status projection CIDR count does not match protocol",
		},
		{
			name:     "dual wrong family order",
			protocol: kubeovnv1.ProtocolDual,
			cidr:     "2001:db8::/125,10.0.0.0/29",
			message:  "subnet status projection CIDR family does not match protocol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ProjectSubnetStatusIP(&kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: tt.cidr,
				Protocol:  tt.protocol,
			}}, SubnetStatusIPProjectionInput{})
			require.EqualError(t, err, tt.message)
		})
	}
}
