package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	ovnipam "github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVpcWireGuardAllowedIPsFromSubnets(t *testing.T) {
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Vpc: "tenant",
		},
	}
	require.Equal(t, "0.0.0.0/0", vpcWireGuardAllowedIPsFromSubnets(gw, nil))

	gw.Spec.AllowedIPs = []string{"10.0.0.0/16", "10.1.0.0/16"}
	require.Equal(t, "10.0.0.0/16, 10.1.0.0/16", vpcWireGuardAllowedIPsFromSubnets(gw, nil))

	gw.Spec.AllowedIPs = nil
	subnets := []*kubeovnv1.Subnet{
		{Spec: kubeovnv1.SubnetSpec{Vpc: "tenant", CIDRBlock: "10.0.0.0/24"}},
		{Spec: kubeovnv1.SubnetSpec{Vpc: "other", CIDRBlock: "10.9.0.0/24"}},
		{Spec: kubeovnv1.SubnetSpec{Vpc: "tenant", CIDRBlock: ""}},
	}
	require.Equal(t, "10.0.0.0/24", vpcWireGuardAllowedIPsFromSubnets(gw, subnets))
}

func TestVpcWireGuardContainerCommandWaitsForConfig(t *testing.T) {
	require.Contains(t, vpcWireGuardContainerCommand, "/etc/wireguard/wg0.conf")
	require.Contains(t, vpcWireGuardContainerCommand, "wireguard.sh init")
	require.NotContains(t, vpcWireGuardContainerCommand, "PostStart")
}

func TestVpcWireGuardRouteExternalIDs(t *testing.T) {
	ids := vpcWireGuardRouteExternalIDs("vpn")
	require.Equal(t, "kube-ovn-vpc-wireguard", ids["vendor"])
	require.Equal(t, "vpn", ids["vpc-wireguard"])
}

func TestVpcWireGuardNamespace(t *testing.T) {
	c := &Controller{config: &Configuration{PodNamespace: "kube-system"}}
	gw := &kubeovnv1.VpcWireGuard{}
	require.Equal(t, "kube-system", c.vpcWireGuardNamespace(gw))
	gw.Spec.Namespace = "tenant-ns"
	require.Equal(t, "tenant-ns", c.vpcWireGuardNamespace(gw))
}

func TestVpcWireGuardStatusBytes(t *testing.T) {
	status := &kubeovnv1.VpcWireGuardStatus{Ready: true, LanIP: "10.0.0.1", Endpoint: "1.2.3.4:51820"}
	raw, err := status.Bytes()
	require.NoError(t, err)
	require.Contains(t, string(raw), `"ready":true`)
	require.Contains(t, string(raw), `"lanIp":"10.0.0.1"`)

	peerStatus := &kubeovnv1.VpcWireGuardPeerStatus{Ready: true, ClientIP: "10.255.0.2"}
	raw, err = peerStatus.Bytes()
	require.NoError(t, err)
	require.Contains(t, string(raw), `"clientIP":"10.255.0.2"`)
}

func TestEnqueueVpcWireGuard(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcWireGuardQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcWireGuard", nil),
		delVpcWireGuardQueue:         newTypedRateLimitingQueue[string]("DeleteVpcWireGuard", nil),
	}
	gw := &kubeovnv1.VpcWireGuard{ObjectMeta: metav1.ObjectMeta{Name: "vpn"}}
	c.enqueueAddVpcWireGuard(gw)
	require.Equal(t, 1, c.addOrUpdateVpcWireGuardQueue.Len())

	updated := gw.DeepCopy()
	updated.Spec.ListenPort = 51821
	c.enqueueUpdateVpcWireGuard(gw, updated)
	// workqueue dedupes identical keys still pending
	require.Equal(t, 1, c.addOrUpdateVpcWireGuardQueue.Len())

	same := gw.DeepCopy()
	c.enqueueUpdateVpcWireGuard(gw, same)
	require.Equal(t, 1, c.addOrUpdateVpcWireGuardQueue.Len())

	c.enqueueDeleteVpcWireGuard(gw)
	require.Equal(t, 1, c.delVpcWireGuardQueue.Len())
	c.enqueueDeleteVpcWireGuard(cache.DeletedFinalStateUnknown{Obj: gw})
	require.Equal(t, 1, c.delVpcWireGuardQueue.Len())
	c.enqueueDeleteVpcWireGuard(cache.DeletedFinalStateUnknown{Obj: "bad"})
	c.enqueueDeleteVpcWireGuard("bad")
	require.Equal(t, 1, c.delVpcWireGuardQueue.Len())
}

