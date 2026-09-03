package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const vpcWireGuardRouteVendor = "kube-ovn-vpc-wireguard"

func (c *Controller) vpcWireGuardNamespace(gw *kubeovnv1.VpcWireGuard) string {
	if gw.Spec.Namespace != "" {
		return gw.Spec.Namespace
	}
	return c.config.PodNamespace
}

func vpcWireGuardRouteExternalIDs(name string) map[string]string {
	return map[string]string{
		"vendor":        vpcWireGuardRouteVendor,
		"vpc-wireguard": name,
	}
}

func (c *Controller) enqueueAddVpcWireGuard(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcWireGuard)).String()
	klog.V(3).Infof("enqueue add vpc-wireguard %s", key)
	c.addOrUpdateVpcWireGuardQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcWireGuard(oldObj, newObj any) {
	oldGw := oldObj.(*kubeovnv1.VpcWireGuard)
	newGw := newObj.(*kubeovnv1.VpcWireGuard)
	if !newGw.DeletionTimestamp.IsZero() || !reflect.DeepEqual(oldGw.Spec, newGw.Spec) {
		key := cache.MetaObjectToName(newGw).String()
		klog.V(3).Infof("enqueue update vpc-wireguard %s", key)
		c.addOrUpdateVpcWireGuardQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVpcWireGuard(obj any) {
	var gw *kubeovnv1.VpcWireGuard
	switch t := obj.(type) {
	case *kubeovnv1.VpcWireGuard:
		gw = t
	case cache.DeletedFinalStateUnknown:
		g, ok := t.Obj.(*kubeovnv1.VpcWireGuard)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		gw = g
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}
	key := cache.MetaObjectToName(gw).String()
	klog.V(3).Infof("enqueue delete vpc-wireguard %s", key)
	c.delVpcWireGuardQueue.Add(key)
}

func (c *Controller) handleAddOrUpdateVpcWireGuard(key string) error {
	c.vpcWireGuardKeyMutex.LockKey(key)
	defer func() { _ = c.vpcWireGuardKeyMutex.UnlockKey(key) }()

	gw, err := c.vpcWireGuardLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if !gw.DeletionTimestamp.IsZero() {
		return c.cleanupVpcWireGuard(gw)
	}

	if err := c.ensureVpcWireGuardFinalizer(gw); err != nil {
		return err
	}
	gw, err = c.vpcWireGuardLister.Get(key)
	if err != nil {
		return err
	}

	if vpcNatImage == "" {
		return errors.New("vpc nat image is not configured; set ovn-vpc-nat-config image for WireGuard pods")
	}

	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		return fmt.Errorf("failed to get vpc %s: %w", gw.Spec.Vpc, err)
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		return fmt.Errorf("failed to get subnet %s: %w", gw.Spec.Subnet, err)
	}
	clientSubnet, err := c.subnetsLister.Get(gw.Spec.ClientSubnet)
	if err != nil {
		return fmt.Errorf("failed to get client subnet %s: %w", gw.Spec.ClientSubnet, err)
	}

	publicKey, err := c.ensureVpcWireGuardServerSecret(gw)
	if err != nil {
		return err
	}

	serverTunnelIP, err := c.allocateVpcWireGuardServerIP(gw, clientSubnet)
	if err != nil {
		return err
	}

	if err := c.writeVpcWireGuardServerConfig(gw, serverTunnelIP, clientSubnet.Spec.CIDRBlock); err != nil {
		return err
	}

	if err := c.createOrUpdateVpcWireGuardSts(gw); err != nil {
		return err
	}

	lanIP, err := c.getVpcWireGuardLanIP(gw)
	if err != nil {
		return err
	}

	if err := c.reconcileVpcWireGuardRoutes(gw, clientSubnet, lanIP); err != nil {
		return err
	}

	endpoint, err := c.reconcileVpcWireGuardExposure(gw, lanIP)
	if err != nil {
		return err
	}

	dataplaneErr := c.syncVpcWireGuardDataplane(gw, serverTunnelIP, clientSubnet.Spec.CIDRBlock)
	if dataplaneErr != nil {
		klog.Warningf("wireguard dataplane is not ready for %s: %v", gw.Name, dataplaneErr)
	}

	newGw := gw.DeepCopy()
	newGw.Status.Ready = dataplaneErr == nil && lanIP != "" && endpoint != "" && publicKey != ""
	newGw.Status.LanIP = lanIP
	newGw.Status.Endpoint = endpoint
	newGw.Status.PublicKey = publicKey
	newGw.Status.ClientCIDR = clientSubnet.Spec.CIDRBlock
	newGw.Status.ServerTunnelIP = serverTunnelIP
	newGw.Status.ServerKeySecret = util.GenVpcWireGuardServerSecretName(gw.Name)
	conds := kubeovnv1.Conditions(newGw.Status.Conditions)
	if newGw.Status.Ready {
		conds.SetReady("WireGuardReady", newGw.Generation)
	} else {
		reason := "WireGuardNotReady"
		msg := ""
		if dataplaneErr != nil {
			msg = dataplaneErr.Error()
		}
		conds.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, reason, msg, newGw.Generation)
	}
	newGw.Status.Conditions = conds
	needPeerRefresh := gw.Status.Endpoint != endpoint || gw.Status.PublicKey != publicKey || gw.Status.Ready != newGw.Status.Ready
	if _, err := c.config.KubeOvnClient.KubeovnV1().VpcWireGuards().UpdateStatus(context.Background(), newGw, metav1.UpdateOptions{}); err != nil {
		klog.Error(err)
		return err
	}

	if needPeerRefresh {
		c.enqueueVpcWireGuardPeers(gw.Name)
	}
	if dataplaneErr != nil {
		return dataplaneErr
	}
	return nil
}

