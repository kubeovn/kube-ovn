package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// BgpEdgeRouterClient is a struct for bgp edge router client.
type BgpEdgeRouterClient struct {
	f         *Framework
	namespace string
	v1.BgpEdgeRouterInterface
}

func NewBgpEdgeRouterClient(cs clientset.Interface, namespapce string) *BgpEdgeRouterClient {
	return &BgpEdgeRouterClient{
		namespace:              namespapce,
		BgpEdgeRouterInterface: cs.KubeovnV1().BgpEdgeRouters(namespapce),
	}
}

func (f *Framework) BgpEdgeRouterClient() *BgpEdgeRouterClient {
	return &BgpEdgeRouterClient{
		f:                      f,
		namespace:              f.Namespace.Name,
		BgpEdgeRouterInterface: f.KubeOVNClientSet.KubeovnV1().BgpEdgeRouters(f.Namespace.Name),
	}
}

func (f *Framework) BgpEdgeRouterClientNS(namespapce string) *BgpEdgeRouterClient {
	return &BgpEdgeRouterClient{
		f:                      f,
		namespace:              namespapce,
		BgpEdgeRouterInterface: f.KubeOVNClientSet.KubeovnV1().BgpEdgeRouters(namespapce),
	}
}

func (c *BgpEdgeRouterClient) Get(name string) *apiv1.BgpEdgeRouter {
	ginkgo.GinkgoHelper()
	router, err := c.BgpEdgeRouterInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return router
}

// Create creates a new bgp-edge-router according to the framework specifications
func (c *BgpEdgeRouterClient) Create(router *apiv1.BgpEdgeRouter) *apiv1.BgpEdgeRouter {
	ginkgo.GinkgoHelper()
	g, err := c.BgpEdgeRouterInterface.Create(context.TODO(), router, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating bgp-edge-router")
	return g.DeepCopy()
}

// CreateSync creates a new bgp-edge-router according to the framework specifications, and waits for it to be ready.
func (c *BgpEdgeRouterClient) CreateSync(router *apiv1.BgpEdgeRouter) *apiv1.BgpEdgeRouter {
	ginkgo.GinkgoHelper()
	_ = c.Create(router)
	return c.WaitUntil(router.Name, func(g *apiv1.BgpEdgeRouter) (bool, error) {
		return g.Ready(), nil
	}, "Ready", 2*time.Second, timeout)
}

// Patch patches the router
func (c *BgpEdgeRouterClient) Patch(original, modified *apiv1.BgpEdgeRouter) *apiv1.BgpEdgeRouter {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedRouter *apiv1.BgpEdgeRouter
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		g, err := c.BgpEdgeRouterInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch bgp-edge-router %s/%s", original.Namespace, original.Name)
		}
		patchedRouter = g
		return true, nil
	})
	if err == nil {
		return patchedRouter.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch bgp-edge-router %s/%s", original.Namespace, original.Name)
	}
	Failf("error occurred while retrying to patch bgp-edge-router %s/%s: %v", original.Namespace, original.Name, err)

	return nil
}

// PatchSync patches the router and waits the router to meet the condition
func (c *BgpEdgeRouterClient) PatchSync(original, modified *apiv1.BgpEdgeRouter) *apiv1.BgpEdgeRouter {
	ginkgo.GinkgoHelper()
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, func(g *apiv1.BgpEdgeRouter) (bool, error) {
		return g.Ready(), nil
	}, "Ready", 2*time.Second, timeout)
}

// Delete deletes a bgp-edge-router if the bgp-edge-router exists
func (c *BgpEdgeRouterClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.BgpEdgeRouterInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete bgp-edge-router %s/%s: %v", c.namespace, name, err)
	}
}

// DeleteSync deletes the bgp-edge-router and waits for the bgp-edge-router to disappear for `timeout`.
// If the bgp-edge-router doesn't disappear before the timeout, it will fail the test.
func (c *BgpEdgeRouterClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for bgp-edge-router %s/%s to disappear", c.namespace, name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *BgpEdgeRouterClient) WaitUntil(name string, cond func(g *apiv1.BgpEdgeRouter) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.BgpEdgeRouter {
	var router *apiv1.BgpEdgeRouter
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, false, func(_ context.Context) (bool, error) {
		Logf("Waiting for bgp-edge-router %s/%s to meet condition %q", c.namespace, name, condDesc)
		router = c.Get(name).DeepCopy()
		met, err := cond(router)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for bgp-edge-router %s/%s: %w", c.namespace, name, err)
		}
		if met {
			Logf("bgp-edge-router %s/%s met condition %q", c.namespace, name, condDesc)
		} else {
			Logf("bgp-edge-router %s/%s not met condition %q", c.namespace, name, condDesc)
		}
		return met, nil
	})
	if err == nil {
		return router
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while waiting for bgp-edge-router %s/%s to meet condition %q", c.namespace, name, condDesc)
	}
	Failf("error occurred while waiting for bgp-edge-router %s/%s to meet condition %q: %v", c.namespace, name, condDesc, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified bgp-edge-router to disappear.
func (c *BgpEdgeRouterClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.BgpEdgeRouter, error) {
		svc, err := c.BgpEdgeRouterInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return svc, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected vpc-egress-gateway %s/%s to not be found: %w", c.namespace, name, err)
	}
	return nil
}

func MakeBgpEdgeRouter(namespace, name, vpc string, replicas int32, internalSubnet, externalSubnet, forwardSubnet string) *apiv1.BgpEdgeRouter {
	return &apiv1.BgpEdgeRouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.BgpEdgeRouterSpec{
			Replicas:       replicas,
			VPC:            vpc,
			InternalSubnet: internalSubnet,
			ExternalSubnet: externalSubnet,
			BFD: apiv1.BgpEdgeRouterBFDConfig{
				Enabled:    true,
				MinRX:      300,
				MinTX:      300,
				Multiplier: 3,
			},
			Policies: []apiv1.BgpEdgeRouterPolicy{
				{
					SNAT: false,
					Subnets: []string{
						forwardSubnet,
					},
				},
			},
			BGP: apiv1.BgpEdgeRouterBGPConfig{
				Enabled:        true,
				ASN:            65000,
				EdgeRouterMode: true,
				RemoteASN:      65100,
				Neighbors: []string{
					"192.168.1.1",
				},
				EnableGracefulRestart: true,
			},
		},
	}
}
