package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"io/ioutil"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
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
	once            = sync.Once{}
)

const (
	CorednsContainerName = "coredns"
	CorednsConfigDir     = "/kube-ovn/coredns"
	CorednsLabelKey      = "k8s-app"
	CorednsCMFile        = "coredns-cm.yaml"
	CorednsCRFile        = "coredns-cr.yaml"
	CorednsCRBFile       = "coredns-crb.yaml"
	CorednsSAFile        = "coredns-sa.yaml"
	CorednsDepFile       = "coredns-dep.yaml"
	InitRouteImage       = "busybox:stable"
)

func genVpcDnsDpName(name string) string {
	return fmt.Sprintf("vpc-dns-%s", name)
}

func hostConfigFromReader() error {
	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
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

func (c *Controller) enqueueAddVpcDns(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vpc-dns %s", key)
	c.addOrUpdateVpcDnsQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcDns(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}

	oldVpcDns := old.(*kubeovnv1.VpcDns)
	newVpcDns := new.(*kubeovnv1.VpcDns)
	if oldVpcDns.ResourceVersion != newVpcDns.ResourceVersion &&
		!reflect.DeepEqual(oldVpcDns.Spec, newVpcDns.Spec) {
		klog.V(3).Infof("enqueue update vpc-dns %s", key)
		c.addOrUpdateVpcDnsQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVpcDns(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete vpc-dns %s", key)
	c.delVpcDnsQueue.Add(key)
}

func (c *Controller) runAddOrUpdateVpcDnsWorker() {
	for c.processNextWorkItem("addOrUpdateVpcDns", c.addOrUpdateVpcDnsQueue, c.handleAddOrUpdateVpcDns) {
	}
}

func (c *Controller) runDelVpcDnsWorker() {
	for c.processNextWorkItem("delVpcDns", c.delVpcDnsQueue, c.handleDelVpcDns) {
	}
}

func (c *Controller) handleAddOrUpdateVpcDns(key string) error {
	klog.V(3).Infof("handleAddOrUpdateVpcDns %s", key)
	if !enableCoredns {
		time.Sleep(10 * time.Second)
		if !enableCoredns {
			return fmt.Errorf("failed to  add/update vpc-dns, enable ='%v'", enableCoredns)
		}
	}

	vpcDns, err := c.vpcDnsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	defer func() {
		newVpcDns := vpcDns.DeepCopy()
		newVpcDns.Status.Active = true
		if err != nil {
			newVpcDns.Status.Active = false
		}

		_, err = c.config.KubeOvnClient.KubeovnV1().VpcDnses().UpdateStatus(context.Background(),
			newVpcDns, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("update vpc-dns status failed, %v", err)
		}
	}()

	vpcDnsList, err := c.vpcDnsLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if len(corednsImage) == 0 {
		klog.Errorf("failed to get the vpc-dns coredns image parameter")
	}

	if len(corednsVip) == 0 {
		err := fmt.Errorf("the configuration parameter corednsVip is empty")
		klog.Errorf("failed to get corednsVip, err: %s", err)
		return err
	}

	if _, err := c.vpcsLister.Get(vpcDns.Spec.Vpc); err != nil {
		klog.Errorf("failed to get vpc '%s', err: %v", vpcDns.Spec.Vpc, err)
		return err
	}

	if _, err := c.subnetsLister.Get(vpcDns.Spec.Subnet); err != nil {
		klog.Errorf("failed to get subnet '%s', err: %v", vpcDns.Spec.Subnet, err)
		return err
	}

	for _, item := range vpcDnsList {
		if item.Status.Active &&
			item.Name != vpcDns.Name &&
			item.Spec.Vpc == vpcDns.Spec.Vpc {
			err = fmt.Errorf("only one vpc-dns can be deployed in a vpc")
			klog.Errorf("failed to deploy %s, %v", key, err)
			return err
		}
	}

	if err := c.checkOvnNad(); err != nil {
		klog.Errorf("failed to check nad, %v", err)
		return err
	}

	if err := c.checkOvnProvided(); err != nil {
		klog.Errorf("failed to check %s provided, %v", util.DefaultSubnet, err)
		return err
	}

	needToCreateDp := false
	oldDp, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
		Get(context.Background(), genVpcDnsDpName(vpcDns.Name), metav1.GetOptions{})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateDp = true
		} else {
			return err
		}
	}

	newDp, err := c.genVpcDnsDeployment(vpcDns, oldDp)
	if err != nil {
		klog.Errorf("failed to generate vpc-dns deployment, %v", err)
		return err
	}

	if needToCreateDp {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Create(context.Background(), newDp, metav1.CreateOptions{})

		if err != nil {
			klog.Errorf("failed to create deployment '%s', err: %s", newDp.Name, err)
			return err
		}
		klog.Infof("%s Deployment is successfully deployed", newDp.Name)
	} else {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Update(context.Background(), newDp, metav1.UpdateOptions{})

		if err != nil {
			klog.Errorf("failed to update deployment '%s', err: %v", newDp.Name, err)
			return err
		}

		klog.Infof("%s Deployment is successfully updated", newDp.Name)
	}

	needToCreateSvc := false
	oldSlr, err := c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Get(context.Background(),
		genVpcDnsDpName(vpcDns.Name), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateSvc = true
		} else {
			return err
		}
	}

	newSlr, err := c.genVpcDnsSlr(vpcDns.Name, c.config.PodNamespace)

	if needToCreateSvc {
		_, err := c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Create(context.Background(), newSlr, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("failed to create deployment '%s', err: %v", newDp.Name, err)
			return err
		}
		klog.V(3).Infof("%s SwitchLBRule is successfully deployed", newSlr.Name)
	} else {
		if reflect.DeepEqual(oldSlr.Spec, newSlr.Spec) {
			return nil
		}

		newSlr.ResourceVersion = oldSlr.ResourceVersion
		_, err := c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Update(context.Background(), newSlr, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update deployment '%s', err: %v", newDp.Name, err)
			return err
		}
		klog.V(3).Infof("%s SwitchLBRule is successfully updated", newSlr.Name)
	}

	return nil
}

