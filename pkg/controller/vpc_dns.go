package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	kubeovnyaml "github.com/kubeovn/kube-ovn/yamls"
)

var (
	corednsImage    = ""
	corednsVip      = ""
	nadName         = ""
	nadProvider     = ""
	cmVersion       = ""
	k8sServiceHost  = ""
	k8sServicePort  = ""
	enableCoredns   = false
	hostNameservers []string

	corednsTemplateContent = kubeovnyaml.CorednsTemplateContent
)

const (
	CorednsContainerName = "coredns"
	CorednsLabelKey      = "k8s-app"
	CorednsTemplateDep   = "coredns-template.yaml"
)

func genVpcDNSDpName(name string) string {
	return fmt.Sprintf("vpc-dns-%s", name)
}

func hostConfigFromReader() error {
	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		klog.Error(err)
		return err
	}
	defer func(file *os.File) {
		if err := file.Close(); err != nil {
			klog.Errorf("failed to close file, %s", err)
		}
	}(file)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			klog.Error(err)
			return err
		}
		line := scanner.Text()
		f := strings.Fields(line)
		if len(f) < 1 {
			continue
		}
		if f[0] == "nameserver" && len(f) > 1 {
			name := f[1]
			hostNameservers = append(hostNameservers, name)
		}
	}

	return err
}

