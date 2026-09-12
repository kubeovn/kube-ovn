package controller

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestQoSPolicyDeleteHonorsUIDClaim(t *testing.T) {
	makePolicy := func() *kubeovnv1.QoSPolicy {
		now := metav1.Now()
		return &kubeovnv1.QoSPolicy{
			Name:              "qos-delete",
			UID:               "qos-delete-uid",
			DeletionTimestamp: &now,
			Finalizers:        []string{util.KubeOVNControllerFinalizer},
			Spec:              kubeovnv1.QoSPolicySpec{BindingType: kubeovnv1.QoSBindingTypeEIP},
		}
	}

	t.Run("referencing EIP blocks finalizer removal", func(t *testing.T) {
		qos := makePolicy()
		eip := &kubeovnv1.IptablesEIP{
			Name: "eip", UID: "eip-uid", Labels: map[string]string{util.QoSPolicyUIDLabel: string(qos.UID)},
			Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: qos.Name},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{QoSPolicies: []*kubeovnv1.QoSPolicy{qos}, IptablesEips: []*kubeovnv1.IptablesEIP{eip}})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.handleUpdateQoSPolicy(qos.Name))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(context.Background(), qos.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	})

	t.Run("without claim removes finalizer", func(t *testing.T) {
		qos := makePolicy()
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{QoSPolicies: []*kubeovnv1.QoSPolicy{qos}})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.handleUpdateQoSPolicy(qos.Name))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(context.Background(), qos.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	})
}

