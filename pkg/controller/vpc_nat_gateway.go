package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatEnabled   = "unknown"
	VpcNatCmVersion = ""
	natGwCreatedAT  = ""
)

const (
	natGwInit             = "init"
	natGwEipAdd           = "eip-add"
	natGwEipDel           = "eip-del"
	natGwDnatAdd          = "dnat-add"
	natGwDnatDel          = "dnat-del"
	natGwSnatAdd          = "snat-add"
	natGwSnatDel          = "snat-del"
	natGwEipIngressQoSAdd = "eip-ingress-qos-add"
	natGwEipIngressQoSDel = "eip-ingress-qos-del"
	QoSAdd                = "qos-add"
	QoSDel                = "qos-del"
	natGwEipEgressQoSAdd  = "eip-egress-qos-add"
	natGwEipEgressQoSDel  = "eip-egress-qos-del"
	natGwSubnetFipAdd     = "floating-ip-add"
	natGwSubnetFipDel     = "floating-ip-del"
	natGwSubnetRouteAdd   = "subnet-route-add"
	natGwSubnetRouteDel   = "subnet-route-del"

	getIptablesVersion = "get-iptables-version"
)

// natGwNamespace returns the namespace where the NAT gateway StatefulSet/Pod should be created.
// If gw.Spec.Namespace is set, it is used; otherwise the controller's own namespace is used.
func (c *Controller) natGwNamespace(gw *kubeovnv1.VpcNatGateway) string {
	if gw.Spec.Namespace != "" {
		return gw.Spec.Namespace
	}
	return c.config.PodNamespace
}

// getNatGwReplicas returns the effective number of replicas for the NAT gateway.
// Defaults to 1 if not specified.
func getNatGwReplicas(gw *kubeovnv1.VpcNatGateway) int32 {
	if gw.Spec.Replicas > 0 {
		return gw.Spec.Replicas
	}
	return 1
}

// natGwNamespaceByName looks up the VpcNatGateway by name and returns natGwNamespace.
// Falls back to c.config.PodNamespace when the gw is not found (e.g., already deleted).
func (c *Controller) natGwNamespaceByName(gwName string) string {
	gw, err := c.vpcNatGatewayLister.Get(gwName)
	if err != nil {
		return c.config.PodNamespace
	}
	return c.natGwNamespace(gw)
}

func (c *Controller) resyncVpcNatGwConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-vpc-nat-gw-config, %v", err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-nat-gw"] == "false" {
		if vpcNatEnabled == "false" {
			return
		}
		klog.Info("start to clean up vpc nat gateway")
		if err := c.cleanUpVpcNatGw(); err != nil {
			klog.Errorf("failed to clean up vpc nat gateway, %v", err)
			return
		}
		vpcNatEnabled = "false"
		VpcNatCmVersion = ""
		klog.Info("finish clean up vpc nat gateway")
		return
	}
	if vpcNatEnabled == "true" && VpcNatCmVersion == cm.ResourceVersion {
		return
	}
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vpc nat gateway, %v", err)
		return
	}
	vpcNatEnabled = "true"
	VpcNatCmVersion = cm.ResourceVersion
	for _, gw := range gws {
		c.addOrUpdateVpcNatGatewayQueue.Add(gw.Name)
	}
	klog.Info("finish establishing vpc-nat-gateway")
}

func (c *Controller) enqueueAddVpcNatGw(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcNatGateway)).String()
	klog.V(3).Infof("enqueue add vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueAddOrUpdateVpcNatGwByName(gwName, reason string) {
	if gwName == "" || c.addOrUpdateVpcNatGatewayQueue == nil {
		return
	}
	klog.V(3).Infof("enqueue vpc-nat-gw %s from %s", gwName, reason)
	c.addOrUpdateVpcNatGatewayQueue.Add(gwName)
}

func (c *Controller) enqueueUpdateVpcNatGw(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.VpcNatGateway)).String()
	klog.V(3).Infof("enqueue update vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcNatGw(obj any) {
	var gw *kubeovnv1.VpcNatGateway
	switch t := obj.(type) {
	case *kubeovnv1.VpcNatGateway:
		gw = t
	case cache.DeletedFinalStateUnknown:
		g, ok := t.Obj.(*kubeovnv1.VpcNatGateway)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		gw = g
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	// Use "namespace/gwName" as the queue key so the delete handler knows where the STS lives.
	natGwNs := gw.Spec.Namespace
	if natGwNs == "" {
		natGwNs = c.config.PodNamespace
	}
	key := natGwNs + "/" + gw.Name
	klog.V(3).Infof("enqueue del vpc-nat-gw %s", key)
	c.delVpcNatGatewayQueue.Add(key)

	// Trigger QoS Policy reconcile after NatGw is deleted
	// This allows the QoS Policy to remove its finalizer if no other NatGws are using it
	if gw.Status.QoSPolicy != "" {
		c.updateQoSPolicyQueue.Add(gw.Status.QoSPolicy)
	}
}

// handleDelVpcNatGw handles NAT gateways when they've been deleted
// This function should soon not be needed anymore, due to the introduction of the finalizer
// If the finalizer is present, deletions are handled by the update workflow which will detect
// the new deletionTimestamp set on the resource.
// This is still useful for legacy NAT gateways which have not been updated with one.
func (c *Controller) handleDelVpcNatGw(key string) error {
	// key is "namespace/gwName" as encoded by enqueueDeleteVpcNatGw.
	// Parse gwName first so we can lock by gwName — consistent with all other handlers
	// (add/update/init) that lock by gwName alone. Using the composite key here would
	// allow delete to run concurrently with a reconcile for the same gateway.
	parts := strings.SplitN(key, "/", 2)
	var stsNamespace, gwName string
	if len(parts) == 2 {
		stsNamespace, gwName = parts[0], parts[1]
	} else {
		// Fallback for legacy queue entries without namespace prefix
		stsNamespace, gwName = c.config.PodNamespace, key
	}

	c.vpcNatGwKeyMutex.LockKey(gwName)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(gwName) }()
	workloadName := util.GenNatGwName(gwName)
	klog.Infof("delete vpc nat gw %s in namespace %s", workloadName, stsNamespace)

	// STS are legacy NAT gateways, which might not have the finalizer yet.
	if err := c.config.KubeClient.AppsV1().StatefulSets(stsNamespace).Delete(context.Background(),
		workloadName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Error(err)
		return err
	}

	// Get the gateway to reconcile routes
	// It might already have been deleted. If so, the finalizer guarantees we cleaned up properly.
	gw, err := c.vpcNatGatewayLister.Get(gwName)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Error(err)
		return err
	}

	// Gateway doesn't exist anymore, there's nothing to clean
	if k8serrors.IsNotFound(err) {
		return nil
	}

	// Reconcile the routes to clean up everything (policies, BFD, ...)
	if err := c.reconcileVpcNatGatewayOVNRoutes(gw); err != nil {
		klog.Error(err)
		return err
	}

	// Remove the finalizer on the gateway to let the object get deleted
	if err := c.handleDeleteVpcNatGwFinalizer(gw); err != nil {
		klog.Errorf("failed to remove finalizer for vpc nat gateway %s: %v", gwName, err)
		return err
	}

	return nil
}

// isVpcNatGwChanged checks if VpcNatGateway spec fields have changed compared to status.
// Note: User-defined annotations (gw.Spec.Annotations) are NOT checked here because
// updating StatefulSet Pod template annotations would trigger Pod recreation.
// TODO: support hot update of runtime Pod annotations directly via patch
func isVpcNatGwChanged(gw *kubeovnv1.VpcNatGateway) bool {
	if !slices.Equal(gw.Spec.ExternalSubnets, gw.Status.ExternalSubnets) {
		return true
	}
	if !slices.Equal(gw.Spec.Selector, gw.Status.Selector) {
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Tolerations, gw.Status.Tolerations) {
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Affinity, gw.Status.Affinity) {
		return true
	}
	if !slices.Equal(gw.Spec.InternalSubnets, gw.Status.InternalSubnets) {
		return true
	}
	if !slices.Equal(gw.Spec.InternalCIDRs, gw.Status.InternalCIDRs) {
		return true
	}
	if gw.Spec.Replicas != gw.Status.Replicas {
		return true
	}
	return false
}

