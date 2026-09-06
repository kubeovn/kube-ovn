package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
)

// VpcEndpointServiceClient wraps the cluster-scoped VpcEndpointService API.
type VpcEndpointServiceClient struct {
	f *Framework
	v1.VpcEndpointServiceInterface
}

func (f *Framework) VpcEndpointServiceClient() *VpcEndpointServiceClient {
	return &VpcEndpointServiceClient{
		f:                           f,
		VpcEndpointServiceInterface: f.KubeOVNClientSet.KubeovnV1().VpcEndpointServices(),
	}
}

func (c *VpcEndpointServiceClient) Get(name string) *apiv1.VpcEndpointService {
	ginkgo.GinkgoHelper()
	eps, err := c.VpcEndpointServiceInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return eps.DeepCopy()
}

func (c *VpcEndpointServiceClient) Create(eps *apiv1.VpcEndpointService) *apiv1.VpcEndpointService {
	ginkgo.GinkgoHelper()
	eps, err := c.VpcEndpointServiceInterface.Create(context.TODO(), eps, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating VpcEndpointService")
	return eps.DeepCopy()
}

func (c *VpcEndpointServiceClient) CreateSync(eps *apiv1.VpcEndpointService) *apiv1.VpcEndpointService {
	ginkgo.GinkgoHelper()
	eps = c.Create(eps)
	ExpectTrue(c.WaitToBeReady(eps.Name, 5*time.Minute))
	return c.Get(eps.Name)
}

func (c *VpcEndpointServiceClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for VpcEndpointService %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		eps := c.Get(name)
		if eps.Status.Ready && eps.Status.TransitVIP != "" {
			Logf("VpcEndpointService %s is ready transitVIP=%s", name, eps.Status.TransitVIP)
			return true
		}
		Logf("VpcEndpointService %s is not ready", name)
	}
	Logf("VpcEndpointService %s was not ready within %v", name, timeout)
	return false
}

func (c *VpcEndpointServiceClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VpcEndpointServiceInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("failed to delete VpcEndpointService %s: %v", name, err)
	}
}

func (c *VpcEndpointServiceClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	err := wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := c.VpcEndpointServiceInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
	ExpectNoError(err, fmt.Sprintf("timed out waiting for VpcEndpointService %s to be deleted", name))
}

// VpcEndpointClient wraps the cluster-scoped VpcEndpoint API.
type VpcEndpointClient struct {
	f *Framework
	v1.VpcEndpointInterface
}

func (f *Framework) VpcEndpointClient() *VpcEndpointClient {
	return &VpcEndpointClient{
		f:                    f,
		VpcEndpointInterface: f.KubeOVNClientSet.KubeovnV1().VpcEndpoints(),
	}
}

func (c *VpcEndpointClient) Get(name string) *apiv1.VpcEndpoint {
	ginkgo.GinkgoHelper()
	ep, err := c.VpcEndpointInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return ep.DeepCopy()
}

func (c *VpcEndpointClient) Create(ep *apiv1.VpcEndpoint) *apiv1.VpcEndpoint {
	ginkgo.GinkgoHelper()
	ep, err := c.VpcEndpointInterface.Create(context.TODO(), ep, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating VpcEndpoint")
	return ep.DeepCopy()
}

func (c *VpcEndpointClient) CreateSync(ep *apiv1.VpcEndpoint) *apiv1.VpcEndpoint {
	ginkgo.GinkgoHelper()
	ep = c.Create(ep)
	ExpectTrue(c.WaitToBeReady(ep.Name, 5*time.Minute))
	return c.Get(ep.Name)
}

func (c *VpcEndpointClient) WaitToBeReady(name string, timeout time.Duration) bool {
	Logf("Waiting up to %v for VpcEndpoint %s to be ready", timeout, name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		ep := c.Get(name)
		if ep.Status.Ready && ep.Status.LocalVIP != "" {
			Logf("VpcEndpoint %s is ready localVIP=%s", name, ep.Status.LocalVIP)
			return true
		}
		Logf("VpcEndpoint %s is not ready", name)
	}
	Logf("VpcEndpoint %s was not ready within %v", name, timeout)
	return false
}

func (c *VpcEndpointClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VpcEndpointInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("failed to delete VpcEndpoint %s: %v", name, err)
	}
}

func (c *VpcEndpointClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	err := wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := c.VpcEndpointInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out waiting for VpcEndpoint %s to be deleted", name)
	}
	ExpectNoError(err)
}

func MakeVpcEndpointService(name, vpc, namespace, service string, allowedVpcs []string) *apiv1.VpcEndpointService {
	return &apiv1.VpcEndpointService{
		Name: name,
		Spec: apiv1.VpcEndpointServiceSpec{
			Vpc:         vpc,
			Namespace:   namespace,
			Service:     service,
			AllowedVpcs: allowedVpcs,
		},
	}
}

func MakeVpcEndpoint(name, vpc, subnet, endpointService string) *apiv1.VpcEndpoint {
	return &apiv1.VpcEndpoint{
		Name: name,
		Spec: apiv1.VpcEndpointSpec{
			Vpc:             vpc,
			Subnet:          subnet,
			EndpointService: endpointService,
		},
	}
}
