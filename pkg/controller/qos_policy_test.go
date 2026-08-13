package controller

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestValidateRateValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		fieldName string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid numeric value",
			value:     "100",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid large numeric value",
			value:     "10000",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid zero value",
			value:     "0",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid decimal value",
			value:     "100.5",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid small decimal value",
			value:     "0.5",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid very small decimal value 0.01",
			value:     "0.01",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid very small decimal value 0.001",
			value:     "0.001",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "valid decimal burst value",
			value:     "1.25",
			fieldName: "burstMax",
			wantErr:   false,
		},
		{
			name:      "valid small decimal burst value 0.01",
			value:     "0.01",
			fieldName: "burstMax",
			wantErr:   false,
		},
		{
			name:      "empty value allowed",
			value:     "",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "invalid - contains unit suffix",
			value:     "100Mbit",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - contains unit suffix Mbps",
			value:     "100Mbps",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - command injection attempt semicolon",
			value:     "100;rm -rf /",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - command injection attempt backtick",
			value:     "100`whoami`",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - command injection attempt $(...)",
			value:     "$(cat /etc/passwd)",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - negative number",
			value:     "-100",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - multiple decimal points",
			value:     "100.5.5",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - spaces",
			value:     "100 200",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - hex format",
			value:     "0x64",
			fieldName: "burstMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - trailing decimal point",
			value:     "100.",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
		{
			name:      "invalid - leading decimal point",
			value:     ".5",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be a positive number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateRateValue(tt.value, tt.fieldName)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Contains(t, err.Error(), tt.fieldName)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateIPMatchValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		matchValue string
		want       bool
	}{
		{
			name:       "valid src with IPv4 CIDR /32",
			matchValue: "src 192.168.1.1/32",
			want:       true,
		},
		{
			name:       "valid dst with IPv4 CIDR /32",
			matchValue: "dst 10.0.0.1/32",
			want:       true,
		},
		{
			name:       "valid src with IPv4 subnet",
			matchValue: "src 192.168.0.0/24",
			want:       true,
		},
		{
			name:       "valid dst with IPv4 subnet",
			matchValue: "dst 10.0.0.0/8",
			want:       true,
		},
		{
			name:       "valid src with IPv6 CIDR",
			matchValue: "src 2001:db8::1/128",
			want:       true,
		},
		{
			name:       "valid dst with IPv6 subnet",
			matchValue: "dst 2001:db8::/32",
			want:       true,
		},
		{
			name:       "invalid - missing direction",
			matchValue: "192.168.1.1/32",
			want:       false,
		},
		{
			name:       "invalid - wrong direction",
			matchValue: "in 192.168.1.1/32",
			want:       false,
		},
		{
			name:       "invalid - missing CIDR prefix",
			matchValue: "src 192.168.1.1",
			want:       false,
		},
		{
			name:       "invalid - malformed IP",
			matchValue: "src 192.168.1.256/32",
			want:       false,
		},
		{
			name:       "invalid - empty string",
			matchValue: "",
			want:       false,
		},
		{
			name:       "invalid - only direction",
			matchValue: "src",
			want:       false,
		},
		{
			name:       "invalid - extra parts",
			matchValue: "src 192.168.1.1/32 extra",
			want:       false,
		},
		{
			name:       "invalid - command injection in direction",
			matchValue: "src;rm 192.168.1.1/32",
			want:       false,
		},
		{
			name:       "invalid - command injection in CIDR",
			matchValue: "src 192.168.1.1/32;whoami",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := validateIPMatchValue(tt.matchValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDiffQoSPolicyBandwidthLimitRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		oldList     kubeovnv1.QoSPolicyBandwidthLimitRules
		newList     kubeovnv1.QoSPolicyBandwidthLimitRules
		wantAdded   kubeovnv1.QoSPolicyBandwidthLimitRules
		wantDeleted kubeovnv1.QoSPolicyBandwidthLimitRules
		wantUpdated kubeovnv1.QoSPolicyBandwidthLimitRules
	}{
		{
			name:        "both empty lists",
			oldList:     kubeovnv1.QoSPolicyBandwidthLimitRules{},
			newList:     kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name:    "add new rule to empty list",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name: "delete all rules",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			newList:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name: "no changes - identical rules",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name: "update rule - change RateMax",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "200", BurstMax: "10", Direction: "egress"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "200", BurstMax: "10", Direction: "egress"},
			},
		},
		{
			name: "update rule - change BurstMax",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "20", Direction: "egress"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "20", Direction: "egress"},
			},
		},
		{
			name: "update rule - change Direction",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "ingress"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "ingress"},
			},
		},
		{
			name: "complex scenario - add, delete, update",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
				{Name: "rule2", RateMax: "200", BurstMax: "20", Direction: "ingress"},
				{Name: "rule3", RateMax: "300", BurstMax: "30", Direction: "egress"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "150", BurstMax: "10", Direction: "egress"},  // updated
				{Name: "rule3", RateMax: "300", BurstMax: "30", Direction: "egress"},  // unchanged
				{Name: "rule4", RateMax: "400", BurstMax: "40", Direction: "ingress"}, // added
			},
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule4", RateMax: "400", BurstMax: "40", Direction: "ingress"},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule2", RateMax: "200", BurstMax: "20", Direction: "ingress"},
			},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "150", BurstMax: "10", Direction: "egress"},
			},
		},
		{
			name: "update rule with MatchType and MatchValue",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", MatchType: "ip", MatchValue: "src 192.168.1.0/24"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", MatchType: "ip", MatchValue: "dst 10.0.0.0/8"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", MatchType: "ip", MatchValue: "dst 10.0.0.0/8"},
			},
		},
		{
			name: "update rule with Interface change",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "eth0"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "net1"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "net1"},
			},
		},
		{
			name: "update rule with Priority change",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 1},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 2},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 2},
			},
		},
		{
			name:    "multiple adds",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100"},
				{Name: "rule2", RateMax: "200"},
				{Name: "rule3", RateMax: "300"},
			},
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100"},
				{Name: "rule2", RateMax: "200"},
				{Name: "rule3", RateMax: "300"},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name: "decimal rate values - verify reflect.DeepEqual works correctly",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "0.5", BurstMax: "0.1"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "0.5", BurstMax: "0.1"},
			},
			wantAdded:   kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotAdded, gotDeleted, gotUpdated := diffQoSPolicyBandwidthLimitRules(tt.oldList, tt.newList)

			// For added and updated, order matters as they come from newList iteration
			assert.ElementsMatch(t, tt.wantAdded, gotAdded, "added rules mismatch")
			assert.ElementsMatch(t, tt.wantUpdated, gotUpdated, "updated rules mismatch")
			// For deleted, order may vary as it comes from map iteration
			assert.ElementsMatch(t, tt.wantDeleted, gotDeleted, "deleted rules mismatch")
		})
	}
}

func TestValidateInterfaceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		iface   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid interface eth0",
			iface:   "eth0",
			wantErr: false,
		},
		{
			name:    "valid interface net1",
			iface:   "net1",
			wantErr: false,
		},
		{
			name:    "valid interface with underscore",
			iface:   "bond_0",
			wantErr: false,
		},
		{
			name:    "valid interface with hyphen",
			iface:   "veth-abc",
			wantErr: false,
		},
		{
			name:    "valid max length interface (15 chars)",
			iface:   "abcdefghijklmno",
			wantErr: false,
		},
		{
			name:    "empty interface allowed",
			iface:   "",
			wantErr: false,
		},
		{
			name:    "invalid - too long (16 chars)",
			iface:   "abcdefghijklmnop",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
		{
			name:    "invalid - command injection with semicolon",
			iface:   "eth0;rm -rf /",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
		{
			name:    "invalid - command injection with backtick",
			iface:   "eth0`whoami`",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
		{
			name:    "invalid - command injection with $(...)",
			iface:   "$(cat /etc/passwd)",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
		{
			name:    "invalid - contains space",
			iface:   "eth 0",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
		{
			name:    "invalid - contains dot",
			iface:   "eth0.1",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
		{
			name:    "invalid - contains slash",
			iface:   "eth/0",
			wantErr: true,
			errMsg:  "must be 1-15 alphanumeric",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateInterfaceName(tt.iface)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateDirection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		direction kubeovnv1.QoSPolicyRuleDirection
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid ingress",
			direction: kubeovnv1.QoSDirectionIngress,
			wantErr:   false,
		},
		{
			name:      "valid egress",
			direction: kubeovnv1.QoSDirectionEgress,
			wantErr:   false,
		},
		{
			name:      "empty direction allowed",
			direction: "",
			wantErr:   false,
		},
		{
			name:      "invalid - arbitrary string",
			direction: "invalid",
			wantErr:   true,
			errMsg:    "must be 'ingress' or 'egress'",
		},
		{
			name:      "invalid - command injection attempt",
			direction: "ingress;rm -rf /",
			wantErr:   true,
			errMsg:    "must be 'ingress' or 'egress'",
		},
		{
			name:      "invalid - case sensitive (INGRESS)",
			direction: "INGRESS",
			wantErr:   true,
			errMsg:    "must be 'ingress' or 'egress'",
		},
		{
			name:      "invalid - typo",
			direction: "ingresss",
			wantErr:   true,
			errMsg:    "must be 'ingress' or 'egress'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateDirection(tt.direction)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompareQoSPolicyBandwidthLimitRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		oldObj kubeovnv1.QoSPolicyBandwidthLimitRules
		newObj kubeovnv1.QoSPolicyBandwidthLimitRules
		want   bool
	}{
		{
			name:   "both nil",
			oldObj: nil,
			newObj: nil,
			want:   true,
		},
		{
			name:   "both empty",
			oldObj: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			newObj: kubeovnv1.QoSPolicyBandwidthLimitRules{},
			want:   true,
		},
		{
			name:   "identical single rule",
			oldObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1", RateMax: "100"}},
			newObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1", RateMax: "100"}},
			want:   true,
		},
		{
			name:   "different RateMax",
			oldObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1", RateMax: "100"}},
			newObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1", RateMax: "200"}},
			want:   false,
		},
		{
			name:   "different length",
			oldObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1"}},
			newObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1"}, {Name: "r2"}},
			want:   false,
		},
		{
			name:   "same rules different order",
			oldObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r1"}, {Name: "r2"}},
			newObj: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "r2"}, {Name: "r1"}},
			want:   true, // order-independent comparison after sorting by Name
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compareQoSPolicyBandwidthLimitRules(tt.oldObj, tt.newObj)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateQosPolicy(t *testing.T) {
	t.Parallel()

	ctrl := &Controller{}
	tests := []struct {
		name      string
		qosPolicy *kubeovnv1.QoSPolicy
		errMsg    string
	}{
		{
			name: "natgw binding must be shared",
			qosPolicy: &kubeovnv1.QoSPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "qos-natgw-unshared"},
				Spec: kubeovnv1.QoSPolicySpec{
					Shared:      false,
					BindingType: kubeovnv1.QoSBindingTypeNatGw,
				},
			},
			errMsg: "qos policy qos-natgw-unshared is not shared, but binding to nat gateway",
		},
		{
			name: "shared natgw binding is valid",
			qosPolicy: &kubeovnv1.QoSPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "qos-natgw-shared"},
				Spec: kubeovnv1.QoSPolicySpec{
					Shared:      true,
					BindingType: kubeovnv1.QoSBindingTypeNatGw,
					BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
						Name:      "net1-egress",
						Interface: "net1",
						RateMax:   "50",
						BurstMax:  "50",
						Direction: kubeovnv1.QoSDirectionEgress,
					}},
				},
			},
		},
		{
			name: "unshared eip binding is valid",
			qosPolicy: &kubeovnv1.QoSPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "qos-eip-unshared"},
				Spec: kubeovnv1.QoSPolicySpec{
					Shared:      false,
					BindingType: kubeovnv1.QoSBindingTypeEIP,
					BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
						Name:      "eip-ingress",
						RateMax:   "0.5",
						BurstMax:  "0.06",
						Direction: kubeovnv1.QoSDirectionIngress,
					}},
				},
			},
		},
		{
			name: "invalid rate is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "qos-invalid-rate"},
				Spec: kubeovnv1.QoSPolicySpec{
					Shared:      true,
					BindingType: kubeovnv1.QoSBindingTypeNatGw,
					BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
						Name:    "net1-egress",
						RateMax: "10; rm -rf /",
					}},
				},
			},
			errMsg: "invalid rateMax value",
		},
		{
			name: "invalid ip match value is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "qos-invalid-match"},
				Spec: kubeovnv1.QoSPolicySpec{
					Shared:      true,
					BindingType: kubeovnv1.QoSBindingTypeNatGw,
					BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
						Name:       "net1-extip-egress",
						RateMax:    "25",
						BurstMax:   "25",
						Direction:  kubeovnv1.QoSDirectionEgress,
						MatchType:  kubeovnv1.QoSMatchTypeIP,
						MatchValue: "dst 172.20.0.24",
					}},
				},
			},
			errMsg: "invalid ip MatchValue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ctrl.validateQosPolicy(tt.qosPolicy)
			if tt.errMsg == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.errMsg)
		})
	}
}

