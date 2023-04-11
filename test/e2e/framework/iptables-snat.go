package framework

import (
	"context"
	"fmt"
	"math/big"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

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

func (s *IptablesSnatClient) Get(name string) *apiv1.IptablesSnatRule {
	snat, err := s.IptablesSnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return snat
}

// Create creates a new iptables snat according to the framework specifications
func (c *IptablesSnatClient) Create(snat *apiv1.IptablesSnatRule) *apiv1.IptablesSnatRule {
	snat, err := c.IptablesSnatRuleInterface.Create(context.TODO(), snat, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating iptables snat")
	return snat.DeepCopy()
}

// CreateSync creates a new iptables snat according to the framework specifications, and waits for it to be ready.
func (c *IptablesSnatClient) CreateSync(snat *apiv1.IptablesSnatRule) *apiv1.IptablesSnatRule {
	snat = c.Create(snat)
	ExpectTrue(c.WaitToBeReady(snat.Name, timeout))
	// Get the newest iptables snat after it becomes ready
	return c.Get(snat.Name).DeepCopy()
}

// Patch patches the iptables snat
func (c *IptablesSnatClient) Patch(original, modified *apiv1.IptablesSnatRule) *apiv1.IptablesSnatRule {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIptablesSnatRule *apiv1.IptablesSnatRule
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		snat, err := c.IptablesSnatRuleInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch iptables snat %q", original.Name)
		}
		patchedIptablesSnatRule = snat
		return true, nil
	})
	if err == nil {
		return patchedIptablesSnatRule.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch iptables snat %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching iptables snat %s", original.Name))

	return nil
}

// PatchSync patches the iptables snat and waits for the iptables snat to be ready for `timeout`.
// If the iptables snat doesn't become ready before the timeout, it will fail the test.
func (c *IptablesSnatClient) PatchSync(original, modified *apiv1.IptablesSnatRule, requiredNodes []string, timeout time.Duration) *apiv1.IptablesSnatRule {
	snat := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(snat, timeout))
	ExpectTrue(c.WaitToBeReady(snat.Name, timeout))
	// Get the newest iptables snat after it becomes ready
	return c.Get(snat.Name).DeepCopy()
}

// Delete deletes a iptables snat if the iptables snat exists
func (c *IptablesSnatClient) Delete(name string) {
	err := c.IptablesSnatRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete iptables snat %q: %v", name, err)
	}
}

// DeleteSync deletes the iptables snat and waits for the iptables snat to disappear for `timeout`.
// If the iptables snat doesn't disappear before the timeout, it will fail the test.
func (c *IptablesSnatClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for iptables snat %q to disappear", name)
}

// WaitToBeReady returns whether the iptables snat is ready within timeout.
func (c *IptablesSnatClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("snat %s is ready ", name)
			return true
		}
		Logf("snat %s is not ready ", name)
	}
	return false
}

// WaitToBeUpdated returns whether the iptables snat is updated within timeout.
func (c *IptablesSnatClient) WaitToBeUpdated(snat *apiv1.IptablesSnatRule, timeout time.Duration) bool {
	Logf("Waiting up to %v for iptables snat %s to be updated", timeout, snat.Name)
	rv, _ := big.NewInt(0).SetString(snat.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(snat.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("iptables snat %s was not updated within %v", snat.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified iptables snat to disappear.
func (c *IptablesSnatClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastIptablesSnatRule *apiv1.IptablesSnatRule
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for iptables snat %s to disappear", name)
		_, err := c.IptablesSnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Logf("iptables snat %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for iptables snat %s to disappear", name),
			lastIptablesSnatRule,
		)
	}
	return maybeTimeoutError(err, "waiting for iptables snat %s to disappear", name)
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
