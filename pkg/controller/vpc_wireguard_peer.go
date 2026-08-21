package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVpcWireGuardPeer(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcWireGuardPeer)).String()
	klog.V(3).Infof("enqueue add vpc-wireguard-peer %s", key)
	c.addOrUpdateVpcWireGuardPeerQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcWireGuardPeer(oldObj, newObj any) {
	oldPeer := oldObj.(*kubeovnv1.VpcWireGuardPeer)
	newPeer := newObj.(*kubeovnv1.VpcWireGuardPeer)
	if !newPeer.DeletionTimestamp.IsZero() || !reflect.DeepEqual(oldPeer.Spec, newPeer.Spec) {
		key := cache.MetaObjectToName(newPeer).String()
		klog.V(3).Infof("enqueue update vpc-wireguard-peer %s", key)
		c.addOrUpdateVpcWireGuardPeerQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVpcWireGuardPeer(obj any) {
	var peer *kubeovnv1.VpcWireGuardPeer
	switch t := obj.(type) {
	case *kubeovnv1.VpcWireGuardPeer:
		peer = t
	case cache.DeletedFinalStateUnknown:
		p, ok := t.Obj.(*kubeovnv1.VpcWireGuardPeer)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		peer = p
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}
	c.delVpcWireGuardPeerQueue.Add(peer.Name)
}

func (c *Controller) handleAddOrUpdateVpcWireGuardPeer(key string) error {
	c.vpcWireGuardPeerKeyMutex.LockKey(key)
	defer func() { _ = c.vpcWireGuardPeerKeyMutex.UnlockKey(key) }()

	peer, err := c.vpcWireGuardPeerLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !peer.DeletionTimestamp.IsZero() {
		return c.cleanupVpcWireGuardPeer(peer)
	}
	if err := c.ensureVpcWireGuardPeerFinalizer(peer); err != nil {
		return err
	}
	peer, err = c.vpcWireGuardPeerLister.Get(key)
	if err != nil {
		return err
	}

	gw, err := c.vpcWireGuardLister.Get(peer.Spec.WireGuard)
	if err != nil {
		return fmt.Errorf("failed to get vpc wireguard %s: %w", peer.Spec.WireGuard, err)
	}
	if !gw.Status.Ready {
		return fmt.Errorf("vpc wireguard %s is not ready", gw.Name)
	}
	clientSubnet, err := c.subnetsLister.Get(gw.Spec.ClientSubnet)
	if err != nil {
		return err
	}

	clientIP, err := c.allocateVpcWireGuardPeerIP(peer, clientSubnet)
	if err != nil {
		return err
	}
	publicKey, privateKey, err := c.ensureVpcWireGuardPeerKeys(peer, gw)
	if err != nil {
		return err
	}

	allowedIPs, err := c.vpcWireGuardClientAllowedIPs(gw)
	if err != nil {
		return err
	}
	address, err := util.GetIPAddrWithMask(clientIP, clientSubnet.Spec.CIDRBlock)
	if err != nil {
		address = clientIP
	}
	psk, err := c.readPeerPresharedKey(peer)
	if err != nil {
		return err
	}

	configSecret := ""
	if peer.Spec.GenerateKey {
		configSecret = util.GenVpcWireGuardPeerSecretName(peer.Name)
		if err := c.writePeerConfigSecret(peer, gw, configSecret, privateKey, publicKey, address, allowedIPs, psk); err != nil {
			return err
		}
	}

	newPeer := peer.DeepCopy()
	newPeer.Status.Ready = true
	newPeer.Status.ClientIP = clientIP
	newPeer.Status.PublicKey = publicKey
	newPeer.Status.ServerPublicKey = gw.Status.PublicKey
	newPeer.Status.Endpoint = gw.Status.Endpoint
	newPeer.Status.ConfigSecret = configSecret
	conds := kubeovnv1.Conditions(newPeer.Status.Conditions)
	conds.SetReady("PeerReady", newPeer.Generation)
	newPeer.Status.Conditions = conds
	changed := peer.Status.ClientIP != clientIP || peer.Status.PublicKey != publicKey || !peer.Status.Ready
	if _, err := c.config.KubeOvnClient.KubeovnV1().VpcWireGuardPeers().UpdateStatus(context.Background(), newPeer, metav1.UpdateOptions{}); err != nil {
		return err
	}

	if changed {
		c.addOrUpdateVpcWireGuardQueue.Add(gw.Name)
	}
	return nil
}

func (c *Controller) handleDelVpcWireGuardPeer(key string) error {
	c.vpcWireGuardPeerKeyMutex.LockKey(key)
	defer func() { _ = c.vpcWireGuardPeerKeyMutex.UnlockKey(key) }()
	peer, err := c.vpcWireGuardPeerLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			c.ipam.ReleaseAddressByNic(util.VpcWireGuardPeerIPAMName(key), util.VpcWireGuardPeerIPAMName(key), "")
			return nil
		}
		return err
	}
	return c.cleanupVpcWireGuardPeer(peer)
}