func (c *Controller) enqueueAddVpcDNS(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vpc-dns %s", key)
	c.addOrUpdateVpcDNSQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcDNS(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	oldVPCDNS := oldObj.(*kubeovnv1.VpcDns)
	newVPCDNS := newObj.(*kubeovnv1.VpcDns)
	if oldVPCDNS.ResourceVersion != newVPCDNS.ResourceVersion &&
		!reflect.DeepEqual(oldVPCDNS.Spec, newVPCDNS.Spec) {
		klog.V(3).Infof("enqueue update vpc-dns %s", key)
		c.addOrUpdateVpcDNSQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVPCDNS(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete vpc-dns %s", key)
	c.delVpcDNSQueue.Add(key)
}

func (c *Controller) runAddOrUpdateVPCDNSWorker() {
	for c.processNextWorkItem("addOrUpdateVpcDns", c.addOrUpdateVpcDNSQueue, c.handleAddOrUpdateVPCDNS) {
	}
}

func (c *Controller) runDelVPCDNSWorker() {
	for c.processNextWorkItem("delVpcDns", c.delVpcDNSQueue, c.handleDelVpcDNS) {
	}
}

func (c *Controller) handleAddOrUpdateVPCDNS(key string) error {
	klog.V(3).Infof("handleAddOrUpdateVPCDNS %s", key)
	if !enableCoredns {
		time.Sleep(10 * time.Second)
		if !enableCoredns {
			return fmt.Errorf("failed to add or update vpc-dns, not enabled")
		}
	}

	vpcDNS, err := c.vpcDNSLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	defer func() {
		newVPCDNS := vpcDNS.DeepCopy()
		newVPCDNS.Status.Active = true
		if err != nil {
			newVPCDNS.Status.Active = false
		}

		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcDnses().UpdateStatus(context.Background(),
			newVPCDNS, metav1.UpdateOptions{}); err != nil {
			err := fmt.Errorf("failed to update vpc dns status, %v", err)
			klog.Error(err)
		}
	}()

	if len(corednsImage) == 0 {
		err := fmt.Errorf("vpc-dns coredns image should be set")
		klog.Error(err)
		return err
	}

	if len(corednsVip) == 0 {
		err := fmt.Errorf("vpc-dns corednsVip should be set")
		klog.Error(err)
		return err
	}

	if _, err := c.vpcsLister.Get(vpcDNS.Spec.Vpc); err != nil {
		err := fmt.Errorf("failed to get vpc '%s', err: %v", vpcDNS.Spec.Vpc, err)
		klog.Error(err)
		return err
	}

	if _, err := c.subnetsLister.Get(vpcDNS.Spec.Subnet); err != nil {
		err := fmt.Errorf("failed to get subnet '%s', err: %v", vpcDNS.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	if err := c.checkOvnNad(); err != nil {
		err := fmt.Errorf("failed to check nad, %v", err)
		klog.Error(err)
		return err
	}

	if err := c.checkVpcDNSDuplicated(vpcDNS); err != nil {
		err = fmt.Errorf("failed to deploy %s, %v", vpcDNS.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.createOrUpdateVpcDNSDep(vpcDNS); err != nil {
		err = fmt.Errorf("failed to create or update vpc dns %s, %v", vpcDNS.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.createOrUpdateVpcDNSSlr(vpcDNS); err != nil {
		err = fmt.Errorf("failed to create or update slr for vpc dns %s, %v", vpcDNS.Name, err)
		klog.Error(err)
		return err
	}

	return err
}

func (c *Controller) handleDelVpcDNS(key string) error {
	klog.V(3).Infof("handleDelVpcDNS,%s", key)
	name := genVpcDNSDpName(key)
	err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		err := fmt.Errorf("failed to delete vpc dns deployment: %v", err)
		klog.Error(err)
		return err
	}

	err = c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		err := fmt.Errorf("failed to delete switch lb rule: %v", err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) checkVpcDNSDuplicated(vpcDNS *kubeovnv1.VpcDns) error {
	vpcDNSList, err := c.vpcDNSLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	for _, item := range vpcDNSList {
		if item.Status.Active &&
			item.Name != vpcDNS.Name &&
			item.Spec.Vpc == vpcDNS.Spec.Vpc {
			err = fmt.Errorf("only one vpc-dns can be deployed in a vpc")
			return err
		}
	}
	return nil
}

func (c *Controller) createOrUpdateVpcDNSDep(vpcDNS *kubeovnv1.VpcDns) error {
	needToCreateDp := false
	oldDp, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
		Get(context.Background(), genVpcDNSDpName(vpcDNS.Name), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateDp = true
		} else {
			return err
		}
	}

	newDp, err := c.genVpcDNSDeployment(vpcDNS, oldDp)
	if err != nil {
		klog.Errorf("failed to generate vpc-dns deployment, %v", err)
		return err
	}

	if vpcDNS.Spec.Replicas != 0 {
		newDp.Spec.Replicas = &vpcDNS.Spec.Replicas
	}

	if needToCreateDp {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Create(context.Background(), newDp, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("failed to create deployment '%s', err: %s", newDp.Name, err)
			return err
		}
	} else {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Update(context.Background(), newDp, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update deployment '%s', err: %v", newDp.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) createOrUpdateVpcDNSSlr(vpcDNS *kubeovnv1.VpcDns) error {
	needToCreateSlr := false
	oldSlr, err := c.switchLBRuleLister.Get(genVpcDNSDpName(vpcDNS.Name))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateSlr = true
		} else {
			klog.Error(err)
			return err
		}
	}

	newSlr, err := c.genVpcDNSSlr(vpcDNS.Name, c.config.PodNamespace)
	if err != nil {
		klog.Errorf("failed to generate vpc-dns switchLBRule, %v", err)
		return err
	}

	if needToCreateSlr {
		_, err := c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Create(context.Background(), newSlr, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("failed to create switchLBRules '%s', err: %v", newSlr.Name, err)
			return err
		}
	} else {
		if reflect.DeepEqual(oldSlr.Spec, newSlr.Spec) {
			return nil
		}

		newSlr.ResourceVersion = oldSlr.ResourceVersion
		_, err := c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Update(context.Background(), newSlr, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update switchLBRules '%s', err: %v", newSlr.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) genVpcDNSDeployment(vpcDNS *kubeovnv1.VpcDns, oldDeploy *v1.Deployment) (*v1.Deployment, error) {
	if _, err := os.Stat(CorednsTemplateDep); errors.Is(err, os.ErrNotExist) {
		klog.Errorf("failed to get coredns template file, %v", err)
		return nil, err
	}

	tmp, err := template.ParseFiles(CorednsTemplateDep)
	if err != nil {
		klog.Errorf("failed to parse coredns template file, %v", err)
		return nil, err
	}

	buffer := new(bytes.Buffer)
	name := genVpcDNSDpName(vpcDNS.Name)
	if err := tmp.Execute(buffer, map[string]interface{}{
		"DeployName":   name,
		"CorednsImage": corednsImage,
	}); err != nil {
		return nil, err
	}

	dep := &v1.Deployment{}
	retJSON, err := yaml.ToJSON(buffer.Bytes())
	if err != nil {
		klog.Errorf("failed to switch yaml, %v", err)
		return nil, err
	}

	if err := json.Unmarshal(retJSON, dep); err != nil {
		klog.Errorf("failed to switch json, %v", err)
		return nil, err
	}

	dep.Spec.Template.Annotations = make(map[string]string)

	if oldDeploy != nil && len(oldDeploy.Annotations) != 0 {
		dep.Spec.Template.Annotations = oldDeploy.Annotations
	}

	dep.ObjectMeta.Labels = map[string]string{
		util.VpcDNSNameLabel: "true",
	}

	setCoreDNSEnv(dep)
	setVpcDNSInterface(dep, vpcDNS.Spec.Subnet)

	defaultSubnet, err := c.subnetsLister.Get(util.DefaultSubnet)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return nil, err
	}
	if err = c.resyncVpcNatImage(); err != nil {
		klog.Errorf("failed to resync vpc nat config, err: %v", err)
		return nil, err
	}
	setVpcDNSRoute(dep, defaultSubnet.Spec.Gateway)
	return dep, nil
}

func (c *Controller) genVpcDNSSlr(vpcName, namespace string) (*kubeovnv1.SwitchLBRule, error) {
	name := genVpcDNSDpName(vpcName)
	label := fmt.Sprintf("%s:%s", CorednsLabelKey, name)

	ports := []kubeovnv1.SlrPort{
		{Name: "dns", Port: 53, Protocol: "UDP"},
		{Name: "dns-tcp", Port: 53, Protocol: "TCP"},
		{Name: "metrics", Port: 9153, Protocol: "TCP"},
	}

	slr := &kubeovnv1.SwitchLBRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				util.VpcDNSNameLabel: "true",
			},
		},
		Spec: kubeovnv1.SwitchLBRuleSpec{
			Vip:             corednsVip,
			Namespace:       namespace,
			Selector:        []string{label},
			SessionAffinity: "",
			Ports:           ports,
		},
	}

	return slr, nil
}

func setVpcDNSInterface(dp *v1.Deployment, subnetName string) {
	annotations := dp.Spec.Template.Annotations
	annotations[util.LogicalSwitchAnnotation] = subnetName
	annotations[util.AttachmentNetworkAnnotation] = fmt.Sprintf("%s/%s", corev1.NamespaceDefault, nadName)
	annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, nadProvider)] = util.DefaultSubnet
}

func setCoreDNSEnv(dp *v1.Deployment) {
	var env []corev1.EnvVar

	if len(k8sServiceHost) != 0 {
		env = append(env, corev1.EnvVar{Name: "KUBERNETES_SERVICE_HOST", Value: k8sServiceHost})
	}

	if len(k8sServicePort) != 0 {
		env = append(env, corev1.EnvVar{Name: "KUBERNETES_SERVICE_PORT", Value: k8sServicePort})
	}

	for i, container := range dp.Spec.Template.Spec.Containers {
		if container.Name == CorednsContainerName {
			dp.Spec.Template.Spec.Containers[i].Env = env
			break
		}
	}
}

func setVpcDNSRoute(dp *v1.Deployment, subnetGw string) {
	var serviceHost string
	if len(k8sServiceHost) == 0 {
		serviceHost = os.Getenv("KUBERNETES_SERVICE_HOST")
	} else {
		serviceHost = k8sServiceHost
	}

	var routeCmd string
	v4Gw, _ := util.SplitStringIP(subnetGw)

	if v4Gw != "" {
		routeCmd = fmt.Sprintf("ip -4 route add %s via %s dev net1;", serviceHost, v4Gw)
		for _, nameserver := range hostNameservers {
			routeCmd += fmt.Sprintf("ip -4 route add %s via %s dev net1;", nameserver, v4Gw)
		}
	}
	// TODO:// ipv6
	privileged := true
	allowPrivilegeEscalation := true
	dp.Spec.Template.Spec.InitContainers = append(dp.Spec.Template.Spec.InitContainers, corev1.Container{
		Name:            "init-route",
		Image:           vpcNatImage,
		Command:         []string{"sh", "-c", routeCmd},
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			Privileged:               &privileged,
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		},
	})
}

func (c *Controller) checkOvnNad() error {
	_, err := c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(corev1.NamespaceDefault).
		Get(context.Background(), nadName, metav1.GetOptions{})
	if err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) resyncVpcDNSConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcDNSConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get %s, %v", util.VpcDNSConfig, err)
		return
	}

	if k8serrors.IsNotFound(err) {
		klog.V(3).Infof("the vpc-dns configuration is not set")
		if len(cmVersion) != 0 {
			if err := c.cleanVpcDNS(); err != nil {
				klog.Errorf("failed to clear all vpc-dns, %v", err)
				return
			}
			cmVersion = ""
		}
		return
	}

	if cmVersion == cm.ResourceVersion {
		return
	}
	cmVersion = cm.ResourceVersion
	klog.V(3).Infof("the vpc-dns ConfigMap update")

	getValue := func(key string) string {
		if v, ok := cm.Data[key]; ok {
			return v
		}
		return ""
	}

	corednsImage = getValue("coredns-image")
	if len(corednsImage) == 0 {
		defaultImage, err := c.getDefaultCoreDNSImage()
		if err != nil {
			klog.Errorf("failed to get kube-system/coredns image, %s", err)
			return
		}
		corednsImage = defaultImage
		klog.V(3).Infof("use the cluster default coredns image version, %s", corednsImage)
	}

	if err := os.WriteFile(CorednsTemplateDep, corednsTemplateContent, 0o644); err != nil {
		klog.Errorf("failed to wirite local coredns-template.yaml file, %v", err)
		return
	}

	nadName = getValue("nad-name")
	nadProvider = getValue("nad-provider")
	corednsVip = getValue("coredns-vip")
	k8sServiceHost = getValue("k8s-service-host")
	k8sServicePort = getValue("k8s-service-port")

	newEnableCoredns := true
	if v, ok := cm.Data["enable-vpc-dns"]; ok {
		raw, err := strconv.ParseBool(v)
		if err != nil {
			klog.Errorf("failed to parse cm enable, %v", err)
			return
		}
		newEnableCoredns = raw
	}

	if enableCoredns && !newEnableCoredns {
		if err := c.cleanVpcDNS(); err != nil {
			klog.Errorf("failed to clear all vpc-dns, %v", err)
			return
		}
	} else {
		if err := c.updateVpcDNS(); err != nil {
			klog.Errorf("failed to update vpc-dns deployment")
			return
		}
	}
	enableCoredns = newEnableCoredns
}

func (c *Controller) getDefaultCoreDNSImage() (string, error) {
	dp, err := c.config.KubeClient.AppsV1().Deployments("kube-system").
		Get(context.Background(), "coredns", metav1.GetOptions{})
	if err != nil {
		klog.Error(err)
		return "", err
	}

	for _, container := range dp.Spec.Template.Spec.Containers {
		if container.Name == CorednsContainerName {
			return container.Image, nil
		}
	}

	return "", fmt.Errorf("coredns container no found")
}

func (c *Controller) initVpcDNSConfig() error {
	if err := hostConfigFromReader(); err != nil {
		klog.Errorf("failed to get get host nameserver, %v", err)
		return err
	}

	c.resyncVpcDNSConfig()
	return nil
}

func (c *Controller) cleanVpcDNS() error {
	klog.Infof("clear all vpc-dns")
	err := c.config.KubeOvnClient.KubeovnV1().VpcDnses().DeleteCollection(context.Background(), metav1.DeleteOptions{},
		metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to clear all vpc-dns %s", err)
		return err
	}

	return nil
}

func (c *Controller) updateVpcDNS() error {
	list, err := c.vpcDNSLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vpc-dns list, %s", err)
		return err
	}

	for _, vd := range list {
		c.addOrUpdateVpcDNSQueue.Add(vd.Name)
	}
	return nil
}
