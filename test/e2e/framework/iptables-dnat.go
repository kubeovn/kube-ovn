package framework

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

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

// IptablesDnatClient is a struct for iptables dnat client.
type IptablesDnatClient struct {
	f *Framework
	v1.IptablesDnatRuleInterface
}

func (f *Framework) IptablesDnatClient() *IptablesDnatClient {
	return &IptablesDnatClient{
		f:                         f,
		IptablesDnatRuleInterface: f.KubeOVNClientSet.KubeovnV1().IptablesDnatRules(),
	}
}

func (c *IptablesDnatClient) Get(name string) *apiv1.IptablesDnatRule {
	dnat, err := c.IptablesDnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return dnat
}

// Create creates a new iptables dnat according to the framework specifications
func (c *IptablesDnatClient) Create(dnat *apiv1.IptablesDnatRule) *apiv1.IptablesDnatRule {
	dnat, err := c.IptablesDnatRuleInterface.Create(context.TODO(), dnat, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating iptables dnat")
	return dnat.DeepCopy()
}

// CreateSync creates a new iptables dnat according to the framework specifications, and waits for it to be ready.
func (c *IptablesDnatClient) CreateSync(dnat *apiv1.IptablesDnatRule) *apiv1.IptablesDnatRule {
	dnat = c.Create(dnat)
	ExpectTrue(c.WaitToBeReady(dnat.Name, timeout))
	// Get the newest iptables dnat after it becomes ready
	return c.Get(dnat.Name).DeepCopy()
}

// Patch patches the iptables dnat
func (c *IptablesDnatClient) Patch(original, modified *apiv1.IptablesDnatRule) *apiv1.IptablesDnatRule {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIptablesDnatRule *apiv1.IptablesDnatRule
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		dnat, err := c.IptablesDnatRuleInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch iptables dnat %q", original.Name)
		}
		patchedIptablesDnatRule = dnat
		return true, nil
	})
	if err == nil {
		return patchedIptablesDnatRule.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch iptables DNAT rule %s", original.Name)
	}
	Failf("error occurred while retrying to patch iptables DNAT rule %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the iptables dnat and waits for the iptables dnat to be ready for `timeout`.
// If the iptables dnat doesn't become ready before the timeout, it will fail the test.
func (c *IptablesDnatClient) PatchSync(original, modified *apiv1.IptablesDnatRule, _ []string, timeout time.Duration) *apiv1.IptablesDnatRule {
	dnat := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(dnat, timeout))
	ExpectTrue(c.WaitToBeReady(dnat.Name, timeout))
	// Get the newest iptables dnat after it becomes ready
	return c.Get(dnat.Name).DeepCopy()
}

// Delete deletes a iptables dnat if the iptables dnat exists
func (c *IptablesDnatClient) Delete(name string) {
	err := c.IptablesDnatRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete iptables dnat %q: %v", name, err)
	}
}

// DeleteSync deletes the iptables dnat and waits for the iptables dnat to disappear for `timeout`.
// If the iptables dnat doesn't disappear before the timeout, it will fail the test.
func (c *IptablesDnatClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for iptables dnat %q to disappear", name)
}

// WaitToBeReady returns whether the iptables dnat is ready within timeout.
func (c *IptablesDnatClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("dnat %s is ready", name)
			return true
		}
		Logf("dnat %s is not ready", name)
	}
	return false
}

// WaitToBeUpdated returns whether the iptables dnat is updated within timeout.
func (c *IptablesDnatClient) WaitToBeUpdated(dnat *apiv1.IptablesDnatRule, timeout time.Duration) bool {
	Logf("Waiting up to %v for iptables dnat %s to be updated", timeout, dnat.Name)
	rv, _ := big.NewInt(0).SetString(dnat.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(dnat.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("iptables dnat %s was not updated within %v", dnat.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified iptables DNAT rule to disappear.
func (c *IptablesDnatClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.IptablesDnatRule, error) {
		rule, err := c.IptablesDnatRuleInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return rule, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected iptables DNAT rule %s to not be found: %w", name, err)
	}
	return nil
}

func MakeIptablesDnatRule(name, eip, externalPort, protocol, internalIP, internalPort string) *apiv1.IptablesDnatRule {
	dnat := &apiv1.IptablesDnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.IptablesDnatRuleSpec{
			EIP:          eip,
			ExternalPort: externalPort,
			Protocol:     protocol,
			InternalIP:   internalIP,
			InternalPort: internalPort,
		},
	}
	return dnat
}
