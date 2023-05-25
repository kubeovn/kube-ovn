package framework

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type PodClient struct {
	*e2epod.PodClient
}

func (f *Framework) PodClient() *PodClient {
	return f.PodClientNS(f.Namespace.Name)
}

func (f *Framework) PodClientNS(namespace string) *PodClient {
	return &PodClient{e2epod.PodClientNS(f.Framework, namespace)}
}

func (c *PodClient) Create(pod *corev1.Pod) *corev1.Pod {
	return c.PodClient.Create(context.Background(), pod)
}

func (c *PodClient) CreateSync(pod *corev1.Pod) *corev1.Pod {
	return c.PodClient.CreateSync(context.Background(), pod)
}

func (c *PodClient) Delete(name string) error {
	return c.PodClient.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func (c *PodClient) DeleteSync(name string) {
	c.PodClient.DeleteSync(context.Background(), name, metav1.DeleteOptions{}, timeout)
}

func (c *PodClient) PatchPod(original, modified *corev1.Pod) *corev1.Pod {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedPod *corev1.Pod
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		p, err := c.PodInterface.Patch(ctx, original.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch pod %s/%s", original.Namespace, original.Name)
		}
		patchedPod = p
		return true, nil
	})
	if err == nil {
		return patchedPod.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch pod %s/%s", original.Namespace, original.Name)
	}
	Failf("error occurred while retrying to patch pod %s/%s: %v", original.Namespace, original.Name, err)

	return nil
}

func MakePod(ns, name string, labels, annotations map[string]string, image string, command, args []string) *corev1.Pod {
	if image == "" {
		image = PauseImage
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "container",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         command,
					Args:            args,
				},
			},
		},
	}
	pod.Spec.TerminationGracePeriodSeconds = new(int64)
	*pod.Spec.TerminationGracePeriodSeconds = 3

	return pod
}
