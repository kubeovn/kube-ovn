package docker

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
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

func getNetwork(ctx context.Context, name string, ignoreNotFound bool) (*network.Inspect, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	f := filters.NewArgs()
	f.Add("name", name)
	networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	if len(networks) == 0 {
		if !ignoreNotFound {
			return nil, fmt.Errorf("network %s does not exist", name)
		}
		return nil, nil
	}

	info, err := cli.NetworkInspect(ctx, networks[0].ID, network.InspectOptions{})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func NetworkInspect(ctx context.Context, name string) (*network.Inspect, error) {
	return getNetwork(ctx, name, false)
}

func NetworkCreate(ctx context.Context, name string, ipv6, skipIfExists bool) (*network.Inspect, error) {
	if skipIfExists {
		network, err := getNetwork(ctx, name, true)
		if err != nil {
			return nil, err
		}
		if network != nil {
			return network, nil
		}
	}

	options := network.CreateOptions{
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
			Subnet:  subnet,
			Gateway: gateway,
		}
		options.IPAM.Config = append(options.IPAM.Config, config)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	if _, err = cli.NetworkCreate(ctx, name, options); err != nil {
		return nil, err
	}

	return getNetwork(ctx, name, false)
}

func NetworkConnect(ctx context.Context, networkID, containerID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	return cli.NetworkConnect(ctx, networkID, containerID, nil)
}

func NetworkDisconnect(ctx context.Context, networkID, containerID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	return cli.NetworkDisconnect(ctx, networkID, containerID, false)
}

func NetworkRemove(ctx context.Context, networkID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.NetworkRemove(ctx, networkID)
}
