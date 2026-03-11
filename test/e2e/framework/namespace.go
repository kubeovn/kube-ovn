package framework

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// NamespaceClient is a struct for namespace client.
type NamespaceClient struct {
	v1core.NamespaceInterface
}

func NewNamespaceClient(cs clientset.Interface) *NamespaceClient {
	return &NamespaceClient{
		NamespaceInterface: cs.CoreV1().Namespaces(),
	}
}

func (f *Framework) NamespaceClient() *NamespaceClient {
	return &NamespaceClient{
		NamespaceInterface: f.ClientSet.CoreV1().Namespaces(),
	}
}

func (c *NamespaceClient) Get(name string) *corev1.Namespace {
	ginkgo.GinkgoHelper()
	np, err := c.NamespaceInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return np
}

// Create creates a new namespace according to the framework specifications
func (c *NamespaceClient) Create(ns *corev1.Namespace) *corev1.Namespace {
	ginkgo.GinkgoHelper()
	np, err := c.NamespaceInterface.Create(context.TODO(), ns, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating namespace")
	return np.DeepCopy()
}

func (c *NamespaceClient) Patch(original, modified *corev1.Namespace) *corev1.Namespace {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedNS *corev1.Namespace
	err = wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
		ns, err := c.NamespaceInterface.Patch(ctx, original.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch namespace %s", original.Name)
		}
		patchedNS = ns
		return true, nil
	})
	if err == nil {
		return patchedNS.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch namespace %s", original.Name)
	}
	Failf("error occurred while retrying to patch namespace %s: %v", original.Name, err)

	return nil
}

// Delete deletes a namespace if the namespace exists
func (c *NamespaceClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.NamespaceInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete namespace %q: %v", name, err)
	}
}

// DeleteSync deletes the namespace and waits for the namespace to disappear for `timeout`.
// If the namespace doesn't disappear before the timeout, it will fail the test.
func (c *NamespaceClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for namespace %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified namespace to disappear.
func (c *NamespaceClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*corev1.Namespace, error) {
		policy, err := c.NamespaceInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return policy, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected namespace %s to not be found: %w", name, err)
	}
	return nil
}

func MakeNamespace(name string, labels, annotations map[string]string) *corev1.Namespace {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	return namespace
}
