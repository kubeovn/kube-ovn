package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) ensureVpcWireGuardServerSecret(gw *kubeovnv1.VpcWireGuard) (string, error) {
	ns := c.vpcWireGuardNamespace(gw)
	name := util.GenVpcWireGuardServerSecretName(gw.Name)
	secret, getErr := c.config.KubeClient.CoreV1().Secrets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if getErr != nil && !k8serrors.IsNotFound(getErr) {
		return "", getErr
	}

	var privateKey, publicKey string
	if getErr == nil && len(secret.Data["privateKey"]) > 0 && len(secret.Data["publicKey"]) > 0 {
		return string(secret.Data["publicKey"]), nil
	}

	if gw.Spec.GenerateServerKey {
		var err error
		privateKey, publicKey, err = util.GenerateWireGuardKeyPair()
		if err != nil {
			return "", err
		}
	} else {
		if gw.Spec.PrivateKeySecretRef == nil || gw.Spec.PublicKey == "" {
			return "", errors.New("publicKey and privateKeySecretRef are required when generateServerKey is false")
		}
		refNS := ns
		if gw.Spec.PrivateKeySecretRef.Name == "" {
			return "", errors.New("privateKeySecretRef.name is required")
		}
		src, err := c.config.KubeClient.CoreV1().Secrets(refNS).Get(context.Background(), gw.Spec.PrivateKeySecretRef.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get private key secret: %w", err)
		}
		keyName := gw.Spec.PrivateKeySecretRef.Key
		if keyName == "" {
			keyName = "privateKey"
		}
		raw, ok := src.Data[keyName]
		if !ok || len(raw) == 0 {
			return "", fmt.Errorf("private key %s/%s key %s is empty", refNS, gw.Spec.PrivateKeySecretRef.Name, keyName)
		}
		privateKey = strings.TrimSpace(string(raw))
		publicKey = gw.Spec.PublicKey
		if err := util.ParseWireGuardPublicKey(publicKey); err != nil {
			return "", err
		}
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    util.GenVpcWireGuardLabels(gw.Name),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"privateKey": []byte(privateKey),
			"publicKey":  []byte(publicKey),
		},
	}
	if err := util.SetOwnerReference(gw, obj); err != nil {
		return "", err
	}
	if k8serrors.IsNotFound(getErr) {
		if _, err := c.config.KubeClient.CoreV1().Secrets(ns).Create(context.Background(), obj, metav1.CreateOptions{}); err != nil {
			return "", err
		}
		return publicKey, nil
	}
	secret.Data = obj.Data
	if _, err := c.config.KubeClient.CoreV1().Secrets(ns).Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
		return "", err
	}
	return publicKey, nil
}

func (c *Controller) allocateVpcWireGuardServerIP(gw *kubeovnv1.VpcWireGuard, clientSubnet *kubeovnv1.Subnet) (string, error) {
	nic := util.VpcWireGuardServerIPAMName(gw.Name)
	var v4, v6 string
	var err error
	if gw.Status.ServerTunnelIP != "" {
		v4, v6, _, err = c.ipam.GetStaticAddress(nic, nic, gw.Status.ServerTunnelIP, nil, clientSubnet.Name, true)
	} else {
		v4, v6, _, err = c.ipam.GetRandomAddress(nic, nic, nil, clientSubnet.Name, "", nil, true)
	}
	if err != nil {
		return "", fmt.Errorf("failed to allocate wireguard server tunnel ip: %w", err)
	}
	if v4 != "" {
		return v4, nil
	}
	return v6, nil
}

func (c *Controller) createOrUpdateVpcWireGuardSts(gw *kubeovnv1.VpcWireGuard) error {
	ns := c.vpcWireGuardNamespace(gw)
	name := util.GenVpcWireGuardName(gw.Name)
	oldSts, getErr := c.config.KubeClient.AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if getErr != nil && !k8serrors.IsNotFound(getErr) {
		return getErr
	}
	newSts, err := c.genVpcWireGuardStatefulSet(gw)
	if err != nil {
		return err
	}
	if k8serrors.IsNotFound(getErr) {
		_, err = c.config.KubeClient.AppsV1().StatefulSets(ns).Create(context.Background(), newSts, metav1.CreateOptions{})
		return err
	}
	newSts.ResourceVersion = oldSts.ResourceVersion
	_, err = c.config.KubeClient.AppsV1().StatefulSets(ns).Update(context.Background(), newSts, metav1.UpdateOptions{})
	return err
}