// handleAddOrUpdateVpcNatGw is called when a VPC NAT gateway is added or updated.
// If a VPC NAT gateway is deleted, the deletionTimestamp will be updated and this function will also be called.
func (c *Controller) handleAddOrUpdateVpcNatGw(key string) error {
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if !gw.DeletionTimestamp.IsZero() {
		return c.handleDelVpcNatGw(key)
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()

	if err := c.handleAddVpcNatGwFinalizer(gw); err != nil {
		klog.Errorf("failed to add vpc nat gateway finalizer for %s: %v", key, err)
		return err
	}

	klog.Infof("handle add/update vpc nat gateway %s", key)

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		err = fmt.Errorf("failed to get vpc '%s', err: %w", gw.Spec.Vpc, err)
		klog.Error(err)
		return err
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		err = fmt.Errorf("failed to get subnet '%s', err: %w", gw.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	var natGwPodContainerRestartCount int32
	pod, err := c.getNatGwPod(key, c.natGwNamespace(gw))
	if err == nil {
		if !util.IsNatGwHAMode(gw) {
			if err = c.backfillVpcNatGwLanIPFromPod(pod, key); err != nil {
				klog.Errorf("failed to backfill lanIP for vpc nat gateway %s: %v", key, err)
				return err
			}
		}
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == "vpc-nat-gw" {
				natGwPodContainerRestartCount = containerStatus.RestartCount
				break
			}
		}
	}
	needRestartRecovery := natGwPodContainerRestartCount > 0

	// Choose between Deployment (HA mode) or StatefulSet (legacy mode)
	if util.IsNatGwHAMode(gw) {
		// HA mode: use Deployment
		needToCreate := false
		oldDeploy, err := c.config.KubeClient.AppsV1().Deployments(c.natGwNamespace(gw)).
			Get(context.Background(), util.GenNatGwName(gw.Name), metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Error(err)
				return err
			}
			needToCreate = true
		}

		newDeploy, err := c.genNatGwDeployment(gw)
		if err != nil {
			klog.Error(err)
			return err
		}

		// Handle Deployment creation (early return - QoS will be handled in init flow)
		if needToCreate {
			if _, err := c.config.KubeClient.AppsV1().Deployments(c.natGwNamespace(gw)).
				Create(context.Background(), newDeploy, metav1.CreateOptions{}); err != nil {
				err := fmt.Errorf("failed to create deployment '%s', err: %w", newDeploy.Name, err)
				klog.Error(err)
				return err
			}
			if err = c.patchNatGwStatus(key); err != nil {
				klog.Errorf("failed to patch nat gw deployment status for nat gw %s, %v", key, err)
				return err
			}
			return nil
		}

		hashChanged := oldDeploy.Annotations[util.GenerateHashAnnotation] != newDeploy.Annotations[util.GenerateHashAnnotation]
		parametersChanged := isVpcNatGwChanged(gw)

		if hashChanged || parametersChanged {
			if _, err := c.config.KubeClient.AppsV1().Deployments(c.natGwNamespace(gw)).
				Update(context.Background(), newDeploy, metav1.UpdateOptions{}); err != nil {
				err := fmt.Errorf("failed to update deployment '%s', err: %w", newDeploy.Name, err)
				klog.Error(err)
				return err
			}
		}
	} else {
		// Legacy mode: use StatefulSet
		needToCreate := false
		oldSts, err := c.config.KubeClient.AppsV1().StatefulSets(c.natGwNamespace(gw)).
			Get(context.Background(), util.GenNatGwName(gw.Name), metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Error(err)
				return err
			}
			needToCreate, oldSts = true, nil
		}
		gwChanged := isVpcNatGwChanged(gw)
		needPatchStatus := gwChanged

		newSts, err := c.genNatGwStatefulSet(gw, oldSts, natGwPodContainerRestartCount)
		if err != nil {
			klog.Error(err)
			return err
		}

		// Handle StatefulSet creation (early return - QoS will be handled in init flow)
		if needToCreate {
			if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.natGwNamespace(gw)).
				Create(context.Background(), newSts, metav1.CreateOptions{}); err != nil {
				err := fmt.Errorf("failed to create statefulset '%s', err: %w", newSts.Name, err)
				klog.Error(err)
				return err
			}
			if err = c.patchNatGwStatus(key); err != nil {
				klog.Errorf("failed to patch nat gw sts status for nat gw %s, %v", key, err)
				return err
			}
			return nil
		}

		// Handle StatefulSet update if needed
		// WARNING: This will update STS template directly, which triggers NAT GW Pod recreation.
		// TODO: support hot update of runtime Pod annotations directly via patch
		if gwChanged || needRestartRecovery {
			if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.natGwNamespace(gw)).
				Update(context.Background(), newSts, metav1.UpdateOptions{}); err != nil {
				err := fmt.Errorf("failed to update statefulset '%s', err: %w", newSts.Name, err)
				klog.Error(err)
				return err
			}
		}

		if needPatchStatus {
			if err = c.patchNatGwStatus(key); err != nil {
				klog.Errorf("failed to patch nat gw sts status for nat gw %s, %v", key, err)
				return err
			}
		}
	}

	// Reconcile BFD sessions and OVN routes for HA mode
	if util.IsNatGwHAMode(gw) {
		// Reconcile routes to the NAT gateways so that the traffic from internal subnets is routed to it
		if err = c.reconcileVpcNatGatewayOVNRoutes(gw); err != nil {
			klog.Errorf("failed to reconcile OVN routes for nat gw %s: %v", key, err)
			return err
		}
	}

	// Handle QoS update (independent of StatefulSet/Deployment changes)
	if gw.Spec.QoSPolicy != gw.Status.QoSPolicy {
		if gw.Status.QoSPolicy != "" {
			if err = c.execNatGwQoS(gw, gw.Status.QoSPolicy, QoSDel); err != nil {
				klog.Errorf("failed to del qos for nat gw %s, %v", key, err)
				return err
			}
		}
		if gw.Spec.QoSPolicy != "" {
			if err = c.execNatGwQoS(gw, gw.Spec.QoSPolicy, QoSAdd); err != nil {
				klog.Errorf("failed to add qos for nat gw %s, %v", key, err)
				return err
			}
		}
		if err := c.updateCrdNatGwLabels(key, gw.Spec.QoSPolicy); err != nil {
			err := fmt.Errorf("failed to update nat gw %s: %w", gw.Name, err)
			klog.Error(err)
			return err
		}
		if err = c.patchNatGwQoSStatus(key, gw.Spec.QoSPolicy); err != nil {
			klog.Errorf("failed to patch nat gw qos status for nat gw %s, %v", key, err)
			return err
		}
	}

	if err = c.patchNatGwStatus(key); err != nil {
		klog.Errorf("failed to patch nat gw status for nat gw %s, %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) handleInitVpcNatGw(key string) error {
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle init vpc nat gateway %s", key)

	// subnet for vpc-nat-gw has been checked when create vpc-nat-gw

	pods, err := c.getNatGwPods(key, c.natGwNamespace(gw), true)
	if err != nil {
		err := fmt.Errorf("failed to get nat gw %s pods: %w", gw.Name, err)
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
			continue
		}
		last, _ := time.Parse("2006-01-02T15:04:05", natGwCreatedAT)
		if pod.CreationTimestamp.Unix() > last.Unix() {
			natGwCreatedAT = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
		}
		klog.V(3).Infof("nat gw pod '%s/%s' inited at %s", pod.Namespace, pod.Name, natGwCreatedAT)
		// During initialization, when KubeOVN is running on non primary cni mode, we need to ensure the NAT gateway interfaces
		// are properly configured. We extract the interfaces from the runtime Pod annotations (network-status).
		var interfaces []string
		if c.config.EnableNonPrimaryCNI {
			// extract external nad interface name
			externalNadNs, externalNadName, nadErr := c.getExternalSubnetNad(gw)
			if nadErr != nil {
				klog.Errorf("failed to get external subnet NAD for gateway %s: %v", gw.Name, nadErr)
				return nadErr
			}
			networkStatusAnnotations := pod.Annotations[nadv1.NetworkStatusAnnot]
			externalNadFullName := fmt.Sprintf("%s/%s", externalNadNs, externalNadName)
			externalNadIfName, err := util.GetNadInterfaceFromNetworkStatusAnnotation(networkStatusAnnotations, externalNadFullName)
			if err != nil {
				klog.Errorf("failed to extract external nad interface name from runtime Pod annotation network-status, %v", err)
				return err
			}
			// extract vpc nad interface name
			providers, err := c.getPodProviders(pod)
			if err != nil || len(providers) == 0 {
				klog.Errorf("failed to get providers for pod %s/%s: %v", pod.Namespace, pod.Name, err)
				return fmt.Errorf("failed to get providers for pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			// if more than one provider exists, use the first one
			provider := providers[0]
			providerParts := strings.Split(provider, ".")
			if len(providerParts) < 2 {
				klog.Errorf("failed to format provider %s for pod %s/%s", provider, pod.Namespace, pod.Name)
				return fmt.Errorf("failed to format provider %s parts for pod %s/%s", provider, pod.Namespace, pod.Name)
			}
			vpcNadName, vpcNadNamespace := providerParts[0], providerParts[1]
			vpcNadFullName := fmt.Sprintf("%s/%s", vpcNadNamespace, vpcNadName)
			vpcNadIfName, err := util.GetNadInterfaceFromNetworkStatusAnnotation(networkStatusAnnotations, vpcNadFullName)
			if err != nil {
				klog.Errorf("failed to extract internal nad interface name from runtime Pod annotation network-status, %v", err)
				return err
			}

			klog.Infof("nat gw pod %s/%s internal nad interface %s, external nad interface %s", pod.Namespace, pod.Name, vpcNadIfName, externalNadIfName)
			interfaces = []string{
				strings.Join([]string{vpcNadIfName, externalNadIfName}, ","),
			}
		}
		if err = c.execNatGwRules(pod, natGwInit, interfaces); err != nil {
			// Check if this is a transient initialization error (e.g., first attempt before iptables chains are created)
			// The init script may fail on first run but succeed on retry after chains are established
			klog.Warningf("vpc nat gateway %s pod %s/%s init attempt failed (will retry): %v", key, pod.Namespace, pod.Name, err)
			return fmt.Errorf("failed to init vpc nat gateway, %w", err)
		}
	}

	if gw.Spec.QoSPolicy != "" {
		if err = c.execNatGwQoS(gw, gw.Spec.QoSPolicy, QoSAdd); err != nil {
			klog.Errorf("failed to add qos for nat gw %s, %v", key, err)
			return err
		}
	}
	// if update qos success, will update nat gw status
	if gw.Spec.QoSPolicy != gw.Status.QoSPolicy {
		if err = c.patchNatGwQoSStatus(key, gw.Spec.QoSPolicy); err != nil {
			klog.Errorf("failed to patch status for nat gw %s, %v", key, err)
			return err
		}
	}

	if err := c.updateCrdNatGwLabels(gw.Name, gw.Spec.QoSPolicy); err != nil {
		err := fmt.Errorf("failed to update nat gw %s: %w", gw.Name, err)
		klog.Error(err)
		return err
	}

	c.updateVpcFloatingIPQueue.Add(key)
	c.updateVpcDnatQueue.Add(key)
	c.updateVpcSnatQueue.Add(key)
	c.updateVpcSubnetQueue.Add(key)
	c.updateVpcEipQueue.Add(key)

	for _, pod := range pods {
		if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
			continue
		}

		patch := util.KVPatch{util.VpcNatGatewayInitAnnotation: "true"}
		if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
			err := fmt.Errorf("failed to patch pod %s/%s: %w", pod.Namespace, pod.Name, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcFloatingIP(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc fip %s", natGwKey)

	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}

	fips, err := c.iptablesFipsLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err := fmt.Errorf("failed to get all fips, %w", err)
		klog.Error(err)
		return err
	}

	for _, fip := range fips {
		if fip.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo fip %s", fip.Name)
			if err = c.redoFip(fip.Name, natGwCreatedAT, false); err != nil {
				klog.Errorf("failed to update eip '%s' to re-apply, %v", fip.Spec.EIP, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcEip(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc eip %s", natGwKey)

	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}
	eips, err := c.iptablesEipsLister.List(labels.Everything())
	if err != nil {
		err = fmt.Errorf("failed to get eip list, %w", err)
		klog.Error(err)
		return err
	}
	for _, eip := range eips {
		if eip.Spec.NatGwDp == natGwKey && eip.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo eip %s", eip.Name)
			if err = c.patchEipStatus(eip.Name, "", natGwCreatedAT, "", false); err != nil {
				klog.Errorf("failed to update eip '%s' to re-apply, %v", eip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcSnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc snat %s", natGwKey)

	// refresh exist snats
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}
	snats, err := c.iptablesSnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err = fmt.Errorf("failed to get all snats, %w", err)
		klog.Error(err)
		return err
	}
	for _, snat := range snats {
		if snat.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo snat %s", snat.Name)
			if err = c.redoSnat(snat.Name, natGwCreatedAT, false); err != nil {
				err = fmt.Errorf("failed to update eip '%s' to re-apply, %w", snat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcDnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc dnat %s", natGwKey)

	// refresh exist dnats
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}

	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err = fmt.Errorf("failed to get all dnats, %w", err)
		klog.Error(err)
		return err
	}
	for _, dnat := range dnats {
		if dnat.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo dnat %s", dnat.Name)
			if err = c.redoDnat(dnat.Name, natGwCreatedAT, false); err != nil {
				err := fmt.Errorf("failed to update dnat '%s' to redo, %w", dnat.Name, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) getIptablesVersion(pod *corev1.Pod) (version string, err error) {
	operation := getIptablesVersion
	cmd := "bash /kube-ovn/nat-gateway.sh " + operation
	klog.V(3).Info(cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-nat-gw", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.V(3).Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return "", err
	}

	if len(stdOutput) > 0 {
		klog.V(3).Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return "", err
	}

	versionMatcher := regexp.MustCompile(`v([0-9]+(\.[0-9]+)+)`)
	match := versionMatcher.FindStringSubmatch(stdOutput)
	if match == nil {
		return "", fmt.Errorf("no iptables version found in string: %s", stdOutput)
	}
	return match[1], nil
}

func (c *Controller) handleUpdateNatGwSubnetRoute(natGwKey string) error {
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update subnet route for nat gateway %s", natGwKey)

	pods, err := c.getNatGwPods(natGwKey, c.natGwNamespace(gw), false)
	if err != nil {
		err = fmt.Errorf("failed to get nat gw '%s' pods, %w", natGwKey, err)
		klog.Error(err)
		return err
	}
	// Use the first pod to read annotations (they should be consistent across pods)
	pod := pods[0]

	v4InternalGw, _, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		err = fmt.Errorf("failed to get gw, err: %w", err)
		klog.Error(err)
		return err
	}
	vpc, err := c.vpcsLister.Get(gw.Spec.Vpc)
	if err != nil {
		err = fmt.Errorf("failed to get vpc, err: %w", err)
		klog.Error(err)
		return err
	}

	// update route table
	var newCIDRS, oldCIDRs, toBeDelCIDRs []string
	// Map of subnet provider to CIDRs, used to generate/update runtime Pod annotations
	newProviderCIDRMap := make(map[string][]string)

	if len(vpc.Status.Subnets) > 0 {
		for _, s := range vpc.Status.Subnets {
			subnet, err := c.subnetsLister.Get(s)
			if err != nil {
				err = fmt.Errorf("failed to get subnet, err: %w", err)
				klog.Error(err)
				return err
			}
			if subnet.Spec.Vlan != "" && !subnet.Spec.U2OInterconnection {
				continue
			}
			if !isOvnSubnet(subnet) || !subnet.Status.IsValidated() {
				continue
			}
			if v4Cidr, _ := util.SplitStringIP(subnet.Spec.CIDRBlock); v4Cidr != "" {
				newCIDRS = append(newCIDRS, v4Cidr)
				// Store the provider and CIDR for later use to update runtime Pod annotations
				newProviderCIDRMap[subnet.Spec.Provider] = append(newProviderCIDRMap[subnet.Spec.Provider], v4Cidr)
			}
		}
	}
	// Get all the CIDRs that are already in the runtime Pod annotations
	for annotation, value := range pod.Annotations {
		if strings.Contains(annotation, ".kubernetes.io/vpc_cidrs") {
			var existingCIDR []string
			if err = json.Unmarshal([]byte(value), &existingCIDR); err != nil {
				klog.Error(err)
				return err
			}
			// Defense in depth: validate CIDR format before using in shell commands
			for _, cidr := range existingCIDR {
				if err = util.CheckCidrs(cidr); err != nil {
					klog.Warningf("skipping invalid CIDR %q from annotation %q: %v", cidr, annotation, err)
					continue
				}
				oldCIDRs = append(oldCIDRs, cidr)
			}
		}
	}
	for _, old := range oldCIDRs {
		if !slices.Contains(newCIDRS, old) {
			toBeDelCIDRs = append(toBeDelCIDRs, old)
		}
	}

	if len(newCIDRS) > 0 {
		var rules []string
		for _, cidr := range newCIDRS {
			if !util.CIDRContainIP(cidr, v4InternalGw) {
				rules = append(rules, fmt.Sprintf("%s,%s", cidr, v4InternalGw))
			}
		}
		if len(rules) > 0 {
			for _, p := range pods {
				if err = c.execNatGwRules(p, natGwSubnetRouteAdd, rules); err != nil {
					err = fmt.Errorf("failed to exec nat gateway rule in pod %s/%s, err: %w", p.Namespace, p.Name, err)
					klog.Error(err)
					return err
				}
			}
		}
	}

	if len(toBeDelCIDRs) > 0 {
		for _, cidr := range toBeDelCIDRs {
			for _, p := range pods {
				if err = c.execNatGwRules(p, natGwSubnetRouteDel, []string{cidr}); err != nil {
					err = fmt.Errorf("failed to exec nat gateway rule in pod %s/%s, err: %w", p.Namespace, p.Name, err)
					klog.Error(err)
					return err
				}
			}
		}
	}

	// Generate runtime Pod annotations for vpc_cidrs (one per subnet provider)
	patch := util.KVPatch{}

	// Track existing vpc_cidrs runtime Pod annotations to identify stale ones
	existingProviders := make(map[string]bool)
	for annotation := range pod.Annotations {
		if strings.Contains(annotation, ".kubernetes.io/vpc_cidrs") {
			// Extract provider name from annotation key: <provider>.kubernetes.io/vpc_cidrs
			parts := strings.Split(annotation, ".kubernetes.io/vpc_cidrs")
			if len(parts) == 2 && parts[1] == "" {
				provider := parts[0]
				existingProviders[provider] = true
			}
		}
	}

	// Add/update runtime Pod annotations for current providers
	for provider, cidrs := range newProviderCIDRMap {
		cidrBytes, err := json.Marshal(cidrs)
		if err != nil {
			klog.Errorf("marshal vpc_cidrs annotation failed %v", err)
			return err
		}
		patch[fmt.Sprintf(util.VpcCIDRsAnnotationTemplate, provider)] = string(cidrBytes)
		// Mark this provider as still active
		delete(existingProviders, provider)
	}

	// Remove stale runtime Pod annotations for providers no longer associated with the VPC
	for provider := range existingProviders {
		patch[fmt.Sprintf(util.VpcCIDRsAnnotationTemplate, provider)] = nil
		klog.V(3).Infof("Removing stale vpc_cidrs runtime annotation for provider %s from pod %s/%s", provider, pod.Namespace, pod.Name)
	}

	// Only patch if there are changes to make
	if len(patch) > 0 {
		if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
			err = fmt.Errorf("failed to patch pod %s/%s: %w", pod.Namespace, pod.Name, err)
			klog.Error(err)
			return err
		}
		klog.V(3).Infof("Successfully patched %d vpc_cidrs annotations on pod %s/%s", len(patch), pod.Namespace, pod.Name)
	}

	return nil
}

// TODO: Refactor to avoid shell command injection vulnerability.
// Current implementation uses "bash -c" with string concatenation, which could be exploited
// if any element in the rules slice contains shell metacharacters.
// Recommended fix: Pass arguments directly as a slice instead of joining them into a shell command:
//
//	args := append([]string{"/kube-ovn/nat-gateway.sh", operation}, rules...)
//	util.ExecuteCommandInContainer(..., args...)
//
// This requires updating nat-gateway.sh to accept arguments via $@ instead of parsing a single string.
// Current risk is mitigated by CIDR format validation on all data sources reaching this function.
func (c *Controller) execNatGwRules(pod *corev1.Pod, operation string, rules []string) error {
	lockKey := fmt.Sprintf("nat-gw-exec:%s/%s", pod.Namespace, pod.Name)

	c.vpcNatGwExecKeyMutex.LockKey(lockKey)
	defer func() {
		_ = c.vpcNatGwExecKeyMutex.UnlockKey(lockKey)
	}()

	cmd := fmt.Sprintf("bash /kube-ovn/nat-gateway.sh %s %s", operation, strings.Join(rules, " "))
	klog.V(3).Infof("executing NAT gateway command: %s", cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-nat-gw", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("NAT gateway command failed - stderr: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.Infof("NAT gateway command failed - stdout: %v", stdOutput)
		}
		klog.Errorf("NAT gateway command execution error: %v", err)
		return err
	}

	if len(stdOutput) > 0 {
		klog.V(3).Infof("NAT gateway command succeeded - stdout: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		// tc commands may output warnings to stderr (e.g., "Warning: sch_htb: quantum of class is big")
		// Filter out lines that are only warnings, but preserve actual errors
		lines := strings.Split(errOutput, "\n")
		var errorLines []string
		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == "" {
				continue
			}
			// Skip lines that are just warnings
			if strings.HasPrefix(trimmedLine, "Warning:") {
				klog.Warningf("NAT gateway command warning: %v", trimmedLine)
				continue
			}
			errorLines = append(errorLines, trimmedLine)
		}
		// If there are actual error lines (not just warnings), return error
		if len(errorLines) > 0 {
			errMsg := strings.Join(errorLines, "; ")
			klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errMsg)
			return errors.New(errMsg)
		}
	}
	return nil
}

// setNatGwAPIAccess modifies StatefulSet Pod template annotations to add an interface with API access to the NAT gateway.
// It attaches the standard externalNetwork to the gateway via a NetworkAttachmentDefinition (NAD) with a provider
// corresponding to one that is configured on a subnet part of the default VPC (the K8S apiserver runs in the default VPC).
func (c *Controller) setNatGwAPIAccess(annotations map[string]string) error {
	// Check the NetworkAttachmentDefinition provider exists, must be user-configured
	if vpcNatAPINadProvider == "" {
		return errors.New("no NetworkAttachmentDefinition provided to access apiserver, check configmap ovn-vpc-nat-config and field 'apiNadProvider'")
	}

	// Subdivide provider so we can infer the name of the NetworkAttachmentDefinition
	providerSplit := strings.Split(vpcNatAPINadProvider, ".")
	if len(providerSplit) != 3 || providerSplit[2] != util.OvnProvider {
		return fmt.Errorf("name of the provider must have syntax 'name.namespace.ovn', got %s", vpcNatAPINadProvider)
	}

	// Extract the name of the provider and its namespace
	name, namespace := providerSplit[0], providerSplit[1]

	// Craft the name of the NAD for the externalNetwork and the apiNetwork
	networkAttachments := []string{fmt.Sprintf("%s/%s", namespace, name)}
	if externalNetworkAttachment, ok := annotations[nadv1.NetworkAttachmentAnnot]; ok {
		networkAttachments = append([]string{externalNetworkAttachment}, networkAttachments...)
	}

	// Attach the NADs to the Pod by adding them to the special annotation
	annotations[nadv1.NetworkAttachmentAnnot] = strings.Join(networkAttachments, ",")

	// Set the network route to the API, so we can reach it
	return c.setNatGwAPIRoute(annotations, namespace, name)
}

// setNatGwAPIRoute modifies StatefulSet Pod template annotations to add routes for reaching the K8S API server.
func (c *Controller) setNatGwAPIRoute(annotations map[string]string, nadNamespace, nadName string) error {
	dst := os.Getenv(util.EnvKubernetesServiceHost)

	protocol := util.CheckProtocol(dst)
	if !strings.ContainsRune(dst, '/') {
		switch protocol {
		case kubeovnv1.ProtocolIPv4:
			dst += "/32"
		case kubeovnv1.ProtocolIPv6:
			dst += "/128"
		}
	}

	// Retrieve every subnet on the cluster
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list subnets: %w", err)
	}

	// Retrieve the subnet connected to the NAD, this subnet should be in the VPC of the API
	apiSubnet, err := c.findSubnetByNetworkAttachmentDefinition(nadNamespace, nadName, subnets)
	if err != nil {
		return fmt.Errorf("failed to find api subnet using the nad %s/%s: %w", nadNamespace, nadName, err)
	}

	// Craft the route to reach the API from the subnet we've just retrieved
	for gw := range strings.SplitSeq(apiSubnet.Spec.Gateway, ",") {
		if util.CheckProtocol(gw) == protocol {
			routes := []request.Route{{Destination: dst, Gateway: gw}}
			buf, err := json.Marshal(routes)
			if err != nil {
				return fmt.Errorf("failed to marshal routes %+v: %w", routes, err)
			}

			annotations[fmt.Sprintf(util.RoutesAnnotationTemplate, vpcNatAPINadProvider)] = string(buf)
			break
		}
	}

	return nil
}

func (c *Controller) GetSubnetProvider(subnetName string) (string, error) {
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		return "", fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
	}

	// Make sure the subnet is an OVN subnet
	if !isOvnSubnet(subnet) {
		return "", fmt.Errorf("subnet %s is not an OVN subnet", subnetName)
	}
	return subnet.Spec.Provider, nil
}

// generateNatGwRoutes generates route annotations for NAT gateway pods using util.NewPodRoutes().
// This ensures routes are added to reach the VPC BFD port for BFD session establishment,
// along with routes to service CIDRs, VPC subnets, and custom user routes.
func (c *Controller) generateNatGwRoutes(
	gw *kubeovnv1.VpcNatGateway,
	bfdIP string,
	eth0SubnetProvider string,
	eth0V4Gateway, eth0V6Gateway string,
	net1SubnetProvider string,
	net1V4Gateway, net1V6Gateway string,
) (map[string]string, error) {
	routes := util.NewPodRoutes()

	// Add routes for the VPC BFD Port so the gateway can establish BFD sessions with it
	if bfdIP != "" {
		bfdIPv4, bfdIPv6 := util.SplitStringIP(bfdIP)
		routes.Add(eth0SubnetProvider, bfdIPv4, eth0V4Gateway)
		routes.Add(eth0SubnetProvider, bfdIPv6, eth0V6Gateway)
	}

	// Add routes to reach service cluster IP range
	v4ClusterIPRange, v6ClusterIPRange := util.SplitStringIP(c.config.ServiceClusterIPRange)
	routes.Add(eth0SubnetProvider, v4ClusterIPRange, eth0V4Gateway)
	routes.Add(eth0SubnetProvider, v6ClusterIPRange, eth0V6Gateway)

	// Add routes to reach all subnets in the same VPC (might be unnecessary now as they're loaded dynamically)
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return nil, err
	}

	for _, subnet := range subnets {
		if subnet.Spec.Vpc != gw.Spec.Vpc || subnet.Name == gw.Spec.Subnet ||
			!isOvnSubnet(subnet) || !subnet.Status.IsValidated() ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.U2OInterconnection) {
			continue
		}
		cidrV4, cidrV6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
		routes.Add(eth0SubnetProvider, cidrV4, eth0V4Gateway)
		routes.Add(eth0SubnetProvider, cidrV6, eth0V6Gateway)
	}

	// Add custom user-specified routes
	for _, route := range gw.Spec.Routes {
		nexthop := route.NextHopIP
		if nexthop == "gateway" {
			if util.CheckProtocol(route.CIDR) == kubeovnv1.ProtocolIPv4 {
				nexthop = eth0V4Gateway
			} else {
				nexthop = eth0V6Gateway
			}
		}
		routes.Add(eth0SubnetProvider, route.CIDR, nexthop)
	}

	// Add default routes to the external network
	routes.Add(net1SubnetProvider, "0.0.0.0/0", net1V4Gateway)
	routes.Add(net1SubnetProvider, "::/0", net1V6Gateway)

	// Convert routes to annotations
	annotations, err := routes.ToAnnotations()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return annotations, nil
}

func (c *Controller) genNatGwStatefulSet(gw *kubeovnv1.VpcNatGateway, oldSts *v1.StatefulSet, natGwPodContainerRestartCount int32) (*v1.StatefulSet, error) {
	externalNadNamespace, externalNadName, err := c.getExternalSubnetNad(gw)
	if err != nil {
		klog.Errorf("failed to get gw external subnet nad: %v", err)
		return nil, err
	}

	eth0SubnetProvider, err := c.GetSubnetProvider(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get gw eth0 valid subnet provider: %v", err)
		return nil, err
	}

	// Get additional networks specified by user in VpcNatGateway CR metadata.annotations (for secondary CNI mode)
	// TODO: the EnableNonPrimaryCNI check may not be necessary, as additional NADs could also
	// be useful in primary CNI mode. Consider removing this condition in the future.
	var additionalNetworks string
	if c.config.EnableNonPrimaryCNI && gw.Annotations != nil {
		additionalNetworks = gw.Annotations[nadv1.NetworkAttachmentAnnot]
	}

	// Generate StatefulSet Pod template annotations.
	// User-defined annotations (gw.Spec.Annotations) are used as base, system annotations are set on top.
	templateAnnotations, err := util.GenNatGwPodAnnotations(gw.Spec.Annotations, gw, externalNadNamespace, externalNadName, eth0SubnetProvider, additionalNetworks, c.config.EnableNonPrimaryCNI)
	if err != nil {
		klog.Errorf("vpc nat gateway annotation generation failed: %s", err.Error())
		return nil, err
	}

	// Restart logic to fix #5072
	if oldSts != nil && len(oldSts.Spec.Template.Annotations) != 0 {
		if _, ok := oldSts.Spec.Template.Annotations[util.VpcNatGatewayContainerRestartAnnotation]; !ok && natGwPodContainerRestartCount > 0 {
			templateAnnotations[util.VpcNatGatewayContainerRestartAnnotation] = ""
		}
	}
	klog.V(3).Infof("%s templateAnnotations:%v", gw.Name, templateAnnotations)

	// Add an interface that can reach the API server, we need access to it to probe Kube-OVN resources
	if gw.Spec.BgpSpeaker.Enabled {
		if err := c.setNatGwAPIAccess(templateAnnotations); err != nil {
			klog.Errorf("couldn't add an API interface to the NAT gateway: %v", err)
			return nil, err
		}
	}

	// Retrieve the gateways of the subnet sitting behind the NAT gateway
	eth0V4Gateway, eth0V6Gateway, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get gateway ips for subnet %s: %v", gw.Spec.Subnet, err)
		return nil, err
	}

	// Get the external subnet for default routes
	net1Subnet, err := c.subnetsLister.Get(util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets))
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	net1V4Gateway, net1V6Gateway := util.SplitStringIP(net1Subnet.Spec.Gateway)

	// Generate route annotations using the new helper function
	routeAnnotations, err := c.generateNatGwRoutes(
		gw,
		"",
		eth0SubnetProvider,
		eth0V4Gateway, eth0V6Gateway,
		net1Subnet.Spec.Provider,
		net1V4Gateway, net1V6Gateway,
	)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	// Merge route annotations into template annotations
	maps.Copy(templateAnnotations, routeAnnotations)

	// Handle NoDefaultEIP case
	if gw.Spec.NoDefaultEIP {
		klog.Infof("skipping IP allocation for NAT gateway %s (NoDefaultEIP enabled)", gw.Name)
		templateAnnotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, net1Subnet.Spec.Provider)] = "true"
		// Remove default route annotations for external network when NoDefaultEIP is set
		delete(templateAnnotations, fmt.Sprintf(util.RoutesAnnotationTemplate, net1Subnet.Spec.Provider))
	}

	selectors := util.GenNatGwSelectors(gw.Spec.Selector)
	klog.V(3).Infof("prepare for vpc nat gateway pod, node selector: %v", selectors)

	labels := util.GenNatGwLabels(gw.Name)

	sts := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenNatGwName(gw.Name),
			Namespace: c.natGwNamespace(gw),
			Labels:    labels,
		},
		Spec: v1.StatefulSetSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: templateAnnotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					Containers: []corev1.Container{
						{
							Name:    "vpc-nat-gw",
							Image:   vpcNatImage,
							Command: []string{"sleep", "infinity"},
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", "sysctl -w net.ipv4.ip_forward=1"},
									},
								},
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name:  "GATEWAY_V4",
									Value: net1V4Gateway,
								},
								{
									Name:  "GATEWAY_V6",
									Value: net1V6Gateway,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged:               new(true),
								AllowPrivilegeEscalation: new(true),
							},
						},
					},
					NodeSelector: selectors,
					Tolerations:  gw.Spec.Tolerations,
					Affinity:     &gw.Spec.Affinity,
				},
			},
			UpdateStrategy: v1.StatefulSetUpdateStrategy{
				Type: v1.RollingUpdateStatefulSetStrategyType,
			},
		},
	}

	// BGP speaker is enabled on this instance, add a BGP speaker to the statefulset
	if gw.Spec.BgpSpeaker.Enabled {
		// We need to connect to the K8S API to make the BGP speaker work, this implies a ServiceAccount
		sts.Spec.Template.Spec.ServiceAccountName = "vpc-nat-gw"
		sts.Spec.Template.Spec.AutomountServiceAccountToken = new(true)

		// Craft a BGP speaker container to add to our statefulset
		bgpSpeakerContainer, err := util.GenNatGwBgpSpeakerContainer(gw.Spec.BgpSpeaker, vpcNatGwBgpSpeakerImage, gw.Name)
		if err != nil {
			klog.Errorf("failed to create a BGP speaker container for gateway %s: %v", gw.Name, err)
			return nil, err
		}

		// Add our container to the list of containers in the statefulset
		sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, *bgpSpeakerContainer)
	}

	// Set owner reference so that the workload will be deleted automatically when the VPC NAT gateway is deleted
	if err := util.SetOwnerReference(gw, sts); err != nil {
		return nil, err
	}

	return sts, nil
}

