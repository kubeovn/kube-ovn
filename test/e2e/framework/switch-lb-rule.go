package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
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

// SwitchLBRuleClient is a struct for switch-lb-rule client.
type SwitchLBRuleClient struct {
	f *Framework
	v1.SwitchLBRuleInterface
	namespace string
}

func (f *Framework) SwitchLBRuleClient() *SwitchLBRuleClient {
	return f.SwitchLBRuleClientNS(f.Namespace.Name)
}

func (f *Framework) SwitchLBRuleClientNS(namespace string) *SwitchLBRuleClient {
	return &SwitchLBRuleClient{
		f:                     f,
		SwitchLBRuleInterface: f.KubeOVNClientSet.KubeovnV1().SwitchLBRules(),
		namespace:             namespace,
	}
}

func (c *SwitchLBRuleClient) Get(ctx context.Context, name string) *apiv1.SwitchLBRule {
	ginkgo.GinkgoHelper()
	rules, err := c.SwitchLBRuleInterface.Get(ctx, name, metav1.GetOptions{})
	ExpectNoError(err)
	return rules
}

// Create creates a new switch-lb-rule according to the framework specifications
func (c *SwitchLBRuleClient) Create(ctx context.Context, rule *apiv1.SwitchLBRule) *apiv1.SwitchLBRule {
	ginkgo.GinkgoHelper()
	e, err := c.SwitchLBRuleInterface.Create(ctx, rule, metav1.CreateOptions{})
	ExpectNoError(err, "error creating switch-lb-rule")
	return e.DeepCopy()
}

// CreateSync creates a new switch-lb-rule according to the framework specifications, and waits for it to be updated.
func (c *SwitchLBRuleClient) CreateSync(ctx context.Context, rule *apiv1.SwitchLBRule, cond func(s *apiv1.SwitchLBRule) (bool, error), condDesc string) *apiv1.SwitchLBRule {
	ginkgo.GinkgoHelper()
	_ = c.Create(ctx, rule)
	return c.WaitUntil(ctx, rule.Name, cond, condDesc, timeout)
}

// Patch patches the switch-lb-rule
func (c *SwitchLBRuleClient) Patch(ctx context.Context, original, modified *apiv1.SwitchLBRule) *apiv1.SwitchLBRule {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedService *apiv1.SwitchLBRule
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		s, err := c.SwitchLBRuleInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch switch-lb-rule %q", original.Name)
		}
		patchedService = s
		return true, nil
	})
	if err == nil {
		return patchedService.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch switch-lb-rule %s", original.Name)
	}
	Failf("error occurred while retrying to patch switch-lb-rule %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the switch-lb-rule and waits the switch-lb-rule to meet the condition
func (c *SwitchLBRuleClient) PatchSync(ctx context.Context, original, modified *apiv1.SwitchLBRule, cond func(s *apiv1.SwitchLBRule) (bool, error), condDesc string) *apiv1.SwitchLBRule {
	ginkgo.GinkgoHelper()
	_ = c.Patch(ctx, original, modified)
	return c.WaitUntil(ctx, original.Name, cond, condDesc, timeout)
}

// Delete deletes a switch-lb-rule if the switch-lb-rule exists
func (c *SwitchLBRuleClient) Delete(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := c.SwitchLBRuleInterface.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete switch-lb-rule %q: %v", name, err)
	}
}

// DeleteSync deletes the switch-lb-rule and waits for the switch-lb-rule to disappear for `timeout`.
// If the switch-lb-rule doesn't disappear before the timeout, it will fail the test.
func (c *SwitchLBRuleClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.Delete(ctx, name)
	gomega.Expect(c.WaitToDisappear(ctx, name, timeout)).To(gomega.Succeed(), "wait for switch-lb-rule %q to disappear", name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *SwitchLBRuleClient) WaitUntil(ctx context.Context, name string, cond func(s *apiv1.SwitchLBRule) (bool, error), condDesc string, timeout time.Duration) *apiv1.SwitchLBRule {
	ginkgo.GinkgoHelper()

	var rules *apiv1.SwitchLBRule
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		Logf("Waiting for switch-lb-rule %s to meet condition %q", name, condDesc)
		rules = c.Get(ctx, name).DeepCopy()
		met, err := cond(rules)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for switch-lb-rule %s: %v", name, err)
		}
		if met {
			Logf("switch-lb-rule %s met condition %q", name, condDesc)
		} else {
			Logf("switch-lb-rule %s not met condition %q", name, condDesc)
		}
		return met, nil
	})
	if err == nil {
		return rules
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch switch-lb-rule %s", name)
	}
	Failf("error occurred while retrying to patch switch-lb-rule %s: %v", name, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified switch-lb-rule to disappear.
func (c *SwitchLBRuleClient) WaitToDisappear(ctx context.Context, name string, timeout time.Duration) error {
	err := framework.Gomega().Eventually(ctx, framework.HandleRetry(func(ctx context.Context) (*apiv1.SwitchLBRule, error) {
		svc, err := c.SwitchLBRuleInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return svc, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected endpoints %s to not be found: %w", name, err)
	}
	return nil
}

func MakeSwitchLBRule(name, namespace, vip string, sessionAffinity corev1.ServiceAffinity, annotations map[string]string, slector, endpoints []string, ports []apiv1.SlrPort) *apiv1.SwitchLBRule {
	return &apiv1.SwitchLBRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: apiv1.SwitchLBRuleSpec{
			Vip:             vip,
			Namespace:       namespace,
			Selector:        slector,
			Endpoints:       endpoints,
			SessionAffinity: string(sessionAffinity),
			Ports:           ports,
		},
	}
}
