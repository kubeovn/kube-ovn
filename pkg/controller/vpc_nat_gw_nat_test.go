package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// TestNatRuleCreationClaimsEipBeforeRule pins that every NAT rule records the eip generation it
// depends on before the rule can exist in the gateway pod. An eip marked for deletion finds its
// users through that label, so a rule that is already in the pod must be able to claim the eip.
func TestNatRuleCreationClaimsEipBeforeRule(t *testing.T) {
	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })

	eip := func() *kubeovnv1.IptablesEIP {
		return &kubeovnv1.IptablesEIP{
			Name: "eip", UID: "eip-uid",
			Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw"},
			Status: kubeovnv1.IptablesEIPStatus{IP: "2.2.2.2", Ready: true},
		}
	}
	// The gateway exists but has no pod, so creating the rule fails while the claim is written.
	options := func() *FakeControllerOptions {
		return &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
			IptablesEips:   []*kubeovnv1.IptablesEIP{eip()},
		}
	}

	t.Run("fip", func(t *testing.T) {
		fip := &kubeovnv1.IptablesFIPRule{
			Name: "fip",
			Spec: kubeovnv1.IptablesFIPRuleSpec{EIP: "eip", InternalIP: "10.0.0.5"},
		}
		opts := options()
		opts.IptablesFips = []*kubeovnv1.IptablesFIPRule{fip}
		fc, err := newFakeControllerWithOptions(t, opts)
		require.NoError(t, err)

		require.Error(t, fc.fakeController.handleAddIptablesFip("fip"))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Get(
			context.Background(), "fip", metav1.GetOptions{},
		)
		require.NoError(t, err)
		require.Equal(t, "eip-uid", got.Labels[util.EipUIDLabel])
	})

	t.Run("dnat", func(t *testing.T) {
		dnat := &kubeovnv1.IptablesDnatRule{
			Name: "dnat",
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: "eip", Protocol: "tcp", ExternalPort: "80", InternalIP: "10.0.0.5", InternalPort: "8080",
			},
		}
		opts := options()
		opts.IptablesDnatRules = []*kubeovnv1.IptablesDnatRule{dnat}
		fc, err := newFakeControllerWithOptions(t, opts)
		require.NoError(t, err)

		require.Error(t, fc.fakeController.handleAddIptablesDnatRule("dnat"))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Get(
			context.Background(), "dnat", metav1.GetOptions{},
		)
		require.NoError(t, err)
		require.Equal(t, "eip-uid", got.Labels[util.EipUIDLabel])
	})

	t.Run("snat", func(t *testing.T) {
		snat := &kubeovnv1.IptablesSnatRule{
			Name: "snat",
			Spec: kubeovnv1.IptablesSnatRuleSpec{EIP: "eip", InternalCIDR: "10.0.0.0/24"},
		}
		opts := options()
		opts.IptablesSnatRules = []*kubeovnv1.IptablesSnatRule{snat}
		fc, err := newFakeControllerWithOptions(t, opts)
		require.NoError(t, err)

		require.Error(t, fc.fakeController.handleAddIptablesSnatRule("snat"))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Get(
			context.Background(), "snat", metav1.GetOptions{},
		)
		require.NoError(t, err)
		require.Equal(t, "eip-uid", got.Labels[util.EipUIDLabel])
	})
}

