package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	vegBFDInitCommand    = "chmod +t /usr/local/sbin && chmod 1777 /var/run/kube-ovn/bfdd-supervisor && bash /kube-ovn/init-vpc-egress-gateway.sh"
	vegBFDDStateDir      = "/var/run/kube-ovn/bfdd-supervisor"
	vegBFDDStateVolume   = "bfdd-supervisor-state"
	vegBFDDSupervisorBin = "/kube-ovn/kube-ovn-bfdd-supervisor"
	vegBFDDMetricsPort   = "bfdd-metrics"
)

var (
	vegBFDDSupervisorLimitCPU    = resource.MustParse("300m")
	vegBFDDSupervisorLimitMemory = resource.MustParse("64Mi")
)

func (c *Controller) enqueueAddVpcEgressGateway(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcEgressGateway)).String()
	klog.V(3).Infof("enqueue add vpc-egress-gateway %s", key)
	c.addOrUpdateVpcEgressGatewayQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcEgressGateway(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.VpcEgressGateway)).String()
	klog.V(3).Infof("enqueue update vpc-egress-gateway %s", key)
	c.addOrUpdateVpcEgressGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcEgressGateway(obj any) {
	var gw *kubeovnv1.VpcEgressGateway
	switch t := obj.(type) {
	case *kubeovnv1.VpcEgressGateway:
		gw = t
	case cache.DeletedFinalStateUnknown:
		g, ok := t.Obj.(*kubeovnv1.VpcEgressGateway)
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
	klog.V(3).Infof("enqueue delete vpc-egress-gateway %s", key)
	c.delVpcEgressGatewayQueue.Add(key)
}

func vegWorkloadLabels(vegName string) map[string]string {
	return map[string]string{
		"app":                      "vpc-egress-gateway",
		util.VpcEgressGatewayLabel: util.NormalizeLabelValue(vegName),
	}
}

func podReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func insertNodeNexthop(nodeNexthops map[string]set.Set[string], nodeName, nexthop string) {
	if nodeNexthops[nodeName] == nil {
		nodeNexthops[nodeName] = set.New[string]()
	}
	nodeNexthops[nodeName].Insert(nexthop)
}

func flattenVpcEgressGatewayNexthops(nodeNexthops map[string]set.Set[string]) set.Set[string] {
	nextHops := set.New[string]()
	for _, nodeNextHops := range nodeNexthops {
		nextHops.Insert(nodeNextHops.UnsortedList()...)
	}
	return nextHops
}

func updateVpcEgressGatewayPolicyNexthops(policy *ovnnb.LogicalRouterPolicy, nextHops, bfdSessions set.Set[string]) bool {
	if nextHops.Equal(set.New(policy.Nexthops...)) && bfdSessions.Equal(set.New(policy.BFDSessions...)) {
		return false
	}
	policy.Nexthops = nextHops.UnsortedList()
	policy.BFDSessions = bfdSessions.UnsortedList()
	return true
}

func collectVpcEgressGatewayWorkloadStatus(gw *kubeovnv1.VpcEgressGateway, pods []*corev1.Pod, attachmentNetworkName string) (map[string]set.Set[string], map[string]set.Set[string], []string) {
	nodeNexthopIPv4 := make(map[string]set.Set[string], int(gw.Spec.Replicas))
	nodeNexthopIPv6 := make(map[string]set.Set[string], int(gw.Spec.Replicas))
	notReadyMessages := make([]string, 0)
	workloadNodes := set.New[string]()

	gw.Status.InternalIPs = nil
	gw.Status.ExternalIPs = nil
	gw.Status.Workload.Nodes = nil

	for _, pod := range pods {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}

		if pod.Status.Phase != corev1.PodRunning {
			notReadyMessages = append(notReadyMessages, fmt.Sprintf("pod %s/%s is not ready", pod.Namespace, pod.Name))
			continue
		}
		if !podReady(pod) {
			notReadyMessages = append(notReadyMessages, fmt.Sprintf("pod %s/%s is not ready", pod.Namespace, pod.Name))
		}

		ips := util.PodIPs(*pod)
		if len(ips) == 0 {
			notReadyMessages = append(notReadyMessages, fmt.Sprintf("pod %s/%s has no pod IP", pod.Namespace, pod.Name))
			continue
		}

		extIPs, err := util.PodAttachmentIPs(pod, attachmentNetworkName)
		if err != nil {
			notReadyMessages = append(notReadyMessages, err.Error())
			continue
		}
		if len(extIPs) == 0 {
			notReadyMessages = append(notReadyMessages, fmt.Sprintf("pod %s/%s has no IP for network %s", pod.Namespace, pod.Name, attachmentNetworkName))
			continue
		}

		ipv4, ipv6 := util.SplitIpsByProtocol(ips)
		if len(ipv4) != 0 {
			insertNodeNexthop(nodeNexthopIPv4, pod.Spec.NodeName, ipv4[0])
		}
		if len(ipv6) != 0 {
			insertNodeNexthop(nodeNexthopIPv6, pod.Spec.NodeName, ipv6[0])
		}
		gw.Status.InternalIPs = append(gw.Status.InternalIPs, strings.Join(ips, ","))
		gw.Status.ExternalIPs = append(gw.Status.ExternalIPs, strings.Join(extIPs, ","))
		workloadNodes.Insert(pod.Spec.NodeName)
	}
	gw.Status.Workload.Nodes = workloadNodes.SortedList()

	if len(gw.Status.ExternalIPs) != int(gw.Spec.Replicas) {
		notReadyMessages = append(notReadyMessages, fmt.Sprintf("expected %d networked workload pods with network %s, got %d", gw.Spec.Replicas, attachmentNetworkName, len(gw.Status.ExternalIPs)))
	}

	return nodeNexthopIPv4, nodeNexthopIPv6, notReadyMessages
}

func (c *Controller) recordVpcEgressGatewayEvent(gw *kubeovnv1.VpcEgressGateway, eventType, reason, message string) {
	if c.recorder == nil {
		return
	}
	c.recorder.Eventf(gw, eventType, reason, "%s", message)
}

func (c *Controller) recordVpcEgressGatewayError(gw *kubeovnv1.VpcEgressGateway, reason string, err error) error {
	c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, reason, err.Error())
	return err
}

func (c *Controller) recordVpcEgressGatewayKeyError(namespace, name, reason string, err error) error {
	gw := &kubeovnv1.VpcEgressGateway{Namespace: namespace, Name: name}
	return c.recordVpcEgressGatewayError(gw, reason, err)
}

func vpcEgressGatewayReadyConditionChanged(gw *kubeovnv1.VpcEgressGateway, status corev1.ConditionStatus, reason, message string) bool {
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.Ready)
	return condition == nil ||
		condition.Status != status ||
		condition.Reason != reason ||
		condition.Message != message ||
		condition.ObservedGeneration != gw.Generation
}

func setVpcEgressGatewayNotReady(gw *kubeovnv1.VpcEgressGateway, reason, message string) bool {
	conditionChanged := vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, reason, message)
	gw.Status.Ready = false
	gw.Status.Phase = kubeovnv1.PhaseProcessing
	gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, reason, message, gw.Generation)
	return conditionChanged
}

func (c *Controller) failVpcEgressGatewayReconcile(gw *kubeovnv1.VpcEgressGateway, reason string, reconcileErr error) error {
	setVpcEgressGatewayNotReady(gw, reason, reconcileErr.Error())
	c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, reason, reconcileErr.Error())
	if _, statusErr := c.updateVpcEgressGatewayStatus(gw); statusErr != nil {
		c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, "UpdateStatusFailed", statusErr.Error())
		return errors.Join(reconcileErr, statusErr)
	}
	return reconcileErr
}

