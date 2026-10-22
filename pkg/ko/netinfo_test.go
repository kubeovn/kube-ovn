package ko

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestIPLinksExposeAddressesAndHostVethPeer(t *testing.T) {
	var podLinks []ipJSONLink
	err := json.Unmarshal([]byte(`[
        {"ifindex":2,"ifname":"eth0","link_index":42,"mtu":1500,"operstate":"UP","address":"0a:58:0a:f4:00:02","flags":["BROADCAST","UP"],"link_type":"ether","linkinfo":{"info_kind":"veth"},"addr_info":[{"family":"inet","local":"10.244.0.2","prefixlen":24}]}
    ]`), &podLinks)
	if err != nil {
		t.Fatal(err)
	}
	var hostLinks []ipJSONLink
	err = json.Unmarshal([]byte(`[
        {"ifindex":42,"ifname":"pod123_h","link_index":2,"mtu":1500,"operstate":"UP","address":"aa:bb:cc:dd:ee:ff","flags":["BROADCAST","UP"],"link_type":"ether","linkinfo":{"info_kind":"veth"}}
    ]`), &hostLinks)
	if err != nil {
		t.Fatal(err)
	}
	hostByIndex := map[int]networkLink{hostLinks[0].Index: hostLinks[0].networkLink()}
	item := podLinks[0].podNetworkInterface()
	peer, ok := hostByIndex[item.PeerIndex]
	if !ok {
		t.Fatal("pod veth peer was not found")
	}
	item.HostPeer = new(peer)

	if item.Name != "eth0" || item.Kind != "veth" || item.Addresses[0] != "10.244.0.2/24" {
		t.Fatalf("unexpected pod interface: %#v", item)
	}
	if item.HostPeer.Name != "pod123_h" || item.HostPeer.Index != 42 {
		t.Fatalf("unexpected host peer: %#v", item.HostPeer)
	}
}

