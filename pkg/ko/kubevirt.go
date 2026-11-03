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
	pod         *corev1.Pod
	vmi         *unstructured.Unstructured
	node        string
	iface       string
	netns       string
	observation string
}

func (c *Client) captureTarget(ctx context.Context, ref sourceRef) (captureTarget, error) {
	return c.captureTargetOnNode(ctx, ref, "", "")
}

func (c *Client) captureTargetOnNode(ctx context.Context, ref sourceRef, nodeHint, observeAt string) (captureTarget, error) {
	if ref.kind == sourceKindLRP {
		if observeAt == "" {
			return captureTarget{}, errors.New("LRP capture requires --observe-at lsp/NAME")
		}
		observed, err := parseSourceRef(observeAt)
		if err != nil || observed.kind != sourceKindLSP {
			return captureTarget{}, errors.New("--observe-at must be lsp/NAME")
		}
		if err := c.checkLRPObservation(ctx, ref.name, observed.name); err != nil {
			return captureTarget{}, err
		}
		target, err := c.captureTargetOnNode(ctx, observed, nodeHint, "")
		if err != nil {
			return captureTarget{}, err
		}
		target.observation = fmt.Sprintf("Capturing traffic adjacent to LRP %s at LSP %s on node %s interface %s", ref.name, observed.name, target.node, target.iface)
		return target, nil
	}
	if ref.kind == sourceKindLocalnet || ref.kind == sourceKindLSP {
		source, err := c.ovnSource(ctx, ref, nodeHint, true)
		if err != nil {
			return captureTarget{}, err
		}
		iface := source.physicalNIC
		netns := ""
		if iface == "" {
			ovs, err := c.nodeTarget(ctx, source.node, "ovs")
			if err != nil {
				return captureTarget{}, err
			}
			nic, err := c.podInterface(ctx, ovs, source.lsp)
			if err != nil {
				return captureTarget{}, err
			}
			iface = nic.name
			if nic.kind == "internal" {
				if nic.netns == "" {
					return captureTarget{}, fmt.Errorf("internal LSP interface %q has no network namespace", nic.name)
				}
				netns = nic.netns
			}
		}
		return captureTarget{node: source.node, iface: iface, netns: netns}, nil
	}
	if ref.kind == sourceKindPod {
		pod, err := c.pod(ctx, ref.name)
		return captureTarget{pod: pod}, err
	}
	if ref.kind == sourceKindNode {
		return captureTarget{node: ref.name}, nil
	}
	namespace, name := c.WorkloadNamespace, ref.name
	if ref.kind != sourceKindVM && ref.kind != sourceKindVMI {
		pod, err := c.execPod(ctx, string(ref.kind)+"/"+name, "", c.DiscoveryTimeout)
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
