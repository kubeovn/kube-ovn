package controller

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestIPsecIssuerPoliciesMatchControllerContract(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("requires helm; the IPsec CI workflow installs it")
	}
	for _, chart := range []string{"kube-ovn", "kube-ovn-v2"} {
		t.Run(chart, func(t *testing.T) {
			output, err := exec.CommandContext(t.Context(), "helm", "template", "ipsec-test", "../../charts/"+chart, "--kube-version=1.37.0", "--set=namespace=ipsec-test", "--set=ipsec.certManager.enabled=true", "--set=ipsec.certManager.issuerName=test-issuer").Output()
			require.NoError(t, err)
			var policy *admissionv1.ValidatingAdmissionPolicy
			var binding *admissionv1.ValidatingAdmissionPolicyBinding
			for document := range strings.SplitSeq(string(output), "\n---\n") {
				var header struct {
					Kind string `json:"kind"`
				}
				require.NoError(t, yaml.Unmarshal([]byte(document), &header))
				switch header.Kind {
				case "ValidatingAdmissionPolicy":
					policy = new(admissionv1.ValidatingAdmissionPolicy)
					require.NoError(t, yaml.UnmarshalStrict([]byte(document), policy))
				case "ValidatingAdmissionPolicyBinding":
					binding = new(admissionv1.ValidatingAdmissionPolicyBinding)
					require.NoError(t, yaml.UnmarshalStrict([]byte(document), binding))
				}
			}
			require.NotNil(t, policy)
			require.NotNil(t, binding)
			c := &Controller{config: &Configuration{PodNamespace: "ipsec-test", CertManagerIssuerName: "test-issuer", KubeClient: fake.NewClientset(policy, binding)}}
			require.NoError(t, c.verifyIPsecIssuerPolicy(t.Context()))
		})
	}
}

func TestIPsecIssuerRejectsMissingOrWeakenedAdmissionPolicy(t *testing.T) {
	for _, scenario := range []string{"valid", "api-defaults", "missing", "ignore-errors", "namespace-filter", "object-filter", "audit-only", "wrong-issuer", "stale-status"} {
		t.Run(scenario, func(t *testing.T) {
			policy, binding := testIPsecIssuerPolicy("kube-system", "kube-ovn")
			switch scenario {
			case "api-defaults":
				policy.Spec.MatchConstraints.NamespaceSelector = &metav1.LabelSelector{}
				policy.Spec.MatchConstraints.ObjectSelector = &metav1.LabelSelector{}
				policy.Spec.MatchConstraints.MatchPolicy = new(admissionv1.Equivalent)
				policy.Spec.MatchConstraints.ResourceRules[0].Scope = new(admissionv1.AllScopes)
			case "ignore-errors":
				policy.Spec.FailurePolicy = new(admissionv1.Ignore)
			case "namespace-filter":
				policy.Spec.MatchConstraints.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"namespace": "kube-system"}}
			case "object-filter":
				policy.Spec.MatchConstraints.ObjectSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "ipsec", Operator: metav1.LabelSelectorOpExists}}}
			case "audit-only":
				binding.Spec.ValidationActions = []admissionv1.ValidationAction{admissionv1.Audit}
			case "wrong-issuer":
				policy.Spec.MatchConditions[0].Expression, _ = ipsecIssuerPolicyExpressions("kube-system", "other-issuer")
			case "stale-status":
				policy.Generation = 1
			}
			client := fake.NewClientset(policy, binding)
			if scenario == "missing" {
				client = fake.NewClientset()
			}
			c := &Controller{config: &Configuration{PodNamespace: "kube-system", CertManagerIssuerName: "kube-ovn", KubeClient: client}}
			err := c.verifyIPsecIssuerPolicy(t.Context())
			if scenario == "valid" || scenario == "api-defaults" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestIPsecIssuerAdmissionCELAuthorization(t *testing.T) {
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("request", cel.DynType), cel.Variable("oldObject", cel.DynType))
	require.NoError(t, err)
	match, authorized := ipsecIssuerPolicyExpressions("kube-system", "kube-ovn")
	for expression, cases := range map[string][]struct {
		username, namespace, issuer, kind, group string
		want                                     bool
	}{
		match: {
			{issuer: "kube-ovn", kind: "ClusterIssuer", group: "cert-manager.io", want: true},
			{issuer: "kube-ovn", kind: "ClusterIssuer", want: true},
			{issuer: "other", kind: "ClusterIssuer", want: false},
			{issuer: "kube-ovn", kind: "Issuer", want: false},
			{issuer: "kube-ovn", kind: "ClusterIssuer", group: "example.net", want: false},
		},
		authorized: {
			{username: "system:serviceaccount:kube-system:ovn", namespace: "kube-system", want: true},
			{username: "system:serviceaccount:kube-system:kube-ovn-cni", namespace: "kube-system", want: false},
			{username: "system:serviceaccount:other:ovn", namespace: "kube-system", want: false},
			{username: "system:serviceaccount:kube-system:ovn", namespace: "other", want: false},
		},
	} {
		ast, issues := env.Compile(expression)
		require.NoError(t, issues.Err())
		program, err := env.Program(ast)
		require.NoError(t, err)
		for _, tc := range cases {
			ref := map[string]any{"name": tc.issuer, "kind": tc.kind}
			if tc.group != "" {
				ref["group"] = tc.group
			}
			value, _, err := program.Eval(map[string]any{
				"object":    map[string]any{"metadata": map[string]any{"namespace": tc.namespace}, "spec": map[string]any{"issuerRef": ref}},
				"request":   map[string]any{"operation": "CREATE", "userInfo": map[string]any{"username": tc.username}},
				"oldObject": nil,
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, value.Value())
		}
	}
	ast, issues := env.Compile(authorized)
	require.NoError(t, issues.Err())
	program, err := env.Program(ast)
	require.NoError(t, err)
	for _, changed := range []bool{false, true} {
		previous := "same-csr"
		if changed {
			previous = "different-csr"
		}
		value, _, err := program.Eval(map[string]any{
			"request":   map[string]any{"operation": "UPDATE", "userInfo": map[string]any{"username": "system:serviceaccount:cert-manager:cert-manager"}},
			"object":    map[string]any{"metadata": map[string]any{"namespace": "kube-system"}, "spec": map[string]any{"request": "same-csr"}},
			"oldObject": map[string]any{"spec": map[string]any{"request": previous}},
		})
		require.NoError(t, err)
		require.Equal(t, !changed, value.Value(), "metadata-only updates can proceed but another signer cannot change the authorized request")
	}
}
