package controller

import (
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

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
	kubeovnyaml "github.com/kubeovn/kube-ovn/yamls"
)

var (
	corednsImage   = ""
	corednsVip     = ""
	nadName        = ""
	nadProvider    = ""
	cmVersion      = ""
	k8sServiceHost = ""
	k8sServicePort = ""
	enableCoreDNS  = false

	corednsTemplateContent = string(kubeovnyaml.CorednsTemplateContent)
)

const (
	CorednsContainerName = "coredns"
	CorednsLabelKey      = "k8s-app"
)

func genVpcDNSDpName(name string) string {
	return "vpc-dns-" + name
}

func (c *Controller) enqueueAddVpcDNS(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcDns)).String()
	klog.V(3).Infof("enqueue add vpc-dns %s", key)
	c.addOrUpdateVpcDNSQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcDNS(oldObj, newObj any) {
	oldVPCDNS := oldObj.(*kubeovnv1.VpcDns)
	newVPCDNS := newObj.(*kubeovnv1.VpcDns)
	if oldVPCDNS.ResourceVersion != newVPCDNS.ResourceVersion &&
		!reflect.DeepEqual(oldVPCDNS.Spec, newVPCDNS.Spec) {
		key := cache.MetaObjectToName(newVPCDNS).String()
		klog.V(3).Infof("enqueue update vpc-dns %s", key)
		c.addOrUpdateVpcDNSQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVPCDNS(obj any) {
	var dns *kubeovnv1.VpcDns
	switch t := obj.(type) {
	case *kubeovnv1.VpcDns:
		dns = t
	case cache.DeletedFinalStateUnknown:
		d, ok := t.Obj.(*kubeovnv1.VpcDns)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		dns = d
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(dns).String()
	klog.V(3).Infof("enqueue delete vpc-dns %s", key)
	c.delVpcDNSQueue.Add(key)
}

func (c *Controller) handleAddOrUpdateVPCDNS(key string) error {
	klog.Infof("handle add or update vpc dns %s", key)
	if !enableCoreDNS {
		return errors.New("failed to add or update vpc-dns, not enabled")
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
			err := fmt.Errorf("failed to update vpc dns status, %w", err)
			klog.Error(err)
		}
	}()

	if len(corednsImage) == 0 {
		err := errors.New("vpc-dns coredns image should be set")
		klog.Error(err)
		return err
	}

	if len(corednsVip) == 0 {
		err := errors.New("vpc-dns corednsVip should be set")
		klog.Error(err)
		return err
	}

	if _, err := c.vpcsLister.Get(vpcDNS.Spec.Vpc); err != nil {
		err := fmt.Errorf("failed to get vpc '%s', err: %w", vpcDNS.Spec.Vpc, err)
		klog.Error(err)
		return err
	}

	if _, err := c.subnetsLister.Get(vpcDNS.Spec.Subnet); err != nil {
		err := fmt.Errorf("failed to get subnet '%s', err: %w", vpcDNS.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	if err := c.checkOvnNad(); err != nil {
		err := fmt.Errorf("failed to check nad, %w", err)
		klog.Error(err)
		return err
	}

	if err := c.checkVpcDNSDuplicated(vpcDNS); err != nil {
		err = fmt.Errorf("failed to deploy %s, %w", vpcDNS.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.createOrUpdateVpcDNSDep(vpcDNS); err != nil {
		err = fmt.Errorf("failed to create or update vpc dns %s, %w", vpcDNS.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.createOrUpdateVpcDNSSlr(vpcDNS); err != nil {
		err = fmt.Errorf("failed to create or update slr for vpc dns %s, %w", vpcDNS.Name, err)
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
		err := fmt.Errorf("failed to delete vpc dns deployment: %w", err)
		klog.Error(err)
		return err
	}

	err = c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		err := fmt.Errorf("failed to delete switch lb rule: %w", err)
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
			err = errors.New("only one vpc-dns can be deployed in a vpc")
			return err
		}
	}
	return nil
}

func (c *Controller) createOrUpdateVpcDNSDep(vpcDNS *kubeovnv1.VpcDns) error {
	deployName := genVpcDNSDpName(vpcDNS.Name)
	deploy, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
		Get(context.Background(), deployName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get deployment %s/%s: %v", c.config.PodNamespace, deployName, err)
			return err
		}
		deploy = nil
	}

	newDp, err := c.genVpcDNSDeployment(vpcDNS)
	if err != nil {
		klog.Errorf("failed to generate vpc-dns deployment, %v", err)
		return err
	}

	if vpcDNS.Spec.Replicas != 0 {
		newDp.Spec.Replicas = ptr.To(vpcDNS.Spec.Replicas)
	}

	if deploy == nil {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Create(context.Background(), newDp, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("failed to create deployment %q: %v", newDp.Name, err)
			return err
		}
	} else {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Update(context.Background(), newDp, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update deployment %q: %v", newDp.Name, err)
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

func (c *Controller) genVpcDNSDeployment(vpcDNS *kubeovnv1.VpcDns) (*v1.Deployment, error) {
	tmp := template.New("coredns")
	tmp, err := tmp.Parse(corednsTemplateContent)
	if err != nil {
		klog.Errorf("failed to parse coredns template file, %v", err)
		return nil, err
	}
	vpcDNSCorefile := vpcDNS.Spec.Corefile
	if vpcDNSCorefile == "" {
		vpcDNSCorefile = "vpc-dns-corefile"
	}

	buffer := new(bytes.Buffer)
	name := genVpcDNSDpName(vpcDNS.Name)
	if err := tmp.Execute(buffer, map[string]any{
		"DeployName":     name,
		"CorednsImage":   corednsImage,
		"VpcDnsCorefile": vpcDNSCorefile,
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

	dep.Labels = map[string]string{
		util.VpcDNSNameLabel: "true",
	}

	defaultSubnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err != nil {
		klog.Errorf("failed to get default subnet %s: %v", c.config.DefaultLogicalSwitch, err)
		return nil, err
	}

	setCoreDNSEnv(dep)
	setVpcDNSInterface(dep, vpcDNS.Spec.Subnet, defaultSubnet)

	return dep, nil
}

func (c *Controller) genVpcDNSSlr(vpcName, namespace string) (*kubeovnv1.SwitchLBRule, error) {
	name := genVpcDNSDpName(vpcName)
	label := fmt.Sprintf("%s:%s", CorednsLabelKey, name)

	ports := []kubeovnv1.SwitchLBRulePort{
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

func setVpcDNSInterface(dp *v1.Deployment, subnetName string, defaultSubnet *kubeovnv1.Subnet) {
	annotations := dp.Spec.Template.Annotations
	annotations[util.LogicalSwitchAnnotation] = subnetName
	annotations[nadv1.NetworkAttachmentAnnot] = fmt.Sprintf("%s/%s", corev1.NamespaceDefault, nadName)
	annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, nadProvider)] = util.DefaultSubnet

	setVpcDNSRoute(dp, defaultSubnet.Spec.Gateway)
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
	dst := k8sServiceHost
	if len(dst) == 0 {
		dst = os.Getenv("KUBERNETES_SERVICE_HOST")
	}

	protocol := util.CheckProtocol(dst)
	if !strings.ContainsRune(dst, '/') {
		switch protocol {
		case kubeovnv1.ProtocolIPv4:
			dst += "/32"
		case kubeovnv1.ProtocolIPv6:
			dst += "/128"
		}
	}
	for gw := range strings.SplitSeq(subnetGw, ",") {
		if util.CheckProtocol(gw) == protocol {
			routes := []request.Route{{Destination: dst, Gateway: gw}}
			buf, err := json.Marshal(routes)
			if err != nil {
				klog.Errorf("failed to marshal routes %+v: %v", routes, err)
			} else {
				if dp.Spec.Template.Annotations == nil {
					dp.Spec.Template.Annotations = map[string]string{}
				}
				dp.Spec.Template.Annotations[fmt.Sprintf(util.RoutesAnnotationTemplate, nadProvider)] = string(buf)
			}
			break
		}
	}
}

func (c *Controller) checkOvnNad() error {
	_, err := c.netAttachLister.NetworkAttachmentDefinitions(corev1.NamespaceDefault).Get(nadName)
	if err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) resyncVpcDNSConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcDNSConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get configmap %q: %v", util.VpcDNSConfig, err)
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

	nadName = getValue("nad-name")
	nadProvider = getValue("nad-provider")
	corednsVip = getValue("coredns-vip")
	k8sServiceHost = getValue("k8s-service-host")
	k8sServicePort = getValue("k8s-service-port")

	newEnableCoreDNS := true
	if v, ok := cm.Data["enable-vpc-dns"]; ok {
		raw, err := strconv.ParseBool(v)
		if err != nil {
			klog.Errorf("failed to parse cm enable, %v", err)
			return
		}
		newEnableCoreDNS = raw
	}

	if enableCoreDNS && !newEnableCoreDNS {
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
	enableCoreDNS = newEnableCoreDNS
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

	return "", errors.New("coredns container no found")
}

func (c *Controller) initVpcDNSConfig() error {
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
