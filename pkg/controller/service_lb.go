package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"strings"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	initRouteTable = "init"
	podEIPAdd      = "eip-add"
	podDNATAdd     = "dnat-add"
	attachmentName = "lb-svc-attachment"
	attachmentNs   = "kube-system"
)

func genLbSvcDpName(name string) string {
	return fmt.Sprintf("lb-svc-%s", name)
}

func getAttachNetworkProvider(svc *corev1.Service) string {
	providerName := fmt.Sprintf("%s.%s", attachmentName, attachmentNs)
	if svc.Annotations[util.AttachmentProvider] != "" {
		providerName = svc.Annotations[util.AttachmentProvider]
	}

	return providerName
}

func parseAttachNetworkProvider(svc *corev1.Service) (string, string) {
	var attachmentName, attachmentNs string

	providerName := getAttachNetworkProvider(svc)
	values := strings.Split(providerName, ".")
	if len(values) <= 1 {
		return attachmentName, attachmentNs
	}
	attachmentName = values[0]
	attachmentNs = values[1]

	return attachmentName, attachmentNs
}

func (c *Controller) checkAttachNetwork(svc *corev1.Service) error {
	attachmentName, attachmentNs := parseAttachNetworkProvider(svc)
	if attachmentName == "" && attachmentNs == "" {
		return errors.New("the provider name should be consisted of name and namespace")
	}

	_, err := c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(attachmentNs).Get(context.Background(), attachmentName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get network attachment definition %s in namespace %s, err: %v", attachmentName, attachmentNs, err)
		return err
	}
	return nil
}

func (c *Controller) genLbSvcDeployment(svc *corev1.Service) (dp *v1.Deployment) {
	name := genLbSvcDpName(svc.Name)
	labels := map[string]string{
		"app":       name,
		"namespace": svc.Namespace,
		"service":   svc.Name,
	}

	attachmentName, attachmentNs := parseAttachNetworkProvider(svc)
	providerName := getAttachNetworkProvider(svc)
	attachSubnetAnnotation := fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)
	attachIPAnnotation := fmt.Sprintf(util.IPAddressAnnotationTemplate, providerName)
	podAnnotations := map[string]string{
		util.AttachmentNetworkAnnotation: fmt.Sprintf("%s/%s", attachmentNs, attachmentName),
		attachSubnetAnnotation:           svc.Annotations[attachSubnetAnnotation],
	}
	if svc.Spec.LoadBalancerIP != "" {
		podAnnotations[attachIPAnnotation] = svc.Spec.LoadBalancerIP
	}
	if v, ok := svc.Annotations[util.LogicalSwitchAnnotation]; ok {
		podAnnotations[util.LogicalSwitchAnnotation] = v
	}

	dp = &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "lb-svc",
							Image:           vpcNatImage,
							Command:         []string{"sleep", "infinity"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								Privileged:               ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(true),
							},
						},
					},
					TerminationGracePeriodSeconds: ptr.To(int64(0)),
				},
			},
			Strategy: v1.DeploymentStrategy{
				Type: v1.RecreateDeploymentStrategyType,
			},
		},
	}
	return
}

func (c *Controller) updateLbSvcDeployment(svc *corev1.Service, dp *v1.Deployment) *v1.Deployment {
	attachmentName, attachmentNs := parseAttachNetworkProvider(svc)
	providerName := getAttachNetworkProvider(svc)
	attachSubnetAnnotation := fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)
	attachIPAnnotation := fmt.Sprintf(util.IPAddressAnnotationTemplate, providerName)
	podAnnotations := map[string]string{
		util.AttachmentNetworkAnnotation: fmt.Sprintf("%s/%s", attachmentNs, attachmentName),
		attachSubnetAnnotation:           svc.Annotations[attachSubnetAnnotation],
	}
	if svc.Spec.LoadBalancerIP != "" {
		podAnnotations[attachIPAnnotation] = svc.Spec.LoadBalancerIP
	}
	if maps.Equal(podAnnotations, dp.Spec.Template.Annotations) {
		return nil
	}

	dp.Spec.Template.Annotations = podAnnotations
	return dp
}

