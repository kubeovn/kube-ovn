package controller

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

// This optional integration test uses an isolated real NB/SB database and
// northd. The fixture contains client/backend/router/localnet switch ports.
func TestServiceNodePortLogicalFlows(t *testing.T) {
	endpoint := os.Getenv("KUBE_OVN_NODEPORT_TEST_DB")
	if endpoint == "" {
		t.Skip("set KUBE_OVN_NODEPORT_TEST_DB and KUBE_OVN_NODEPORT_TEST_BIN for a real OVN fixture")
	}
	bin := os.Getenv("KUBE_OVN_NODEPORT_TEST_BIN")
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), filepath.Join(bin, name), args...).CombinedOutput()
		require.NoError(t, err, "%s: %s", name, out)
		return string(out)
	}
	sync := func() { run("ovn-nbctl", "--db="+endpoint, "--timeout=10", "--wait=sb", "sync") }
	trace := func(inport, source, destination string, destinationMAC ...string) string {
		mac := "00:00:00:00:00:01"
		if len(destinationMAC) != 0 {
			mac = destinationMAC[0]
		}
		packet := "inport == " + inport + " && eth.src == 00:00:00:00:00:10 && eth.dst == " + mac + " && " +
			source + " && " + destination + " && tcp.src == 40000 && tcp.dst == 30080 && ip.ttl == 64"
		out := run("ovn-trace", "--db="+endpoint, "--summary", "--ct=new", "subnet", packet)
		t.Log(out)
		return out
	}
	ctrl, _, svc, endpoints := serviceLBReconcileFixture(t, corev1.ServiceTypeNodePort, false)
	nb, err := ovs.NewOvnNbClient(endpoint, 3, 3, 0, 1)
	require.NoError(t, err)
	t.Cleanup(nb.Close)
	ctrl.OVNNbClient = nb
	svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	svc.Spec.Ports[0].NodePort = 30080
	node, err := ctrl.nodesLister.Get("node")
	require.NoError(t, err)
	node.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
		{Type: corev1.NodeInternalIP, Address: "2001:db8::10"},
		{Type: corev1.NodeExternalIP, Address: "10.0.0.99"},
	}
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	sync()
	for _, family := range []struct{ source, destination string }{
		{"ip4.src == 10.0.0.10", "ip4.dst == 192.0.2.10"},
		{"ip6.src == fd00::10", "ip6.dst == 2001:db8::10"},
	} {
		out := trace("\"client\"", family.source, family.destination)
		require.Contains(t, out, "ct_lb_mark(backends=", "Pod traffic must reach the static LB")
	}
	out := trace("\"router\"", "ip4.src == 198.51.100.20", "ip4.dst == 192.0.2.10")
	require.NotContains(t, out, "ct_lb_mark(backends=", "router ingress must bypass the switch LB")
	// Model a pure L2 underlay: no router port, and kube-ovn's default
	// ls-dnat-mod-dl-dst option directs the translated frame to its backend.
	run("ovn-nbctl", "--db="+endpoint, "lsp-del", "router", "--", "set", "NB_Global", ".", "options:ls_dnat_mod_dl_dst=true")
	sync()
	out = trace("\"client\"", "ip4.src == 10.0.0.10", "ip4.dst == 10.0.0.99", "00:00:00:00:00:99")
	require.Contains(t, out, "ct_lb_mark(backends=")
	require.Contains(t, out, "output to \"backend\"")
	out = trace("\"localnet.subnet\"", "ip4.src == 198.51.100.20", "ip4.dst == 192.0.2.10")
	require.NotContains(t, out, "output;", "guarded external traffic must not be forwarded")
	require.NotContains(t, out, "ct_lb_mark(backends=", "external localnet traffic must not enter the Cluster LB")
	out = trace("\"localnet.subnet\"", "ip6.src == 2001:db8:ffff::20", "ip6.dst == 2001:db8::10")
	require.NotContains(t, out, "output;")
	require.NotContains(t, out, "ct_lb_mark(backends=")
	// Show that the localnet assertion detects the actual bypass when its
	// guard is removed, then restore the complete desired state.
	require.NoError(t, nb.DeleteAcls("subnet", ovs.LogicalSwitchKey, "", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"}))
	sync()
	out = trace("\"localnet.subnet\"", "ip4.src == 198.51.100.20", "ip4.dst == 192.0.2.10")
	require.Contains(t, out, "ct_lb_mark(backends=", "unguarded control must reproduce external LB bypass")
	require.NoError(t, ctrl.reconcileServiceEndpointSlices(svc, endpoints))
	sync()
}