type vpcEgressGatewayReconcileContext struct {
	gateway *kubeovnv1.VpcEgressGateway
	vpc     *kubeovnv1.Vpc
	bfdIP   string
	bfdIPv4 string
	bfdIPv6 string
}

type vpcEgressGatewayWorkloadState struct {
	ready           bool
	ipv4Src         set.Set[string]
	ipv6Src         set.Set[string]
	nodeNexthopIPv4 map[string]set.Set[string]
	nodeNexthopIPv6 map[string]set.Set[string]
}

func (c *Controller) prepareVpcEgressGateway(gw *kubeovnv1.VpcEgressGateway) (*vpcEgressGatewayReconcileContext, error) {
	var err error
	if gw, err = c.initVpcEgressGatewayStatus(gw); err != nil {
		return nil, err
	}

	vpcName := gw.Spec.VPC
	if vpcName == "" {
		vpcName = c.config.ClusterRouter
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		klog.Error(err)
		return nil, c.recordVpcEgressGatewayError(gw, "GetVpcFailed", err)
	}
	if gw.Spec.BFD.Enabled && vpc.Status.BFDPort.IP == "" {
		err = fmt.Errorf("vpc %s bfd port is not enabled or not ready", vpc.Name)
		klog.Error(err)
		gw.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "VpcBfdPortNotEnabled", err.Error(), gw.Generation)
		c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, "VpcBfdPortNotEnabled", err.Error())
		if _, statusErr := c.updateVpcEgressGatewayStatus(gw); statusErr != nil {
			return nil, errors.Join(err, c.recordVpcEgressGatewayError(gw, "UpdateStatusFailed", statusErr))
		}
		return nil, err
	}

	if controllerutil.AddFinalizer(gw, util.KubeOVNControllerFinalizer) {
		updatedGateway, err := c.config.KubeOvnClient.KubeovnV1().VpcEgressGateways(gw.Namespace).
			Update(context.Background(), gw, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for vpc-egress-gateway %s/%s: %w", gw.Namespace, gw.Name, err)
			klog.Error(err)
			return nil, c.recordVpcEgressGatewayError(gw, "AddFinalizerFailed", err)
		}
		gw = updatedGateway
	}

	ctx := &vpcEgressGatewayReconcileContext{gateway: gw, vpc: vpc}
	if gw.Spec.BFD.Enabled {
		ctx.bfdIP = vpc.Status.BFDPort.IP
		ctx.bfdIPv4, ctx.bfdIPv6 = util.SplitStringIP(ctx.bfdIP)
	}
	return ctx, nil
}

func (c *Controller) reconcileVpcEgressGatewayWorkloadState(ctx *vpcEgressGatewayReconcileContext) (*vpcEgressGatewayWorkloadState, error) {
	gw := ctx.gateway
	attachmentNetworkName, ipv4Src, ipv6Src, deploy, err := c.reconcileVpcEgressGatewayWorkload(
		gw, ctx.vpc, ctx.bfdIP, ctx.bfdIPv4, ctx.bfdIPv6,
	)
	gw.Status.Replicas = gw.Spec.Replicas
	gw.Status.LabelSelector = labels.FormatLabels(vegWorkloadLabels(gw.Name))
	if err != nil {
		klog.Error(err)
		gw.Status.Replicas = 0
		return nil, c.failVpcEgressGatewayReconcile(gw, "ReconcileWorkloadFailed", err)
	}

	gw.Status.InternalIPs = nil
	gw.Status.ExternalIPs = nil
	gw.Status.Workload.APIVersion = deploy.APIVersion
	gw.Status.Workload.Kind = deploy.Kind
	gw.Status.Workload.Name = deploy.Name
	gw.Status.Workload.Nodes = nil
	deploymentReady := util.DeploymentIsReady(deploy)
	if !deploymentReady {
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		if setVpcEgressGatewayNotReady(gw, "Processing", msg) {
			c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeNormal, "Processing", msg)
		}
	}

	podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		err = fmt.Errorf("failed to get pod selector of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return nil, c.failVpcEgressGatewayReconcile(gw, "GetPodSelectorFailed", err)
	}
	pods, err := c.podsLister.Pods(deploy.Namespace).List(podSelector)
	if err != nil {
		err = fmt.Errorf("failed to list pods of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return nil, c.failVpcEgressGatewayReconcile(gw, "ListWorkloadPodsFailed", err)
	}

	nodeNexthopIPv4, nodeNexthopIPv6, notReadyMessages := collectVpcEgressGatewayWorkloadStatus(gw, pods, attachmentNetworkName)
	workloadReady := len(notReadyMessages) == 0
	if !workloadReady {
		if deploymentReady {
			msg := strings.Join(notReadyMessages, "; ")
			if setVpcEgressGatewayNotReady(gw, "WorkloadNetworkNotReady", msg) {
				c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, "WorkloadNetworkNotReady", msg)
			}
		} else {
			gw.Status.Ready = false
			gw.Status.Phase = kubeovnv1.PhaseProcessing
		}
	}
	updatedGateway, err := c.updateVpcEgressGatewayStatus(gw)
	if err != nil {
		return nil, c.recordVpcEgressGatewayError(gw, "UpdateStatusFailed", err)
	}
	ctx.gateway = updatedGateway

	return &vpcEgressGatewayWorkloadState{
		ready:           deploymentReady && workloadReady,
		ipv4Src:         ipv4Src,
		ipv6Src:         ipv6Src,
		nodeNexthopIPv4: nodeNexthopIPv4,
		nodeNexthopIPv6: nodeNexthopIPv6,
	}, nil
}

func (c *Controller) reconcileVpcEgressGatewayRoutes(ctx *vpcEgressGatewayReconcileContext, state *vpcEgressGatewayWorkloadState) error {
	routes := [...]struct {
		addressFamily int
		protocol      string
		bfdIP         string
		nexthops      map[string]set.Set[string]
		sources       set.Set[string]
	}{
		{addressFamily: 4, protocol: "IPv4", bfdIP: ctx.bfdIPv4, nexthops: state.nodeNexthopIPv4, sources: state.ipv4Src},
		{addressFamily: 6, protocol: "IPv6", bfdIP: ctx.bfdIPv6, nexthops: state.nodeNexthopIPv6, sources: state.ipv6Src},
	}
	for _, route := range routes {
		if err := c.reconcileVpcEgressGatewayOVNRoutes(ctx.gateway, route.addressFamily, ctx.vpc.Status.Router,
			ctx.vpc.Status.BFDPort.Name, route.bfdIP, route.nexthops, route.sources); err != nil {
			klog.Error(err)
			err = fmt.Errorf("failed to reconcile %s OVN routes: %w", route.protocol, err)
			return c.failVpcEgressGatewayReconcile(ctx.gateway, "ReconcileOVNRoutesFailed", err)
		}
	}
	return nil
}

func (c *Controller) completeVpcEgressGatewayReconcile(gw *kubeovnv1.VpcEgressGateway) error {
	gw.Status.Ready = true
	gw.Status.Phase = kubeovnv1.PhaseCompleted
	conditionChanged := vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionTrue, "ReconcileSuccess", "")
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)
	updatedGateway, err := c.updateVpcEgressGatewayStatus(gw)
	if err != nil {
		return c.recordVpcEgressGatewayError(gw, "UpdateStatusFailed", err)
	}
	if conditionChanged {
		c.recordVpcEgressGatewayEvent(updatedGateway, corev1.EventTypeNormal, "ReconcileSuccess", fmt.Sprintf("VpcEgressGateway %s/%s reconciled successfully", gw.Namespace, gw.Name))
	}
	return nil
}