func (c *Controller) createLbSvcPod(svc *corev1.Service) error {
	deployName := genLbSvcDpName(svc.Name)
	deploy, err := c.config.KubeClient.AppsV1().Deployments(svc.Namespace).Get(context.Background(), deployName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		deploy = nil
	}

	if deploy == nil {
		deploy = c.genLbSvcDeployment(svc)
		klog.Infof("creating deployment %s/%s", deploy.Namespace, deploy.Name)
		if _, err := c.config.KubeClient.AppsV1().Deployments(svc.Namespace).Create(context.Background(), deploy, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create deployment %s/%s: err: %v", deploy.Namespace, deploy.Name, err)
			return err
		}
	} else {
		newDeploy := c.updateLbSvcDeployment(svc, deploy)
		if newDeploy == nil {
			klog.V(3).Infof("no need to update deployment %s/%s", deploy.Namespace, deploy.Name)
			return nil
		}
		if _, err := c.config.KubeClient.AppsV1().Deployments(svc.Namespace).Update(context.Background(), newDeploy, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update deployment %s, err: %v", deploy.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) getLbSvcPod(svcName, svcNamespace string) (*corev1.Pod, error) {
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"app": genLbSvcDpName(svcName), "namespace": svcNamespace},
	})

	pods, err := c.podsLister.Pods(svcNamespace).List(sel)
	switch {
	case err != nil:
		klog.Error(err)
		return nil, err
	case len(pods) == 0:
		time.Sleep(2 * time.Second)
		return nil, fmt.Errorf("pod of deployment %s/%s not found", svcNamespace, genLbSvcDpName(svcName))
	case len(pods) != 1:
		time.Sleep(2 * time.Second)
		return nil, errors.New("too many pods")
	case pods[0].Status.Phase != corev1.PodRunning:
		time.Sleep(2 * time.Second)
		return nil, fmt.Errorf("pod %s/%s is not running", pods[0].Namespace, pods[0].Name)
	}

	return pods[0], nil
}

func (c *Controller) validateSvc(svc *corev1.Service) error {
	providerName := getAttachNetworkProvider(svc)
	attachSubnetAnnotation := fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)

	if svc.Spec.LoadBalancerIP != "" {
		if ip := net.ParseIP(svc.Spec.LoadBalancerIP); ip == nil {
			return fmt.Errorf("invalid static loadbalancerIP %s for svc %s", svc.Spec.LoadBalancerIP, svc.Name)
		}
	}

	if svc.Annotations[attachSubnetAnnotation] != "" {
		subnet, err := c.subnetsLister.Get(svc.Annotations[attachSubnetAnnotation])
		if err != nil {
			klog.Errorf("failed to get subnet %v", err)
			return err
		}

		if svc.Spec.LoadBalancerIP != "" && !util.CIDRContainIP(subnet.Spec.CIDRBlock, svc.Spec.LoadBalancerIP) {
			return fmt.Errorf("the loadbalancer IP %s is not in the range of subnet %s, cidr %v", svc.Spec.LoadBalancerIP, subnet.Name, subnet.Spec.CIDRBlock)
		}
	}
	return nil
}

func (c *Controller) getPodAttachIP(pod *corev1.Pod, svc *corev1.Service) (string, error) {
	var loadBalancerIP string
	var err error

	providerName := getAttachNetworkProvider(svc)
	attachIPAnnotation := fmt.Sprintf(util.IPAddressAnnotationTemplate, providerName)

	if pod.Annotations[attachIPAnnotation] != "" {
		loadBalancerIP = pod.Annotations[attachIPAnnotation]
	} else {
		err = errors.New("failed to get attachment ip from pod's annotation")
	}

	return loadBalancerIP, err
}

func (c *Controller) deleteLbSvc(svc *corev1.Service) error {
	if err := c.config.KubeClient.AppsV1().Deployments(svc.Namespace).Delete(context.Background(), genLbSvcDpName(svc.Name), metav1.DeleteOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to delete deployment %s, err: %v", genLbSvcDpName(svc.Name), err)
		return err
	}

	return nil
}

func (c *Controller) execNatRules(pod *corev1.Pod, operation string, rules []string) error {
	cmd := fmt.Sprintf("bash /kube-ovn/lb-svc.sh %s %s", operation, strings.Join(rules, " "))
	klog.V(3).Info(cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "lb-svc", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.V(3).Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return err
	}

	if len(stdOutput) > 0 {
		klog.V(3).Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return errors.New(errOutput)
	}
	return nil
}

