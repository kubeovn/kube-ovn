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

	gw.Status.InternalIPs = nil
	gw.Status.ExternalIPs = nil
	gw.Status.Workload.APIVersion = deploy.APIVersion
	gw.Status.Workload.Kind = deploy.Kind
	gw.Status.Workload.Name = deploy.Name
	gw.Status.Workload.Nodes = nil
	nodeNexthopIPv4 := make(map[string]string, int(gw.Spec.Replicas))
	nodeNexthopIPv6 := make(map[string]string, int(gw.Spec.Replicas))
	ready := util.DeploymentIsReady(deploy)
	if !ready {
		gw.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", msg, gw.Generation)
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

	// update gateway status including the internal/external IPs and the nodes where the pods are running
	gw.Status.Workload.Nodes = make([]string, 0, len(pods))
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
		gw.Status.InternalIPs = append(gw.Status.InternalIPs, strings.Join(ips, ","))
		gw.Status.ExternalIPs = append(gw.Status.ExternalIPs, strings.Join(extIPs, ","))
		gw.Status.Workload.Nodes = append(gw.Status.Workload.Nodes, pod.Spec.NodeName)
	}
	if gw, err = c.updateVpcEgressGatewayStatus(gw); err != nil {
		klog.Error(err)
		return err
	}

	// reconcile OVN routes
	if err = c.reconcileVpcEgressGatewayOVNRoutes(gw, 4, vpc.Status.Router, vpc.Status.BFDPort.Name, bfdIPv4, nodeNexthopIPv4, ipv4Src); err != nil {
		klog.Error(err)
		return err
	}
	if err = c.reconcileVpcEgressGatewayOVNRoutes(gw, 6, vpc.Status.Router, vpc.Status.BFDPort.Name, bfdIPv6, nodeNexthopIPv6, ipv6Src); err != nil {
		klog.Error(err)
		return err
	}

	if ready {
		gw.Status.Ready = true
		gw.Status.Phase = kubeovnv1.PhaseCompleted
		gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)
		if _, err = c.updateVpcEgressGatewayStatus(gw); err != nil {
			return err
		}
	}

	klog.Infof("finished reconciling vpc-egress-gateway %s", key)

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
	if _, err = c.netAttachLister.NetworkAttachmentDefinitions(nadNamespace).Get(nadName); err != nil {
		klog.Errorf("failed to get net-attach-def %s/%s: %v", nadNamespace, nadName, err)
		return "", nil, nil, nil, err
	}
	attachmentNetworkName := fmt.Sprintf("%s/%s", nadNamespace, nadName)
	internalCIDRv4, internalCIDRv6 := util.SplitStringIP(intSubnet.Spec.CIDRBlock)

	// collect egress policies
	ipv4Src, ipv6Src := set.New[string](), set.New[string]()
	ipv4ForwardSrc, ipv6ForwardSrc := set.New[string](), set.New[string]()
	ipv4SNATSrc, ipv6SNATSrc := set.New[string](), set.New[string]()
	fnFilter := func(internalCIDR string, ipBlocks []string) set.Set[string] {
		if internalCIDR == "" {
			return nil
		}

		ret := set.New[string]()
		for _, cidr := range ipBlocks {
			if ok, _ := util.CIDRContainsCIDR(internalCIDR, cidr); !ok {
				ret.Insert(cidr)
			}
		}
		return ret
	}

	for _, policy := range gw.Spec.Policies {
		ipv4, ipv6 := util.SplitIpsByProtocol(policy.IPBlocks)
		ipv4Src = ipv4Src.Insert(ipv4...)
		ipv6Src = ipv6Src.Insert(ipv6...)
		filteredV4 := fnFilter(internalCIDRv4, ipv4)
		filteredV6 := fnFilter(internalCIDRv6, ipv6)
		if policy.SNAT {
			ipv4SNATSrc = ipv4SNATSrc.Union(filteredV4)
			ipv6SNATSrc = ipv6SNATSrc.Union(filteredV6)
		} else {
			ipv4ForwardSrc = ipv4ForwardSrc.Union(filteredV4)
			ipv6ForwardSrc = ipv6ForwardSrc.Union(filteredV6)
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
			ipv4Src = ipv4Src.Insert(ipv4)
			ipv6Src = ipv6Src.Insert(ipv6)
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
	ipv4Src.Delete("")
	ipv6Src.Delete("")
	ipv4ForwardSrc.Delete("")
	ipv6ForwardSrc.Delete("")
	ipv4SNATSrc.Delete("")
	ipv6SNATSrc.Delete("")
	intRouteDstIPv4, intRouteDstIPv6 := ipv4ForwardSrc.Union(ipv4SNATSrc), ipv6ForwardSrc.Union(ipv6SNATSrc)

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
							RequiredDuringSchedulingIgnoredDuringExecution: mergeNodeSelector(gw.Spec.NodeSelector),
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
						Command: []string{
							"bash",
							"-exc",
							"chmod +t /usr/local/sbin && bash /kube-ovn/init-vpc-egress-gateway.sh",
						},
						Env: initEnv,
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
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Tolerations: slices.Clone(gw.Spec.Tolerations),
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
	deploy.APIVersion, deploy.Kind = appsv1.SchemeGroupVersion.String(), util.KindDeployment
	return attachmentNetworkName, ipv4Src, ipv6Src, deploy, nil
}

func (c *Controller) reconcileVpcEgressGatewayOVNRoutes(gw *kubeovnv1.VpcEgressGateway, af int, lrName, lrpName, bfdIP string, nextHops map[string]string, sources set.Set[string]) error {
	if len(nextHops) == 0 {
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
	asName := vegAddressSetName(key, af)
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
			bfd, err := c.OVNNbClient.CreateBFD(lrpName, dstIP, int(gw.Spec.BFD.MinRX), int(gw.Spec.BFD.MinTX), int(gw.Spec.BFD.Multiplier), externalIDs)
			if err != nil {
				klog.Error(err)
				return err
			}
			bfdIDs.Insert(bfd.UUID)
			bfdMap[bfd.DstIP] = bfd.UUID
		}
	}

	// reconcile LR policy
	if gw.Spec.TrafficPolicy == kubeovnv1.TrafficPolicyLocal {
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

	if gw.Spec.BFD.Enabled {
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
	if err = c.cleanOVNForVpcEgressGateway(key, cachedGateway.Spec.VPC); err != nil {
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

func (c *Controller) cleanOVNForVpcEgressGateway(key, lrName string) error {
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
	if err = c.OVNNbClient.DeletePortGroup(vegPortGroupName(key)); err != nil {
		klog.Error(err)
		return err
	}
	for _, af := range [...]int{4, 6} {
		if err = c.OVNNbClient.DeleteAddressSet(vegAddressSetName(key, af)); err != nil {
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

func vegAddressSetName(key string, af int) string {
	hash := util.Sha256Hash([]byte(key))
	return fmt.Sprintf("VEG.%s.ipv%d", hash[:12], af)
}

func (c *Controller) handlePodEventForVpcEgressGateway(pod *corev1.Pod) error {
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
