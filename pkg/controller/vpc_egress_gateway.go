package controller

import (
	"context"
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
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcEgressGateway)).String()
	klog.V(3).Infof("enqueue delete vpc-egress-gateway %s", key)
	c.delVpcEgressGatewayQueue.Add(key)
}

func vegWorkloadLabels(vegName string) map[string]string {
	return map[string]string{"app": "vpc-egress-gateway", util.VpcEgressGatewayLabel: vegName}
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
			return err
		}
		return nil
	}

	if !cachedGateway.DeletionTimestamp.IsZero() {
		c.delVpcEgressGatewayQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling vpc-egress-gateway %s", key)
	gw := cachedGateway.DeepCopy()
	if gw, err = c.initVpcEgressGatewayStatus(gw); err != nil {
		return err
	}

	vpcName := gw.Spec.VPC
	if vpcName == "" {
		vpcName = c.config.ClusterRouter
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		klog.Error(err)
		return err
	}
	if gw.Spec.BFD.Enabled && vpc.Status.BFDPort.IP == "" {
		err = fmt.Errorf("vpc %s bfd port is not enabled or not ready", vpc.Name)
		klog.Error(err)
		gw.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "VpcBfdPortNotEnabled", err.Error(), gw.Generation)
		_, _ = c.updateVpcEgressGatewayStatus(gw)
		return err
	}

	if controllerutil.AddFinalizer(gw, util.KubeOVNControllerFinalizer) {
		updatedGateway, err := c.config.KubeOvnClient.KubeovnV1().VpcEgressGateways(gw.Namespace).
			Update(context.Background(), gw, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for vpc-egress-gateway %s/%s: %w", gw.Namespace, gw.Name, err)
			klog.Error(err)
			return err
		}
		gw = updatedGateway
	}

	var bfdIP, bfdIPv4, bfdIPv6 string
	if gw.Spec.BFD.Enabled {
		bfdIP = vpc.Status.BFDPort.IP
		bfdIPv4, bfdIPv6 = util.SplitStringIP(bfdIP)
	}

	// reconcile the vpc egress gateway workload and get the route sources for later OVN resources reconciliation
	attachmentNetworkName, ipv4Src, ipv6Src, deploy, err := c.reconcileVpcEgressGatewayWorkload(gw, vpc, bfdIP, bfdIPv4, bfdIPv6)
	gw.Status.Replicas = gw.Spec.Replicas
	gw.Status.LabelSelector = labels.FormatLabels(vegWorkloadLabels(gw.Name))
	if err != nil {
		klog.Error(err)
		gw.Status.Replicas = 0
		gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "ReconcileWorkloadFailed", err.Error(), gw.Generation)
		_, _ = c.updateVpcEgressGatewayStatus(gw)
		return err
	}

	var podIPs []string
	gw.Status.InternalIPs = nil
	gw.Status.ExternalIPs = nil
	gw.Status.Workload.APIVersion = deploy.APIVersion
	gw.Status.Workload.Kind = deploy.Kind
	gw.Status.Workload.Name = deploy.Name
	gw.Status.Workload.Nodes = nil
	if !util.DeploymentIsReady(deploy) {
		gw.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", msg, gw.Generation)
	} else {
		// get the pods of the deployment to collect the pod IPs
		podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
		if err != nil {
			err = fmt.Errorf("failed to get pod selector of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return err
		}

		pods, err := c.podsLister.Pods(deploy.Namespace).List(podSelector)
		if err != nil {
			err = fmt.Errorf("failed to list pods of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return err
		}

		// update gateway status including the internal/external IPs and the nodes where the pods are running
		gw.Status.Workload.Nodes = make([]string, 0, len(pods))
		for _, pod := range pods {
			gw.Status.Workload.Nodes = append(gw.Status.Workload.Nodes, pod.Spec.NodeName)
			ips := util.PodIPs(*pod)
			podIPs = append(podIPs, ips...)
			gw.Status.InternalIPs = append(gw.Status.InternalIPs, strings.Join(ips, ","))
			extIPs, err := util.PodAttachmentIPs(pod, attachmentNetworkName)
			if err != nil {
				klog.Error(err)
				gw.Status.ExternalIPs = append(gw.Status.ExternalIPs, "<unknown>")
				continue
			}
			gw.Status.ExternalIPs = append(gw.Status.ExternalIPs, strings.Join(extIPs, ","))
		}
	}
	if gw, err = c.updateVpcEgressGatewayStatus(gw); err != nil {
		klog.Error(err)
		return err
	}
	if len(gw.Status.Workload.Nodes) == 0 {
		// the workload is not ready yet
		return nil
	}

	// reconcile OVN routes
	nextHopsIPv4, hextHopsIPv6 := util.SplitIpsByProtocol(podIPs)
	if err = c.reconcileVpcEgressGatewayOVNRoutes(gw, 4, vpc.Status.Router, vpc.Status.BFDPort.Name, bfdIPv4, set.New(nextHopsIPv4...), ipv4Src); err != nil {
		klog.Error(err)
		return err
	}
	if err = c.reconcileVpcEgressGatewayOVNRoutes(gw, 6, vpc.Status.Router, vpc.Status.BFDPort.Name, bfdIPv6, set.New(hextHopsIPv6...), ipv6Src); err != nil {
		klog.Error(err)
		return err
	}

	gw.Status.Ready = true
	gw.Status.Phase = kubeovnv1.PhaseCompleted
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)
	if _, err = c.updateVpcEgressGatewayStatus(gw); err != nil {
		return err
	}

	return nil
}

