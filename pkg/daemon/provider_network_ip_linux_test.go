package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestRemoveProviderNetworkVlanAddresses(t *testing.T) {
	const helperEnv = "KUBE_OVN_PROVIDER_NETWORK_IP_CLEANUP_TEST_HELPER"
	if os.Getenv(helperEnv) != "1" {
		args := []string{"--user", "--map-root-user", "--net"}
		if output, err := exec.Command("unshare", append(args, "true")...).CombinedOutput(); err != nil {
			t.Skipf("user/network namespaces are unavailable: %v: %s", err, output)
		}
		binary, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.Command("unshare", append(args, binary, "-test.run=^TestRemoveProviderNetworkVlanAddresses$", "-test.v")...)
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	bridge := &netlink.Dummy{Name: "pn-test-br"}
	require.NoError(t, netlink.LinkAdd(bridge))
	require.NoError(t, netlink.LinkSetUp(bridge))
	for _, cidr := range []string{"192.0.2.2/24", "192.0.2.3/24"} {
		addr, err := netlink.ParseAddr(cidr)
		require.NoError(t, err)
		require.NoError(t, netlink.AddrAdd(bridge, addr))
	}

	sysctlKey := "net/ipv4/conf/" + bridge.Attrs().Name + "/promote_secondaries"
	previous, err := sysctl.Sysctl(sysctlKey)
	require.NoError(t, err)
	// Remove only the IPAM allocation; preserve host addresses for the existing teardown logic.
	allocated := []string{"192.0.2.2/24", "192.0.2.2"}
	require.NoError(t, removeProviderNetworkVlanAddresses(bridge, allocated))
	addresses, err := util.AddrList(bridge, netlink.FAMILY_V4)
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.Equal(t, "192.0.2.3", addresses[0].IP.String())
	restored, err := sysctl.Sysctl(sysctlKey)
	require.NoError(t, err)
	require.Equal(t, previous, restored, "cleanup must restore the original sysctl")

	// Retrying cleanup after a partial teardown must be harmless.
	require.NoError(t, removeProviderNetworkVlanAddresses(bridge, allocated))

	// Verify that the handshake acknowledges only after local netlink cleanup.
	addr, err := netlink.ParseAddr("192.0.2.2/24")
	require.NoError(t, err)
	require.NoError(t, netlink.AddrAdd(bridge, addr))
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "ovs-vsctl"), []byte("#!/bin/sh\nprintf 'provider:pn-test-br\\n'\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	pn := providerNetworkCleanupTestObject()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node", Labels: map[string]string{"provider.provider-network.kubernetes.io/ready": "true"},
		Annotations: map[string]string{"unrelated": "keep"},
	}}
	kubeClient := fake.NewSimpleClientset(node)
	ovnClient := newProviderNetworkCleanupClient(t, pn)
	ctrl := &Controller{config: &Configuration{NodeName: node.Name, KubeClient: kubeClient, KubeOvnClient: ovnClient}}
	skipInitialization, err := ctrl.processProviderNetworkAddressCleanup(pn, node)
	require.NoError(t, err)
	require.True(t, skipInitialization)
	addresses, err = util.AddrList(bridge, netlink.FAMILY_V4)
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.Equal(t, "192.0.2.3", addresses[0].IP.String())
	stored, err := ovnClient.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, stored.Status.VlanSubinterfaceAddresses[0].Phase)
	require.Equal(t, "ip-uid", stored.Status.VlanSubinterfaceAddresses[0].CleanupAcknowledgedIPUID)
	storedNode, err := kubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"unrelated": "keep"}, storedNode.Annotations)

	// A temporarily missing status entry must not leave the allocated IP behind
	// for bridge teardown to transfer back to the NIC.
	require.NoError(t, netlink.AddrAdd(bridge, addr))
	allocation := pn.Status.VlanSubinterfaceAddresses[0]
	_, err = ovnClient.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: allocation.IPName, OwnerReferences: []metav1.OwnerReference{{
			APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindProviderNetwork, Name: pn.Name, UID: pn.UID,
		}}},
		Spec: kubeovnv1.IPSpec{NodeName: node.Name, PodType: util.KindProviderNetwork, IPAddress: allocation.IPAddress},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	withoutStatus := pn.DeepCopy()
	withoutStatus.Status.VlanSubinterfaceAddresses = nil
	require.NoError(t, ctrl.cleanupProviderNetworkVlanSubinterfaceAddress(withoutStatus, node))
	addresses, err = util.AddrList(bridge, netlink.FAMILY_V4)
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.Equal(t, "192.0.2.3", addresses[0].IP.String())
}

