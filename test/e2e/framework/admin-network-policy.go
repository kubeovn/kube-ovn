package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	netpolv1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	anpclient "sigs.k8s.io/network-policy-api/pkg/client/clientset/versioned/typed/apis/v1alpha1"
)

// MakeAdminNetworkPolicy creates a basic AdminNetworkPolicy with common defaults
func MakeAdminNetworkPolicy(name string, priority int32, namespaceSelector *metav1.LabelSelector, egressRules []netpolv1alpha1.AdminNetworkPolicyEgressRule, ingressRules []netpolv1alpha1.AdminNetworkPolicyIngressRule) *netpolv1alpha1.AdminNetworkPolicy {
	anp := &netpolv1alpha1.AdminNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: netpolv1alpha1.AdminNetworkPolicySpec{
			Priority: priority,
			Subject: netpolv1alpha1.AdminNetworkPolicySubject{
				Namespaces: namespaceSelector,
			},
			Egress:  egressRules,
			Ingress: ingressRules,
		},
	}
	return anp
}

// MakeAdminNetworkPolicyEgressRule creates an egress rule with domain names
func MakeAdminNetworkPolicyEgressRule(name string, action netpolv1alpha1.AdminNetworkPolicyRuleAction, ports []netpolv1alpha1.AdminNetworkPolicyPort, domainNames []netpolv1alpha1.DomainName) netpolv1alpha1.AdminNetworkPolicyEgressRule {
	rule := netpolv1alpha1.AdminNetworkPolicyEgressRule{
		Name:   name,
		Action: action,
		To: []netpolv1alpha1.AdminNetworkPolicyEgressPeer{
			{
				DomainNames: domainNames,
			},
		},
	}
	if len(ports) > 0 {
		rule.Ports = &ports
	}
	return rule
}

// MakeAdminNetworkPolicyPort creates a port specification
func MakeAdminNetworkPolicyPort(port int32, protocol corev1.Protocol) netpolv1alpha1.AdminNetworkPolicyPort {
	return netpolv1alpha1.AdminNetworkPolicyPort{
		PortNumber: &netpolv1alpha1.Port{
			Port:     port,
			Protocol: protocol,
		},
	}
}

// AnpClient is a struct for AdminNetworkPolicy client.
type AnpClient struct {
	f *Framework
	anpclient.AdminNetworkPolicyInterface
}

func (f *Framework) AnpClient() *AnpClient {
	return &AnpClient{
		f:                           f,
		AdminNetworkPolicyInterface: f.AnpClientSet.PolicyV1alpha1().AdminNetworkPolicies(),
	}
}

// Get gets the AdminNetworkPolicy.
func (c *AnpClient) Get(name string) *netpolv1alpha1.AdminNetworkPolicy {
	ginkgo.GinkgoHelper()
	anp, err := c.AdminNetworkPolicyInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return anp
}

// Create creates the AdminNetworkPolicy.
func (c *AnpClient) Create(anp *netpolv1alpha1.AdminNetworkPolicy) *netpolv1alpha1.AdminNetworkPolicy {
	ginkgo.GinkgoHelper()
	anp, err := c.AdminNetworkPolicyInterface.Create(context.TODO(), anp, metav1.CreateOptions{})
	ExpectNoError(err)
	return anp
}

// Update updates the AdminNetworkPolicy.
func (c *AnpClient) Update(anp *netpolv1alpha1.AdminNetworkPolicy) *netpolv1alpha1.AdminNetworkPolicy {
	ginkgo.GinkgoHelper()
	anp, err := c.AdminNetworkPolicyInterface.Update(context.TODO(), anp, metav1.UpdateOptions{})
	ExpectNoError(err)
	return anp
}

// Delete deletes the AdminNetworkPolicy.
func (c *AnpClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.AdminNetworkPolicyInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	// If the resource is not found, that's also considered a successful deletion
	if err != nil && !apierrors.IsNotFound(err) {
		ExpectNoError(err)
	}
}

// CreateSync creates the AdminNetworkPolicy and waits for it to be ready.
func (c *AnpClient) CreateSync(anp *netpolv1alpha1.AdminNetworkPolicy) *netpolv1alpha1.AdminNetworkPolicy {
	ginkgo.GinkgoHelper()
	anp = c.Create(anp)

	// Wait for DNSNameResolver CRs to be created if the ANP has domain names
	if c.hasDomainNames(anp) {
		c.waitForDNSNameResolvers(anp.Name)
	}

	return anp
}

// hasDomainNames checks if the ANP has any domain names in its egress rules
func (c *AnpClient) hasDomainNames(anp *netpolv1alpha1.AdminNetworkPolicy) bool {
	for _, egressRule := range anp.Spec.Egress {
		for _, peer := range egressRule.To {
			if len(peer.DomainNames) > 0 {
				return true
			}
		}
	}
	return false
}

// waitForDNSNameResolvers waits for DNSNameResolver CRs to be created for the ANP
func (c *AnpClient) waitForDNSNameResolvers(anpName string) {
	ginkgo.GinkgoHelper()

	// Get DNSNameResolver client
	dnsNameResolverClient := c.f.DNSNameResolverClient()

	// Wait for at least one DNSNameResolver to be created with the ANP label
	expectedLabel := fmt.Sprintf("anp=%s", anpName)

	err := wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 30*time.Second, true, func(_ context.Context) (bool, error) {
		dnsNameResolverList := dnsNameResolverClient.ListByLabel(expectedLabel)
		return len(dnsNameResolverList.Items) > 0, nil
	})

	ExpectNoError(err, "Failed to wait for DNSNameResolver CRs to be created for ANP %s", anpName)
}
