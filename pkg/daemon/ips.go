package daemon

import (
	"context"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"strings"
	"time"
)

func (c *cniServerHandler) runAddPodWorker() {
	for c.processNextAddPodWorkItem() {
	}
}

func (c *cniServerHandler) processNextAddPodWorkItem() bool {
	obj, shutdown := c.IPsQueue.Get()
	if shutdown {
		return false
	}
	now := time.Now()

	err := func(obj interface{}) error {
		defer c.IPsQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.IPsQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		klog.Infof("handle add ips %s", key)
		if err := c.handleUpdateIPS(key); err != nil {
			c.IPsQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		last := time.Since(now)
		klog.Infof("take %d ms to handle add ips %s", last.Milliseconds(), key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *cniServerHandler) handleUpdateIPS(key string) error {

	input := strings.Split(key, "//")
	ips := input[0]
	podInfo := strings.Split(ips, ".")
	podName := podInfo[0]
	podNs := podInfo[1]
	_, err := c.Controller.podsLister.Pods(podNs).Get(podName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("get pod %s/%s failed %v", podNs, podName, err)
		return err
	}

	subnet := input[1]
	oriIpCr, err := c.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ips, metav1.GetOptions{})
	if err != nil {
		errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ips, err)
		klog.Error(errMsg)
		return errMsg
	}
	ipCr := oriIpCr.DeepCopy()
	ipCr.Spec.NodeName = c.Config.NodeName
	ipCr.Spec.AttachIPs = []string{}
	ipCr.Labels[subnet] = ""
	ipCr.Spec.AttachSubnets = []string{}
	ipCr.Spec.AttachMacs = []string{}
	if _, err := c.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ipCr, metav1.UpdateOptions{}); err != nil {
		errMsg := fmt.Errorf("failed to update ip crd for %s, %v", ips, err)
		klog.Error(errMsg)
		return errMsg
	}
	return nil
}
