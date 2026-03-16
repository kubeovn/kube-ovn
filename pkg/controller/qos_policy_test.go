package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
