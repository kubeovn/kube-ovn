package daemon

import (
	"net"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

func TestNonRootVlanLifecycle(t *testing.T) {
	const helperEnv = "KUBE_OVN_NONROOT_VLAN_TEST_HELPER"
	if os.Getenv(helperEnv) != "1" {
		// Keep capability changes and network devices in a disposable subprocess.
		args := []string{"--user", "--map-user=65534", "--map-group=65534", "--net", "--keep-caps"}
		if output, err := exec.Command("unshare", append(args, "true")...).CombinedOutput(); err != nil {
			t.Skipf("user/network namespaces are unavailable: %v: %s", err, output)
		}
		binary, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.Command("unshare", append(args, binary, "-test.run=^TestNonRootVlanLifecycle$", "-test.v")...)
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		t.Logf("%s", output)
		return
	}

	require.Equal(t, 65534, os.Getuid())
	// Match the daemon: effective/permitted capabilities, no inheritable or
	// ambient capabilities that could accidentally make an ip subprocess work.
	require.NoError(t, cap.ResetAmbient())
	caps, err := cap.FromText("cap_net_bind_service,cap_net_admin,cap_net_raw=ep")
	require.NoError(t, err)
	require.NoError(t, caps.SetProc())
	require.Equal(t, caps.String(), cap.GetProc().String())

	parent := &netlink.Dummy{Name: "vlan-test"}
	require.NoError(t, netlink.LinkAdd(parent))
	require.NoError(t, netlink.LinkSetUp(parent))
	controller := &Controller{}
	names := []string{"vlan-test.100", "vlan-test.200", "vlan-test.300"}
	require.NoError(t, controller.createVlanSubinterfaces(names, parent.Name, "test-provider"))
	for i, name := range names {
		link, err := netlink.LinkByName(name)
		require.NoError(t, err)
		vlan, ok := link.(*netlink.Vlan)
		require.True(t, ok)
		require.Equal(t, (i+1)*100, vlan.VlanId)
		require.Equal(t, parent.Index, vlan.ParentIndex)
		require.Equal(t, "kube-ovn:test-provider", vlan.Alias)
		require.NotZero(t, vlan.Flags&net.FlagUp)
	}

	// Reconciliation must retain an existing interface's ownership marker.
	existing, err := netlink.LinkByName(names[0])
	require.NoError(t, err)
	require.NoError(t, netlink.LinkSetAlias(existing, "existing-owner"))
	require.NoError(t, controller.createVlanSubinterfaces(names, parent.Name, "test-provider"))
	existing, err = netlink.LinkByName(names[0])
	require.NoError(t, err)
	require.Equal(t, "existing-owner", existing.Attrs().Alias)
	require.NoError(t, netlink.LinkSetAlias(existing, "kube-ovn:test-provider"))

	foreign := &netlink.Vlan{Name: "vlan-test.400", ParentIndex: parent.Index, VlanId: 400}
	require.NoError(t, netlink.LinkAdd(foreign))
	require.NoError(t, netlink.LinkSetAlias(foreign, "kube-ovn:other-provider"))
	require.NoError(t, controller.cleanupAutoCreatedVlanInterfaces("test-provider", names[0], map[string]int{names[1]: 200}))
	for _, name := range []string{names[0], names[1], foreign.Name} {
		_, err := netlink.LinkByName(name)
		require.NoError(t, err)
	}
	_, err = netlink.LinkByName(names[2])
	require.ErrorAs(t, err, new(netlink.LinkNotFoundError))
	require.NoError(t, controller.cleanupAutoCreatedVlanInterfaces("test-provider", "", nil))
	for _, name := range names[:2] {
		_, err := netlink.LinkByName(name)
		require.ErrorAs(t, err, new(netlink.LinkNotFoundError))
	}
	_, err = netlink.LinkByName(foreign.Name)
	require.NoError(t, err)
}
