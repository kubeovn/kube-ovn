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

// VpcNatGatewayClient is a struct for vpc nat gw client.
type VpcNatGatewayClient struct {
	f *Framework
	v1.VpcNatGatewayInterface
}

func (f *Framework) VpcNatGatewayClient() *VpcNatGatewayClient {
	return &VpcNatGatewayClient{
		f:                      f,
		VpcNatGatewayInterface: f.KubeOVNClientSet.KubeovnV1().VpcNatGateways(),
	}
}

func (s *VpcNatGatewayClient) Get(name string) *apiv1.VpcNatGateway {
	vpcNatGw, err := s.VpcNatGatewayInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return vpcNatGw
}

// Create creates a new vpc nat gw according to the framework specifications
func (c *VpcNatGatewayClient) Create(vpcNatGw *apiv1.VpcNatGateway) *apiv1.VpcNatGateway {
	vpcNatGw, err := c.VpcNatGatewayInterface.Create(context.TODO(), vpcNatGw, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vpc nat gw")
	return vpcNatGw.DeepCopy()
}

// CreateSync creates a new vpc nat gw according to the framework specifications, and waits for it to be ready.
func (c *VpcNatGatewayClient) CreateSync(vpcNatGw *apiv1.VpcNatGateway) *apiv1.VpcNatGateway {
	vpcNatGw = c.Create(vpcNatGw)
	ExpectTrue(c.WaitToBeReady(vpcNatGw.Name, timeout))
	// Get the newest vpc nat gw after it becomes ready
	return c.Get(vpcNatGw.Name).DeepCopy()
}

// Patch patches the vpc nat gw
func (c *VpcNatGatewayClient) Patch(original, modified *apiv1.VpcNatGateway) *apiv1.VpcNatGateway {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedVpcNatGateway *apiv1.VpcNatGateway
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		vpcNatGw, err := c.VpcNatGatewayInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vpc nat gw %q", original.Name)
		}
		patchedVpcNatGateway = vpcNatGw
		return true, nil
	})
	if err == nil {
		return patchedVpcNatGateway.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch VPC NAT gateway %s", original.Name)
	}
	Failf("error occurred while retrying to patch VPC NAT gateway %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the vpc nat gw and waits for the vpc nat gw to be ready for `timeout`.
// If the vpc nat gw doesn't become ready before the timeout, it will fail the test.
func (c *VpcNatGatewayClient) PatchSync(original, modified *apiv1.VpcNatGateway, requiredNodes []string, timeout time.Duration) *apiv1.VpcNatGateway {
	vpcNatGw := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(vpcNatGw, timeout))
	ExpectTrue(c.WaitToBeReady(vpcNatGw.Name, timeout))
	// Get the newest vpc nat gw after it becomes ready
	return c.Get(vpcNatGw.Name).DeepCopy()
}

// Delete deletes a vpc nat gw if the vpc nat gw exists
func (c *VpcNatGatewayClient) Delete(name string) {
	err := c.VpcNatGatewayInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vpc nat gw %q: %v", name, err)
	}
}

// DeleteSync deletes the vpc nat gw and waits for the vpc nat gw to disappear for `timeout`.
// If the vpc nat gw doesn't disappear before the timeout, it will fail the test.
func (c *VpcNatGatewayClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for vpc nat gw %q to disappear", name)
}

// WaitToBeReady returns whether the vpc nat gw is ready within timeout.
func (c *VpcNatGatewayClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Spec.LanIp != "" {
			return true
		}
	}
	return false
}

// WaitToBeUpdated returns whether the vpc nat gw is updated within timeout.
func (c *VpcNatGatewayClient) WaitToBeUpdated(vpcNatGw *apiv1.VpcNatGateway, timeout time.Duration) bool {
	Logf("Waiting up to %v for vpc nat gw %s to be updated", timeout, vpcNatGw.Name)
	rv, _ := big.NewInt(0).SetString(vpcNatGw.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(vpcNatGw.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("vpc nat gw %s was not updated within %v", vpcNatGw.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified VPC NAT gateway to disappear.
func (c *VpcNatGatewayClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.VpcNatGateway, error) {
		gw, err := c.VpcNatGatewayInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return gw, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected VPC NAT gateway %s to not be found: %w", name, err)
	}
	return nil
}

func MakeVpcNatGateway(name, vpc, subnet, lanIp string) *apiv1.VpcNatGateway {
	vpcNatGw := &apiv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.VpcNatSpec{
			Vpc:    vpc,
			Subnet: subnet,
			LanIp:  lanIp,
		},
	}
	return vpcNatGw
}
