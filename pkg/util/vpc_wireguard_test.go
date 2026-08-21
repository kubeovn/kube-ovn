package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestGenerateWireGuardKeyPair(t *testing.T) {
	priv, pub, err := GenerateWireGuardKeyPair()
	require.NoError(t, err)
	require.NotEmpty(t, priv)
	require.NotEmpty(t, pub)
	require.NoError(t, ParseWireGuardPublicKey(pub))
	require.Error(t, ParseWireGuardPublicKey("not-a-key"))
	require.Error(t, ParseWireGuardPublicKey(""))
}

func TestGenVpcWireGuardNames(t *testing.T) {
	require.Equal(t, "vpc-wg-foo", GenVpcWireGuardName("foo"))
	require.Equal(t, "vpc-wg-foo-0", GenVpcWireGuardPodName("foo"))
	require.Equal(t, "vpc-wg-foo-server", GenVpcWireGuardServerSecretName("foo"))
	require.Equal(t, "vpc-wg-peer-alice", GenVpcWireGuardPeerSecretName("alice"))
	require.Equal(t, "vpc-wg-dnat-foo", GenVpcWireGuardDnatName("foo"))
	require.Equal(t, "vpc-wg-fip-foo", GenVpcWireGuardFipName("foo"))
	require.Equal(t, "vpc-wg-peer.alice", VpcWireGuardPeerIPAMName("alice"))
	require.Equal(t, "vpc-wg-server.foo", VpcWireGuardServerIPAMName("foo"))
	require.Equal(t, int32(51820), DefaultVpcWireGuardListenPort(0))
	require.Equal(t, int32(12345), DefaultVpcWireGuardListenPort(12345))
	require.NoError(t, ValidateVpcWireGuardStatefulSetNameLength("ok"))
	require.Error(t, ValidateVpcWireGuardStatefulSetNameLength(strings.Repeat("a", 80)))
	require.Equal(t, map[string]string{
		"app":             "vpc-wg-foo",
		VpcWireGuardLabel: "true",
	}, GenVpcWireGuardLabels("foo"))
}

func TestRenderWireGuardConfigs(t *testing.T) {
	server := RenderWireGuardServerConfig("skey", "10.255.0.1/24", 51820, 1420, []WireGuardPeerConfig{
		{PublicKey: "pkey", AllowedIPs: "10.255.0.2/32", PresharedKey: "psk"},
	})
	require.Contains(t, server, "ListenPort = 51820")
	require.Contains(t, server, "MTU = 1420")
	require.Contains(t, server, "AllowedIPs = 10.255.0.2/32")
	require.Contains(t, server, "PresharedKey = psk")

	client := RenderWireGuardClientConfig("ckey", "10.255.0.2/24", "", "spub", "1.2.3.4:51820", "10.0.0.0/16", "", 25)
	require.Contains(t, client, "Endpoint = 1.2.3.4:51820")
	require.Contains(t, client, "PersistentKeepalive = 25")
	require.False(t, strings.Contains(client, "DNS ="))

	client = RenderWireGuardClientConfig("ckey", "10.255.0.2/24", "1.1.1.1", "spub", "1.2.3.4:51820", "10.0.0.0/16", "psk", 0)
	require.Contains(t, client, "DNS = 1.1.1.1")
	require.Contains(t, client, "PresharedKey = psk")
	require.NotContains(t, client, "PersistentKeepalive")

	serverNoMTU := RenderWireGuardServerConfig("skey", "10.255.0.1/24", 51820, 0, nil)
	require.NotContains(t, serverNoMTU, "MTU =")
	require.NotContains(t, serverNoMTU, "[Peer]")
}

func TestGenVpcWireGuardPodAnnotations(t *testing.T) {
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Subnet: "lan",
			LanIP:  "10.0.0.10",
			Exposure: kubeovnv1.VpcWireGuardExposure{
				Type: kubeovnv1.VpcWireGuardExposureDNAT,
			},
		},
	}
	ann, err := GenVpcWireGuardPodAnnotations(gw, "", "", OvnProvider, false)
	require.NoError(t, err)
	require.Equal(t, "vpn", ann[VpcWireGuardAnnotation])
	require.Equal(t, "lan", ann["ovn.kubernetes.io/logical_switch"])
	require.Equal(t, "10.0.0.10", ann["ovn.kubernetes.io/ip_address"])
	require.Empty(t, ann["k8s.v1.cni.cncf.io/networks"])

	gw.Spec.Exposure.Type = kubeovnv1.VpcWireGuardExposureDualNIC
	_, err = GenVpcWireGuardPodAnnotations(gw, "", "", OvnProvider, false)
	require.Error(t, err)
	ann, err = GenVpcWireGuardPodAnnotations(gw, "kube-system", "ext", OvnProvider, false)
	require.NoError(t, err)
	require.Equal(t, "kube-system/ext", ann["k8s.v1.cni.cncf.io/networks"])

	_, err = GenVpcWireGuardPodAnnotations(gw, "kube-system", "ext", "bad-provider", false)
	require.Error(t, err)
	ann, err = GenVpcWireGuardPodAnnotations(gw, "kube-system", "ext", "net.kube-system.ovn", false)
	require.NoError(t, err)
	require.Equal(t, "kube-system/net", ann[DefaultNetworkAnnotation])
	ann, err = GenVpcWireGuardPodAnnotations(gw, "kube-system", "ext", "net.kube-system.ovn", true)
	require.NoError(t, err)
	require.Empty(t, ann[DefaultNetworkAnnotation])
}
