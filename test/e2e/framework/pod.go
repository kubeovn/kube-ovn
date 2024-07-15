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
	psaapi "k8s.io/pod-security-admission/api"
	"k8s.io/utils/ptr"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type PodClient struct {
	f *Framework
	*e2epod.PodClient
	namespace string
}

func (f *Framework) PodClient() *PodClient {
	return f.PodClientNS(f.Namespace.Name)
}

func (f *Framework) PodClientNS(namespace string) *PodClient {
	return &PodClient{f, e2epod.PodClientNS(f.Framework, namespace), namespace}
}

func (c *PodClient) List(ctx context.Context, options metav1.ListOptions) *corev1.PodList {
	ginkgo.GinkgoHelper()
	pods, err := c.PodInterface.List(ctx, options)
	ExpectNoError(err)
	return pods
}

func (c *PodClient) Get(ctx context.Context, name string) *corev1.Pod {
	ginkgo.GinkgoHelper()
	pod, err := c.PodInterface.Get(ctx, name, metav1.GetOptions{})
	ExpectNoError(err)
	return pod
}

func (c *PodClient) Create(ctx context.Context, pod *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()
	return c.PodClient.Create(ctx, pod)
}

func (c *PodClient) CreateSync(ctx context.Context, pod *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()
	return c.PodClient.CreateSync(ctx, pod)
}

func (c *PodClient) Delete(ctx context.Context, name string) error {
	ginkgo.GinkgoHelper()
	return c.PodClient.Delete(ctx, name, metav1.DeleteOptions{})
}

func (c *PodClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.PodClient.DeleteSync(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: ptr.To(int64(1))}, timeout)
}

func (c *PodClient) Patch(ctx context.Context, original, modified *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedPod *corev1.Pod
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
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

func (c *PodClient) WaitForRunning(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := e2epod.WaitTimeoutForPodRunningInNamespace(ctx, c.f.ClientSet, name, c.namespace, timeout)
	ExpectNoError(err)
}

func (c *PodClient) WaitForNotFound(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := e2epod.WaitForPodNotFoundInNamespace(ctx, c.f.ClientSet, name, c.namespace, timeout)
	ExpectNoError(err)
}

func makePod(ns, name string, labels, annotations map[string]string, image string, command, args []string, securityLevel psaapi.Level) *corev1.Pod {
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
					SecurityContext: e2epod.GenerateContainerSecurityContext(securityLevel),
				},
			},
			SecurityContext:               e2epod.GeneratePodSecurityContext(nil, nil),
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
		},
	}
	if securityLevel == psaapi.LevelRestricted {
		pod = e2epod.MustMixinRestrictedPodSecurity(pod)
	}
	return pod
}

func MakePod(ns, name string, labels, annotations map[string]string, image string, command, args []string) *corev1.Pod {
	return makePod(ns, name, labels, annotations, image, command, args, psaapi.LevelBaseline)
}

func MakeRestrictedPod(ns, name string, labels, annotations map[string]string, image string, command, args []string) *corev1.Pod {
	return makePod(ns, name, labels, annotations, image, command, args, psaapi.LevelRestricted)
}

func MakePrivilegedPod(ns, name string, labels, annotations map[string]string, image string, command, args []string) *corev1.Pod {
	return makePod(ns, name, labels, annotations, image, command, args, psaapi.LevelPrivileged)
}
