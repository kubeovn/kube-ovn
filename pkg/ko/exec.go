package ko

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

// execPod resolves a kubectl-style resource reference to one running Pod.
// Resources with a selector use the same selector that kubectl derives from
// the object, while -l selects Pods directly.
func (c *Client) execPod(ctx context.Context, reference, selector string, timeout time.Duration) (*corev1.Pod, error) {
	if reference != "" && selector != "" {
		return nil, fmt.Errorf("resource %q and --selector cannot be used together", reference)
	}
	if reference == "" {
		if selector == "" {
			return nil, fmt.Errorf("pod, type/name, or --selector must be specified")
		}
		return c.firstRunningPod(ctx, selector, timeout)
	}

	resource, name, qualified := strings.Cut(reference, "/")
	if !qualified {
		return c.Kubernetes.CoreV1().Pods(c.WorkloadNamespace).Get(ctx, reference, metav1.GetOptions{})
	}
	if resource == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("invalid resource reference %q", reference)
	}
	resource = canonicalExecResource(resource)
	var object runtime.Object
	var err error
	switch resource {
	case "pod":
		return c.Kubernetes.CoreV1().Pods(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "service":
		object, err = c.Kubernetes.CoreV1().Services(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "deployment":
		object, err = c.Kubernetes.AppsV1().Deployments(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "daemonset":
		object, err = c.Kubernetes.AppsV1().DaemonSets(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "statefulset":
		object, err = c.Kubernetes.AppsV1().StatefulSets(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "replicaset":
		object, err = c.Kubernetes.AppsV1().ReplicaSets(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "replicationcontroller":
		object, err = c.Kubernetes.CoreV1().ReplicationControllers(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	case "job":
		object, err = c.Kubernetes.BatchV1().Jobs(c.WorkloadNamespace).Get(ctx, name, metav1.GetOptions{})
	default:
		return nil, fmt.Errorf("unsupported exec resource %q; use pod, deploy, svc, ds, sts, rs, rc, or job", resource)
	}
	if err != nil {
		return nil, err
	}
	_, objectSelector, err := polymorphichelpers.SelectorsForObject(object)
	if err != nil {
		return nil, err
	}
	return c.firstRunningPod(ctx, objectSelector.String(), timeout)
}

func canonicalExecResource(resource string) string {
	if group, _, ok := strings.Cut(resource, "."); ok {
		resource = group
	}
	switch resource {
	case "po", "pod", "pods":
		return "pod"
	case "svc", "service", "services":
		return "service"
	case "deploy", "deployment", "deployments":
		return "deployment"
	case "ds", "daemonset", "daemonsets":
		return "daemonset"
	case "sts", "statefulset", "statefulsets":
		return "statefulset"
	case "rs", "replicaset", "replicasets":
		return "replicaset"
	case "rc", "replicationcontroller", "replicationcontrollers":
		return "replicationcontroller"
	case "job", "jobs":
		return "job"
	default:
		return resource
	}
}

func (c *Client) firstRunningPod(ctx context.Context, selector string, timeout time.Duration) (*corev1.Pod, error) {
	var result *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, timeout, true,
		func(ctx context.Context) (bool, error) {
			pods, err := c.Kubernetes.CoreV1().Pods(c.WorkloadNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return false, err
			}
			candidates := make([]*corev1.Pod, 0, len(pods.Items))
			for i := range pods.Items {
				pod := &pods.Items[i]
				if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning && podHasRunningContainer(pod) {
					candidates = append(candidates, pod)
				}
			}
			if len(candidates) == 0 {
				return false, nil
			}
			slices.SortFunc(candidates, func(a, b *corev1.Pod) int { return strings.Compare(a.Name, b.Name) })
			result = candidates[0].DeepCopy()
			return true, nil
		})
	if err != nil {
		return nil, fmt.Errorf("wait for a running pod matching %q: %w", selector, err)
	}
	return result, nil
}

func podHasRunningContainer(pod *corev1.Pod) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Running != nil {
			return true
		}
	}
	return false
}

func (c *Client) exec(ctx context.Context, reference, selector, container string, quiet bool, timeout time.Duration, streams Streams, argv []string) error {
	pod, err := c.execPod(ctx, reference, selector, timeout)
	if err != nil {
		return err
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("cannot exec into a container in a completed pod; current phase is %s", pod.Status.Phase)
	}
	selected, err := podcmd.FindOrDefaultContainerByName(pod, container, quiet, streams.ErrOut)
	if err != nil {
		return err
	}
	container = selected.Name
	if streams.TTY {
		streams.ErrOut = nil
	}
	return c.Executor.Exec(ctx, Target{Namespace: pod.Namespace, Pod: pod.Name, Container: container, Node: pod.Spec.NodeName}, argv, streams)
}