func (c *Controller) handleDelVpcDns(key string) error {
	klog.V(3).Infof("handleDelVpcDns,%s", key)
	_, err := c.vpcDnsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			name := genVpcDnsDpName(key)
			err = c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(), name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete Deployments: %v", err)
				return err
			}

			err = c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Delete(context.Background(), name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete SwitchLBRule: %v", err)
				return err
			}
		}
		return err
	}
	return nil
}

func (c *Controller) genVpcDnsDeployment(vpcDns *kubeovnv1.VpcDns, oldDeploy *v1.Deployment) (*v1.Deployment, error) {
	filePath := path.Join(CorednsConfigDir, CorednsDepFile)
	tmp, err := template.ParseFiles(filePath)
	if err != nil {
		return nil, err
	}

	buffer := new(bytes.Buffer)
	name := genVpcDnsDpName(vpcDns.Name)
	if err := tmp.Execute(buffer, map[string]interface{}{
		"DeployName":   name,
		"CorednsImage": corednsImage,
	}); err != nil {
		return nil, err
	}

	dep := &v1.Deployment{}
	retJson, err := yaml.ToJSON(buffer.Bytes())
	if err != nil {
		klog.Errorf("failed to switch yaml, %v", err)
		return nil, err
	}

	if err := json.Unmarshal(retJson, dep); err != nil {
		klog.Errorf("failed to switch json, %v", err)
		return nil, err
	}

	dep.Spec.Template.Annotations = make(map[string]string)

	if oldDeploy != nil && len(oldDeploy.Annotations) != 0 {
		dep.Spec.Template.Annotations = oldDeploy.Annotations
	}

	setCoreDnsEnv(dep)
	setVpcDnsInterface(dep, vpcDns.Spec.Subnet)

	defaultSubnet, err := c.subnetsLister.Get(util.DefaultSubnet)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return nil, err
	}

	setVpcDnsNetwork(dep, defaultSubnet.Spec.Gateway)
	return dep, nil
}

