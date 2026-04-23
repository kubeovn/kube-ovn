package framework

import (
	"context"
	"errors"
	"fmt"
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

// RouterLBRuleClient is a struct for router-lb-rule client.
type RouterLBRuleClient struct {
	f *Framework
	v1.RouterLBRuleInterface
}

func (f *Framework) RouterLBRuleClient() *RouterLBRuleClient {
	return &RouterLBRuleClient{
		f:                    f,
		RouterLBRuleInterface: f.KubeOVNClientSet.KubeovnV1().RouterLBRules(),
	}
}

func (c *RouterLBRuleClient) Get(name string) *apiv1.RouterLBRule {
	ginkgo.GinkgoHelper()
	rule, err := c.RouterLBRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return rule.DeepCopy()
}

// Create creates a new router-lb-rule.
func (c *RouterLBRuleClient) Create(rule *apiv1.RouterLBRule) *apiv1.RouterLBRule {
	ginkgo.GinkgoHelper()
	r, err := c.RouterLBRuleInterface.Create(context.TODO(), rule, metav1.CreateOptions{})
	ExpectNoError(err, "error creating router-lb-rule")
	return r.DeepCopy()
}

// CreateSync creates a new router-lb-rule and waits until Status.Service is set.
func (c *RouterLBRuleClient) CreateSync(rule *apiv1.RouterLBRule, cond func(r *apiv1.RouterLBRule) (bool, error), condDesc string) *apiv1.RouterLBRule {
	ginkgo.GinkgoHelper()
	_ = c.Create(rule)
	return c.WaitUntil(rule.Name, cond, condDesc, poll, timeout)
}

// Patch patches the router-lb-rule.
func (c *RouterLBRuleClient) Patch(original, modified *apiv1.RouterLBRule) *apiv1.RouterLBRule {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patched *apiv1.RouterLBRule
	err = wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(_ context.Context) (bool, error) {
		r, err := c.RouterLBRuleInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch router-lb-rule %q", original.Name)
		}
		patched = r
		return true, nil
	})
	if err == nil {
		return patched.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch router-lb-rule %s", original.Name)
	}
	Failf("error occurred while retrying to patch router-lb-rule %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the router-lb-rule and waits for the condition.
func (c *RouterLBRuleClient) PatchSync(original, modified *apiv1.RouterLBRule, cond func(r *apiv1.RouterLBRule) (bool, error), condDesc string) *apiv1.RouterLBRule {
	ginkgo.GinkgoHelper()
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, cond, condDesc, poll, timeout)
}

// Delete deletes a router-lb-rule if it exists.
func (c *RouterLBRuleClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.RouterLBRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete router-lb-rule %q: %v", name, err)
	}
}

// DeleteSync deletes the router-lb-rule and waits for it to disappear.
func (c *RouterLBRuleClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for router-lb-rule %q to disappear", name)
}

// WaitUntil waits for the given condition to be met.
func (c *RouterLBRuleClient) WaitUntil(name string, cond func(r *apiv1.RouterLBRule) (bool, error), condDesc string, _, timeout time.Duration) *apiv1.RouterLBRule {
	ginkgo.GinkgoHelper()

	var rule *apiv1.RouterLBRule
	err := wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(_ context.Context) (bool, error) {
		Logf("Waiting for router-lb-rule %s to meet condition %q", name, condDesc)
		rule = c.Get(name).DeepCopy()
		met, err := cond(rule)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for router-lb-rule %s: %w", name, err)
		}
		if met {
			Logf("router-lb-rule %s met condition %q", name, condDesc)
		}
		return met, nil
	})
	if err == nil {
		return rule
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out waiting for router-lb-rule %s to meet condition %q", name, condDesc)
	}
	Failf("error waiting for router-lb-rule %s: %v", name, err)

	return nil
}

// WaitToDisappear waits for the router-lb-rule to be deleted.
func (c *RouterLBRuleClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.RouterLBRule, error) {
		rule, err := c.RouterLBRuleInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return rule, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected router-lb-rule %s to not be found: %w", name, err)
	}
	return nil
}

// IsReady returns a condition func that checks Status.Service is set (reconciliation succeeded).
func RouterLBRuleIsReady(r *apiv1.RouterLBRule) (bool, error) {
	return r.Status.Service != "", nil
}

func MakeRouterLBRule(name, vpc, ovnEip, namespace, sessionAffinity string, selector, endpoints []string, ports []apiv1.RouterLBRulePort) *apiv1.RouterLBRule {
	return &apiv1.RouterLBRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.RouterLBRuleSpec{
			Vpc:             vpc,
			OvnEip:          ovnEip,
			Namespace:       namespace,
			Selector:        selector,
			Endpoints:       endpoints,
			SessionAffinity: sessionAffinity,
			Ports:           ports,
		},
	}
}
