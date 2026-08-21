package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcIPsecEnabled   = "unknown"
	vpcIPsecCmVersion = ""
)

const (
	ipsecGwContainerName = "vpc-ipsec-gw"
	ipsecGwConfVolume    = "ipsec-conf"
	ipsecGwPSKVolume     = "ipsec-psk"
	ipsecGwConfMountPath = "/etc/swanctl/kube-ovn"
	ipsecGwPSKMountPath  = "/etc/ipsec.d/psk"
)

const swanctlConfTemplate = `connections {
    vpc-ipsec-{{.Name}} {
        version = 2
        local_addrs = %any
        remote_addrs = {{.RemoteEndpoint}}
        local {
            auth = psk
        }
        remote {
            auth = psk
        }
        children {
            net {
                local_ts = {{.LocalTrafficSelectors}}
                remote_ts = {{.RemoteTrafficSelectors}}
                start_action = start
                dpd_action = restart
            }
        }
    }
}
`

type swanctlParams struct {
	Name                   string
	RemoteEndpoint         string
	LocalTrafficSelectors  string
	RemoteTrafficSelectors string
}

func (c *Controller) ipsecGwNamespace(gw *kubeovnv1.VpcIPsecGateway) string {
	if gw.Spec.Namespace != "" {
		return gw.Spec.Namespace
	}
	return c.config.PodNamespace
}

func (c *Controller) resyncVpcIPsecGwConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcIPsecGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get %s: %v", util.VpcIPsecGatewayConfig, err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-ipsec-gw"] == "false" {
		if vpcIPsecEnabled == "false" {
			return
		}
		klog.Info("start to clean up vpc ipsec gateway")
		if err := c.cleanUpVpcIPsecGw(); err != nil {
			klog.Errorf("failed to clean up vpc ipsec gateway: %v", err)
			return
		}
		vpcIPsecEnabled = "false"
		vpcIPsecCmVersion = ""
		klog.Info("finish clean up vpc ipsec gateway")
		return
	}
	if vpcIPsecEnabled == "true" && vpcIPsecCmVersion == cm.ResourceVersion {
		return
	}
	gws, err := c.vpcIPsecGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc ipsec gateway: %v", err)
		return
	}
	vpcIPsecEnabled = "true"
	vpcIPsecCmVersion = cm.ResourceVersion
	for _, gw := range gws {
		c.addOrUpdateVpcIPsecGatewayQueue.Add(gw.Name)
	}
	klog.Info("finish establishing vpc-ipsec-gateway")
}

func (c *Controller) enqueueAddVpcIPsecGw(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcIPsecGateway)).String()
	klog.V(3).Infof("enqueue add vpc-ipsec-gw %s", key)
	c.addOrUpdateVpcIPsecGatewayQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcIPsecGw(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.VpcIPsecGateway)).String()
	klog.V(3).Infof("enqueue update vpc-ipsec-gw %s", key)
	c.addOrUpdateVpcIPsecGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcIPsecGw(obj any) {
	var gw *kubeovnv1.VpcIPsecGateway
	switch t := obj.(type) {
	case *kubeovnv1.VpcIPsecGateway:
		gw = t
	case cache.DeletedFinalStateUnknown:
		g, ok := t.Obj.(*kubeovnv1.VpcIPsecGateway)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		gw = g
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	ns := gw.Spec.Namespace
	if ns == "" {
		ns = c.config.PodNamespace
	}
	key := ns + "/" + gw.Name
	klog.V(3).Infof("enqueue del vpc-ipsec-gw %s", key)
	c.delVpcIPsecGatewayQueue.Add(key)
}

func (c *Controller) enqueueAddOrUpdateVpcIPsecGwByName(gwName, reason string) {
	if gwName == "" || c.addOrUpdateVpcIPsecGatewayQueue == nil {
		return
	}
	klog.V(3).Infof("enqueue vpc-ipsec-gw %s from %s", gwName, reason)
	c.addOrUpdateVpcIPsecGatewayQueue.Add(gwName)
}

func (c *Controller) handleAddOrUpdateVpcIPsecGw(key string) error {
	gw, err := c.vpcIPsecGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if !gw.DeletionTimestamp.IsZero() {
		return c.handleDelVpcIPsecGw(c.ipsecGwNamespace(gw) + "/" + gw.Name)
	}

	c.vpcIPsecGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcIPsecGwKeyMutex.UnlockKey(key) }()

	if err := c.handleAddVpcIPsecGwFinalizer(gw); err != nil {
		klog.Errorf("failed to add vpc ipsec gateway finalizer for %s: %v", key, err)
		return err
	}

	if vpcIPsecEnabled != "true" {
		return c.patchIPsecGwStatus(gw, false, "Disabled", "vpc ipsec gateway feature is disabled")
	}
	if vpcIPsecImage == "" {
		return c.patchIPsecGwStatus(gw, false, "Error", fmt.Sprintf("%s image is not configured", util.VpcIPsecConfig))
	}

	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		klog.Errorf("failed to get vpc %s: %v", gw.Spec.Vpc, err)
		return err
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		klog.Errorf("failed to get subnet %s: %v", gw.Spec.Subnet, err)
		return err
	}

	if err := c.ensureIPsecGwConf(gw); err != nil {
		klog.Errorf("failed to ensure ipsec conf for %s: %v", key, err)
		return err
	}

	if err := c.ensureIPsecGwStatefulSet(gw); err != nil {
		klog.Errorf("failed to ensure ipsec statefulset for %s: %v", key, err)
		return err
	}

	if err := c.reconcileVpcIPsecGatewayOVNRoutes(gw); err != nil {
		klog.Errorf("failed to reconcile ovn routes for ipsec gw %s: %v", key, err)
		return err
	}

	ready, message := c.ipsecGwReadyState(gw)
	phase := "Pending"
	if ready {
		phase = "Ready"
	}
	return c.patchIPsecGwStatus(gw, ready, phase, message)
}