func (c *Controller) genVpcWireGuardStatefulSet(gw *kubeovnv1.VpcWireGuard) (*v1.StatefulSet, error) {
	provider, err := c.GetSubnetProvider(gw.Spec.Subnet)
	if err != nil {
		return nil, err
	}

	var externalNadNamespace, externalNadName string
	if gw.Spec.Exposure.Type == kubeovnv1.VpcWireGuardExposureDualNIC {
		externalNadNamespace, externalNadName, err = c.getWireGuardExternalSubnetNad(gw)
		if err != nil {
			return nil, err
		}
	}

	annotations, err := util.GenVpcWireGuardPodAnnotations(gw, externalNadNamespace, externalNadName, provider, c.config.EnableNonPrimaryCNI)
	if err != nil {
		return nil, err
	}

	if gw.Spec.Exposure.Type == kubeovnv1.VpcWireGuardExposureDualNIC {
		routeAnn, err := c.generateVpcWireGuardRoutes(gw, provider)
		if err != nil {
			return nil, err
		}
		for k, v := range routeAnn {
			annotations[k] = v
		}
	}

	labels := util.GenVpcWireGuardLabels(gw.Name)
	ns := c.vpcWireGuardNamespace(gw)
	sts := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenVpcWireGuardName(gw.Name),
			Namespace: ns,
			Labels:    labels,
		},
		Spec: v1.StatefulSetSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					Containers: []corev1.Container{
						{
							Name:            util.VpcWireGuardContainer,
							Image:           vpcNatImage,
							Command:         []string{"bash", "-c", vpcWireGuardContainerCommand},
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts: []corev1.VolumeMount{{
								Name:      "wg-config",
								MountPath: "/etc/wireguard",
								ReadOnly:  true,
							}},
							SecurityContext: &corev1.SecurityContext{
								Privileged:               ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(true),
							},
						},
					},
					Volumes: []corev1.Volume{{
						Name: "wg-config",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: util.GenVpcWireGuardServerSecretName(gw.Name),
								Optional:   ptr.To(false),
							},
						},
					}},
					NodeSelector: util.GenNatGwSelectors(gw.Spec.Selector),
					Tolerations:  gw.Spec.Tolerations,
					Affinity:     &gw.Spec.Affinity,
				},
			},
			UpdateStrategy: v1.StatefulSetUpdateStrategy{Type: v1.RollingUpdateStatefulSetStrategyType},
		},
	}
	if err := util.SetOwnerReference(gw, sts); err != nil {
		return nil, err
	}
	return sts, nil
}

func (c *Controller) getWireGuardExternalSubnetNad(gw *kubeovnv1.VpcWireGuard) (string, string, error) {
	externalNadNamespace := c.config.PodNamespace
	externalSubnetName := util.GetNatGwExternalNetwork(gw.Spec.Exposure.ExternalSubnets)
	externalSubnet, err := c.subnetsLister.Get(externalSubnetName)
	if err != nil {
		return "", "", fmt.Errorf("failed to get external subnet %s: %w", externalSubnetName, err)
	}
	if name, namespace, ok := util.GetNadBySubnetProvider(externalSubnet.Spec.Provider); ok {
		return namespace, name, nil
	}
	return externalNadNamespace, externalSubnetName, nil
}

func (c *Controller) generateVpcWireGuardRoutes(gw *kubeovnv1.VpcWireGuard, eth0Provider string) (map[string]string, error) {
	eth0V4Gateway, eth0V6Gateway, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		return nil, err
	}
	net1Subnet, err := c.subnetsLister.Get(util.GetNatGwExternalNetwork(gw.Spec.Exposure.ExternalSubnets))
	if err != nil {
		return nil, err
	}
	net1V4Gateway, net1V6Gateway := util.SplitStringIP(net1Subnet.Spec.Gateway)
	routes := util.NewPodRoutes()
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != gw.Spec.Vpc || subnet.Name == gw.Spec.Subnet || !isOvnSubnet(subnet) {
			continue
		}
		cidrV4, cidrV6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
		routes.Add(eth0Provider, cidrV4, eth0V4Gateway)
		routes.Add(eth0Provider, cidrV6, eth0V6Gateway)
	}
	routes.Add(net1Subnet.Spec.Provider, "0.0.0.0/0", net1V4Gateway)
	routes.Add(net1Subnet.Spec.Provider, "::/0", net1V6Gateway)
	return routes.ToAnnotations()
}