func TestConfigureProviderNetworkVlanAddresses(t *testing.T) {
	const helperEnv = "KUBE_OVN_PROVIDER_NETWORK_IP_CONFIGURE_TEST_HELPER"
	if os.Getenv(helperEnv) != "1" {
		args := []string{"--user", "--map-root-user", "--net"}
		if output, err := exec.Command("unshare", append(args, "true")...).CombinedOutput(); err != nil {
			t.Skipf("user/network namespaces are unavailable: %v: %s", err, output)
		}
		binary, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.Command("unshare", append(args, binary, "-test.run=^TestConfigureProviderNetworkVlanAddresses$", "-test.v")...)
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	for _, tt := range []struct {
		name      string
		family    int
		allocated string
		desired   string
		host      string
	}{
		{name: "v4", family: netlink.FAMILY_V4, allocated: "192.0.2.2/24", desired: "192.0.2.2/25", host: "192.0.2.3/24"},
		{name: "v6", family: netlink.FAMILY_V6, allocated: "2001:db8::2/64", desired: "2001:db8::2/80", host: "2001:db8::3/64"},
		{name: "dual", family: netlink.FAMILY_ALL, allocated: "192.0.2.2/24,2001:db8::2/64", desired: "192.0.2.2/25,2001:db8::2/80", host: "192.0.2.3/24,2001:db8::3/64"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bridge := &netlink.Dummy{Name: "pn-mask-" + tt.name}
			require.NoError(t, netlink.LinkAdd(bridge))
			require.NoError(t, netlink.LinkSetUp(bridge))
			for _, cidr := range append(strings.Split(tt.allocated, ","), strings.Split(tt.host, ",")...) {
				addr, err := netlink.ParseAddr(cidr)
				require.NoError(t, err)
				require.NoError(t, netlink.AddrAdd(bridge, addr))
			}
			sysctlKey := "net/ipv4/conf/" + bridge.Attrs().Name + "/promote_secondaries"
			previous, err := sysctl.Sysctl(sysctlKey)
			require.NoError(t, err)
			// Change the mask without an annotation; preserve the unrelated host address.
			for range 2 {
				require.NoError(t, configureProviderNetworkVlanAddresses(bridge, tt.desired))
				addresses, err := util.AddrList(bridge, tt.family)
				require.NoError(t, err)
				var cidrs []string
				for _, address := range addresses {
					if !address.IP.IsLinkLocalUnicast() {
						cidrs = append(cidrs, address.IPNet.String())
					}
				}
				require.ElementsMatch(t, append(strings.Split(tt.desired, ","), strings.Split(tt.host, ",")...), cidrs)
			}
			// Cleanup matches the allocated IP even if its recorded mask is outdated.
			require.NoError(t, removeProviderNetworkVlanAddresses(bridge, strings.Split(tt.allocated, ",")))
			addresses, err := util.AddrList(bridge, tt.family)
			require.NoError(t, err)
			var remaining []string
			for _, address := range addresses {
				if !address.IP.IsLinkLocalUnicast() {
					remaining = append(remaining, address.IPNet.String())
				}
			}
			require.ElementsMatch(t, strings.Split(tt.host, ","), remaining)
			restored, err := sysctl.Sysctl(sysctlKey)
			require.NoError(t, err)
			require.Equal(t, previous, restored, "mask changes and cleanup must restore the original sysctl")
		})
	}
}
