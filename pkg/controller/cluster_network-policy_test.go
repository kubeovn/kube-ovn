package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestGetCnpPortGroupName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cnp    *v1alpha2.ClusterNetworkPolicy
		result string
	}{
		{
			name: "basic",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			result: "test",
		},
		{
			name: "dash",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-123",
				},
			},
			result: "test.123",
		},
		{
			name: "starts with integer and has dash",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "123-test",
				},
			},
			result: "cnp123.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCnpPortGroupName(tt.cnp)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestShouldUpdateCnpPortGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		oldCnp *v1alpha2.ClusterNetworkPolicy
		newCnp *v1alpha2.ClusterNetworkPolicy
		result bool
	}{
		{
			name:   "no change",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{},
			newCnp: &v1alpha2.ClusterNetworkPolicy{},
			result: false,
		},
		{
			name: "subject changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Subject: v1alpha2.ClusterNetworkPolicySubject{
						Namespaces: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label1": "value",
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Subject: v1alpha2.ClusterNetworkPolicySubject{
						Namespaces: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label1": "value",
								"label2": "value",
							},
						},
					},
				},
			},
			result: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldUpdateCnpPortGroup(tt.oldCnp, tt.newCnp)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestGetCnpAddressSetsToUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		oldCnp  *v1alpha2.ClusterNetworkPolicy
		newCnp  *v1alpha2.ClusterNetworkPolicy
		ingress bool
		egress  bool
	}{
		{
			name:    "no change",
			oldCnp:  &v1alpha2.ClusterNetworkPolicy{},
			newCnp:  &v1alpha2.ClusterNetworkPolicy{},
			ingress: false,
			egress:  false,
		},
		{
			name: "changed ingress peer",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test",
							From: []v1alpha2.ClusterNetworkPolicyIngressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label1": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test",
							From: []v1alpha2.ClusterNetworkPolicyIngressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label2": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			ingress: true,
			egress:  false,
		},
		{
			name: "changed ingress name",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test",
							From: []v1alpha2.ClusterNetworkPolicyIngressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label1": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "changed",
							From: []v1alpha2.ClusterNetworkPolicyIngressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label2": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			ingress: true,
			egress:  false,
		},
		{
			name: "changed egress peer",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label1": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label2": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			ingress: false,
			egress:  true,
		},
		{
			name: "changed egress name",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label1": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "changed",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label2": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			ingress: false,
			egress:  true,
		},
		{
			name: "changed ingress/egress",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test1",
							From: []v1alpha2.ClusterNetworkPolicyIngressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label1": "value",
										},
									},
								},
							},
						},
					},
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test2",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label2": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test3",
							From: []v1alpha2.ClusterNetworkPolicyIngressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label3": "value",
										},
									},
								},
							},
						},
					},
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test4",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Namespaces: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"label4": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			ingress: true,
			egress:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ingressChanged, _, egressChanged := getCnpAddressSetsToUpdate(tt.oldCnp, tt.newCnp)
			require.Equal(t, tt.ingress, ingressChanged)
			require.Equal(t, tt.egress, egressChanged)
		})
	}
}

func TestShouldRecreateCnpACLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		oldCnp *v1alpha2.ClusterNetworkPolicy
		newCnp *v1alpha2.ClusterNetworkPolicy
		result bool
	}{
		{
			name:   "no change",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{},
			newCnp: &v1alpha2.ClusterNetworkPolicy{},
			result: false,
		},
		{
			name: "tier changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier: v1alpha2.BaselineTier,
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier: v1alpha2.AdminTier,
				},
			},
			result: true,
		},
		{
			name: "priority changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 1,
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 2,
				},
			},
			result: true,
		},
		{
			name: "logging changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.ACLActionsLogAnnotation: "true",
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.ACLActionsLogAnnotation: "false",
					},
				},
			},
			result: true,
		},
		{
			name: "ingress count changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name:   "test1",
							Action: "test1",
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name:   "test1",
							Action: "test1",
						},
						{
							Name:   "test2",
							Action: "test2",
						},
					},
				},
			},
			result: true,
		},
		{
			name: "egress count changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name:   "test1",
							Action: "test1",
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name:   "test1",
							Action: "test1",
						},
						{
							Name:   "test2",
							Action: "test2",
						},
					},
				},
			},
			result: true,
		},
		{
			name: "ingress action changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Action: v1alpha2.ClusterNetworkPolicyRuleActionDeny,
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Action: v1alpha2.ClusterNetworkPolicyRuleActionAccept,
						},
					},
				},
			},
			result: true,
		},
		{
			name: "egress action changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Action: v1alpha2.ClusterNetworkPolicyRuleActionDeny,
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Action: v1alpha2.ClusterNetworkPolicyRuleActionAccept,
						},
					},
				},
			},
			result: true,
		},
		{
			name: "ingress port changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Ports: &[]v1alpha2.ClusterNetworkPolicyPort{
								{
									PortNumber: &v1alpha2.Port{
										Protocol: "TCP",
										Port:     1000,
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Ports: &[]v1alpha2.ClusterNetworkPolicyPort{
								{
									PortNumber: &v1alpha2.Port{
										Protocol: "TCP",
										Port:     2000,
									},
								},
							},
						},
					},
				},
			},
			result: true,
		},
		{
			name: "egress port changed",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Ports: &[]v1alpha2.ClusterNetworkPolicyPort{
								{
									PortNumber: &v1alpha2.Port{
										Protocol: "TCP",
										Port:     1000,
									},
								},
							},
						},
					},
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Ports: &[]v1alpha2.ClusterNetworkPolicyPort{
								{
									PortNumber: &v1alpha2.Port{
										Protocol: "TCP",
										Port:     2000,
									},
								},
							},
						},
					},
				},
			},
			result: true,
		},
		{
			name: "no change",
			oldCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 1000,
				},
			},
			newCnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 1000,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldRecreateCnpACLs(tt.oldCnp, tt.newCnp)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestGetCnpPriorityMaps(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

	ctrl.anpPrioNameMap = map[int32]string{1001: "test1"}
	ctrl.anpNamePrioMap = map[string]int32{"test1": 1001}
	ctrl.bnpPrioNameMap = map[int32]string{1002: "test2"}
	ctrl.bnpNamePrioMap = map[string]int32{"test2": 1002}

	t.Run("admin tier", func(t *testing.T) {
		p1, p2, err := ctrl.getCnpPriorityMaps(v1alpha2.AdminTier)
		require.Equal(t, ctrl.anpPrioNameMap, p1)
		require.Equal(t, ctrl.anpNamePrioMap, p2)
		require.NoError(t, err)
	})

	t.Run("base tier", func(t *testing.T) {
		p1, p2, err := ctrl.getCnpPriorityMaps(v1alpha2.BaselineTier)
		require.Equal(t, ctrl.bnpPrioNameMap, p1)
		require.Equal(t, ctrl.bnpNamePrioMap, p2)
		require.NoError(t, err)
	})

	t.Run("unknown tier", func(t *testing.T) {
		_, _, err := ctrl.getCnpPriorityMaps("unknown")
		require.Error(t, err)
	})
}

func TestDeleteCnpPriorityMapEntries(t *testing.T) {
	t.Parallel()

	var fakeController *fakeController = newFakeController(t)
	ctrl := fakeController.fakeController

	ctrl.anpPrioNameMap = map[int32]string{1001: "test1"}
	ctrl.anpNamePrioMap = map[string]int32{"test1": 1001}
	ctrl.bnpPrioNameMap = map[int32]string{1002: "test2"}
	ctrl.bnpNamePrioMap = map[string]int32{"test2": 1002}

	cnpAdmin := &v1alpha2.ClusterNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test1"},
		Spec: v1alpha2.ClusterNetworkPolicySpec{
			Tier:     v1alpha2.AdminTier,
			Priority: 1001,
		},
	}

	cnpBase := &v1alpha2.ClusterNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test2"},
		Spec: v1alpha2.ClusterNetworkPolicySpec{
			Tier:     v1alpha2.BaselineTier,
			Priority: 1002,
		},
		Status: v1alpha2.ClusterNetworkPolicyStatus{},
	}

	cnpUnknown := &v1alpha2.ClusterNetworkPolicy{
		Spec: v1alpha2.ClusterNetworkPolicySpec{
			Tier: "unknown",
		},
	}

	t.Run("admin tier", func(t *testing.T) {
		err := ctrl.deleteCnpPriorityMapEntries(cnpAdmin)
		require.Equal(t, 0, len(ctrl.anpPrioNameMap))
		require.Equal(t, 0, len(ctrl.anpNamePrioMap))
		require.NoError(t, err)
	})

	t.Run("base tier", func(t *testing.T) {
		err := ctrl.deleteCnpPriorityMapEntries(cnpBase)
		require.Equal(t, 0, len(ctrl.bnpPrioNameMap))
		require.Equal(t, 0, len(ctrl.bnpNamePrioMap))
		require.NoError(t, err)
	})

	t.Run("unknown tier", func(t *testing.T) {
		ctrl.anpPrioNameMap = map[int32]string{1001: "test1"}
		ctrl.anpNamePrioMap = map[string]int32{"test1": 1001}
		ctrl.bnpPrioNameMap = map[int32]string{1002: "test2"}
		ctrl.bnpNamePrioMap = map[string]int32{"test2": 1002}

		err := ctrl.deleteCnpPriorityMapEntries(cnpUnknown)
		require.Equal(t, 1, len(ctrl.anpPrioNameMap))
		require.Equal(t, 1, len(ctrl.anpNamePrioMap))
		require.Equal(t, 1, len(ctrl.bnpPrioNameMap))
		require.Equal(t, 1, len(ctrl.bnpNamePrioMap))
		require.Error(t, err)
	})
}