// genNatGwDeployment generates a Deployment for HA mode NAT gateway (replicas > 1).
// It reuses most of the logic from genNatGwStatefulSet but creates a Deployment instead.
func (c *Controller) genNatGwDeployment(gw *kubeovnv1.VpcNatGateway) (*v1.Deployment, error) {
	// Reuse all the annotation and routing logic from StatefulSet generation
	externalNadNamespace, externalNadName, err := c.getExternalSubnetNad(gw)
	if err != nil {
		klog.Errorf("failed to get gw external subnet nad: %v", err)
		return nil, err
	}

	eth0SubnetProvider, err := c.GetSubnetProvider(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get gw eth0 valid subnet provider: %v", err)
		return nil, err
	}

	var additionalNetworks string
	if c.config.EnableNonPrimaryCNI && gw.Annotations != nil {
		additionalNetworks = gw.Annotations[nadv1.NetworkAttachmentAnnot]
	}

	templateAnnotations, err := util.GenNatGwPodAnnotations(gw.Spec.Annotations, gw, externalNadNamespace, externalNadName, eth0SubnetProvider, additionalNetworks, c.config.EnableNonPrimaryCNI)
	if err != nil {
		klog.Errorf("vpc nat gateway annotation generation failed: %s", err.Error())
		return nil, err
	}

	// Get VPC and BFD port information for BFD session support
	vpc, err := c.vpcsLister.Get(gw.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get vpc %s: %v", gw.Spec.Vpc, err)
		return nil, err
	}

	var bfdIP string
	if gw.Spec.BFD.Enabled {
		bfdIP = vpc.Status.BFDPort.IP
	}

	if gw.Spec.BgpSpeaker.Enabled {
		if err := c.setNatGwAPIAccess(templateAnnotations); err != nil {
			klog.Errorf("couldn't add an API interface to the NAT gateway: %v", err)
			return nil, err
		}
	}

	eth0V4Gateway, eth0V6Gateway, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get gateway ips for subnet %s: %v", gw.Spec.Subnet, err)
		return nil, err
	}

	// Get the external subnet for default routes
	net1Subnet, err := c.subnetsLister.Get(util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets))
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	net1V4Gateway, net1V6Gateway := util.SplitStringIP(net1Subnet.Spec.Gateway)

	routeAnnotations, err := c.generateNatGwRoutes(
		gw,
		bfdIP,
		eth0SubnetProvider,
		eth0V4Gateway, eth0V6Gateway,
		net1Subnet.Spec.Provider,
		net1V4Gateway, net1V6Gateway,
	)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	// Merge route annotations into template annotations
	maps.Copy(templateAnnotations, routeAnnotations)

	// Handle NoDefaultEIP case
	if gw.Spec.NoDefaultEIP {
		klog.Infof("skipping IP allocation for NAT gateway %s (NoDefaultEIP enabled)", gw.Name)
		templateAnnotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, net1Subnet.Spec.Provider)] = "true"
		// Remove default route annotations for external network when NoDefaultEIP is set
		delete(templateAnnotations, fmt.Sprintf(util.RoutesAnnotationTemplate, net1Subnet.Spec.Provider))
	}

	selectors := util.GenNatGwSelectors(gw.Spec.Selector)
	klog.V(3).Infof("prepare for vpc nat gateway pod, node selector: %v", selectors)

	labels := util.GenNatGwLabels(gw.Name)
	replicas := getNatGwReplicas(gw)

	// Create Deployment with HA configuration
	deploy := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenNatGwName(gw.Name),
			Namespace: c.natGwNamespace(gw),
			Labels:    labels,
		},
		Spec: v1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Strategy: genGatewayDeploymentStrategy(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: templateAnnotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					Containers: []corev1.Container{
						{
							Name:    "vpc-nat-gw",
							Image:   vpcNatImage,
							Command: []string{"sleep", "infinity"},
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", "sysctl -w net.ipv4.ip_forward=1"},
									},
								},
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name:  "GATEWAY_V4",
									Value: net1V4Gateway,
								},
								{
									Name:  "GATEWAY_V6",
									Value: net1V6Gateway,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged:               new(true),
								AllowPrivilegeEscalation: new(true),
							},
						},
					},
					Volumes: []corev1.Volume{{
						Name: "usr-local-sbin",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}},
					NodeSelector: selectors,
					Tolerations:  gw.Spec.Tolerations,
					// Merge pod anti-affinity for HA with user-specified affinity
					Affinity: mergeGatewayAffinity(
						genGatewayPodAntiAffinity(labels),
						&gw.Spec.Affinity,
					),
				},
			},
		},
	}

	// Set owner reference so that the workload will be deleted automatically when the VPC NAT gateway is deleted
	if err = util.SetOwnerReference(gw, deploy); err != nil {
		return nil, err
	}

	// Run BFD in the gateway container to establish BFD session(s) with the VPC BFD LRP
	// Use the main kube-ovn image which contains bfdd binaries (vpc-nat-gateway image doesn't have them)
	if gw.Spec.BFD.Enabled {
		bfdContainer := genGatewayBFDDContainer(c.config.Image, bfdIP, gw.Spec.BFD.MinTX, gw.Spec.BFD.MinRX, gw.Spec.BFD.Multiplier)
		deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, bfdContainer)
	}

	// BGP speaker is enabled on this instance
	if gw.Spec.BgpSpeaker.Enabled {
		deploy.Spec.Template.Spec.ServiceAccountName = "vpc-nat-gw"
		deploy.Spec.Template.Spec.AutomountServiceAccountToken = new(true)

		bgpSpeakerContainer, err := util.GenNatGwBgpSpeakerContainer(gw.Spec.BgpSpeaker, vpcNatGwBgpSpeakerImage, gw.Name)
		if err != nil {
			klog.Errorf("failed to create a BGP speaker container for gateway %s: %v", gw.Name, err)
			return nil, err
		}

		deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, *bgpSpeakerContainer)
	}

	// Generate hash for the workload to determine whether to update the existing workload or not
	// Only take the specs to prevent reloading the deployments for annotations/labels/status updates
	hash, err := util.Sha256HashObject(deploy.Spec)
	if err != nil {
		klog.Errorf("failed to hash generated deployment %s/%s: %v", deploy.Namespace, deploy.Name, err)
		return nil, err
	}

	if deploy.Annotations == nil {
		deploy.Annotations = make(map[string]string)
	}
	deploy.Annotations[util.GenerateHashAnnotation] = hash[:12]

	return deploy, nil
}

