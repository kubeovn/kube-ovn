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

// OvnEipClient is a struct for ovn eip client.
type OvnEipClient struct {
	f *Framework
	v1.OvnEipInterface
}

func (f *Framework) OvnEipClient() *OvnEipClient {
	return &OvnEipClient{
		f:               f,
		OvnEipInterface: f.KubeOVNClientSet.KubeovnV1().OvnEips(),
	}
}

func (s *OvnEipClient) Get(name string) *apiv1.OvnEip {
	eip, err := s.OvnEipInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return eip
}

// Create creates a new ovn eip according to the framework specifications
func (c *OvnEipClient) Create(eip *apiv1.OvnEip) *apiv1.OvnEip {
	eip, err := c.OvnEipInterface.Create(context.TODO(), eip, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating ovn eip")
	return eip.DeepCopy()
}

// CreateSync creates a new ovn eip according to the framework specifications, and waits for it to be ready.
func (c *OvnEipClient) CreateSync(eip *apiv1.OvnEip) *apiv1.OvnEip {
	eip = c.Create(eip)
	ExpectTrue(c.WaitToBeReady(eip.Name, timeout))
	// Get the newest ovn eip after it becomes ready
	return c.Get(eip.Name).DeepCopy()
}

// Patch patches the ovn eip
func (c *OvnEipClient) Patch(original, modified *apiv1.OvnEip) *apiv1.OvnEip {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedOvnEip *apiv1.OvnEip
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		eip, err := c.OvnEipInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch ovn eip %q", original.Name)
		}
		patchedOvnEip = eip
		return true, nil
	})
	if err == nil {
		return patchedOvnEip.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch ovn eip %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching ovn eip %s", original.Name))

	return nil
}

// PatchSync patches the ovn eip and waits for the ovn eip to be ready for `timeout`.
// If the ovn eip doesn't become ready before the timeout, it will fail the test.
func (c *OvnEipClient) PatchSync(original, modified *apiv1.OvnEip, requiredNodes []string, timeout time.Duration) *apiv1.OvnEip {
	eip := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(eip, timeout))
	ExpectTrue(c.WaitToBeReady(eip.Name, timeout))
	// Get the newest ovn eip after it becomes ready
	return c.Get(eip.Name).DeepCopy()
}

// Delete deletes a ovn eip if the ovn eip exists
func (c *OvnEipClient) Delete(name string) {
	err := c.OvnEipInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete ovn eip %q: %v", name, err)
	}
}

// DeleteSync deletes the ovn eip and waits for the ovn eip to disappear for `timeout`.
// If the ovn eip doesn't disappear before the timeout, it will fail the test.
func (c *OvnEipClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for ovn eip %q to disappear", name)
}

// WaitToBeReady returns whether the ovn eip is ready within timeout.
func (c *OvnEipClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn eip %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Ready {
			Logf("ovn eip %s is ready ", name)
			return true
		}
		Logf("ovn eip %s is not ready ", name)
	}
	Logf("ovn eip %s was not ready within %v", name, timeout)
	return false
}

// WaitToBeUpdated returns whether the ovn eip is updated within timeout.
func (c *OvnEipClient) WaitToBeUpdated(eip *apiv1.OvnEip, timeout time.Duration) bool {
	Logf("Waiting up to %v for ovn eip %s to be updated", timeout, eip.Name)
	rv, _ := big.NewInt(0).SetString(eip.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(eip.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("ovn eip %s was not updated within %v", eip.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified ovn eip to disappear.
func (c *OvnEipClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastOvnEip *apiv1.OvnEip
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for ovn eip %s to disappear", name)
		_, err := c.OvnEipInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Logf("ovn eip %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for ovn eip %s to disappear", name),
			lastOvnEip,
		)
	}
	return maybeTimeoutError(err, "waiting for ovn eip %s to disappear", name)
}

func MakeOvnEip(name, subnet, v4ip, v6ip, mac, usage string) *apiv1.OvnEip {
	eip := &apiv1.OvnEip{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.OvnEipSpec{
			ExternalSubnet: subnet,
			V4Ip:           v4ip,
			V6Ip:           v6ip,
			MacAddress:     mac,
			Type:           usage,
		},
	}
	return eip
}