func (c *Controller) handleAddOrUpdateVpcEgressGateway(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.vpcEgressGatewayKeyMutex.LockKey(key)
	defer func() { _ = c.vpcEgressGatewayKeyMutex.UnlockKey(key) }()

	cachedGateway, err := c.vpcEgressGatewayLister.VpcEgressGateways(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return c.recordVpcEgressGatewayKeyError(ns, name, "GetVpcEgressGatewayFailed", err)
		}
		return nil
	}

	if !cachedGateway.DeletionTimestamp.IsZero() {
		c.delVpcEgressGatewayQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling vpc-egress-gateway %s", key)
	ctx, err := c.prepareVpcEgressGateway(cachedGateway.DeepCopy())
	if err != nil {
		return err
	}

	state, err := c.reconcileVpcEgressGatewayWorkloadState(ctx)
	if err != nil {
		return err
	}
	if err = c.reconcileVpcEgressGatewayRoutes(ctx, state); err != nil {
		return err
	}
	if state.ready {
		if err = c.completeVpcEgressGatewayReconcile(ctx.gateway); err != nil {
			return err
		}
	}

	klog.Infof("finished reconciling vpc-egress-gateway %s", key)

	return nil
}

func (c *Controller) initVpcEgressGatewayStatus(gw *kubeovnv1.VpcEgressGateway) (*kubeovnv1.VpcEgressGateway, error) {
	if gw.Status.Phase == "" || gw.Status.Phase == kubeovnv1.PhasePending {
		gw.Status.Phase = kubeovnv1.PhaseProcessing
		updatedGateway, err := c.updateVpcEgressGatewayStatus(gw)
		if err != nil {
			return gw, c.recordVpcEgressGatewayError(gw, "UpdateStatusFailed", err)
		}
		gw = updatedGateway
		c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeNormal, "Processing", fmt.Sprintf("Started reconciling VpcEgressGateway %s/%s", gw.Namespace, gw.Name))
	}
	return gw, nil
}

func (c *Controller) updateVpcEgressGatewayStatus(gw *kubeovnv1.VpcEgressGateway) (*kubeovnv1.VpcEgressGateway, error) {
	if len(gw.Status.Conditions) == 0 {
		gw.Status.Conditions.SetCondition(kubeovnv1.Init, corev1.ConditionUnknown, "Processing", "", gw.Generation)
	}
	if !gw.Status.Ready {
		gw.Status.Phase = kubeovnv1.PhaseProcessing
	}

	updateGateway, err := c.config.KubeOvnClient.KubeovnV1().VpcEgressGateways(gw.Namespace).
		UpdateStatus(context.Background(), gw, metav1.UpdateOptions{})
	if err != nil {
		err = fmt.Errorf("failed to update status of vpc-egress-gateway %s/%s: %w", gw.Namespace, gw.Name, err)
		klog.Error(err)
		return nil, err
	}

	return updateGateway, nil
}