func TestEnqueueVpcWireGuardPeer(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcWireGuardPeerQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcWireGuardPeer", nil),
		delVpcWireGuardPeerQueue:         newTypedRateLimitingQueue[string]("DeleteVpcWireGuardPeer", nil),
	}
	peer := &kubeovnv1.VpcWireGuardPeer{ObjectMeta: metav1.ObjectMeta{Name: "alice"}}
	c.enqueueAddVpcWireGuardPeer(peer)
	require.Equal(t, 1, c.addOrUpdateVpcWireGuardPeerQueue.Len())

	updated := peer.DeepCopy()
	updated.Spec.GenerateKey = true
	c.enqueueUpdateVpcWireGuardPeer(peer, updated)
	require.Equal(t, 1, c.addOrUpdateVpcWireGuardPeerQueue.Len())

	c.enqueueDeleteVpcWireGuardPeer(peer)
	require.Equal(t, 1, c.delVpcWireGuardPeerQueue.Len())
	c.enqueueDeleteVpcWireGuardPeer(cache.DeletedFinalStateUnknown{Obj: peer})
	require.Equal(t, 1, c.delVpcWireGuardPeerQueue.Len())
	c.enqueueDeleteVpcWireGuardPeer("bad")
	require.Equal(t, 1, c.delVpcWireGuardPeerQueue.Len())
}

type wireGuardTestEnv struct {
	controller *Controller
	kubeClient *fake.Clientset
	ovnClient  *kubeovnfake.Clientset
	mockNB     *mockovs.MockNbClient
}

func secretBytes(secret *corev1.Secret, key string) []byte {
	if secret.Data != nil {
		if v, ok := secret.Data[key]; ok && len(v) > 0 {
			return v
		}
	}
	if secret.StringData != nil {
		if v, ok := secret.StringData[key]; ok {
			return []byte(v)
		}
	}
	return nil
}

