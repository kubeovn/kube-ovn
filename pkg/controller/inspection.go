package controller

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
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

	for _, oriPod := range pods {
		if oriPod.Spec.HostNetwork || !isPodAlive(oriPod) {
			continue
		}

		pod := oriPod.DeepCopy()
		podName := c.getNameByPod(pod)
		key := cache.MetaObjectToName(pod).String()
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to list pod subnets, %v", err)
			return err
		}
		for _, podNet := range filterSubnets(pod, podNets) {
			if podNet.Type != providerTypeIPAM {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				exists, err := c.OVNNbClient.LogicalSwitchPortExists(portName)
				if err != nil {
					klog.Errorf("failed to check port %s exists, %v", portName, err)
					return err
				}

				if !exists { // pod exists but not lsp
					patch := util.KVPatch{
						fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName): nil,
						fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName):    nil,
					}
					if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
						klog.Errorf("patch pod %s/%s failed %v during inspection", pod.Name, pod.Namespace, err)
						return err
					}
					klog.V(5).Infof("finish remove annotation for %s", portName)
					klog.V(5).Infof("enqueue update pod %s", key)
					c.addOrUpdatePodQueue.Add(key)
					break
				} else if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" && pod.Spec.NodeName != "" &&
					pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] != "true" {
					klog.V(5).Infof("enqueue update pod %s", key)
					c.addOrUpdatePodQueue.Add(key)
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