// create or update vpc egress gateway workload
func (c *Controller) reconcileVpcEgressGatewayWorkload(gw *kubeovnv1.VpcEgressGateway, vpc *kubeovnv1.Vpc, bfdIP, bfdIPv4, bfdIPv6 string) (string, set.Set[string], set.Set[string], *appsv1.Deployment, error) {
	image := c.config.Image
	if gw.Spec.Image != "" {
		image = gw.Spec.Image
	}
	if image == "" {
		err := fmt.Errorf("no image specified for vpc egress gateway %s/%s", gw.Namespace, gw.Name)
		klog.Error(err)
		return "", nil, nil, nil, err
	}

	if len(gw.Spec.InternalIPs) != 0 && len(gw.Spec.InternalIPs) < int(gw.Spec.Replicas) {
		err := fmt.Errorf("internal IPs count %d is less than replicas %d", len(gw.Spec.InternalIPs), gw.Spec.Replicas)
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	if len(gw.Spec.ExternalIPs) != 0 && len(gw.Spec.ExternalIPs) < int(gw.Spec.Replicas) {
		err := fmt.Errorf("external IPs count %d is less than replicas %d", len(gw.Spec.ExternalIPs), gw.Spec.Replicas)
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	if len(gw.Spec.InternalIPs) != 0 && gw.Spec.InternalIPPool != "" {
		err := errors.New("internalIPs and internalIPPool are mutually exclusive")
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	if len(gw.Spec.ExternalIPs) != 0 && gw.Spec.ExternalIPPool != "" {
		err := errors.New("externalIPs and externalIPPool are mutually exclusive")
		klog.Error(err)
		return "", nil, nil, nil, err
	}

	internalSubnet := gw.Spec.InternalSubnet
	if internalSubnet == "" {
		internalSubnet = vpc.Status.DefaultLogicalSwitch
	}
	if internalSubnet == "" {
		err := fmt.Errorf("default subnet of vpc %s not found, please set internal subnet of the egress gateway", vpc.Name)
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	intSubnet, err := c.subnetsLister.Get(internalSubnet)
	if err != nil {
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	extSubnet, err := c.subnetsLister.Get(gw.Spec.ExternalSubnet)
	if err != nil {
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	if !strings.ContainsRune(extSubnet.Spec.Provider, '.') {
		err = fmt.Errorf("please set correct provider of subnet %s to get the network-attachment-definition", extSubnet.Name)
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	subStrings := strings.Split(extSubnet.Spec.Provider, ".")
	nadName, nadNamespace := subStrings[0], subStrings[1]
	if _, err = c.netAttachLister.NetworkAttachmentDefinitions(nadNamespace).Get(nadName); err != nil {
		klog.Errorf("failed to get net-attach-def %s/%s: %v", nadNamespace, nadName, err)
		return "", nil, nil, nil, err
	}
	attachmentNetworkName := fmt.Sprintf("%s/%s", nadNamespace, nadName)
	internalCIDRv4, internalCIDRv6 := util.SplitStringIP(intSubnet.Spec.CIDRBlock)

	// collect egress policies
	ipv4ForwardSrc, ipv6ForwardSrc := set.New[string](), set.New[string]()
	ipv4SNATSrc, ipv6SNATSrc := set.New[string](), set.New[string]()
	for _, policy := range gw.Spec.Policies {
		ipv4, ipv6 := util.SplitIpsByProtocol(policy.IPBlocks)
		if policy.SNAT {
			ipv4SNATSrc = ipv4SNATSrc.Insert(ipv4...)
			ipv6SNATSrc = ipv6SNATSrc.Insert(ipv6...)
		} else {
			ipv4ForwardSrc = ipv4ForwardSrc.Insert(ipv4...)
			ipv6ForwardSrc = ipv6ForwardSrc.Insert(ipv6...)
		}
		for _, subnetName := range policy.Subnets {
			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Error(err)
				return attachmentNetworkName, nil, nil, nil, err
			}
			if subnet.Status.IsNotValidated() {
				err = fmt.Errorf("subnet %s is not validated", subnet.Name)
				klog.Error(err)
				return attachmentNetworkName, nil, nil, nil, err
			}
			// TODO: check subnet's vpc and vlan
			ipv4, ipv6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
			if policy.SNAT {
				ipv4SNATSrc.Insert(ipv4)
				ipv6SNATSrc.Insert(ipv6)
			} else {
				ipv4ForwardSrc.Insert(ipv4)
				ipv6ForwardSrc.Insert(ipv6)
			}
		}
	}

	// calculate internal route destinations and forward source CIDR blocks
	ipv4ForwardSrc.Delete("")
	ipv6ForwardSrc.Delete("")
	ipv4SNATSrc.Delete("")
	ipv6SNATSrc.Delete("")
	ipv4Src := ipv4ForwardSrc.Union(ipv4SNATSrc)
	ipv6Src := ipv6ForwardSrc.Union(ipv6SNATSrc)

	// filter out ip blocks within the internal subnet CIDR(s) to avoid route(s) configuration failure
	fnFilter := func(internalCIDR string, ipBlocks set.Set[string]) set.Set[string] {
		if internalCIDR == "" {
			return nil
		}

		ret := set.New[string]()
		for cidr := range ipBlocks {
			if ok, _ := util.CIDRContainsCIDR(internalCIDR, cidr); !ok {
				ret.Insert(cidr)
			}
		}
		return ret
	}
	intRouteDstIPv4 := fnFilter(internalCIDRv4, ipv4Src)
	intRouteDstIPv6 := fnFilter(internalCIDRv6, ipv6Src)

	// generate route annotations used to configure routes in the pod
	routes := util.NewPodRoutes()
	intGatewayIPv4, intGatewayIPv6 := util.SplitStringIP(intSubnet.Spec.Gateway)
	extGatewayIPv4, extGatewayIPv6 := util.SplitStringIP(extSubnet.Spec.Gateway)
	// add routes for the VPC BFD Port so that the egress gateway can establish BFD session(s) with it
	routes.Add(util.OvnProvider, bfdIPv4, intGatewayIPv4)
	routes.Add(util.OvnProvider, bfdIPv6, intGatewayIPv6)
	// add routes for the internal networks
	for _, dst := range intRouteDstIPv4.UnsortedList() {
		routes.Add(util.OvnProvider, dst, intGatewayIPv4)
	}
	for _, dst := range intRouteDstIPv6.UnsortedList() {
		routes.Add(util.OvnProvider, dst, intGatewayIPv6)
	}
	// add default routes to forward traffic to the external network
	routes.Add(extSubnet.Spec.Provider, "0.0.0.0/0", extGatewayIPv4)
	routes.Add(extSubnet.Spec.Provider, "::/0", extGatewayIPv6)

	// generate pod annotations
	annotations, err := routes.ToAnnotations()
	if err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}
	annotations[nadv1.NetworkAttachmentAnnot] = attachmentNetworkName
	annotations[util.LogicalSwitchAnnotation] = intSubnet.Name
	if len(gw.Spec.InternalIPs) != 0 {
		// set internal IPs
		annotations[util.IPPoolAnnotation] = strings.Join(gw.Spec.InternalIPs, ";")
	} else if gw.Spec.InternalIPPool != "" {
		// allocate the internal IP from the referenced IPPool
		pool, err := c.ippoolLister.Get(gw.Spec.InternalIPPool)
		if err != nil {
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		if pool.Spec.Subnet != intSubnet.Name {
			err = fmt.Errorf("subnet %q of internal IPPool %q does not match internal subnet %q", pool.Spec.Subnet, gw.Spec.InternalIPPool, intSubnet.Name)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		annotations[util.IPPoolAnnotation] = gw.Spec.InternalIPPool
	}
	if len(gw.Spec.ExternalIPs) != 0 {
		// set external IPs
		annotations[fmt.Sprintf(util.IPPoolAnnotationTemplate, extSubnet.Spec.Provider)] = strings.Join(gw.Spec.ExternalIPs, ";")
	} else if gw.Spec.ExternalIPPool != "" {
		// allocate the external IP from the referenced IPPool
		pool, err := c.ippoolLister.Get(gw.Spec.ExternalIPPool)
		if err != nil {
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		if pool.Spec.Subnet != extSubnet.Name {
			err = fmt.Errorf("subnet %q of external IPPool %q does not match external subnet %q", pool.Spec.Subnet, gw.Spec.ExternalIPPool, extSubnet.Name)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		annotations[fmt.Sprintf(util.IPPoolAnnotationTemplate, extSubnet.Spec.Provider)] = gw.Spec.ExternalIPPool
	}
	if err := setVpcEgressGatewayBandwidthAnnotations(annotations, gw.Spec.Bandwidth); err != nil {
		return attachmentNetworkName, nil, nil, nil, err
	}

	// generate init container environment variables
	// the init container is responsible for adding routes and SNAT rules to the pod network namespace
	initEnv, err := vpcEgressGatewayInitContainerEnv(4, intGatewayIPv4, extGatewayIPv4, ipv4ForwardSrc)
	if err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}
	ipv6Env, err := vpcEgressGatewayInitContainerEnv(6, intGatewayIPv6, extGatewayIPv6, ipv6ForwardSrc)
	if err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}
	initEnv = append(initEnv, ipv6Env...)

	var evpnConf *kubeovnv1.EvpnConf
	if gw.Spec.BgpConf != "" {
		initEnv = append(initEnv, corev1.EnvVar{
			Name:  "ENABLE_BGP",
			Value: "true",
		})

		if gw.Spec.EvpnConf != "" {
			evpnLister := c.evpnConfLister.Load()
			if evpnLister == nil {
				err = fmt.Errorf("EvpnConf CRD is not installed on the cluster, cannot configure EVPN for vpc-egress-gateway %s/%s", gw.Namespace, gw.Name)
				klog.Error(err)
				return attachmentNetworkName, nil, nil, nil, err
			}
			evpnConf, err = (*evpnLister).Get(gw.Spec.EvpnConf)
			if err != nil {
				err = fmt.Errorf("failed to get EvpnConf %s: %w", gw.Spec.EvpnConf, err)
				klog.Error(err)
				return attachmentNetworkName, nil, nil, nil, err
			}
			initEnv = append(initEnv, corev1.EnvVar{
				Name:  "ENABLE_EVPN",
				Value: "true",
			}, corev1.EnvVar{
				Name:  "VNI",
				Value: strconv.FormatUint(uint64(evpnConf.Spec.VNI), 10),
			})
		}
	}

	// generate workload
	labels := vegWorkloadLabels(gw.Name)
	observerState := c.reconcileVpcEgressGatewayObservability(gw, attachmentNetworkName, labels)
	deploy := &appsv1.Deployment{
		Name:      gw.Spec.Prefix + gw.Name,
		Namespace: gw.Namespace,
		Labels:    labels,
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Strategy: genGatewayDeploymentStrategy(),
			Template: corev1.PodTemplateSpec{
				Labels:      labels,
				Annotations: annotations,
				Spec: corev1.PodSpec{
					Affinity: mergeGatewayAffinity(
						genGatewayPodAntiAffinity(labels, gw.Spec.PodAntiAffinity),
						&corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: mergeNodeSelector(gw.Spec.NodeSelector),
							},
						},
					),
					InitContainers: []corev1.Container{{
						Name:            "init",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command: []string{
							"bash",
							"-exc",
							"chmod +t /usr/local/sbin && bash /kube-ovn/init-vpc-egress-gateway.sh",
						},
						Env: initEnv,
						SecurityContext: &corev1.SecurityContext{
							Privileged: new(true),
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "usr-local-sbin",
							MountPath: "/usr/local/sbin",
						}},
					}},
					Containers: []corev1.Container{
						genGatewaySleepContainer(image),
					},
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Tolerations: slices.Clone(gw.Spec.Tolerations),
					Volumes: []corev1.Volume{{
						Name:     "usr-local-sbin",
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					}},
					TerminationGracePeriodSeconds: ptr.To[int64](0),
				},
			},
		},
	}
	// set owner reference so that the workload will be deleted automatically when the vpc egress gateway is deleted
	if err = util.SetOwnerReference(gw, deploy); err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}

	if bfdIP != "" {
		// Run BFD in the gateway container to establish BFD sessions with the VPC BFD LRP.
		container := genVpcEgressGatewayBFDDContainer(
			image, bfdIP, gw.Spec.BFD.MinTX, gw.Spec.BFD.MinRX, gw.Spec.BFD.Multiplier,
			vpc.Name == c.config.ClusterRouter,
		)
		if err = configureVpcEgressGatewayBFDWorkload(deploy, container); err != nil {
			return attachmentNetworkName, nil, nil, nil, err
		}
	}

	if gw.Spec.Resources != nil {
		if err = setVpcEgressGatewayWorkloadResources(deploy, *gw.Spec.Resources); err != nil {
			return attachmentNetworkName, nil, nil, nil, err
		}
	}

	// add FRR container if bgpConf is specified
	if gw.Spec.BgpConf != "" {
		bgpLister := c.bgpConfLister.Load()
		if bgpLister == nil {
			err = fmt.Errorf("BgpConf CRD is not installed on the cluster, cannot configure BGP for vpc-egress-gateway %s/%s", gw.Namespace, gw.Name)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		bgpConf, err := (*bgpLister).Get(gw.Spec.BgpConf)
		if err != nil {
			err = fmt.Errorf("failed to get BgpConf %s: %w", gw.Spec.BgpConf, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}

		frrInitContainer := vpcEgressGatewayInitContainerFRRConfig(c.config.Image, bgpConf, evpnConf)
		deploy.Spec.Template.Spec.InitContainers = append(deploy.Spec.Template.Spec.InitContainers, frrInitContainer)

		frrContainer := vpcEgressGatewayContainerFRR(c.config.FRRImage)
		deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, frrContainer)

		frrVolume := corev1.Volume{
			Name:     "frr-config",
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
		deploy.Spec.Template.Spec.Volumes = append(deploy.Spec.Template.Spec.Volumes, frrVolume)
	}
	addVpcEgressGatewayObserver(&deploy.Spec.Template.Spec, image, gw.Spec.Observability, observerState)

	// generate hash for the workload to determine whether to update the existing workload or not
	hash, err := util.Sha256HashObject(deploy)
	if err != nil {
		err = fmt.Errorf("failed to hash generated deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}

	hash = hash[:12]
	// replicas and the hash annotation should be excluded from hash calculation
	deploy.Spec.Replicas = new(gw.Spec.Replicas)
	deploy.Annotations = map[string]string{util.GenerateHashAnnotation: hash}
	var currentDeploy *appsv1.Deployment
	if c.deploymentsLister != nil {
		if currentDeploy, err = c.deploymentsLister.Deployments(gw.Namespace).Get(deploy.Name); err != nil && !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
	}
	switch {
	case currentDeploy == nil:
		if deploy, err = c.config.KubeClient.AppsV1().Deployments(gw.Namespace).
			Create(context.Background(), deploy, metav1.CreateOptions{}); err != nil {
			err = fmt.Errorf("failed to create deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
	case !reflect.DeepEqual(currentDeploy.Spec.Replicas, deploy.Spec.Replicas) ||
		currentDeploy.Annotations[util.GenerateHashAnnotation] != hash:
		// update the deployment if replicas or hash annotation is changed
		if deploy, err = c.config.KubeClient.AppsV1().Deployments(gw.Namespace).
			Update(context.Background(), deploy, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to update deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
	default:
		// no need to create or update the deployment
		deploy = currentDeploy
	}

	// return the source CIDR blocks for later OVN resources reconciliation
	deploy.APIVersion, deploy.Kind = appsv1.SchemeGroupVersion.String(), util.KindDeployment
	return attachmentNetworkName, ipv4Src, ipv6Src, deploy, nil
}

func setVpcEgressGatewayBandwidthAnnotations(annotations map[string]string, bandwidth *kubeovnv1.BandwidthLimit) error {
	ingressMbps, egressMbps, err := bandwidth.Mbps()
	if err != nil {
		return err
	}
	if ingressMbps > 0 {
		// The 'ingress' on the VpcEgressGateway CRD refers to traffic entering the VPC from external.
		// From the gateway pod's perspective, this is egress traffic of the primary network interface.
		annotations[util.EgressRateAnnotation] = strconv.FormatInt(ingressMbps, 10)
	}
	if egressMbps > 0 {
		// The 'egress' on the VpcEgressGateway CRD refers to traffic leaving the VPC.
		// From the gateway pod's perspective, this is ingress traffic of the primary network interface.
		annotations[util.IngressRateAnnotation] = strconv.FormatInt(egressMbps, 10)
	}
	return nil
}

func vpcEgressGatewayPolicyMatches(af int, pgName, asName string, includePortGroup bool) set.Set[string] {
	matches := set.New[string](fmt.Sprintf("ip%d.src == $%s", af, asName))
	if includePortGroup {
		matches.Insert(fmt.Sprintf("ip%d.src == $%s_ip%d", af, pgName, af))
	}
	return matches
}

func vpcEgressGatewayLocalPolicyMatches(af int, localPgName, pgName, asName string, includePortGroup bool) set.Set[string] {
	matches := set.New[string](fmt.Sprintf(
		"ip%d.src == $%s_ip%d && ip%d.src == $%s", af, localPgName, af, af, asName,
	))
	if includePortGroup {
		matches.Insert(fmt.Sprintf(
			"ip%d.src == $%s_ip%d && ip%d.src == $%s_ip%d", af, localPgName, af, af, pgName, af,
		))
	}
	return matches
}

func (c *Controller) reconcileVpcEgressGatewayOVNRoutes(gw *kubeovnv1.VpcEgressGateway, af int, lrName, lrpName, bfdIP string, nodeNexthops map[string]set.Set[string], sources set.Set[string]) error {
	nextHops := flattenVpcEgressGatewayNexthops(nodeNexthops)

	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: fmt.Sprintf("%s/%s", gw.Namespace, gw.Name),
		"af":                           strconv.Itoa(af),
	}

	// reconcile OVN port group
	var err error
	ports := set.New[string]()
	for _, selector := range gw.Spec.Selectors {
		sel := labels.Everything()
		if selector.NamespaceSelector != nil {
			if sel, err = metav1.LabelSelectorAsSelector(selector.NamespaceSelector); err != nil {
				err = fmt.Errorf("failed to create label selector for namespace selector %#v: %w", selector.NamespaceSelector, err)
				klog.Error(err)
				return err
			}
		}
		namespaces, err := c.namespacesLister.List(sel)
		if err != nil {
			err = fmt.Errorf("failed to list namespaces with selector %s: %w", sel, err)
			klog.Error(err)
			return err
		}
		sel = labels.Everything()
		if selector.PodSelector != nil {
			if sel, err = metav1.LabelSelectorAsSelector(selector.PodSelector); err != nil {
				err = fmt.Errorf("failed to create label selector for pod selector %#v: %w", selector.PodSelector, err)
				klog.Error(err)
				return err
			}
		}
		for _, ns := range namespaces {
			pods, err := c.podsLister.Pods(ns.Name).List(sel)
			if err != nil {
				err = fmt.Errorf("failed to list pods with selector %s in namespace %s: %w", sel, ns.Name, err)
				klog.Error(err)
				return err
			}
			for _, pod := range pods {
				if pod.Spec.HostNetwork ||
					pod.Annotations[util.AllocatedAnnotation] != "true" ||
					pod.Annotations[util.LogicalRouterAnnotation] != gw.VPC(c.config.ClusterRouter) ||
					!isPodAlive(pod) {
					continue
				}
				podName := c.getNameByPod(pod)
				ports.Insert(ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider))
			}
		}
	}
	key := cache.MetaObjectToName(gw).String()
	pgName := vegPortGroupName(key)
	// Keep both policy forms for compatibility with existing policy observers. For
	// subnet-only gateways, the port-group address-set name is backed by an empty
	// standalone address set below because no Port Group selector exists.
	includePortGroup := true
	hasSelectors := len(gw.Spec.Selectors) > 0
	if hasSelectors {
		if err = c.createPortGroup(pgName, externalIDs); err != nil {
			err = fmt.Errorf("failed to create port group %s: %w", pgName, err)
			klog.Error(err)
			return err
		}
		if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports.UnsortedList()); err != nil {
			err = fmt.Errorf("failed to set ports of port group %s: %w", pgName, err)
			klog.Error(err)
			return err
		}
	} else if err = c.deletePortGroups(pgName); err != nil {
		err = fmt.Errorf("failed to delete stale port group %s: %w", pgName, err)
		klog.Error(err)
		return err
	}

	// reconcile OVN address set
	asName := vegAddressSetName(key, af)
	if err = c.createAddressSet(asName, externalIDs); err != nil {
		err = fmt.Errorf("failed to create address set %s: %w", asName, err)
		klog.Error(err)
		return err
	}
	if err = c.OVNNbClient.AddressSetUpdateAddress(asName, sources.SortedList()...); err != nil {
		err = fmt.Errorf("failed to update address set %s: %w", asName, err)
		klog.Error(err)
		return err
	}
	compatAsName := vegPortGroupAddressSetName(key, af)
	if !hasSelectors {
		if err = c.createAddressSet(compatAsName, externalIDs); err != nil {
			err = fmt.Errorf("failed to create compatibility address set %s: %w", compatAsName, err)
			klog.Error(err)
			return err
		}
		if err = c.OVNNbClient.AddressSetUpdateAddress(compatAsName); err != nil {
			err = fmt.Errorf("failed to clear compatibility address set %s: %w", compatAsName, err)
			klog.Error(err)
			return err
		}
	} else if err = c.deleteAddressSets(compatAsName); err != nil {
		err = fmt.Errorf("failed to delete stale compatibility address set %s: %w", compatAsName, err)
		klog.Error(err)
		return err
	}

	// reconcile OVN BFD entries
	bfdIDs, bfdMap, staleBFDIDs, err := reconcileGatewayBFD(
		c.OVNNbClient,
		bfdIP,
		lrpName,
		nextHops,
		gw.Spec.BFD.MinTX,
		gw.Spec.BFD.MinRX,
		gw.Spec.BFD.Multiplier,
		externalIDs,
	)
	if err != nil {
		return err
	}

	// reconcile LR policy
	if gw.Spec.TrafficPolicy == kubeovnv1.TrafficPolicyLocal {
		rules := make(map[string]set.Set[string], len(nodeNexthops))
		for nodeName, nodeNextHops := range nodeNexthops {
			node, err := c.nodesLister.Get(nodeName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				klog.Errorf("failed to get node %s: %v", nodeName, err)
				return err
			}
			portName := node.Annotations[util.PortNameAnnotation]
			if portName == "" {
				err = fmt.Errorf("node %s does not have port name annotation", nodeName)
				klog.Error(err)
				return err
			}
			localPgName := strings.ReplaceAll(portName, "-", ".")
			for _, match := range vpcEgressGatewayLocalPolicyMatches(af, localPgName, pgName, asName, includePortGroup).UnsortedList() {
				rules[match] = nodeNextHops
			}
		}
		policies, err := c.listLogicalRouterPolicies(lrName, util.EgressGatewayLocalPolicyPriority, externalIDs, false)
		if err != nil {
			klog.Error(err)
			return err
		}
		// update/delete existing policies
		for _, policy := range policies {
			nextHops := rules[policy.Match]
			if nextHops.Len() == 0 {
				if err = c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
					err = fmt.Errorf("failed to delete ovn lr policy %q: %w", policy.Match, err)
					klog.Error(err)
					return err
				}
			} else {
				bfdSessions := localGatewayPolicyBFDSessions(bfdMap, nextHops)
				if updateVpcEgressGatewayPolicyNexthops(policy, nextHops, bfdSessions) {
					if err = c.updateLogicalRouterPolicy(policy, &policy.Nexthops, &policy.BFDSessions); err != nil {
						err = fmt.Errorf("failed to update logical router policy %s: %w", policy.UUID, err)
						klog.Error(err)
						return err
					}
				}
			}
			delete(rules, policy.Match)
		}
		// create new policies
		for match, nextHops := range rules {
			if err = c.OVNNbClient.AddLogicalRouterPolicy(lrName, util.EgressGatewayLocalPolicyPriority, match,
				ovnnb.LogicalRouterPolicyActionReroute, nextHops.UnsortedList(), localGatewayPolicyBFDSessions(bfdMap, nextHops).UnsortedList(), externalIDs); err != nil {
				klog.Error(err)
				return err
			}
		}
	} else {
		if err = c.OVNNbClient.DeleteLogicalRouterPolicies(lrName, util.EgressGatewayLocalPolicyPriority, externalIDs); err != nil {
			klog.Error(err)
			return err
		}
	}
	policies, err := c.listLogicalRouterPolicies(lrName, util.EgressGatewayPolicyPriority, externalIDs, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	matches := set.New[string]()
	if nextHops.Len() != 0 {
		matches = vpcEgressGatewayPolicyMatches(af, pgName, asName, includePortGroup)
	}
	for _, policy := range policies {
		if matches.Has(policy.Match) {
			if updateVpcEgressGatewayPolicyNexthops(policy, nextHops, bfdIDs) {
				if err = c.updateLogicalRouterPolicy(policy, &policy.Nexthops, &policy.BFDSessions); err != nil {
					err = fmt.Errorf("failed to update bfd sessions of logical router policy %s: %w", policy.UUID, err)
					klog.Error(err)
					return err
				}
			}
			matches.Delete(policy.Match)
			continue
		}
		if err = c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
			err = fmt.Errorf("failed to delete ovn lr policy %q: %w", policy.Match, err)
			klog.Error(err)
			return err
		}
	}
	for _, match := range matches.UnsortedList() {
		if err = c.OVNNbClient.AddLogicalRouterPolicy(lrName, util.EgressGatewayPolicyPriority, match,
			ovnnb.LogicalRouterPolicyActionReroute, nextHops.UnsortedList(), bfdIDs.UnsortedList(), externalIDs); err != nil {
			klog.Error(err)
			return err
		}
	}

	if gw.Spec.BFD.Enabled {
		// drop traffic if no nexthop is available
		if policies, err = c.listLogicalRouterPolicies(lrName, util.EgressGatewayDropPolicyPriority, externalIDs, false); err != nil {
			klog.Error(err)
			return err
		}
		matches = vpcEgressGatewayPolicyMatches(af, pgName, asName, includePortGroup)
		for _, policy := range policies {
			if matches.Has(policy.Match) {
				matches.Delete(policy.Match)
				continue
			}
			if err = c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
				err = fmt.Errorf("failed to delete ovn lr policy %q: %w", policy.Match, err)
				klog.Error(err)
				return err
			}
		}
		for _, match := range matches.UnsortedList() {
			if err = c.OVNNbClient.AddLogicalRouterPolicy(lrName, util.EgressGatewayDropPolicyPriority, match,
				ovnnb.LogicalRouterPolicyActionDrop, nil, nil, externalIDs); err != nil {
				klog.Error(err)
				return err
			}
		}
	} else if err = c.OVNNbClient.DeleteLogicalRouterPolicies(lrName, util.EgressGatewayDropPolicyPriority, externalIDs); err != nil {
		klog.Error(err)
		return err
	}

	// Cleanup stale BFD sessions
	if err = cleanupStaleBFD(c.OVNNbClient, staleBFDIDs); err != nil {
		return err
	}

	return nil
}

func localGatewayPolicyBFDSessions(bfdMap map[string]string, nextHops set.Set[string]) set.Set[string] {
	bfdSessions := set.New[string]()
	for nextHop := range nextHops {
		if bfdID := bfdMap[nextHop]; bfdID != "" {
			bfdSessions.Insert(bfdID)
		}
	}
	return bfdSessions
}

func mergeNodeSelector(nodeSelector []kubeovnv1.VpcEgressGatewayNodeSelector) *corev1.NodeSelector {
	if len(nodeSelector) == 0 {
		return nil
	}

	result := &corev1.NodeSelector{
		NodeSelectorTerms: make([]corev1.NodeSelectorTerm, len(nodeSelector)),
	}
	for i, selector := range nodeSelector {
		result.NodeSelectorTerms[i] = corev1.NodeSelectorTerm{
			MatchExpressions: make([]corev1.NodeSelectorRequirement, len(selector.MatchExpressions), len(selector.MatchLabels)+len(selector.MatchExpressions)),
			MatchFields:      make([]corev1.NodeSelectorRequirement, len(selector.MatchFields)),
		}
		for j := range selector.MatchExpressions {
			selector.MatchExpressions[j].DeepCopyInto(&result.NodeSelectorTerms[i].MatchExpressions[j])
		}
		for _, key := range slices.Sorted(maps.Keys(selector.MatchLabels)) {
			result.NodeSelectorTerms[i].MatchExpressions = append(result.NodeSelectorTerms[i].MatchExpressions, corev1.NodeSelectorRequirement{
				Key:      key,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{selector.MatchLabels[key]},
			})
		}
		for j := range selector.MatchFields {
			selector.MatchFields[j].DeepCopyInto(&result.NodeSelectorTerms[i].MatchFields[j])
		}
	}

	return result
}

func vpcEgressGatewayInitContainerEnv(af int, internalGateway, externalGateway string, forwardSrc set.Set[string]) ([]corev1.EnvVar, error) {
	if internalGateway == "" {
		return nil, nil
	}

	return []corev1.EnvVar{{
		Name:  fmt.Sprintf("INTERNAL_GATEWAY_IPV%d", af),
		Value: internalGateway,
	}, {
		Name:  fmt.Sprintf("EXTERNAL_GATEWAY_IPV%d", af),
		Value: externalGateway,
	}, {
		Name:  fmt.Sprintf("NO_SNAT_SOURCES_IPV%d", af),
		Value: strings.Join(forwardSrc.SortedList(), ","),
	}}, nil
}

func genVpcEgressGatewayBFDDContainer(image, bfdIP string, minTX, minRX, multiplier int32, useHTTPProbe bool) corev1.Container {
	container := genGatewayBFDDContainer(image, bfdIP, minTX, minRX, multiplier)
	container.Resources.Limits[corev1.ResourceCPU] = vegBFDDSupervisorLimitCPU
	container.Resources.Limits[corev1.ResourceMemory] = vegBFDDSupervisorLimitMemory
	execProbeHandler := func() corev1.ProbeHandler {
		return corev1.ProbeHandler{Exec: &corev1.ExecAction{
			Command: []string{vegBFDDSupervisorBin, "live"},
		}}
	}
	runtimeProbeHandler := execProbeHandler
	if useHTTPProbe {
		runtimeProbeHandler = func() corev1.ProbeHandler {
			return corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path:   "/livez",
				Port:   intstr.FromString(vegBFDDMetricsPort),
				Scheme: corev1.URISchemeHTTP,
			}}
		}
	}

	container.Command = []string{vegBFDDSupervisorBin, "run"}
	container.Ports = []corev1.ContainerPort{{
		Name:          vegBFDDMetricsPort,
		ContainerPort: 10669,
		Protocol:      corev1.ProtocolTCP,
	}}
	container.StartupProbe = &corev1.Probe{
		ProbeHandler:        execProbeHandler(),
		InitialDelaySeconds: 1,
		PeriodSeconds:       2,
		TimeoutSeconds:      10,
		FailureThreshold:    30,
	}
	container.LivenessProbe = &corev1.Probe{
		ProbeHandler:        runtimeProbeHandler(),
		InitialDelaySeconds: 1,
		PeriodSeconds:       5,
		TimeoutSeconds:      10,
	}
	container.ReadinessProbe = &corev1.Probe{
		// Strict session readiness remains disabled until every supported
		// controller can reconcile network-complete NotReady pods.
		ProbeHandler:        runtimeProbeHandler(),
		InitialDelaySeconds: 3,
		PeriodSeconds:       3,
		TimeoutSeconds:      10,
		FailureThreshold:    1,
	}
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      vegBFDDStateVolume,
		MountPath: vegBFDDStateDir,
	})
	return container
}

