package ko

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var vmiResource = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}

type captureTarget struct {
	pod  *corev1.Pod
	vmi  *unstructured.Unstructured
	node string
}

func (c *Client) captureTarget(ctx context.Context, ref sourceRef) (captureTarget, error) {
	if ref.kind == sourceKindPod {
		pod, err := c.pod(ctx, ref.kubeReference())
		return captureTarget{pod: pod}, err
	}
	if ref.kind == sourceKindNode {
		return captureTarget{node: ref.name}, nil
	}
	namespace, name := c.WorkloadNamespace, ref.name
	if ref.namespace != "" {
		namespace = ref.namespace
	}
	if ref.kind != sourceKindVM && ref.kind != sourceKindVMI {
		pod, err := c.execPodInNamespace(ctx, namespace, string(ref.kind)+"/"+name, "", c.DiscoveryTimeout)
		return captureTarget{pod: pod}, err
	}
	vmi, err := c.Dynamic.Resource(vmiResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return captureTarget{}, fmt.Errorf("get VMI %s/%s: %w", namespace, name, err)
	}
	if ref.kind == sourceKindVM {
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
		if preferred == "" {
			return nil, fmt.Errorf("VMI %s/%s has incomplete migration launcher state; retry", vmi.GetNamespace(), vmi.GetName())
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
		if status.ActivePods != nil && status.ActivePods[string(pod.UID)] != status.NodeName {
			continue
		}
		if preferred != "" && pod.Name != preferred {
			continue
		}
		candidates = append(candidates, pod)
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("resolve VMI %s/%s on node %s: expected one running launcher Pod (migration Pod %q), found %d; retry or select the launcher with pod/NAME", vmi.GetNamespace(), vmi.GetName(), status.NodeName, preferred, len(candidates))
	}
	return candidates[0], nil
}

func (c *Client) checkCaptureTarget(ctx context.Context, target captureTarget) error {
	if target.pod == nil {
		return nil
	}
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
