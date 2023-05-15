package framework

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	v1apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/kubernetes/test/e2e/framework/statefulset"

	"github.com/onsi/gomega"
)

type StatefulSetClient struct {
	f *Framework
	v1apps.StatefulSetInterface
}

func (f *Framework) StatefulSetClient() *StatefulSetClient {
	return f.StatefulSetClientNS(f.Namespace.Name)
}

func (f *Framework) StatefulSetClientNS(namespace string) *StatefulSetClient {
	return &StatefulSetClient{
		f:                    f,
		StatefulSetInterface: f.ClientSet.AppsV1().StatefulSets(namespace),
	}
}

func (c *StatefulSetClient) Get(name string) *appsv1.StatefulSet {
	sts, err := c.StatefulSetInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return sts
}

func (c *StatefulSetClient) GetPods(sts *appsv1.StatefulSet) *corev1.PodList {
	pods := statefulset.GetPodList(c.f.ClientSet, sts)
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
	statefulset.WaitForRunningAndReady(c.f.ClientSet, *sts.Spec.Replicas, sts)
}

// WaitToDisappear waits the given timeout duration for the specified statefulset to disappear.
func (c *StatefulSetClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastStatefulSet *appsv1.StatefulSet
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for statefulset %s to disappear", name)
		statefulsets, err := c.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing statefulsets")
		}
		found := false
		for i, sts := range statefulsets.Items {
			if sts.Name == name {
				Logf("StatefulSet %s still exists", name)
				found = true
				lastStatefulSet = &(statefulsets.Items[i])
				break
			}
		}
		if !found {
			Logf("StatefulSet %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for statefulset %s to disappear", name),
			lastStatefulSet,
		)
	}
	return maybeTimeoutError(err, "waiting for statefulset %s to disappear", name)
}

func MakeStatefulSet(name, svcName string, replicas int32, labels map[string]string, image string) *appsv1.StatefulSet {
	sts := statefulset.NewStatefulSet(name, "", svcName, replicas, nil, nil, labels)
	sts.Spec.Template.Spec.Containers[0].Image = image
	return sts
}