func TestValidateDnat(t *testing.T) {
	c := &Controller{}

	tests := []struct {
		name    string
		dnat    *kubeovnv1.IptablesDnatRule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid dnat rule",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: false,
		},
		{
			name: "neither eip nor clusterIP",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "one of eip and clusterIP must be set",
		},
		{
			// A Service handled by the nftable LB service feature aligns its ingress IP and its
			// ClusterIP on one rule: both fields together is the normal shape there.
			name: "eip and clusterIP together",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ClusterIP:    "10.96.1.5",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
					Type:         kubeovnv1.DnatRuleTypeShare,
				},
			},
			wantErr: false,
		},
		{
			name: "clusterIP without a gateway",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					ClusterIP:    "10.96.1.5",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
					Type:         kubeovnv1.DnatRuleTypeShare,
				},
			},
			wantErr: true,
			errMsg:  "vpcNatGwDp is required",
		},
		{
			name: "clusterIP must be IPv4",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					ClusterIP:    "fd00::1",
					VpcNatGwDp:   "gw0",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
					Type:         kubeovnv1.DnatRuleTypeShare,
				},
			},
			wantErr: true,
			errMsg:  "must be IPv4",
		},
		{
			name: "clusterIP requires share type",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					ClusterIP:    "10.96.1.5",
					VpcNatGwDp:   "gw0",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "clusterIP requires type=share",
		},
		{
			name: "valid clusterIP share rule",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					ClusterIP:    "10.96.1.5",
					VpcNatGwDp:   "gw0",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
					Type:         kubeovnv1.DnatRuleTypeShare,
				},
			},
			wantErr: false,
		},
		{
			name: "empty externalPort",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid externalPort",
		},
		{
			name: "invalid externalPort not a number",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "abc",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid externalPort",
		},
		{
			name: "invalid externalPort out of range",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "70000",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "must be between 1 and 65535",
		},
		{
			name: "invalid externalPort zero",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "0",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "must be between 1 and 65535",
		},
		{
			name: "empty internalPort",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalPort",
		},
		{
			name: "invalid internalPort not a number",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "xyz",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalPort",
		},
		{
			name: "empty internalIP",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "internalIP cannot be empty",
		},
		{
			name: "invalid internalIP",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "not-an-ip",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalIP",
		},
		{
			name: "empty protocol",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "",
				},
			},
			wantErr: true,
			errMsg:  "invalid protocol",
		},
		{
			name: "invalid protocol",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "icmp",
				},
			},
			wantErr: true,
			errMsg:  "invalid protocol",
		},
		{
			name: "uppercase TCP protocol",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "TCP",
				},
			},
			wantErr: false,
		},
		{
			name: "uppercase UDP protocol",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "UDP",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid IPv6 internalIP - not supported",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "443",
					InternalPort: "8443",
					InternalIP:   "fd00::1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "must be IPv4",
		},
		{
			name: "max valid port",
			dnat: &kubeovnv1.IptablesDnatRule{
				Name: "test-dnat",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "65535",
					InternalPort: "65535",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.validateDnatRule(tt.dnat)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateFip(t *testing.T) {
	c := &Controller{}

	tests := []struct {
		name    string
		fip     *kubeovnv1.IptablesFIPRule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid fip rule",
			fip: &kubeovnv1.IptablesFIPRule{
				Name: "test-fip",
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "10.0.0.1",
				},
			},
			wantErr: false,
		},
		{
			name: "empty eip",
			fip: &kubeovnv1.IptablesFIPRule{
				Name: "test-fip",
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "",
					InternalIP: "10.0.0.1",
				},
			},
			wantErr: true,
			errMsg:  "eip cannot be empty",
		},
		{
			name: "empty internalIP",
			fip: &kubeovnv1.IptablesFIPRule{
				Name: "test-fip",
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "",
				},
			},
			wantErr: true,
			errMsg:  "internalIP cannot be empty",
		},
		{
			name: "invalid internalIP",
			fip: &kubeovnv1.IptablesFIPRule{
				Name: "test-fip",
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "invalid-ip",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalIP",
		},
		{
			name: "invalid IPv6 internalIP - not supported",
			fip: &kubeovnv1.IptablesFIPRule{
				Name: "test-fip",
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "2001:db8::1",
				},
			},
			wantErr: true,
			errMsg:  "must be IPv4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.validateFipRule(tt.fip)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSnat(t *testing.T) {
	c := &Controller{}

	tests := []struct {
		name    string
		snat    *kubeovnv1.IptablesSnatRule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid snat rule",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "10.0.0.0/24",
				},
			},
			wantErr: false,
		},
		{
			name: "empty eip",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "",
					InternalCIDR: "10.0.0.0/24",
				},
			},
			wantErr: true,
			errMsg:  "eip cannot be empty",
		},
		{
			name: "empty internalCIDR",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "",
				},
			},
			wantErr: true,
			errMsg:  "internalCIDR cannot be empty",
		},
		{
			name: "valid single IP address",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "10.0.0.1",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid single IPv6 address - not supported",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "fd00::1",
				},
			},
			wantErr: true,
			errMsg:  "must be IPv4",
		},
		{
			name: "invalid internalCIDR - malformed IP",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "10.0.0.256",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalCIDR",
		},
		{
			name: "invalid internalCIDR - invalid format",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "invalid-cidr",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalCIDR",
		},
		{
			name: "invalid IPv6 internalCIDR - not supported",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "fd00::/64",
				},
			},
			wantErr: true,
			errMsg:  "must be IPv4",
		},
		{
			name: "invalid multiple CIDRs - not supported",
			snat: &kubeovnv1.IptablesSnatRule{
				Name: "test-snat",
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "10.0.0.0/24,192.168.1.0/24",
				},
			},
			wantErr: true,
			errMsg:  "contains multiple CIDRs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.validateSnatRule(tt.snat)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDeleteFipInPod_NatGwGone verifies that deleteFipInPod returns nil (skips