func configureVpcEgressGatewayBFDWorkload(deploy *appsv1.Deployment, container corev1.Container) error {
	podSpec := &deploy.Spec.Template.Spec
	containerIndex := slices.IndexFunc(podSpec.Containers, func(item corev1.Container) bool { return item.Name == "gateway" })
	if containerIndex == -1 {
		return errors.New("vpc egress gateway workload container not found")
	}
	initIndex := slices.IndexFunc(podSpec.InitContainers, func(item corev1.Container) bool { return item.Name == "init" })
	if initIndex == -1 || len(podSpec.InitContainers[initIndex].Command) < 3 {
		return errors.New("vpc egress gateway init container is invalid")
	}

	podSpec.Containers[containerIndex] = container
	podSpec.InitContainers[initIndex].Command[2] = vegBFDInitCommand
	podSpec.InitContainers[initIndex].VolumeMounts = append(podSpec.InitContainers[initIndex].VolumeMounts, corev1.VolumeMount{
		Name:      vegBFDDStateVolume,
		MountPath: vegBFDDStateDir,
	})
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name:     vegBFDDStateVolume,
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	})
	deploy.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To[int64](30)
	return nil
}

func setVpcEgressGatewayWorkloadResources(deploy *appsv1.Deployment, resources corev1.ResourceRequirements) error {
	containers := deploy.Spec.Template.Spec.Containers
	containerIndex := slices.IndexFunc(containers, func(item corev1.Container) bool {
		return item.Name == "gateway" || item.Name == "bfdd"
	})
	if containerIndex == -1 {
		return errors.New("vpc egress gateway workload container not found")
	}
	containers[containerIndex].Resources = resources
	return nil
}