func newWireGuardTestEnv(t *testing.T, kubeObjs, ovnObjs []runtime.Object) *wireGuardTestEnv {
	t.Helper()
	kubeClient := fake.NewSimpleClientset()
	ovnClient := kubeovnfake.NewSimpleClientset()

	kubeFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	ovnFactory := kubeovninformerfactory.NewSharedInformerFactory(ovnClient, 0)

	podInformer := kubeFactory.Core().V1().Pods()
	subnetInformer := ovnFactory.Kubeovn().V1().Subnets()
	vpcInformer := ovnFactory.Kubeovn().V1().Vpcs()
	wgInformer := ovnFactory.Kubeovn().V1().VpcWireGuards()
	peerInformer := ovnFactory.Kubeovn().V1().VpcWireGuardPeers()
	eipInformer := ovnFactory.Kubeovn().V1().IptablesEIPs()
	dnatInformer := ovnFactory.Kubeovn().V1().IptablesDnatRules()
	fipInformer := ovnFactory.Kubeovn().V1().IptablesFIPRules()

	mockCtrl := gomock.NewController(t)
	mockNB := mockovs.NewMockNbClient(mockCtrl)

	c := &Controller{
		config: &Configuration{
			KubeClient:    kubeClient,
			KubeOvnClient: ovnClient,
			PodNamespace:  metav1.NamespaceSystem,
		},
		podsLister:                       podInformer.Lister(),
		subnetsLister:                    subnetInformer.Lister(),
		vpcsLister:                       vpcInformer.Lister(),
		vpcWireGuardLister:               wgInformer.Lister(),
		vpcWireGuardPeerLister:           peerInformer.Lister(),
		iptablesEipsLister:               eipInformer.Lister(),
		iptablesDnatRulesLister:          dnatInformer.Lister(),
		iptablesFipsLister:               fipInformer.Lister(),
		vpcWireGuardKeyMutex:             keymutex.NewHashed(0),
		vpcWireGuardPeerKeyMutex:         keymutex.NewHashed(0),
		addOrUpdateVpcWireGuardQueue:     newTypedRateLimitingQueue[string]("AddOrUpdateVpcWireGuard", nil),
		addOrUpdateVpcWireGuardPeerQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcWireGuardPeer", nil),
		OVNNbClient:                      mockNB,
		ipam:                             ovnipam.NewIPAM(),
	}

	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })
	kubeFactory.Start(stopCh)
	ovnFactory.Start(stopCh)
	kubeFactory.WaitForCacheSync(stopCh)
	ovnFactory.WaitForCacheSync(stopCh)

	for _, obj := range kubeObjs {
		switch o := obj.(type) {
		case *corev1.Pod:
			_, err := kubeClient.CoreV1().Pods(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		case *corev1.Secret:
			_, err := kubeClient.CoreV1().Secrets(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		default:
			t.Fatalf("unsupported kube object %T", obj)
		}
	}
	for _, obj := range ovnObjs {
		switch o := obj.(type) {
		case *kubeovnv1.Subnet:
			_, err := ovnClient.KubeovnV1().Subnets().Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		case *kubeovnv1.Vpc:
			_, err := ovnClient.KubeovnV1().Vpcs().Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		case *kubeovnv1.VpcWireGuard:
			_, err := ovnClient.KubeovnV1().VpcWireGuards().Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		case *kubeovnv1.VpcWireGuardPeer:
			_, err := ovnClient.KubeovnV1().VpcWireGuardPeers().Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		case *kubeovnv1.IptablesEIP:
			_, err := ovnClient.KubeovnV1().IptablesEIPs().Create(context.Background(), o, metav1.CreateOptions{})
			require.NoError(t, err)
		default:
			t.Fatalf("unsupported ovn object %T", obj)
		}
	}

	require.Eventually(t, func() bool {
		for _, obj := range kubeObjs {
			if o, ok := obj.(*corev1.Pod); ok {
				if _, err := c.podsLister.Pods(o.Namespace).Get(o.Name); err != nil {
					return false
				}
			}
		}
		for _, obj := range ovnObjs {
			switch o := obj.(type) {
			case *kubeovnv1.Subnet:
				if _, err := c.subnetsLister.Get(o.Name); err != nil {
					return false
				}
			case *kubeovnv1.VpcWireGuard:
				if _, err := c.vpcWireGuardLister.Get(o.Name); err != nil {
					return false
				}
			case *kubeovnv1.VpcWireGuardPeer:
				if _, err := c.vpcWireGuardPeerLister.Get(o.Name); err != nil {
					return false
				}
			case *kubeovnv1.IptablesEIP:
				if _, err := c.iptablesEipsLister.Get(o.Name); err != nil {
					return false
				}
			}
		}
		return true
	}, 3*time.Second, 20*time.Millisecond)

	return &wireGuardTestEnv{controller: c, kubeClient: kubeClient, ovnClient: ovnClient, mockNB: mockNB}
}

func TestGetVpcWireGuardLanIP(t *testing.T) {
	env := newWireGuardTestEnv(t, nil, nil)
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec:       kubeovnv1.VpcWireGuardSpec{LanIP: "10.0.8.10"},
	}
	ip, err := env.controller.getVpcWireGuardLanIP(gw)
	require.NoError(t, err)
	require.Equal(t, "10.0.8.10", ip)
}