// getExternalSubnetNad returns the namespace and name of the NetworkAttachmentDefinition associated with
// an external network attached to a NAT gateway
func (c *Controller) getExternalSubnetNad(gw *kubeovnv1.VpcNatGateway) (string, string, error) {
	externalNadNamespace := c.config.PodNamespace
	// GetNatGwExternalNetwork returns the subnet name from ExternalSubnets, or "ovn-vpc-external-network" if empty
	externalSubnetName := util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets)

	externalSubnet, err := c.subnetsLister.Get(externalSubnetName)
	if err != nil {
		err = fmt.Errorf("failed to get external subnet %s for NAT gateway %s: %w", externalSubnetName, gw.Name, err)
		klog.Error(err)
		return "", "", err
	}

	// Try to parse NAD info from subnet's provider
	if name, namespace, ok := util.GetNadBySubnetProvider(externalSubnet.Spec.Provider); ok {
		return namespace, name, nil
	}

	// Provider cannot be parsed to NAD info (e.g., provider is "ovn" or empty)
	// Fall back to default NAD name which is the same as subnet name for external subnets
	klog.Warningf("subnet %s provider %q cannot be parsed to NAD info, using default NAD %s/%s",
		externalSubnetName, externalSubnet.Spec.Provider, externalNadNamespace, externalSubnetName)
	return externalNadNamespace, externalSubnetName, nil
}