func (c *Controller) genVpcDnsSlr(vpcName, namespace string) (*kubeovnv1.SwitchLBRule, error) {
	name := genVpcDnsDpName(vpcName)
	label := fmt.Sprintf("%s:%s", CorednsLabelKey, name)

	ports := []kubeovnv1.SlrPort{
		{Name: "dns", Port: 53, Protocol: "UDP"},
		{Name: "dns-tcp", Port: 53, Protocol: "TCP"},
		{Name: "metrics", Port: 9153, Protocol: "TCP"},
	}

	slr := &kubeovnv1.SwitchLBRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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

func setVpcDnsInterface(dp *v1.Deployment, subnetName string) {
	annotations := dp.Spec.Template.Annotations
	annotations[util.LogicalSwitchAnnotation] = subnetName
	annotations[util.AttachmentNetworkAnnotation] = fmt.Sprintf("%s/%s", corev1.NamespaceDefault, nadName)
	annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, nadProvider)] = util.DefaultSubnet
}

func setCoreDnsEnv(dp *v1.Deployment) {
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

func setVpcDnsNetwork(dp *v1.Deployment, subnetGw string) {
	var serviceHost string
	if len(k8sServiceHost) == 0 {
		serviceHost = "${KUBERNETES_SERVICE_HOST}"
	} else {
		serviceHost = k8sServiceHost
	}

	var routeCmd string
	routeCmd = fmt.Sprintf("ip route add %s via %s dev net1;", serviceHost, subnetGw)
	for _, nameserver := range hostNameservers {
		routeCmd += fmt.Sprintf("ip route add %s via %s dev net1;", nameserver, subnetGw)
	}

	privileged := true
	allowPrivilegeEscalation := true
	dp.Spec.Template.Spec.InitContainers = append(dp.Spec.Template.Spec.InitContainers, corev1.Container{
		Name:            "init-route",
		Image:           InitRouteImage,
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
		return err
	}

	return nil
}

func (c *Controller) checkOvnProvided() error {
	cachedSubnet, err := c.subnetsLister.Get(util.DefaultSubnet)
	if err != nil {
		return fmt.Errorf("failed to get default subnet %v", err)
	}

	if cachedSubnet.Spec.Provider != nadProvider {
		return fmt.Errorf("the %s provider does not exist", nadProvider)
	}

	return nil
}

func (c *Controller) resyncVpcDnsConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcDnsConfig)
	if err != nil {
		klog.Errorf("failed to get %s, %v", util.VpcDnsConfig, err)
		return
	}

	if cmVersion == cm.ResourceVersion {
		return
	}
	cmVersion = cm.ResourceVersion
	klog.V(3).Infof("The vpc-dns ConfigMap update")

	setValue := func(key string) string {
		if v, ok := cm.Data[key]; ok {
			return v
		}
		return ""
	}

	corednsImage = setValue("coredns-image")
	if len(corednsImage) == 0 {
		dp, err := c.config.KubeClient.AppsV1().Deployments("kube-system").
			Get(context.Background(), "coredns", metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get kube-system/coredns info, %v", err)
			return
		}

		for _, container := range dp.Spec.Template.Spec.Containers {
			if container.Name == CorednsContainerName {
				corednsImage = container.Image
				klog.Infof("use the cluster default coredns image version, %s", corednsImage)
				break
			}
		}
	}

	nadName = setValue("nad-name")
	nadProvider = setValue("nad-provider")
	corednsVip = setValue("coredns-vip")
	k8sServiceHost = setValue("k8s-service-host")
	k8sServicePort = setValue("k8s-service-port")

	newEnableCoredns := true
	if v, ok := cm.Data["enable-vpc-dns"]; ok {
		raw, err := strconv.ParseBool(v)
		if err != nil {
			klog.Errorf("failed to parse cm enable, %v", err)
			return
		}
		newEnableCoredns = raw
	}

	if !enableCoredns && newEnableCoredns {
		once.Do(func() {
			if err := c.initCorednsResource(); err != nil {
				klog.Errorf("failed to init coredns resource")
			}
			klog.Errorf("init coredns resource succeeded")
		})
	}

	if enableCoredns && !newEnableCoredns {
		if err := c.cleanVpcDns(); err != nil {
			klog.Errorf("failed to clear all vpc-dns, %v", err)
		}
	} else {
		if err := c.updateVpcDns(); err != nil {
			klog.Errorf("failed to update vpc-dns deployment")
		}
	}

	enableCoredns = newEnableCoredns
}