func TestEnsureVpcWireGuardDnatAndFip(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		ObjectMeta: metav1.ObjectMeta{Name: "eip1"},
		Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "203.0.113.10"},
		Status:     kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{eip})
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Exposure: kubeovnv1.VpcWireGuardExposure{
				Type:       kubeovnv1.VpcWireGuardExposureDNAT,
				EIP:        "eip1",
				NatGateway: "nat1",
			},
		},
	}

	endpoint, err := env.controller.ensureVpcWireGuardDnat(gw, "10.0.8.10", 51820)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.10:51820", endpoint)
	dnat, err := env.ovnClient.KubeovnV1().IptablesDnatRules().Get(context.Background(), util.GenVpcWireGuardDnatName("vpn"), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "udp", dnat.Spec.Protocol)
	require.Equal(t, "10.0.8.10", dnat.Spec.InternalIP)

	endpoint, err = env.controller.reconcileVpcWireGuardExposure(gw, "10.0.8.10")
	require.NoError(t, err)
	require.Equal(t, "203.0.113.10:51820", endpoint)

	gw.Spec.Exposure.Type = "bad"
	_, err = env.controller.reconcileVpcWireGuardExposure(gw, "10.0.8.10")
	require.Error(t, err)
}

func TestEnsureVpcWireGuardFip(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		ObjectMeta: metav1.ObjectMeta{Name: "eip1"},
		Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "203.0.113.10"},
		Status:     kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{eip})
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Exposure: kubeovnv1.VpcWireGuardExposure{
				Type:       kubeovnv1.VpcWireGuardExposureFIP,
				EIP:        "eip1",
				NatGateway: "nat1",
			},
		},
	}
	endpoint, err := env.controller.ensureVpcWireGuardFip(gw, "10.0.8.10", 51820)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.10:51820", endpoint)
	fip, err := env.ovnClient.KubeovnV1().IptablesFIPRules().Get(context.Background(), util.GenVpcWireGuardFipName("vpn"), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "10.0.8.10", fip.Spec.InternalIP)
}

func TestEnsureVpcWireGuardServerSecretAndConfig(t *testing.T) {
	env := newWireGuardTestEnv(t, nil, nil)
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			GenerateServerKey: true,
			ListenPort:        51820,
			MTU:               1420,
		},
	}

	pub, err := env.controller.ensureVpcWireGuardServerSecret(gw)
	require.NoError(t, err)
	require.NoError(t, util.ParseWireGuardPublicKey(pub))
	secret, err := env.kubeClient.CoreV1().Secrets(metav1.NamespaceSystem).Get(context.Background(), util.GenVpcWireGuardServerSecretName("vpn"), metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, secretBytes(secret, "privateKey"))

	pub2, err := env.controller.ensureVpcWireGuardServerSecret(gw)
	require.NoError(t, err)
	require.Equal(t, pub, pub2)

	require.NoError(t, env.controller.writeVpcWireGuardServerConfig(gw, "10.255.0.1", "10.255.0.0/24"))
	secret, err = env.kubeClient.CoreV1().Secrets(metav1.NamespaceSystem).Get(context.Background(), util.GenVpcWireGuardServerSecretName("vpn"), metav1.GetOptions{})
	require.NoError(t, err)
	conf := string(secretBytes(secret, "wg0.conf"))
	require.Contains(t, conf, "ListenPort = 51820")
	require.Contains(t, conf, "Address = 10.255.0.1")
}