func TestNetworkInspectWithoutOVSInterface(t *testing.T) {
	for _, queryError := range []bool{false, true} {
		name := "no OVS row"
		if queryError {
			name = "OVS unavailable"
		}
		t.Run(name, func(t *testing.T) {
			pod := &corev1.Pod{Name: "web", Namespace: "app", UID: "pod-uid", Spec: corev1.PodSpec{NodeName: "worker-a"}}
			agent := readyPod("agent-a", "worker-a", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
			app, executor, out, _ := testApplication(t, pod, agent, &corev1.Node{Name: "worker-a"})
			client, err := app.newClient()
			require.NoError(t, err)
			client.ComponentFree = true
			executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
				require.Equal(t, "agent-a", target.Pod)
				switch argv[0] {
				case "ovs-vsctl":
					if queryError {
						return errors.New("OVS is unavailable")
					}
					_, err := io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[]}`)
					return err
				case "/kube-ovn/kubectl-ko-node-agent":
					require.Equal(t, []string{"/kube-ovn/kubectl-ko-node-agent", "netns", "pod-uid"}, argv)
					_, err := io.WriteString(streams.Out, "/proc/42/ns/net\n")
					return err
				case "nsenter":
					_, err := io.WriteString(streams.Out, `[{"ifindex":5,"ifname":"net1","link_index":5,"linkinfo":{"info_kind":"ipvlan"}}]`)
					return err
				case "ip":
					_, err := io.WriteString(streams.Out, `[{"ifindex":5,"ifname":"parent0","link_type":"ether"}]`)
					return err
				default:
					t.Fatalf("unexpected command: %v", argv)
					return nil
				}
			}
			require.NoError(t, app.Execute(t.Context(), []string{"network", "inspect", "--pod", "app/web", "--output", "json"}))
			var info podNetworkInfo
			require.NoError(t, json.Unmarshal(out.Bytes(), &info))
			require.Equal(t, "/proc/42/ns/net", info.NetNS)
			require.Len(t, info.Interfaces, 1)
			require.NotNil(t, info.Interfaces[0].Parent, "ifindexes can coincide across namespaces")
			require.Equal(t, "parent0", info.Interfaces[0].Parent.Name)
		})
	}
}

func TestHostNetworkPeerIndexesDoNotAliasHostNICs(t *testing.T) {
	pod := &corev1.Pod{Name: "web", Namespace: "app", Spec: corev1.PodSpec{NodeName: "worker-a", HostNetwork: true}}
	ovs := readyPod("ovs-a", "worker-a", "openvswitch", map[string]string{"app": "ovs"})
	app, executor, out, _ := testApplication(t, pod, ovs, &corev1.Node{Name: "worker-a"})
	executor.run = func(_ context.Context, _ Target, _ []string, streams Streams) error {
		_, err := io.WriteString(streams.Out, `[{"ifindex":2,"ifname":"eth0","link_type":"ether"},{"ifindex":42,"ifname":"pod_h","link_index":2,"link_netnsid":0,"linkinfo":{"info_kind":"veth"}}]`)
		return err
	}
	require.NoError(t, app.Execute(t.Context(), []string{"network", "inspect", "--pod", "app/web", "--output", "json"}))
	var info podNetworkInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &info))
	require.Nil(t, info.Interfaces[1].Peer, "peer ifindex 2 belongs to another netns, not host eth0")
	require.Equal(t, 2, info.Interfaces[1].PeerIndex)
}

func TestIPLinksExposeMacvlanParent(t *testing.T) {
	var podLinks []ipJSONLink
	err := json.Unmarshal([]byte(`[
        {"ifindex":5,"ifname":"net1","link_index":10,"link":"eth0","mtu":1500,"operstate":"UP","address":"02:00:00:00:00:05","flags":["BROADCAST","UP"],"link_type":"ether","linkinfo":{"info_kind":"macvlan"},"addr_info":[{"family":"inet","local":"192.0.2.5","prefixlen":24}]}
    ]`), &podLinks)
	if err != nil {
		t.Fatal(err)
	}
	var hostLinks []ipJSONLink
	err = json.Unmarshal([]byte(`[
        {"ifindex":10,"ifname":"eth0","mtu":1500,"operstate":"UP","address":"aa:bb:cc:dd:ee:01","flags":["BROADCAST","UP"],"link_type":"ether"}
    ]`), &hostLinks)
	if err != nil {
		t.Fatal(err)
	}
	hostByName := map[string]networkLink{hostLinks[0].Name: hostLinks[0].networkLink()}
	hostByIndex := map[int]networkLink{hostLinks[0].Index: hostLinks[0].networkLink()}
	item := podLinks[0].podNetworkInterface()
	parent, ok := podLinks[0].parentLink(hostByName, hostByIndex, nil)
	if !ok {
		t.Fatal("macvlan parent was not found")
	}
	item.Parent = new(parent)

	if item.Kind != "macvlan" || item.Parent.Name != "eth0" || item.Parent.Index != 10 {
		t.Fatalf("unexpected macvlan parent: %#v", item)
	}
}

func TestWritePodNetworkIncludesNetnsAndPeer(t *testing.T) {
	info := &podNetworkInfo{
		Namespace: "app", Name: "web", Node: "worker-a", NetNS: "/var/run/netns/pod", Interfaces: []podNetworkInterface{{
			Name: "eth0", Index: 2, Kind: "veth", MAC: "0a:58:0a:f4:00:02", MTU: 1500, OperState: "UP", Addresses: []string{"10.244.0.2/24"},
			HostPeer: &networkLink{Name: "pod123_h", Index: 42, MAC: "aa:bb:cc:dd:ee:ff", OperState: "UP"},
		}},
	}
	var out bytes.Buffer
	if err := writePodNetwork(&out, info); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/var/run/netns/pod", "eth0", "10.244.0.2/24", "host peer: pod123_h"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q: %s", want, out.String())
		}
	}
}

func TestNetworkInspectReadsPodAndHostLinks(t *testing.T) {
	pod := &corev1.Pod{Name: "web", Namespace: "app", Spec: corev1.PodSpec{NodeName: "worker-a"}}
	ovs := readyPod("ovs-a", "worker-a", "openvswitch", map[string]string{"app": "ovs"})
	app, executor, out, _ := testApplication(t, pod, ovs, &corev1.Node{Name: "worker-a"})
	executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
		switch argv[0] {
		case "ovs-vsctl":
			_, err := io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[["pod123_h",["map",[["pod_name","web"],["pod_namespace","app"],["pod_netns","/var/run/netns/pod"]]],4]]}`)
			return err
		case "nsenter":
			if !slices.Contains(argv, "-s") {
				t.Errorf("pod netns ip command lacks -s: %v", argv)
			}
			_, err := io.WriteString(streams.Out, `[{"ifindex":2,"ifname":"eth0","link_index":42,"stats64":{"rx":{"bytes":9007199254740993,"packets":12},"tx":{"bytes":21,"packets":8}},"mtu":1500,"operstate":"UP","address":"0a:58:0a:f4:00:02","flags":["BROADCAST","UP"],"link_type":"ether","linkinfo":{"info_kind":"veth"},"addr_info":[{"family":"inet","local":"10.244.0.2","prefixlen":24}]}]`)
			return err
		case "ip":
			if !slices.Contains(argv, "-s") {
				t.Errorf("host netns ip command lacks -s: %v", argv)
			}
			_, err := io.WriteString(streams.Out, `[{"ifindex":42,"ifname":"pod123_h","link_index":2,"stats64":{"rx":{"bytes":0,"packets":0},"tx":{"bytes":21,"packets":8}},"mtu":1500,"operstate":"UP","address":"aa:bb:cc:dd:ee:ff","flags":["BROADCAST","UP"],"link_type":"ether","linkinfo":{"info_kind":"veth"}}]`)
			return err
		default:
			return nil
		}
	}
	if err := app.Execute(t.Context(), []string{"network", "inspect", "--pod", "app/web"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Network namespace: /var/run/netns/pod", "eth0", "10.244.0.2/24", "host peer: pod123_h", "RX: bytes=9007199254740993 packets=12", "RX: bytes=0 packets=0", "TX: bytes=21 packets=8"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q: %s", want, out.String())
		}
	}
}

