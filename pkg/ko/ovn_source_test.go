package ko

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func localnetTestClient(t *testing.T, extraNodes ...*corev1.Node) (*Application, *recordingExecutor, *Client) {
	t.Helper()
	labels := map[string]string{
		"provider.provider-network.kubernetes.io/ready":     "true",
		"provider.provider-network.kubernetes.io/interface": "eno1",
	}
	objects := []runtime.Object{
		readyPod("central", "worker", "ovn-central", map[string]string{"ovn-nb-leader": "true", "ovn-sb-leader": "true"}),
		readyPod("ovs", "worker", "openvswitch", map[string]string{"app": "ovs"}),
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker", Labels: labels}},
	}
	for _, node := range extraNodes {
		objects = append(objects, node)
	}
	app, executor, _, _ := testApplication(t, objects...)
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = false
	client.Dynamic = fake.NewSimpleDynamicClient(runtime.NewScheme(),
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "kubeovn.io/v1", "kind": "Subnet", "metadata": map[string]any{"name": "underlay"}, "spec": map[string]any{"vlan": "vlan-a", "cidrBlock": "10.0.0.0/24"}}},
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "kubeovn.io/v1", "kind": "Vlan", "metadata": map[string]any{"name": "vlan-a"}, "spec": map[string]any{"provider": "provider", "id": int64(100)}}},
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "kubeovn.io/v1", "kind": "ProviderNetwork", "metadata": map[string]any{"name": "provider"}}},
	)
	executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
		if argv[0] == "ovn-nbctl" && strings.Contains(strings.Join(argv, " "), "Logical_Switch_Port") {
			_, err := io.WriteString(streams.Out, `{"headings":["name","type","addresses","dynamic_addresses","external_ids","options"],"data":[["localnet.underlay","localnet","unknown",["set",[]],["map",[["ls","underlay"]]],["map",[["network_name","provider"]]]]]}`)
			return err
		}
		if argv[0] == "ovs-vsctl" {
			_, err := io.WriteString(streams.Out, `{"headings":["name","ofport"],"data":[["eno1",9]]}`)
			return err
		}
		return nil
	}
	return app, executor, client
}

func TestLocalnetTraceUsesProviderNICAndVLAN(t *testing.T) {
	for _, reference := range []string{"localnet/underlay", "lsp/localnet.underlay"} {
		t.Run(reference, func(t *testing.T) {
			app, executor, _ := localnetTestClient(t)
			err := app.Execute(t.Context(), []string{"trace", reference, "--target", "addr/10.0.0.8", "--source-ip", "10.0.0.2", "--source-mac", "02:00:00:00:00:02", "--dst-mac", "02:00:00:00:00:08", "--node", "worker"})
			require.NoError(t, err)
			var ovn, ovs []string
			for _, call := range executor.calls {
				switch call.argv[0] {
				case "ovn-trace":
					ovn = call.argv
				case "ovs-appctl":
					ovs = call.argv
				}
			}
			require.Contains(t, ovn, "underlay")
			require.Equal(t, "br-"+"provider", ovs[2])
			require.Contains(t, ovs[3], "in_port=9")
			require.Contains(t, ovs[3], "dl_vlan=100")
		})
	}
}

func TestLocalnetCaptureUsesProviderNIC(t *testing.T) {
	for _, reference := range []string{"localnet/underlay", "lsp/localnet.underlay"} {
		t.Run(reference, func(t *testing.T) {
			app, executor, _ := localnetTestClient(t)
			require.NoError(t, app.Execute(t.Context(), []string{"capture", reference, "--", "-c", "1"}))
			var capture []string
			for _, call := range executor.calls {
				if call.argv[0] == "capture" {
					capture = call.argv
				}
			}
			require.Equal(t, []string{"capture", "--netns", "/proc/1/ns/net", "--interface", "eno1", "--count", "1"}, capture)
		})
	}
	app, _, _ := localnetTestClient(t)
	for _, args := range [][]string{{"capture", "localnet/underlay", "--node", "worker", "--", "-i", "eth0"}, {"trace", "localnet/underlay", "--target", "addr/10.0.0.8"}} {
		require.Error(t, app.Execute(t.Context(), args))
	}
	require.ErrorContains(t, app.Execute(t.Context(), []string{"trace", "lsp/localnet.underlay", "--target", "addr/10.0.0.8"}), "requires --source-ip and --source-mac")
}