func TestGenVpcWireGuardStatefulSet(t *testing.T) {
	vpcNatImage = "kubeovn/vpc-nat-gateway:test"
	t.Cleanup(func() { vpcNatImage = "" })

	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "lan"},
		Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Vpc: "tenant"},
	}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{subnet})
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Subnet: "lan",
			Exposure: kubeovnv1.VpcWireGuardExposure{
				Type: kubeovnv1.VpcWireGuardExposureDNAT,
			},
		},
	}
	sts, err := env.controller.genVpcWireGuardStatefulSet(gw)
	require.NoError(t, err)
	require.Equal(t, util.GenVpcWireGuardName("vpn"), sts.Name)
	require.Equal(t, vpcNatImage, sts.Spec.Template.Spec.Containers[0].Image)
	require.Contains(t, sts.Spec.Template.Spec.Containers[0].Command[2], "wg0.conf")
	require.Equal(t, util.GenVpcWireGuardServerSecretName("vpn"), sts.Spec.Template.Spec.Volumes[0].Secret.SecretName)
}

func TestListVpcWireGuardPeerConfigsAndAllowedIPs(t *testing.T) {
	peerReady := &kubeovnv1.VpcWireGuardPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "alice"},
		Spec:       kubeovnv1.VpcWireGuardPeerSpec{WireGuard: "vpn"},
		Status: kubeovnv1.VpcWireGuardPeerStatus{
			Ready:     true,
			PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			ClientIP:  "10.255.0.2",
		},
	}
	peerOther := &kubeovnv1.VpcWireGuardPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "bob"},
		Spec:       kubeovnv1.VpcWireGuardPeerSpec{WireGuard: "other"},
		Status: kubeovnv1.VpcWireGuardPeerStatus{
			Ready:     true,
			PublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
			ClientIP:  "10.255.0.3",
		},
	}
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "lan"},
		Spec:       kubeovnv1.SubnetSpec{Vpc: "tenant", CIDRBlock: "10.0.8.0/24"},
	}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{peerReady, peerOther, subnet})

	peers, err := env.controller.listVpcWireGuardPeerConfigs("vpn")
	require.NoError(t, err)
	require.Len(t, peers, 1)
	require.Equal(t, "10.255.0.2/32", peers[0].AllowedIPs)

	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec:       kubeovnv1.VpcWireGuardSpec{Vpc: "tenant"},
	}
	allowed, err := env.controller.vpcWireGuardClientAllowedIPs(gw)
	require.NoError(t, err)
	require.Equal(t, "10.0.8.0/24", allowed)

	gw.Spec.AllowedIPs = []string{"10.0.0.0/8"}
	allowed, err = env.controller.vpcWireGuardClientAllowedIPs(gw)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/8", allowed)
}

func TestReconcileAndDeleteVpcWireGuardRoutes(t *testing.T) {
	env := newWireGuardTestEnv(t, nil, nil)
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec:       kubeovnv1.VpcWireGuardSpec{Vpc: "tenant"},
	}
	clientSubnet := &kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.255.0.0/24"}}
	externalIDs := vpcWireGuardRouteExternalIDs("vpn")

	env.mockNB.EXPECT().ListLogicalRouterStaticRoutes("tenant", nil, nil, "", externalIDs).Return(nil, nil)
	env.mockNB.EXPECT().AddLogicalRouterStaticRoute("tenant", "", ovnnb.LogicalRouterStaticRoutePolicyDstIP, "10.255.0.0/24", nil, externalIDs, "10.0.8.10").Return(nil)
	require.NoError(t, env.controller.reconcileVpcWireGuardRoutes(gw, clientSubnet, "10.0.8.10"))

	existed := []*ovnnb.LogicalRouterStaticRoute{{UUID: "u1", IPPrefix: "10.255.0.0/24", Nexthop: "10.0.8.10"}}
	env.mockNB.EXPECT().ListLogicalRouterStaticRoutes("tenant", nil, nil, "", externalIDs).Return(existed, nil)
	env.mockNB.EXPECT().DeleteLogicalRouterStaticRouteByUUID("tenant", "u1").Return(nil)
	require.NoError(t, env.controller.deleteVpcWireGuardRoutes(gw))
}

