package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
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
			errMsg:  "internalIP cannot be empty",
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
			errMsg:  "invalid internalIP",
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
			name: "invalid IPv6 internalIP - not supported",
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
			wantErr: true,
			errMsg:  "must be IPv4",
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
			errMsg:  "internalIP cannot be empty",
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
			errMsg:  "invalid internalIP",
		},
		{
			name: "invalid IPv6 internalIP - not supported",
			fip: &kubeovnv1.IptablesFIPRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-fip"},
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
			errMsg:  "internalCIDR cannot be empty",
		},
		{
			name: "valid single IP address",
			snat: &kubeovnv1.IptablesSnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
			name: "invalid IPv6 internalCIDR - not supported",
			snat: &kubeovnv1.IptablesSnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "test-snat"},
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
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				util.VpcNatGatewayNameLabel: gw,
				util.VpcDnatEPortLabel:      eport,
			},
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

	backends, err := c.getShareBackends("gw", "eip", "80", "tcp", "self")
	require.NoError(t, err)
	// Self is excluded; only ready share siblings with the same identity are returned.
	// Exclusive, other protocol/eip, incomplete spec and deleting rules are filtered out.
	assert.ElementsMatch(t, []string{"10.0.0.1:8080", "10.0.0.2:8080"}, backends)
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
			dup, err := c.isDnatDuplicated("gw", "eip", "new", "80", "tcp", tt.newType)
			assert.Equal(t, tt.wantDup, dup)
			if tt.wantDup {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
