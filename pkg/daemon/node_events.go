package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	eventutil "k8s.io/client-go/tools/record/util"
	"k8s.io/klog/v2"
)

const (
	addNodeFailedReason    = "AddNodeFailed"
	updateNodeFailedReason = "UpdateNodeFailed"
)

type nodeFailureKey struct {
	nodeName string
	nodeUID  types.UID
	reason   string
	stage    string
}

const nodeEventWriteTimeout = 3 * time.Second

func recordNodeFailureEvent(recorder record.EventRecorder, node *corev1.Node, reason, stage string, err error) {
	if recorder == nil || node == nil || err == nil {
		return
	}
	recorder.Eventf(node, corev1.EventTypeWarning, reason, "stage=%s error=%v", stage, err)
}

func (c *Controller) recordNodeFailure(node *corev1.Node, reason, stage string, err error) {
	if node == nil || err == nil {
		return
	}

	key := nodeFailureKey{nodeName: node.Name, nodeUID: node.UID, reason: reason, stage: stage}
	message := err.Error()
	c.nodeFailuresMutex.Lock()
	if c.nodeFailures == nil {
		c.nodeFailures = make(map[nodeFailureKey]string)
	}
	for existingKey := range c.nodeFailures {
		if existingKey.nodeName == node.Name && existingKey.nodeUID != node.UID {
			delete(c.nodeFailures, existingKey)
		}
	}
	if c.nodeFailures[key] == message {
		c.nodeFailuresMutex.Unlock()
		return
	}
	c.nodeFailures[key] = message
	c.nodeFailuresMutex.Unlock()

	recordNodeFailureEvent(c.recorder, node, reason, stage, err)
}

func (c *Controller) clearNodeFailure(node *corev1.Node, reason, stage string) {
	c.nodeFailuresMutex.Lock()
	for existingKey := range c.nodeFailures {
		if existingKey.nodeName == node.Name && (existingKey.nodeUID != node.UID || existingKey.reason == reason && existingKey.stage == stage) {
			delete(c.nodeFailures, existingKey)
		}
	}
	c.nodeFailuresMutex.Unlock()
}

func (c *Controller) reconcileNodeNetworkStage(node *corev1.Node, stage string, reconcile func() error) error {
	if err := reconcile(); err != nil {
		c.recordNodeFailure(node, updateNodeFailedReason, stage, err)
		return err
	}
	c.clearNodeFailure(node, updateNodeFailedReason, stage)
	return nil
}

func recordNodeFailureEventSync(client kubernetes.Interface, node *corev1.Node, component, reason, stage string, failure error) error {
	if client == nil || node == nil || failure == nil {
		return errors.New("node event requires client, node, and failure")
	}

	now := metav1.Now()
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eventutil.GenerateEventName(node.Name, now.UnixNano()),
			Namespace: metav1.NamespaceDefault,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:            "Node",
			APIVersion:      corev1.SchemeGroupVersion.String(),
			Name:            node.Name,
			UID:             node.UID,
			ResourceVersion: node.ResourceVersion,
		},
		Reason:              reason,
		Message:             fmt.Sprintf("stage=%s error=%v", stage, failure),
		Source:              corev1.EventSource{Component: component},
		FirstTimestamp:      now,
		LastTimestamp:       now,
		Count:               1,
		Type:                corev1.EventTypeWarning,
		ReportingController: component,
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeEventWriteTimeout)
	defer cancel()
	_, err := client.CoreV1().Events(metav1.NamespaceDefault).Create(ctx, event, metav1.CreateOptions{})
	return err
}

func (c *Controller) recordLocalNodeFailureSync(stage string, failure error) {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Warningf("failed to get node %s from cache while recording stage %s failure: %v", c.config.NodeName, stage, err)
		ctx, cancel := context.WithTimeout(context.Background(), nodeEventWriteTimeout)
		defer cancel()
		node, err = c.config.KubeClient.CoreV1().Nodes().Get(ctx, c.config.NodeName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get node %s while recording stage %s failure: %v", c.config.NodeName, stage, err)
			return
		}
	}
	if err := recordNodeFailureEventSync(c.config.KubeClient, node, c.config.NodeName, updateNodeFailedReason, stage, failure); err != nil {
		klog.Errorf("failed to record node %s stage %s failure: %v", c.config.NodeName, stage, err)
	}
}
