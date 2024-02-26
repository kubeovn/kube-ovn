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

	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// VipClient is a struct for vip client.
type VipClient struct {
	f *Framework
	v1.VipInterface
}

func (f *Framework) VipClient() *VipClient {
	return &VipClient{
		f:            f,
		VipInterface: f.KubeOVNClientSet.KubeovnV1().Vips(),
	}
}

func (c *VipClient) Get(name string) *apiv1.Vip {
	vip, err := c.VipInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return vip.DeepCopy()
}

// Create creates a new vip according to the framework specifications
func (c *VipClient) Create(vip *apiv1.Vip) *apiv1.Vip {
	vip, err := c.VipInterface.Create(context.TODO(), vip, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vip")
	return vip.DeepCopy()
}

// CreateSync creates a new ovn vip according to the framework specifications, and waits for it to be ready.
func (c *VipClient) CreateSync(vip *apiv1.Vip) *apiv1.Vip {
	vip = c.Create(vip)
	ExpectTrue(c.WaitToBeReady(vip.Name, timeout))
	// Get the newest ovn vip after it becomes ready
	return c.Get(vip.Name).DeepCopy()
}

// WaitToBeReady returns whether the ovn vip is ready within timeout.
func (c *VipClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn vip %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("ovn vip %s is ready", name)
			return true
		}
		Logf("ovn vip %s is not ready", name)
	}
	Logf("ovn vip %s was not ready within %v", name, timeout)
	return false
}

// Patch patches the vip
func (c *VipClient) Patch(original, modified *apiv1.Vip, timeout time.Duration) *apiv1.Vip {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedVip *apiv1.Vip
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		p, err := c.VipInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vip %q", original.Name)
		}
		patchedVip = p
		return true, nil
	})
	if err == nil {
		return patchedVip.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch vip %s", original.Name)
	}
	Failf("error occurred while retrying to patch vip %s: %v", original.Name, err)

	return nil
}

// Delete deletes a vip if the vip exists
func (c *VipClient) Delete(name string) {
	err := c.VipInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vip %q: %v", name, err)
	}
}

// DeleteSync deletes the ovn vip and waits for the ovn vip to disappear for `timeout`.
// If the ovn vip doesn't disappear before the timeout, it will fail the test.
func (c *VipClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for ovn vip %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified OVN VIP to disappear.
func (c *VipClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.Vip, error) {
		vip, err := c.VipInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return vip, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected vip %s to not be found: %w", name, err)
	}
	return nil
}

func MakeVip(namespaceName, name, subnet, v4ip, v6ip, vipType string) *apiv1.Vip {
	vip := &apiv1.Vip{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.VipSpec{
			Namespace: namespaceName,
			Subnet:    subnet,
			V4ip:      v4ip,
			V6ip:      v6ip,
			Type:      vipType,
		},
	}
	return vip
}
