package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/alauda/kube-ovn/pkg/util"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
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
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns,
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
		ip := current.IPConfig{
			Version: "4",
			Address: net.IPNet{IP: net.ParseIP(cniResponse.IpAddress).To4(), Mask: mask.Mask},
			Gateway: net.ParseIP(cniResponse.Gateway).To4(),
		}
		result.IPs = []*current.IPConfig{&ip}

		route := types.Route{
			Dst: net.IPNet{IP: net.ParseIP("0.0.0.0").To4(), Mask: net.CIDRMask(0, 32)},
			GW:  net.ParseIP(cniResponse.Gateway).To4(),
		}
		result.Routes = []*types.Route{&route}
	case kubeovnv1.ProtocolIPv6:
		ip := current.IPConfig{
			Version: "6",
			Address: net.IPNet{IP: net.ParseIP(cniResponse.IpAddress).To16(), Mask: mask.Mask},
			Gateway: net.ParseIP(cniResponse.Gateway).To16(),
		}
		result.IPs = []*current.IPConfig{&ip}

		route := types.Route{
			Dst: net.IPNet{IP: net.ParseIP("::").To16(), Mask: net.CIDRMask(0, 128)},
			GW:  net.ParseIP(cniResponse.Gateway).To16(),
		}
		result.Routes = []*types.Route{&route}
	case kubeovnv1.ProtocolDual:
		cidrBlocks := strings.Split(cniResponse.CIDR, ",")
		_, v4Mask, _ := net.ParseCIDR(cidrBlocks[0])
		// _, v6Mask, _ := net.ParseCIDR(cidrBlocks[1])
		ips := strings.Split(cniResponse.IpAddress, ",")
		gws := strings.Split(cniResponse.Gateway, ",")

		ip := current.IPConfig{
			Version: "4",
			Address: net.IPNet{IP: net.ParseIP(ips[0]).To4(), Mask: v4Mask.Mask},
			Gateway: net.ParseIP(gws[0]).To4(),
		}
		result.IPs = append(result.IPs, &ip)

		route := types.Route{
			Dst: net.IPNet{IP: net.ParseIP("0.0.0.0").To4(), Mask: net.CIDRMask(0, 32)},
			GW:  net.ParseIP(gws[0]).To4(),
		}
		result.Routes = append(result.Routes, &route)

		// // This should be added when K8S support dualstack
		// ip = current.IPConfig{
		// 	Version: "6",
		// 	Address: net.IPNet{IP: net.ParseIP(ips[1]).To16(), Mask: v6Mask.Mask},
		// 	Gateway: net.ParseIP(gws[1]).To16(),
		// }
		// result.IPs = append(result.IPs, &ip)

		// route = types.Route{
		// 	Dst: net.IPNet{IP: net.ParseIP("::").To16(), Mask: net.CIDRMask(0, 128)},
		// 	GW:  net.ParseIP(gws[1]).To16(),
		// }
		// result.Routes = append(result.Routes, &route)
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
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns,
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

	if n.IPAM != nil {
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
			podName := strings.TrimPrefix(arg, fmt.Sprintf("%s=", key))
			if len(podName) > 0 {
				return podName, nil
			}
		}
	}
	return "", fmt.Errorf("%s is required in CNI_ARGS", key)
}