func vpcEgressGatewayInitContainerFRRConfig(image string, bgpConf *kubeovnv1.BgpConf, evpnConf *kubeovnv1.EvpnConf) corev1.Container {
	env := []corev1.EnvVar{
		{
			Name:  "LOCAL_ASN",
			Value: strconv.FormatUint(uint64(bgpConf.Spec.LocalASN), 10),
		},
		{
			Name:  "PEER_ASN",
			Value: strconv.FormatUint(uint64(bgpConf.Spec.PeerASN), 10),
		},
		{
			Name:  "ROUTER_ID",
			Value: bgpConf.Spec.RouterID,
		},
		{
			Name:  "NEIGHBOURS",
			Value: strings.Join(bgpConf.Spec.Neighbours, ","),
		},
	}

	if bgpConf.Spec.Password != "" {
		env = append(env, corev1.EnvVar{
			Name:  "BGP_PASSWORD",
			Value: bgpConf.Spec.Password,
		})
	}

	if bgpConf.Spec.HoldTime != (metav1.Duration{}) {
		env = append(env, corev1.EnvVar{
			Name:  "BGP_HOLD_TIME",
			Value: formatDurationToSeconds(bgpConf.Spec.HoldTime),
		})
	}

	if bgpConf.Spec.KeepaliveTime != (metav1.Duration{}) {
		env = append(env, corev1.EnvVar{
			Name:  "BGP_KEEPALIVE_TIME",
			Value: formatDurationToSeconds(bgpConf.Spec.KeepaliveTime),
		})
	}

	if bgpConf.Spec.ConnectTime != (metav1.Duration{}) {
		env = append(env, corev1.EnvVar{
			Name:  "BGP_CONNECT_TIME",
			Value: formatDurationToSeconds(bgpConf.Spec.ConnectTime),
		})
	}

	if bgpConf.Spec.EbgpMultiHop {
		env = append(env, corev1.EnvVar{
			Name:  "BGP_EBGP_MULTIHOP",
			Value: "true",
		})
	}

	if evpnConf != nil {
		env = append(
			env,
			corev1.EnvVar{
				Name:  "VNI",
				Value: strconv.FormatUint(uint64(evpnConf.Spec.VNI), 10),
			},
			corev1.EnvVar{
				Name:  "ROUTE_TARGETS",
				Value: strings.Join(evpnConf.Spec.RouteTargets, ","),
			},
		)
	}

	return corev1.Container{
		Name:            "frr-config",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			"/kube-ovn/kube-ovn-cmd",
			"frr-render",
		},
		Env: env,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "frr-config",
				MountPath: "/etc/frr",
			},
		},
	}
}