func (c *Controller) cleanUpVpcNatGw() error {
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vpc nat gateway, %v", err)
		return err
	}
	for _, gw := range gws {
		natGwNs := gw.Spec.Namespace
		if natGwNs == "" {
			natGwNs = c.config.PodNamespace
		}
		c.delVpcNatGatewayQueue.Add(natGwNs + "/" + gw.Name)
	}
	return nil
}

func (c *Controller) getNatGwPod(name, namespace string) (*corev1.Pod, error) {
	pods, err := c.getNatGwPods(name, namespace, false)
	if err != nil {
		return nil, err
	}
	return pods[0], nil
}

func (c *Controller) getNatGwPods(name, namespace string, allPods bool) ([]*corev1.Pod, error) {
	selector := labels.Set{"app": util.GenNatGwName(name), util.VpcNatGatewayLabel: "true"}.AsSelector()
	pods, err := c.podsLister.Pods(namespace).List(selector)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	activePods := make([]*corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if allPods || (pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil) {
			activePods = append(activePods, pod)
		}
	}

	if len(activePods) == 0 {
		time.Sleep(5 * time.Second)
		return nil, errors.New("no active pod now")
	}

	return activePods, nil
}

func (c *Controller) initCreateAt(key string) (err error) {
	if natGwCreatedAT != "" {
		return nil
	}
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		klog.Error(err)
		return err
	}
	pods, err := c.getNatGwPods(key, c.natGwNamespace(gw), false)
	if err != nil {
		klog.Error(err)
		return err
	}

	natGwCreatedAT = pods[0].CreationTimestamp.Format("2006-01-02T15:04:05")
	for _, pod := range pods {
		last, _ := time.Parse("2006-01-02T15:04:05", natGwCreatedAT)
		if pod.CreationTimestamp.Unix() > last.Unix() {
			natGwCreatedAT = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
		}
	}

	return nil
}