func TestValidateCnpConfig(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

	var tooBigIngressList []v1alpha2.ClusterNetworkPolicyIngressRule
	for range util.CnpMaxRules + 1 {
		tooBigIngressList = append(tooBigIngressList, v1alpha2.ClusterNetworkPolicyIngressRule{Name: "test"})
	}

	var tooBigEgressList []v1alpha2.ClusterNetworkPolicyEgressRule
	for range util.CnpMaxRules + 1 {
		tooBigEgressList = append(tooBigEgressList, v1alpha2.ClusterNetworkPolicyEgressRule{Name: "test"})
	}

	var tooManyDomains []v1alpha2.DomainName
	for i := 0; i < util.CnpMaxNetworks+1; i++ {
		tooManyDomains = append(tooManyDomains, "kube-ovn.io")
	}

	var tooManyNetworks []v1alpha2.CIDR
	for i := 0; i < util.CnpMaxNetworks+1; i++ {
		tooManyNetworks = append(tooManyNetworks, "10.0.0.0/24")
	}

	tests := []struct {
		name        string
		priorityMap map[int32]string
		cnp         *v1alpha2.ClusterNetworkPolicy
		error       bool
	}{
		{
			name:        "no egress rule",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test",
						},
					},
				},
			},
			error: false,
		},
		{
			name:        "no ingress rule",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
						},
					},
				},
			},
			error: false,
		},
		{
			name:        "no ingress or egressrule",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
				},
			},
			error: false,
		},
		{
			name:        "too many ingress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
					Ingress:  tooBigIngressList,
				},
			},
			error: true,
		},
		{
			name:        "too many egress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
					Egress:   tooBigEgressList,
				},
			},
			error: true,
		},
		{
			name:        "too many egress/ingress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
					Ingress:  tooBigIngressList,
					Egress:   tooBigEgressList,
				},
			},
			error: true,
		},
		{
			name:        "just enough egress/ingress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 99,
					Ingress:  tooBigIngressList[:util.CnpMaxRules], // We have one too much, remove it
					Egress:   tooBigEgressList[:util.CnpMaxRules],  // We have one too much, remove it
				},
			},
			error: false,
		},
		{
			name:        "priority error",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 1000,
				},
			},
			error: true,
		},
		{
			name:        "domain and networks error",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier: v1alpha2.BaselineTier,
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									DomainNames: tooManyDomains,
									Networks:    tooManyNetworks,
								},
							},
						},
					},
				},
			},
			error: true,
		},
		{
			name:        "baseline priority conflict",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.BaselineTier,
					Priority: 10,
				},
			},
			error: true,
		},
		{
			name:        "admin priority conflict",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier:     v1alpha2.AdminTier,
					Priority: 10,
				},
			},
			error: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl.anpPrioNameMap = tt.priorityMap
			ctrl.bnpPrioNameMap = tt.priorityMap

			err := ctrl.validateCnpConfig(tt.cnp)
			if tt.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckNetworkAndDomainRules(t *testing.T) {
	t.Parallel()

	var tooManyDomains []v1alpha2.DomainName
	for i := 0; i < util.CnpMaxNetworks+1; i++ {
		tooManyDomains = append(tooManyDomains, "kube-ovn.io")
	}

	var tooManyNetworks []v1alpha2.CIDR
	for i := 0; i < util.CnpMaxNetworks+1; i++ {
		tooManyNetworks = append(tooManyNetworks, "10.0.0.0/24")
	}

	tests := []struct {
		name        string
		priorityMap map[int32]string
		cnp         *v1alpha2.ClusterNetworkPolicy
		error       bool
	}{
		{
			name: "too many domains",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									DomainNames: tooManyDomains,
								},
							},
						},
					},
				},
			},
			error: true,
		},
		{
			name: "too many networks",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									Networks: tooManyNetworks,
								},
							},
						},
					},
				},
			},
			error: true,
		},
		{
			name: "too many domains and networks",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									DomainNames: tooManyDomains,
									Networks:    tooManyNetworks,
								},
							},
						},
					},
				},
			},
			error: true,
		},
		{
			name: "just enough domains and networks",
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									DomainNames: tooManyDomains[:util.CnpMaxDomains],
									Networks:    tooManyNetworks[:util.CnpMaxNetworks],
								},
							},
						},
					},
				},
			},
			error: false,
		},
		{
			name:  "no domains and networks",
			cnp:   &v1alpha2.ClusterNetworkPolicy{},
			error: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkNetworkAndDomainRules(tt.cnp)
			if tt.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckCnpPriorities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		priorityMap map[int32]string
		cnp         *v1alpha2.ClusterNetworkPolicy
		error       bool
	}{
		{
			name:  "no priorityMap",
			cnp:   &v1alpha2.ClusterNetworkPolicy{},
			error: true,
		},
		{
			name:        "no cnp",
			priorityMap: make(map[int32]string),
			error:       true,
		},
		{
			name:  "no priorityMap and cnp",
			error: true,
		},
		{
			name:        "cnp priority conflict",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 10},
			},
			error: true,
		},
		{
			name:        "cnp no priority conflict",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 99},
			},
			error: false,
		},
		{
			name:        "cnp priority too high",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 100},
			},
			error: true,
		},
		{
			name:        "cnp priority map empty",
			priorityMap: map[int32]string{},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 99},
			},
			error: false,
		},
		{
			name:        "cnp priority negative",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: -1},
			},
			error: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCnpPriorities(tt.priorityMap, tt.cnp)
			if tt.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFetchCnpCIDRAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		networks []v1alpha2.CIDR
		v4Addrs  []string
		v6Addrs  []string
	}{
		{
			name: "nil",
		},
		{
			name:     "empty",
			networks: []v1alpha2.CIDR{},
		},
		{
			name:     "ipv4 only",
			networks: []v1alpha2.CIDR{"1.1.1.0/24"},
			v4Addrs:  []string{"1.1.1.0/24"},
		},
		{
			name:     "ipv6 only",
			networks: []v1alpha2.CIDR{"fd00::/64"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "mixed",
			networks: []v1alpha2.CIDR{"1.1.1.0/24", "fd00::/64"},
			v4Addrs:  []string{"1.1.1.0/24"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "invalid ipv4 cidr",
			networks: []v1alpha2.CIDR{"1.1.1.0/33", "fd00::/64"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "invalid ipv6 cidr",
			networks: []v1alpha2.CIDR{"1.1.1.0/24", "fd00::/129"},
			v4Addrs:  []string{"1.1.1.0/24"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4Addrs, v6Addrs := fetchCnpCIDRAddresses(tt.networks)
			require.Equal(t, tt.v4Addrs, v4Addrs)
			require.Equal(t, tt.v6Addrs, v6Addrs)
		})
	}
}

func TestGetCnpName(t *testing.T) {
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
			"start with digital",
			"123",
			"cnp123",
		},
		{
			"start with hyphen",
			"-foo",
			"cnp-foo",
		},
		{
			"start with underscore",
			"_foo",
			"cnp_foo",
		},
		{
			"start with dot",
			".foo",
			"cnp.foo",
		},
		{
			"start with slash",
			"/foo",
			"cnp/foo",
		},
		{
			"start with colon",
			":foo",
			"cnp:foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := getCnpName(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}

func TestGetCnpACLAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  v1alpha2.ClusterNetworkPolicyRuleAction
		ret  ovnnb.ACLAction
	}{
		{
			"allow",
			v1alpha2.ClusterNetworkPolicyRuleActionAccept,
			ovnnb.ACLActionAllowRelated,
		},
		{
			"pass",
			v1alpha2.ClusterNetworkPolicyRuleActionPass,
			ovnnb.ACLActionPass,
		},
		{
			"deny",
			v1alpha2.ClusterNetworkPolicyRuleActionDeny,
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
			ret := getCnpACLAction(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}

func TestUpdateCnpPriorityMapEntries(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

	ctrl.anpPrioNameMap = map[int32]string{1001: "test1", 1002: "test2", 1003: "test3"}
	ctrl.anpNamePrioMap = map[string]int32{"test1": 1001, "test2": 1002, "test3": 1003}
	ctrl.bnpPrioNameMap = make(map[int32]string)
	ctrl.bnpNamePrioMap = make(map[string]int32)

	t.Run("no change", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test1",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Tier:     v1alpha2.AdminTier,
				Priority: 1001,
			},
		}

		err := ctrl.updateCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		require.Equal(t, "test1", ctrl.anpPrioNameMap[1001])
		require.Equal(t, int32(1001), ctrl.anpNamePrioMap["test1"])
	})

	t.Run("priority changed within same tier", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test1",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Tier:     v1alpha2.AdminTier,
				Priority: 1002,
			},
		}

		err := ctrl.updateCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		require.Equal(t, "", ctrl.anpPrioNameMap[1001])
		require.Equal(t, "test1", ctrl.anpPrioNameMap[1002])
		require.Equal(t, int32(1002), ctrl.anpNamePrioMap["test1"])
	})

	t.Run("tier changed", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test2",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Tier:     v1alpha2.BaselineTier,
				Priority: 1002,
			},
		}

		err := ctrl.updateCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		// CNP gone from admin maps
		require.Equal(t, "", ctrl.anpPrioNameMap[1002])
		require.Equal(t, int32(0), ctrl.anpNamePrioMap[cnp.Name])

		// CNP present in base maps
		require.Equal(t, "test2", ctrl.bnpPrioNameMap[1002])
		require.Equal(t, int32(1002), ctrl.bnpNamePrioMap[cnp.Name])
	})

	t.Run("tier and priority changed", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test3",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Tier:     v1alpha2.BaselineTier,
				Priority: 1004,
			},
		}

		err := ctrl.updateCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		// CNP gone from admin maps
		require.Equal(t, "", ctrl.anpPrioNameMap[1003])
		require.Equal(t, int32(0), ctrl.anpNamePrioMap[cnp.Name])

		// CNP present in base maps
		require.Equal(t, "test3", ctrl.bnpPrioNameMap[1004])
		require.Equal(t, int32(1004), ctrl.bnpNamePrioMap[cnp.Name])
	})
}

