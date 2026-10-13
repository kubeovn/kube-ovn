package ko

import (
	"context"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTraceValidation(t *testing.T) {
	for _, args := range [][]string{
		{"p", "invalid", "icmp"},
		{"p", "::1", "arp"},
		{"p", "10.0.0.1", "tcp"},
		{"p", "10.0.0.1", "tcp", "65536"},
		{"p", "10.0.0.1", "tcp", "0"},
		{"p", "10.0.0.1", "udp", "80;touch"},
		{"p", "10.0.0.1", "arp", "invalid"},
		{"p", "10.0.0.1", "icmp", "80"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { _, err := parseTrace(args); require.Error(t, err) })
	}
}

func TestTracePlans(t *testing.T) {
	source := &networkSource{lsp: "web.app", addresses: []string{"10.0.0.2", "fd00::2"}, annotations: map[string]string{
		annotationPrefix + "logical_switch": "subnet", annotationPrefix + "mac_address": "00:00:00:00:00:02", annotationPrefix + "ip_address": "10.0.0.2,fd00::2",
	}}
	tests := []struct {
		args     []string
		ovn, ovs string
	}{
		{[]string{"p", "10.0.0.3", "00:00:00:00:00:03", "tcp", "443"}, "tcp.dst == 443 && tcp.flags == 2", "tcp_dst=443"},
		{[]string{"p", "fd00::3", "00:00:00:00:00:03", "icmp"}, "icmp6.type == 128", "icmp6,nw_ttl=64,ipv6_src=fd00::2"},
		{[]string{"p", "10.0.0.3", "arp"}, "arp.op == 1", "arp_op=1"},
		{[]string{"p", "10.0.0.3", "00:00:00:00:00:03", "arp", "reply"}, "arp.tha == 00:00:00:00:00:03", "arp_tha=00:00:00:00:00:03"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			request, err := parseTrace(tt.args)
			require.NoError(t, err)
			plan, err := prepareTrace(source, request)
			require.NoError(t, err)
			require.Contains(t, strings.Join(plan.ovnCommand(), " "), tt.ovn)
			require.Contains(t, strings.Join(plan.ovsCommand(7), " "), tt.ovs)
		})
	}
}

func TestIPv6CIDRMembership(t *testing.T) {
	require.True(t, sameSubnet(netip.MustParseAddr("fd00::ffff"), "10.0.0.0/24,fd00::/64"))
	require.False(t, sameSubnet(netip.MustParseAddr("fd00:0:0:8000::1"), "fd00::/65"))
	require.True(t, sameSubnet(netip.MustParseAddr("10.0.0.0"), "10.0.0.0/31"))
}

func TestTCPDumpUsesNetNSAndPreservesBinary(t *testing.T) {
	for _, internal := range []bool{false, true} {
		t.Run(strconv.FormatBool(internal), func(t *testing.T) {
			pod := &corev1.Pod{Name: "web", Namespace: "app", UID: "web-id", Spec: corev1.PodSpec{NodeName: "node-a"}, Annotations: map[string]string{}}
			if internal {
				pod.Annotations[annotationPrefix+"pod_nic_type"] = "internal-port"
			}
			agent := readyPod("agent-a", "node-a", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
			app, executor, out, _ := testApplication(t, pod, agent, &corev1.Node{Name: "node-a"})
			client, err := app.newClient()
			require.NoError(t, err)
			client.ComponentFree = true
			app.client = client
			executor.run = func(_ context.Context, _ Target, argv []string, s Streams) error {
				if argv[0] == "ovs-vsctl" {
					_, err := io.WriteString(s.Out, `{"headings":["name","external_ids","ofport"],"data":[["nic-a",["map",[["pod_netns","/var/run/netns/pod-a"]]],4]]}`)
					return err
				}
				_, err := s.Out.Write([]byte{0, 255, 10, 13, 0})
				return err
			}
			require.NoError(t, app.Execute(t.Context(), []string{"capture", "--pod", "app/web", "--", "-w", "-", "-c", "1"}))
			nic := "eth0"
			if internal {
				nic = "nic-a"
			}
			require.Equal(t, []string{"capture", "--netns", "/var/run/netns/pod-a", "--interface", nic, "--count", "1", "--pcap"}, executor.calls[1].argv)
			require.Equal(t, "agent", executor.calls[1].target.Container)
			// The native capture writes a pcap stream; the executor fixture keeps
			// its binary payload unchanged to verify that stdout is not decorated.
			require.Equal(t, []byte{0, 255, 10, 13, 0}, out.Bytes())
		})
	}
}

func TestTCPDumpFallsBackToPodNetNS(t *testing.T) {
	pod := &corev1.Pod{Name: "web", Namespace: "app", UID: "web-id", Spec: corev1.PodSpec{NodeName: "node-a"}, Annotations: map[string]string{}}
	agent := readyPod("agent-a", "node-a", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
	app, executor, _, _ := testApplication(t, pod, agent, &corev1.Node{Name: "node-a"})
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = true
	app.client = client
	executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
		switch argv[0] {
		case "ovs-vsctl":
			_, err := io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[]}`)
			return err
		case "/kube-ovn/kubectl-ko-node-agent":
			_, err := io.WriteString(streams.Out, "/proc/42/ns/net\n")
			return err
		default:
			_, err := streams.Out.Write([]byte{0, 255, 10, 13, 0})
			return err
		}
	}

	require.NoError(t, app.Execute(t.Context(), []string{"capture", "--pod", "app/web", "--", "-c", "1"}))
	var captureCall []string
	for _, call := range executor.calls {
		if len(call.argv) > 0 && call.argv[0] == "capture" {
			captureCall = call.argv
			break
		}
	}
	require.Equal(t, []string{"capture", "--netns", "/proc/42/ns/net", "--interface", "eth0", "--count", "1"}, captureCall)
}

func TestVMPortResolvedBeforeInterfaceLookup(t *testing.T) {
	pod := &corev1.Pod{Name: "virt-launcher", Namespace: "app", Spec: corev1.PodSpec{NodeName: "node-a"}, OwnerReferences: []metav1.OwnerReference{{Kind: "VirtualMachineInstance", Name: "vm-a"}}}
	app, _, _, _ := testApplication(t, pod)
	client, err := app.newClient()
	require.NoError(t, err)
	source, err := client.networkSource(t.Context(), "app/virt-launcher")
	require.NoError(t, err)
	require.Equal(t, "vm-a.app", source.lsp)
}