func (c *Controller) handleDelVpcIPsecGw(key string) error {
	parts := strings.SplitN(key, "/", 2)
	var stsNamespace, gwName string
	if len(parts) == 2 {
		stsNamespace, gwName = parts[0], parts[1]
	} else {
		stsNamespace, gwName = c.config.PodNamespace, key
	}

	c.vpcIPsecGwKeyMutex.LockKey(gwName)
	defer func() { _ = c.vpcIPsecGwKeyMutex.UnlockKey(gwName) }()

	workloadName := util.GenIPsecGwName(gwName)
	klog.Infof("delete vpc ipsec gw %s in namespace %s", workloadName, stsNamespace)

	if err := c.config.KubeClient.AppsV1().StatefulSets(stsNamespace).Delete(context.Background(),
		workloadName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Error(err)
		return err
	}

	if err := c.config.KubeClient.CoreV1().ConfigMaps(stsNamespace).Delete(context.Background(),
		util.GenIPsecGwConfName(gwName), metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Error(err)
		return err
	}

	gw, err := c.vpcIPsecGatewayLister.Get(gwName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if err := c.deleteVpcIPsecGatewayOVNRoutes(gw); err != nil {
		klog.Error(err)
		return err
	}

	if err := c.handleDeleteVpcIPsecGwFinalizer(gw); err != nil {
		klog.Errorf("failed to remove finalizer for vpc ipsec gateway %s: %v", gwName, err)
		return err
	}
	return nil
}

func (c *Controller) handleInitVpcIPsecGw(key string) error {
	c.vpcIPsecGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcIPsecGwKeyMutex.UnlockKey(key) }()

	if vpcIPsecEnabled != "true" {
		return nil
	}

	gw, err := c.vpcIPsecGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	pod, err := c.podsLister.Pods(c.ipsecGwNamespace(gw)).Get(util.GenIPsecGwPodName(gw.Name))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if pod.Status.Phase != corev1.PodRunning || pod.Annotations[util.VpcIPsecGatewayInitAnnotation] == "true" {
		return nil
	}

	if err := c.execIPsecGwInit(pod); err != nil {
		klog.Errorf("failed to init vpc ipsec gateway pod %s: %v", pod.Name, err)
		return err
	}

	patch := util.KVPatch{util.VpcIPsecGatewayInitAnnotation: "true"}
	if err := util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
		klog.Errorf("failed to patch ipsec gw pod %s: %v", pod.Name, err)
		return err
	}

	c.addOrUpdateVpcIPsecGatewayQueue.Add(key)
	return nil
}

func (c *Controller) execIPsecGwInit(pod *corev1.Pod) error {
	cmd := []string{"/kube-ovn/ipsec-gateway.sh", "init"}
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(
		c.config.KubeClient, c.config.KubeRestConfig,
		pod.Namespace, pod.Name, ipsecGwContainerName, cmd...)
	if err != nil {
		klog.Errorf("failed to exec init in pod %s/%s: stdout=%s stderr=%s err=%v",
			pod.Namespace, pod.Name, stdOutput, errOutput, err)
		return err
	}
	klog.Infof("initialized vpc ipsec gateway pod %s/%s", pod.Namespace, pod.Name)
	return nil
}

func (c *Controller) cleanUpVpcIPsecGw() error {
	gws, err := c.vpcIPsecGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc ipsec gateway: %v", err)
		return err
	}
	for _, gw := range gws {
		ns := gw.Spec.Namespace
		if ns == "" {
			ns = c.config.PodNamespace
		}
		c.delVpcIPsecGatewayQueue.Add(ns + "/" + gw.Name)
	}
	return nil
}

func (c *Controller) initVpcIPsecGw() error {
	klog.Infof("init all vpc ipsec gateways")
	if vpcIPsecEnabled != "true" {
		klog.Warning("vpc ipsec gateway feature is disabled")
		return nil
	}
	gws, err := c.vpcIPsecGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc ipsec gateways: %v", err)
		return err
	}
	for _, gw := range gws {
		pod, err := c.podsLister.Pods(c.ipsecGwNamespace(gw)).Get(util.GenIPsecGwPodName(gw.Name))
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to get ipsec gw pod for %s: %v", gw.Name, err)
			}
			continue
		}
		if _, hasInit := pod.Annotations[util.VpcIPsecGatewayInitAnnotation]; hasInit {
			continue
		}
		c.initVpcIPsecGatewayQueue.Add(gw.Name)
	}
	return nil
}