// cleanup) when the VpcNatGateway CRD no longer exists.
func TestDeleteFipInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	err = fc.fakeController.deleteFipInPod("missing-gw", "10.0.0.1")
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDeleteFipInPod_NatGwNoRunningInstance verifies that deleteFipInPod skips the cleanup
// when the gateway has no running instance: the rules live in the gateway container.
func TestDeleteFipInPod_NatGwNoRunningInstance(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteFipInPod("test-gw", "10.0.0.1")
	require.NoError(t, err, "should skip cleanup when the gateway has no running instance")
}

// TestDeleteDnatInPod_NatGwGone verifies that deleteDnatInPod returns nil when
// the VpcNatGateway CRD no longer exists.
func TestDeleteDnatInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	err = fc.fakeController.deleteDnatInPod("missing-gw", "tcp", "10.0.0.1", "80")
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDeleteDnatInPod_NatGwNoRunningInstance verifies that deleteDnatInPod skips the cleanup
// when the gateway has no running instance.
func TestDeleteDnatInPod_NatGwNoRunningInstance(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteDnatInPod("test-gw", "tcp", "10.0.0.1", "80")
	require.NoError(t, err, "should skip cleanup when the gateway has no running instance")
}

// TestDeleteSnatInPod_NatGwGone verifies that deleteSnatInPod returns nil when
// the VpcNatGateway CRD no longer exists.
func TestDeleteSnatInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	err = fc.fakeController.deleteSnatInPod("missing-gw", "10.0.0.1", "192.168.1.0/24")
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDeleteSnatInPod_NatGwNoRunningInstance verifies that deleteSnatInPod skips the cleanup
// when the gateway has no running instance.
func TestDeleteSnatInPod_NatGwNoRunningInstance(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteSnatInPod("test-gw", "10.0.0.1", "192.168.1.0/24")
	require.NoError(t, err, "should skip cleanup when the gateway has no running instance")
}

// shareDnat builds an IptablesDnatRule with the identity labels that getShareBackends
// and isDnatDuplicated select on. dnatType may be "" (defaults to exclusive behavior).
func shareDnat(name, gw, eip, eport, proto, intIP, intPort, dnatType string) *kubeovnv1.IptablesDnatRule {
	return &kubeovnv1.IptablesDnatRule{
		Name: name,
		Labels: map[string]string{
			util.VpcNatGatewayNameLabel: gw,
			util.VpcDnatEPortLabel:      eport,
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP:          eip,
			ExternalPort: eport,
			Protocol:     proto,
			InternalIP:   intIP,
			InternalPort: intPort,
			Type:         dnatType,
		},
	}
}

// dnatListerController returns a Controller whose iptablesDnatRulesLister is backed by the
// given objects, so lister-based helpers can be unit tested without a running informer.
func dnatListerController(t *testing.T, dnats ...*kubeovnv1.IptablesDnatRule) *Controller {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, d := range dnats {
		require.NoError(t, indexer.Add(d))
	}
	return &Controller{iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(indexer)}
}

