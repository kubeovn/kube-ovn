package framework

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
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

// IPPoolClient is a struct for ippool client.
type IPPoolClient struct {
	f *Framework
	v1.IPPoolInterface
}

func (f *Framework) IPPoolClient() *IPPoolClient {
	return &IPPoolClient{
		f:               f,
		IPPoolInterface: f.KubeOVNClientSet.KubeovnV1().IPPools(),
	}
}

func (c *IPPoolClient) Get(name string) *apiv1.IPPool {
	ippool, err := c.IPPoolInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return ippool
}

// Create creates a new ippool according to the framework specifications
func (c *IPPoolClient) Create(ippool *apiv1.IPPool) *apiv1.IPPool {
	s, err := c.IPPoolInterface.Create(context.TODO(), ippool, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating ippool")
	return s.DeepCopy()
}

// CreateSync creates a new ippool according to the framework specifications, and waits for it to be ready.
func (c *IPPoolClient) CreateSync(ippool *apiv1.IPPool) *apiv1.IPPool {
	s := c.Create(ippool)
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest ippool after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Update updates the ippool
func (c *IPPoolClient) Update(ippool *apiv1.IPPool, options metav1.UpdateOptions, timeout time.Duration) *apiv1.IPPool {
	var updatedIPPool *apiv1.IPPool
	err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		s, err := c.IPPoolInterface.Update(ctx, ippool, options)
		if err != nil {
			return handleWaitingAPIError(err, false, "update ippool %q", ippool.Name)
		}
		updatedIPPool = s
		return true, nil
	})
	if err == nil {
		return updatedIPPool.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to update ippool %s", ippool.Name)
	}
	Failf("error occurred while retrying to update ippool %s: %v", ippool.Name, err)

	return nil
}

// UpdateSync updates the ippool and waits for the ippool to be ready for `timeout`.
// If the ippool doesn't become ready before the timeout, it will fail the test.
func (c *IPPoolClient) UpdateSync(ippool *apiv1.IPPool, options metav1.UpdateOptions, timeout time.Duration) *apiv1.IPPool {
	s := c.Update(ippool, options, timeout)
	ExpectTrue(c.WaitToBeUpdated(s, timeout))
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest ippool after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Patch patches the ippool
func (c *IPPoolClient) Patch(original, modified *apiv1.IPPool, timeout time.Duration) *apiv1.IPPool {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIPPool *apiv1.IPPool
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		s, err := c.IPPoolInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch ippool %q", original.Name)
		}
		patchedIPPool = s
		return true, nil
	})
	if err == nil {
		return patchedIPPool.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch ippool %s", original.Name)
	}
	Failf("error occurred while retrying to patch ippool %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the ippool and waits for the ippool to be ready for `timeout`.
// If the ippool doesn't become ready before the timeout, it will fail the test.
func (c *IPPoolClient) PatchSync(original, modified *apiv1.IPPool) *apiv1.IPPool {
	s := c.Patch(original, modified, timeout)
	ExpectTrue(c.WaitToBeUpdated(s, timeout))
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest ippool after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Delete deletes a ippool if the ippool exists
func (c *IPPoolClient) Delete(name string) {
	err := c.IPPoolInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete ippool %q: %v", name, err)
	}
}

// DeleteSync deletes the ippool and waits for the ippool to disappear for `timeout`.
// If the ippool doesn't disappear before the timeout, it will fail the test.
func (c *IPPoolClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for ippool %q to disappear", name)
}

func isIPPoolConditionSetAsExpected(ippool *apiv1.IPPool, conditionType apiv1.ConditionType, wantTrue, silent bool) bool {
	for _, cond := range ippool.Status.Conditions {
		if cond.Type == conditionType {
			if (wantTrue && (cond.Status == corev1.ConditionTrue)) || (!wantTrue && (cond.Status != corev1.ConditionTrue)) {
				return true
			}
			if !silent {
				Logf("Condition %s of ippool %s is %v instead of %t. Reason: %v, message: %v",
					conditionType, ippool.Name, cond.Status == corev1.ConditionTrue, wantTrue, cond.Reason, cond.Message)
			}
			return false
		}
	}
	if !silent {
		Logf("Couldn't find condition %v on ippool %v", conditionType, ippool.Name)
	}
	return false
}

// IsIPPoolConditionSetAsExpected returns a wantTrue value if the ippool has a match to the conditionType,
// otherwise returns an opposite value of the wantTrue with detailed logging.
func IsIPPoolConditionSetAsExpected(ippool *apiv1.IPPool, conditionType apiv1.ConditionType, wantTrue bool) bool {
	return isIPPoolConditionSetAsExpected(ippool, conditionType, wantTrue, false)
}

// WaitConditionToBe returns whether ippool "name's" condition state matches wantTrue
// within timeout. If wantTrue is true, it will ensure the ippool condition status is
// ConditionTrue; if it's false, it ensures the ippool condition is in any state other
// than ConditionTrue (e.g. not true or unknown).
func (c *IPPoolClient) WaitConditionToBe(name string, conditionType apiv1.ConditionType, wantTrue bool, timeout time.Duration) bool {
	Logf("Waiting up to %v for ippool %s condition %s to be %t", timeout, name, conditionType, wantTrue)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		ippool := c.Get(name)
		if IsIPPoolConditionSetAsExpected(ippool, conditionType, wantTrue) {
			Logf("IPPool %s reach desired %t condition status", name, wantTrue)
			return true
		}
		Logf("IPPool %s still not reach desired %t condition status", name, wantTrue)
	}
	Logf("IPPool %s didn't reach desired %s condition status (%t) within %v", name, conditionType, wantTrue, timeout)
	return false
}

// WaitToBeReady returns whether the ippool is ready within timeout.
func (c *IPPoolClient) WaitToBeReady(name string, timeout time.Duration) bool {
	return c.WaitConditionToBe(name, apiv1.Ready, true, timeout)
}

// WaitToBeUpdated returns whether the ippool is updated within timeout.
func (c *IPPoolClient) WaitToBeUpdated(ippool *apiv1.IPPool, timeout time.Duration) bool {
	Logf("Waiting up to %v for ippool %s to be updated", timeout, ippool.Name)
	rv, _ := big.NewInt(0).SetString(ippool.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(ippool.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			Logf("IPPool %s updated", ippool.Name)
			return true
		}
		Logf("IPPool %s still not updated", ippool.Name)
	}
	Logf("IPPool %s was not updated within %v", ippool.Name, timeout)
	return false
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *IPPoolClient) WaitUntil(name string, cond func(s *apiv1.IPPool) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.IPPool {
	var ippool *apiv1.IPPool
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, true, func(_ context.Context) (bool, error) {
		Logf("Waiting for ippool %s to meet condition %q", name, condDesc)
		ippool = c.Get(name).DeepCopy()
		met, err := cond(ippool)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for ippool %s: %v", name, err)
		}
		return met, nil
	})
	if err == nil {
		return ippool
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while waiting for ippool %s to meet condition %q", name, condDesc)
	}
	Failf("error occurred while waiting for ippool %s to meet condition %q: %v", name, condDesc, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified ippool to disappear.
func (c *IPPoolClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.IPPool, error) {
		ippool, err := c.IPPoolInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return ippool, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected ippool %s to not be found: %w", name, err)
	}
	return nil
}

func MakeIPPool(name, subnet string, ips, namespaces []string) *apiv1.IPPool {
	return &apiv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.IPPoolSpec{
			Subnet:     subnet,
			IPs:        ips,
			Namespaces: namespaces,
		},
	}
}
