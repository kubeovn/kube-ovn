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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// IPClient is a struct for IP client.
type IPClient struct {
	f *Framework
	v1.IPInterface
}

func (f *Framework) IPClient() *IPClient {
	return &IPClient{
		f:           f,
		IPInterface: f.KubeOVNClientSet.KubeovnV1().IPs(),
	}
}

func (c *IPClient) Get(ctx context.Context, name string) *apiv1.IP {
	ginkgo.GinkgoHelper()
	IP, err := c.IPInterface.Get(ctx, name, metav1.GetOptions{})
	ExpectNoError(err)
	return IP.DeepCopy()
}

// Create creates a new IP according to the framework specifications
func (c *IPClient) Create(ctx context.Context, iP *apiv1.IP) *apiv1.IP {
	ginkgo.GinkgoHelper()
	iP, err := c.IPInterface.Create(ctx, iP, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating IP")
	return iP.DeepCopy()
}

// CreateSync creates a new IP according to the framework specifications, and waits for it to be ready.
func (c *IPClient) CreateSync(ctx context.Context, iP *apiv1.IP) *apiv1.IP {
	ginkgo.GinkgoHelper()

	iP = c.Create(ctx, iP)
	ExpectTrue(c.WaitToBeReady(ctx, iP.Name, timeout))
	// Get the newest IP after it becomes ready
	return c.Get(ctx, iP.Name).DeepCopy()
}

// WaitToBeReady returns whether the IP is ready within timeout.
func (c *IPClient) WaitToBeReady(ctx context.Context, name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for IP %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		ip := c.Get(ctx, name)
		if ip.Spec.V4IPAddress != "" || ip.Spec.V6IPAddress != "" {
			Logf("IP %s is ready", name)
			return true
		}
		Logf("IP %s is not ready", name)
	}
	Logf("IP %s was not ready within %v", name, timeout)
	return false
}

// Patch patches the IP
func (c *IPClient) Patch(ctx context.Context, original, modified *apiv1.IP, timeout time.Duration) *apiv1.IP {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIP *apiv1.IP
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		p, err := c.IPInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch IP %q", original.Name)
		}
		patchedIP = p
		return true, nil
	})
	if err == nil {
		return patchedIP.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch IP %s", original.Name)
	}
	Failf("error occurred while retrying to patch IP %s: %v", original.Name, err)

	return nil
}

// Delete deletes a IP if the IP exists
func (c *IPClient) Delete(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := c.IPInterface.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete IP %q: %v", name, err)
	}
}

// DeleteSync deletes the IP and waits for the IP to disappear for `timeout`.
// If the IP doesn't disappear before the timeout, it will fail the test.
func (c *IPClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.Delete(ctx, name)
	gomega.Expect(c.WaitToDisappear(ctx, name, timeout)).To(gomega.Succeed(), "wait for ovn eip %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified IP to disappear.
func (c *IPClient) WaitToDisappear(ctx context.Context, name string, timeout time.Duration) error {
	err := framework.Gomega().Eventually(ctx, framework.HandleRetry(func(ctx context.Context) (*apiv1.IP, error) {
		ip, err := c.IPInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return ip, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected IP %s to not be found: %w", name, err)
	}
	return nil
}

func MakeIP(name, ns, subnet string) *apiv1.IP {
	// pod ip name should including: pod name and namespace
	// node ip name: only node name
	IP := &apiv1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.IPSpec{
			Namespace: ns,
			Subnet:    subnet,
		},
	}
	return IP
}
