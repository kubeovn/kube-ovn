package docker

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"k8s.io/utils/ptr"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const MTU = 1500

// Base network for test networks: 172.28.0.0/16
// This allows 256 /24 subnets (172.28.0.0/24 to 172.28.255.0/24)
const testNetworkBase = "172.28"

// GenerateRandomSubnets generates N random /24 subnets within 172.28.0.0/16
// Returns a slice of subnets in CIDR notation (e.g., ["172.28.123.0/24", "172.28.124.0/24"])
// Subnets are guaranteed to be unique within the returned slice
func GenerateRandomSubnets(count int) []string {
	if count <= 0 || count > 256 {
		panic(fmt.Sprintf("invalid subnet count: %d (must be 1-256)", count))
	}

	// Use current nanosecond timestamp as seed for better randomness
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate a random permutation of subnet indices
	subnets := make([]string, count)
	usedOctets := make(map[int]bool)

	for i := 0; i < count; i++ {
		var octet int
		// Find an unused octet
		for {
			octet = rng.Intn(256)
			if !usedOctets[octet] {
				usedOctets[octet] = true
				break
			}
		}
		subnets[i] = fmt.Sprintf("%s.%d.0/24", testNetworkBase, octet)
	}

	return subnets
}

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

// NetworkCreateWithSubnet creates a docker network with specified IPv4 subnet
func NetworkCreateWithSubnet(name, ipv4Subnet string, ipv6, skipIfExists bool) (*network.Inspect, error) {
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

	// Add IPv4 subnet if specified
	if ipv4Subnet != "" {
		gateway, err := util.FirstIP(ipv4Subnet)
		if err != nil {
			return nil, fmt.Errorf("failed to get gateway for subnet %s: %w", ipv4Subnet, err)
		}
		config := network.IPAMConfig{
			Subnet:  netip.MustParsePrefix(ipv4Subnet),
			Gateway: netip.MustParseAddr(gateway),
		}
		options.IPAM.Config = append(options.IPAM.Config, config)
	}

	// Add IPv6 subnet if enabled
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
		// Handle race condition: if network was created between our check and create attempt
		// Docker returns error like "network with name xxx already exists"
		if skipIfExists && strings.Contains(err.Error(), "already exists") {
			// Network already exists, retrieve and return it
			network, getErr := getNetwork(name, false)
			if getErr != nil {
				return nil, getErr
			}
			return network, nil
		}
		return nil, err
	}

	return getNetwork(name, false)
}

// NetworkCreate creates a docker network (backward compatible wrapper)
func NetworkCreate(name string, ipv6, skipIfExists bool) (*network.Inspect, error) {
	return NetworkCreateWithSubnet(name, "", ipv6, skipIfExists)
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
