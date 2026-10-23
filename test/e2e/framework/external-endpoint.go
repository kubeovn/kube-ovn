package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

// ExternalEndpointClient is a struct for external endpoint client.
type ExternalEndpointClient struct {
	f         *Framework
	namespace string
	v1.ExternalEndpointInterface
}

func (f *Framework) ExternalEndpointClient() *ExternalEndpointClient {
	return f.ExternalEndpointClientNS(f.Namespace.Name)
}

func (f *Framework) ExternalEndpointClientNS(namespace string) *ExternalEndpointClient {
	return &ExternalEndpointClient{
		f:                         f,
		namespace:                 namespace,
		ExternalEndpointInterface: f.KubeOVNClientSet.KubeovnV1().ExternalEndpoints(namespace),
	}
}

func (c *ExternalEndpointClient) Get(name string) *apiv1.ExternalEndpoint {
	ginkgo.GinkgoHelper()
	ep, err := c.ExternalEndpointInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return ep
}

// Create creates a new external endpoint according to the framework specifications
func (c *ExternalEndpointClient) Create(ep *apiv1.ExternalEndpoint) *apiv1.ExternalEndpoint {
	ginkgo.GinkgoHelper()
	e, err := c.ExternalEndpointInterface.Create(context.TODO(), ep, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating external endpoint")
	return e.DeepCopy()
}

// CreateSync creates a new external endpoint and waits for it to be ready.
func (c *ExternalEndpointClient) CreateSync(ep *apiv1.ExternalEndpoint) *apiv1.ExternalEndpoint {
	ginkgo.GinkgoHelper()
	_ = c.Create(ep)
	return c.WaitUntil(ep.Name, func(e *apiv1.ExternalEndpoint) (bool, error) {
		return meta.IsStatusConditionTrue(e.Status.Conditions, apiv1.ExternalEndpointConditionReady), nil
	}, "Ready", poll, timeout)
}

// Patch patches the external endpoint
func (c *ExternalEndpointClient) Patch(original, modified *apiv1.ExternalEndpoint) *apiv1.ExternalEndpoint {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patched *apiv1.ExternalEndpoint
	err = wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
		e, err := c.ExternalEndpointInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch external endpoint %s/%s", original.Namespace, original.Name)
		}
		patched = e
		return true, nil
	})
	if err == nil {
		return patched.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch external endpoint %s/%s", original.Namespace, original.Name)
	}
	Failf("error occurred while retrying to patch external endpoint %s/%s: %v", original.Namespace, original.Name, err)

	return nil
}

// Delete deletes an external endpoint if the external endpoint exists
func (c *ExternalEndpointClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.ExternalEndpointInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete external endpoint %s/%s: %v", c.namespace, name, err)
	}
}

// DeleteSync deletes the external endpoint and waits for it to disappear for `timeout`.
// If the external endpoint doesn't disappear before the timeout, it will fail the test.
func (c *ExternalEndpointClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for external endpoint %s/%s to disappear", c.namespace, name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *ExternalEndpointClient) WaitUntil(name string, cond func(e *apiv1.ExternalEndpoint) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.ExternalEndpoint {
	var ep *apiv1.ExternalEndpoint
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, false, func(_ context.Context) (bool, error) {
		Logf("Waiting for external endpoint %s/%s to meet condition %q", c.namespace, name, condDesc)
		ep = c.Get(name).DeepCopy()
		met, err := cond(ep)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for external endpoint %s/%s: %w", c.namespace, name, err)
		}
		return met, nil
	})
	if err == nil {
		return ep
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while waiting for external endpoint %s/%s to meet condition %q", c.namespace, name, condDesc)
	}
	Failf("error occurred while waiting for external endpoint %s/%s to meet condition %q: %v", c.namespace, name, condDesc, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified external endpoint to disappear.
func (c *ExternalEndpointClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.ExternalEndpoint, error) {
		ep, err := c.ExternalEndpointInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return ep, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected external endpoint %s/%s to not be found: %w", c.namespace, name, err)
	}
	return nil
}

func MakeExternalEndpoint(namespace, name, subnet, chassis, macAddress, ipAddress string) *apiv1.ExternalEndpoint {
	return &apiv1.ExternalEndpoint{
		Name:      name,
		Namespace: namespace,
		Spec: apiv1.ExternalEndpointSpec{
			Subnet:     subnet,
			Chassis:    chassis,
			MacAddress: macAddress,
			IPAddress:  ipAddress,
		},
	}
}