func (c *Controller) initVpcEgressGatewayStatus(gw *kubeovnv1.VpcEgressGateway) (*kubeovnv1.VpcEgressGateway, error) {
	var err error
	if gw.Status.Phase == "" || gw.Status.Phase == kubeovnv1.PhasePending {
		gw.Status.Phase = kubeovnv1.PhaseProcessing
		gw, err = c.updateVpcEgressGatewayStatus(gw)
	}
	return gw, err
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
	if _, err = c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(nadNamespace).
		Get(context.Background(), nadName, metav1.GetOptions{}); err != nil {
		klog.Errorf("failed to get net-attach-def %s/%s: %v", nadNamespace, nadName, err)
		return "", nil, nil, nil, err
	}
	attachmentNetworkName := fmt.Sprintf("%s/%s", nadNamespace, nadName)

	// collect egress policies
	ipv4ForwardSrc, ipv6ForwardSrc := set.New[string](), set.New[string]()
	ipv4SNATSrc, ipv6SNATSrc := set.New[string](), set.New[string]()
	for _, policy := range gw.Spec.Policies {
		ipv4, ipv6 := util.SplitIpsByProtocol(policy.IPBlocks)
		if policy.SNAT {
			ipv4SNATSrc.Insert(ipv4...)
			ipv6SNATSrc.Insert(ipv6...)
		} else {
			ipv4ForwardSrc.Insert(ipv4...)
			ipv6ForwardSrc.Insert(ipv6...)
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
	intRouteDstIPv4, intRouteDstIPv6 := ipv4ForwardSrc.Union(ipv4SNATSrc), ipv6ForwardSrc.Union(ipv6SNATSrc)
	// intRouteDstIPv4.Insert(bfdIPv4)
	// intRouteDstIPv6.Insert(bfdIPv6)
	intRouteDstIPv4.Delete("")
	intRouteDstIPv6.Delete("")
	ipv4ForwardSrc.Delete("")
	ipv6ForwardSrc.Delete("")

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
	}
	if len(gw.Spec.ExternalIPs) != 0 {
		// set external IPs
		annotations[fmt.Sprintf(util.IPPoolAnnotationTemplate, extSubnet.Spec.Provider)] = strings.Join(gw.Spec.ExternalIPs, ";")
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

	// generate workload
	labels := vegWorkloadLabels(gw.Name)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Spec.Prefix + gw.Name,
			Namespace: gw.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: ptr.To(intstr.FromInt(1)),
					MaxSurge:       ptr.To(intstr.FromInt(1)),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: mergeNodeSelector(gw.Spec.NodeSelector),
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: labels,
								},
								TopologyKey: "kubernetes.io/hostname",
							}},
						},
					},
					InitContainers: []corev1.Container{{
						Name:            "init",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"bash", "/kube-ovn/init-vpc-egress-gateway.sh"},
						Env:             initEnv,
						SecurityContext: &corev1.SecurityContext{
							Privileged: ptr.To(true),
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "usr-local-sbin",
							MountPath: "/usr/local/sbin",
						}},
					}},
					Containers: []corev1.Container{{
						Name:            "gateway",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"sleep", "infinity"},
						SecurityContext: &corev1.SecurityContext{
							Privileged: ptr.To(false),
							RunAsUser:  ptr.To[int64](65534),
							Capabilities: &corev1.Capabilities{
								Add:  []corev1.Capability{"NET_ADMIN", "NET_RAW"},
								Drop: []corev1.Capability{"ALL"},
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "usr-local-sbin",
							MountPath: "/usr/local/sbin",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "usr-local-sbin",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
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
		// run BFD in the gateway container	to establish BFD session(s) with the VPC BFD LRP
		container := vpcEgressGatewayContainerBFDD(image, bfdIP, gw.Spec.BFD.MinTX, gw.Spec.BFD.MinRX, gw.Spec.BFD.Multiplier)
		deploy.Spec.Template.Spec.Containers[0] = container
	}

	// generate hash for the workload to determine whether to update the existing workload or not
	hash, err := util.Sha256HashObject(deploy)
	if err != nil {
		err = fmt.Errorf("failed to hash generated deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}

	hash = hash[:12]
	// replicas and the hash annotation should be excluded from hash calculation
	deploy.Spec.Replicas = ptr.To(gw.Spec.Replicas)
	deploy.Annotations = map[string]string{util.GenerateHashAnnotation: hash}
	if currentDeploy, err := c.deploymentsLister.Deployments(gw.Namespace).Get(deploy.Name); err != nil {
		if !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		if deploy, err = c.config.KubeClient.AppsV1().Deployments(gw.Namespace).
			Create(context.Background(), deploy, metav1.CreateOptions{}); err != nil {
			err = fmt.Errorf("failed to create deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
	} else if !reflect.DeepEqual(currentDeploy.Spec.Replicas, deploy.Spec.Replicas) ||
		currentDeploy.Annotations[util.GenerateHashAnnotation] != hash {
		// update the deployment if replicas or hash annotation is changed
		if deploy, err = c.config.KubeClient.AppsV1().Deployments(gw.Namespace).
			Update(context.Background(), deploy, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to update deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
	} else {
		// no need to create or update the deployment
		deploy = currentDeploy
	}

	// return the source CIDR blocks for later OVN resources reconciliation
	deploy.APIVersion, deploy.Kind = deploymentGroupVersion, deploymentKind
	return attachmentNetworkName, intRouteDstIPv4, intRouteDstIPv6, deploy, nil
}

func (c *Controller) reconcileVpcEgressGatewayOVNRoutes(gw *kubeovnv1.VpcEgressGateway, af int, lrName, lrpName, bfdIP string, nextHops, sources set.Set[string]) error {
	if nextHops.Len() == 0 {
		return nil
	}

	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: fmt.Sprintf("%s/%s", gw.Namespace, gw.Name),
		"af":                           strconv.Itoa(af),
	}
	bfdList, err := c.OVNNbClient.FindBFD(externalIDs)
	if err != nil {
		klog.Error(err)
		return err
	}

	// reconcile OVN port group
	ports := set.New[string]()
	for _, selector := range gw.Spec.Selectors {
		sel, err := metav1.LabelSelectorAsSelector(selector.NamespaceSelector)
		if err != nil {
			err = fmt.Errorf("failed to create label selector for namespace selector %#v: %w", selector.NamespaceSelector, err)
			klog.Error(err)
			return err
		}
		namespaces, err := c.namespacesLister.List(sel)
		if err != nil {
			err = fmt.Errorf("failed to list namespaces with selector %s: %w", sel, err)
			klog.Error(err)
			return err
		}
		if sel, err = metav1.LabelSelectorAsSelector(selector.PodSelector); err != nil {
			err = fmt.Errorf("failed to create label selector for pod selector %#v: %w", selector.PodSelector, err)
			klog.Error(err)
			return err
		}
		for _, ns := range namespaces {
			pods, err := c.podsLister.Pods(ns.Name).List(sel)
			if err != nil {
				err = fmt.Errorf("failed to list pods with selector %s in namespace %s: %w", sel, ns.Name, err)
				klog.Error(err)
				return err
			}
			for _, pod := range pods {
				if pod.Spec.HostNetwork || !isPodAlive(pod) {
					continue
				}
				if pod.Annotations[util.LogicalRouterAnnotation] != c.config.ClusterRouter ||
					pod.Annotations[util.AllocatedAnnotation] != "true" {
					continue
				}
				podName := c.getNameByPod(pod)
				ports.Insert(ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider))
			}
		}
	}
	hash := util.Sha256Hash([]byte(cache.MetaObjectToName(gw).String()))
	pgName := "VEG." + hash[:12]
	if err = c.OVNNbClient.CreatePortGroup(pgName, externalIDs); err != nil {
		err = fmt.Errorf("failed to create port group %s: %w", pgName, err)
		klog.Error(err)
		return err
	}
	if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports.UnsortedList()); err != nil {
		err = fmt.Errorf("failed to set ports of port group %s: %w", pgName, err)
		klog.Error(err)
		return err
	}

	// reconcile OVN address set
	asName := pgName
	if err = c.OVNNbClient.CreateAddressSet(asName, externalIDs); err != nil {
		err = fmt.Errorf("failed to create address set %s: %w", asName, err)
		klog.Error(err)
		return err
	}
	if err = c.OVNNbClient.AddressSetUpdateAddress(asName, sources.SortedList()...); err != nil {
		err = fmt.Errorf("failed to update address set %s: %w", asName, err)
		klog.Error(err)
		return err
	}

	// reconcile OVN BFD entries
	bfdIDs := set.New[string]()
	bfdDstIPs := nextHops.Clone()
	bfdMap := make(map[string]*string, nextHops.Len())
	for _, bfd := range bfdList {
		if bfdIP == "" || bfd.LogicalPort != lrpName || !bfdDstIPs.Has(bfd.DstIP) {
			if err = c.OVNNbClient.DeleteBFD(bfd.UUID); err != nil {
				err = fmt.Errorf("failed to delete bfd %s: %w", bfd.UUID, err)
				klog.Error(err)
				return err
			}
		}
		if bfdIP == "" || bfd.LogicalPort == lrpName && bfdDstIPs.Has(bfd.DstIP) {
			// TODO: update min_rx, min_tx and multiplier
			if bfdIP != "" {
				bfdIDs.Insert(bfd.UUID)
				bfdMap[bfd.DstIP] = ptr.To(bfd.UUID)
			}
			bfdDstIPs.Delete(bfd.DstIP)
		}
	}
	if bfdIP != "" {
		for _, dstIP := range bfdDstIPs.UnsortedList() {
			bfd, err := c.OVNNbClient.CreateBFD(lrpName, dstIP, int(gw.Spec.BFD.MinRX), int(gw.Spec.BFD.MinTX), int(gw.Spec.BFD.Multiplier), externalIDs)
			if err != nil {
				klog.Error(err)
				return err
			}
			bfdIDs.Insert(bfd.UUID)
			bfdMap[bfd.DstIP] = ptr.To(bfd.UUID)
		}
	}

	// reconcile LR policy
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(lrName, util.EgressGatewayPolicyPriority, externalIDs, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	matches := set.New(
		fmt.Sprintf("ip%d.src == $%s_ip%d", af, pgName, af),
		fmt.Sprintf("ip%d.src == $%s", af, asName),
	)
	for _, policy := range policies {
		if matches.Has(policy.Match) {
			if !nextHops.Equal(set.New(policy.Nexthops...)) || !bfdIDs.Equal(set.New(policy.BFDSessions...)) {
				policy.Nexthops, policy.BFDSessions = nextHops.UnsortedList(), bfdIDs.UnsortedList()
				if err = c.OVNNbClient.UpdateLogicalRouterPolicy(policy, &policy.Nexthops, &policy.BFDSessions); err != nil {
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
	bfdSessions := bfdIDs.UnsortedList()
	for _, match := range matches.UnsortedList() {
		if err := c.OVNNbClient.AddLogicalRouterPolicy(lrName, util.EgressGatewayPolicyPriority, match,
			ovnnb.LogicalRouterPolicyActionReroute, nextHops.UnsortedList(), bfdSessions, externalIDs); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
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

func vpcEgressGatewayContainerBFDD(image, bfdIP string, minTX, minRX, multiplier int32) corev1.Container {
	return corev1.Container{
		Name:            "bfdd",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"bash", "/kube-ovn/start-bfdd.sh"},
		Env: []corev1.EnvVar{{
			Name: "POD_IPS",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIPs",
				},
			},
		}, {
			Name:  "BFD_PEER_IPS",
			Value: bfdIP,
		}, {
			Name:  "BFD_MIN_TX",
			Value: strconv.Itoa(int(minTX)),
		}, {
			Name:  "BFD_MIN_RX",
			Value: strconv.Itoa(int(minRX)),
		}, {
			Name:  "BFD_MULTI",
			Value: strconv.Itoa(int(multiplier)),
		}},
		// wait for the BFD process to be running and initialize the BFD configuration
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bash", "/kube-ovn/bfdd-prestart.sh"},
				},
			},
			InitialDelaySeconds: 1,
			FailureThreshold:    1,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bfdd-control", "status"},
				},
			},
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bfdd-control", "status"},
				},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       3,
			FailureThreshold:    1,
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(false),
			RunAsUser:  ptr.To[int64](65534),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "usr-local-sbin",
			MountPath: "/usr/local/sbin",
		}},
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
			return err
		}
		return nil
	}

	klog.Infof("handle deleting vpc-egress-gateway %s", key)
	if err = c.cleanOVNforVpcEgressGateway(key, cachedGateway.Spec.VPC); err != nil {
		klog.Error(err)
		return err
	}

	gw := cachedGateway.DeepCopy()
	if controllerutil.RemoveFinalizer(gw, util.KubeOVNControllerFinalizer) {
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcEgressGateways(gw.Namespace).
			Update(context.Background(), gw, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to remove finalizer from vpc-egress-gateway %s: %w", key, err)
			klog.Error(err)
		}
	}

	return nil
}

func (c *Controller) cleanOVNforVpcEgressGateway(key, lrName string) error {
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: key,
	}

	bfdList, err := c.OVNNbClient.FindBFD(externalIDs)
	if err != nil {
		klog.Error(err)
		return err
	}
	for _, bfd := range bfdList {
		if err = c.OVNNbClient.DeleteBFD(bfd.UUID); err != nil {
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
	if err = c.OVNNbClient.DeleteLogicalRouterStaticRouteByExternalIDs(lrName, externalIDs); err != nil {
		klog.Error(err)
		return err
	}
	hash := util.Sha256Hash([]byte(key))
	pgName := "VEG." + hash[:12]
	if err = c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Error(err)
		return err
	}
	asName := pgName
	if err = c.OVNNbClient.DeleteAddressSet(asName); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}