func (c *Controller) updateCrdNatGwLabels(key, qos string) error {
	oriGw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		errMsg := fmt.Errorf("failed to get vpc nat gw '%s', %w", key, err)
		klog.Error(errMsg)
		return errMsg
	}
	var needUpdateLabel bool
	var op string

	// Create a new labels map to avoid modifying the informer cache
	labels := make(map[string]string, len(oriGw.Labels)+3)
	maps.Copy(labels, oriGw.Labels)

	// vpc nat gw label may lost
	if len(oriGw.Labels) == 0 {
		op = "add"
		labels[util.SubnetNameLabel] = oriGw.Spec.Subnet
		labels[util.VpcNameLabel] = oriGw.Spec.Vpc
		labels[util.QoSLabel] = qos
		needUpdateLabel = true
	} else {
		if oriGw.Labels[util.SubnetNameLabel] != oriGw.Spec.Subnet {
			op = "replace"
			labels[util.SubnetNameLabel] = oriGw.Spec.Subnet
			needUpdateLabel = true
		}
		if oriGw.Labels[util.VpcNameLabel] != oriGw.Spec.Vpc {
			op = "replace"
			labels[util.VpcNameLabel] = oriGw.Spec.Vpc
			needUpdateLabel = true
		}
		if oriGw.Labels[util.QoSLabel] != qos {
			op = "replace"
			labels[util.QoSLabel] = qos
			needUpdateLabel = true
		}
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), oriGw.Name, types.JSONPatchType,
			[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch vpc nat gw %s: %v", oriGw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchNatGwQoSStatus(key, qos string) error {
	// add qos label to vpc nat gw
	var changed bool
	oriGw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc nat gw %s, %v", key, err)
		return err
	}
	gw := oriGw.DeepCopy()

	// update status.qosPolicy
	if gw.Status.QoSPolicy != qos {
		gw.Status.QoSPolicy = qos
		changed = true
	}

	if changed {
		bytes, err := gw.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal vpc nat gw %s status, %v", gw.Name, err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch gw %s, %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchNatGwStatus(key string) error {
	var changed bool
	oriGw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc nat gw %s, %v", key, err)
		return err
	}
	gw := oriGw.DeepCopy()

	if !slices.Equal(gw.Spec.ExternalSubnets, gw.Status.ExternalSubnets) {
		gw.Status.ExternalSubnets = gw.Spec.ExternalSubnets
		changed = true
	}
	if !slices.Equal(gw.Spec.Selector, gw.Status.Selector) {
		gw.Status.Selector = gw.Spec.Selector
		changed = true
	}
	if !reflect.DeepEqual(gw.Spec.Tolerations, gw.Status.Tolerations) {
		gw.Status.Tolerations = gw.Spec.Tolerations
		changed = true
	}
	if !reflect.DeepEqual(gw.Spec.Affinity, gw.Status.Affinity) {
		gw.Status.Affinity = gw.Spec.Affinity
		changed = true
	}
	if !slices.Equal(gw.Spec.InternalSubnets, gw.Status.InternalSubnets) {
		gw.Status.InternalSubnets = gw.Spec.InternalSubnets
		changed = true
	}
	if !slices.Equal(gw.Spec.InternalCIDRs, gw.Status.InternalCIDRs) {
		gw.Status.InternalCIDRs = gw.Spec.InternalCIDRs
		changed = true
	}
	if gw.Status.Replicas != gw.Spec.Replicas {
		gw.Status.Replicas = gw.Spec.Replicas
		changed = true
	}
	var lanIPs []string
	if !util.IsNatGwHAMode(gw) {
		lanIPs = []string{gw.Spec.LanIP}
	} else {
		pods, err := c.getNatGwPods(gw.Name, c.natGwNamespace(gw), false)
		if err != nil {
			klog.Errorf("failed to get nat gw pods, %v", err)
			return err
		}
		subnet, err := c.subnetsLister.Get(gw.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get subnet %s, %v", gw.Spec.Subnet, err)
			return err
		}
		for _, pod := range pods {
			podIP := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, subnet.Spec.Provider)]
			if podIP != "" {
				lanIPs = append(lanIPs, podIP)
			}
		}
		slices.Sort(lanIPs)
	}
	lanIP := strings.Join(lanIPs, ",")
	if gw.Status.LanIP != lanIP {
		gw.Status.LanIP = lanIP
		changed = true
	}

	if updateNatGwWorkloadStatus(gw, c.podsLister, c.deploymentsLister, c.config.KubeClient, c.natGwNamespace(gw)) {
		changed = true
	}

	if changed {
		bytes, err := gw.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch gw %s, %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) execNatGwQoS(gw *kubeovnv1.VpcNatGateway, qos, operation string) error {
	qosPolicy, err := c.qosPoliciesLister.Get(qos)
	if err != nil {
		klog.Errorf("get qos policy %s failed: %v", qos, err)
		return err
	}
	if !qosPolicy.Status.Shared {
		err := fmt.Errorf("not support unshared qos policy %s to related to gw", qos)
		klog.Error(err)
		return err
	}
	if qosPolicy.Status.BindingType != kubeovnv1.QoSBindingTypeNatGw {
		err := fmt.Errorf("not support qos policy %s binding type %s to related to gw", qos, qosPolicy.Status.BindingType)
		klog.Error(err)
		return err
	}
	return c.execNatGwBandwidthLimitRules(gw, qosPolicy.Status.BandwidthLimitRules, operation)
}

