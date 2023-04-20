package framework

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/onsi/gomega"
)

// ServiceClient is a struct for service client.
type ServiceClient struct {
	f *Framework
	v1core.ServiceInterface
}

func (f *Framework) ServiceClient() *ServiceClient {
	return f.ServiceClientNS(f.Namespace.Name)
}

func (f *Framework) ServiceClientNS(namespace string) *ServiceClient {
	return &ServiceClient{
		f:                f,
		ServiceInterface: f.ClientSet.CoreV1().Services(namespace),
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
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		s, err := c.ServiceInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch service %q", original.Name)
		}
		patchedService = s
		return true, nil
	})
	if err == nil {
		return patchedService.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch service %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching service %s", original.Name))

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
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
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
	if IsTimeout(err) {
		Failf("timed out while waiting for service %s to meet condition %q", name, condDesc)
	}
	Fail(maybeTimeoutError(err, "waiting for service %s to meet condition %q", name, condDesc).Error())
	return nil
}

// WaitToDisappear waits the given timeout duration for the specified service to disappear.
func (c *ServiceClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastService *corev1.Service
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for service %s to disappear", name)
		services, err := c.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing services")
		}
		found := false
		for i, service := range services.Items {
			if service.Name == name {
				Logf("Service %s still exists", name)
				found = true
				lastService = &(services.Items[i])
				break
			}
		}
		if !found {
			Logf("Service %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for service %s to disappear", name),
			lastService,
		)
	}
	return maybeTimeoutError(err, "waiting for service %s to disappear", name)
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

	return service
}