func (c *Controller) getVpcWireGuardLanIP(gw *kubeovnv1.VpcWireGuard) (string, error) {
	if gw.Spec.LanIP != "" {
		v4, _ := util.SplitStringIP(gw.Spec.LanIP)
		if v4 != "" {
			return v4, nil
		}
		return gw.Spec.LanIP, nil
	}
	pod, err := c.getVpcWireGuardPod(gw)
	if err != nil {
		return "", err
	}
	provider, err := c.GetSubnetProvider(gw.Spec.Subnet)
	if err != nil {
		return "", err
	}
	if provider == "" {
		provider = util.OvnProvider
	}
	ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
	if ip == "" {
		return "", fmt.Errorf("wireguard pod %s/%s has no LAN IP yet", pod.Namespace, pod.Name)
	}
	v4, _ := util.SplitStringIP(ip)
	if v4 != "" {
		return v4, nil
	}
	return ip, nil
}

func (c *Controller) getVpcWireGuardPod(gw *kubeovnv1.VpcWireGuard) (*corev1.Pod, error) {
	pod, err := c.podsLister.Pods(c.vpcWireGuardNamespace(gw)).Get(util.GenVpcWireGuardPodName(gw.Name))
	if err != nil {
		return nil, fmt.Errorf("failed to get wireguard pod: %w", err)
	}
	return pod, nil
}

