package controller

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) inspectPod() error {
	klog.V(4).Infof("start inspection")
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ip, %v", err)
		return err
	}
	lsps, err := c.ovnClient.ListLogicalSwitchPort(c.config.EnableExternalVpc)
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}
	for _, oripod := range pods {
		pod := oripod.DeepCopy()
		if pod.Spec.HostNetwork {
			continue
		}
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to list pod subnets, %v", err)
			return err
		}
		for _, podNet := range filterSubnets(pod, podNets) {
			if podNet.Type != providerTypeIPAM {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				isLspExist := false
				for _, lsp := range lsps {
					if portName == lsp {
						isLspExist = true
					}
				}
				if !isLspExist {
					delete(pod.Annotations, fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName))
					delete(pod.Annotations, util.RoutedAnnotation)
					if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
						klog.Errorf("patch pod %s/%s failed %v during inspection", pod.Name, pod.Namespace, err)
						return err
					}
					klog.V(5).Infof("finish remove annotation for %s", portName)
					c.addPodQueue.Add(fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
					break
				}
			}
		}
	}
	return nil
}

func filterSubnets(pod *v1.Pod, nets []*kubeovnNet) []*kubeovnNet {

	if pod.Annotations == nil {
		return nets
	}
	result := make([]*kubeovnNet, 0, len(nets))
	for _, n := range nets {
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, n.ProviderName)] == "true" {
			result = append(result, n)
		}
	}
	return result
}
