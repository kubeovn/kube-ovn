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

// OvnSnatRuleClient is a struct for ovn snat client.
type OvnSnatRuleClient struct {
	f *Framework
	v1.OvnSnatRuleInterface
}

func (f *Framework) OvnSnatRuleClient() *OvnSnatRuleClient {
	return &OvnSnatRuleClient{
		f:                    f,
		OvnSnatRuleInterface: f.KubeOVNClientSet.KubeovnV1().OvnSnatRules(),
	}
}

func (s *OvnSnatRuleClient) Get(name string) *apiv1.OvnSnatRule {
	snat, err := s.OvnSnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return snat
}

// Create creates a new ovn snat according to the framework specifications
func (c *OvnSnatRuleClient) Create(snat *apiv1.OvnSnatRule) *apiv1.OvnSnatRule {
	snat, err := c.OvnSnatRuleInterface.Create(context.TODO(), snat, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating ovn snat")
	return snat.DeepCopy()
}

// CreateSync creates a new ovn snat according to the framework specifications, and waits for it to be ready.
func (c *OvnSnatRuleClient) CreateSync(snat *apiv1.OvnSnatRule) *apiv1.OvnSnatRule {
	snat = c.Create(snat)
	ExpectTrue(c.WaitToBeReady(snat.Name, timeout))
	// Get the newest ovn snat after it becomes ready
	return c.Get(snat.Name).DeepCopy()
}

// Patch patches the ovn snat
func (c *OvnSnatRuleClient) Patch(original, modified *apiv1.OvnSnatRule) *apiv1.OvnSnatRule {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedOvnSnatRule *apiv1.OvnSnatRule
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		snat, err := c.OvnSnatRuleInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch ovn snat %q", original.Name)
		}
		patchedOvnSnatRule = snat
		return true, nil
	})
	if err == nil {
		return patchedOvnSnatRule.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch ovn snat %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching ovn snat %s", original.Name))

	return nil
}

// PatchSync patches the ovn snat and waits for the ovn snat to be ready for `timeout`.
// If the ovn snat doesn't become ready before the timeout, it will fail the test.
func (c *OvnSnatRuleClient) PatchSync(original, modified *apiv1.OvnSnatRule, requiredNodes []string, timeout time.Duration) *apiv1.OvnSnatRule {
	snat := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(snat, timeout))
	ExpectTrue(c.WaitToBeReady(snat.Name, timeout))
	// Get the newest ovn snat after it becomes ready
	return c.Get(snat.Name).DeepCopy()
}

// Delete deletes a ovn snat if the ovn snat exists
func (c *OvnSnatRuleClient) Delete(name string) {
	err := c.OvnSnatRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete ovn snat %q: %v", name, err)
	}
}

// DeleteSync deletes the ovn snat and waits for the ovn snat to disappear for `timeout`.
// If the ovn snat doesn't disappear before the timeout, it will fail the test.
func (c *OvnSnatRuleClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for ovn snat %q to disappear", name)
}

// WaitToBeReady returns whether the ovn snat is ready within timeout.
func (c *OvnSnatRuleClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn snat %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("ovn snat %s is ready ", name)
			return true
		}
		Logf("ovn snat %s is not ready ", name)
	}
	Logf("ovn snat %s was not ready within %v", name, timeout)
	return false
}

// WaitToBeUpdated returns whether the ovn snat is updated within timeout.
func (c *OvnSnatRuleClient) WaitToBeUpdated(snat *apiv1.OvnSnatRule, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn snat %s to be updated", timeout, snat.Name)
	rv, _ := big.NewInt(0).SetString(snat.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(snat.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("ovn snat %s was not updated within %v", snat.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified ovn snat to disappear.
func (c *OvnSnatRuleClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastOvnSnatRule *apiv1.OvnSnatRule
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for ovn snat %s to disappear", name)
		_, err := c.OvnSnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Logf("ovn snat %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for ovn snat %s to disappear", name),
			lastOvnSnatRule,
		)
	}
	return maybeTimeoutError(err, "waiting for ovn snat %s to disappear", name)
}

func MakeOvnSnatRule(name, ovnEip, vpcSubnet, ipName string) *apiv1.OvnSnatRule {
	snat := &apiv1.OvnSnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.OvnSnatRuleSpec{
			OvnEip:    ovnEip,
			VpcSubnet: vpcSubnet,
			IpName:    ipName,
		},
	}
	return snat
}