func formatDurationToSeconds(d metav1.Duration) string {
	seconds := min(max(int64(d.Seconds()), 0), 65535)
	return fmt.Sprintf("%ds", seconds)
}

func vpcEgressGatewayContainerFRR(image string) corev1.Container {
	return corev1.Container{
		Name:            "frr",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			"bash",
			"-c",
			"/usr/lib/frr/docker-start & until [[ -f /etc/frr/frr.log ]]; do sleep 1; done; tail -f /etc/frr/frr.log",
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE", "SYS_ADMIN"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "frr-config",
				MountPath: "/etc/frr",
			},
		},
	}
}

func (c *Controller) handleDelVpcEgressGateway(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.vpcEgressGatewayKeyMutex.LockKey(key)
	defer func() { _ = c.vpcEgressGatewayKeyMutex.UnlockKey(key) }()

	cachedGateway, err := c.vpcEgressGatewayLister.VpcEgressGateways(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get vpc-egress-gateway %s: %w", key, err)
			klog.Error(err)
			return c.recordVpcEgressGatewayKeyError(ns, name, "GetVpcEgressGatewayFailed", err)
		}
		return nil
	}

	klog.Infof("handle deleting vpc-egress-gateway %s", key)
	if err = c.cleanOVNForVpcEgressGateway(key, cachedGateway.Spec.VPC); err != nil {
		klog.Error(err)
		return c.recordVpcEgressGatewayError(cachedGateway, "DeleteFailed", err)
	}

	gw := cachedGateway.DeepCopy()
	if controllerutil.RemoveFinalizer(gw, util.KubeOVNControllerFinalizer) {
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcEgressGateways(gw.Namespace).
			Update(context.Background(), gw, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to remove finalizer from vpc-egress-gateway %s: %w", key, err)
			klog.Error(err)
			return c.recordVpcEgressGatewayError(gw, "DeleteFailed", err)
		}
		c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeNormal, "DeleteSuccess", fmt.Sprintf("VpcEgressGateway %s/%s deleted successfully", gw.Namespace, gw.Name))
	}

	return nil
}