func TestDedupSortedBackends(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty entries dropped", in: []string{"", ""}, want: nil},
		{
			name: "dedup and sort",
			in:   []string{"10.0.0.2:80", "10.0.0.1:80", "10.0.0.2:80", "", "10.0.0.3:80"},
			want: []string{"10.0.0.1:80", "10.0.0.2:80", "10.0.0.3:80"},
		},
		{
			name: "already unique stays sorted",
			in:   []string{"10.0.0.1:80", "10.0.0.2:80"},
			want: []string{"10.0.0.1:80", "10.0.0.2:80"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, dedupSortedBackends(tt.in))
		})
	}
}

func TestIsDnatDuplicated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing *kubeovnv1.IptablesDnatRule
		wantDup  bool
	}{
		{
			name:     "same exclusive identity is duplicate",
			existing: shareDnat("other", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeExclusive),
			wantDup:  true,
		},
		{
			name:     "different protocol is not duplicate",
			existing: shareDnat("other", "gw", "eip", "80", "udp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeExclusive),
			wantDup:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := dnatListerController(t, tt.existing)
			// The incoming rule is hand-managed unless the case labels it, which is what decides
			// whether it may share an identity with the existing one.
			newRule := &kubeovnv1.IptablesDnatRule{
				Name: "new",
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP: "eip", ExternalPort: "80", Protocol: "tcp",
					InternalIP: "10.0.0.9", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeExclusive,
				},
			}
			dup, err := c.isDnatDuplicated("gw", newRule)
			assert.Equal(t, tt.wantDup, dup)
			if tt.wantDup {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// assertEnqueueAddRouting checks that the add handler routes a live object to the add queue and a
// terminating object to the update queue (the deletion-cleanup path relied on after a restart).
func assertEnqueueAddRouting(
	t *testing.T,
	addQueue, updateQueue workqueue.TypedRateLimitingInterface[string],
	enqueue func(any),
	live, terminating any,
) {
	t.Helper()
	enqueue(live)
	require.Equal(t, 1, addQueue.Len(), "live object should go to the add queue")
	require.Equal(t, 0, updateQueue.Len(), "live object must not go to the update queue")

	enqueue(terminating)
	require.Equal(t, 1, addQueue.Len(), "terminating object must not go to the add queue")
	require.Equal(t, 1, updateQueue.Len(), "terminating object should go to the update queue for cleanup")
}

func TestEnqueueAddIptablesFip(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addIptablesFipQueue:    newTypedRateLimitingQueue[string]("AddIptablesFip", nil),
		updateIptablesFipQueue: newTypedRateLimitingQueue[string]("UpdateIptablesFip", nil),
	}
	t.Cleanup(c.addIptablesFipQueue.ShutDown)
	t.Cleanup(c.updateIptablesFipQueue.ShutDown)
	now := metav1.Now()
	assertEnqueueAddRouting(t, c.addIptablesFipQueue, c.updateIptablesFipQueue, c.enqueueAddIptablesFip,
		&kubeovnv1.IptablesFIPRule{Name: "live-fip"},
		&kubeovnv1.IptablesFIPRule{Name: "terminating-fip", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddIptablesDnatRule(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addIptablesDnatRuleQueue:    newTypedRateLimitingQueue[string]("AddIptablesDnat", nil),
		updateIptablesDnatRuleQueue: newTypedRateLimitingQueue[string]("UpdateIptablesDnat", nil),
	}
	t.Cleanup(c.addIptablesDnatRuleQueue.ShutDown)
	t.Cleanup(c.updateIptablesDnatRuleQueue.ShutDown)
	now := metav1.Now()
	assertEnqueueAddRouting(t, c.addIptablesDnatRuleQueue, c.updateIptablesDnatRuleQueue, c.enqueueAddIptablesDnatRule,
		&kubeovnv1.IptablesDnatRule{Name: "live-dnat"},
		&kubeovnv1.IptablesDnatRule{Name: "terminating-dnat", DeletionTimestamp: &now},
	)
}

func TestNftableLbRecordEventsEnqueueNothing(t *testing.T) {
	t.Parallel()
	addQueue := newTypedRateLimitingQueue[string]("AddIptablesDnat", nil)
	updateQueue := newTypedRateLimitingQueue[string]("UpdateIptablesDnat", nil)
	deleteQueue := newTypedRateLimitingQueue[string]("DelIptablesDnat", nil)
	serviceQueue := newTypedRateLimitingQueue[string]("AddOrUpdateNftableLbSvc", nil)
	t.Cleanup(addQueue.ShutDown)
	t.Cleanup(updateQueue.ShutDown)
	t.Cleanup(deleteQueue.ShutDown)
	t.Cleanup(serviceQueue.ShutDown)
	c := &Controller{
		addIptablesDnatRuleQueue:    addQueue,
		updateIptablesDnatRuleQueue: updateQueue, delIptablesDnatRuleQueue: deleteQueue,
		addOrUpdateNftableLbSvcQueue: serviceQueue, config: &Configuration{EnableGwNftableLbSvc: true},
	}
	record := &kubeovnv1.IptablesDnatRule{Name: "record", Labels: map[string]string{
		util.NftableLbSvcRecordLabel: "true", util.NftableLbSvcNsLabel: "ns1", util.NftableLbSvcNameLabel: "svc1",
	}, Spec: kubeovnv1.IptablesDnatRuleSpec{Type: kubeovnv1.DnatRuleTypeShare}}
	ready := record.DeepCopy()
	ready.Status.Ready = true
	c.enqueueAddIptablesDnatRule(record)
	c.enqueueUpdateIptablesDnatRule(record, ready)
	require.Zero(t, addQueue.Len())
	c.enqueueDelIptablesDnatRule(record)
	require.Zero(t, updateQueue.Len())
	require.Zero(t, deleteQueue.Len())
	require.Zero(t, serviceQueue.Len())

	c.enqueueDelIptablesDnatRule(&kubeovnv1.IptablesDnatRule{Name: "manual"})
	require.Equal(t, 1, deleteQueue.Len(), "exclusive DNAT keeps the ordinary writer")
}

func TestEnqueueAddIptablesSnatRule(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addIptablesSnatRuleQueue:    newTypedRateLimitingQueue[string]("AddIptablesSnat", nil),
		updateIptablesSnatRuleQueue: newTypedRateLimitingQueue[string]("UpdateIptablesSnat", nil),
	}
	t.Cleanup(c.addIptablesSnatRuleQueue.ShutDown)
	t.Cleanup(c.updateIptablesSnatRuleQueue.ShutDown)
	now := metav1.Now()
	assertEnqueueAddRouting(t, c.addIptablesSnatRuleQueue, c.updateIptablesSnatRuleQueue, c.enqueueAddIptablesSnatRule,
		&kubeovnv1.IptablesSnatRule{Name: "live-snat"},
		&kubeovnv1.IptablesSnatRule{Name: "terminating-snat", DeletionTimestamp: &now},
	)
}

// Test_resolveDnatAddress pins what each rule shape resolves to: the EIP gives the gateway, the
// IPv4 address the identity is programmed with and the IPv6 address recorded in the rule status
// (the CRD prints it), while a rule that serves a ClusterIP has neither an EIP to derive them from
// nor an IPv6 address to record.
func Test_resolveDnatAddress(t *testing.T) {
	t.Parallel()

	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: "gw0", V4ip: "192.0.2.10", V6ip: "fd00::10"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "192.0.2.10"},
	}))
	c := &Controller{iptablesEipsLister: kubeovnlister.NewIptablesEIPLister(eipIndexer)}

	gwName, v4ip, v6ip, err := c.resolveDnatAddress(&kubeovnv1.IptablesDnatRule{
		Name: "eip-rule",
		Spec: kubeovnv1.IptablesDnatRuleSpec{EIP: "eip0", ExternalPort: "80", Protocol: "tcp"},
	})
	require.NoError(t, err)
	require.Equal(t, "gw0", gwName)
	require.Equal(t, "192.0.2.10", v4ip)
	require.Equal(t, "fd00::10", v6ip, "the IPv6 address of the EIP is recorded in the rule status")

	// A LoadBalancer Service rule carries the gateway as well, and it must agree with the EIP's.
	gwName, v4ip, v6ip, err = c.resolveDnatAddress(&kubeovnv1.IptablesDnatRule{
		Name: "lb-rule",
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip0", ClusterIP: "10.96.1.5", VpcNatGwDp: "gw0",
			ExternalPort: "80", Protocol: "tcp", Type: kubeovnv1.DnatRuleTypeShare,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "gw0", gwName)
	require.Equal(t, "192.0.2.10", v4ip, "the programmed address is the public one")
	require.Equal(t, "fd00::10", v6ip)

	// A ClusterIP Service rule: its own address is the only one it has.
	gwName, v4ip, v6ip, err = c.resolveDnatAddress(&kubeovnv1.IptablesDnatRule{
		Name: "clusterip-rule",
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			ClusterIP: "10.96.1.5", VpcNatGwDp: "gw0",
			ExternalPort: "80", Protocol: "tcp", Type: kubeovnv1.DnatRuleTypeShare,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "gw0", gwName)
	require.Equal(t, "10.96.1.5", v4ip)
	require.Empty(t, v6ip)

	// An EIP that does not exist cannot be resolved at all.
	_, _, _, err = c.resolveDnatAddress(&kubeovnv1.IptablesDnatRule{
		Name: "missing",
		Spec: kubeovnv1.IptablesDnatRuleSpec{EIP: "absent", ExternalPort: "80", Protocol: "tcp"},
	})
	require.Error(t, err)
}

// Test_dnatNeedsSpecCleanup pins the state a crashed spec change leaves behind, for every rule shape:
// the data plane may hold an identity the status does not point at any more, so both have to be
// cleaned up. A rule that is ready, or has no programmed identity yet, is not in that state.
func Test_dnatNeedsSpecCleanup(t *testing.T) {
	t.Parallel()

	eipOnly := &kubeovnv1.IptablesDnatRule{Spec: kubeovnv1.IptablesDnatRuleSpec{EIP: "eip0", ExternalPort: "80", Protocol: "tcp", Type: kubeovnv1.DnatRuleTypeShare}}
	clusterIPServed := &kubeovnv1.IptablesDnatRule{Spec: kubeovnv1.IptablesDnatRuleSpec{
		ClusterIP: "10.96.1.5", VpcNatGwDp: "gw0", ExternalPort: "80", Protocol: "tcp", Type: kubeovnv1.DnatRuleTypeShare,
	}}

	// A crashed spec change: the status points at what the data plane was programmed with. Only a
	// rule that resolves an EIP can be in this state, because that divergent identity is looked up
	// through the EIP; a ClusterIP rule records no second identity in its status.
	for _, rule := range []*kubeovnv1.IptablesDnatRule{eipOnly, clusterIPServed} {
		rule.Status = kubeovnv1.IptablesDnatRuleStatus{V4ip: "192.0.2.10", Ready: false}
		if dnatUsesEip(&rule.Spec) {
			require.True(t, dnatNeedsSpecCleanup(rule), "a not-ready EIP rule with a programmed identity needs both cleanups")
		} else {
			require.False(t, dnatNeedsSpecCleanup(rule), "a rule without an EIP has no diverging EIP identity to clean")
		}
		rule.Status.Ready = true
		require.False(t, dnatNeedsSpecCleanup(rule), "a ready rule has no diverging identity")
		rule.Status = kubeovnv1.IptablesDnatRuleStatus{}
		require.False(t, dnatNeedsSpecCleanup(rule), "a rule that never programmed an identity has none to clean")
	}
}

// Test_clusterIPDnatRedoNeeded pins the branch matrix behind "is the data plane of a ClusterIP rule
// restored after its gateway instance was replaced": the redo replays the status, so it needs a
// gateway and a complete identity there, and it must not run for a rule that is already ready or for
// one whose add handler never finished (recovering that is the add handler's job).
func TestEnqueueUpdateIptablesDnatRuleSkipsShareRecords(t *testing.T) {
	t.Parallel()

	newDnat := func(spec kubeovnv1.IptablesDnatRuleSpec) *kubeovnv1.IptablesDnatRule {
		return &kubeovnv1.IptablesDnatRule{Name: "rule", Spec: spec}
	}
	clusterIPServed := kubeovnv1.IptablesDnatRuleSpec{
		ClusterIP: "10.96.1.5", VpcNatGwDp: "gw0", ExternalPort: "80", Protocol: "tcp",
		InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
	}
	eipServed := clusterIPServed
	eipServed.ClusterIP, eipServed.EIP = "", "eip0"

	tests := []struct {
		name     string
		old, new *kubeovnv1.IptablesDnatRule
		enqueued bool
	}{
		{
			name: "a ClusterIP record whose redo token changed is skipped",
			old:  newDnat(clusterIPServed),
			new:  func() *kubeovnv1.IptablesDnatRule { d := newDnat(clusterIPServed); d.Status.Redo = "new"; return d }(),
		},
		{
			name: "an EIP record whose redo token changed is skipped",
			old:  newDnat(eipServed),
			new:  func() *kubeovnv1.IptablesDnatRule { d := newDnat(eipServed); d.Status.Redo = "new"; return d }(),
		},
		{
			name: "a record whose gateway label became visible is skipped",
			old:  newDnat(clusterIPServed),
			new: func() *kubeovnv1.IptablesDnatRule {
				d := newDnat(clusterIPServed)
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw0"}
				return d
			}(),
		},
		{
			name:     "a rule with neither address is still rejected",
			old:      newDnat(kubeovnv1.IptablesDnatRuleSpec{ExternalPort: "80", Protocol: "tcp", InternalIP: "10.0.7.2", InternalPort: "8080"}),
			new:      newDnat(kubeovnv1.IptablesDnatRuleSpec{ExternalPort: "80", Protocol: "tcp", InternalIP: "10.0.7.2", InternalPort: "8080"}),
			enqueued: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := newTypedRateLimitingQueue[string]("UpdateIptablesDnatRule", nil)
			t.Cleanup(queue.ShutDown)
			c := &Controller{updateIptablesDnatRuleQueue: queue, config: &Configuration{}}
			c.enqueueUpdateIptablesDnatRule(tt.old, tt.new)
			if tt.enqueued {
				require.Equal(t, 1, c.updateIptablesDnatRuleQueue.Len())
			} else {
				require.Zero(t, c.updateIptablesDnatRuleQueue.Len())
			}
		})
	}
}