func (c *Controller) reconcileVpcWireGuardRoutes(gw *kubeovnv1.VpcWireGuard, clientSubnet *kubeovnv1.Subnet, lanIP string) error {
	if lanIP == "" {
		return errors.New("lan ip is empty")
	}
	externalIDs := vpcWireGuardRouteExternalIDs(gw.Name)
	existed, err := c.OVNNbClient.ListLogicalRouterStaticRoutes(gw.Spec.Vpc, nil, nil, "", externalIDs)
	if err != nil {
		return err
	}
	desired := map[string]string{}
	v4cidr, v6cidr := util.SplitStringIP(clientSubnet.Spec.CIDRBlock)
	if v4cidr != "" {
		desired[v4cidr] = lanIP
	}
	if v6cidr != "" {
		desired[v6cidr] = lanIP
	}
	for _, route := range existed {
		if next, ok := desired[route.IPPrefix]; ok && route.Nexthop == next {
			delete(desired, route.IPPrefix)
			continue
		}
		if err := c.OVNNbClient.DeleteLogicalRouterStaticRouteByUUID(gw.Spec.Vpc, route.UUID); err != nil {
			return err
		}
	}
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	for cidr, nextHop := range desired {
		if err := c.OVNNbClient.AddLogicalRouterStaticRoute(gw.Spec.Vpc, "", policy, cidr, nil, externalIDs, nextHop); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) deleteVpcWireGuardRoutes(gw *kubeovnv1.VpcWireGuard) error {
	existed, err := c.OVNNbClient.ListLogicalRouterStaticRoutes(gw.Spec.Vpc, nil, nil, "", vpcWireGuardRouteExternalIDs(gw.Name))
	if err != nil {
		return err
	}
	for _, route := range existed {
		if err := c.OVNNbClient.DeleteLogicalRouterStaticRouteByUUID(gw.Spec.Vpc, route.UUID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileVpcWireGuardExposure(gw *kubeovnv1.VpcWireGuard, lanIP string) (string, error) {
	port := util.DefaultVpcWireGuardListenPort(gw.Spec.ListenPort)
	switch gw.Spec.Exposure.Type {
	case kubeovnv1.VpcWireGuardExposureDualNIC:
		return c.vpcWireGuardDualNICEndpoint(gw, port)
	case kubeovnv1.VpcWireGuardExposureDNAT:
		return c.ensureVpcWireGuardDnat(gw, lanIP, port)
	case kubeovnv1.VpcWireGuardExposureFIP:
		return c.ensureVpcWireGuardFip(gw, lanIP, port)
	default:
		return "", fmt.Errorf("unsupported exposure type %s", gw.Spec.Exposure.Type)
	}
}

func (c *Controller) vpcWireGuardDualNICEndpoint(gw *kubeovnv1.VpcWireGuard, port int32) (string, error) {
	if gw.Spec.Exposure.EIP != "" {
		eip, err := c.iptablesEipsLister.Get(gw.Spec.Exposure.EIP)
		if err != nil {
			return "", err
		}
		if eip.Status.IP != "" {
			return fmt.Sprintf("%s:%d", eip.Status.IP, port), nil
		}
		if eip.Spec.V4ip != "" {
			return fmt.Sprintf("%s:%d", eip.Spec.V4ip, port), nil
		}
	}
	pod, err := c.getVpcWireGuardPod(gw)
	if err != nil {
		return "", err
	}
	externalSubnet, err := c.subnetsLister.Get(util.GetNatGwExternalNetwork(gw.Spec.Exposure.ExternalSubnets))
	if err != nil {
		return "", err
	}
	provider := externalSubnet.Spec.Provider
	if provider == "" {
		provider = util.OvnProvider
	}
	ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
	if ip == "" {
		return "", errors.New("wireguard pod has no external IP yet")
	}
	v4, _ := util.SplitStringIP(ip)
	if v4 == "" {
		v4 = ip
	}
	return fmt.Sprintf("%s:%d", v4, port), nil
}

func (c *Controller) ensureVpcWireGuardDnat(gw *kubeovnv1.VpcWireGuard, lanIP string, port int32) (string, error) {
	if gw.Spec.Exposure.EIP == "" {
		return "", errors.New("exposure.eip is required for DNAT")
	}
	eip, err := c.iptablesEipsLister.Get(gw.Spec.Exposure.EIP)
	if err != nil {
		return "", err
	}
	name := util.GenVpcWireGuardDnatName(gw.Name)
	spec := kubeovnv1.IptablesDnatRuleSpec{
		EIP:          gw.Spec.Exposure.EIP,
		ExternalPort: strconv.Itoa(int(port)),
		InternalPort: strconv.Itoa(int(port)),
		InternalIP:   lanIP,
		Protocol:     "udp",
		Type:         kubeovnv1.DnatRuleTypeExclusive,
	}
	existing, err := c.iptablesDnatRulesLister.Get(name)
	if err != nil && !k8serrors.IsNotFound(err) {
		return "", err
	}
	if k8serrors.IsNotFound(err) {
		rule := &kubeovnv1.IptablesDnatRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: util.GenVpcWireGuardLabels(gw.Name)},
			Spec:       spec,
		}
		if err := util.SetOwnerReference(gw, rule); err != nil {
			return "", err
		}
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Create(context.Background(), rule, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
			return "", err
		}
	} else if existing.Spec != spec {
		updated := existing.DeepCopy()
		updated.Spec = spec
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
			return "", err
		}
	}
	ip := eip.Status.IP
	if ip == "" {
		ip = eip.Spec.V4ip
	}
	if ip == "" {
		return "", fmt.Errorf("eip %s has no IP yet", eip.Name)
	}
	return fmt.Sprintf("%s:%d", ip, port), nil
}

func (c *Controller) ensureVpcWireGuardFip(gw *kubeovnv1.VpcWireGuard, lanIP string, port int32) (string, error) {
	if gw.Spec.Exposure.EIP == "" {
		return "", errors.New("exposure.eip is required for FIP")
	}
	eip, err := c.iptablesEipsLister.Get(gw.Spec.Exposure.EIP)
	if err != nil {
		return "", err
	}
	name := util.GenVpcWireGuardFipName(gw.Name)
	spec := kubeovnv1.IptablesFIPRuleSpec{EIP: gw.Spec.Exposure.EIP, InternalIP: lanIP}
	existing, err := c.iptablesFipsLister.Get(name)
	if err != nil && !k8serrors.IsNotFound(err) {
		return "", err
	}
	if k8serrors.IsNotFound(err) {
		rule := &kubeovnv1.IptablesFIPRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: util.GenVpcWireGuardLabels(gw.Name)},
			Spec:       spec,
		}
		if err := util.SetOwnerReference(gw, rule); err != nil {
			return "", err
		}
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Create(context.Background(), rule, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
			return "", err
		}
	} else if existing.Spec != spec {
		updated := existing.DeepCopy()
		updated.Spec = spec
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
			return "", err
		}
	}
	ip := eip.Status.IP
	if ip == "" {
		ip = eip.Spec.V4ip
	}
	if ip == "" {
		return "", fmt.Errorf("eip %s has no IP yet", eip.Name)
	}
	return fmt.Sprintf("%s:%d", ip, port), nil
}

