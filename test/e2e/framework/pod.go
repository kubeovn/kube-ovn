package framework

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
	psaapi "k8s.io/pod-security-admission/api"

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

func (c *PodClient) GetPod(name string) *corev1.Pod {
	ginkgo.GinkgoHelper()
	pod, err := c.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return pod
}

func (c *PodClient) Create(pod *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()
	return c.PodClient.Create(context.Background(), pod)
}

func (c *PodClient) CreateSync(pod *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()
	return c.PodClient.CreateSync(context.Background(), pod)
}

func (c *PodClient) Delete(name string) error {
	ginkgo.GinkgoHelper()
	return c.PodClient.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func (c *PodClient) DeleteGracefully(name string) {
	ginkgo.GinkgoHelper()
	err := c.PodInterface.Delete(context.Background(), name, metav1.DeleteOptions{GracePeriodSeconds: new(int64(1))})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete pod %q: %v", name, err)
	}
}

func (c *PodClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.PodClient.DeleteSync(context.Background(), name, metav1.DeleteOptions{GracePeriodSeconds: new(int64(1))}, timeout)
}

func (c *PodClient) Patch(original, modified *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()

	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedPod *corev1.Pod
	err = wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
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

func (c *PodClient) WaitForRunning(name string) {
	ginkgo.GinkgoHelper()
	err := e2epod.WaitTimeoutForPodRunningInNamespace(context.TODO(), c.f.ClientSet, name, c.namespace, timeout)
	ExpectNoError(err)
}

func (c *PodClient) WaitForNotFound(name string) {
	ginkgo.GinkgoHelper()
	err := e2epod.WaitForPodNotFoundInNamespace(context.TODO(), c.f.ClientSet, name, c.namespace, timeout)
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
			TerminationGracePeriodSeconds: new(int64(1)),
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

func CheckPodEgressRoutes(ns, pod string, ipv4, ipv6 bool, ttl int, expectedHops []string) {
	ginkgo.GinkgoHelper()

	afs := make([]int, 0, 2)
	dst := make([]string, 0, 2)
	if ipv4 {
		afs = append(afs, 4)
		dst = append(dst, "1.1.1.1")
	}
	if ipv6 {
		afs = append(afs, 6)
		dst = append(dst, "2606:4700:4700::1111")
	}

	for i, af := range afs {
		ginkgo.By(fmt.Sprintf("Checking IPv%d egress routes for pod %s/%s", af, ns, pod))
		cmd := fmt.Sprintf("traceroute -%d -n -f%d -m%d %s", af, ttl, ttl, dst[i])
		WaitUntil(3*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			// traceroute to 1.1.1.1 (1.1.1.1), 2 hops max, 60 byte packets
			// 2  172.19.0.2  0.663 ms  0.613 ms  0.605 ms
			output, err := e2epodoutput.RunHostCmd(ns, pod, cmd)
			if err != nil {
				return false, nil
			}

			lines := strings.Split(strings.TrimSpace(output), "\n")
			fields := strings.Fields(lines[len(lines)-1])
			return len(fields) > 2 && slices.Contains(expectedHops, fields[1]), nil
		}, "expected hops: "+strings.Join(expectedHops, ", "))
	}
}