func TestLocalnetMultipleNodesRequireSelection(t *testing.T) {
	ready := map[string]string{
		"provider.provider-network.kubernetes.io/ready":     "true",
		"provider.provider-network.kubernetes.io/interface": "eno2",
	}
	_, _, client := localnetTestClient(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-b", Labels: ready}})
	_, err := client.ovnSource(t.Context(), sourceRef{kind: sourceKindLocalnet, name: "underlay"}, "", true)
	require.ErrorContains(t, err, "specify --node")
}

func TestLocalnetExchangeLinkNameUsesRenamedNIC(t *testing.T) {
	_, _, client := localnetTestClient(t)
	provider, err := client.Dynamic.Resource(providerNetworkResource).Get(t.Context(), "provider", metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(provider.Object, true, "spec", "exchangeLinkName"))
	_, err = client.Dynamic.Resource(providerNetworkResource).Update(t.Context(), provider, metav1.UpdateOptions{})
	require.NoError(t, err)
	source, err := client.ovnSource(t.Context(), sourceRef{kind: sourceKindLocalnet, name: "underlay"}, "worker", true)
	require.NoError(t, err)
	require.Equal(t, "eno1", source.bridge)
	require.Equal(t, "br-provider", source.physicalNIC)
}

func ovnPortTestClient(t *testing.T) (*Application, *recordingExecutor) {
	t.Helper()
	app, executor, _, _ := testApplication(t,
		readyPod("central", "worker", "ovn-central", map[string]string{"ovn-nb-leader": "true", "ovn-sb-leader": "true"}),
		readyPod("ovs", "worker", "openvswitch", map[string]string{"app": "ovs"}),
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker"}},
	)
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = false
	executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
		if argv[0] == "ovn-nbctl" {
			command := strings.Join(argv, " ")
			switch {
			case strings.Contains(command, "Logical_Router_Port"):
				_, err = io.WriteString(streams.Out, `{"headings":["name","mac","networks","external_ids"],"data":[["router-underlay","02:00:00:00:00:01","10.0.0.1/24",["map",[["lr","router"]]]]]}`)
			case strings.Contains(command, "type=\"router\""):
				_, err = io.WriteString(streams.Out, `{"headings":["name","options","external_ids"],"data":[["underlay-router",["map",[["router-port","router-underlay"]]],["map",[["ls","underlay"]]]]]}`)
			case strings.Contains(command, "--columns=name,type,external_ids"):
				_, err = io.WriteString(streams.Out, `{"headings":["name","type","external_ids"],"data":[["web.app","",["map",[["ls","underlay"]]]]]}`)
			default:
				_, err = io.WriteString(streams.Out, `{"headings":["name","type","addresses","dynamic_addresses","external_ids","options"],"data":[["web.app","","02:00:00:00:00:02 10.0.0.2",["set",[]],["map",[["ls","underlay"]]],["map",[]]]]}`)
			}
			return err
		}
		if argv[0] == "ovn-sbctl" {
			if strings.Contains(strings.Join(argv, " "), "Port_Binding") {
				_, err = io.WriteString(streams.Out, `{"headings":["logical_port","chassis"],"data":[["web.app",["uuid","aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"]]]}`)
			} else {
				_, err = io.WriteString(streams.Out, `{"headings":["hostname"],"data":[["worker"]]}`)
			}
			return err
		}
		if argv[0] == "ovs-vsctl" {
			_, err = io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[["veth-web",["map",[]],7]]}`)
			return err
		}
		return nil
	}
	return app, executor
}

func TestOVNPortTraceAndCapture(t *testing.T) {
	app, executor := ovnPortTestClient(t)
	require.NoError(t, app.Execute(t.Context(), []string{"trace", "lsp/web.app", "--target", "addr/10.0.0.8", "--dst-mac", "02:00:00:00:00:08"}))
	require.NoError(t, app.Execute(t.Context(), []string{"trace", "lrp/router-underlay", "--target", "addr/10.0.0.8"}))
	require.NoError(t, app.Execute(t.Context(), []string{"capture", "lsp/web.app", "--", "-c", "1"}))
	require.NoError(t, app.Execute(t.Context(), []string{"capture", "lrp/router-underlay", "--observe-at", "lsp/web.app", "--", "-c", "1"}))
	var ovnDatapaths, ovsBridges []string
	var captures [][]string
	for _, call := range executor.calls {
		switch call.argv[0] {
		case "ovn-trace":
			ovnDatapaths = append(ovnDatapaths, call.argv[len(call.argv)-2])
		case "ovs-appctl":
			ovsBridges = append(ovsBridges, call.argv[2])
		case "capture":
			captures = append(captures, call.argv)
		}
	}
	require.Equal(t, []string{"underlay", "router"}, ovnDatapaths)
	require.Equal(t, []string{"br-int"}, ovsBridges)
	require.Len(t, captures, 2)
	for _, capture := range captures {
		require.Equal(t, []string{"capture", "--netns", "/proc/1/ns/net", "--interface", "veth-web", "--count", "1"}, capture)
	}
}

func TestLRPCaptureRejectsNonAdjacentLSP(t *testing.T) {
	app, executor := ovnPortTestClient(t)
	executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
		if argv[0] != "ovn-nbctl" {
			return nil
		}
		if strings.Contains(strings.Join(argv, " "), "Logical_Router_Port") {
			_, err := io.WriteString(streams.Out, `{"headings":["name","mac","networks","external_ids"],"data":[["router-underlay","02:00:00:00:00:01","10.0.0.1/24",["map",[["lr","router"]]]]]}`)
			return err
		}
		_, err := io.WriteString(streams.Out, `{"headings":["name","options","external_ids"],"data":[]}`)
		return err
	}
	err := app.Execute(t.Context(), []string{"capture", "lrp/router-underlay", "--observe-at", "lsp/web.app", "--", "-c", "1"})
	require.ErrorContains(t, err, "no adjacent logical switch")
}

func TestInternalLSPCaptureUsesInterfaceNamespace(t *testing.T) {
	app, executor := ovnPortTestClient(t)
	original := executor.run
	executor.run = func(ctx context.Context, target Target, argv []string, streams Streams) error {
		if argv[0] == "ovs-vsctl" {
			_, err := io.WriteString(streams.Out, `{"headings":["name","type","external_ids","ofport"],"data":[["nic-web","internal",["map",[["pod_netns","/var/run/netns/web"]]],7]]}`)
			return err
		}
		return original(ctx, target, argv, streams)
	}
	require.NoError(t, app.Execute(t.Context(), []string{"capture", "lsp/web.app", "--", "-c", "1"}))
	require.Equal(t, []string{"capture", "--netns", "/var/run/netns/web", "--interface", "nic-web", "--count", "1"}, executor.calls[len(executor.calls)-1].argv)
}