func (c *Controller) cleanOVNForVpcEgressGateway(key, lrName string) error {
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: key,
	}

	bfdList, err := c.findBFD(externalIDs)
	if err != nil {
		klog.Error(err)
		return err
	}
	for _, bfd := range bfdList {
		if err = c.deleteBFD(bfd.UUID); err != nil {
			klog.Error(err)
			return err
		}
	}

	if lrName == "" {
		lrName = c.config.ClusterRouter
	}
	if err = c.OVNNbClient.DeleteLogicalRouterPolicies(lrName, -1, externalIDs); err != nil {
		klog.Error(err)
		return err
	}
	if err = c.deletePortGroups(vegPortGroupName(key)); err != nil {
		klog.Error(err)
		return err
	}
	for _, af := range [...]int{4, 6} {
		if err = c.deleteAddressSets(vegAddressSetName(key, af)); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.deleteAddressSets(vegPortGroupAddressSetName(key, af)); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func vegPortGroupName(key string) string {
	hash := util.Sha256Hash([]byte(key))
	return "VEG." + hash[:12]
}

func vegPortGroupAddressSetName(key string, af int) string {
	return fmt.Sprintf("%s_ip%d", vegPortGroupName(key), af)
}

func vegAddressSetName(key string, af int) string {
	hash := util.Sha256Hash([]byte(key))
	return fmt.Sprintf("VEG.%s.ipv%d", hash[:12], af)
}

func (c *Controller) handlePodEventForVpcEgressGateway(pod *corev1.Pod) error {
	if vegName := pod.Labels[util.VpcEgressGatewayLabel]; pod.Labels["app"] == "vpc-egress-gateway" && vegName != "" {
		gateways, err := c.vpcEgressGatewayLister.VpcEgressGateways(pod.Namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list vpc egress gateways in namespace %s: %v", pod.Namespace, err)
			utilruntime.HandleError(err)
			return err
		}
		for _, veg := range gateways {
			if util.NormalizeLabelValue(veg.Name) == vegName {
				key := cache.MetaObjectToName(veg).String()
				klog.V(3).Infof("enqueue update vpc-egress-gateway %s for workload pod %s/%s", key, pod.Namespace, pod.Name)
				c.addOrUpdateVpcEgressGatewayQueue.Add(key)
				return nil
			}
		}
	}

	if !pod.DeletionTimestamp.IsZero() || pod.Annotations[util.AllocatedAnnotation] != "true" {
		return nil
	}
	vpc := pod.Annotations[util.LogicalRouterAnnotation]
	if vpc == "" {
		return nil
	}

	ns, err := c.namespacesLister.Get(pod.Namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get namespace %s: %v", pod.Namespace, err)
		utilruntime.HandleError(err)
		return err
	}

	gateways, err := c.vpcEgressGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc egress gateways: %v", err)
		utilruntime.HandleError(err)
		return err
	}

	for _, veg := range gateways {
		if veg.VPC(c.config.ClusterRouter) != vpc {
			continue
		}

		for _, selector := range veg.Spec.Selectors {
			if selector.NamespaceSelector != nil && !util.ObjectMatchesLabelSelector(ns, selector.NamespaceSelector) {
				continue
			}
			if selector.PodSelector != nil && !util.ObjectMatchesLabelSelector(pod, selector.PodSelector) {
				continue
			}
			c.addOrUpdateVpcEgressGatewayQueue.Add(cache.MetaObjectToName(veg).String())
		}
	}
	return nil
}
