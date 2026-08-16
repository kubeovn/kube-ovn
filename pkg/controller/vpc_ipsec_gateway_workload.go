package controller

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"text/template"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) ensureIPsecGwConf(gw *kubeovnv1.VpcIPsecGateway) error {
	ns := c.ipsecGwNamespace(gw)
	pskNs, pskKey := util.ResolveIPsecPSKSecretRef(gw.Spec.PSKSecretRef, ns)
	secret, err := c.config.KubeClient.CoreV1().Secrets(pskNs).Get(context.Background(), gw.Spec.PSKSecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get psk secret %s/%s: %w", pskNs, gw.Spec.PSKSecretRef.Name, err)
	}
	if _, ok := secret.Data[pskKey]; !ok || len(secret.Data[pskKey]) == 0 {
		return fmt.Errorf("psk secret %s/%s missing key %q", pskNs, gw.Spec.PSKSecretRef.Name, pskKey)
	}

	localCIDRs, err := c.resolveIPsecLocalCIDRs(gw)
	if err != nil {
		return err
	}

	conf, err := renderSwanctlConf(gw, localCIDRs)
	if err != nil {
		return err
	}

	cmName := util.GenIPsecGwConfName(gw.Name)
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: ns,
			Labels:    util.GenIPsecGwLabels(gw.Name),
		},
		Data: map[string]string{
			"vpc-ipsec.conf": conf,
		},
	}
	if err := util.SetOwnerReference(gw, desired); err != nil {
		return err
	}

	old, err := c.config.KubeClient.CoreV1().ConfigMaps(ns).Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = c.config.KubeClient.CoreV1().ConfigMaps(ns).Create(context.Background(), desired, metav1.CreateOptions{})
			return err
		}
		return err
	}
	if reflect.DeepEqual(old.Data, desired.Data) {
		return nil
	}
	old.Data = desired.Data
	old.Labels = desired.Labels
	_, err = c.config.KubeClient.CoreV1().ConfigMaps(ns).Update(context.Background(), old, metav1.UpdateOptions{})
	return err
}

func renderSwanctlConf(gw *kubeovnv1.VpcIPsecGateway, localCIDRs []string) (string, error) {
	tpl, err := template.New("swanctl").Parse(swanctlConfTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	params := swanctlParams{
		Name:                   gw.Name,
		RemoteEndpoint:         gw.Spec.RemoteEndpoint,
		LocalTrafficSelectors:  strings.Join(localCIDRs, ","),
		RemoteTrafficSelectors: strings.Join(gw.Spec.RemoteCIDRs, ","),
	}
	if err := tpl.Execute(&buf, params); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (c *Controller) resolveIPsecLocalCIDRs(gw *kubeovnv1.VpcIPsecGateway) ([]string, error) {
	if len(gw.Spec.LocalCIDRs) > 0 {
		return slices.Clone(gw.Spec.LocalCIDRs), nil
	}
	subnet, err := c.subnetsLister.Get(gw.Spec.Subnet)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet %s: %w", gw.Spec.Subnet, err)
	}
	cidrs := strings.Split(subnet.Spec.CIDRBlock, ",")
	out := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr != "" {
			out = append(out, cidr)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("subnet %s has empty cidrBlock", gw.Spec.Subnet)
	}
	return out, nil
}

func (c *Controller) ensureIPsecGwStatefulSet(gw *kubeovnv1.VpcIPsecGateway) error {
	if err := util.ValidateIPsecGwStatefulSetNameLength(gw.Name); err != nil {
		return err
	}
	sts, err := c.genIPsecGwStatefulSet(gw)
	if err != nil {
		return err
	}
	ns := c.ipsecGwNamespace(gw)
	old, err := c.config.KubeClient.AppsV1().StatefulSets(ns).Get(context.Background(), sts.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = c.config.KubeClient.AppsV1().StatefulSets(ns).Create(context.Background(), sts, metav1.CreateOptions{})
			return err
		}
		return err
	}

	needUpdate := !reflect.DeepEqual(old.Spec.Template, sts.Spec.Template) ||
		!reflect.DeepEqual(old.Labels, sts.Labels)
	if !needUpdate {
		return nil
	}
	old.Spec.Template = sts.Spec.Template
	old.Labels = sts.Labels
	_, err = c.config.KubeClient.AppsV1().StatefulSets(ns).Update(context.Background(), old, metav1.UpdateOptions{})
	return err
}

func (c *Controller) generateIPsecGwRoutes(
	gw *kubeovnv1.VpcIPsecGateway,
	eth0Provider, eth0V4Gateway, eth0V6Gateway, net1Provider, net1V4Gateway, net1V6Gateway string,
) (map[string]string, error) {
	routes := util.NewPodRoutes()

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != gw.Spec.Vpc || subnet.Name == gw.Spec.Subnet ||
			!isOvnSubnet(subnet) || !subnet.Status.IsValidated() ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.U2OInterconnection) {
			continue
		}
		cidrV4, cidrV6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
		routes.Add(eth0Provider, cidrV4, eth0V4Gateway)
		routes.Add(eth0Provider, cidrV6, eth0V6Gateway)
	}

	routes.Add(net1Provider, "0.0.0.0/0", net1V4Gateway)
	routes.Add(net1Provider, "::/0", net1V6Gateway)

	return routes.ToAnnotations()
}