func (c *Controller) initVpcDnsConfig() error {
	if err := hostConfigFromReader(); err != nil {
		klog.Errorf("failed to get get host nameserver, %v", err)
		return err
	}
	c.resyncVpcDnsConfig()

	return nil
}

func (c *Controller) cleanVpcDns() error {
	klog.Infof("clear all vpc-dns")
	err := c.config.KubeOvnClient.KubeovnV1().VpcDnses().DeleteCollection(context.Background(), metav1.DeleteOptions{},
		metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to clear all vpc-dns %s", err)
	}

	return nil
}

func (c *Controller) updateVpcDns() error {
	list, err := c.vpcDnsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vpc-dns list, %s", err)
		return err
	}

	for _, vd := range list {
		c.addOrUpdateVpcDnsQueue.Add(vd.Name)
	}
	return nil
}

func parseYamlToResource(file string, any interface{}) error {
	filePath := path.Join(CorednsConfigDir, file)
	fileBytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		klog.Errorf("failed to read %s, %v", file, err)
		return err
	}

	yamlBytes, err := yaml.ToJSON(fileBytes)
	if err != nil {
		klog.Errorf("failed to switch yaml, %v", err)
		return err
	}

	if err := json.Unmarshal(yamlBytes, any); err != nil {
		klog.Errorf("failed to switch json, %v", err)
		return err
	}

	return nil
}

func (c *Controller) initCorednsResource() error {
	cr := &rbacv1.ClusterRole{}
	if err := parseYamlToResource(CorednsCRFile, cr); err != nil {
		klog.Errorf("failed to develop %s,%s", CorednsCRFile, err)
		return err
	}

	_, err := c.config.KubeClient.RbacV1().ClusterRoles().Get(context.Background(), cr.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = c.config.KubeClient.RbacV1().ClusterRoles().Create(context.Background(), cr, metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("failed to create vpc-dns clusterRoles:%s, %s", cr.Name, err)
				klog.Errorf(err.Error())
				return err
			}
		} else {
			klog.Errorf("failed to get vpc-dns clusterRoles:%s, %s", cr.Name, err)
			return err
		}
	}

	crb := &rbacv1.ClusterRoleBinding{}
	if err := parseYamlToResource(CorednsCRBFile, crb); err != nil {
		klog.Errorf("failed to develop %s,%s", CorednsCRBFile, err)
		return err
	}

	_, err = c.config.KubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), crb.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = c.config.KubeClient.RbacV1().ClusterRoleBindings().Create(context.Background(), crb, metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("failed to create vpc-dns clusterRoleBindings:%s, %s", crb.Name, err)
				return err
			}
		} else {
			klog.Errorf("failed to get vpc-dns clusterRoleBindings:%s, %s", crb.Name, err)
			return err
		}
	}

	sa := &corev1.ServiceAccount{}
	if err := parseYamlToResource(CorednsSAFile, sa); err != nil {
		klog.Errorf("failed to develop %s,%s", CorednsSAFile, err)
		return err
	}

	_, err = c.config.KubeClient.CoreV1().ServiceAccounts(c.config.PodNamespace).Get(context.Background(), sa.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = c.config.KubeClient.CoreV1().ServiceAccounts(c.config.PodNamespace).Create(context.Background(), sa,
				metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("failed to create vpc-dns serviceAccounts:%s, %s", sa.Name, err)
				return err
			}
		} else {
			klog.Errorf("failed to get vpc-dns serviceAccounts:%s, %s", sa.Name, err)
			return err
		}
	}

	cm := &corev1.ConfigMap{}
	if err := parseYamlToResource(CorednsCMFile, cm); err != nil {
		klog.Errorf("failed to develop %s,%s", CorednsCMFile, err)
		return err
	}

	_, err = c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Get(context.Background(), cm.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Create(context.Background(), cm,
				metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("failed to create vpc-dns configmap:%s, %s", cm.Name, err)
				return err
			}
		} else {
			klog.Errorf("failed to get vpc-dns configmap:%s, %s", cm.Name, err)
			return err
		}
	}
	return nil
}