func TestEnqueueQoSPolicyRelease(t *testing.T) {
	t.Parallel()
	q := newTypedRateLimitingQueue[string]("UpdateQoSPolicy", nil)
	t.Cleanup(q.ShutDown)
	c := &Controller{updateQoSPolicyQueue: q}

	c.enqueueQoSPolicyRelease("old", "", "new", "")
	require.Equal(t, 1, q.Len())
	got, shutdown := q.Get()
	require.False(t, shutdown)
	require.Equal(t, "old", got)
	q.Done(got)

	c.enqueueQoSPolicyRelease("old", "", "old", "")
	require.Equal(t, 0, q.Len())

	// A reference that is still applied keeps the policy enqueued until the referring
	// resource has cleaned up its data plane, even though the desired reference is gone.
	c.enqueueQoSPolicyRelease("", "applied", "", "")
	require.Equal(t, 1, q.Len())
	got, shutdown = q.Get()
	require.False(t, shutdown)
	require.Equal(t, "applied", got)
	q.Done(got)

	c.enqueueQoSPolicyRelease("unchanged", "unchanged", "unchanged", "unchanged")
	require.Equal(t, 0, q.Len())
}

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
			name:      "zero value is rejected",
			value:     "0",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must be greater than zero",
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
			name:      "minimum representable rate",
			value:     "0.000008",
			fieldName: "rateMax",
			wantErr:   false,
		},
		{
			name:      "rate below one byte per second is rejected",
			value:     "0.000007",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "tc cannot represent less than 0.000008 Mbps",
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
			name:      "empty value is rejected",
			value:     "",
			fieldName: "rateMax",
			wantErr:   true,
			errMsg:    "must not be empty",
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
			name:       "host bits in prefix are rejected",
			matchValue: "src 192.168.1.1/24",
			want:       false,
		},
		{
			name:       "IPv6 is rejected",
			matchValue: "src 2001:db8::/64",
			want:       false,
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
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "ingress"},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", BurstMax: "10", Direction: "egress"},
			},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
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
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", MatchType: "ip", MatchValue: "dst 10.0.0.0/8"},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", MatchType: "ip", MatchValue: "src 192.168.1.0/24"},
			},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name: "update rule with Interface change",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "eth0"},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "net1"},
			},
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "net1"},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Interface: "eth0"},
			},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
		},
		{
			name: "update rule with Priority change",
			oldList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 1},
			},
			newList: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 2},
			},
			wantAdded: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 2},
			},
			wantDeleted: kubeovnv1.QoSPolicyBandwidthLimitRules{
				{Name: "rule1", RateMax: "100", Priority: 1},
			},
			wantUpdated: kubeovnv1.QoSPolicyBandwidthLimitRules{},
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
			name:      "empty direction is rejected",
			direction: "",
			wantErr:   true,
			errMsg:    "must be 'ingress' or 'egress'",
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
				Name: "qos-natgw-unshared",
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
				Name: "qos-natgw-shared",
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
				Name: "qos-eip-unshared",
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
			name:      "unknown binding type is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{BindingType: "unknown"}},
			errMsg:    "invalid binding type",
		},
		{
			name: "duplicate rule name is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{
					{Name: "duplicate", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionIngress},
					{Name: "duplicate", RateMax: "2", BurstMax: "2", Direction: kubeovnv1.QoSDirectionEgress},
				},
			}},
			errMsg: "duplicate bandwidth rule name",
		},
		{
			name: "eip duplicate direction is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeEIP,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{
					{Name: "first", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionIngress},
					{Name: "second", RateMax: "2", BurstMax: "2", Direction: kubeovnv1.QoSDirectionIngress},
				},
			}},
			errMsg: "duplicates direction",
		},
		{
			name: "natgw normalized duplicate identity is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{
					{Name: "default-interface", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionEgress},
					{Name: "explicit-interface", Interface: "net1", RateMax: "2", BurstMax: "2", Direction: kubeovnv1.QoSDirectionEgress},
				},
			}},
			errMsg: "duplicates an existing rule identity",
		},
		{
			name: "natgw matchall value does not distinguish identity",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{
					{Name: "first", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionEgress, MatchValue: "ignored-1"},
					{Name: "second", RateMax: "2", BurstMax: "2", Direction: kubeovnv1.QoSDirectionEgress, MatchValue: "ignored-2"},
				},
			}},
			errMsg: "matchValue must be empty",
		},
		{
			name: "natgw same direction with different priority is valid",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{
					{Name: "first", RateMax: "1", BurstMax: "1", Priority: 1, Direction: kubeovnv1.QoSDirectionEgress},
					{Name: "second", RateMax: "2", BurstMax: "2", Priority: 2, Direction: kubeovnv1.QoSDirectionEgress},
				},
			}},
		},
		{
			name: "natgw matchall priority wraps to the same identity",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{
					{Name: "first", RateMax: "1", BurstMax: "1", Priority: 1, Direction: kubeovnv1.QoSDirectionEgress},
					{Name: "second", RateMax: "2", BurstMax: "2", Priority: 256, Direction: kubeovnv1.QoSDirectionEgress},
				},
			}},
			errMsg: "duplicates an existing rule identity",
		},
		{
			name: "zero priority is rejected for natgw ip match",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
					Name: "rule", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionEgress,
					MatchType: kubeovnv1.QoSMatchTypeIP, MatchValue: "src 192.0.2.0/24",
				}},
			}},
			errMsg: "priority must be greater than zero",
		},
		{
			name: "priority above tc limit is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeEIP,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
					Name: "rule", RateMax: "1", BurstMax: "1", Priority: 65536, Direction: kubeovnv1.QoSDirectionIngress,
				}},
			}},
			errMsg: "must be between 0 and 65535",
		},
		{
			name: "negative priority is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{
				Spec: kubeovnv1.QoSPolicySpec{
					BindingType: kubeovnv1.QoSBindingTypeEIP,
					BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
						Name: "rule", RateMax: "1", BurstMax: "1", Priority: -1, Direction: kubeovnv1.QoSDirectionIngress,
					}},
				},
			},
			errMsg: "must be between 0 and 65535",
		},
		{
			name: "empty direction is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType:         kubeovnv1.QoSBindingTypeEIP,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "rule", RateMax: "1", BurstMax: "1"}},
			}},
			errMsg: "invalid direction",
		},
		{
			name: "empty rate is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType:         kubeovnv1.QoSBindingTypeEIP,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "rule", BurstMax: "1", Direction: kubeovnv1.QoSDirectionIngress}},
			}},
			errMsg: "rateMax must not be empty",
		},
		{
			name: "empty burst is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType:         kubeovnv1.QoSBindingTypeEIP,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{Name: "rule", RateMax: "1", Direction: kubeovnv1.QoSDirectionIngress}},
			}},
			errMsg: "burstMax must not be empty",
		},
		{
			name: "match value without match type is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
					Name: "rule", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionIngress, MatchValue: "src 192.0.2.0/24",
				}},
			}},
			errMsg: "matchValue must be empty",
		},
		{
			name: "eip match fields are rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeEIP,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
					Name: "rule", Interface: "net1", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionIngress,
				}},
			}},
			errMsg: "not supported for EIP binding",
		},
		{
			name: "unknown match type is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeNatGw,
				Shared:      true,
				BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
					Name: "rule", RateMax: "1", BurstMax: "1", Direction: kubeovnv1.QoSDirectionIngress, MatchType: "unknown",
				}},
			}},
			errMsg: "invalid match type",
		},
		{
			name: "invalid rate is rejected",
			qosPolicy: &kubeovnv1.QoSPolicy{
				Name: "qos-invalid-rate",
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
				Name: "qos-invalid-match",
				Spec: kubeovnv1.QoSPolicySpec{
					Shared:      true,
					BindingType: kubeovnv1.QoSBindingTypeNatGw,
					BandwidthLimitRules: kubeovnv1.QoSPolicyBandwidthLimitRules{{
						Name:       "net1-extip-egress",
						RateMax:    "25",
						BurstMax:   "25",
						Priority:   1,
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
		Name: name, UID: types.UID(name + "-uid"),
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

func TestEnqueueQoSPolicyReferences(t *testing.T) {
	eipPolicy := &kubeovnv1.QoSPolicy{
		Name:   "eip-qos",
		Spec:   kubeovnv1.QoSPolicySpec{BindingType: kubeovnv1.QoSBindingTypeEIP},
		Status: kubeovnv1.QoSPolicyStatus{BindingType: kubeovnv1.QoSBindingTypeEIP},
	}
	gwPolicy := &kubeovnv1.QoSPolicy{
		Name:   "gw-qos",
		Spec:   kubeovnv1.QoSPolicySpec{BindingType: kubeovnv1.QoSBindingTypeNatGw},
		Status: kubeovnv1.QoSPolicyStatus{BindingType: kubeovnv1.QoSBindingTypeNatGw},
	}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies: []*kubeovnv1.QoSPolicy{eipPolicy, gwPolicy},
		IptablesEips: []*kubeovnv1.IptablesEIP{
			{Name: "bound-eip", Spec: kubeovnv1.IptablesEIPSpec{NatGwDp: "eip-gw", QoSPolicy: eipPolicy.Name}},
			{Name: "other-eip"},
		},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{
			{Name: "bound-gw", Spec: kubeovnv1.VpcNatGatewaySpec{QoSPolicy: gwPolicy.Name}},
			{Name: "other-gw"},
		},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController
	ctrl.updateQoSPolicyQueue = newTypedRateLimitingQueue[string]("UpdateQoSPolicy", nil)

	oldEIPPolicy := eipPolicy.DeepCopy()
	oldEIPPolicy.Status = kubeovnv1.QoSPolicyStatus{}
	partialEIPPolicy := eipPolicy.DeepCopy()
	partialEIPPolicy.Status.BindingType = ""
	ctrl.enqueueUpdateQoSPolicy(oldEIPPolicy, partialEIPPolicy)
	require.Zero(t, ctrl.updateIptablesEipQueue.Len())

	ctrl.enqueueUpdateQoSPolicy(partialEIPPolicy, eipPolicy)
	// The referencing EIP is woken, not its gateway: one EIP that cannot apply the
	// policy must not hold up the gateway initialization of the others.
	require.Equal(t, 1, ctrl.updateIptablesEipQueue.Len())
	require.Zero(t, ctrl.addOrUpdateVpcNatGatewayQueue.Len())

	oldGWPolicy := gwPolicy.DeepCopy()
	oldGWPolicy.Status = kubeovnv1.QoSPolicyStatus{}
	ctrl.enqueueUpdateQoSPolicy(oldGWPolicy, gwPolicy)
	require.Equal(t, 1, ctrl.addOrUpdateVpcNatGatewayQueue.Len())

	for ctrl.updateIptablesEipQueue.Len() != 0 {
		item, _ := ctrl.updateIptablesEipQueue.Get()
		ctrl.updateIptablesEipQueue.Done(item)
		ctrl.updateIptablesEipQueue.Forget(item)
	}
	ctrl.enqueueUpdateQoSPolicy(eipPolicy, eipPolicy.DeepCopy())
	require.Zero(t, ctrl.updateIptablesEipQueue.Len())
	require.Zero(t, ctrl.initVpcNatGatewayQueue.Len())
}

func TestEnqueueQoSPolicyReleaseWaitsForAppliedStatus(t *testing.T) {
	ctrl := &Controller{updateQoSPolicyQueue: newTypedRateLimitingQueue[string]("UpdateQoSPolicy", nil)}
	t.Cleanup(ctrl.updateQoSPolicyQueue.ShutDown)

	ctrl.enqueueQoSPolicyRelease("old-qos", "old-qos", "", "old-qos")
	require.Zero(t, ctrl.updateQoSPolicyQueue.Len())

	ctrl.enqueueQoSPolicyRelease("", "old-qos", "", "")
	require.Equal(t, 1, ctrl.updateQoSPolicyQueue.Len())
}

func TestDeletingQoSPolicyKeepsFinalizerWhileClaimed(t *testing.T) {
	// Spec and Status only carry names, so only the UID the referrer was labeled with
	// tells the generation of the policy it claims. A leftover reference of an earlier
	// policy that used the same name must neither block nor delay the claim of this one.
	const qosUID = "terminating-qos-uid"
	tests := []struct {
		name           string
		bindingType    kubeovnv1.QoSPolicyBindingType
		eip            *kubeovnv1.IptablesEIP
		keepsFinalizer bool
	}{
		{name: "desired spec reference", bindingType: kubeovnv1.QoSBindingTypeEIP, eip: &kubeovnv1.IptablesEIP{
			Name: "pending-eip", Labels: map[string]string{util.QoSPolicyUIDLabel: qosUID},
			Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: "terminating-qos"},
		}, keepsFinalizer: true},
		{name: "applied status credential", bindingType: kubeovnv1.QoSBindingTypeEIP, eip: &kubeovnv1.IptablesEIP{
			Name: "cleaning-eip", Labels: map[string]string{util.QoSPolicyUIDLabel: qosUID},
			Status: kubeovnv1.IptablesEIPStatus{QoSPolicy: "terminating-qos"},
		}, keepsFinalizer: true},
		{name: "binding type drift", bindingType: kubeovnv1.QoSBindingTypeNatGw, eip: &kubeovnv1.IptablesEIP{
			Name: "old-eip", Labels: map[string]string{util.QoSPolicyUIDLabel: qosUID},
			Status: kubeovnv1.IptablesEIPStatus{QoSPolicy: "terminating-qos"},
		}, keepsFinalizer: true},
		{
			name: "reference of an earlier policy with the same name", bindingType: kubeovnv1.QoSBindingTypeEIP,
			eip: &kubeovnv1.IptablesEIP{
				Name: "stale-eip", Labels: map[string]string{util.QoSPolicyUIDLabel: "replaced-qos-uid"},
				Spec:   kubeovnv1.IptablesEIPSpec{QoSPolicy: "terminating-qos"},
				Status: kubeovnv1.IptablesEIPStatus{QoSPolicy: "terminating-qos"},
			},
		},
		{name: "reference without a claim", bindingType: kubeovnv1.QoSBindingTypeEIP, eip: &kubeovnv1.IptablesEIP{
			Name: "unclaimed-eip", Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: "terminating-qos"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := metav1.Now()
			qos := &kubeovnv1.QoSPolicy{
				Name:              "terminating-qos",
				UID:               qosUID,
				DeletionTimestamp: &now,
				Finalizers:        []string{util.KubeOVNControllerFinalizer},
				Spec:              kubeovnv1.QoSPolicySpec{BindingType: tt.bindingType},
			}
			fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				QoSPolicies: []*kubeovnv1.QoSPolicy{qos}, IptablesEips: []*kubeovnv1.IptablesEIP{tt.eip},
			})
			require.NoError(t, err)

			require.NoError(t, fakeCtrl.fakeController.handleUpdateQoSPolicy(qos.Name))
			got, err := fakeCtrl.fakeController.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(
				context.Background(), qos.Name, metav1.GetOptions{},
			)
			require.NoError(t, err)
			if tt.keepsFinalizer {
				require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
			} else {
				require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
			}
		})
	}
}

func TestDeletingNatGwQoSPolicyKeepsFinalizerWhileClaimed(t *testing.T) {
	const qosUID = "terminating-qos-uid"
	tests := []struct {
		name           string
		labels         map[string]string
		keepsFinalizer bool
	}{
		{name: "applied status credential", labels: map[string]string{util.QoSPolicyUIDLabel: qosUID}, keepsFinalizer: true},
		{name: "claim of an earlier policy with the same name", labels: map[string]string{util.QoSPolicyUIDLabel: "replaced-qos-uid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := metav1.Now()
			qos := &kubeovnv1.QoSPolicy{
				Name: "terminating-qos", UID: qosUID, DeletionTimestamp: &now,
				Finalizers: []string{util.KubeOVNControllerFinalizer},
				Spec:       kubeovnv1.QoSPolicySpec{BindingType: kubeovnv1.QoSBindingTypeNatGw},
			}
			fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				QoSPolicies: []*kubeovnv1.QoSPolicy{qos},
				VpcNatGateways: []*kubeovnv1.VpcNatGateway{{
					Name: "cleaning-gateway", Labels: tt.labels,
					Status: kubeovnv1.VpcNatGatewayStatus{QoSPolicy: qos.Name},
				}},
			})
			require.NoError(t, err)

			require.NoError(t, fakeCtrl.fakeController.handleUpdateQoSPolicy(qos.Name))
			got, err := fakeCtrl.fakeController.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(
				context.Background(), qos.Name, metav1.GetOptions{},
			)
			require.NoError(t, err)
			if tt.keepsFinalizer {
				require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
			} else {
				require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
			}
		})
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
			Name: name,
			Labels: map[string]string{
				util.QoSLabel:          multiEIPQoS.Name,
				util.QoSPolicyUIDLabel: string(multiEIPQoS.UID),
			},
			Spec:   kubeovnv1.IptablesEIPSpec{QoSPolicy: multiEIPQoS.Name},
			Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.10"},
		})
	}
	// The second EIP has released its desired reference but still owns applied
	// data-plane state, so the unshared policy remains claimed until cleanup.
	eips[1].Spec.QoSPolicy = ""
	eips[1].Status.QoSPolicy = multiEIPQoS.Name

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
			context.Background(), unboundEIPQoS.Name, metav1.GetOptions{},
		)
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