func TestNetworkStatisticsPreserveCounters(t *testing.T) {
	for _, fixture := range []struct {
		name, input string
		bytes       uint64
	}{
		{"stats64", `"stats64":{"rx":{"bytes":9007199254740993,"packets":12,"errors":3},"tx":{"bytes":21,"packets":8,"dropped":2}}`, 9007199254740993},
		{"stats", `"stats":{"rx":{"bytes":123,"packets":12,"errors":3},"tx":{"bytes":21,"packets":8,"dropped":2}}`, 123},
		{"prefer stats64", `"stats":{"rx":{"bytes":1}},"stats64":{"rx":{"bytes":321,"packets":12,"errors":3},"tx":{"bytes":21,"packets":8,"dropped":2}}`, 321},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var link ipJSONLink
			require.NoError(t, json.Unmarshal([]byte(`{"ifindex":2,"ifname":"eth0",`+fixture.input+`}`), &link))
			item := link.podNetworkInterface()
			peer := link.networkLink()
			for _, related := range []string{"host peer", "parent", "peer"} {
				t.Run(related, func(t *testing.T) {
					switch related {
					case "host peer":
						item.HostPeer = &peer
					case "parent":
						item.HostPeer = nil
						item.Parent = &peer
					case "peer":
						item.Parent = nil
						item.Peer = &peer
					}
					data, err := json.Marshal(item)
					require.NoError(t, err)
					var decoded struct {
						Statistics *struct {
							RX map[string]uint64 `json:"rx"`
							TX map[string]uint64 `json:"tx"`
						} `json:"statistics"`
					}
					require.NoError(t, json.Unmarshal(data, &decoded))
					require.NotNil(t, decoded.Statistics, "ip -s counters must reach CLI output")
					require.Equal(t, fixture.bytes, decoded.Statistics.RX["bytes"])
					require.EqualValues(t, 2, decoded.Statistics.TX["dropped"])
					var out bytes.Buffer
					require.NoError(t, writePodNetwork(&out, &podNetworkInfo{Interfaces: []podNetworkInterface{item}}))
					require.Equal(t, 2, strings.Count(out.String(), "RX: "), "Pod and related link statistics must both be displayed")
					require.Equal(t, 2, strings.Count(out.String(), "TX: bytes=21 dropped=2 packets=8"))
				})
			}
		})
	}
}

