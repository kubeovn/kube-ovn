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

// OvnFipClient is a struct for ovn fip client.
type OvnFipClient struct {
	f *Framework
	v1.OvnFipInterface
}

func (f *Framework) OvnFipClient() *OvnFipClient {
	return &OvnFipClient{
		f:               f,
		OvnFipInterface: f.KubeOVNClientSet.KubeovnV1().OvnFips(),
	}
}

func (c *OvnFipClient) Get(ctx context.Context, name string) *apiv1.OvnFip {
	ginkgo.GinkgoHelper()
	fip, err := c.OvnFipInterface.Get(ctx, name, metav1.GetOptions{})
	ExpectNoError(err)
	return fip
}

// Create creates a new ovn fip according to the framework specifications
func (c *OvnFipClient) Create(ctx context.Context, fip *apiv1.OvnFip) *apiv1.OvnFip {
	ginkgo.GinkgoHelper()
	fip, err := c.OvnFipInterface.Create(ctx, fip, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating ovn fip")
	return fip.DeepCopy()
}

// CreateSync creates a new ovn fip according to the framework specifications, and waits for it to be ready.
func (c *OvnFipClient) CreateSync(ctx context.Context, fip *apiv1.OvnFip) *apiv1.OvnFip {
	ginkgo.GinkgoHelper()

	fip = c.Create(ctx, fip)
	ExpectTrue(c.WaitToBeReady(ctx, fip.Name, timeout))
	// Get the newest ovn fip after it becomes ready
	return c.Get(ctx, fip.Name).DeepCopy()
}

// Patch patches the ovn fip
func (c *OvnFipClient) Patch(ctx context.Context, original, modified *apiv1.OvnFip) *apiv1.OvnFip {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedOvnFip *apiv1.OvnFip
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		fip, err := c.OvnFipInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch ovn fip %q", original.Name)
		}
		patchedOvnFip = fip
		return true, nil
	})
	if err == nil {
		return patchedOvnFip.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch OVN FIP %s", original.Name)
	}
	Failf("error occurred while retrying to patch OVN FIP %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the ovn fip and waits for the ovn fip to be ready for `timeout`.
// If the ovn fip doesn't become ready before the timeout, it will fail the test.
func (c *OvnFipClient) PatchSync(ctx context.Context, original, modified *apiv1.OvnFip, timeout time.Duration) *apiv1.OvnFip {
	ginkgo.GinkgoHelper()

	fip := c.Patch(ctx, original, modified)
	ExpectTrue(c.WaitToBeUpdated(ctx, fip, timeout))
	ExpectTrue(c.WaitToBeReady(ctx, fip.Name, timeout))
	// Get the newest ovn fip after it becomes ready
	return c.Get(ctx, fip.Name).DeepCopy()
}

// Delete deletes a ovn fip if the ovn fip exists
func (c *OvnFipClient) Delete(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := c.OvnFipInterface.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete ovn fip %q: %v", name, err)
	}
}

// DeleteSync deletes the ovn fip and waits for the ovn fip to disappear for `timeout`.
// If the ovn fip doesn't disappear before the timeout, it will fail the test.
func (c *OvnFipClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.Delete(ctx, name)
	gomega.Expect(c.WaitToDisappear(ctx, name, timeout)).To(gomega.Succeed(), "wait for ovn fip %q to disappear", name)
}

// WaitToBeReady returns whether the ovn fip is ready within timeout.
func (c *OvnFipClient) WaitToBeReady(ctx context.Context, name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn fip %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(ctx, name).Status.Ready {
			Logf("ovn fip %s is ready", name)
			return true
		}
		Logf("ovn fip %s is not ready", name)
	}
	Logf("ovn fip %s was not ready within %v", name, timeout)
	return false
}

// WaitToBeUpdated returns whether the ovn fip is updated within timeout.
func (c *OvnFipClient) WaitToBeUpdated(ctx context.Context, fip *apiv1.OvnFip, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn fip %s to be updated", timeout, fip.Name)
	rv, _ := big.NewInt(0).SetString(fip.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(ctx, fip.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("ovn fip %s was not updated within %v", fip.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified ovn fip to disappear.
func (c *OvnFipClient) WaitToDisappear(ctx context.Context, name string, timeout time.Duration) error {
	err := framework.Gomega().Eventually(ctx, framework.HandleRetry(func(ctx context.Context) (*apiv1.OvnFip, error) {
		fip, err := c.OvnFipInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return fip, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected OVN FIP %s to not be found: %w", name, err)
	}
	return nil
}

func MakeOvnFip(name, ovnEip, ipType, ipName, vpc, v4Ip string) *apiv1.OvnFip {
	fip := &apiv1.OvnFip{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.OvnFipSpec{
			OvnEip: ovnEip,
			IPType: ipType,
			IPName: ipName,
			Vpc:    vpc,
			V4Ip:   v4Ip,
		},
	}
	return fip
}
