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

// SecurityGroupClient is a struct for security-group client.
type SecurityGroupClient struct {
	f *Framework
	v1.SecurityGroupInterface
}

func (f *Framework) SecurityGroupClient() *SecurityGroupClient {
	return &SecurityGroupClient{
		f:                      f,
		SecurityGroupInterface: f.KubeOVNClientSet.KubeovnV1().SecurityGroups(),
	}
}

func (c *SecurityGroupClient) Get(name string) *apiv1.SecurityGroup {
	sg, err := c.SecurityGroupInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return sg.DeepCopy()
}

// Create creates a new security group according to the framework specifications
func (c *SecurityGroupClient) Create(sg *apiv1.SecurityGroup) *apiv1.SecurityGroup {
	sg, err := c.SecurityGroupInterface.Create(context.TODO(), sg, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating security group")
	return sg.DeepCopy()
}

// CreateSync creates a new security group according to the framework specifications, and waits for it to be ready.
func (c *SecurityGroupClient) CreateSync(sg *apiv1.SecurityGroup) *apiv1.SecurityGroup {
	sg = c.Create(sg)
	ExpectTrue(c.WaitToBeReady(sg.Name, timeout))
	// Get the newest ovn security group after it becomes ready
	return c.Get(sg.Name).DeepCopy()
}

// WaitToBeReady returns whether the security group is ready within timeout.
func (c *SecurityGroupClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for security group %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.PortGroup != "" {
			Logf("security group %s is ready", name)
			return true
		}
		Logf("security group %s is not ready", name)
	}
	Logf("security group %s was not ready within %v", name, timeout)
	return false
}

// Patch patches the security group
func (c *SecurityGroupClient) Patch(original, modified *apiv1.SecurityGroup, timeout time.Duration) *apiv1.SecurityGroup {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedSg *apiv1.SecurityGroup
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		p, err := c.SecurityGroupInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch security group %q", original.Name)
		}
		patchedSg = p
		return true, nil
	})
	if err == nil {
		return patchedSg.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch security group %s", original.Name)
	}
	Failf("error occurred while retrying to patch security group %s: %v", original.Name, err)

	return nil
}

// Delete deletes a security group if the security group exists
func (c *SecurityGroupClient) Delete(name string) {
	err := c.SecurityGroupInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete security group %q: %v", name, err)
	}
}

// DeleteSync deletes the security group and waits for the security group to disappear for `timeout`.
// If the security group doesn't disappear before the timeout, it will fail the test.
func (c *SecurityGroupClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for security group %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified Security Group to disappear.
func (c *SecurityGroupClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.SecurityGroup, error) {
		sg, err := c.SecurityGroupInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return sg, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected security group %s to not be found: %w", name, err)
	}
	return nil
}

func MakeSecurityGroup(name string, allowSameGroupTraffic bool, ingressRules, egressRules []*apiv1.SgRule) *apiv1.SecurityGroup {
	sg := &apiv1.SecurityGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.SecurityGroupSpec{
			AllowSameGroupTraffic: allowSameGroupTraffic,
			IngressRules:          ingressRules,
			EgressRules:           egressRules,
		},
	}
	return sg
}
