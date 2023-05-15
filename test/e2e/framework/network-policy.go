package framework

import (
	"context"
	"fmt"
	"time"

	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	v1net "k8s.io/client-go/kubernetes/typed/networking/v1"

	"github.com/onsi/gomega"
)

// NetworkPolicyClient is a struct for network policy  client.
type NetworkPolicyClient struct {
	f *Framework
	v1net.NetworkPolicyInterface
}

func (f *Framework) NetworkPolicyClient() *NetworkPolicyClient {
	return f.NetworkPolicyClientNS(f.Namespace.Name)
}

func (f *Framework) NetworkPolicyClientNS(namespace string) *NetworkPolicyClient {
	return &NetworkPolicyClient{
		f:                      f,
		NetworkPolicyInterface: f.ClientSet.NetworkingV1().NetworkPolicies(namespace),
	}
}

func (s *NetworkPolicyClient) Get(name string) *netv1.NetworkPolicy {
	np, err := s.NetworkPolicyInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return np
}

// Create creates a new network policy according to the framework specifications
func (c *NetworkPolicyClient) Create(netpol *netv1.NetworkPolicy) *netv1.NetworkPolicy {
	np, err := c.NetworkPolicyInterface.Create(context.TODO(), netpol, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating network policy")
	return np.DeepCopy()
}

// Delete deletes a network policy if the network policy exists
func (c *NetworkPolicyClient) Delete(name string) {
	err := c.NetworkPolicyInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete network policy %q: %v", name, err)
	}
}

// DeleteSync deletes the network policy and waits for the network policy to disappear for `timeout`.
// If the network policy doesn't disappear before the timeout, it will fail the test.
func (c *NetworkPolicyClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for network policy %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified network policy to disappear.
func (c *NetworkPolicyClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastNetpol *netv1.NetworkPolicy
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for network policy %s to disappear", name)
		policies, err := c.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing network policies")
		}
		found := false
		for i, netpol := range policies.Items {
			if netpol.Name == name {
				Logf("Network policy %s still exists", name)
				found = true
				lastNetpol = &(policies.Items[i])
				break
			}
		}
		if !found {
			Logf("Network policy %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for network policy %s to disappear", name),
			lastNetpol,
		)
	}
	return maybeTimeoutError(err, "waiting for network policy %s to disappear", name)
}
