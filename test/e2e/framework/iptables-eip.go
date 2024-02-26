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

// IptablesEIPClient is a struct for iptables eip client.
type IptablesEIPClient struct {
	f *Framework
	v1.IptablesEIPInterface
}

func (f *Framework) IptablesEIPClient() *IptablesEIPClient {
	return &IptablesEIPClient{
		f:                    f,
		IptablesEIPInterface: f.KubeOVNClientSet.KubeovnV1().IptablesEIPs(),
	}
}

func (c *IptablesEIPClient) Get(name string) *apiv1.IptablesEIP {
	eip, err := c.IptablesEIPInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return eip
}

// Create creates a new iptables eip according to the framework specifications
func (c *IptablesEIPClient) Create(eip *apiv1.IptablesEIP) *apiv1.IptablesEIP {
	eip, err := c.IptablesEIPInterface.Create(context.TODO(), eip, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating iptables eip")
	return eip.DeepCopy()
}

// CreateSync creates a new iptables eip according to the framework specifications, and waits for it to be ready.
func (c *IptablesEIPClient) CreateSync(eip *apiv1.IptablesEIP) *apiv1.IptablesEIP {
	eip = c.Create(eip)
	ExpectTrue(c.WaitToBeReady(eip.Name, timeout))
	// Get the newest iptables eip after it becomes ready
	return c.Get(eip.Name).DeepCopy()
}

// Patch patches the iptables eip
func (c *IptablesEIPClient) Patch(original, modified *apiv1.IptablesEIP) *apiv1.IptablesEIP {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedIptablesEIP *apiv1.IptablesEIP
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		eip, err := c.IptablesEIPInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch iptables eip %q", original.Name)
		}
		patchedIptablesEIP = eip
		return true, nil
	})
	if err == nil {
		return patchedIptablesEIP.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch iptables EIP %s", original.Name)
	}
	Failf("error occurred while retrying to patch iptables EIP %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the iptables eip and waits for the iptables eip to be ready for `timeout`.
// If the iptables eip doesn't become ready before the timeout, it will fail the test.
func (c *IptablesEIPClient) PatchSync(original, modified *apiv1.IptablesEIP, _ []string, timeout time.Duration) *apiv1.IptablesEIP {
	eip := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(eip, timeout))
	ExpectTrue(c.WaitToBeReady(eip.Name, timeout))
	// Get the newest iptables eip after it becomes ready
	return c.Get(eip.Name).DeepCopy()
}

// PatchQoS patches the vpc nat gw and waits for the qos to be ready for `timeout`.
// If the qos doesn't become ready before the timeout, it will fail the test.
func (c *IptablesEIPClient) PatchQoSPolicySync(eipName, qosPolicyName string) *apiv1.IptablesEIP {
	eip := c.Get(eipName)
	modifiedEIP := eip.DeepCopy()
	modifiedEIP.Spec.QoSPolicy = qosPolicyName
	_ = c.Patch(eip, modifiedEIP)
	ExpectTrue(c.WaitToQoSReady(eipName))
	return c.Get(eipName).DeepCopy()
}

// Delete deletes a iptables eip if the iptables eip exists
func (c *IptablesEIPClient) Delete(name string) {
	err := c.IptablesEIPInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete iptables eip %q: %v", name, err)
	}
}

// DeleteSync deletes the iptables eip and waits for the iptables eip to disappear for `timeout`.
// If the iptables eip doesn't disappear before the timeout, it will fail the test.
func (c *IptablesEIPClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for iptables eip %q to disappear", name)
}

// WaitToBeReady returns whether the iptables eip is ready within timeout.
func (c *IptablesEIPClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		eip := c.Get(name)
		if eip.Status.Ready && eip.Status.IP != "" && eip.Spec.V4ip != "" {
			Logf("eip %s is ready", name)
			return true
		}
		Logf("eip %s is not ready", name)
	}
	return false
}

// WaitToQoSReady returns whether the qos is ready within timeout.
func (c *IptablesEIPClient) WaitToQoSReady(name string) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		eip := c.Get(name)
		if eip.Status.QoSPolicy == eip.Spec.QoSPolicy {
			Logf("qos %s is ready", name)
			return true
		}
		Logf("qos %s is not ready", name)
	}
	return false
}

// WaitToBeUpdated returns whether the iptables eip is updated within timeout.
func (c *IptablesEIPClient) WaitToBeUpdated(eip *apiv1.IptablesEIP, timeout time.Duration) bool {
	Logf("Waiting up to %v for iptables eip %s to be updated", timeout, eip.Name)
	rv, _ := big.NewInt(0).SetString(eip.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(eip.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("iptables eip %s was not updated within %v", eip.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified iptables eip to disappear.
func (c *IptablesEIPClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.IptablesEIP, error) {
		eip, err := c.IptablesEIPInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return eip, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected iptables EIP %s to not be found: %w", name, err)
	}
	return nil
}

func MakeIptablesEIP(name, v4ip, v6ip, mac, natGwDp, externalSubnet, qosPolicyName string) *apiv1.IptablesEIP {
	eip := &apiv1.IptablesEIP{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.IptablesEipSpec{
			V4ip:       v4ip,
			V6ip:       v6ip,
			MacAddress: mac,
			NatGwDp:    natGwDp,
		},
	}
	if externalSubnet != "" {
		eip.Spec.ExternalSubnet = externalSubnet
	}
	eip.Spec.QoSPolicy = qosPolicyName
	return eip
}
