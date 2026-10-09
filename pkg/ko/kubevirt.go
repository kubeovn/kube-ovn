package ko

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var vmiResource = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}

type captureOptions struct {
	pod, vm, vmi string
}

func (o captureOptions) reference() (string, string, error) {
	kind, reference := "", ""
	for _, item := range []struct{ kind, value string }{{"pod", o.pod}, {"vm", o.vm}, {"vmi", o.vmi}} {
		if item.value == "" {
			continue
		}
		if reference != "" {
			return "", "", errors.New("specify exactly one of --pod, --vm or --vmi")
		}
		kind, reference = item.kind, item.value
	}
	if reference == "" {
		return "", "", errors.New("specify exactly one of --pod, --vm or --vmi")
	}
	return kind, reference, nil
}

type captureTarget struct {
	pod *corev1.Pod
	vmi *unstructured.Unstructured
}

func (c *Client) captureTarget(ctx context.Context, options captureOptions) (captureTarget, error) {
	kind, reference, err := options.reference()
	if err != nil {
		return captureTarget{}, err
	}
	if kind == "pod" {
		pod, err := c.pod(ctx, reference)
		return captureTarget{pod: pod}, err
	}
	namespace, name := c.WorkloadNamespace, reference
	if ns, n, ok := strings.Cut(reference, "/"); ok {
		namespace, name = ns, n
	}
	vmi, err := c.Dynamic.Resource(vmiResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return captureTarget{}, fmt.Errorf("get VMI %s/%s: %w", namespace, name, err)
	}
	if kind == "vm" {
		resource := schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}
		vm, err := c.Dynamic.Resource(resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return captureTarget{}, fmt.Errorf("get VM %s/%s: %w", namespace, name, err)
		}
		owner := metav1.GetControllerOf(vmi)
		if vm.GetUID() == "" || vm.GetDeletionTimestamp() != nil || owner == nil || owner.Kind != "VirtualMachine" || owner.Name != vm.GetName() || owner.UID != vm.GetUID() {
			return captureTarget{}, fmt.Errorf("VMI %s/%s is not controlled by the current VM", namespace, name)
		}
	}
	pod, err := c.vmiLauncher(ctx, vmi)
	return captureTarget{pod: pod, vmi: vmi}, err
}

type vmiCaptureStatus struct {
	Phase      string            `json:"phase"`
	NodeName   string            `json:"nodeName"`
	ActivePods map[string]string `json:"activePods"`
	Migration  *struct {
		Completed bool   `json:"completed"`
		Failed    bool   `json:"failed"`
		SourcePod string `json:"sourcePod"`
		TargetPod string `json:"targetPod"`
	} `json:"migrationState"`
}

func (c *Client) vmiLauncher(ctx context.Context, vmi *unstructured.Unstructured) (*corev1.Pod, error) {
	var status vmiCaptureStatus
	raw, _, err := unstructured.NestedMap(vmi.Object, "status")
	if err != nil {
		return nil, err
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &status); err != nil {
		return nil, fmt.Errorf("decode VMI capture status: %w", err)
	}
	if vmi.GetUID() == "" || vmi.GetDeletionTimestamp() != nil || status.Phase != "Running" || status.NodeName == "" {
		return nil, fmt.Errorf("VMI %s/%s is not running on a node", vmi.GetNamespace(), vmi.GetName())
	}
	preferred := ""
	if status.Migration != nil {
		// KubeVirt may omit completed=false. Only successful completion moves
		// capture to the target; failed or aborted migrations keep the source.
		preferred = status.Migration.SourcePod
		if status.Migration.Completed && !status.Migration.Failed {
			preferred = status.Migration.TargetPod
		}
	}
	pods, err := c.Kubernetes.CoreV1().Pods(vmi.GetNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: "kubevirt.io=virt-launcher,kubevirt.io/created-by=" + string(vmi.GetUID()),
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", status.NodeName).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list launcher Pods for VMI %s/%s: %w", vmi.GetNamespace(), vmi.GetName(), err)
	}
	var candidates []*corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		owner := metav1.GetControllerOf(pod)
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName != status.NodeName || pod.UID == "" || owner == nil || owner.Kind != "VirtualMachineInstance" || owner.Name != vmi.GetName() || owner.UID != vmi.GetUID() {
			continue
		}
		if len(status.ActivePods) != 0 && status.ActivePods[string(pod.UID)] != status.NodeName {
			continue
		}
		if preferred != "" && pod.Name != preferred {
			continue
		}
		candidates = append(candidates, pod)
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("resolve VMI %s/%s on node %s: expected one running launcher Pod (migration Pod %q), found %d; retry or specify --pod", vmi.GetNamespace(), vmi.GetName(), status.NodeName, preferred, len(candidates))
	}
	return candidates[0], nil
}

func (c *Client) checkCaptureTarget(ctx context.Context, target captureTarget) error {
	if err := c.checkSource(ctx, podNetworkSource(target.pod)); err != nil {
		return err
	}
	if target.vmi == nil {
		return nil
	}
	vmi, err := c.Dynamic.Resource(vmiResource).Namespace(target.vmi.GetNamespace()).Get(ctx, target.vmi.GetName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("recheck VMI before capture: %w", err)
	}
	if vmi.GetUID() != target.vmi.GetUID() {
		return errors.New("VMI changed while resolving its network; retry")
	}
	pod, err := c.vmiLauncher(ctx, vmi)
	if err != nil {
		return err
	}
	if pod.UID != target.pod.UID || pod.Spec.NodeName != target.pod.Spec.NodeName {
		return errors.New("VMI launcher changed while resolving its network; retry")
	}
	return nil
}