func (c *Controller) genIPsecGwStatefulSet(gw *kubeovnv1.VpcIPsecGateway) (*v1.StatefulSet, error) {
	externalNadNamespace, externalNadName, err := c.getIPsecExternalSubnetNad(gw)
	if err != nil {
		return nil, err
	}
	eth0SubnetProvider, err := c.GetSubnetProvider(gw.Spec.Subnet)
	if err != nil {
		return nil, err
	}

	templateAnnotations, err := util.GenIPsecGwPodAnnotations(
		gw.Spec.Annotations, gw, externalNadNamespace, externalNadName, eth0SubnetProvider, c.config.EnableNonPrimaryCNI)
	if err != nil {
		return nil, err
	}

	eth0V4Gateway, eth0V6Gateway, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		return nil, err
	}
	net1Subnet, err := c.subnetsLister.Get(gw.Spec.ExternalSubnet)
	if err != nil {
		return nil, err
	}
	net1V4Gateway, net1V6Gateway := util.SplitStringIP(net1Subnet.Spec.Gateway)

	routeAnnotations, err := c.generateIPsecGwRoutes(
		gw, eth0SubnetProvider, eth0V4Gateway, eth0V6Gateway,
		net1Subnet.Spec.Provider, net1V4Gateway, net1V6Gateway)
	if err != nil {
		return nil, err
	}
	maps.Copy(templateAnnotations, routeAnnotations)

	labels := util.GenIPsecGwLabels(gw.Name)
	selectors := util.GenIPsecGwSelectors(gw.Spec.Selector)
	ns := c.ipsecGwNamespace(gw)
	pskNs, pskKey := util.ResolveIPsecPSKSecretRef(gw.Spec.PSKSecretRef, ns)
	if pskNs != ns {
		return nil, fmt.Errorf("psk secret namespace %q must match gateway workload namespace %q", pskNs, ns)
	}

	sts := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenIPsecGwName(gw.Name),
			Namespace: ns,
			Labels:    labels,
		},
		Spec: v1.StatefulSetSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: templateAnnotations,
				},
				Spec: genIPsecGwPodSpec(gw, selectors, pskKey, net1V4Gateway, net1V6Gateway),
			},
			UpdateStrategy: v1.StatefulSetUpdateStrategy{Type: v1.RollingUpdateStatefulSetStrategyType},
		},
	}

	if err := util.SetOwnerReference(gw, sts); err != nil {
		return nil, err
	}
	return sts, nil
}

func genIPsecGwPodSpec(gw *kubeovnv1.VpcIPsecGateway, selectors map[string]string, pskKey, net1V4Gateway, net1V6Gateway string) corev1.PodSpec {
	return corev1.PodSpec{
		TerminationGracePeriodSeconds: ptr.To[int64](0),
		Containers: []corev1.Container{
			{
				Name:            ipsecGwContainerName,
				Image:           vpcIPsecImage,
				Command:         []string{"/kube-ovn/ipsec-gateway.sh", "run"},
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "GATEWAY_V4", Value: net1V4Gateway},
					{Name: "GATEWAY_V6", Value: net1V6Gateway},
					{Name: "PSK_FILE", Value: ipsecGwPSKMountPath + "/psk"},
				},
				SecurityContext: &corev1.SecurityContext{
					Privileged:               ptr.To(true),
					AllowPrivilegeEscalation: ptr.To(true),
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"NET_ADMIN", "NET_RAW"},
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: ipsecGwConfVolume, MountPath: ipsecGwConfMountPath, ReadOnly: true},
					{Name: ipsecGwPSKVolume, MountPath: ipsecGwPSKMountPath, ReadOnly: true},
				},
			},
		},
		NodeSelector: selectors,
		Tolerations:  gw.Spec.Tolerations,
		Affinity:     &gw.Spec.Affinity,
		Volumes: []corev1.Volume{
			{
				Name: ipsecGwConfVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: util.GenIPsecGwConfName(gw.Name)},
					},
				},
			},
			{
				Name: ipsecGwPSKVolume,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: gw.Spec.PSKSecretRef.Name,
						Items: []corev1.KeyToPath{
							{Key: pskKey, Path: "psk"},
						},
					},
				},
			},
		},
	}
}

func (c *Controller) getIPsecExternalSubnetNad(gw *kubeovnv1.VpcIPsecGateway) (string, string, error) {
	externalNadNamespace := c.config.PodNamespace
	externalSubnet, err := c.subnetsLister.Get(gw.Spec.ExternalSubnet)
	if err != nil {
		return "", "", fmt.Errorf("failed to get external subnet %s: %w", gw.Spec.ExternalSubnet, err)
	}
	if name, namespace, ok := util.GetNadBySubnetProvider(externalSubnet.Spec.Provider); ok {
		return namespace, name, nil
	}
	klog.Warningf("subnet %s provider %q cannot be parsed to NAD info, using default NAD %s/%s",
		gw.Spec.ExternalSubnet, externalSubnet.Spec.Provider, externalNadNamespace, gw.Spec.ExternalSubnet)
	return externalNadNamespace, gw.Spec.ExternalSubnet, nil
}