func TestNetworkInspectResolvesParentNamespace(t *testing.T) {
	for _, kind := range []string{"macvlan", "ipvlan"} {
		for _, fixture := range []struct{ external, hostPresent, nameOnly bool }{{false, true, false}, {true, true, false}, {true, false, false}, {false, true, true}} {
			t.Run(kind+"/external="+strconv.FormatBool(fixture.external)+"/host="+strconv.FormatBool(fixture.hostPresent)+"/nameOnly="+strconv.FormatBool(fixture.nameOnly), func(t *testing.T) {
				pod := &corev1.Pod{Name: "web", Namespace: "app", Spec: corev1.PodSpec{NodeName: "worker-a"}}
				agent := readyPod("agent-a", "worker-a", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
				app, executor, out, _ := testApplication(t, pod, agent, &corev1.Node{Name: "worker-a"})
				client, err := app.newClient()
				require.NoError(t, err)
				client.ComponentFree = true
				executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
					require.Equal(t, "agent-a", target.Pod)
					switch argv[0] {
					case "ovs-vsctl":
						_, err := io.WriteString(streams.Out, `{"headings":["external_ids"],"data":[[["map",[["pod_netns","/var/run/netns/pod"]]]]]}`)
						return err
					case "nsenter":
						link := ipJSONLink{Index: 5, Name: "net1", LinkIndex: 2, LinkName: "eth0", LinkInfo: ipJSONLinkInfo{InfoKind: kind}}
						if fixture.nameOnly {
							link.LinkIndex = 0
							return json.MarshalWrite(streams.Out, []ipJSONLink{{Index: 133, Name: "eth0", Address: "02:00:00:00:00:02", LinkInfo: ipJSONLinkInfo{InfoKind: "veth"}}, link})
						}
						if fixture.external {
							link.LinkNetNSID = new(0)
						}
						return json.MarshalWrite(streams.Out, []ipJSONLink{{Index: 2, Name: "eth0", Address: "02:00:00:00:00:02", LinkInfo: ipJSONLinkInfo{InfoKind: "veth"}}, link})
					case "ip":
						if !fixture.hostPresent {
							_, err := io.WriteString(streams.Out, `[]`)
							return err
						}
						_, err := io.WriteString(streams.Out, `[{"ifindex":2,"ifname":"eth0","address":"02:00:00:00:00:01","link_type":"ether"}]`)
						return err
					default:
						t.Fatalf("unexpected command: %v", argv)
						return nil
					}
				}
				require.NoError(t, app.Execute(t.Context(), []string{"network", "inspect", "--pod", "app/web", "--output", "json"}))
				var info podNetworkInfo
				require.NoError(t, json.Unmarshal(out.Bytes(), &info))
				require.Len(t, info.Interfaces, 2)
				parent := info.Interfaces[1].Parent
				if !fixture.hostPresent {
					require.Nil(t, parent, "external parents must not alias a Pod-local interface")
					return
				}
				require.NotNil(t, parent)
				want := "02:00:00:00:00:02"
				if fixture.external {
					want = "02:00:00:00:00:01"
				}
				require.Equal(t, want, parent.MAC, "same name/index in different netns must not alias the parent NIC")
			})
		}
	}
}
