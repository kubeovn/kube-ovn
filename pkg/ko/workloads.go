package ko

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (c *Client) waitDeployment(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		deployment, err := c.Kubernetes.AppsV1().Deployments(c.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		replicas := int32(1)
		if deployment.Spec.Replicas != nil {
			replicas = *deployment.Spec.Replicas
		}
		status := deployment.Status
		return status.ObservedGeneration >= deployment.Generation && status.UpdatedReplicas == replicas && status.ReadyReplicas == replicas && status.AvailableReplicas == replicas && status.Replicas == replicas, nil
	})
}

func (c *Client) waitDaemonSet(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		ds, err := c.Kubernetes.AppsV1().DaemonSets(c.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return daemonSetReady(ds), nil
	})
}

func daemonSetReady(ds *appsv1.DaemonSet) bool {
	s := ds.Status
	return s.ObservedGeneration >= ds.Generation && s.DesiredNumberScheduled > 0 && s.CurrentNumberScheduled == s.DesiredNumberScheduled && s.UpdatedNumberScheduled == s.DesiredNumberScheduled && s.NumberReady == s.DesiredNumberScheduled && s.NumberAvailable == s.DesiredNumberScheduled && s.NumberMisscheduled == 0
}

func (c *Client) restart(ctx context.Context, kind, name string) error {
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339Nano)}}}}})
	if err != nil {
		return err
	}
	if kind == "deployment" {
		if _, err := c.Kubernetes.AppsV1().Deployments(c.Namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return err
		}
		return c.waitDeployment(ctx, name, 5*time.Minute)
	}
	if _, err := c.Kubernetes.AppsV1().DaemonSets(c.Namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return err
	}
	return c.waitDaemonSet(ctx, name, 5*time.Minute)
}

func (a *Application) reload(ctx context.Context, client *Client, _ []string) error {
	components := [][2]string{{"deployment", "ovn-central"}, {"daemonset", "ovs-ovn"}, {"deployment", "kube-ovn-controller"}, {"daemonset", "kube-ovn-cni"}, {"daemonset", "kube-ovn-pinger"}, {"deployment", "kube-ovn-monitor"}}
	for _, component := range components {
		if _, err := fmt.Fprintf(a.streams.ErrOut, "Restarting %s/%s\n", component[0], component[1]); err != nil {
			return err
		}
		if err := client.restart(ctx, component[0], component[1]); err != nil {
			return fmt.Errorf("restart stopped at %s/%s: %w", component[0], component[1], err)
		}
	}
	return nil
}

type ownedResource struct {
	kind, name string
	uid        types.UID
}

type resourceRun struct {
	client    *Client
	id        string
	resources []ownedResource
}

func (r *resourceRun) labels() map[string]string {
	return map[string]string{"app": "kubectl-ko-probe", "kubeovn.io/ko-run": r.id}
}

func (r *resourceRun) cleanup(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var failures []error
	for _, item := range slices.Backward(r.resources) {
		options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: new(item.uid)}}
		var err error
		switch item.kind {
		case "pod":
			err = r.client.Kubernetes.CoreV1().Pods(r.client.Namespace).Delete(ctx, item.name, options)
		case "service":
			err = r.client.Kubernetes.CoreV1().Services(r.client.Namespace).Delete(ctx, item.name, options)
		case "daemonset":
			err = r.client.Kubernetes.AppsV1().DaemonSets(r.client.Namespace).Delete(ctx, item.name, options)
		}
		if err != nil && !apierrors.IsNotFound(err) {
			failures = append(failures, fmt.Errorf("cleanup %s/%s/%s (UID %s): %w", r.client.Namespace, item.kind, item.name, item.uid, err))
		}
	}
	return errors.Join(failures...)
}

func (r *resourceRun) createPod(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	result, err := r.client.Kubernetes.CoreV1().Pods(r.client.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, r.recordUncertainCreation(ctx, "pod", pod.Name, err)
	}
	r.resources = append(r.resources, ownedResource{kind: "pod", name: result.Name, uid: result.UID})
	return result, nil
}

func (r *resourceRun) createService(ctx context.Context, service *corev1.Service) (*corev1.Service, error) {
	result, err := r.client.Kubernetes.CoreV1().Services(r.client.Namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, r.recordUncertainCreation(ctx, "service", service.Name, err)
	}
	r.resources = append(r.resources, ownedResource{kind: "service", name: result.Name, uid: result.UID})
	return result, nil
}

