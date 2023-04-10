package framework

import (
	"context"
	"fmt"
	"math/big"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/onsi/gomega"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	k8scnicncfiov1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/typed/k8s.cni.cncf.io/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// NetworkAttachmentDefinitionClient is a struct for provider network client.
type NetworkAttachmentDefinitionClient struct {
	f *Framework
	k8scnicncfiov1.NetworkAttachmentDefinitionInterface
}

func (f *Framework) NetworkAttachmentDefinitionClient() *NetworkAttachmentDefinitionClient {
	return &NetworkAttachmentDefinitionClient{
		f:                                    f,
		NetworkAttachmentDefinitionInterface: f.nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(f.Namespace.Name),
	}
}

func (c *NetworkAttachmentDefinitionClient) Get(name string) *nadv1.NetworkAttachmentDefinition {
	nad, err := c.NetworkAttachmentDefinitionInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return nad
}

// Create creates a new provider network according to the framework specifications
func (c *NetworkAttachmentDefinitionClient) Create(nad *nadv1.NetworkAttachmentDefinition) *nadv1.NetworkAttachmentDefinition {
	nad, err := c.NetworkAttachmentDefinitionInterface.Create(context.TODO(), nad, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating provider network")
	return nad.DeepCopy()
}

// CreateSync creates a new provider network according to the framework specifications, and waits for it to be ready.
func (c *NetworkAttachmentDefinitionClient) CreateSync(nad *nadv1.NetworkAttachmentDefinition) *nadv1.NetworkAttachmentDefinition {
	nad = c.Create(nad)
	ExpectTrue(c.WaitToBeReady(nad.Name, timeout))
	// Get the newest provider network after it becomes ready
	return c.Get(nad.Name).DeepCopy()
}

// Patch patches the provider network
func (c *NetworkAttachmentDefinitionClient) Patch(original, modified *nadv1.NetworkAttachmentDefinition) *nadv1.NetworkAttachmentDefinition {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedNetworkAttachmentDefinition *nadv1.NetworkAttachmentDefinition
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		nad, err := c.NetworkAttachmentDefinitionInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch provider network %q", original.Name)
		}
		patchedNetworkAttachmentDefinition = nad
		return true, nil
	})
	if err == nil {
		return patchedNetworkAttachmentDefinition.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch provider network %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching provider network %s", original.Name))

	return nil
}

// PatchSync patches the provider network and waits for the provider network to be ready for `timeout`.
// If the provider network doesn't become ready before the timeout, it will fail the test.
func (c *NetworkAttachmentDefinitionClient) PatchSync(original, modified *nadv1.NetworkAttachmentDefinition, requiredNodes []string, timeout time.Duration) *nadv1.NetworkAttachmentDefinition {
	nad := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(nad, timeout))
	ExpectTrue(c.WaitToBeReady(nad.Name, timeout))
	// Get the newest subnet after it becomes ready
	return c.Get(nad.Name).DeepCopy()
}

// Delete deletes a provider network if the provider network exists
func (c *NetworkAttachmentDefinitionClient) Delete(name string) {
	err := c.NetworkAttachmentDefinitionInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete provider network %q: %v", name, err)
	}
}

// DeleteSync deletes the provider network and waits for the provider network to disappear for `timeout`.
// If the provider network doesn't disappear before the timeout, it will fail the test.
func (c *NetworkAttachmentDefinitionClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for provider network %q to disappear", name)
}

// WaitToBeReady returns whether the provider network is ready within timeout.
func (c *NetworkAttachmentDefinitionClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		if c.Get(name).Spec.Config != "" {
			// exist means ready
			return true
		}
	}
	return false
}

// WaitToBeUpdated returns whether the provider network is updated within timeout.
func (c *NetworkAttachmentDefinitionClient) WaitToBeUpdated(nad *nadv1.NetworkAttachmentDefinition, timeout time.Duration) bool {
	Logf("Waiting up to %v for provider network %s to be updated", timeout, nad.Name)
	rv, _ := big.NewInt(0).SetString(nad.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(nad.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	Logf("NetworkAttachmentDefinition %s was not updated within %v", nad.Name, timeout)
	return false
}

// WaitToDisappear waits the given timeout duration for the specified provider network to disappear.
func (c *NetworkAttachmentDefinitionClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastNetworkAttachmentDefinition *nadv1.NetworkAttachmentDefinition
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		_, err := c.NetworkAttachmentDefinitionInterface.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for subnet %s to disappear", name),
			lastNetworkAttachmentDefinition,
		)
	}
	return maybeTimeoutError(err, "waiting for subnet %s to disappear", name)
}

func MakeNetworkAttachmentDefinition(name, config string) *nadv1.NetworkAttachmentDefinition {
	netAttachDef := nadv1.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: config},
	}
	return &netAttachDef
}
