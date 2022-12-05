package controller

import (
	"context"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

func (c *Controller) resyncProviderNetworkStatus() {
	klog.Infof("start to sync ProviderNetwork status")
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

	for _, cachedPn := range pns {
		pn := cachedPn.DeepCopy()
		var readyNodes, notReadyNodes, expectNodes []string
		for _, node := range nodes {
			if util.ContainsString(pn.Spec.ExcludeNodes, node.Name) {
				pn.Status.RemoveNodeConditions(node.Name)
				continue
			}
			if node.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)] == "true" {
				pn.Status.SetNodeReady(node.Name, "InitOVSBridgeSucceeded", "")
				readyNodes = append(readyNodes, node.Name)
			} else {
				pods, err := c.config.KubeClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
					LabelSelector: "app=kube-ovn-cni",
					FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name),
				})
				if err != nil {
					klog.Errorf("failed to list pod: %v", err)
					continue
				}

				var errMsg string
				if len(pods.Items) == 1 && pods.Items[0].Annotations != nil {
					errMsg = pods.Items[0].Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, pn.Name)]
				}
				pn.Status.SetNodeNotReady(node.Name, "InitOVSBridgeFailed", errMsg)
				notReadyNodes = append(notReadyNodes, node.Name)
			}
		}

		expectNodes = append(readyNodes, notReadyNodes...)
		conditionsChange := false
		for _, c := range pn.Status.Conditions {
			if !util.ContainsString(expectNodes, c.Node) {
				pn.Status.RemoveNodeConditions(c.Node)
				conditionsChange = true
			}
		}

		if conditionsChange || len(util.DiffStringSlice(pn.Status.ReadyNodes, readyNodes)) != 0 ||
			len(util.DiffStringSlice(pn.Status.NotReadyNodes, notReadyNodes)) != 0 {
			pn.Status.ReadyNodes = readyNodes
			pn.Status.NotReadyNodes = notReadyNodes
			pn.Status.Ready = (len(notReadyNodes) == 0)
			if _, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), pn, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to update provider network %s: %v", pn.Name, err)
			}
		}
	}
}
