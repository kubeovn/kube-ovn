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

func (c *Controller) enqueueAddBgpEdgeRouter(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.BgpEdgeRouter)).String()
	klog.V(3).Infof("enqueue add bgp-edge-router %s", key)
	c.addOrUpdateBgpEdgeRouterQueue.Add(key)
}

func (c *Controller) enqueueUpdateBgpEdgeRouter(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.BgpEdgeRouter)).String()
	klog.V(3).Infof("enqueue update bgp-edge-router %s", key)
	c.addOrUpdateBgpEdgeRouterQueue.Add(key)
}

func (c *Controller) enqueueDeleteBgpEdgeRouter(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.BgpEdgeRouter)).String()
	klog.V(3).Infof("enqueue delete bgp-edge-router %s", key)
	c.delBgpEdgeRouterQueue.Add(key)
}

func bgpEdgeRouterWorkloadLabels(bgpEdgeRouterName string) map[string]string {
	return map[string]string{"app": "bgp-edge-router", util.BgpEdgeRouterLabel: bgpEdgeRouterName}
}

func (c *Controller) handleAddOrUpdateBgpEdgeRouter(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.bgpEdgeRouterKeyMutex.LockKey(key)
	defer func() { _ = c.bgpEdgeRouterKeyMutex.UnlockKey(key) }()

	cachedRouter, err := c.bgpEdgeRouterLister.BgpEdgeRouters(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedRouter.DeletionTimestamp.IsZero() {
		c.delBgpEdgeRouterQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling bgp-edge-router %s", key)
	router := cachedRouter.DeepCopy()
	if router, err = c.initBgpEdgeRouterStatus(router); err != nil {
		return err
	}

	vpcName := router.Spec.VPC
	if vpcName == "" {
		vpcName = c.config.ClusterRouter
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		klog.Error(err)
		return err
	}
	if router.Spec.BFD.Enabled && vpc.Status.BFDPort.IP == "" {
		err = fmt.Errorf("vpc %s bfd port is not enabled or not ready", vpc.Name)
		klog.Error(err)
		router.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "VpcBfdPortNotEnabled", err.Error(), router.Generation)
		_, _ = c.updatebgpEdgeRouterStatus(router)
		return err
	}

	if controllerutil.AddFinalizer(router, util.KubeOVNControllerFinalizer) {
		updatedGateway, err := c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouters(router.Namespace).
			Update(context.Background(), router, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for bgp-edge-router %s/%s: %w", router.Namespace, router.Name, err)
			klog.Error(err)
			return err
		}
		router = updatedGateway
	}

	var bfdIP, bfdIPv4, bfdIPv6 string
	if router.Spec.BFD.Enabled {
		bfdIP = vpc.Status.BFDPort.IP
		bfdIPv4, bfdIPv6 = util.SplitStringIP(bfdIP)
	}

	// reconcile the bgp edge router workload and get the route sources for later OVN resources reconciliation
	attachmentNetworkName, ipv4Src, ipv6Src, deploy, err := c.reconcileBgpEdgeRouterWorkload(router, vpc, bfdIP, bfdIPv4, bfdIPv6)
	router.Status.Replicas = router.Spec.Replicas
	router.Status.LabelSelector = labels.FormatLabels(bgpEdgeRouterWorkloadLabels(router.Name))
	if err != nil {
		klog.Error(err)
		router.Status.Replicas = 0
		router.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "ReconcileWorkloadFailed", err.Error(), router.Generation)
		_, _ = c.updatebgpEdgeRouterStatus(router)
		return err
	}

	router.Status.InternalIPs = nil
	router.Status.ExternalIPs = nil
	router.Status.Workload.APIVersion = deploy.APIVersion
	router.Status.Workload.Kind = deploy.Kind
	router.Status.Workload.Name = deploy.Name
	router.Status.Workload.Nodes = nil
	nodeNexthopIPv4 := make(map[string]string, int(router.Spec.Replicas))
	nodeNexthopIPv6 := make(map[string]string, int(router.Spec.Replicas))
	ready := util.DeploymentIsReady(deploy)
	if !ready {
		router.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		router.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", msg, router.Generation)
	}
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

	// update router status including the internal/external IPs and the nodes where the pods are running
	router.Status.Workload.Nodes = make([]string, 0, len(pods))
	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		extIPs, err := util.PodAttachmentIPs(pod, attachmentNetworkName)
		if err != nil {
			klog.Error(err)
			continue
		}

		ips := util.PodIPs(*pod)
		ipv4, ipv6 := util.SplitIpsByProtocol(ips)
		if len(ipv4) != 0 {
			nodeNexthopIPv4[pod.Spec.NodeName] = ipv4[0]
		}
		if len(ipv6) != 0 {
			nodeNexthopIPv6[pod.Spec.NodeName] = ipv6[0]
		}
		router.Status.InternalIPs = append(router.Status.InternalIPs, strings.Join(ips, ","))
		router.Status.ExternalIPs = append(router.Status.ExternalIPs, strings.Join(extIPs, ","))
		router.Status.Workload.Nodes = append(router.Status.Workload.Nodes, pod.Spec.NodeName)
	}
	if router, err = c.updatebgpEdgeRouterStatus(router); err != nil {
		klog.Error(err)
		return err
	}

	// reconcile OVN routes
	if err = c.reconcileBgpEdgeRouterOVNRoutes(router, 4, vpc.Status.Router, vpc.Status.BFDPort.Name, bfdIPv4, nodeNexthopIPv4, ipv4Src); err != nil {
		klog.Error(err)
		return err
	}
	if err = c.reconcileBgpEdgeRouterOVNRoutes(router, 6, vpc.Status.Router, vpc.Status.BFDPort.Name, bfdIPv6, nodeNexthopIPv6, ipv6Src); err != nil {
		klog.Error(err)
		return err
	}

	if ready {
		router.Status.Ready = true
		router.Status.Phase = kubeovnv1.PhaseCompleted
		router.Status.Conditions.SetReady("ReconcileSuccess", router.Generation)
		if _, err = c.updatebgpEdgeRouterStatus(router); err != nil {
			return err
		}
	}

	klog.Infof("finished reconciling bgp-edge-router %s", key)

	return nil
}

