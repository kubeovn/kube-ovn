package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	// enable-vpc-dns of the ConfigMap version in cmVersion; enableCoreDNS is closed earlier when a cleanup fails
	appliedEnableCoreDNS = false
	vpcDNSConfigSynced   = false
	// serializes the vpc-dns-config sync with handleAddOrUpdateVPCDNS, which share the values above
	vpcDNSConfigMutex sync.Mutex

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

func (c *Controller) handleAddOrUpdateVPCDNS(key string) (retErr error) {
	klog.Infof("handle add or update vpc dns %s", key)
	vpcDNS, err := c.vpcDNSLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		_ = c.recordResourceError(&kubeovnv1.VpcDns{Name: key},
			"GetVpcDNSFailed", err)
		return err
	}

	defer func() {
		reconcileErr := retErr
		newVPCDNS := vpcDNS.DeepCopy()
		newVPCDNS.Status.Active = retErr == nil

		// NotFound: the config sync below can delete this VpcDns, so there is no status to update
		if _, statusErr := c.config.KubeOvnClient.KubeovnV1().VpcDnses().UpdateStatus(context.Background(),
			newVPCDNS, metav1.UpdateOptions{}); statusErr != nil && !k8serrors.IsNotFound(statusErr) {
			statusErr = fmt.Errorf("failed to update vpc dns status: %w", statusErr)
			klog.Error(statusErr)
			_ = c.recordResourceError(vpcDNS, "UpdateStatusFailed", statusErr)
			if retErr == nil {
				retErr = statusErr
			} else {
				retErr = errors.Join(retErr, statusErr)
			}
		}
		if reconcileErr != nil {
			_ = c.recordResourceError(vpcDNS, "ReconcileFailed", reconcileErr)
		} else if retErr == nil {
			c.recordResourceEvent(newVPCDNS, corev1.EventTypeNormal, "ReconcileSuccess",
				fmt.Sprintf("VpcDns %s reconciled successfully", vpcDNS.Name))
		}
	}()
	// the periodic resync can be up to 5 seconds late, so apply the current vpc-dns-config first
	vpcDNSConfigMutex.Lock()
	defer vpcDNSConfigMutex.Unlock()
	c.syncVpcDNSConfig()
	if !enableCoreDNS {
		return errors.New("failed to add or update vpc-dns, not enabled")
	}

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

	return nil
}

func (c *Controller) handleDelVpcDNS(key string) (retErr error) {
	klog.V(3).Infof("handleDelVpcDNS,%s", key)
	vpcDNS := &kubeovnv1.VpcDns{Name: key}
	defer func() {
		if retErr != nil {
			_ = c.recordResourceError(vpcDNS, "DeleteFailed", retErr)
			return
		}
		c.recordResourceEvent(vpcDNS, corev1.EventTypeNormal, "DeleteSuccess",
			fmt.Sprintf("VpcDns %s deleted successfully", key))
	}()
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
		newDp.Spec.Replicas = new(vpcDNS.Spec.Replicas)
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
		Name: name,
		Labels: map[string]string{
			util.VpcDNSNameLabel: "true",
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
		env = append(env, corev1.EnvVar{Name: util.EnvKubernetesServiceHost, Value: k8sServiceHost})
	}

	if len(k8sServicePort) != 0 {
		env = append(env, corev1.EnvVar{Name: util.EnvKubernetesServicePort, Value: k8sServicePort})
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
		dst = os.Getenv(util.EnvKubernetesServiceHost)
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
	vpcDNSConfigMutex.Lock()
	defer vpcDNSConfigMutex.Unlock()
	c.syncVpcDNSConfig()
}

// syncVpcDNSConfig applies vpc-dns-config; the caller must hold vpcDNSConfigMutex.
// cmVersion is updated only after a version is fully applied, so a failed step is retried on the next call.
func (c *Controller) syncVpcDNSConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcDNSConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get configmap %q: %v", util.VpcDNSConfig, err)
		return
	}

	if k8serrors.IsNotFound(err) {
		if vpcDNSConfigSynced && len(cmVersion) == 0 {
			return
		}
		klog.V(3).Infof("the vpc-dns configuration is not set")
		// on the first sync too: the ConfigMap may have been deleted while the controller was down
		if err := c.disableVpcDNS(); err != nil {
			klog.Errorf("failed to clear all vpc-dns, %v", err)
			return
		}
		cmVersion, appliedEnableCoreDNS, vpcDNSConfigSynced = "", false, true
		return
	}

	if vpcDNSConfigSynced && cmVersion == cm.ResourceVersion {
		return
	}
	klog.V(3).Infof("the vpc-dns ConfigMap update")

	getValue := func(key string) string {
		if v, ok := cm.Data[key]; ok {
			return v
		}
		return ""
	}

	newEnableCoreDNS := true
	if v, ok := cm.Data["enable-vpc-dns"]; ok {
		raw, err := strconv.ParseBool(v)
		if err != nil {
			klog.Errorf("failed to parse cm enable, %v", err)
			return
		}
		newEnableCoreDNS = raw
	}

	if !newEnableCoreDNS {
		// on the first sync too: vpc-dns may have been disabled while the controller was down
		if appliedEnableCoreDNS || !vpcDNSConfigSynced {
			if err := c.disableVpcDNS(); err != nil {
				klog.Errorf("failed to clear all vpc-dns, %v", err)
				return
			}
		}
		enableCoreDNS = false
		cmVersion, appliedEnableCoreDNS, vpcDNSConfigSynced = cm.ResourceVersion, false, true
		return
	}

	image := getValue("coredns-image")
	if len(image) == 0 {
		defaultImage, err := c.getDefaultCoreDNSImage()
		if err != nil {
			klog.Errorf("failed to get the default coredns image, %v", err)
			// no VpcDns is deployed with the previous image until the lookup succeeds on a later resync
			corednsImage = ""
			return
		}
		image = defaultImage
		klog.V(3).Infof("use the cluster default coredns image version, %s", image)
	}

	corednsImage = image
	nadName = getValue("nad-name")
	nadProvider = getValue("nad-provider")
	corednsVip = getValue("coredns-vip")
	k8sServiceHost = getValue("k8s-service-host")
	k8sServicePort = getValue("k8s-service-port")
	enableCoreDNS = true

	if err := c.updateVpcDNS(); err != nil {
		klog.Errorf("failed to update vpc-dns deployment")
		return
	}
	cmVersion, appliedEnableCoreDNS, vpcDNSConfigSynced = cm.ResourceVersion, true, true
}