func (r *resourceRun) createDaemonSet(ctx context.Context, ds *appsv1.DaemonSet) error {
	result, err := r.client.Kubernetes.AppsV1().DaemonSets(r.client.Namespace).Create(ctx, ds, metav1.CreateOptions{})
	if err != nil {
		return r.recordUncertainCreation(ctx, "daemonset", ds.Name, err)
	}
	r.resources = append(r.resources, ownedResource{kind: "daemonset", name: result.Name, uid: result.UID})
	if err := r.client.waitDaemonSet(ctx, result.Name, 2*time.Minute); err != nil {
		return r.daemonSetFailure(ctx, result, err)
	}
	return nil
}

func (r *resourceRun) daemonSetFailure(ctx context.Context, ds *appsv1.DaemonSet, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var report strings.Builder
	if ds.UID == "" {
		return fmt.Errorf("daemonset %s/%s did not become ready: %w", ds.Namespace, ds.Name, cause)
	}
	pods, err := r.client.Kubernetes.CoreV1().Pods(ds.Namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(ds.Spec.Selector)})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("inspect failed daemonset %s/%s: %w", ds.Namespace, ds.Name, err))
	}
	inspected := 0
	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, ds) {
			continue
		}
		if inspected == 8 {
			fmt.Fprintln(&report, "Further probe pods omitted (diagnostic limit: 8 pods)")
			break
		}
		inspected++
		fmt.Fprintf(&report, "%s phase=%s node=%s\n", pod.Name, pod.Status.Phase, pod.Spec.NodeName)
		for _, condition := range pod.Status.Conditions {
			if condition.Status == corev1.ConditionFalse {
				fmt.Fprintf(&report, "%s: %s %s\n", condition.Type, condition.Reason, condition.Message)
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			fmt.Fprintf(&report, "%s/%s restarts=%d", pod.Name, status.Name, status.RestartCount)
			if state := status.State.Waiting; state != nil {
				fmt.Fprintf(&report, " state=%s", state.Reason)
			}
			for _, state := range []*corev1.ContainerStateTerminated{status.State.Terminated, status.LastTerminationState.Terminated} {
				if state != nil {
					fmt.Fprintf(&report, " exit=%d reason=%s", state.ExitCode, state.Reason)
				}
			}
			fmt.Fprintln(&report)
			options := &corev1.PodLogOptions{Container: status.Name, Previous: status.RestartCount > 0, TailLines: new(int64(20)), LimitBytes: new(int64(4096))}
			stream, err := r.client.Kubernetes.CoreV1().Pods(ds.Namespace).GetLogs(pod.Name, options).Stream(ctx)
			if err != nil {
				fmt.Fprintf(&report, "Cannot read probe log: %v\n", err)
				continue
			}
			_, readErr := io.Copy(&report, io.LimitReader(stream, 4096))
			closeErr := stream.Close()
			fmt.Fprintln(&report)
			if err := errors.Join(readErr, closeErr); err != nil {
				fmt.Fprintf(&report, "Probe log read failed: %v\n", err)
			}
		}
	}
	return fmt.Errorf("daemonset %s/%s did not become ready: %w\n%s", ds.Namespace, ds.Name, cause, report.String())
}

// A lost create response can leave an object on the server. Only adopt it for
// cleanup when its unique run label matches, then retain the exact returned UID.
func (r *resourceRun) recordUncertainCreation(ctx context.Context, kind, name string, createErr error) error {
	if apierrors.IsAlreadyExists(createErr) {
		return createErr
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var object metav1.Object
	var err error
	switch kind {
	case "pod":
		object, err = r.client.Kubernetes.CoreV1().Pods(r.client.Namespace).Get(ctx, name, metav1.GetOptions{})
	case "service":
		object, err = r.client.Kubernetes.CoreV1().Services(r.client.Namespace).Get(ctx, name, metav1.GetOptions{})
	case "daemonset":
		object, err = r.client.Kubernetes.AppsV1().DaemonSets(r.client.Namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return errors.Join(createErr, fmt.Errorf("verify uncertain creation of %s/%s/%s: %w", r.client.Namespace, kind, name, err))
	}
	if r.id == "" || object.GetLabels()["kubeovn.io/ko-run"] != r.id || object.GetUID() == "" {
		return errors.Join(createErr, fmt.Errorf("cannot establish ownership of %s/%s/%s; leaving it unchanged", r.client.Namespace, kind, name))
	}
	r.resources = append(r.resources, ownedResource{kind: kind, name: name, uid: object.GetUID()})
	return createErr
}

func (c *Client) waitPod(ctx context.Context, name string) (*corev1.Pod, error) {
	var result *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		var err error
		result, err = c.Kubernetes.CoreV1().Pods(c.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range result.Status.Conditions {
			if condition.Type == corev1.PodReady {
				return condition.Status == corev1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	return result, err
}
