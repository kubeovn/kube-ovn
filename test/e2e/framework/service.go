package framework

import (
	"context"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/onsi/gomega"
)

// ServiceClient is a struct for service client.
type ServiceClient struct {
	f *Framework
	v1core.ServiceInterface
}

func (f *Framework) ServiceClient() *ServiceClient {
	return &ServiceClient{
		f:                f,
		ServiceInterface: f.ClientSet.CoreV1().Services(f.Namespace.Name),
	}
}

func (s *ServiceClient) Get(name string) *corev1.Service {
	service, err := s.ServiceInterface.Get(context.TODO(), name, metav1.GetOptions{})
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
func (c *ServiceClient) CreateSync(service *corev1.Service) *corev1.Service {
	s := c.Create(service)
	ExpectTrue(c.WaitToBeUpdated(s))
	// Get the newest service
	return c.Get(s.Name).DeepCopy()
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

// WaitToBeUpdated returns whether the service is updated within timeout.
func (c *ServiceClient) WaitToBeUpdated(service *corev1.Service) bool {
	Logf("Waiting up to %v for service %s to be updated", timeout, service.Name)
	rv, _ := big.NewInt(0).SetString(service.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(service.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("Service %s was not updated within %v", service.Name, timeout)
	return false
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
