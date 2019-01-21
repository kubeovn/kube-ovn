package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/request"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"net"
	"runtime"
	"strings"
)

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func main() {
	skel.PluginMain(cmdAdd, cmdDel, version.All)
}

func cmdAdd(args *skel.CmdArgs) error {
	var err error

	n, cniVersion, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}
	podName, err := parseValueFromArgs("K8S_POD_NAME", args.Args)
	if err != nil {
		return err
	}
	podNamespace, err := parseValueFromArgs("K8S_POD_NAMESPACE", args.Args)
	if err != nil {
		return err
	}
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	client := request.NewCniServerClient(n.ServerSocket)

	hostNicName, containerNicName := generateNicName(args.ContainerID)
	veth := netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostNicName}, PeerName: containerNicName}
	defer func() {
		if err != nil {
			netlink.LinkDel(&veth)
		}
	}()

	err = netlink.LinkAdd(&veth)
	if err != nil {
		return fmt.Errorf("failed to crate veth for %s %v", podName, err)
	}

	res, err := client.Add(request.PodRequest{
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns})
	if err != nil {
		return err
	}

	result := generateCNIResult(cniVersion, res)
	macAddr, err := net.ParseMAC(res.MacAddress)
	if err != nil {
		return fmt.Errorf("failed to parse mac")
	}

	err = configureHostNic(hostNicName, macAddr)
	if err != nil {
		return err
	}

	err = configureContainerNic(containerNicName, result.IPs[0].Address.String(), macAddr, netns)
	if err != nil {
		return err
	}
	return types.PrintResult(&result, cniVersion)
}

func generateCNIResult(cniVersion string, podResponse *request.PodResponse) current.Result {
	result := current.Result{CNIVersion: cniVersion}
	_, mask, _ := net.ParseCIDR(podResponse.CIDR)
	ip := current.IPConfig{
		Version: "4",
		Address: net.IPNet{IP: net.ParseIP(podResponse.IpAddress).To4(), Mask: mask.Mask},
		Gateway: net.ParseIP(podResponse.Gateway).To4(),
	}
	result.IPs = []*current.IPConfig{&ip}

	route := types.Route{}
	route.Dst = net.IPNet{IP: net.ParseIP("0.0.0.0").To4(), Mask: net.CIDRMask(0, 32)}
	route.GW = net.ParseIP(podResponse.Gateway).To4()
	result.Routes = []*types.Route{&route}
	return result
}

func configureHostNic(nicName string, macAddr net.HardwareAddr) error {
	hostLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find host nic %s %v", nicName, err)
	}

	err = netlink.LinkSetHardwareAddr(hostLink, macAddr)
	if err != nil {
		return fmt.Errorf("can not set mac address to host nic %s %v", nicName, err)
	}
	err = netlink.LinkSetUp(hostLink)
	if err != nil {
		return fmt.Errorf("can not set host nic %s up %v", nicName, err)
	}
	return nil
}

func configureContainerNic(nicName, ipAddr string, macaddr net.HardwareAddr, netns ns.NetNS) error {
	containerLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find container nic %s %v", nicName, err)
	}

	err = netlink.LinkSetNsFd(containerLink, int(netns.Fd()))
	if err != nil {
		return fmt.Errorf("failed to link netns %v", err)
	}

	return ns.WithNetNSPath(netns.Path(), func(_ ns.NetNS) error {
		err = netlink.LinkSetName(containerLink, "eth0")
		if err != nil {
			return err
		}
		addr, err := netlink.ParseAddr(ipAddr)
		if err != nil {
			return fmt.Errorf("can not parse %s %v", ipAddr, err)
		}
		err = netlink.AddrAdd(containerLink, addr)
		if err != nil {
			return fmt.Errorf("can not add address to container nic %v", err)
		}

		err = netlink.LinkSetHardwareAddr(containerLink, macaddr)
		if err != nil {
			return fmt.Errorf("can not set mac address to container nic %v", err)
		}
		err = netlink.LinkSetUp(containerLink)
		if err != nil {
			return fmt.Errorf("can not set container nic %s up %v", nicName, err)
		}
		return nil
	})
}

func generateNicName(containerID string) (string, string) {
	return fmt.Sprintf("%s_h", containerID[0:12]), fmt.Sprintf("%s_c", containerID[0:12])
}

func cmdDel(args *skel.CmdArgs) error {
	n, _, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}

	client := request.NewCniServerClient(n.ServerSocket)
	podName, err := parseValueFromArgs("K8S_POD_NAME", args.Args)
	if err != nil {
		return err
	}
	podNamespace, err := parseValueFromArgs("K8S_POD_NAMESPACE", args.Args)
	if err != nil {
		return err
	}

	hostNicName, _ := generateNicName(args.ContainerID)
	l, err := netlink.LinkByName(hostNicName)
	if _, ok := err.(netlink.LinkNotFoundError); ok {
		return client.Del(request.PodRequest{
			PodName:      podName,
			PodNamespace: podNamespace,
			ContainerID:  args.ContainerID,
			NetNs:        args.Netns})
	}
	if err != nil {
		return fmt.Errorf("failed to get link %s %v", l.Attrs().Name, err)
	}

	err = netlink.LinkDel(l)
	if err != nil {
		return fmt.Errorf("delete link %s failed %v", l.Attrs().Name, err)
	}
	return client.Del(request.PodRequest{
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns})
}

type NetConf struct {
	types.NetConf
	ServerSocket string `json:"server_socket"`
}

func loadNetConf(bytes []byte) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}
	if n.ServerSocket == "" {
		return nil, "", fmt.Errorf("server_socket is required in cni.conf")
	}
	return n, n.CNIVersion, nil
}

func parseValueFromArgs(key, argString string) (string, error) {
	if argString == "" {
		return "", errors.New("CNI_ARGS is required")
	}
	args := strings.Split(argString, ";")
	for _, arg := range args {
		if strings.HasPrefix(arg, fmt.Sprintf("%s=", key)) {
			podName := strings.TrimPrefix(arg, fmt.Sprintf("%s=", key))
			if len(podName) > 0 {
				return podName, nil
			}
		}
	}
	return "", fmt.Errorf("%s is required in CNI_ARGS", key)
}
