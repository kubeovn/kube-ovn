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

// IptablesFIPClient is a struct for iptables fip client.
type IptablesFIPClient struct {
	f *Framework
	v1.IptablesFIPRuleInterface
}

func (f *Framework) IptablesFIPClient() *IptablesFIPClient {
	return &IptablesFIPClient{
		f:                        f,
		IptablesFIPRuleInterface: f.KubeOVNClientSet.KubeovnV1().IptablesFIPRules(),
	}
}

func (s *IptablesFIPClient) Get(name string) *apiv1.IptablesFIPRule {
	fip, err := s.IptablesFIPRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return fip
}

// Create creates a new iptables fip according to the framework specifications
func (c *IptablesFIPClient) Create(fip *apiv1.IptablesFIPRule) *apiv1.IptablesFIPRule {
	fip, err := c.IptablesFIPRuleInterface.Create(context.TODO(), fip, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating iptables fip")
	return fip.DeepCopy()
}

// CreateSync creates a new iptables fip according to the framework specifications, and waits for it to be ready.
func (c *IptablesFIPClient) CreateSync(fip *apiv1.IptablesFIPRule) *apiv1.IptablesFIPRule {
	fip = c.Create(fip)
	ExpectTrue(c.WaitToBeReady(fip.Name, timeout))
	// Get the newest iptables fip after it becomes ready
	return c.Get(fip.Name).DeepCopy()
}

// Patch patches the iptables fip
func (c *IptablesFIPClient) Patch(original, modified *apiv1.IptablesFIPRule) *apiv1.IptablesFIPRule {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIptablesFIPRule *apiv1.IptablesFIPRule
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		fip, err := c.IptablesFIPRuleInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch iptables fip %q", original.Name)
		}
		patchedIptablesFIPRule = fip
		return true, nil
	})
	if err == nil {
		return patchedIptablesFIPRule.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch iptables fip %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching iptables fip %s", original.Name))

	return nil
}

// PatchSync patches the iptables fip and waits for the iptables fip to be ready for `timeout`.
// If the iptables fip doesn't become ready before the timeout, it will fail the test.
func (c *IptablesFIPClient) PatchSync(original, modified *apiv1.IptablesFIPRule, requiredNodes []string, timeout time.Duration) *apiv1.IptablesFIPRule {
	fip := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(fip, timeout))
	ExpectTrue(c.WaitToBeReady(fip.Name, timeout))
	// Get the newest iptables fip after it becomes ready
	return c.Get(fip.Name).DeepCopy()
}

// Delete deletes a iptables fip if the iptables fip exists
func (c *IptablesFIPClient) Delete(name string) {
	err := c.IptablesFIPRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete iptables fip %q: %v", name, err)
	}
}

// DeleteSync deletes the iptables fip and waits for the iptables fip to disappear for `timeout`.
// If the iptables fip doesn't disappear before the timeout, it will fail the test.
func (c *IptablesFIPClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for iptables fip %q to disappear", name)
}

// WaitToBeReady returns whether the iptables fip is ready within timeout.
func (c *IptablesFIPClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("fip %s is ready ", name)
			return true
		}
		Logf("fip %s is not ready ", name)
	}
	return false
}

// WaitToBeUpdated returns whether the iptables fip is updated within timeout.
func (c *IptablesFIPClient) WaitToBeUpdated(fip *apiv1.IptablesFIPRule, timeout time.Duration) bool {
	Logf("Waiting up to %v for iptables fip %s to be updated", timeout, fip.Name)
	rv, _ := big.NewInt(0).SetString(fip.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(fip.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("iptables fip %s was not updated within %v", fip.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified iptables fip to disappear.
func (c *IptablesFIPClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastIptablesFIPRule *apiv1.IptablesFIPRule
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for iptables fip %s to disappear", name)
		_, err := c.IptablesFIPRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Logf("iptables fip %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for iptables fip %s to disappear", name),
			lastIptablesFIPRule,
		)
	}
	return maybeTimeoutError(err, "waiting for iptables fip %s to disappear", name)
}

func MakeIptablesFIPRule(name, eip, internalIp string) *apiv1.IptablesFIPRule {
	fip := &apiv1.IptablesFIPRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.IptablesFIPRuleSpec{
			EIP:        eip,
			InternalIp: internalIp,
		},
	}
	return fip
}