func TestWipeCnpPriorityMapEntries(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

	ctrl.anpPrioNameMap = map[int32]string{1001: "test1", 1003: "test3"}
	ctrl.anpNamePrioMap = map[string]int32{"test1": 1001, "test3": 1003}
	ctrl.bnpPrioNameMap = map[int32]string{1002: "test2", 1003: "test3"}
	ctrl.bnpNamePrioMap = map[string]int32{"test2": 1002, "test3": 1003}

	t.Run("present in admin maps", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test1",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Priority: 1001,
			},
		}

		err := ctrl.wipeCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		require.Equal(t, int32(0), ctrl.anpNamePrioMap[cnp.Name])
		require.Equal(t, "", ctrl.anpPrioNameMap[1001])
	})

	t.Run("present in base maps", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test2",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Priority: 1002,
			},
		}

		err := ctrl.wipeCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		require.Equal(t, int32(0), ctrl.bnpNamePrioMap[cnp.Name])
		require.Equal(t, "", ctrl.bnpPrioNameMap[1002])
	})

	t.Run("present in both maps", func(t *testing.T) {
		cnp := &v1alpha2.ClusterNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test3",
			},
			Spec: v1alpha2.ClusterNetworkPolicySpec{
				Priority: 1003,
			},
		}

		err := ctrl.wipeCnpPriorityMapEntries(cnp)
		require.NoError(t, err)
		require.Equal(t, int32(0), ctrl.anpNamePrioMap[cnp.Name])
		require.Equal(t, "", ctrl.anpPrioNameMap[1003])
		require.Equal(t, int32(0), ctrl.bnpNamePrioMap[cnp.Name])
		require.Equal(t, "", ctrl.bnpPrioNameMap[1003])
	})
}

func TestGetCnpACLTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tier   v1alpha2.Tier
		result int
	}{
		{
			"base",
			v1alpha2.BaselineTier,
			util.BanpACLTier,
		},
		{
			"admin",
			v1alpha2.AdminTier,
			util.AnpACLTier,
		},
		{
			"unknown",
			"unknown",
			util.BanpACLTier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := getCnpACLTier(tt.tier)
			require.Equal(t, tt.result, ret)
		})
	}
}

func TestHasCnpDomainNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cnp    *v1alpha2.ClusterNetworkPolicy
		result bool
	}{
		{
			"nothing",
			&v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{},
			},
			false,
		},
		{
			"one domain",
			&v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									DomainNames: []v1alpha2.DomainName{
										"kube-ovn.io",
									},
								},
							},
						},
					},
				},
			},
			true,
		},
		{
			"two domains",
			&v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
							To: []v1alpha2.ClusterNetworkPolicyEgressPeer{
								{
									DomainNames: []v1alpha2.DomainName{
										"kube-ovn.io",
										"google.com",
									},
								},
							},
						},
					},
				},
			},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := hasCnpDomainNames(tt.cnp)
			require.Equal(t, tt.result, ret)
		})
	}
}
