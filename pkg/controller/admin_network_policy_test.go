package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestValidateAnpConfig(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

	anp := &v1alpha1.AdminNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "anp-prio",
		},
		Spec: v1alpha1.AdminNetworkPolicySpec{
			Priority: util.AnpMaxPriority,
			Subject:  v1alpha1.AdminNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
		},
	}

	t.Run("normal", func(t *testing.T) {
		err := ctrl.validateAnpConfig(anp)
		require.NoError(t, err)
	})

	t.Run("conflict priority", func(t *testing.T) {
		ctrl.anpPrioNameMap = map[int32]string{anp.Spec.Priority: anp.Name + "-conflict"}
		err := ctrl.validateAnpConfig(anp)
		require.ErrorContains(t, err, "can not create anp with same priority")
	})

	t.Run("priority out of range", func(t *testing.T) {
		anp.Spec.Priority = util.AnpMaxPriority + 1
		err := ctrl.validateAnpConfig(anp)
		require.ErrorContains(t, err, "is greater than max value")
	})
}

func TestFetchCIDRAddrs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		networks []v1alpha1.CIDR
		v4Addrs  []string
		v6Addrs  []string
	}{
		{
			name: "nil",
		},
		{
			name:     "empty",
			networks: []v1alpha1.CIDR{},
		},
		{
			name:     "ipv4 only",
			networks: []v1alpha1.CIDR{"1.1.1.0/24"},
			v4Addrs:  []string{"1.1.1.0/24"},
		},
		{
			name:     "ipv6 only",
			networks: []v1alpha1.CIDR{"fd00::/64"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "mixed",
			networks: []v1alpha1.CIDR{"1.1.1.0/24", "fd00::/64"},
			v4Addrs:  []string{"1.1.1.0/24"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "invalid ipv4 cidr",
			networks: []v1alpha1.CIDR{"1.1.1.0/33", "fd00::/64"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "invalid ipv6 cidr",
			networks: []v1alpha1.CIDR{"1.1.1.0/24", "fd00::/129"},
			v4Addrs:  []string{"1.1.1.0/24"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4Addrs, v6Addrs := fetchCIDRAddrs(tt.networks)
			require.Equal(t, tt.v4Addrs, v4Addrs)
			require.Equal(t, tt.v6Addrs, v6Addrs)
		})
	}
}

func TestIsRulesArrayEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  [util.AnpMaxRules]ChangedName
		ret  bool
	}{
		{
			"empty",
			[util.AnpMaxRules]ChangedName{},
			true,
		},
		{
			"rule name",
			[util.AnpMaxRules]ChangedName{{curRuleName: "foo"}},
			false,
		},
		{
			"match",
			[util.AnpMaxRules]ChangedName{{isMatch: true}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := isRulesArrayEmpty(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}

func TestGetAnpName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		ret  string
	}{
		{
			"normal",
			"foo",
			"foo",
		},
		{
			"start with digit",
			"123",
			"anp123",
		},
		{
			"start with hyphen",
			"-foo",
			"anp-foo",
		},
		{
			"start with underscore",
			"_foo",
			"anp_foo",
		},
		{
			"start with dot",
			".foo",
			"anp.foo",
		},
		{
			"start with slash",
			"/foo",
			"anp/foo",
		},
		{
			"start with colon",
			":foo",
			"anp:foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := getAnpName(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}

func TestGetAnpAddressSetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pgName    string
		ruleName  string
		index     int
		isIngress bool
		v4Name    string
		v6Name    string
	}{
		{
			"ingress",
			"foo",
			"bar",
			1,
			true,
			"foo.ingress.1.bar.IPv4",
			"foo.ingress.1.bar.IPv6",
		},
		{
			"egress",
			"bar",
			"foo",
			0,
			false,
			"bar.egress.0.foo.IPv4",
			"bar.egress.0.foo.IPv6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4Name, v6Name := getAnpAddressSetName(tt.pgName, tt.ruleName, tt.index, tt.isIngress)
			require.Equal(t, tt.v4Name, v4Name)
			require.Equal(t, tt.v6Name, v6Name)
		})
	}
}

func TestIsLabelsMatch(t *testing.T) {
	t.Parallel()

	nsLabelMap := make(map[string]string, 1)
	nsLabelMap["nsName"] = "test-ns"
	nsCmpLabelMap := make(map[string]string, 1)
	nsCmpLabelMap["nsName"] = "test-ns-cmp"
	nsLabels := metav1.LabelSelector{MatchLabels: nsLabelMap}

	t.Run("check namespace label match", func(t *testing.T) {
		isMatch := isLabelsMatch(&nsLabels, nil, nsLabelMap, nil)
		require.True(t, isMatch)

		isMatch = isLabelsMatch(&nsLabels, nil, nsCmpLabelMap, nil)
		require.False(t, isMatch)
	})

	podLabelMap := make(map[string]string, 1)
	podLabelMap["podName"] = "test-pod"
	podCmpLabelMap := make(map[string]string, 1)
	podCmpLabelMap["podName"] = "test-pod-cmp"
	podLabels := metav1.LabelSelector{MatchLabels: podLabelMap}
	nsPod := v1alpha1.NamespacedPod{NamespaceSelector: nsLabels, PodSelector: podLabels}

	t.Run("check pod label match", func(t *testing.T) {
		isMatch := isLabelsMatch(nil, &nsPod, nsLabelMap, podLabelMap)
		require.True(t, isMatch)

		isMatch = isLabelsMatch(nil, &nsPod, nsCmpLabelMap, podLabelMap)
		require.False(t, isMatch)

		isMatch = isLabelsMatch(nil, &nsPod, nsLabelMap, podCmpLabelMap)
		require.False(t, isMatch)

		isMatch = isLabelsMatch(nil, &nsPod, nsCmpLabelMap, podCmpLabelMap)
		require.False(t, isMatch)
	})
}

func TestAnpACLAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  v1alpha1.AdminNetworkPolicyRuleAction
		ret  ovnnb.ACLAction
	}{
		{
			"allow",
			v1alpha1.AdminNetworkPolicyRuleActionAllow,
			ovnnb.ACLActionAllowRelated,
		},
		{
			"pass",
			v1alpha1.AdminNetworkPolicyRuleActionPass,
			ovnnb.ACLActionPass,
		},
		{
			"deny",
			v1alpha1.AdminNetworkPolicyRuleActionDeny,
			ovnnb.ACLActionDrop,
		},
		{
			"unknown",
			"foo",
			ovnnb.ACLActionDrop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := anpACLAction(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}
