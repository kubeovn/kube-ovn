package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

type DaemonSetClient struct {
	f *Framework
	v1apps.DaemonSetInterface
	namespace string
}

func (f *Framework) DaemonSetClient() *DaemonSetClient {
	return f.DaemonSetClientNS(f.Namespace.Name)
}

func (f *Framework) DaemonSetClientNS(namespace string) *DaemonSetClient {
	return &DaemonSetClient{
		f:                  f,
		DaemonSetInterface: f.ClientSet.AppsV1().DaemonSets(namespace),
		namespace:          namespace,
	}
}

func (c *DaemonSetClient) Get(name string) *appsv1.DaemonSet {
	ds, err := c.DaemonSetInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return ds
}

func (c *DaemonSetClient) GetPods(ds *appsv1.DaemonSet) (*corev1.PodList, error) {
	podSelector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	if err != nil {
		return nil, err
	}
	podListOptions := metav1.ListOptions{LabelSelector: podSelector.String()}
	allPods, err := c.f.ClientSet.CoreV1().Pods(ds.Namespace).List(context.TODO(), podListOptions)
	if err != nil {
		return nil, err
	}

	ownedPods := &corev1.PodList{Items: make([]corev1.Pod, 0, len(allPods.Items))}
	for i, pod := range allPods.Items {
		controllerRef := metav1.GetControllerOf(&allPods.Items[i])
		if controllerRef != nil && controllerRef.UID == ds.UID {
			ownedPods.Items = append(ownedPods.Items, pod)
		}
	}

	return ownedPods, nil
}

func (c *DaemonSetClient) GetPodOnNode(ds *appsv1.DaemonSet, node string) (*corev1.Pod, error) {
	pods, err := c.GetPods(ds)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == node {
			return pod.DeepCopy(), nil
		}
	}

	return nil, fmt.Errorf("pod for daemonset %s/%s on node %s not found", ds.Namespace, ds.Name, node)
}

func (c *DaemonSetClient) Patch(daemonset *appsv1.DaemonSet) *appsv1.DaemonSet {
	modifiedBytes, err := json.Marshal(daemonset)
	if err != nil {
		Failf("failed to marshal modified DaemonSet: %v", err)
	}
	ExpectNoError(err)
	var patchedDaemonSet *appsv1.DaemonSet
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		daemonSet, err := c.DaemonSetInterface.Patch(ctx, daemonset.Name, types.MergePatchType, modifiedBytes, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch daemonset %s/%s", daemonset.Namespace, daemonset.Name)
		}
		patchedDaemonSet = daemonSet
		return true, nil
	})
	if err == nil {
		return patchedDaemonSet.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch daemonset %s/%s", daemonset.Namespace, daemonset.Name)
	}
	Failf("error occurred while retrying to patch daemonset %s/%s: %v", daemonset.Namespace, daemonset.Name, err)

	return nil
}

func (c *DaemonSetClient) PatchSync(modifiedDaemonset *appsv1.DaemonSet) *appsv1.DaemonSet {
	daemonSet := c.Patch(modifiedDaemonset)
	return c.RolloutStatus(daemonSet.Name)
}

func (c *DaemonSetClient) RolloutStatus(name string) *appsv1.DaemonSet {
	var daemonSet *appsv1.DaemonSet
	WaitUntil(2*time.Second, timeout, func(_ context.Context) (bool, error) {
		var err error
		daemonSet = c.Get(name)
		unstructured := &unstructured.Unstructured{}
		if unstructured.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(daemonSet); err != nil {
			return false, err
		}

		dsv := &polymorphichelpers.DaemonSetStatusViewer{}
		msg, done, err := dsv.Status(unstructured, 0)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}

		Logf(strings.TrimSpace(msg))
		return false, nil
	}, "")

	return daemonSet
}