func (c *Controller) execNatGwBandwidthLimitRules(gw *kubeovnv1.VpcNatGateway, rules kubeovnv1.QoSPolicyBandwidthLimitRules, operation string) error {
	var err error
	for _, rule := range rules {
		if err = c.execNatGwQoSInPod(gw, &rule, operation); err != nil {
			klog.Errorf("failed to %s %s gw '%s' qos in pod, %v", operation, rule.Direction, gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) execNatGwQoSInPod(
	gw *kubeovnv1.VpcNatGateway, r *kubeovnv1.QoSPolicyBandwidthLimitRule, operation string,
) error {
	gwPods, err := c.getNatGwPods(gw.Name, c.natGwNamespace(gw), false)
	if err != nil {
		klog.Errorf("failed to get nat gw pods, %v", err)
		return err
	}
	var addRules []string
	var classifierType, matchDirection, cidr string
	switch r.MatchType {
	case "ip":
		classifierType = "u32"
		// matchValue: dst xxx.xxx.xxx.xxx/32
		splitStr := strings.Split(r.MatchValue, " ")
		if len(splitStr) != 2 {
			err := fmt.Errorf("matchValue %s format error", r.MatchValue)
			klog.Error(err)
			return err
		}
		matchDirection = splitStr[0]
		cidr = splitStr[1]
	case "":
		classifierType = "matchall"
	default:
		err := fmt.Errorf("MatchType %s format error", r.MatchType)
		klog.Error(err)
		return err
	}
	rule := fmt.Sprintf("%s,%s,%d,%s,%s,%s,%s,%s,%s",
		r.Direction, r.Interface, r.Priority,
		classifierType, r.MatchType, matchDirection,
		cidr, r.RateMax, r.BurstMax)
	addRules = append(addRules, rule)

	for _, gwPod := range gwPods {
		if err = c.execNatGwRules(gwPod, operation, addRules); err != nil {
			err = fmt.Errorf("failed to exec nat gateway rule in pod %s/%s, err: %w", gwPod.Namespace, gwPod.Name, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) initVpcNatGw() error {
	klog.Infof("init all vpc nat gateways")
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		err = fmt.Errorf("failed to get vpc nat gw list, %w", err)
		klog.Error(err)
		return err
	}
	if len(gws) == 0 {
		return nil
	}

	if vpcNatEnabled != "true" {
		err := errors.New("iptables nat gw not enable")
		klog.Warning(err)
		return nil
	}

	for _, gw := range gws {
		pods, err := c.getNatGwPods(gw.Name, c.natGwNamespace(gw), false)
		if err != nil {
			// the nat gw maybe deleted
			err := fmt.Errorf("failed to get nat gw %s pods: %w", gw.Name, err)
			klog.Error(err)
			continue
		}

		for _, pod := range pods {
			if isNatGateway, natGateway := c.checkIsPodVpcNatGw(pod); isNatGateway {
				if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
					continue
				}
				c.initVpcNatGatewayQueue.Add(natGateway)
			}
		}
	}
	return nil
}

// reconcileVpcNatGatewayOVNRoutes reconciles OVN routing policies for VPC NAT Gateways.
// It creates policy-based routes for traffic from specified internal CIDRs,
// directing them to NAT gateway instances (with BFD-based automatic failover if enabled).
// Stale routes (and stale BFD entries) are removed during the reconciliation process.
func (c *Controller) reconcileVpcNatGatewayOVNRoutes(gw *kubeovnv1.VpcNatGateway) error {
	// Resolve internal CIDRs from subnet names and direct CIDRs. We'll inject routes to redirect traffic coming from those CIDRs into the VPC NAT gateway.
	internalCIDRs := resolveInternalCIDRs(c.subnetsLister, gw.Spec.InternalSubnets, gw.Spec.InternalCIDRs)

	// Retrieve the VPC in which the gateway is running
	vpc, err := c.vpcsLister.Get(gw.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get vpc %s: %v", gw.Spec.Vpc, err)
		return err
	}

	// Collect the BFD IP(s) if it's enabled on the NAT gateway
	var bfdIP string
	if gw.Spec.BFD.Enabled {
		bfdIP = vpc.Status.BFDPort.IP
	}

	// Split the BFD IP into IPv4 and IPv6 components
	bfdIPv4, bfdIPv6 := util.SplitStringIP(bfdIP)
	bfdIPs := map[int]string{4: bfdIPv4, 6: bfdIPv6}

	// Collect nexthop IPs from running gateway pods
	nextHops, err := c.getNatGwNextHops(gw)
	if err != nil {
		klog.Errorf("failed to collect next hops for nat gw %s: %v", gw.Name, err)
		return err
	}

	// Group internal CIDRs and nexthops by address family
	cidrsByAF, nextHopsByAF := util.GroupInternalCIDRsAndNextHops(internalCIDRs, nextHops)

	// Process each address family
	for _, af := range []int{4, 6} {
		if err := c.reconcileVpcNatGatewayOVNRoutesAF(gw, af, vpc.Status.BFDPort.Name, bfdIPs[af], cidrsByAF[af], nextHopsByAF[af]); err != nil {
			return err
		}
	}

	return nil
}

// reconcileVpcNatGatewayOVNRoutesAF reconciles OVN routing policies and BFD sessions for a specific address family.
func (c *Controller) reconcileVpcNatGatewayOVNRoutesAF(gw *kubeovnv1.VpcNatGateway, af int, bfdLrp, bfdIP string, internalCIDRsAF []string, nextHopsAF map[string]string) error {
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:        util.CniTypeName,
		ovs.ExternalIDVpcNatGateway: gw.Name,
		"af":                        strconv.Itoa(af),
	}

	// Reconcile BFD sessions for this address family
	bfdIDs, err := reconcileGatewayBFDWithCleanup(
		c.OVNNbClient,
		bfdIP,
		bfdLrp,
		nextHopsAF,
		gw.Spec.BFD.MinTX,
		gw.Spec.BFD.MinRX,
		gw.Spec.BFD.Multiplier,
		externalIDs,
	)
	if err != nil {
		klog.Errorf("failed to reconcile BFD for nat gw %s af %d: %v", gw.Name, af, err)
		return err
	}

	// Reconcile the OVN policy routes for this address family
	if err := reconcileNatGatewayPolicies(
		c.OVNNbClient,
		gw.Name,
		gw.Spec.Vpc,
		af,
		gw.Spec.BFD.Enabled,
		bfdIDs,
		internalCIDRsAF,
		nextHopsAF,
		externalIDs,
	); err != nil {
		klog.Errorf("failed to reconcile policies for nat gw %s af %d: %v", gw.Name, af, err)
		return err
	}

	return nil
}

// getNatGwNextHops collects the IP addresses of the running NAT gateway pods to be used as next hops in OVN routes.
func (c *Controller) getNatGwNextHops(gw *kubeovnv1.VpcNatGateway) (map[string]string, error) {
	// The gateway is getting deleted, do not return any next hop as we don't want to send the
	// traffic to the gateway pods anymore.
	if !gw.DeletionTimestamp.IsZero() {
		return nil, nil
	}

	deploy, err := c.config.KubeClient.AppsV1().Deployments(c.natGwNamespace(gw)).
		Get(context.Background(), util.GenNatGwName(gw.Name), metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get deployment %s: %v", util.GenNatGwName(gw.Name), err)
		return nil, err
	}

	podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		klog.Errorf("failed to get pod selector: %v", err)
		return nil, err
	}

	pods, err := c.podsLister.Pods(c.natGwNamespace(gw)).List(podSelector)
	if err != nil {
		klog.Errorf("failed to list pods: %v", err)
		return nil, err
	}

	nextHops := make(map[string]string)
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		ready := true
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}

		if len(pod.Status.PodIPs) == 0 || pod.Spec.NodeName == "" {
			continue
		}
		for _, podIP := range pod.Status.PodIPs {
			// For dual-stack, prefer v4 or concatenate both
			if _, exists := nextHops[pod.Spec.NodeName]; !exists {
				nextHops[pod.Spec.NodeName] = podIP.IP
			} else if util.CheckProtocol(podIP.IP) == kubeovnv1.ProtocolIPv4 {
				nextHops[pod.Spec.NodeName] = podIP.IP
			}
		}
	}
	return nextHops, nil
}