func (c *Controller) handleDelVpcWireGuard(key string) error {
	c.vpcWireGuardKeyMutex.LockKey(key)
	defer func() { _ = c.vpcWireGuardKeyMutex.UnlockKey(key) }()

	gw, err := c.vpcWireGuardLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.cleanupVpcWireGuard(gw)
}

func (c *Controller) cleanupVpcWireGuard(gw *kubeovnv1.VpcWireGuard) error {
	ns := c.vpcWireGuardNamespace(gw)
	stsName := util.GenVpcWireGuardName(gw.Name)
	if err := c.config.KubeClient.AppsV1().StatefulSets(ns).Delete(context.Background(), stsName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Error(err)
		return err
	}
	if err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Delete(context.Background(), util.GenVpcWireGuardDnatName(gw.Name), metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Delete(context.Background(), util.GenVpcWireGuardFipName(gw.Name), metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if err := c.deleteVpcWireGuardRoutes(gw); err != nil {
		return err
	}
	c.ipam.ReleaseAddressByNic(util.VpcWireGuardServerIPAMName(gw.Name), util.VpcWireGuardServerIPAMName(gw.Name), gw.Spec.ClientSubnet)

	newGw := gw.DeepCopy()
	controllerutil.RemoveFinalizer(newGw, util.KubeOVNControllerFinalizer)
	if !reflect.DeepEqual(gw.Finalizers, newGw.Finalizers) {
		if _, err := c.config.KubeOvnClient.KubeovnV1().VpcWireGuards().Update(context.Background(), newGw, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) ensureVpcWireGuardFinalizer(gw *kubeovnv1.VpcWireGuard) error {
	if controllerutil.ContainsFinalizer(gw, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newGw := gw.DeepCopy()
	controllerutil.AddFinalizer(newGw, util.KubeOVNControllerFinalizer)
	_, err := c.config.KubeOvnClient.KubeovnV1().VpcWireGuards().Update(context.Background(), newGw, metav1.UpdateOptions{})
	return err
}