func (c *Controller) cleanupVpcWireGuardPeer(peer *kubeovnv1.VpcWireGuardPeer) error {
	subnetName := ""
	if gw, err := c.vpcWireGuardLister.Get(peer.Spec.WireGuard); err == nil {
		subnetName = gw.Spec.ClientSubnet
		c.addOrUpdateVpcWireGuardQueue.Add(gw.Name)
	}
	c.ipam.ReleaseAddressByNic(util.VpcWireGuardPeerIPAMName(peer.Name), util.VpcWireGuardPeerIPAMName(peer.Name), subnetName)
	newPeer := peer.DeepCopy()
	controllerutil.RemoveFinalizer(newPeer, util.KubeOVNControllerFinalizer)
	if !reflect.DeepEqual(peer.Finalizers, newPeer.Finalizers) {
		if _, err := c.config.KubeOvnClient.KubeovnV1().VpcWireGuardPeers().Update(context.Background(), newPeer, metav1.UpdateOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (c *Controller) ensureVpcWireGuardPeerFinalizer(peer *kubeovnv1.VpcWireGuardPeer) error {
	if controllerutil.ContainsFinalizer(peer, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newPeer := peer.DeepCopy()
	controllerutil.AddFinalizer(newPeer, util.KubeOVNControllerFinalizer)
	_, err := c.config.KubeOvnClient.KubeovnV1().VpcWireGuardPeers().Update(context.Background(), newPeer, metav1.UpdateOptions{})
	return err
}

func (c *Controller) allocateVpcWireGuardPeerIP(peer *kubeovnv1.VpcWireGuardPeer, clientSubnet *kubeovnv1.Subnet) (string, error) {
	nic := util.VpcWireGuardPeerIPAMName(peer.Name)
	requested := peer.Spec.ClientIP
	if requested == "" {
		requested = peer.Status.ClientIP
	}
	var v4, v6 string
	var err error
	if requested != "" {
		v4, v6, _, err = c.ipam.GetStaticAddress(nic, nic, requested, nil, clientSubnet.Name, true)
	} else {
		v4, v6, _, err = c.ipam.GetRandomAddress(nic, nic, nil, clientSubnet.Name, "", nil, true)
	}
	if err != nil {
		return "", fmt.Errorf("failed to allocate wireguard peer ip: %w", err)
	}
	if v4 != "" {
		return v4, nil
	}
	return v6, nil
}

func (c *Controller) ensureVpcWireGuardPeerKeys(peer *kubeovnv1.VpcWireGuardPeer, gw *kubeovnv1.VpcWireGuard) (publicKey, privateKey string, err error) {
	if !peer.Spec.GenerateKey {
		if err := util.ParseWireGuardPublicKey(peer.Spec.PublicKey); err != nil {
			return "", "", err
		}
		return peer.Spec.PublicKey, "", nil
	}

	ns := c.vpcWireGuardNamespace(gw)
	name := util.GenVpcWireGuardPeerSecretName(peer.Name)
	secret, getErr := c.config.KubeClient.CoreV1().Secrets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if getErr != nil && !k8serrors.IsNotFound(getErr) {
		return "", "", getErr
	}
	if getErr == nil && len(secret.Data["privateKey"]) > 0 && len(secret.Data["publicKey"]) > 0 {
		return string(secret.Data["publicKey"]), string(secret.Data["privateKey"]), nil
	}
	privateKey, publicKey, err = util.GenerateWireGuardKeyPair()
	if err != nil {
		return "", "", err
	}
	return publicKey, privateKey, nil
}

func (c *Controller) writePeerConfigSecret(peer *kubeovnv1.VpcWireGuardPeer, gw *kubeovnv1.VpcWireGuard, secretName, privateKey, publicKey, address, allowedIPs, psk string) error {
	ns := c.vpcWireGuardNamespace(gw)
	conf := util.RenderWireGuardClientConfig(privateKey, address, "", gw.Status.PublicKey, gw.Status.Endpoint, allowedIPs, psk, peer.Spec.PersistentKeepalive)
	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
			Labels:    util.GenVpcWireGuardLabels(gw.Name),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"privateKey":    []byte(privateKey),
			"publicKey":     []byte(publicKey),
			"wg-quick.conf": []byte(conf),
		},
	}
	if err := util.SetOwnerReference(peer, obj); err != nil {
		return err
	}
	existing, err := c.config.KubeClient.CoreV1().Secrets(ns).Get(context.Background(), secretName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = c.config.KubeClient.CoreV1().Secrets(ns).Create(context.Background(), obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Data = obj.Data
	_, err = c.config.KubeClient.CoreV1().Secrets(ns).Update(context.Background(), existing, metav1.UpdateOptions{})
	return err
}

func (c *Controller) readPeerPresharedKey(peer *kubeovnv1.VpcWireGuardPeer) (string, error) {
	if peer.Spec.PresharedKeySecretRef == nil {
		return "", nil
	}
	if peer.Spec.PresharedKeySecretRef.Name == "" {
		return "", errors.New("presharedKeySecretRef.name is required")
	}
	ns := c.config.PodNamespace
	secret, err := c.config.KubeClient.CoreV1().Secrets(ns).Get(context.Background(), peer.Spec.PresharedKeySecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	keyName := peer.Spec.PresharedKeySecretRef.Key
	if keyName == "" {
		keyName = "presharedKey"
	}
	raw, ok := secret.Data[keyName]
	if !ok {
		return "", fmt.Errorf("preshared key %s not found in secret %s", keyName, secret.Name)
	}
	return strings.TrimSpace(string(raw)), nil
}

func (c *Controller) vpcWireGuardClientAllowedIPs(gw *kubeovnv1.VpcWireGuard) (string, error) {
	if len(gw.Spec.AllowedIPs) > 0 {
		return strings.Join(gw.Spec.AllowedIPs, ", "), nil
	}
	all, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return "", err
	}
	return vpcWireGuardAllowedIPsFromSubnets(gw, all), nil
}

func vpcWireGuardAllowedIPsFromSubnets(gw *kubeovnv1.VpcWireGuard, subnets []*kubeovnv1.Subnet) string {
	if len(gw.Spec.AllowedIPs) > 0 {
		return strings.Join(gw.Spec.AllowedIPs, ", ")
	}
	cidrs := make([]string, 0)
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != gw.Spec.Vpc {
			continue
		}
		if subnet.Spec.CIDRBlock == "" {
			continue
		}
		cidrs = append(cidrs, subnet.Spec.CIDRBlock)
	}
	if len(cidrs) == 0 {
		return "0.0.0.0/0"
	}
	return strings.Join(cidrs, ", ")
}