const vpcWireGuardContainerCommand = `set -euo pipefail
until [ -s /etc/wireguard/wg0.conf ]; do sleep 1; done
bash /kube-ovn/wireguard.sh init
exec sleep infinity
`

func (c *Controller) writeVpcWireGuardServerConfig(gw *kubeovnv1.VpcWireGuard, serverTunnelIP, clientCIDR string) error {
	ns := c.vpcWireGuardNamespace(gw)
	secretName := util.GenVpcWireGuardServerSecretName(gw.Name)
	secret, err := c.config.KubeClient.CoreV1().Secrets(ns).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	privateKey := string(secret.Data["privateKey"])
	if privateKey == "" {
		return errors.New("server private key is empty")
	}
	address, err := util.GetIPAddrWithMask(serverTunnelIP, clientCIDR)
	if err != nil {
		address = serverTunnelIP
	}
	peers, err := c.listVpcWireGuardPeerConfigs(gw.Name)
	if err != nil {
		return err
	}
	conf := util.RenderWireGuardServerConfig(privateKey, address, util.DefaultVpcWireGuardListenPort(gw.Spec.ListenPort), gw.Spec.MTU, peers)
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	if string(secret.Data["wg0.conf"]) == conf {
		return nil
	}
	secret.Data["wg0.conf"] = []byte(conf)
	_, err = c.config.KubeClient.CoreV1().Secrets(ns).Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}

func (c *Controller) syncVpcWireGuardDataplane(gw *kubeovnv1.VpcWireGuard, serverTunnelIP, clientCIDR string) error {
	if err := c.writeVpcWireGuardServerConfig(gw, serverTunnelIP, clientCIDR); err != nil {
		return err
	}
	pod, err := c.getVpcWireGuardPod(gw)
	if err != nil {
		return err
	}
	if pod.Status.Phase != corev1.PodRunning {
		return errors.New("wireguard pod is not running")
	}
	if _, _, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, util.VpcWireGuardContainer, "bash", "/kube-ovn/wireguard.sh", "sync"); err != nil {
		return fmt.Errorf("failed to sync wireguard dataplane: %w", err)
	}
	if _, _, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, util.VpcWireGuardContainer, "ip", "link", "show", "wg0"); err != nil {
		return fmt.Errorf("wireguard interface wg0 is not up: %w", err)
	}
	return nil
}

func (c *Controller) listVpcWireGuardPeerConfigs(gwName string) ([]util.WireGuardPeerConfig, error) {
	peers, err := c.vpcWireGuardPeerLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	result := make([]util.WireGuardPeerConfig, 0)
	for _, peer := range peers {
		if peer.Spec.WireGuard != gwName || !peer.Status.Ready || peer.Status.PublicKey == "" || peer.Status.ClientIP == "" {
			continue
		}
		cfg := util.WireGuardPeerConfig{
			PublicKey:  peer.Status.PublicKey,
			AllowedIPs: peer.Status.ClientIP + "/32",
		}
		if peer.Spec.PresharedKeySecretRef != nil {
			psk, err := c.readPeerPresharedKey(peer)
			if err != nil {
				klog.Warningf("skip psk for peer %s: %v", peer.Name, err)
			} else {
				cfg.PresharedKey = psk
			}
		}
		result = append(result, cfg)
	}
	return result, nil
}

func (c *Controller) enqueueVpcWireGuardPeers(gwName string) {
	peers, err := c.vpcWireGuardPeerLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list wireguard peers: %v", err)
		return
	}
	for _, peer := range peers {
		if peer.Spec.WireGuard == gwName {
			c.addOrUpdateVpcWireGuardPeerQueue.Add(peer.Name)
		}
	}
}