func (c *Controller) initBgpEdgeRouterStatus(router *kubeovnv1.BgpEdgeRouter) (*kubeovnv1.BgpEdgeRouter, error) {
	var err error
	if router.Status.Phase == "" || router.Status.Phase == kubeovnv1.PhasePending {
		router.Status.Phase = kubeovnv1.PhaseProcessing
		router, err = c.updatebgpEdgeRouterStatus(router)
	}
	return router, err
}

func (c *Controller) updatebgpEdgeRouterStatus(router *kubeovnv1.BgpEdgeRouter) (*kubeovnv1.BgpEdgeRouter, error) {
	if len(router.Status.Conditions) == 0 {
		router.Status.Conditions.SetCondition(kubeovnv1.Init, corev1.ConditionUnknown, "Processing", "", router.Generation)
	}
	if !router.Status.Ready {
		router.Status.Phase = kubeovnv1.PhaseProcessing
	}

	updateRouter, err := c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouters(router.Namespace).
		UpdateStatus(context.Background(), router, metav1.UpdateOptions{})
	if err != nil {
		err = fmt.Errorf("failed to update status of bgp-edge-router %s/%s: %w", router.Namespace, router.Name, err)
		klog.Error(err)
		return nil, err
	}

	return updateRouter, nil
}

// create or update bgp edge router workload
func (c *Controller) reconcileBgpEdgeRouterWorkload(router *kubeovnv1.BgpEdgeRouter, vpc *kubeovnv1.Vpc, bfdIP, bfdIPv4, bfdIPv6 string) (string, set.Set[string], set.Set[string], *appsv1.Deployment, error) {
	image := c.config.Image
	bgpImage := c.config.Image
	if router.Spec.Image != "" {
		image = router.Spec.Image
	}
	if router.Spec.BGP.Image != "" {
		bgpImage = router.Spec.BGP.Image
	}
	if image == "" {
		err := fmt.Errorf("no image specified for bgp edge router %s/%s", router.Namespace, router.Name)
		klog.Error(err)
		return "", nil, nil, nil, err
	}

	if len(router.Spec.InternalIPs) != 0 && len(router.Spec.InternalIPs) < int(router.Spec.Replicas) {
		err := fmt.Errorf("internal IPs count %d is less than replicas %d", len(router.Spec.InternalIPs), router.Spec.Replicas)
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	if len(router.Spec.ExternalIPs) != 0 && len(router.Spec.ExternalIPs) < int(router.Spec.Replicas) {
		err := fmt.Errorf("external IPs count %d is less than replicas %d", len(router.Spec.ExternalIPs), router.Spec.Replicas)
		klog.Error(err)
		return "", nil, nil, nil, err
	}

	internalSubnet := router.Spec.InternalSubnet
	if internalSubnet == "" {
		internalSubnet = vpc.Status.DefaultLogicalSwitch
	}
	if internalSubnet == "" {
		err := fmt.Errorf("default subnet of vpc %s not found, please set internal subnet of the bgp edge router", vpc.Name)
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	intSubnet, err := c.subnetsLister.Get(internalSubnet)
	if err != nil {
		klog.Error(err)
		return "", nil, nil, nil, err
	}
	extSubnet, err := c.subnetsLister.Get(router.Spec.ExternalSubnet)
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

	// collect egress policies
	ipv4ForwardSrc, ipv6ForwardSrc := set.New[string](), set.New[string]()
	ipv4SNATSrc, ipv6SNATSrc := set.New[string](), set.New[string]()
	for _, policy := range router.Spec.Policies {
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
	intRouteDstIPv4.Delete("")
	intRouteDstIPv6.Delete("")
	ipv4ForwardSrc.Delete("")
	ipv6ForwardSrc.Delete("")

	// generate route annotations used to configure routes in the pod
	routes := util.NewPodRoutes()
	intGatewayIPv4, intGatewayIPv6 := util.SplitStringIP(intSubnet.Spec.Gateway)
	extGatewayIPv4, extGatewayIPv6 := util.SplitStringIP(extSubnet.Spec.Gateway)
	// add routes for the VPC BFD Port so that the bgp edge router can establish BFD session(s) with it
	routes.Add(util.OvnProvider, bfdIPv4, intGatewayIPv4)
	routes.Add(util.OvnProvider, bfdIPv6, intGatewayIPv6)
	// add routes for the internal networks
	for _, dst := range intRouteDstIPv4.UnsortedList() {
		// skip the route to the internal subnet itself
		if intSubnet.Spec.CIDRBlock == dst {
			continue
		}
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
	if len(router.Spec.InternalIPs) != 0 {
		// set internal IPs
		annotations[util.IPPoolAnnotation] = strings.Join(router.Spec.InternalIPs, ";")
	}
	if len(router.Spec.ExternalIPs) != 0 {
		// set external IPs
		annotations[fmt.Sprintf(util.IPPoolAnnotationTemplate, extSubnet.Spec.Provider)] = strings.Join(router.Spec.ExternalIPs, ";")
	}

	// generate init container environment variables
	// the init container is responsible for adding routes and SNAT rules to the pod network namespace
	initEnv, err := bgpEdgeRouterInitContainerEnv(4, intGatewayIPv4, extGatewayIPv4, ipv4ForwardSrc)
	if err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}
	ipv6Env, err := bgpEdgeRouterInitContainerEnv(6, intGatewayIPv6, extGatewayIPv6, ipv6ForwardSrc)
	if err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}
	initEnv = append(initEnv, ipv6Env...)

	// generate workload
	labels := bgpEdgeRouterWorkloadLabels(router.Name)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      router.Spec.Prefix + router.Name,
			Namespace: router.Namespace,
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
					MaxSurge:       ptr.To(intstr.FromInt(0)),
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
							RequiredDuringSchedulingIgnoredDuringExecution: berMergeNodeSelector(router.Spec.NodeSelector),
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: labels,
								},
								TopologyKey: corev1.LabelHostname,
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
					Volumes: []corev1.Volume{
						{
							Name: "usr-local-sbin",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "kube-ovn-logs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					TerminationGracePeriodSeconds: ptr.To[int64](0),
				},
			},
		},
	}
	// set owner reference so that the workload will be deleted automatically when the bgp edge router is deleted
	if err = util.SetOwnerReference(router, deploy); err != nil {
		klog.Error(err)
		return attachmentNetworkName, nil, nil, nil, err
	}

	if bfdIP != "" {
		// run BFD in the router container	to establish BFD session(s) with the VPC BFD LRP
		container := bgpEdgeRouterContainerBFDD(image, bfdIP, router.Spec.BFD.MinTX, router.Spec.BFD.MinRX, router.Spec.BFD.Multiplier)
		deploy.Spec.Template.Spec.Containers[0] = container
	}

	// bgp sidecar container logic
	if router.Spec.BGP.Enabled {
		// run BGP in the router container
		bgpContainer, err := bgpEdgeRouterContainerBGP(bgpImage, router.Name, &router.Spec.BGP)
		if err != nil {
			klog.Errorf("failed to create a BGP speaker container for router %s: %v", router.Name, err)
			return "", nil, nil, nil, err
		}
		deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, *bgpContainer)
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
	deploy.Spec.Replicas = ptr.To(router.Spec.Replicas)
	deploy.Annotations = map[string]string{util.GenerateHashAnnotation: hash}

	if currentDeploy, err := c.berDeploymentsLister.Deployments(router.Namespace).Get(deploy.Name); err != nil {
		if !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
		if deploy, err = c.config.KubeClient.AppsV1().Deployments(router.Namespace).
			Create(context.Background(), deploy, metav1.CreateOptions{}); err != nil {
			err = fmt.Errorf("failed to create deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
			klog.Error(err)
			return attachmentNetworkName, nil, nil, nil, err
		}
	} else if !reflect.DeepEqual(currentDeploy.Spec.Replicas, deploy.Spec.Replicas) ||
		currentDeploy.Annotations[util.GenerateHashAnnotation] != hash {
		// update the deployment if replicas or hash annotation is changed
		if deploy, err = c.config.KubeClient.AppsV1().Deployments(router.Namespace).
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

func (c *Controller) reconcileBgpEdgeRouterOVNRoutes(router *kubeovnv1.BgpEdgeRouter, af int, lrName, lrpName, bfdIP string, nextHops map[string]string, sources set.Set[string]) error {
	if len(nextHops) == 0 {
		return nil
	}

	externalIDs := map[string]string{
		ovs.ExternalIDVendor:        util.CniTypeName,
		ovs.ExternalIDBgpEdgeRouter: fmt.Sprintf("%s/%s", router.Namespace, router.Name),
		"af":                        strconv.Itoa(af),
	}
	bfdList, err := c.OVNNbClient.FindBFD(externalIDs)
	if err != nil {
		klog.Error(err)
		return err
	}

	// reconcile OVN port group
	ports := set.New[string]()
	key := cache.MetaObjectToName(router).String()
	pgName := berPortGroupName(key)
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
	asName := berAddressSetName(key, af)
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
	staleBFDIDs := set.New[string]()
	bfdDstIPs := set.New(slices.Collect(maps.Values(nextHops))...)
	bfdMap := make(map[string]string, bfdDstIPs.Len())
	for _, bfd := range bfdList {
		if bfdIP == "" || bfd.LogicalPort != lrpName || !bfdDstIPs.Has(bfd.DstIP) {
			staleBFDIDs.Insert(bfd.UUID)
		}
		if bfdIP == "" || (bfd.LogicalPort == lrpName && bfdDstIPs.Has(bfd.DstIP)) {
			// TODO: update min_rx, min_tx and multiplier
			if bfdIP != "" {
				bfdIDs.Insert(bfd.UUID)
				bfdMap[bfd.DstIP] = bfd.UUID
			}
			bfdDstIPs.Delete(bfd.DstIP)
		}
	}
	if bfdIP != "" {
		for _, dstIP := range bfdDstIPs.UnsortedList() {
			bfd, err := c.OVNNbClient.CreateBFD(lrpName, dstIP, int(router.Spec.BFD.MinRX), int(router.Spec.BFD.MinTX), int(router.Spec.BFD.Multiplier), externalIDs)
			if err != nil {
				klog.Error(err)
				return err
			}
			bfdIDs.Insert(bfd.UUID)
			bfdMap[bfd.DstIP] = bfd.UUID
		}
	}

	// reconcile LR policy
	if router.Spec.TrafficPolicy == kubeovnv1.TrafficPolicyLocal {
		rules := make(map[string]string, len(nextHops))
		for nodeName, nexthop := range nextHops {
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
			rules[fmt.Sprintf("ip%d.src == $%s_ip%d && ip%d.src == $%s_ip%d", af, localPgName, af, af, pgName, af)] = nexthop
			rules[fmt.Sprintf("ip%d.src == $%s_ip%d && ip%d.src == $%s", af, localPgName, af, af, asName)] = nexthop
		}
		policies, err := c.OVNNbClient.ListLogicalRouterPolicies(lrName, util.EgressGatewayLocalPolicyPriority, externalIDs, false)
		if err != nil {
			klog.Error(err)
			return err
		}
		// update/delete existing policies
		for _, policy := range policies {
			nexthop := rules[policy.Match]
			bfdSessions := set.New(bfdMap[nexthop]).Delete("")
			if nexthop == "" {
				if err = c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
					err = fmt.Errorf("failed to delete ovn lr policy %q: %w", policy.Match, err)
					klog.Error(err)
					return err
				}
			} else {
				var changed bool
				if len(policy.Nexthops) != 1 || policy.Nexthops[0] != nexthop {
					policy.Nexthops = []string{nexthop}
					changed = true
				}
				if !bfdSessions.Equal(set.New(policy.BFDSessions...)) {
					policy.BFDSessions = bfdSessions.UnsortedList()
					changed = true
				}
				if changed {
					if err = c.OVNNbClient.UpdateLogicalRouterPolicy(policy, &policy.Nexthops, &policy.BFDSessions); err != nil {
						err = fmt.Errorf("failed to update logical router policy %s: %w", policy.UUID, err)
						klog.Error(err)
						return err
					}
				}
			}
			delete(rules, policy.Match)
		}
		// create new policies
		for match, nexthop := range rules {
			if err = c.OVNNbClient.AddLogicalRouterPolicy(lrName, util.EgressGatewayLocalPolicyPriority, match,
				ovnnb.LogicalRouterPolicyActionReroute, []string{nexthop}, []string{bfdMap[nexthop]}, externalIDs); err != nil {
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
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(lrName, util.EgressGatewayPolicyPriority, externalIDs, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	matches := set.New(
		fmt.Sprintf("ip%d.src == $%s_ip%d", af, pgName, af),
		fmt.Sprintf("ip%d.src == $%s", af, asName),
	)
	bfdIPs := set.New(slices.Collect(maps.Values(nextHops))...)
	bfdSessions := bfdIDs.UnsortedList()
	for _, policy := range policies {
		if matches.Has(policy.Match) {
			if !bfdIPs.Equal(set.New(policy.Nexthops...)) || !bfdIDs.Equal(set.New(policy.BFDSessions...)) {
				policy.Nexthops, policy.BFDSessions = bfdIPs.UnsortedList(), bfdSessions
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
	for _, match := range matches.UnsortedList() {
		if err = c.OVNNbClient.AddLogicalRouterPolicy(lrName, util.EgressGatewayPolicyPriority, match,
			ovnnb.LogicalRouterPolicyActionReroute, bfdIPs.UnsortedList(), bfdSessions, externalIDs); err != nil {
			klog.Error(err)
			return err
		}
	}

	if router.Spec.BFD.Enabled {
		// drop traffic if no nexthop is available
		if policies, err = c.OVNNbClient.ListLogicalRouterPolicies(lrName, util.EgressGatewayDropPolicyPriority, externalIDs, false); err != nil {
			klog.Error(err)
			return err
		}
		matches = set.New(
			fmt.Sprintf("ip%d.src == $%s_ip%d", af, pgName, af),
			fmt.Sprintf("ip%d.src == $%s", af, asName),
		)
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

	for _, bfdID := range staleBFDIDs.UnsortedList() {
		if err = c.OVNNbClient.DeleteBFD(bfdID); err != nil {
			err = fmt.Errorf("failed to delete bfd %s: %w", bfdID, err)
			klog.Error(err)
			return err
		}
	}

	return nil
}

func berMergeNodeSelector(nodeSelector []kubeovnv1.BgpEdgeRouterNodeSelector) *corev1.NodeSelector {
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

func bgpEdgeRouterInitContainerEnv(af int, internalGateway, externalGateway string, forwardSrc set.Set[string]) ([]corev1.EnvVar, error) {
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

func bgpEdgeRouterContainerBFDD(image, bfdIP string, minTX, minRX, multiplier int32) corev1.Container {
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

func (c *Controller) handleDelBgpEdgeRouter(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.bgpEdgeRouterKeyMutex.LockKey(key)
	defer func() { _ = c.bgpEdgeRouterKeyMutex.UnlockKey(key) }()

	cachedGateway, err := c.bgpEdgeRouterLister.BgpEdgeRouters(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get bgp-edge-router %s: %w", key, err)
			klog.Error(err)
			return err
		}
		return nil
	}

	klog.Infof("handle deleting bgp-edge-router %s", key)
	if err = c.cleanOVNForBgpEdgeRouter(key, cachedGateway.Spec.VPC); err != nil {
		klog.Error(err)
		return err
	}

	router := cachedGateway.DeepCopy()
	if controllerutil.RemoveFinalizer(router, util.KubeOVNControllerFinalizer) {
		if _, err = c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouters(router.Namespace).
			Update(context.Background(), router, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to remove finalizer from bgp-edge-router %s: %w", key, err)
			klog.Error(err)
		}
	}

	return nil
}

func (c *Controller) cleanOVNForBgpEdgeRouter(key, lrName string) error {
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:        util.CniTypeName,
		ovs.ExternalIDBgpEdgeRouter: key,
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
	if err = c.OVNNbClient.DeletePortGroup(berPortGroupName(key)); err != nil {
		klog.Error(err)
		return err
	}
	for _, af := range [...]int{4, 6} {
		if err = c.OVNNbClient.DeleteAddressSet(berAddressSetName(key, af)); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func berPortGroupName(key string) string {
	hash := util.Sha256Hash([]byte(key))
	return "BER." + hash[:12]
}

func berAddressSetName(key string, af int) string {
	hash := util.Sha256Hash([]byte(key))
	return fmt.Sprintf("BER.%s.ipv%d", hash[:12], af)
}

func (c *Controller) handlePodEventForBgpEdgeRouter(pod *corev1.Pod) error {
	if !pod.DeletionTimestamp.IsZero() || pod.Annotations[util.AllocatedAnnotation] != "true" {
		return nil
	}
	vpc := pod.Annotations[util.LogicalRouterAnnotation]
	if vpc == "" {
		return nil
	}

	router, err := c.bgpEdgeRouterLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list bgp edge router: %v", err)
		utilruntime.HandleError(err)
		return err
	}

	for _, ber := range router {
		if ber.VPC(c.config.ClusterRouter) != vpc {
			continue
		}
	}
	return nil
}

func bgpEdgeRouterContainerBGP(speakerImage, routerName string, speakerParams *kubeovnv1.BgpEdgeRouterBGPConfig) (*corev1.Container, error) {
	if speakerImage == "" {
		return nil, errors.New("BGP speaker image must be specified")
	}
	if speakerParams == nil {
		return nil, errors.New("BGP config must not be nil")
	}
	if speakerParams.ASN == 0 {
		return nil, errors.New("ASN not set, but must be non-zero value")
	}
	if speakerParams.RemoteASN == 0 {
		return nil, errors.New("remote ASN not set, but must be non-zero value")
	}
	if len(speakerParams.Neighbors) == 0 {
		return nil, errors.New("no BGP neighbors specified")
	}

	args := []string{}
	if speakerParams.RouterID != "" {
		args = append(args, "--router-id="+speakerParams.RouterID)
	}
	if speakerParams.Password != "" {
		args = append(args, "--auth-password="+speakerParams.Password)
	}
	if speakerParams.EnableGracefulRestart {
		args = append(args, "--graceful-restart")
	}
	if speakerParams.HoldTime != (metav1.Duration{}) {
		args = append(args, "--holdtime="+speakerParams.HoldTime.Duration.String())
	}
	if speakerParams.EdgeRouterMode {
		args = append(args, "--edge-router-mode=true")
	}
	if speakerParams.RouteServerClient {
		args = append(args, "--route-server-client=true")
	}
	args = append(args, fmt.Sprintf("--cluster-as=%d", speakerParams.ASN))
	args = append(args, fmt.Sprintf("--neighbor-as=%d", speakerParams.RemoteASN))

	var neighIPv4, neighIPv6 []string
	for _, neighbor := range speakerParams.Neighbors {
		switch util.CheckProtocol(neighbor) {
		case kubeovnv1.ProtocolIPv4:
			neighIPv4 = append(neighIPv4, neighbor)
		case kubeovnv1.ProtocolIPv6:
			neighIPv6 = append(neighIPv6, neighbor)
		default:
			return nil, fmt.Errorf("unsupported protocol for peer %s", neighbor)
		}
	}
	if len(neighIPv4) > 0 {
		args = append(args, "--neighbor-address="+strings.Join(neighIPv4, ","))
	}
	if len(neighIPv6) > 0 {
		args = append(args, "--neighbor-ipv6-address="+strings.Join(neighIPv6, ","))
	}

	args = append(args, speakerParams.ExtraArgs...)

	container := &corev1.Container{
		Name:            "bgp-router-speaker",
		Image:           speakerImage,
		Command:         []string{"/kube-ovn/kube-ovn-speaker"},
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{
				Name:  "EGRESS_GATEWAY_NAME",
				Value: routerName,
			},
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIP",
					},
				},
			},
			{
				Name: "MULTI_NET_STATUS",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.annotations['k8s.v1.cni.cncf.io/network-status']",
					},
				},
			},
		},
		Args: args,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "kube-ovn-logs",
				MountPath: "/var/log/kube-ovn",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(false),
			RunAsUser:  ptr.To[int64](0),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	return container, nil
}