func makeQoSPolicyForUpdate(name string, shared bool, bindingType kubeovnv1.QoSPolicyBindingType,
	statusRules, specRules kubeovnv1.QoSPolicyBandwidthLimitRules,
) *kubeovnv1.QoSPolicy {
	return &kubeovnv1.QoSPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kubeovnv1.QoSPolicySpec{
			Shared:              shared,
			BindingType:         bindingType,
			BandwidthLimitRules: specRules,
		},
		Status: kubeovnv1.QoSPolicyStatus{
			Shared:              shared,
			BindingType:         bindingType,
			BandwidthLimitRules: statusRules,
		},
	}
}

func eipQoSRules(rate string) kubeovnv1.QoSPolicyBandwidthLimitRules {
	return kubeovnv1.QoSPolicyBandwidthLimitRules{
		{Name: "eip-ingress", RateMax: rate, BurstMax: rate, Priority: 1, Direction: kubeovnv1.QoSDirectionIngress},
		{Name: "eip-egress", RateMax: rate, BurstMax: rate, Priority: 1, Direction: kubeovnv1.QoSDirectionEgress},
	}
}

func TestHandleUpdateQoSPolicy(t *testing.T) {
	// A shared QoS policy (which a NAT gateway bound policy always is, see validateQosPolicy)
	// does not support changing its bandwidth limit rules: the limits of a NAT gateway can only
	// be changed by binding it to another policy.
	sharedNatGwQoS := makeQoSPolicyForUpdate("qos-natgw-rule-change", true, kubeovnv1.QoSBindingTypeNatGw,
		kubeovnv1.QoSPolicyBandwidthLimitRules{{
			Name: "net1-egress", Interface: "net1", RateMax: "10", BurstMax: "10",
			Priority: 3, Direction: kubeovnv1.QoSDirectionEgress,
		}},
		kubeovnv1.QoSPolicyBandwidthLimitRules{{
			Name: "net1-egress", Interface: "net1", RateMax: "50", BurstMax: "50",
			Priority: 3, Direction: kubeovnv1.QoSDirectionEgress,
		}},
	)
	// An unshared EIP policy supports hot updating its rules, but only when it is bound to at
	// most one EIP, otherwise the rules of the other EIPs would silently change as well.
	unboundEIPQoS := makeQoSPolicyForUpdate("qos-eip-unbound", false, kubeovnv1.QoSBindingTypeEIP,
		eipQoSRules("10"), eipQoSRules("50"))
	multiEIPQoS := makeQoSPolicyForUpdate("qos-eip-multi", false, kubeovnv1.QoSBindingTypeEIP,
		eipQoSRules("10"), eipQoSRules("50"))
	unchangedQoS := makeQoSPolicyForUpdate("qos-natgw-unchanged", true, kubeovnv1.QoSBindingTypeNatGw,
		eipQoSRules("10"), eipQoSRules("10"))
	// Changing .spec.shared of an existing policy is not supported either
	sharedChangedQoS := makeQoSPolicyForUpdate("qos-shared-changed", true, kubeovnv1.QoSBindingTypeNatGw,
		eipQoSRules("10"), eipQoSRules("10"))
	sharedChangedQoS.Spec.Shared = false

	eips := make([]*kubeovnv1.IptablesEIP, 0, 2)
	for _, name := range []string{"eip-1", "eip-2"} {
		eips = append(eips, &kubeovnv1.IptablesEIP{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{util.QoSLabel: multiEIPQoS.Name},
			},
			Spec:   kubeovnv1.IptablesEIPSpec{QoSPolicy: multiEIPQoS.Name},
			Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.10"},
		})
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies:  []*kubeovnv1.QoSPolicy{sharedNatGwQoS, unboundEIPQoS, multiEIPQoS, unchangedQoS, sharedChangedQoS},
		IptablesEips: eips,
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController

	t.Run("shared qos policy does not support changing its rules", func(t *testing.T) {
		err := ctrl.handleUpdateQoSPolicy(sharedNatGwQoS.Name)
		require.ErrorContains(t, err, "not support shared qos "+sharedNatGwQoS.Name+" change rule")
	})

	t.Run("qos policy does not support changing shared", func(t *testing.T) {
		err := ctrl.handleUpdateQoSPolicy(sharedChangedQoS.Name)
		require.ErrorContains(t, err, "not support qos "+sharedChangedQoS.Name+" change shared")
	})

	t.Run("unshared eip qos policy bound to multiple eips does not support changing its rules", func(t *testing.T) {
		err := ctrl.handleUpdateQoSPolicy(multiEIPQoS.Name)
		require.ErrorContains(t, err, "related eip more than one")
	})

	t.Run("unshared eip qos policy without eip updates its status", func(t *testing.T) {
		require.NoError(t, ctrl.handleUpdateQoSPolicy(unboundEIPQoS.Name))

		qos, err := ctrl.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(
			context.Background(), unboundEIPQoS.Name, metav1.GetOptions{})
		require.NoError(t, err)
		// the controller sorts the rules by name before writing them to the status
		expected := eipQoSRules("50")
		sort.Slice(expected, func(i, j int) bool { return expected[i].Name < expected[j].Name })
		require.Equal(t, expected, qos.Status.BandwidthLimitRules)
		require.Contains(t, qos.Finalizers, util.KubeOVNControllerFinalizer)
	})

	t.Run("qos policy without rule change is a no-op", func(t *testing.T) {
		require.NoError(t, ctrl.handleUpdateQoSPolicy(unchangedQoS.Name))
	})

	t.Run("deleted qos policy is ignored", func(t *testing.T) {
		require.NoError(t, ctrl.handleUpdateQoSPolicy("qos-not-found"))
	})
}
