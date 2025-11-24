package framework

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
)

// DNSNameResolverClient is a struct for DNSNameResolver client.
type DNSNameResolverClient struct {
	f *Framework
	v1.DNSNameResolverInterface
}

func (f *Framework) DNSNameResolverClient() *DNSNameResolverClient {
	return &DNSNameResolverClient{
		f:                        f,
		DNSNameResolverInterface: f.KubeOVNClientSet.KubeovnV1().DNSNameResolvers(),
	}
}

// Get gets the DNSNameResolver.
func (c *DNSNameResolverClient) Get(name string) *apiv1.DNSNameResolver {
	ginkgo.GinkgoHelper()
	dnsNameResolver, err := c.DNSNameResolverInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return dnsNameResolver
}

// List lists the DNSNameResolvers.
func (c *DNSNameResolverClient) List(opts metav1.ListOptions) *apiv1.DNSNameResolverList {
	ginkgo.GinkgoHelper()
	dnsNameResolverList, err := c.DNSNameResolverInterface.List(context.TODO(), opts)
	ExpectNoError(err)
	return dnsNameResolverList
}

// Create creates the DNSNameResolver.
func (c *DNSNameResolverClient) Create(dnsNameResolver *apiv1.DNSNameResolver) *apiv1.DNSNameResolver {
	ginkgo.GinkgoHelper()
	dnsNameResolver, err := c.DNSNameResolverInterface.Create(context.TODO(), dnsNameResolver, metav1.CreateOptions{})
	ExpectNoError(err)
	return dnsNameResolver
}

// Delete deletes the DNSNameResolver.
func (c *DNSNameResolverClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.DNSNameResolverInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	ExpectNoError(err)
}

// WaitToBeReady returns whether the DNSNameResolver is ready within timeout.
func (c *DNSNameResolverClient) WaitToBeReady(name string, timeout time.Duration) bool {
	ginkgo.GinkgoHelper()
	result := c.WaitUntil(name, func(dnsNameResolver *apiv1.DNSNameResolver) (bool, error) {
		// DNSNameResolver is considered ready if it has at least one resolved name
		return len(dnsNameResolver.Status.ResolvedNames) > 0, nil
	}, "Ready", 2*time.Second, timeout)
	return result != nil
}

// WaitUntil waits until the condition is met.
func (c *DNSNameResolverClient) WaitUntil(name string, cond func(*apiv1.DNSNameResolver) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.DNSNameResolver {
	ginkgo.GinkgoHelper()
	var dnsNameResolver *apiv1.DNSNameResolver
	err := wait.PollUntilContextTimeout(context.TODO(), interval, timeout, true, func(_ context.Context) (bool, error) {
		var err error
		dnsNameResolver, err = c.DNSNameResolverInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return cond(dnsNameResolver)
	})
	ExpectNoError(err, "DNSNameResolver %s failed to be %s within %v", name, condDesc, timeout)
	return dnsNameResolver
}

// ListByLabel lists DNSNameResolvers by label selector.
func (c *DNSNameResolverClient) ListByLabel(labelSelector string) *apiv1.DNSNameResolverList {
	ginkgo.GinkgoHelper()
	opts := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	return c.List(opts)
}
