package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/test/e2e/framework"

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

func (c *SwitchLBRuleClient) Get(name string) *apiv1.SwitchLBRule {
	rules, err := c.SwitchLBRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return rules
}

// Create creates a new switch-lb-rule according to the framework specifications
func (c *SwitchLBRuleClient) Create(rule *apiv1.SwitchLBRule) *apiv1.SwitchLBRule {
	e, err := c.SwitchLBRuleInterface.Create(context.TODO(), rule, metav1.CreateOptions{})
	ExpectNoError(err, "error creating switch-lb-rule")
	return e.DeepCopy()
}

// CreateSync creates a new switch-lb-rule according to the framework specifications, and waits for it to be updated.
func (c *SwitchLBRuleClient) CreateSync(rule *apiv1.SwitchLBRule, cond func(s *apiv1.SwitchLBRule) (bool, error), condDesc string) *apiv1.SwitchLBRule {
	_ = c.Create(rule)
	return c.WaitUntil(rule.Name, cond, condDesc, 2*time.Second, timeout)
}

// Patch patches the switch-lb-rule
func (c *SwitchLBRuleClient) Patch(original, modified *apiv1.SwitchLBRule) *apiv1.SwitchLBRule {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedService *apiv1.SwitchLBRule
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(_ context.Context) (bool, error) {
		s, err := c.SwitchLBRuleInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
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
func (c *SwitchLBRuleClient) PatchSync(original, modified *apiv1.SwitchLBRule, cond func(s *apiv1.SwitchLBRule) (bool, error), condDesc string) *apiv1.SwitchLBRule {
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, cond, condDesc, 2*time.Second, timeout)
}

// Delete deletes a switch-lb-rule if the switch-lb-rule exists
func (c *SwitchLBRuleClient) Delete(name string) {
	err := c.SwitchLBRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete switch-lb-rule %q: %v", name, err)
	}
}

// DeleteSync deletes the switch-lb-rule and waits for the switch-lb-rule to disappear for `timeout`.
// If the switch-lb-rule doesn't disappear before the timeout, it will fail the test.
func (c *SwitchLBRuleClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for switch-lb-rule %q to disappear", name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *SwitchLBRuleClient) WaitUntil(name string, cond func(s *apiv1.SwitchLBRule) (bool, error), condDesc string, _, timeout time.Duration) *apiv1.SwitchLBRule {
	var rules *apiv1.SwitchLBRule
	err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(_ context.Context) (bool, error) {
		Logf("Waiting for switch-lb-rule %s to meet condition %q", name, condDesc)
		rules = c.Get(name).DeepCopy()
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
func (c *SwitchLBRuleClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.SwitchLBRule, error) {
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
