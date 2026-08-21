package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
)

// VtepBindingClient is a helper for VtepBinding e2e tests.
type VtepBindingClient struct {
	f *Framework
	v1.VtepBindingInterface
}

func (f *Framework) VtepBindingClient() *VtepBindingClient {
	return &VtepBindingClient{
		f:                    f,
		VtepBindingInterface: f.KubeOVNClientSet.KubeovnV1().VtepBindings(),
	}
}

func (c *VtepBindingClient) Get(name string) *apiv1.VtepBinding {
	ginkgo.GinkgoHelper()
	binding, err := c.VtepBindingInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return binding.DeepCopy()
}

func (c *VtepBindingClient) Create(binding *apiv1.VtepBinding) *apiv1.VtepBinding {
	ginkgo.GinkgoHelper()
	created, err := c.VtepBindingInterface.Create(context.TODO(), binding, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vtep binding")
	return created.DeepCopy()
}

func (c *VtepBindingClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VtepBindingInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vtep binding %q: %v", name, err)
	}
}

func (c *VtepBindingClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	ExpectNoError(c.WaitToDisappear(name, 2*time.Second, timeout))
}

func (c *VtepBindingClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := c.VtepBindingInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if err == nil {
		Logf("vtep binding %s no longer exists", name)
	}
	return err
}

func (c *VtepBindingClient) WaitUntil(name string, cond func(*apiv1.VtepBinding) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.VtepBinding {
	ginkgo.GinkgoHelper()
	var latest *apiv1.VtepBinding
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, true, func(ctx context.Context) (bool, error) {
		var err error
		latest, err = c.VtepBindingInterface.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return cond(latest)
	})
	ExpectNoError(err, fmt.Sprintf("timed out waiting for vtep binding %s to %s", name, condDesc))
	return latest.DeepCopy()
}
