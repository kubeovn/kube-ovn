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

// OvnDnatRuleClient is a struct for ovn dnat client.
type OvnDnatRuleClient struct {
	f *Framework
	v1.OvnDnatRuleInterface
}

func (f *Framework) OvnDnatRuleClient() *OvnDnatRuleClient {
	return &OvnDnatRuleClient{
		f:                    f,
		OvnDnatRuleInterface: f.KubeOVNClientSet.KubeovnV1().OvnDnatRules(),
	}
}

func (c *OvnDnatRuleClient) Get(name string) *apiv1.OvnDnatRule {
	dnat, err := c.OvnDnatRuleInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return dnat
}

// Create creates a new ovn dnat according to the framework specifications
func (c *OvnDnatRuleClient) Create(dnat *apiv1.OvnDnatRule) *apiv1.OvnDnatRule {
	dnat, err := c.OvnDnatRuleInterface.Create(context.TODO(), dnat, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating ovn dnat")
	return dnat.DeepCopy()
}

// CreateSync creates a new ovn dnat according to the framework specifications, and waits for it to be ready.
func (c *OvnDnatRuleClient) CreateSync(dnat *apiv1.OvnDnatRule) *apiv1.OvnDnatRule {
	dnat = c.Create(dnat)
	ExpectTrue(c.WaitToBeReady(dnat.Name, timeout))
	// Get the newest ovn dnat after it becomes ready
	return c.Get(dnat.Name).DeepCopy()
}

// Patch patches the ovn dnat
func (c *OvnDnatRuleClient) Patch(original, modified *apiv1.OvnDnatRule) *apiv1.OvnDnatRule {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedOvnDnatRule *apiv1.OvnDnatRule
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		dnat, err := c.OvnDnatRuleInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch ovn dnat %q", original.Name)
		}
		patchedOvnDnatRule = dnat
		return true, nil
	})
	if err == nil {
		return patchedOvnDnatRule.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch OVN DNAT rule %s", original.Name)
	}
	Failf("error occurred while retrying to patch OVN DNAT rule %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the ovn dnat and waits for the ovn dnat to be ready for `timeout`.
// If the ovn dnat doesn't become ready before the timeout, it will fail the test.
func (c *OvnDnatRuleClient) PatchSync(original, modified *apiv1.OvnDnatRule, _ []string, timeout time.Duration) *apiv1.OvnDnatRule {
	dnat := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(dnat, timeout))
	ExpectTrue(c.WaitToBeReady(dnat.Name, timeout))
	// Get the newest ovn dnat after it becomes ready
	return c.Get(dnat.Name).DeepCopy()
}

// Delete deletes a ovn dnat if the ovn dnat exists
func (c *OvnDnatRuleClient) Delete(name string) {
	err := c.OvnDnatRuleInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete ovn dnat %q: %v", name, err)
	}
}

// DeleteSync deletes the ovn dnat and waits for the ovn dnat to disappear for `timeout`.
// If the ovn dnat doesn't disappear before the timeout, it will fail the test.
func (c *OvnDnatRuleClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for ovn dnat %q to disappear", name)
}

// WaitToBeReady returns whether the ovn dnat is ready within timeout.
func (c *OvnDnatRuleClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn dnat %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("ovn dnat %s is ready", name)
			return true
		}
		Logf("ovn dnat %s is not ready", name)
	}
	Logf("ovn dnat %s was not ready within %v", name, timeout)
	return false
}

// WaitToBeUpdated returns whether the ovn dnat is updated within timeout.
func (c *OvnDnatRuleClient) WaitToBeUpdated(dnat *apiv1.OvnDnatRule, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn dnat %s to be updated", timeout, dnat.Name)
	rv, _ := big.NewInt(0).SetString(dnat.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(dnat.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("ovn dnat %s was not updated within %v", dnat.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified ovn dnat to disappear.
func (c *OvnDnatRuleClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.OvnDnatRule, error) {
		rule, err := c.OvnDnatRuleInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return rule, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected OVN DNAT rule %s to not be found: %w", name, err)
	}
	return nil
}

func MakeOvnDnatRule(name, ovnEip, ipType, ipName, vpc, v4Ip, internalPort, externalPort, protocol string) *apiv1.OvnDnatRule {
	dnat := &apiv1.OvnDnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.OvnDnatRuleSpec{
			OvnEip:       ovnEip,
			IPType:       ipType,
			IPName:       ipName,
			Vpc:          vpc,
			V4Ip:         v4Ip,
			InternalPort: internalPort,
			ExternalPort: externalPort,
			Protocol:     protocol,
		},
	}
	return dnat
}
