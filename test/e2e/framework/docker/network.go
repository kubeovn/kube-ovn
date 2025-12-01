package docker

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"k8s.io/utils/ptr"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const MTU = 1500

// https://github.com/kubernetes-sigs/kind/tree/main/pkg/cluster/internal/providers/docker/network.go#L313
// generateULASubnetFromName generate an IPv6 subnet based on the
// name and Nth probing attempt
func generateULASubnetFromName(name string, attempt int32) string {
	ip := make([]byte, 16)
	ip[0] = 0xfc
	ip[1] = 0x00
	h := sha1.New()
	_, _ = h.Write([]byte(name))
	_ = binary.Write(h, binary.LittleEndian, attempt)
	bs := h.Sum(nil)
	for i := 2; i < 8; i++ {
		ip[i] = bs[i]
	}
	subnet := &net.IPNet{
		IP:   net.IP(ip),
		Mask: net.CIDRMask(64, 128),
	}
	return subnet.String()
}

func getNetwork(name string, ignoreNotFound bool) (*network.Inspect, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	f := make(client.Filters, 1)
	f.Add("name", name)
	result, err := cli.NetworkList(context.Background(), client.NetworkListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	if len(result.Items) == 0 {
		if !ignoreNotFound {
			return nil, fmt.Errorf("network %s does not exist", name)
		}
		return nil, nil
	}

	info, err := cli.NetworkInspect(context.Background(), result.Items[0].ID, client.NetworkInspectOptions{})
	if err != nil {
		return nil, err
	}
	return &info.Network, nil
}

func NetworkInspect(name string) (*network.Inspect, error) {
	return getNetwork(name, false)
}

func NetworkCreate(name string, ipv6, skipIfExists bool) (*network.Inspect, error) {
	if skipIfExists {
		network, err := getNetwork(name, true)
		if err != nil {
			return nil, err
		}
		if network != nil {
			return network, nil
		}
	}

	options := client.NetworkCreateOptions{
		Driver:     "bridge",
		Attachable: true,
		IPAM: &network.IPAM{
			Driver: "default",
		},
		Options: map[string]string{
			"com.docker.network.bridge.enable_ip_masquerade": "true",
			"com.docker.network.driver.mtu":                  strconv.Itoa(MTU),
		},
	}
	if ipv6 {
		options.EnableIPv6 = ptr.To(true)
		subnet := generateULASubnetFromName(name, 0)
		gateway, err := util.FirstIP(subnet)
		if err != nil {
			return nil, err
		}
		config := network.IPAMConfig{
			Subnet:  netip.MustParsePrefix(subnet),
			Gateway: netip.MustParseAddr(gateway),
		}
		options.IPAM.Config = append(options.IPAM.Config, config)
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	if _, err = cli.NetworkCreate(context.Background(), name, options); err != nil {
		return nil, err
	}

	return getNetwork(name, false)
}

func NetworkConnect(networkID, containerID string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.NetworkConnect(context.Background(), networkID, client.NetworkConnectOptions{Container: containerID})
	return err
}

func NetworkDisconnect(networkID, containerID string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.NetworkDisconnect(context.Background(), networkID, client.NetworkDisconnectOptions{Container: containerID})
	return err
}

func NetworkRemove(networkID string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.NetworkRemove(context.Background(), networkID, client.NetworkRemoveOptions{})
	return err
}
