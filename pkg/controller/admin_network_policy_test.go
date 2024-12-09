package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
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
			Priority: 100,
			Subject:  v1alpha1.AdminNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
		},
	}

	if ctrl.anpPrioNameMap == nil {
		ctrl.anpPrioNameMap = make(map[int32]string, 1)
	}
	ctrl.anpPrioNameMap[anp.Spec.Priority] = anp.Name + "test"
	t.Run("check same priority conflict", func(t *testing.T) {
		err := ctrl.validateAnpConfig(anp)
		require.ErrorContains(t, err, "can not create anp with same priority")
	})

	delete(ctrl.anpPrioNameMap, anp.Spec.Priority)
	t.Run("check priority out of range", func(t *testing.T) {
		err := ctrl.validateAnpConfig(anp)
		require.ErrorContains(t, err, "is greater than max value")
	})
}

func TestFetchCIDRAddrs(t *testing.T) {
	t.Parallel()

	networks := []v1alpha1.CIDR{"10.10.10.0/24", "fd00:10:96::/112"}
	t.Run("check cidr address", func(t *testing.T) {
		v4Addresses, v6Addresses := fetchCIDRAddrs(networks)
		require.EqualValues(t, v4Addresses, []string{"10.10.10.0/24"})
		require.EqualValues(t, v6Addresses, []string{"fd00:10:96::/112"})
	})
}

func TestIsRulesArrayEmpty(t *testing.T) {
	t.Parallel()

	ruleNames := [util.AnpMaxRules]ChangedName{{isMatch: true}}
	t.Run("check rules array empty", func(t *testing.T) {
		isEmpty := isRulesArrayEmpty(ruleNames)
		require.False(t, isEmpty)
	})
}

func TestGetAnpName(t *testing.T) {
	t.Parallel()

	anpName := "test"
	t.Run("check anp name", func(t *testing.T) {
		checkName := getAnpName(anpName)
		require.EqualValues(t, checkName, anpName)
	})

	anpName = "123"
	t.Run("check anp name start with digital", func(t *testing.T) {
		checkName := getAnpName(anpName)
		require.EqualValues(t, checkName, "anp"+anpName)
	})
}

func TestGetAnpAddressSetName(t *testing.T) {
	t.Parallel()

	pgName := "portgroup"
	ruleName := "rule"
	index := 1
	inAsV4Name := "portgroup.ingress.1.rule.IPv4"
	inAsV6Name := "portgroup.ingress.1.rule.IPv6"
	outAsV4Name := "portgroup.egress.1.rule.IPv4"
	outAsV6Name := "portgroup.egress.1.rule.IPv6"

	t.Run("check ingress address_set name", func(t *testing.T) {
		asV4Name, asV6Name := getAnpAddressSetName(pgName, ruleName, index, true)
		require.EqualValues(t, asV4Name, inAsV4Name)
		require.EqualValues(t, asV6Name, inAsV6Name)
	})

	t.Run("check egress address_set name", func(t *testing.T) {
		asV4Name, asV6Name := getAnpAddressSetName(pgName, ruleName, index, false)
		require.EqualValues(t, asV4Name, outAsV4Name)
		require.EqualValues(t, asV6Name, outAsV6Name)
	})
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
