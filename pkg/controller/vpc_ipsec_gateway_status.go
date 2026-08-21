package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) reconcileVpcIPsecGatewayOVNRoutes(gw *kubeovnv1.VpcIPsecGateway) error {
	if err := c.deleteVpcIPsecGatewayOVNRoutes(gw); err != nil {
		return err
	}
	if !gw.DeletionTimestamp.IsZero() {
		return nil
	}

	externalIDs := map[string]string{
		ovs.ExternalIDVendor:          util.CniTypeName,
		ovs.ExternalIDVpcIPsecGateway: gw.Name,
	}
	for _, cidr := range gw.Spec.RemoteCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if err := c.OVNNbClient.AddLogicalRouterStaticRoute(
			gw.Spec.Vpc, util.MainRouteTable, convertPolicy(kubeovnv1.PolicyDst), cidr, nil, externalIDs, gw.Spec.LanIP,
		); err != nil {
			klog.Errorf("failed to add static route for ipsec gw %s cidr %s: %v", gw.Name, cidr, err)
			return err
		}
	}
	return nil
}

func (c *Controller) deleteVpcIPsecGatewayOVNRoutes(gw *kubeovnv1.VpcIPsecGateway) error {
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:          util.CniTypeName,
		ovs.ExternalIDVpcIPsecGateway: gw.Name,
	}
	if err := c.OVNNbClient.DeleteLogicalRouterStaticRouteByExternalIDs(gw.Spec.Vpc, externalIDs); err != nil {
		klog.Errorf("failed to delete static routes for ipsec gw %s: %v", gw.Name, err)
		return err
	}
	return nil
}

func (c *Controller) ipsecGwReadyState(gw *kubeovnv1.VpcIPsecGateway) (bool, string) {
	pod, err := c.podsLister.Pods(c.ipsecGwNamespace(gw)).Get(util.GenIPsecGwPodName(gw.Name))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, "gateway pod not found"
		}
		return false, err.Error()
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false, fmt.Sprintf("gateway pod phase is %s", pod.Status.Phase)
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return false, "gateway container not ready"
		}
	}
	if pod.Annotations[util.VpcIPsecGatewayInitAnnotation] != "true" {
		return false, "gateway not initialized"
	}
	return true, "IPsec gateway is ready"
}

func (c *Controller) patchIPsecGwStatus(gw *kubeovnv1.VpcIPsecGateway, ready bool, phase, message string) error {
	localCIDRs, err := c.resolveIPsecLocalCIDRs(gw)
	if err != nil {
		klog.Warningf("failed to resolve local cidrs for %s: %v", gw.Name, err)
		localCIDRs = gw.Spec.LocalCIDRs
	}

	newStatus := gw.Status.DeepCopy()
	newStatus.Ready = ready
	newStatus.Phase = phase
	newStatus.Message = message
	newStatus.LanIP = gw.Spec.LanIP
	newStatus.RemoteEndpoint = gw.Spec.RemoteEndpoint
	newStatus.RemoteCIDRs = slices.Clone(gw.Spec.RemoteCIDRs)
	newStatus.LocalCIDRs = localCIDRs
	newStatus.ExternalSubnet = gw.Spec.ExternalSubnet
	newStatus.Selector = slices.Clone(gw.Spec.Selector)
	newStatus.Tolerations = gw.Spec.Tolerations
	newStatus.Affinity = gw.Spec.Affinity
	newStatus.Workload = kubeovnv1.VpcIPsecWorkload{
		APIVersion: "apps/v1",
		Kind:       "StatefulSet",
		Name:       util.GenIPsecGwName(gw.Name),
	}

	pod, err := c.podsLister.Pods(c.ipsecGwNamespace(gw)).Get(util.GenIPsecGwPodName(gw.Name))
	if err == nil && pod.Spec.NodeName != "" {
		newStatus.Workload.Nodes = []string{pod.Spec.NodeName}
	}

	if reflect.DeepEqual(gw.Status, *newStatus) {
		return nil
	}

	bytes, err := newStatus.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().VpcIPsecGateways().Patch(context.Background(),
		gw.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	if err != nil {
		klog.Errorf("failed to patch status for vpc ipsec gateway %s: %v", gw.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleAddVpcIPsecGwFinalizer(gw *kubeovnv1.VpcIPsecGateway) error {
	if !gw.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(gw, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newGw := gw.DeepCopy()
	controllerutil.AddFinalizer(newGw, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(gw, newGw)
	if err != nil {
		klog.Errorf("failed to generate patch payload for vpc ipsec gateway %s: %v", gw.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().VpcIPsecGateways().Patch(context.Background(),
		gw.Name, types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("failed to add finalizer for vpc ipsec gateway %s: %v", gw.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDeleteVpcIPsecGwFinalizer(gw *kubeovnv1.VpcIPsecGateway) error {
	if !controllerutil.ContainsFinalizer(gw, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newGw := gw.DeepCopy()
	controllerutil.RemoveFinalizer(newGw, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(gw, newGw)
	if err != nil {
		klog.Errorf("failed to generate patch payload for vpc ipsec gateway %s: %v", gw.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().VpcIPsecGateways().Patch(context.Background(),
		gw.Name, types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("failed to remove finalizer for vpc ipsec gateway %s: %v", gw.Name, err)
		return err
	}
	return nil
}
