package framework

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// VpcNatGatewayClient is a struct for vpc nat gw client.
type VpcNatGatewayClient struct {
	f *Framework
	v1.VpcNatGatewayInterface
}

func (f *Framework) VpcNatGatewayClient() *VpcNatGatewayClient {
	return &VpcNatGatewayClient{
		f:                      f,
		VpcNatGatewayInterface: f.KubeOVNClientSet.KubeovnV1().VpcNatGateways(),
	}
}

func (c *VpcNatGatewayClient) Get(name string) *apiv1.VpcNatGateway {
	ginkgo.GinkgoHelper()
	vpcNatGw, err := c.VpcNatGatewayInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return vpcNatGw
}

// Create creates a new vpc nat gw according to the framework specifications
func (c *VpcNatGatewayClient) Create(vpcNatGw *apiv1.VpcNatGateway) *apiv1.VpcNatGateway {
	ginkgo.GinkgoHelper()
	vpcNatGw, err := c.VpcNatGatewayInterface.Create(context.TODO(), vpcNatGw, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vpc nat gw")
	return vpcNatGw.DeepCopy()
}

// CreateSync creates a new vpc nat gw according to the framework specifications, and waits for it to be ready.
func (c *VpcNatGatewayClient) CreateSync(vpcNatGw *apiv1.VpcNatGateway, clientSet clientset.Interface) *apiv1.VpcNatGateway {
	ginkgo.GinkgoHelper()

	vpcNatGw = c.Create(vpcNatGw)
	// When multiple VPC NAT gateways are being created, it may require more time to wait.
	timeout := 4 * time.Minute
	ExpectTrue(c.WaitGwPodReady(vpcNatGw.Name, timeout, clientSet))
	// Get the newest vpc nat gw after it becomes ready
	return c.Get(vpcNatGw.Name).DeepCopy()
}

// Patch patches the vpc nat gw
func (c *VpcNatGatewayClient) Patch(original, modified *apiv1.VpcNatGateway) *apiv1.VpcNatGateway {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedVpcNatGateway *apiv1.VpcNatGateway
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		vpcNatGw, err := c.VpcNatGatewayInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vpc nat gw %q", original.Name)
		}
		patchedVpcNatGateway = vpcNatGw
		return true, nil
	})
	if err == nil {
		return patchedVpcNatGateway.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch VPC NAT gateway %s", original.Name)
	}
	Failf("error occurred while retrying to patch VPC NAT gateway %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the vpc nat gw and waits for the vpc nat gw to be ready for `timeout`.
// If the vpc nat gw doesn't become ready before the timeout, it will fail the test.
func (c *VpcNatGatewayClient) PatchSync(original, modified *apiv1.VpcNatGateway, timeout time.Duration) *apiv1.VpcNatGateway {
	ginkgo.GinkgoHelper()

	vpcNatGw := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(vpcNatGw, timeout))
	ExpectTrue(c.WaitToBeReady(vpcNatGw.Name, timeout))
	// Get the newest vpc nat gw after it becomes ready
	return c.Get(vpcNatGw.Name).DeepCopy()
}

// PatchQoS patches the vpc nat gw and waits for the qos to be ready for `timeout`.
// If the qos doesn't become ready before the timeout, it will fail the test.
func (c *VpcNatGatewayClient) PatchQoSPolicySync(natgwName, qosPolicyName string) *apiv1.VpcNatGateway {
	ginkgo.GinkgoHelper()

	natgw := c.Get(natgwName)
	modifiedNATGW := natgw.DeepCopy()
	modifiedNATGW.Spec.QoSPolicy = qosPolicyName
	_ = c.Patch(natgw, modifiedNATGW)
	ExpectTrue(c.WaitToQoSReady(natgwName))
	return c.Get(natgwName).DeepCopy()
}

// Delete deletes a vpc nat gw if the vpc nat gw exists
func (c *VpcNatGatewayClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VpcNatGatewayInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vpc nat gw %q: %v", name, err)
	}
}

// DeleteSync deletes the vpc nat gw and waits for the vpc nat gw to disappear for `timeout`.
// If the vpc nat gw doesn't disappear before the timeout, it will fail the test.
func (c *VpcNatGatewayClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for vpc nat gw %q to disappear", name)
}

// WaitToBeReady returns whether the vpc nat gw is ready within timeout.
func (c *VpcNatGatewayClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Spec.LanIP != "" {
			return true
		}
	}
	return false
}

// WaitGwPodReady returns whether the vpc nat gw pod is ready within timeout.
func (c *VpcNatGatewayClient) WaitGwPodReady(name string, timeout time.Duration, clientSet clientset.Interface) bool {
	podName := util.GenNatGwPodName(name)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		pod, err := clientSet.CoreV1().Pods("kube-system").Get(context.Background(), podName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				Logf("natgw %s is not ready err: %s", name, err)
				continue
			}
			framework.ExpectNoError(err, "failed to get pod %v", podName)
		}
		if len(pod.Annotations) != 0 && pod.Annotations[util.VpcNatGatewayInitAnnotation] == "true" {
			Logf("natgw %s is ready", name)
			return true
		}
		Logf("natgw %s is not ready", name)
	}
	return false
}

// WaitToBeUpdated returns whether the vpc nat gw is updated within timeout.
func (c *VpcNatGatewayClient) WaitToBeUpdated(vpcNatGw *apiv1.VpcNatGateway, timeout time.Duration) bool {
	Logf("Waiting up to %v for vpc nat gw %s to be updated", timeout, vpcNatGw.Name)
	rv, _ := big.NewInt(0).SetString(vpcNatGw.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(vpcNatGw.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("vpc nat gw %s was not updated within %v", vpcNatGw.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified VPC NAT gateway to disappear.
func (c *VpcNatGatewayClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.VpcNatGateway, error) {
		gw, err := c.VpcNatGatewayInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return gw, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected VPC NAT gateway %s to not be found: %w", name, err)
	}
	return nil
}

// WaitToQoSReady returns whether the qos is ready within timeout.
func (c *VpcNatGatewayClient) WaitToQoSReady(name string) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		natgw := c.Get(name)
		if natgw.Status.QoSPolicy == natgw.Spec.QoSPolicy {
			Logf("qos %s of vpc nat gateway %s is ready", natgw.Spec.QoSPolicy, name)
			return true
		}
		Logf("qos %s of vpc nat gateway %s is not ready", natgw.Spec.QoSPolicy, name)
	}
	return false
}

func MakeVpcNatGateway(name, vpc, subnet, lanIP, externalSubnet, qosPolicyName string) *apiv1.VpcNatGateway {
	vpcNatGw := &apiv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.VpcNatGatewaySpec{
			Vpc:    vpc,
			Subnet: subnet,
			LanIP:  lanIP,
		},
	}
	if externalSubnet != "" {
		vpcNatGw.Spec.ExternalSubnets = []string{externalSubnet}
	}
	vpcNatGw.Spec.QoSPolicy = qosPolicyName
	return vpcNatGw
}

func MakeVpcNatGatewayWithNoDefaultEIP(name, vpc, subnet, lanIP, externalSubnet, qosPolicyName string, noDefaultEIP bool) *apiv1.VpcNatGateway {
	vpcNatGw := MakeVpcNatGateway(name, vpc, subnet, lanIP, externalSubnet, qosPolicyName)
	vpcNatGw.Spec.NoDefaultEIP = noDefaultEIP
	return vpcNatGw
}
