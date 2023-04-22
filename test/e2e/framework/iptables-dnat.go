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

func (s *IptablesDnatClient) Get(name string) *apiv1.IptablesDnatRule {
	dnat, err := s.IptablesDnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
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
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		dnat, err := c.IptablesDnatRuleInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch iptables dnat %q", original.Name)
		}
		patchedIptablesDnatRule = dnat
		return true, nil
	})
	if err == nil {
		return patchedIptablesDnatRule.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch iptables dnat %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching iptables dnat %s", original.Name))

	return nil
}

// PatchSync patches the iptables dnat and waits for the iptables dnat to be ready for `timeout`.
// If the iptables dnat doesn't become ready before the timeout, it will fail the test.
func (c *IptablesDnatClient) PatchSync(original, modified *apiv1.IptablesDnatRule, requiredNodes []string, timeout time.Duration) *apiv1.IptablesDnatRule {
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
			Logf("dnat %s is ready ", name)
			return true
		}
		Logf("dnat %s is not ready ", name)
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

// WaitToDisappear waits the given timeout duration for the specified iptables dnat to disappear.
func (c *IptablesDnatClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastIptablesDnatRule *apiv1.IptablesDnatRule
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for iptables dnat %s to disappear", name)
		_, err := c.IptablesDnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Logf("iptables dnat %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for iptables dnat %s to disappear", name),
			lastIptablesDnatRule,
		)
	}
	return maybeTimeoutError(err, "waiting for iptables dnat %s to disappear", name)
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
			InternalIp:   internalIP,
			InternalPort: internalPort,
		},
	}
	return dnat
}