// disableVpcDNS closes the vpc-dns gate before deleting all VpcDns, so that a failed cleanup that is retried later
// does not let any VpcDns be deployed in the meantime
func (c *Controller) disableVpcDNS() error {
	enableCoreDNS, corednsImage = false, ""
	return c.cleanVpcDNS()
}

func (c *Controller) getDefaultCoreDNSImage() (string, error) {
	deployments := c.config.KubeClient.AppsV1().Deployments(metav1.NamespaceSystem)
	dp, err := deployments.Get(context.Background(), "coredns", metav1.GetOptions{})
	if err == nil {
		if image := coreDNSContainerImage(dp); image != "" {
			return image, nil
		}
		return "", errors.New("coredns container not found")
	}
	if !k8serrors.IsNotFound(err) {
		klog.Error(err)
		return "", err
	}

	// some distributions (e.g. RKE2) use another deployment name, so look for the k8s-app=kube-dns label
	list, err := deployments.List(context.Background(), metav1.ListOptions{LabelSelector: CorednsLabelKey + "=kube-dns"})
	if err != nil {
		klog.Error(err)
		return "", err
	}
	// list order is not guaranteed, sort by name to pick the same deployment every time
	slices.SortFunc(list.Items, func(a, b v1.Deployment) int { return strings.Compare(a.Name, b.Name) })
	for i := range list.Items {
		if image := coreDNSContainerImage(&list.Items[i]); image != "" {
			klog.V(3).Infof("use the coredns image of deployment %s/%s", metav1.NamespaceSystem, list.Items[i].Name)
			return image, nil
		}
	}

	err = fmt.Errorf("no deployment named coredns or labeled %s=kube-dns with a %s container in namespace %s",
		CorednsLabelKey, CorednsContainerName, metav1.NamespaceSystem)
	klog.Error(err)
	return "", err
}

func coreDNSContainerImage(dp *v1.Deployment) string {
	for _, container := range dp.Spec.Template.Spec.Containers {
		if container.Name == CorednsContainerName {
			return container.Image
		}
	}
	return ""
}

func (c *Controller) initVpcDNSConfig() error {
	c.resyncVpcDNSConfig()
	return nil
}

func (c *Controller) cleanVpcDNS() error {
	klog.Infof("clear all vpc-dns")
	// list from the API server, not the lister: a VpcDns missing from the informer cache would never be deleted
	list, err := c.config.KubeOvnClient.KubeovnV1().VpcDnses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list vpc-dns, %v", err)
		return err
	}

	// delete one by one: the controller ClusterRole grants delete, not deletecollection, on vpc-dnses
	var errs []error
	for _, vd := range list.Items {
		err := c.config.KubeOvnClient.KubeovnV1().VpcDnses().Delete(context.Background(), vd.Name, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			err = fmt.Errorf("failed to delete vpc-dns %s: %w", vd.Name, err)
			klog.Error(err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
