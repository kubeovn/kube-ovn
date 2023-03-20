package framework

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1apps "k8s.io/client-go/kubernetes/typed/apps/v1"
)

type DaemonSetClient struct {
	f *Framework
	v1apps.DaemonSetInterface
}

func (f *Framework) DaemonSetClient() *DaemonSetClient {
	return f.DaemonSetClientNS(f.Namespace.Name)
}

func (f *Framework) DaemonSetClientNS(namespace string) *DaemonSetClient {
	return &DaemonSetClient{
		f:                  f,
		DaemonSetInterface: f.ClientSet.AppsV1().DaemonSets(namespace),
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
