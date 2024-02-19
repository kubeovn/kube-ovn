package controller

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) resyncProviderNetworkStatus() {
	klog.V(3).Infof("start to sync ProviderNetwork status")
	pns, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list provider network: %v", err)
		return
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return
	}

	pods, err := c.podsLister.Pods("").List(labels.Set{"app": "kube-ovn-cni"}.AsSelector())
	if err != nil {
		klog.Errorf("failed to list kube-ovn-cni pods: %v", err)
		return
	}

	podMap := make(map[string]*corev1.Pod, len(pods))
	for _, pod := range pods {
		podMap[pod.Spec.NodeName] = pod
	}

	for _, cachedPn := range pns {
		pn := cachedPn.DeepCopy()
		var readyNodes, notReadyNodes, expectNodes []string
		pnReadyAnnotation := fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)
		pnErrMsgAnnotation := fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, pn.Name)

		var conditionsUpdated bool
		for _, node := range nodes {
			if slices.Contains(pn.Spec.ExcludeNodes, node.Name) {
				if pn.Status.RemoveNodeConditions(node.Name) {
					conditionsUpdated = true
				}
				continue
			}
			if node.Labels[pnReadyAnnotation] == "true" {
				if pn.Status.SetNodeReady(node.Name, "InitOVSBridgeSucceeded", "") {
					conditionsUpdated = true
				}
				readyNodes = append(readyNodes, node.Name)
			} else {
				var errMsg string
				if pod := podMap[node.Name]; pod == nil {
					errMsg = fmt.Sprintf("kube-ovn-cni pod on node %s not found", node.Name)
					klog.Error(errMsg)
				} else {
					if len(pod.Annotations) != 0 {
						errMsg = pod.Annotations[pnErrMsgAnnotation]
					}
					if errMsg == "" {
						errMsg = "unknown error"
					}
				}

				if pn.Status.SetNodeNotReady(node.Name, "InitOVSBridgeFailed", errMsg) {
					conditionsUpdated = true
				}
				notReadyNodes = append(notReadyNodes, node.Name)
			}
		}

		expectNodes = append(readyNodes, notReadyNodes...)
		for _, c := range pn.Status.Conditions {
			if !slices.Contains(expectNodes, c.Node) {
				if pn.Status.RemoveNodeConditions(c.Node) {
					conditionsUpdated = true
				}
			}
		}

		if conditionsUpdated || len(util.DiffStringSlice(pn.Status.ReadyNodes, readyNodes)) != 0 ||
			len(util.DiffStringSlice(pn.Status.NotReadyNodes, notReadyNodes)) != 0 {
			pn.Status.ReadyNodes = readyNodes
			pn.Status.NotReadyNodes = notReadyNodes
			pn.Status.Ready = (len(notReadyNodes) == 0)
			if _, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), pn, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
			}
		}
	}
}
