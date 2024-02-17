package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// EndpointsClient is a struct for endpoint client.
type EndpointsClient struct {
	f *Framework
	v1core.EndpointsInterface
	namespace string
}

func (f *Framework) EndpointClient() *EndpointsClient {
	return f.EndpointsClientNS(f.Namespace.Name)
}

func (f *Framework) EndpointsClientNS(namespace string) *EndpointsClient {
	return &EndpointsClient{
		f:                  f,
		EndpointsInterface: f.ClientSet.CoreV1().Endpoints(namespace),
		namespace:          namespace,
	}
}

func (c *EndpointsClient) Get(name string) *corev1.Endpoints {
	endpoints, err := c.EndpointsInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return endpoints
}

// Create creates a new endpoints according to the framework specifications
func (c *EndpointsClient) Create(endpoints *corev1.Endpoints) *corev1.Endpoints {
	e, err := c.EndpointsInterface.Create(context.TODO(), endpoints, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating endpoints")
	return e.DeepCopy()
}

// CreateSync creates a new endpoints according to the framework specifications, and waits for it to be updated.
func (c *EndpointsClient) CreateSync(endpoints *corev1.Endpoints, cond func(s *corev1.Endpoints) (bool, error), condDesc string) *corev1.Endpoints {
	_ = c.Create(endpoints)
	return c.WaitUntil(endpoints.Name, cond, condDesc, 2*time.Second, timeout)
}

// Patch patches the endpoints
func (c *EndpointsClient) Patch(original, modified *corev1.Endpoints) *corev1.Endpoints {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedEndpoints *corev1.Endpoints
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(_ context.Context) (bool, error) {
		s, err := c.EndpointsInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch endpoints %q", original.Name)
		}
		patchedEndpoints = s
		return true, nil
	})
	if err == nil {
		return patchedEndpoints.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch endpoints %s", original.Name)
	}
	Failf("error occurred while retrying to patch endpoints %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the endpoints and waits the endpoints to meet the condition
func (c *EndpointsClient) PatchSync(original, modified *corev1.Endpoints, cond func(s *corev1.Endpoints) (bool, error), condDesc string) *corev1.Endpoints {
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, cond, condDesc, 2*time.Second, timeout)
}

// Delete deletes a endpoints if the endpoints exists
func (c *EndpointsClient) Delete(name string) {
	err := c.EndpointsInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete endpoints %q: %v", name, err)
	}
}

// DeleteSync deletes the endpoints and waits for the endpoints to disappear for `timeout`.
// If the endpoints doesn't disappear before the timeout, it will fail the test.
func (c *EndpointsClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for endpoints %q to disappear", name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *EndpointsClient) WaitUntil(name string, cond func(s *corev1.Endpoints) (bool, error), condDesc string, _, timeout time.Duration) *corev1.Endpoints {
	var endpoints *corev1.Endpoints
	err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(_ context.Context) (bool, error) {
		Logf("Waiting for endpoints %s to meet condition %q", name, condDesc)
		endpoints = c.Get(name).DeepCopy()
		met, err := cond(endpoints)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for endpoints %s: %v", name, err)
		}
		if met {
			Logf("endpoints %s met condition %q", name, condDesc)
		} else {
			Logf("endpoints %s not met condition %q", name, condDesc)
		}
		return met, nil
	})
	if err == nil {
		return endpoints
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch endpoints %s", name)
	}
	Failf("error occurred while retrying to patch endpoints %s: %v", name, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified endpoints to disappear.
func (c *EndpointsClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*corev1.Endpoints, error) {
		svc, err := c.EndpointsInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return svc, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected endpoints %s to not be found: %w", name, err)
	}
	return nil
}

func MakeEndpoints(name string, annotations map[string]string, subset []corev1.EndpointSubset) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Subsets: subset,
	}
}
