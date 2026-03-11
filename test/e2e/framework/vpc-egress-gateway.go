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

// VpcEgressGatewayClient is a struct for vpc egress gateway client.
type VpcEgressGatewayClient struct {
	f         *Framework
	namespace string
	v1.VpcEgressGatewayInterface
}

func NewVpcEgressGatewayClient(cs clientset.Interface, namespace string) *VpcEgressGatewayClient {
	return &VpcEgressGatewayClient{
		namespace:                 namespace,
		VpcEgressGatewayInterface: cs.KubeovnV1().VpcEgressGateways(namespace),
	}
}

func (f *Framework) VpcEgressGatewayClient() *VpcEgressGatewayClient {
	return &VpcEgressGatewayClient{
		f:                         f,
		namespace:                 f.Namespace.Name,
		VpcEgressGatewayInterface: f.KubeOVNClientSet.KubeovnV1().VpcEgressGateways(f.Namespace.Name),
	}
}

func (f *Framework) VpcEgressGatewayClientNS(namespace string) *VpcEgressGatewayClient {
	return &VpcEgressGatewayClient{
		f:                         f,
		namespace:                 namespace,
		VpcEgressGatewayInterface: f.KubeOVNClientSet.KubeovnV1().VpcEgressGateways(namespace),
	}
}

func (c *VpcEgressGatewayClient) Get(name string) *apiv1.VpcEgressGateway {
	ginkgo.GinkgoHelper()
	gateway, err := c.VpcEgressGatewayInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return gateway
}

// Create creates a new vpc-egress-gateway according to the framework specifications
func (c *VpcEgressGatewayClient) Create(gateway *apiv1.VpcEgressGateway) *apiv1.VpcEgressGateway {
	ginkgo.GinkgoHelper()
	g, err := c.VpcEgressGatewayInterface.Create(context.TODO(), gateway, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vpc-egress-gateway")
	return g.DeepCopy()
}

// CreateSync creates a new vpc-egress-gateway according to the framework specifications, and waits for it to be ready.
func (c *VpcEgressGatewayClient) CreateSync(gateway *apiv1.VpcEgressGateway) *apiv1.VpcEgressGateway {
	ginkgo.GinkgoHelper()
	_ = c.Create(gateway)
	return c.WaitUntil(gateway.Name, func(g *apiv1.VpcEgressGateway) (bool, error) {
		return g.Ready(), nil
	}, "Ready", poll, timeout)
}

// Patch patches the gateway
func (c *VpcEgressGatewayClient) Patch(original, modified *apiv1.VpcEgressGateway) *apiv1.VpcEgressGateway {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedGateway *apiv1.VpcEgressGateway
	err = wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
		g, err := c.VpcEgressGatewayInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vpc-egress-gateway %s/%s", original.Namespace, original.Name)
		}
		patchedGateway = g
		return true, nil
	})
	if err == nil {
		return patchedGateway.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch vpc-egress-gateway %s/%s", original.Namespace, original.Name)
	}
	Failf("error occurred while retrying to patch vpc-egress-gateway %s/%s: %v", original.Namespace, original.Name, err)

	return nil
}

// PatchSync patches the gateway and waits the gateway to meet the condition
func (c *VpcEgressGatewayClient) PatchSync(original, modified *apiv1.VpcEgressGateway) *apiv1.VpcEgressGateway {
	ginkgo.GinkgoHelper()
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, func(g *apiv1.VpcEgressGateway) (bool, error) {
		return g.Ready(), nil
	}, "Ready", poll, timeout)
}

// Delete deletes a vpc-egress-gateway if the vpc-egress-gateway exists
func (c *VpcEgressGatewayClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VpcEgressGatewayInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vpc-egress-gateway %s/%s: %v", c.namespace, name, err)
	}
}

// DeleteSync deletes the vpc-egress-gateway and waits for the vpc-egress-gateway to disappear for `timeout`.
// If the vpc-egress-gateway doesn't disappear before the timeout, it will fail the test.
func (c *VpcEgressGatewayClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for vpc-egress-gateway %s/%s to disappear", c.namespace, name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *VpcEgressGatewayClient) WaitUntil(name string, cond func(g *apiv1.VpcEgressGateway) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.VpcEgressGateway {
	var gateway *apiv1.VpcEgressGateway
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, false, func(_ context.Context) (bool, error) {
		Logf("Waiting for vpc-egress-gateway %s/%s to meet condition %q", c.namespace, name, condDesc)
		gateway = c.Get(name).DeepCopy()
		met, err := cond(gateway)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for vpc-egress-gateway %s/%s: %w", c.namespace, name, err)
		}
		if met {
			Logf("vpc-egress-gateway %s/%s met condition %q", c.namespace, name, condDesc)
		} else {
			Logf("vpc-egress-gateway %s/%s not met condition %q", c.namespace, name, condDesc)
		}
		return met, nil
	})
	if err == nil {
		return gateway
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while waiting for vpc-egress-gateway %s/%s to meet condition %q", c.namespace, name, condDesc)
	}
	Failf("error occurred while waiting for vpc-egress-gateway %s/%s to meet condition %q: %v", c.namespace, name, condDesc, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified vpc-egress-gateway to disappear.
func (c *VpcEgressGatewayClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.VpcEgressGateway, error) {
		svc, err := c.VpcEgressGatewayInterface.Get(ctx, name, metav1.GetOptions{})
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

func MakeVpcEgressGateway(namespace, name, vpc string, replicas int32, internalSubnet, externalSubnet string) *apiv1.VpcEgressGateway {
	return &apiv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.VpcEgressGatewaySpec{
			Replicas:       replicas,
			VPC:            vpc,
			InternalSubnet: internalSubnet,
			ExternalSubnet: externalSubnet,
		},
	}
}
