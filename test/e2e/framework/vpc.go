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

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// VpcClient is a struct for vpc client.
type VpcClient struct {
	f *Framework
	v1.VpcInterface
}

func (f *Framework) VpcClient() *VpcClient {
	return &VpcClient{
		f:            f,
		VpcInterface: f.KubeOVNClientSet.KubeovnV1().Vpcs(),
	}
}

func (c *VpcClient) Get(name string) *kubeovnv1.Vpc {
	vpc, err := c.VpcInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return vpc
}

// Create creates a new vpc according to the framework specifications
func (c *VpcClient) Create(vpc *kubeovnv1.Vpc) *kubeovnv1.Vpc {
	vpc, err := c.VpcInterface.Create(context.TODO(), vpc, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vpc")
	return vpc.DeepCopy()
}

// CreateSync creates a new vpc according to the framework specifications, and waits for it to be ready.
func (c *VpcClient) CreateSync(vpc *kubeovnv1.Vpc) *kubeovnv1.Vpc {
	vpc = c.Create(vpc)
	ExpectTrue(c.WaitToBeReady(vpc.Name, timeout))
	// Get the newest vpc after it becomes ready
	return c.Get(vpc.Name).DeepCopy()
}

// Patch patches the vpc
func (c *VpcClient) Patch(original, modified *kubeovnv1.Vpc) *kubeovnv1.Vpc {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedVpc *kubeovnv1.Vpc
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		vpc, err := c.VpcInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vpc %q", original.Name)
		}
		patchedVpc = vpc
		return true, nil
	})
	if err == nil {
		return patchedVpc.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch VPC %s", original.Name)
	}
	Failf("error occurred while retrying to patch VPC %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the vpc and waits for the vpc to be ready for `timeout`.
// If the vpc doesn't become ready before the timeout, it will fail the test.
func (c *VpcClient) PatchSync(original, modified *kubeovnv1.Vpc, _ []string, timeout time.Duration) *kubeovnv1.Vpc {
	vpc := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(vpc, timeout))
	ExpectTrue(c.WaitToBeReady(vpc.Name, timeout))
	// Get the newest subnet after it becomes ready
	return c.Get(vpc.Name).DeepCopy()
}

// Delete deletes a vpc if the vpc exists
func (c *VpcClient) Delete(name string) {
	err := c.VpcInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vpc %q: %v", name, err)
	}
}

// DeleteSync deletes the vpc and waits for the vpc to disappear for `timeout`.
// If the vpc doesn't disappear before the timeout, it will fail the test.
func (c *VpcClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for vpc %q to disappear", name)
}

// WaitToBeReady returns whether the vpc is ready within timeout.
func (c *VpcClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Status.Standby {
			// standby means the vpc is ready
			return true
		}
	}
	return false
}

// WaitToBeUpdated returns whether the vpc is updated within timeout.
func (c *VpcClient) WaitToBeUpdated(vpc *kubeovnv1.Vpc, timeout time.Duration) bool {
	Logf("Waiting up to %v for vpc %s to be updated", timeout, vpc.Name)
	rv, _ := big.NewInt(0).SetString(vpc.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(vpc.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("Vpc %s was not updated within %v", vpc.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified VPC to disappear.
func (c *VpcClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*kubeovnv1.Vpc, error) {
		vpc, err := c.VpcInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return vpc, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected VPC %s to not be found: %w", name, err)
	}
	return nil
}

func MakeVpc(name, gatewayV4 string, enableExternal, enableBfd bool, namespaces []string) *kubeovnv1.Vpc {
	routes := make([]*kubeovnv1.StaticRoute, 0, 1)
	if gatewayV4 != "" {
		routes = append(routes, &kubeovnv1.StaticRoute{
			Policy:    kubeovnv1.PolicyDst,
			CIDR:      "0.0.0.0/0",
			NextHopIP: gatewayV4,
		})
	}
	vpc := &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kubeovnv1.VpcSpec{
			StaticRoutes:   routes,
			EnableExternal: enableExternal,
			EnableBfd:      enableBfd,
			Namespaces:     namespaces,
		},
	}
	return vpc
}
