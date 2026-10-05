package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ipsecIssuerPolicyName = "kube-ovn-ipsec-issuer"

func unconditionalIPsecSelector(selector *metav1.LabelSelector) bool {
	// The API Server defaults omitted selectors to {}, which matches all
	// objects/namespaces. Only a nonempty selector narrows the policy's scope.
	return selector == nil || len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0
}

func ipsecIssuerPolicyExpressions(namespace, issuer string) (match, authorized string) {
	match = fmt.Sprintf(`object.spec.issuerRef.name == %q && has(object.spec.issuerRef.kind) && object.spec.issuerRef.kind == "ClusterIssuer" && (!has(object.spec.issuerRef.group) || object.spec.issuerRef.group == "cert-manager.io")`, issuer)
	authorized = fmt.Sprintf(`(request.operation == "UPDATE" && object.spec == oldObject.spec) || request.userInfo.username == %q && object.metadata.namespace == %q`, "system:serviceaccount:"+namespace+":ovn", namespace)
	return match, authorized
}

// Refuse external issuance unless admission protects this specific issuer.
// Checking RBAC on the node alone would still let another CertificateRequest
// creator bypass node authorization through a generic cert-manager approver.
func (c *Controller) verifyIPsecIssuerPolicy(ctx context.Context) error {
	client := c.config.KubeClient.AdmissionregistrationV1()
	policy, err := client.ValidatingAdmissionPolicies().Get(ctx, ipsecIssuerPolicyName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	match, authorized := ipsecIssuerPolicyExpressions(c.config.PodNamespace, c.config.CertManagerIssuerName)
	normalize := func(expression string) string { return strings.Join(strings.Fields(expression), " ") }
	constraints := policy.Spec.MatchConstraints
	if policy.Spec.FailurePolicy == nil || *policy.Spec.FailurePolicy != admissionv1.Fail || policy.Spec.ParamKind != nil || constraints == nil || !unconditionalIPsecSelector(constraints.NamespaceSelector) || !unconditionalIPsecSelector(constraints.ObjectSelector) || len(constraints.ExcludeResourceRules) != 0 || len(constraints.ResourceRules) != 1 || len(policy.Spec.MatchConditions) != 1 || normalize(policy.Spec.MatchConditions[0].Expression) != match || len(policy.Spec.Validations) != 1 || normalize(policy.Spec.Validations[0].Expression) != authorized {
		return errors.New("IPsec issuer admission policy is missing or has an incompatible scope")
	}
	rule := constraints.ResourceRules[0]
	if !slices.Equal(rule.APIGroups, []string{"cert-manager.io"}) || !slices.Equal(rule.APIVersions, []string{"v1"}) || !slices.Equal(rule.Operations, []admissionv1.OperationType{admissionv1.Create, admissionv1.Update}) || !slices.Equal(rule.Resources, []string{"certificaterequests"}) || len(rule.ResourceNames) != 0 || rule.Scope != nil && *rule.Scope != admissionv1.AllScopes && *rule.Scope != admissionv1.NamespacedScope {
		return errors.New("IPsec issuer admission policy does not cover all CertificateRequest creators")
	}
	if policy.Status.ObservedGeneration < policy.Generation || policy.Status.TypeChecking != nil && len(policy.Status.TypeChecking.ExpressionWarnings) != 0 {
		return errors.New("IPsec issuer admission policy is not validated yet")
	}
	binding, err := client.ValidatingAdmissionPolicyBindings().Get(ctx, ipsecIssuerPolicyName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if binding.Spec.PolicyName != policy.Name || binding.Spec.ParamRef != nil || binding.Spec.MatchResources != nil || !slices.Equal(binding.Spec.ValidationActions, []admissionv1.ValidationAction{admissionv1.Deny}) {
		return errors.New("IPsec issuer admission policy needs an unconditional Deny binding")
	}
	return nil
}