func TestDnatRecordReadyUpdateEnqueuesNothing(t *testing.T) {
	t.Parallel()
	record := clusterIPServedRule("record", "gw0", "10.96.1.5")
	record.Labels[util.NftableLbSvcRecordLabel] = "true"
	record.Labels[util.NftableLbSvcNsLabel] = "default"
	record.Labels[util.NftableLbSvcNameLabel] = "web"
	ready := record.DeepCopy()
	ready.Status.Ready = true
	serviceQueue := newTypedRateLimitingQueue[string]("NftableLbService", nil)
	updateQueue := newTypedRateLimitingQueue[string]("UpdateIptablesDnatRule", nil)
	t.Cleanup(serviceQueue.ShutDown)
	t.Cleanup(updateQueue.ShutDown)
	c := &Controller{
		config: &Configuration{EnableGwNftableLbSvc: true}, addOrUpdateNftableLbSvcQueue: serviceQueue,
		updateIptablesDnatRuleQueue: updateQueue,
	}
	c.enqueueUpdateIptablesDnatRule(record, ready)
	require.Zero(t, serviceQueue.Len())
	require.Zero(t, updateQueue.Len())
}

// Test_isDnatDuplicatedRejectsDifferentOwners allows one Service's backends or hand-managed share
// rules to aggregate, but rejects different owners that escaped admission validation.
