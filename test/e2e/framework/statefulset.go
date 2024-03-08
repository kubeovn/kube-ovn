package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/statefulset"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type StatefulSetClient struct {
	f *Framework
	v1apps.StatefulSetInterface
	namespace string
}

func (f *Framework) StatefulSetClient() *StatefulSetClient {
	return f.StatefulSetClientNS(f.Namespace.Name)
}

func (f *Framework) StatefulSetClientNS(namespace string) *StatefulSetClient {
	return &StatefulSetClient{
		f:                    f,
		StatefulSetInterface: f.ClientSet.AppsV1().StatefulSets(namespace),
		namespace:            namespace,
	}
}

func (c *StatefulSetClient) Get(name string) *appsv1.StatefulSet {
	sts, err := c.StatefulSetInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return sts
}

func (c *StatefulSetClient) GetPods(sts *appsv1.StatefulSet) *corev1.PodList {
	pods := statefulset.GetPodList(context.Background(), c.f.ClientSet, sts)
	statefulset.SortStatefulPods(pods)
	return pods
}

// Create creates a new statefulset according to the framework specifications
func (c *StatefulSetClient) Create(sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	s, err := c.StatefulSetInterface.Create(context.TODO(), sts, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating statefulset")
	return s.DeepCopy()
}

// CreateSync creates a new statefulset according to the framework specifications, and waits for it to complete.
func (c *StatefulSetClient) CreateSync(sts *appsv1.StatefulSet) *appsv1.StatefulSet {
	s := c.Create(sts)
	c.WaitForRunningAndReady(s)
	// Get the newest statefulset
	return c.Get(s.Name).DeepCopy()
}

// Delete deletes a statefulset if the statefulset exists
func (c *StatefulSetClient) Delete(name string) {
	err := c.StatefulSetInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete statefulset %q: %v", name, err)
	}
}

// DeleteSync deletes the statefulset and waits for the statefulset to disappear for `timeout`.
// If the statefulset doesn't disappear before the timeout, it will fail the test.
func (c *StatefulSetClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for statefulset %q to disappear", name)
}

func (c *StatefulSetClient) WaitForRunningAndReady(sts *appsv1.StatefulSet) {
	Logf("Waiting up to %v for statefulset %s to be running and ready", timeout, sts.Name)
	statefulset.WaitForRunningAndReady(context.Background(), c.f.ClientSet, *sts.Spec.Replicas, sts)
}

// WaitToDisappear waits the given timeout duration for the specified statefulset to disappear.
func (c *StatefulSetClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*appsv1.StatefulSet, error) {
		sts, err := c.StatefulSetInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return sts, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected statefulset %s to not be found: %w", name, err)
	}
	return nil
}

func (c *StatefulSetClient) PatchSync(original, modified *appsv1.StatefulSet) *appsv1.StatefulSet {
	sts := c.Patch(original, modified)
	return c.RolloutStatus(sts.Name)
}

func (c *StatefulSetClient) Patch(original, modified *appsv1.StatefulSet) *appsv1.StatefulSet {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedSts *appsv1.StatefulSet
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		sts, err := c.StatefulSetInterface.Patch(ctx, original.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch StatefulSet %s/%s", original.Namespace, original.Name)
		}
		patchedSts = sts
		return true, nil
	})
	if err == nil {
		return patchedSts.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch StatefulSet %s/%s", original.Namespace, original.Name)
	}
	Failf("error occurred while retrying to patch StatefulSet %s/%s: %v", original.Namespace, original.Name, err)

	return nil
}

func (c *StatefulSetClient) RolloutStatus(name string) *appsv1.StatefulSet {
	var sts *appsv1.StatefulSet
	WaitUntil(2*time.Second, timeout, func(_ context.Context) (bool, error) {
		var err error
		sts = c.Get(name)
		unstructured := &unstructured.Unstructured{}
		if unstructured.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(sts); err != nil {
			return false, err
		}

		viewer := &polymorphichelpers.StatefulSetStatusViewer{}
		msg, done, err := viewer.Status(unstructured, 0)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}

		Logf(strings.TrimSpace(msg))
		return false, nil
	}, "")

	return sts
}

func MakeStatefulSet(name, svcName string, replicas int32, labels map[string]string, image string) *appsv1.StatefulSet {
	sts := statefulset.NewStatefulSet(name, "", svcName, replicas, nil, nil, labels)
	sts.Spec.Template.Spec.Containers[0].Image = image
	return sts
}
