package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ServiceClient is a struct for service client.
type ServiceClient struct {
	f *Framework
	v1core.ServiceInterface
	namespace string
}

func (f *Framework) ServiceClient() *ServiceClient {
	return f.ServiceClientNS(f.Namespace.Name)
}

func (f *Framework) ServiceClientNS(namespace string) *ServiceClient {
	return &ServiceClient{
		f:                f,
		ServiceInterface: f.ClientSet.CoreV1().Services(namespace),
		namespace:        namespace,
	}
}

func (c *ServiceClient) Get(name string) *corev1.Service {
	service, err := c.ServiceInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return service
}

// Create creates a new service according to the framework specifications
func (c *ServiceClient) Create(service *corev1.Service) *corev1.Service {
	s, err := c.ServiceInterface.Create(context.TODO(), service, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating service")
	return s.DeepCopy()
}

// CreateSync creates a new service according to the framework specifications, and waits for it to be updated.
func (c *ServiceClient) CreateSync(service *corev1.Service, cond func(s *corev1.Service) (bool, error), condDesc string) *corev1.Service {
	_ = c.Create(service)
	return c.WaitUntil(service.Name, cond, condDesc, 2*time.Second, timeout)
}

// Patch patches the service
func (c *ServiceClient) Patch(original, modified *corev1.Service) *corev1.Service {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedService *corev1.Service
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		s, err := c.ServiceInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch service %q", original.Name)
		}
		patchedService = s
		return true, nil
	})
	if err == nil {
		return patchedService.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch service %s", original.Name)
	}
	Failf("error occurred while retrying to patch service %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the service and waits the service to meet the condition
func (c *ServiceClient) PatchSync(original, modified *corev1.Service, cond func(s *corev1.Service) (bool, error), condDesc string) *corev1.Service {
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, cond, condDesc, 2*time.Second, timeout)
}

// Delete deletes a service if the service exists
func (c *ServiceClient) Delete(name string) {
	err := c.ServiceInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete service %q: %v", name, err)
	}
}

// DeleteSync deletes the service and waits for the service to disappear for `timeout`.
// If the service doesn't disappear before the timeout, it will fail the test.
func (c *ServiceClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for service %q to disappear", name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *ServiceClient) WaitUntil(name string, cond func(s *corev1.Service) (bool, error), condDesc string, interval, timeout time.Duration) *corev1.Service {
	var service *corev1.Service
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, false, func(_ context.Context) (bool, error) {
		Logf("Waiting for service %s to meet condition %q", name, condDesc)
		service = c.Get(name).DeepCopy()
		met, err := cond(service)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for service %s: %v", name, err)
		}
		if met {
			Logf("service %s met condition %q", name, condDesc)
		} else {
			Logf("service %s not met condition %q", name, condDesc)
		}
		return met, nil
	})
	if err == nil {
		return service
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while waiting for service %s to meet condition %q", name, condDesc)
	}
	Failf("error occurred while waiting for service %s to meet condition %q: %v", name, condDesc, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified service to disappear.
func (c *ServiceClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*corev1.Service, error) {
		svc, err := c.ServiceInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return svc, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected service %s to not be found: %w", name, err)
	}
	return nil
}

func MakeService(name string, svcType corev1.ServiceType, annotations, selector map[string]string, ports []corev1.ServicePort, affinity corev1.ServiceAffinity) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports:           ports,
			Selector:        selector,
			SessionAffinity: affinity,
			Type:            svcType,
		},
	}
	service.Spec.IPFamilyPolicy = new(corev1.IPFamilyPolicy)
	*service.Spec.IPFamilyPolicy = corev1.IPFamilyPolicyPreferDualStack

	return service
}
