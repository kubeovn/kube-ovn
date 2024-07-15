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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// IptablesSnatClient is a struct for iptables snat client.
type IptablesSnatClient struct {
	f *Framework
	v1.IptablesSnatRuleInterface
}

func (f *Framework) IptablesSnatClient() *IptablesSnatClient {
	return &IptablesSnatClient{
		f:                         f,
		IptablesSnatRuleInterface: f.KubeOVNClientSet.KubeovnV1().IptablesSnatRules(),
	}
}

func (c *IptablesSnatClient) Get(ctx context.Context, name string) *apiv1.IptablesSnatRule {
	ginkgo.GinkgoHelper()
	snat, err := c.IptablesSnatRuleInterface.Get(ctx, name, metav1.GetOptions{})
	ExpectNoError(err)
	return snat
}

// Create creates a new iptables snat according to the framework specifications
func (c *IptablesSnatClient) Create(ctx context.Context, snat *apiv1.IptablesSnatRule) *apiv1.IptablesSnatRule {
	ginkgo.GinkgoHelper()
	snat, err := c.IptablesSnatRuleInterface.Create(ctx, snat, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating iptables snat")
	return snat.DeepCopy()
}

// CreateSync creates a new iptables snat according to the framework specifications, and waits for it to be ready.
func (c *IptablesSnatClient) CreateSync(ctx context.Context, snat *apiv1.IptablesSnatRule) *apiv1.IptablesSnatRule {
	ginkgo.GinkgoHelper()

	snat = c.Create(ctx, snat)
	ExpectTrue(c.WaitToBeReady(ctx, snat.Name, timeout))
	// Get the newest iptables snat after it becomes ready
	return c.Get(ctx, snat.Name).DeepCopy()
}

// Patch patches the iptables snat
func (c *IptablesSnatClient) Patch(ctx context.Context, original, modified *apiv1.IptablesSnatRule) *apiv1.IptablesSnatRule {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIptablesSnatRule *apiv1.IptablesSnatRule
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		snat, err := c.IptablesSnatRuleInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch iptables snat %q", original.Name)
		}
		patchedIptablesSnatRule = snat
		return true, nil
	})
	if err == nil {
		return patchedIptablesSnatRule.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch iptables SNAT rule %s", original.Name)
	}
	Failf("error occurred while retrying to patch iptables SNAT rule %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the iptables snat and waits for the iptables snat to be ready for `timeout`.
// If the iptables snat doesn't become ready before the timeout, it will fail the test.
func (c *IptablesSnatClient) PatchSync(ctx context.Context, original, modified *apiv1.IptablesSnatRule, timeout time.Duration) *apiv1.IptablesSnatRule {
	ginkgo.GinkgoHelper()

	snat := c.Patch(ctx, original, modified)
	ExpectTrue(c.WaitToBeUpdated(ctx, snat, timeout))
	ExpectTrue(c.WaitToBeReady(ctx, snat.Name, timeout))
	// Get the newest iptables snat after it becomes ready
	return c.Get(ctx, snat.Name).DeepCopy()
}

// Delete deletes a iptables snat if the iptables snat exists
func (c *IptablesSnatClient) Delete(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := c.IptablesSnatRuleInterface.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete iptables snat %q: %v", name, err)
	}
}

// DeleteSync deletes the iptables snat and waits for the iptables snat to disappear for `timeout`.
// If the iptables snat doesn't disappear before the timeout, it will fail the test.
func (c *IptablesSnatClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.Delete(ctx, name)
	gomega.Expect(c.WaitToDisappear(ctx, name, timeout)).To(gomega.Succeed(), "wait for iptables snat %q to disappear", name)
}

// WaitToBeReady returns whether the iptables snat is ready within timeout.
func (c *IptablesSnatClient) WaitToBeReady(ctx context.Context, name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(ctx, name).Status.Ready {
			Logf("snat %s is ready", name)
			return true
		}
		Logf("snat %s is not ready", name)
	}
	return false
}

// WaitToBeUpdated returns whether the iptables snat is updated within timeout.
func (c *IptablesSnatClient) WaitToBeUpdated(ctx context.Context, snat *apiv1.IptablesSnatRule, timeout time.Duration) bool {
	Logf("Waiting up to %v for iptables snat %s to be updated", timeout, snat.Name)
	rv, _ := big.NewInt(0).SetString(snat.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(ctx, snat.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("iptables snat %s was not updated within %v", snat.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified iptables SNAT rule to disappear.
func (c *IptablesSnatClient) WaitToDisappear(ctx context.Context, name string, timeout time.Duration) error {
	err := framework.Gomega().Eventually(ctx, framework.HandleRetry(func(ctx context.Context) (*apiv1.IptablesSnatRule, error) {
		rule, err := c.IptablesSnatRuleInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return rule, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected iptables SNAT rule %s to not be found: %w", name, err)
	}
	return nil
}

func MakeIptablesSnatRule(name, eip, internalCIDR string) *apiv1.IptablesSnatRule {
	snat := &apiv1.IptablesSnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.IptablesSnatRuleSpec{
			EIP:          eip,
			InternalCIDR: internalCIDR,
		},
	}
	return snat
}
