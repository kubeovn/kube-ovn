package controller

import (
	"context"
	"testing"
	"time"

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

// TestDeleteFipInPod_NatGwExistsPodMissing verifies that deleteFipInPod returns
// an error to trigger a retry when the gateway CRD exists but the pod is absent.
func TestDeleteFipInPod_NatGwExistsPodMissing(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteFipInPod("test-gw", "10.0.0.1")
	require.Error(t, err, "should return error to retry when pod is temporarily absent")
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

// TestDeleteDnatInPod_NatGwExistsPodMissing verifies that deleteDnatInPod
// returns an error to trigger a retry when the pod is absent.
func TestDeleteDnatInPod_NatGwExistsPodMissing(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteDnatInPod("test-gw", "tcp", "10.0.0.1", "80")
	require.Error(t, err, "should return error to retry when pod is temporarily absent")
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

// TestDeleteSnatInPod_NatGwExistsPodMissing verifies that deleteSnatInPod
// returns an error to trigger a retry when the pod is absent.
func TestDeleteSnatInPod_NatGwExistsPodMissing(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteSnatInPod("test-gw", "10.0.0.1", "192.168.1.0/24")
	require.Error(t, err, "should return error to retry when pod is temporarily absent")
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

func TestGetShareBackends(t *testing.T) {
	t.Parallel()

	deleting := shareDnat("deleting", "gw", "eip", "80", "tcp", "10.0.0.5", "8080", kubeovnv1.DnatRuleTypeShare)
	deleting.DeletionTimestamp = &metav1.Time{Time: time.Unix(1, 0)}

	c := dnatListerController(t,
		shareDnat("self", "gw", "eip", "80", "tcp", "10.0.0.9", "8080", kubeovnv1.DnatRuleTypeShare),
		shareDnat("d1", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeShare),
		shareDnat("d2", "gw", "eip", "80", "tcp", "10.0.0.2", "8080", kubeovnv1.DnatRuleTypeShare),
		shareDnat("exclusive", "gw", "eip", "80", "tcp", "10.0.0.3", "8080", kubeovnv1.DnatRuleTypeExclusive),
		shareDnat("other-proto", "gw", "eip", "80", "udp", "10.0.0.4", "8080", kubeovnv1.DnatRuleTypeShare),
		shareDnat("other-eip", "gw", "eip2", "80", "tcp", "10.0.0.6", "8080", kubeovnv1.DnatRuleTypeShare),
		shareDnat("incomplete", "gw", "eip", "80", "tcp", "", "8080", kubeovnv1.DnatRuleTypeShare),
		deleting,
	)

	self := shareDnat("self", "gw", "eip", "80", "tcp", "10.0.0.9", "8080", kubeovnv1.DnatRuleTypeShare)
	backends, affinity, affinityTimeout, err := c.getShareBackends("gw", self, "80", "tcp")
	require.NoError(t, err)
	// Self is excluded; only ready share siblings with the same identity are returned.
	// Exclusive, other protocol/eip, incomplete spec and deleting rules are filtered out.
	assert.ElementsMatch(t, []string{"10.0.0.1:8080", "10.0.0.2:8080"}, backends)
	assert.Empty(t, affinity)
	assert.Zero(t, affinityTimeout)
}

func TestGetShareBackendsUsesLiveSiblingAffinity(t *testing.T) {
	t.Parallel()

	live := shareDnat("live", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeShare)
	live.Spec.SessionAffinity = kubeovnv1.DnatSessionAffinityClientIP
	live.Spec.SessionAffinityTimeoutSeconds = 600
	deleting := shareDnat("deleting", "gw", "eip", "80", "tcp", "10.0.0.9", "8080", kubeovnv1.DnatRuleTypeShare)
	deleting.DeletionTimestamp = &metav1.Time{Time: time.Unix(1, 0)}

	c := dnatListerController(t, live, deleting)
	backends, affinity, affinityTimeout, err := c.getShareBackends("gw", deleting, "80", "tcp")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"10.0.0.1:8080"}, backends)
	assert.Equal(t, kubeovnv1.DnatSessionAffinityClientIP, affinity)
	assert.Equal(t, int32(600), affinityTimeout)
}

func TestIsDnatDuplicated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing *kubeovnv1.IptablesDnatRule
		newType  string
		wantDup  bool
	}{
		{
			name:     "exclusive vs existing exclusive is duplicate",
			existing: shareDnat("other", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeExclusive),
			newType:  kubeovnv1.DnatRuleTypeExclusive,
			wantDup:  true,
		},
		{
			name:     "share vs existing share coexist",
			existing: shareDnat("other", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeShare),
			newType:  kubeovnv1.DnatRuleTypeShare,
			wantDup:  false,
		},
		{
			name:     "exclusive vs existing share is duplicate",
			existing: shareDnat("other", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeShare),
			newType:  kubeovnv1.DnatRuleTypeExclusive,
			wantDup:  true,
		},
		{
			name:     "share vs existing exclusive is duplicate",
			existing: shareDnat("other", "gw", "eip", "80", "tcp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeExclusive),
			newType:  kubeovnv1.DnatRuleTypeShare,
			wantDup:  true,
		},
		{
			name:     "different protocol is not duplicate",
			existing: shareDnat("other", "gw", "eip", "80", "udp", "10.0.0.1", "8080", kubeovnv1.DnatRuleTypeExclusive),
			newType:  kubeovnv1.DnatRuleTypeExclusive,
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
					InternalIP: "10.0.0.9", InternalPort: "8080", Type: tt.newType,
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

func TestEnqueueUpdateIptablesDnatRuleNotifiesNftableLbService(t *testing.T) {
	t.Parallel()
	c := &Controller{
		updateIptablesDnatRuleQueue:  newTypedRateLimitingQueue[string]("UpdateIptablesDnat", nil),
		addOrUpdateNftableLbSvcQueue: newTypedRateLimitingQueue[string]("AddOrUpdateNftableLbSvc", nil),
		config:                       &Configuration{EnableGwNftableLbSvc: true},
	}
	t.Cleanup(c.updateIptablesDnatRuleQueue.ShutDown)
	t.Cleanup(c.addOrUpdateNftableLbSvcQueue.ShutDown)

	oldDnat := &kubeovnv1.IptablesDnatRule{
		Name: "lb-default-abc",
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:   "ns1",
			util.NftableLbSvcNameLabel: "svc1",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{Type: kubeovnv1.DnatRuleTypeShare},
	}
	newDnat := oldDnat.DeepCopy()
	newDnat.Spec.SessionAffinity = kubeovnv1.DnatSessionAffinityClientIP

	c.enqueueUpdateIptablesDnatRule(oldDnat, newDnat)
	require.Equal(t, 1, c.addOrUpdateNftableLbSvcQueue.Len(), "out-of-band spec drift must notify the owning nftable LB service")
	item, _ := c.addOrUpdateNftableLbSvcQueue.Get()
	require.Equal(t, "ns1/svc1", item)
	c.addOrUpdateNftableLbSvcQueue.Done(item)
}

func TestEnqueueDelIptablesDnatRuleNotifiesNftableLbService(t *testing.T) {
	t.Parallel()
	c := &Controller{
		delIptablesDnatRuleQueue:     newTypedRateLimitingQueue[string]("DelIptablesDnat", nil),
		addOrUpdateNftableLbSvcQueue: newTypedRateLimitingQueue[string]("AddOrUpdateNftableLbSvc", nil),
		config:                       &Configuration{EnableGwNftableLbSvc: true},
	}
	t.Cleanup(c.delIptablesDnatRuleQueue.ShutDown)
	t.Cleanup(c.addOrUpdateNftableLbSvcQueue.ShutDown)

	owned := &kubeovnv1.IptablesDnatRule{
		Name: "lb-default-abc",
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:   "ns1",
			util.NftableLbSvcNameLabel: "svc1",
		},
	}
	c.enqueueDelIptablesDnatRule(owned)
	require.Equal(t, 1, c.delIptablesDnatRuleQueue.Len())
	require.Equal(t, 1, c.addOrUpdateNftableLbSvcQueue.Len(), "deleting a generated rule must notify its owning service")
	item, _ := c.addOrUpdateNftableLbSvcQueue.Get()
	require.Equal(t, "ns1/svc1", item)
	c.addOrUpdateNftableLbSvcQueue.Done(item)

	c.enqueueDelIptablesDnatRule(&kubeovnv1.IptablesDnatRule{Name: "manual"})
	require.Equal(t, 2, c.delIptablesDnatRuleQueue.Len())
	require.Equal(t, 0, c.addOrUpdateNftableLbSvcQueue.Len(), "manual rules must not trigger nftable LB service reconciliation")
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
func Test_clusterIPDnatRedoNeeded(t *testing.T) {
	t.Parallel()

	complete := kubeovnv1.IptablesDnatRuleStatus{
		NatGwDp: "gw0", V4ip: "10.96.1.5", Protocol: "tcp", ExternalPort: "80",
	}
	base := func() *kubeovnv1.IptablesDnatRule {
		return &kubeovnv1.IptablesDnatRule{
			Name: "clusterip-rule",
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				ClusterIP: "10.96.1.5", VpcNatGwDp: "gw0", ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
			Status: complete,
		}
	}

	tests := []struct {
		name   string
		mutate func(*kubeovnv1.IptablesDnatRule)
		want   bool
	}{
		{name: "not ready with a complete identity", want: true},
		{name: "already ready", mutate: func(d *kubeovnv1.IptablesDnatRule) { d.Status.Ready = true }},
		{name: "no gateway in the status", mutate: func(d *kubeovnv1.IptablesDnatRule) { d.Status.NatGwDp = "" }},
		{name: "no programmed address", mutate: func(d *kubeovnv1.IptablesDnatRule) { d.Status.V4ip = "" }},
		{name: "no protocol", mutate: func(d *kubeovnv1.IptablesDnatRule) { d.Status.Protocol = "" }},
		{name: "no external port", mutate: func(d *kubeovnv1.IptablesDnatRule) { d.Status.ExternalPort = "" }},
		{name: "add handler never finished", mutate: func(d *kubeovnv1.IptablesDnatRule) { d.Status = kubeovnv1.IptablesDnatRuleStatus{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dnat := base()
			if tt.mutate != nil {
				tt.mutate(dnat)
			}
			require.Equal(t, tt.want, clusterIPDnatRedoNeeded(dnat))
		})
	}
}

// TestEnqueueUpdateIptablesDnatRuleAcceptsClusterIPServedRules pins the redo's only entry point: a
// gateway instance being replaced makes redoDnat patch the rule's status, and the update event it
// produces is enqueued here. A rule that serves a ClusterIP has no EIP, so rejecting an EIP-less
// rule would leave its identity, hairpin rule and lo address unprogrammed on the new instance.
func TestEnqueueUpdateIptablesDnatRuleAcceptsClusterIPServedRules(t *testing.T) {
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
			name:     "a ClusterIP rule whose redo token changed is enqueued",
			old:      newDnat(clusterIPServed),
			new:      func() *kubeovnv1.IptablesDnatRule { d := newDnat(clusterIPServed); d.Status.Redo = "new"; return d }(),
			enqueued: true,
		},
		{
			name:     "an EIP rule whose redo token changed is enqueued",
			old:      newDnat(eipServed),
			new:      func() *kubeovnv1.IptablesDnatRule { d := newDnat(eipServed); d.Status.Redo = "new"; return d }(),
			enqueued: true,
		},
		{
			name: "a rule whose gateway label became visible is enqueued",
			old:  newDnat(clusterIPServed),
			new: func() *kubeovnv1.IptablesDnatRule {
				d := newDnat(clusterIPServed)
				d.Labels = map[string]string{util.VpcNatGatewayNameLabel: "gw0"}
				return d
			}(),
			enqueued: true,
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

func TestSyncNatGwVipStateFromCacheDisabled(t *testing.T) {
	t.Parallel()

	c := &Controller{config: &Configuration{EnableGwNftableLbSvc: false}}
	require.NoError(t, c.syncNatGwVipStateFromCache("gw0"),
		"a disabled feature must stop before resolving gateway pods")
}

func TestDnatReadyUpdateEnqueuesOwningService(t *testing.T) {
	t.Parallel()

	oldRule := clusterIPServedRule("rule", "gw0", "10.96.1.5")
	oldRule.Labels[util.NftableLbSvcNsLabel] = "default"
	oldRule.Labels[util.NftableLbSvcNameLabel] = "web"
	newRule := oldRule.DeepCopy()
	newRule.Status.Ready = true

	serviceQueue := newTypedRateLimitingQueue[string]("NftableLbService", nil)
	updateQueue := newTypedRateLimitingQueue[string]("UpdateIptablesDnatRule", nil)
	t.Cleanup(serviceQueue.ShutDown)
	t.Cleanup(updateQueue.ShutDown)
	c := &Controller{
		config:                       &Configuration{EnableGwNftableLbSvc: true},
		addOrUpdateNftableLbSvcQueue: serviceQueue,
		updateIptablesDnatRuleQueue:  updateQueue,
	}
	c.enqueueUpdateIptablesDnatRule(oldRule, newRule)
	require.Equal(t, 1, serviceQueue.Len())
	require.Zero(t, updateQueue.Len(), "Ready only completes the Service ownership handoff")
}

// Test_isDnatDuplicatedSeparatesOwners pins the identity ownership the controller has to enforce by
// itself when admission could not see the other rule: two share rules may share an nft map only when
// they belong to the same owner (the backends of one Service, or several hand-managed rules). A
// dual-address rule contributes two identities, so a collision on either address counts.
func Test_isDnatDuplicatedAllowsOwnersToShareIdentities(t *testing.T) {
	t.Parallel()

	const gwName = "gw"
	owned := func(name, eip, clusterIP, ns, svc string) *kubeovnv1.IptablesDnatRule {
		rule := &kubeovnv1.IptablesDnatRule{
			Name: name,
			Labels: map[string]string{
				util.VpcNatGatewayNameLabel: gwName,
				util.VpcDnatEPortLabel:      "80",
			},
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: eip, ClusterIP: clusterIP, ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.0.9", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		}
		if ns != "" {
			rule.Labels[util.NftableLbSvcNsLabel] = ns
			rule.Labels[util.NftableLbSvcNameLabel] = svc
		}
		return rule
	}

	tests := []struct {
		name     string
		existing *kubeovnv1.IptablesDnatRule
		incoming *kubeovnv1.IptablesDnatRule
		wantDup  bool
	}{
		{
			// Allowed rather than rejected: rejecting it would block the loser's reconcile, and after
			// a redo neither side would have a data plane (only the winner writes the identity).
			name:     "another owner sharing the internal VIP is left to the winner",
			existing: owned("a", "eip-a", "10.96.1.5", "default", "web"),
			incoming: owned("b", "eip-b", "10.96.1.5", "", ""),
			wantDup:  false,
		},
		{
			name:     "another owner sharing the EIP is left to the winner",
			existing: owned("a", "eip-a", "10.96.1.5", "default", "web"),
			incoming: owned("b", "eip-a", "10.96.1.6", "", ""),
			wantDup:  false,
		},
		{
			name:     "an exclusive rule still conflicts with a share rule",
			existing: owned("a", "eip-a", "10.96.1.5", "", ""),
			incoming: func() *kubeovnv1.IptablesDnatRule {
				rule := owned("b", "eip-a", "10.96.1.5", "", "")
				rule.Spec.Type = kubeovnv1.DnatRuleTypeExclusive
				return rule
			}(),
			wantDup: true,
		},
		{
			name:     "the backends of one Service share their identities",
			existing: owned("a", "eip-a", "10.96.1.5", "default", "web"),
			incoming: owned("b", "eip-a", "10.96.1.5", "default", "web"),
			wantDup:  false,
		},
		{
			name:     "hand-managed rules keep adding backends to one address",
			existing: owned("a", "eip-a", "10.96.1.5", "", ""),
			incoming: owned("b", "eip-a", "10.96.1.5", "", ""),
			wantDup:  false,
		},
		{
			name:     "hand-managed rules with their own internal VIP stay independent",
			existing: owned("a", "eip-a", "10.96.1.5", "", ""),
			incoming: owned("b", "eip-a", "10.96.1.6", "", ""),
			wantDup:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := dnatListerController(t, tt.existing)
			dup, err := c.isDnatDuplicated(gwName, tt.incoming)
			require.Equal(t, tt.wantDup, dup)
			if tt.wantDup {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test_identityStillServedByAnotherOwnerEip pins the other half of the teardown guard: two rules of
// different owners can share the EIP identity while serving different internal VIPs, and releasing
// the EIP map of the one being deleted would take the other's data plane with it.
func Test_identityStillServedByAnotherOwnerEip(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	svcRule := ownedShareRule("svc-rule", "eip-a", "10.96.1.5")
	manual := clusterIPServedRule("manual-rule", gwName, "10.96.1.6")
	manual.Spec.EIP = "eip-a"
	c := dnatListerController(t, svcRule, manual)

	served, err := c.identityStillServedByAnotherOwner(gwName, svcRule, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.True(t, served, "the hand-managed rule still programs the shared EIP identity")

	served, err = c.identityStillServedByAnotherOwner(gwName, manual, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.True(t, served, "and the Service rule still programs it for the hand-managed one")

	// Their internal VIPs are separate identities and are not served by the other.
	served, err = c.identityStillServedByAnotherOwner(gwName, svcRule, "tcp", "80", "", "10.96.1.5")
	require.NoError(t, err)
	require.False(t, served)
	served, err = c.identityStillServedByAnotherOwner(gwName, manual, "tcp", "80", "", "10.96.1.6")
	require.NoError(t, err)
	require.False(t, served)
}

// Test_shareIdentityServedByOtherOwner pins the teardown guard: the identity of an internal VIP is
// only released when no live rule of another owner still programs it, because releasing it would take
// that owner's data plane with it.
func Test_shareIdentityServedByOtherOwner(t *testing.T) {
	t.Parallel()

	// ownedShareRule labels its rule with this gateway, so both fixtures have to use it.
	const gwName = "gw0"
	svcRule := ownedShareRule("svc-rule", "eip-a", "10.96.1.5")
	manual := clusterIPServedRule("manual-rule", gwName, "10.96.1.5")
	c := dnatListerController(t, svcRule, manual)

	served, err := c.identityStillServedByAnotherOwner(gwName, svcRule, "tcp", "80", "", "10.96.1.5")
	require.NoError(t, err)
	require.True(t, served, "the hand-managed rule still programs that identity")

	// Deleting the hand-managed rule sees the Service rule as another owner too.
	served, err = c.identityStillServedByAnotherOwner(gwName, manual, "tcp", "80", "", "10.96.1.5")
	require.NoError(t, err)
	require.True(t, served)

	// Its own siblings are not another owner.
	served, err = c.identityStillServedByAnotherOwner(gwName, svcRule, "tcp", "80", "", "10.96.1.9")
	require.NoError(t, err)
	require.False(t, served)
}

// Test_releasedHairpinRules pins which hairpin SNAT rules a torn down rule may delete: only the ones
// whose identity was actually released. The two identities of a rule are independent, so a kept one
// (another owner still programs that map) must keep its hairpin -- otherwise the surviving rule
// forwards traffic whose return path is no longer pinned to the instance holding the conntrack.
func Test_releasedHairpinRules(t *testing.T) {
	t.Parallel()

	const eip, clusterIP = "172.20.0.5", "10.96.1.5"
	require.Equal(t,
		[]string{"172.20.0.5,80,tcp"},
		releasedHairpinRules("tcp", "80", map[string]bool{eip: true, clusterIP: false}),
		"a kept identity must not lose its hairpin")

	require.Equal(t,
		[]string{"10.96.1.5,80,tcp"},
		releasedHairpinRules("tcp", "80", map[string]bool{eip: false, clusterIP: true}),
		"the independent internal VIP is released on its own")

	require.Empty(t, releasedHairpinRules("tcp", "80", map[string]bool{eip: false, clusterIP: false}),
		"nothing released, nothing deleted")

	require.Equal(t,
		[]string{"10.96.1.5,80,tcp", "172.20.0.5,80,tcp"},
		releasedHairpinRules("tcp", "80", map[string]bool{eip: true, clusterIP: true}),
		"both identities released, both hairpins deleted")
}

// Test_cleanupShareDnatIdentityDecisionsAreIndependent pins the inputs the teardown decides with when a
// rule's two identities are not in the same state, which is what a conflict with another owner
// produces: the rule's own surviving backend is what both identities have to be rebuilt with, the
// shared EIP identity is left to its other owner, and the rule's independent internal VIP is still
// rebuilt so it cannot keep forwarding to the backend that went away.
func Test_cleanupShareDnatIdentityDecisionsAreIndependent(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	// A1 and A2 are the backends of one Service; B is a hand-managed rule of another owner sharing
	// the EIP identity only.
	a1 := ownedShareRule("a1", "eip-a", "10.96.1.5")
	a1.Spec.InternalIP, a1.Spec.InternalPort = "10.0.7.2", "8080"
	a2 := ownedShareRule("a2", "eip-a", "10.96.1.5")
	a2.Spec.InternalIP, a2.Spec.InternalPort = "10.0.7.3", "8080"
	b := clusterIPServedRule("b", gwName, "10.96.1.6")
	b.Spec.EIP = "eip-a"
	b.Spec.InternalIP, b.Spec.InternalPort = "10.0.7.9", "8080"
	c := dnatListerController(t, a1, a2, b)

	// The EIP identity keeps the sibling's backend and is shared with another owner, so it is left
	// to that owner rather than overwritten.
	eipBackends, _, _, err := c.getShareBackends(gwName, a1, "80", "tcp")
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.7.3:8080"}, eipBackends)
	served, err := c.identityStillServedByAnotherOwner(gwName, a1, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.True(t, served, "the hand-managed rule of another owner shares the EIP identity")

	// The internal VIP is the Service's own: it is not shared, and it is rebuilt with the surviving
	// backend of the same owner.
	clusterIPBackends, _, _, err := c.shareClusterIPBackends(gwName, a1, "80", "tcp", "10.96.1.5")
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.7.3:8080"}, clusterIPBackends)
	served, err = c.identityStillServedByAnotherOwner(gwName, a1, "tcp", "80", "", "10.96.1.5")
	require.NoError(t, err)
	require.False(t, served, "the internal VIP has no other owner, so it still converges")
}

// Test_dnatIdentityWinnerWithCacheLag covers the normal creation of the first rule of an identity:
// its gateway label is patched just before the apply, so the informer may not list it yet. The rule
// being reconciled is a candidate by itself, which both keeps it from being mistaken for a loser and
// keeps the winner an owner rather than a nil pointer.
func Test_dnatIdentityWinnerWithCacheLag(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	rule := ownedShareRule("first-rule", "eip-a", "10.96.1.5")
	c := dnatListerController(t)

	won, err := c.dnatIdentityWinnerIs(gwName, rule, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.True(t, won, "the only rule of an identity owns it, even before the informer sees it")

	// A rule that has not been through the lister at all still loses to a hand-managed rule that is
	// already cached for the same identity.
	manual := clusterIPServedRule("manual-rule", gwName, "10.96.1.6")
	manual.Spec.EIP = "eip-a"
	c = dnatListerController(t, manual)
	won, err = c.dnatIdentityWinnerIs(gwName, rule, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.False(t, won)
	won, err = c.dnatIdentityWinnerIs(gwName, manual, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.True(t, won)
}

// Test_dnatIdentityWinnerIsPerOwner pins that the winner is an owner and not one of its rules: a
// backend added to a Service is a new rule whose name may sort after the existing one, and it still
// has to be able to write the identity with all of that owner's backends.
func Test_dnatIdentityWinnerIsPerOwner(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	first := ownedShareRule("svc-a", "eip-a", "10.96.1.5")
	second := ownedShareRule("svc-z", "eip-a", "10.96.1.5")
	second.Spec.InternalIP, second.Spec.InternalPort = "10.0.7.3", "8080"
	c := dnatListerController(t, first, second)

	for _, rule := range []*kubeovnv1.IptablesDnatRule{first, second} {
		won, err := c.dnatIdentityWinnerIs(gwName, rule, "tcp", "80", "eip-a", "")
		require.NoError(t, err)
		require.True(t, won, "every rule of the winning owner may write its identity")
	}
	// The full backend set of the owner (its own rule included), which is what its reconcile writes.
	backends, _, _, err := c.handOverBackends(gwName, second, "80", "tcp", "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"10.0.7.2:8080", "10.0.7.3:8080"}, backends)
}

// Test_handOverShareIdentityPicksTheOwner pins which owner takes an identity over when the last rule
// of the current owner goes away: the winner among the remaining rules, whose own reconcile is never
// triggered by this teardown.
func Test_handOverShareIdentityPicksTheOwner(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	svcRule := ownedShareRule("svc-rule", "eip-a", "10.96.1.5")
	manual := clusterIPServedRule("manual-rule", gwName, "10.96.1.6")
	manual.Spec.EIP = "eip-a"
	c := dnatListerController(t, manual)

	rules, err := c.shareIdentityRules(gwName, "tcp", "80", "eip-a", "", nil)
	require.NoError(t, err)
	winner := dnatIdentityWinnerRule(rules)
	require.NotNil(t, winner)
	require.False(t, sameShareOwner(winner, svcRule), "a different owner takes it over")
	backends, _, _, err := c.handOverBackends(gwName, winner, "80", "tcp", "")
	require.NoError(t, err)
	require.Equal(t, []string{manual.Spec.InternalIP + ":" + manual.Spec.InternalPort}, backends)

	// The owner's own sibling keeps the identity: nothing to hand over.
	c = dnatListerController(t, svcRule)
	rules, err = c.shareIdentityRules(gwName, "tcp", "80", "eip-a", "", nil)
	require.NoError(t, err)
	require.True(t, sameShareOwner(dnatIdentityWinnerRule(rules), svcRule))
}

// Test_dnatIdentityWinner pins the deterministic choice of who writes a shared identity: one rule per
// identity, chosen the way the Service conflict resolver chooses (a hand-managed rule wins, then the
// smallest name), so the choice does not depend on reconcile order and a restart cannot flip it.
func Test_dnatIdentityWinner(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	manualSvc := ownedShareRule("svc-rule", "eip-a", "10.96.1.5")
	sibling := ownedShareRule("svc-rule-2", "eip-a", "10.96.1.6")
	manual := clusterIPServedRule("manual-rule", gwName, "10.96.1.7")
	manual.Spec.EIP = "eip-a"
	c := dnatListerController(t, manualSvc, sibling, manual)

	// A hand-managed rule has no owner key, so it wins over the Service, like chooseNftableLbOwner.
	won, err := c.dnatIdentityWinnerIs(gwName, manual, "tcp", "80", "eip-a", "")
	require.NoError(t, err)
	require.True(t, won)
	for _, loser := range []*kubeovnv1.IptablesDnatRule{manualSvc, sibling} {
		won, err = c.dnatIdentityWinnerIs(gwName, loser, "tcp", "80", "eip-a", "")
		require.NoError(t, err)
		require.False(t, won, "only the winner writes the identity")
		// The winner is only about the shared EIP identity: the ClusterIP one is their own.
		won, err = c.dnatIdentityWinnerIs(gwName, loser, "tcp", "80", "", loser.Spec.ClusterIP)
		require.NoError(t, err)
		require.True(t, won, "a rule owns the internal VIP nobody else programs")
	}
}
