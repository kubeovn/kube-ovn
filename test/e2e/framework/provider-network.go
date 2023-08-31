package framework

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ProviderNetworkClient is a struct for provider network client.
type ProviderNetworkClient struct {
	f *Framework
	v1.ProviderNetworkInterface
}

func (f *Framework) ProviderNetworkClient() *ProviderNetworkClient {
	return &ProviderNetworkClient{
		f:                        f,
		ProviderNetworkInterface: f.KubeOVNClientSet.KubeovnV1().ProviderNetworks(),
	}
}

func (c *ProviderNetworkClient) Get(name string) *apiv1.ProviderNetwork {
	pn, err := c.ProviderNetworkInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return pn
}

// Create creates a new provider network according to the framework specifications
func (c *ProviderNetworkClient) Create(pn *apiv1.ProviderNetwork) *apiv1.ProviderNetwork {
	pn, err := c.ProviderNetworkInterface.Create(context.TODO(), pn, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating provider network")
	return pn.DeepCopy()
}

// CreateSync creates a new provider network according to the framework specifications, and waits for it to be ready.
func (c *ProviderNetworkClient) CreateSync(pn *apiv1.ProviderNetwork) *apiv1.ProviderNetwork {
	pn = c.Create(pn)
	ExpectTrue(c.WaitToBeReady(pn.Name, timeout))
	// Get the newest provider network after it becomes ready
	return c.Get(pn.Name).DeepCopy()
}

// Patch patches the provider network
func (c *ProviderNetworkClient) Patch(original, modified *apiv1.ProviderNetwork) *apiv1.ProviderNetwork {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedProviderNetwork *apiv1.ProviderNetwork
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pn, err := c.ProviderNetworkInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch provider network %q", original.Name)
		}
		patchedProviderNetwork = pn
		return true, nil
	})
	if err == nil {
		return patchedProviderNetwork.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch provider network %s", original.Name)
	}
	Failf("error occurred while retrying to patch provider network %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the provider network and waits for the provider network to be ready for `timeout`.
// If the provider network doesn't become ready before the timeout, it will fail the test.
func (c *ProviderNetworkClient) PatchSync(original, modified *apiv1.ProviderNetwork, _ []string, timeout time.Duration) *apiv1.ProviderNetwork {
	pn := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(pn, timeout))
	ExpectTrue(c.WaitToBeReady(pn.Name, timeout))
	// Get the newest subnet after it becomes ready
	return c.Get(pn.Name).DeepCopy()
}

// Delete deletes a provider network if the provider network exists
func (c *ProviderNetworkClient) Delete(name string) {
	err := c.ProviderNetworkInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete provider network %q: %v", name, err)
	}
}

// DeleteSync deletes the provider network and waits for the provider network to disappear for `timeout`.
// If the provider network doesn't disappear before the timeout, it will fail the test.
func (c *ProviderNetworkClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for provider network %q to disappear", name)
}

func isProviderNetworkConditionSetAsExpected(pn *apiv1.ProviderNetwork, node string, conditionType apiv1.ConditionType, wantTrue, silent bool) bool {
	for _, cond := range pn.Status.Conditions {
		if cond.Node == node && cond.Type == conditionType {
			if (wantTrue && (cond.Status == corev1.ConditionTrue)) || (!wantTrue && (cond.Status != corev1.ConditionTrue)) {
				return true
			}
			if !silent {
				Logf("Condition %s for node %s of provider network %s is %v instead of %t. Reason: %v, message: %v",
					conditionType, node, pn.Name, cond.Status == corev1.ConditionTrue, wantTrue, cond.Reason, cond.Message)
			}
			return false
		}
	}
	if !silent {
		Logf("Couldn't find condition %v of node %s on provider network %v", conditionType, node, pn.Name)
	}
	return false
}

// IsProviderNetworkConditionSetAsExpected returns a wantTrue value if the subnet has a match to the conditionType,
// otherwise returns an opposite value of the wantTrue with detailed logging.
func IsProviderNetworkConditionSetAsExpected(pn *apiv1.ProviderNetwork, node string, conditionType apiv1.ConditionType, wantTrue bool) bool {
	return isProviderNetworkConditionSetAsExpected(pn, node, conditionType, wantTrue, false)
}

// WaitConditionToBe returns whether provider network "name's" condition state matches wantTrue
// within timeout. If wantTrue is true, it will ensure the provider network condition status is
// ConditionTrue; if it's false, it ensures the provider network condition is in any state other
// than ConditionTrue (e.g. not true or unknown).
func (c *ProviderNetworkClient) WaitConditionToBe(name, node string, conditionType apiv1.ConditionType, wantTrue bool, deadline time.Time) bool {
	timeout := time.Until(deadline)
	Logf("Waiting up to %v for provider network %s condition %s of node %s to be %t", timeout, name, conditionType, node, wantTrue)
	for ; time.Now().Before(deadline); time.Sleep(poll) {
		if pn := c.Get(name); IsProviderNetworkConditionSetAsExpected(pn, node, conditionType, wantTrue) {
			return true
		}
	}
	Logf("ProviderNetwork %s didn't reach desired %s condition status (%t) within %v", name, conditionType, wantTrue, timeout)
	return false
}

// WaitToBeReady returns whether the provider network is ready within timeout.
func (c *ProviderNetworkClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			return true
		}
	}
	return false
}

// WaitToBeUpdated returns whether the provider network is updated within timeout.
func (c *ProviderNetworkClient) WaitToBeUpdated(pn *apiv1.ProviderNetwork, timeout time.Duration) bool {
	Logf("Waiting up to %v for provider network %s to be updated", timeout, pn.Name)
	rv, _ := big.NewInt(0).SetString(pn.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(pn.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("ProviderNetwork %s was not updated within %v", pn.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified provider network to disappear.
func (c *ProviderNetworkClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.ProviderNetwork, error) {
		pn, err := c.ProviderNetworkInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return pn, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected provider network %s to not be found: %w", name, err)
	}
	return nil
}

func MakeProviderNetwork(name string, exchangeLinkName bool, defaultInterface string, customInterfaces map[string][]string, excludeNodes []string) *apiv1.ProviderNetwork {
	pn := &apiv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.ProviderNetworkSpec{
			DefaultInterface: defaultInterface,
			ExcludeNodes:     excludeNodes,
			ExchangeLinkName: exchangeLinkName,
		},
	}
	for iface, nodes := range customInterfaces {
		ci := apiv1.CustomInterface{
			Interface: iface,
			Nodes:     nodes,
		}
		pn.Spec.CustomInterfaces = append(pn.Spec.CustomInterfaces, ci)
	}
	return pn
}