// handleAddVpcNatGwFinalizer adds a finalizer to the VpcNatGateway resource to ensure proper cleanup before deletion.
func (c *Controller) handleAddVpcNatGwFinalizer(gw *kubeovnv1.VpcNatGateway) error {
	if !gw.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(gw, util.KubeOVNControllerFinalizer) {
		return nil
	}

	newGw := gw.DeepCopy()
	controllerutil.AddFinalizer(newGw, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(gw, newGw)
	if err != nil {
		klog.Errorf("failed to generate patch payload for vpc nat gateway '%s', %v", gw.Name, err)
		return err
	}

	if _, err := c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for vpc nat gateway '%s', %v", gw.Name, err)
		return err
	}
	return nil
}

// handleDeleteVpcNatGwFinalizer performs cleanup for the VpcNatGateway and removes the finalizer.
func (c *Controller) handleDeleteVpcNatGwFinalizer(gw *kubeovnv1.VpcNatGateway) error {
	if !controllerutil.ContainsFinalizer(gw, util.KubeOVNControllerFinalizer) {
		return nil
	}

	newGw := gw.DeepCopy()
	controllerutil.RemoveFinalizer(newGw, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(gw, newGw)
	if err != nil {
		klog.Errorf("failed to generate patch payload for vpc nat gateway '%s', %v", gw.Name, err)
		return err
	}

	if _, err := c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from vpc nat gateway '%s', %v", gw.Name, err)
		return err
	}
	return nil
}
