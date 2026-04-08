package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
)

// EvpnConfClient is a struct for EvpnConf client.
type EvpnConfClient struct {
	f *Framework
	v1.EvpnConfInterface
}

func NewEvpnConfClient(cs clientset.Interface) *EvpnConfClient {
	return &EvpnConfClient{
		EvpnConfInterface: cs.KubeovnV1().EvpnConves(),
	}
}

func (f *Framework) EvpnConfClient() *EvpnConfClient {
	return &EvpnConfClient{
		f:                 f,
		EvpnConfInterface: f.KubeOVNClientSet.KubeovnV1().EvpnConves(),
	}
}

func (c *EvpnConfClient) Get(name string) *apiv1.EvpnConf {
	ginkgo.GinkgoHelper()
	evpnConf, err := c.EvpnConfInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return evpnConf
}

func (c *EvpnConfClient) Create(evpnConf *apiv1.EvpnConf) *apiv1.EvpnConf {
	ginkgo.GinkgoHelper()
	ec, err := c.EvpnConfInterface.Create(context.TODO(), evpnConf, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating EvpnConf")
	return ec.DeepCopy()
}

func (c *EvpnConfClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.EvpnConfInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete EvpnConf %q: %v", name, err)
	}
}

func (c *EvpnConfClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for EvpnConf %q to disappear", name)
}

func (c *EvpnConfClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.EvpnConf, error) {
		ec, err := c.EvpnConfInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return ec, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected EvpnConf %q to not be found: %w", name, err)
	}
	return nil
}

func MakeEvpnConf(name string, vni uint32, routeTargets []string) *apiv1.EvpnConf {
	return &apiv1.EvpnConf{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.EvpnConfSpec{
			VNI:          vni,
			RouteTargets: routeTargets,
		},
	}
}