func TestWritePeerConfigSecret(t *testing.T) {
	env := newWireGuardTestEnv(t, nil, nil)
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Status:     kubeovnv1.VpcWireGuardStatus{PublicKey: "spub", Endpoint: "1.2.3.4:51820"},
	}
	peer := &kubeovnv1.VpcWireGuardPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "alice"},
		Spec:       kubeovnv1.VpcWireGuardPeerSpec{PersistentKeepalive: 25},
	}
	require.NoError(t, env.controller.writePeerConfigSecret(peer, gw, util.GenVpcWireGuardPeerSecretName("alice"), "ckey", "cpub", "10.255.0.2/24", "10.0.0.0/16", ""))
	secret, err := env.kubeClient.CoreV1().Secrets(metav1.NamespaceSystem).Get(context.Background(), util.GenVpcWireGuardPeerSecretName("alice"), metav1.GetOptions{})
	require.NoError(t, err)
	conf := string(secretBytes(secret, "wg-quick.conf"))
	require.Contains(t, conf, "Endpoint = 1.2.3.4:51820")
	require.Contains(t, conf, "PersistentKeepalive = 25")
}

func TestCleanupVpcWireGuard(t *testing.T) {
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "vpn",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
		Spec: kubeovnv1.VpcWireGuardSpec{Vpc: "tenant", ClientSubnet: "vpn-pool"},
	}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{gw})
	env.mockNB.EXPECT().ListLogicalRouterStaticRoutes("tenant", nil, nil, "", vpcWireGuardRouteExternalIDs("vpn")).Return(nil, nil)
	require.NoError(t, env.controller.cleanupVpcWireGuard(gw))
}

func TestHandleAddOrUpdateVpcWireGuardMissingImage(t *testing.T) {
	vpcNatImage = ""
	gw := &kubeovnv1.VpcWireGuard{ObjectMeta: metav1.ObjectMeta{Name: "vpn"}}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{gw})
	err := env.controller.handleAddOrUpdateVpcWireGuard("vpn")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "vpc nat image"))
}

func TestEnsureVpcWireGuardFinalizer(t *testing.T) {
	gw := &kubeovnv1.VpcWireGuard{ObjectMeta: metav1.ObjectMeta{Name: "vpn"}}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{gw})
	require.NoError(t, env.controller.ensureVpcWireGuardFinalizer(gw))
	updated, err := env.ovnClient.KubeovnV1().VpcWireGuards().Get(context.Background(), "vpn", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
}

func TestDualNICEndpointFromEIP(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		ObjectMeta: metav1.ObjectMeta{Name: "eip1"},
		Status:     kubeovnv1.IptablesEIPStatus{IP: "198.51.100.8"},
	}
	env := newWireGuardTestEnv(t, nil, []runtime.Object{eip})
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Exposure: kubeovnv1.VpcWireGuardExposure{
				Type: kubeovnv1.VpcWireGuardExposureDualNIC,
				EIP:  "eip1",
			},
		},
	}
	endpoint, err := env.controller.vpcWireGuardDualNICEndpoint(gw, 51820)
	require.NoError(t, err)
	require.Equal(t, "198.51.100.8:51820", endpoint)
}

func TestGetVpcWireGuardPodAndLanIPFromPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenVpcWireGuardPodName("vpn"),
			Namespace: metav1.NamespaceSystem,
			Annotations: map[string]string{
				"ovn.kubernetes.io/ip_address": "10.0.8.11",
			},
		},
	}
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "lan"},
		Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}
	env := newWireGuardTestEnv(t, []runtime.Object{pod}, []runtime.Object{subnet})
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec:       kubeovnv1.VpcWireGuardSpec{Subnet: "lan"},
	}
	got, err := env.controller.getVpcWireGuardPod(gw)
	require.NoError(t, err)
	require.Equal(t, pod.Name, got.Name)

	ip, err := env.controller.getVpcWireGuardLanIP(gw)
	require.NoError(t, err)
	require.Equal(t, "10.0.8.11", ip)
}