func (c *Controller) updatePodAttachNets(pod *corev1.Pod, svc *corev1.Service) error {
	if err := c.execNatRules(pod, initRouteTable, []string{}); err != nil {
		klog.Errorf("failed to init route table, err: %v", err)
		return err
	}

	providerName := getAttachNetworkProvider(svc)
	attachIPAnnotation := fmt.Sprintf(util.IPAddressAnnotationTemplate, providerName)
	attachCidrAnnotation := fmt.Sprintf(util.CidrAnnotationTemplate, providerName)
	attachGatewayAnnotation := fmt.Sprintf(util.GatewayAnnotationTemplate, providerName)

	if pod.Annotations[attachCidrAnnotation] == "" || pod.Annotations[attachGatewayAnnotation] == "" {
		return fmt.Errorf("failed to get attachment network info for pod %s", pod.Name)
	}

	loadBalancerIP := pod.Annotations[attachIPAnnotation]
	ipAddr, err := util.GetIPAddrWithMask(loadBalancerIP, pod.Annotations[attachCidrAnnotation])
	if err != nil {
		klog.Errorf("failed to get ip addr with mask, err: %v", err)
		return err
	}
	var addRules []string
	addRules = append(addRules, fmt.Sprintf("%s,%s", ipAddr, pod.Annotations[attachGatewayAnnotation]))
	klog.Infof("add eip rules for lb svc pod, %v", addRules)
	if err := c.execNatRules(pod, podEIPAdd, addRules); err != nil {
		klog.Errorf("failed to add eip for pod, err: %v", err)
		return err
	}

	defaultGateway := pod.Annotations[util.GatewayAnnotation]
	for _, port := range svc.Spec.Ports {
		var protocol string
		switch port.Protocol {
		case corev1.ProtocolTCP:
			protocol = util.ProtocolTCP
		case corev1.ProtocolUDP:
			protocol = util.ProtocolUDP
		case corev1.ProtocolSCTP:
			protocol = util.ProtocolSCTP
		}

		var rules []string
		rules = append(rules, fmt.Sprintf("%s,%d,%s,%s,%d,%s", loadBalancerIP, port.Port, protocol, svc.Spec.ClusterIP, port.Port, defaultGateway))
		klog.Infof("add dnat rules for lb svc pod, %v", rules)
		if err := c.execNatRules(pod, podDNATAdd, rules); err != nil {
			klog.Errorf("failed to add dnat for pod, err: %v", err)
			return err
		}
	}

	return nil
}

func (c *Controller) checkAndReInitLbSvcPod(pod *corev1.Pod) error {
	if pod.Status.Phase != corev1.PodRunning {
		klog.V(3).Infof("pod %s/%s is not running", pod.Namespace, pod.Name)
		return nil
	}

	var exist bool
	var nsName, svcName string

	// ensure that pod is created by load-balancer service
	if nsName, exist = pod.Labels["namespace"]; !exist {
		return nil
	}
	if svcName, exist = pod.Labels["service"]; !exist {
		return nil
	}
	if deployName, exist := pod.Labels["app"]; !exist || !strings.HasPrefix(deployName, "lb-svc-") {
		return nil
	}

	c.svcKeyMutex.LockKey(svcName)
	defer func() { _ = c.svcKeyMutex.UnlockKey(svcName) }()

	lbsvc, err := c.servicesLister.Services(nsName).Get(svcName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if lbsvc.Spec.Type != corev1.ServiceTypeLoadBalancer || !c.config.EnableLbSvc {
		return nil
	}

	if pod.Status.Phase == corev1.PodRunning && len(lbsvc.Status.LoadBalancer.Ingress) == 1 {
		klog.Infof("LB service pod Running %s/%s for service %s", nsName, pod.Name, svcName)
		if err = c.updatePodAttachNets(pod, lbsvc); err != nil {
			klog.Errorf("failed to update service %s/%s attachment network: %v", nsName, svcName, err)
			return err
		}

		loadBalancerIP, err := c.getPodAttachIP(pod, lbsvc)
		if err != nil {
			klog.Errorf("failed to get loadBalancer IP for %s/%s: %v", nsName, svcName, err)
			return err
		}
		lbsvc = lbsvc.DeepCopy()
		lbsvc.Status.LoadBalancer.Ingress[0].IP = loadBalancerIP

		if _, err = c.config.KubeClient.CoreV1().Services(nsName).UpdateStatus(context.Background(), lbsvc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update service %s/%s status: %v", nsName, svcName, err)
			return err
		}
	}

	return nil
}
