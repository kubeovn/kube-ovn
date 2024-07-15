package framework

import (
	"context"
	"fmt"
	"time"

	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1net "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// NetworkPolicyClient is a struct for network policy  client.
type NetworkPolicyClient struct {
	f *Framework
	v1net.NetworkPolicyInterface
	namespace string
}

func (f *Framework) NetworkPolicyClient() *NetworkPolicyClient {
	return f.NetworkPolicyClientNS(f.Namespace.Name)
}

func (f *Framework) NetworkPolicyClientNS(namespace string) *NetworkPolicyClient {
	return &NetworkPolicyClient{
		f:                      f,
		NetworkPolicyInterface: f.ClientSet.NetworkingV1().NetworkPolicies(namespace),
		namespace:              namespace,
	}
}

func (c *NetworkPolicyClient) Get(ctx context.Context, name string) *netv1.NetworkPolicy {
	ginkgo.GinkgoHelper()
	np, err := c.NetworkPolicyInterface.Get(ctx, name, metav1.GetOptions{})
	ExpectNoError(err)
	return np
}

// Create creates a new network policy according to the framework specifications
func (c *NetworkPolicyClient) Create(ctx context.Context, netpol *netv1.NetworkPolicy) *netv1.NetworkPolicy {
	ginkgo.GinkgoHelper()
	np, err := c.NetworkPolicyInterface.Create(ctx, netpol, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating network policy")
	return np.DeepCopy()
}

// Delete deletes a network policy if the network policy exists
func (c *NetworkPolicyClient) Delete(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := c.NetworkPolicyInterface.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete network policy %q: %v", name, err)
	}
}

// DeleteSync deletes the network policy and waits for the network policy to disappear for `timeout`.
// If the network policy doesn't disappear before the timeout, it will fail the test.
func (c *NetworkPolicyClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.Delete(ctx, name)
	gomega.Expect(c.WaitToDisappear(ctx, name, timeout)).To(gomega.Succeed(), "wait for network policy %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified network policy to disappear.
func (c *NetworkPolicyClient) WaitToDisappear(ctx context.Context, name string, timeout time.Duration) error {
	err := framework.Gomega().Eventually(ctx, framework.HandleRetry(func(ctx context.Context) (*netv1.NetworkPolicy, error) {
		policy, err := c.NetworkPolicyInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return policy, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected network policy %s to not be found: %w", name, err)
	}
	return nil
}
