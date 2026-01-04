package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
			name: "empty eip",
			dnat: &kubeovnv1.IptablesDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "10.0.0.1",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "eip cannot be empty",
		},
		{
			name: "empty externalPort",
			dnat: &kubeovnv1.IptablesDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalIp",
		},
		{
			name: "invalid internalIP",
			dnat: &kubeovnv1.IptablesDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "80",
					InternalPort: "8080",
					InternalIP:   "not-an-ip",
					Protocol:     "tcp",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalIp",
		},
		{
			name: "empty protocol",
			dnat: &kubeovnv1.IptablesDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
			name: "valid IPv6 internalIP",
			dnat: &kubeovnv1.IptablesDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          "test-eip",
					ExternalPort: "443",
					InternalPort: "8443",
					InternalIP:   "fd00::1",
					Protocol:     "tcp",
				},
			},
			wantErr: false,
		},
		{
			name: "max valid port",
			dnat: &kubeovnv1.IptablesDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dnat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-fip"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-fip"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-fip"},
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalIp",
		},
		{
			name: "invalid internalIP",
			fip: &kubeovnv1.IptablesFIPRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-fip"},
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "invalid-ip",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalIp",
		},
		{
			name: "valid IPv6 internalIP",
			fip: &kubeovnv1.IptablesFIPRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-fip"},
				Spec: kubeovnv1.IptablesFIPRuleSpec{
					EIP:        "test-eip",
					InternalIP: "2001:db8::1",
				},
			},
			wantErr: false,
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalCIDR",
		},
		{
			name: "invalid internalCIDR - not a CIDR",
			snat: &kubeovnv1.IptablesSnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "10.0.0.1",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalCIDR",
		},
		{
			name: "invalid internalCIDR - invalid format",
			snat: &kubeovnv1.IptablesSnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "invalid-cidr",
				},
			},
			wantErr: true,
			errMsg:  "invalid internalCIDR",
		},
		{
			name: "valid IPv6 internalCIDR",
			snat: &kubeovnv1.IptablesSnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "fd00::/64",
				},
			},
			wantErr: false,
		},
		{
			name: "valid multiple CIDRs",
			snat: &kubeovnv1.IptablesSnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
				Spec: kubeovnv1.IptablesSnatRuleSpec{
					EIP:          "test-eip",
					InternalCIDR: "10.0.0.0/24,192.168.1.0/24",
				},
			},
			wantErr: false,
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
