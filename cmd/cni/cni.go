package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/util"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
)

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func main() {
	skel.PluginMain(cmdAdd, nil, cmdDel, version.All, "")
}

func cmdAdd(args *skel.CmdArgs) error {
	var err error

	netConf, cniVersion, err := loadNetConf(args.StdinData)
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

	client := request.NewCniServerClient(netConf.ServerSocket)

	response, err := client.Add(request.CniRequest{
		CniType:      netConf.Type,
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns,
		IfName:       args.IfName,
		Provider:     netConf.Provider,
		DeviceID:     netConf.DeviceID,
	})
	if err != nil {
		return err
	}
	result := generateCNIResult(cniVersion, response)
	return types.PrintResult(&result, cniVersion)
}

func generateCNIResult(cniVersion string, cniResponse *request.CniResponse) current.Result {
	result := current.Result{CNIVersion: cniVersion}
	_, mask, _ := net.ParseCIDR(cniResponse.CIDR)
	switch cniResponse.Protocol {
	case kubeovnv1.ProtocolIPv4:
		ip, route := assignV4Address(cniResponse.IpAddress, cniResponse.Gateway, mask)
		result.IPs = []*current.IPConfig{&ip}
		result.Routes = []*types.Route{&route}
	case kubeovnv1.ProtocolIPv6:
		ip, route := assignV6Address(cniResponse.IpAddress, cniResponse.Gateway, mask)
		result.IPs = []*current.IPConfig{&ip}
		result.Routes = []*types.Route{&route}
	case kubeovnv1.ProtocolDual:
		var netMask *net.IPNet
		for _, cidrBlock := range strings.Split(cniResponse.CIDR, ",") {
			_, netMask, _ = net.ParseCIDR(cidrBlock)
			if util.CheckProtocol(cidrBlock) == kubeovnv1.ProtocolIPv4 {
				ipStr := strings.Split(cniResponse.IpAddress, ",")[0]
				gwStr := strings.Split(cniResponse.Gateway, ",")[0]

				ip, route := assignV4Address(ipStr, gwStr, netMask)
				result.IPs = append(result.IPs, &ip)
				result.Routes = append(result.Routes, &route)
			} else if util.CheckProtocol(cidrBlock) == kubeovnv1.ProtocolIPv6 {
				ipStr := strings.Split(cniResponse.IpAddress, ",")[1]
				gwStr := strings.Split(cniResponse.Gateway, ",")[1]

				ip, route := assignV6Address(ipStr, gwStr, netMask)
				result.IPs = append(result.IPs, &ip)
				result.Routes = append(result.Routes, &route)
			}
		}
	}

	return result
}

func cmdDel(args *skel.CmdArgs) error {
	netConf, _, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}

	client := request.NewCniServerClient(netConf.ServerSocket)
	podName, err := parseValueFromArgs("K8S_POD_NAME", args.Args)
	if err != nil {
		return err
	}
	podNamespace, err := parseValueFromArgs("K8S_POD_NAMESPACE", args.Args)
	if err != nil {
		return err
	}

	return client.Del(request.CniRequest{
		CniType:      netConf.Type,
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns,
		IfName:       args.IfName,
		Provider:     netConf.Provider,
		DeviceID:     netConf.DeviceID,
	})
}

type ipamConf struct {
	ServerSocket string `json:"server_socket"`
	Provider     string `json:"provider"`
}

type netConf struct {
	types.NetConf
	ServerSocket string    `json:"server_socket"`
	Provider     string    `json:"provider"`
	IPAM         *ipamConf `json:"ipam"`
	// PciAddrs in case of using sriov
	DeviceID string `json:"deviceID"`
}

func loadNetConf(bytes []byte) (*netConf, string, error) {
	n := &netConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}

	if n.Type != util.CniTypeName && n.IPAM != nil {
		n.Provider = n.IPAM.Provider
		n.ServerSocket = n.IPAM.ServerSocket
	}

	if n.ServerSocket == "" {
		return nil, "", fmt.Errorf("server_socket is required in cni.conf, %+v", n)
	}

	if n.Provider == "" {
		n.Provider = util.OvnProvider
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
			value := strings.TrimPrefix(arg, fmt.Sprintf("%s=", key))
			if len(value) > 0 {
				return value, nil
			}
		}
	}
	return "", fmt.Errorf("%s is required in CNI_ARGS", key)
}

func assignV4Address(ipAddress, gateway string, mask *net.IPNet) (current.IPConfig, types.Route) {
	ip := current.IPConfig{
		Version: "4",
		Address: net.IPNet{IP: net.ParseIP(ipAddress).To4(), Mask: mask.Mask},
		Gateway: net.ParseIP(gateway).To4(),
	}

	route := types.Route{
		Dst: net.IPNet{IP: net.ParseIP("0.0.0.0").To4(), Mask: net.CIDRMask(0, 32)},
		GW:  net.ParseIP(gateway).To4(),
	}

	return ip, route
}

func assignV6Address(ipAddress, gateway string, mask *net.IPNet) (current.IPConfig, types.Route) {
	ip := current.IPConfig{
		Version: "6",
		Address: net.IPNet{IP: net.ParseIP(ipAddress).To16(), Mask: mask.Mask},
		Gateway: net.ParseIP(gateway).To16(),
	}

	route := types.Route{
		Dst: net.IPNet{IP: net.ParseIP("::").To16(), Mask: net.CIDRMask(0, 128)},
		GW:  net.ParseIP(gateway).To16(),
	}

	return ip, route
}
