package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/kubeovn/kube-ovn/cmd/acl_sample"
	cniexec "github.com/kubeovn/kube-ovn/pkg/cni"
	"github.com/kubeovn/kube-ovn/pkg/netconf"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func main() {
	if filepath.Base(os.Args[0]) == acl_sample.CommandName {
		acl_sample.CmdMain()
		return
	}

	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()

	funcs := skel.CNIFuncs{
		Add: cmdAdd,
		Del: cmdDel,
	}
	about := "CNI kube-ovn plugin " + versions.VERSION
	skel.PluginMainFuncs(funcs, version.All, about)
}

func cmdAdd(args *skel.CmdArgs) error {
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
	applyDefaultProvider(netConf, args)

	if err = prepareExecutionTools(); err != nil {
		return err
	}
	if err = sysctlEnableIPv6(args.Netns); err != nil {
		return err
	}

	client := request.NewCniServerClient(netConf.ServerSocket)
	response, err := client.Add(request.CniRequest{
		CniType:                    netConf.Type,
		PodName:                    podName,
		PodNamespace:               podNamespace,
		ContainerID:                args.ContainerID,
		NetNs:                      args.Netns,
		IfName:                     args.IfName,
		Provider:                   netConf.Provider,
		Routes:                     netConf.Routes,
		DNS:                        netConf.DNS,
		DeviceID:                   netConf.DeviceID,
		VfDriver:                   netConf.VfDriver,
		VhostUserSocketVolumeName:  netConf.VhostUserSocketVolumeName,
		VhostUserSocketName:        netConf.VhostUserSocketName,
		VhostUserSocketConsumption: netConf.VhostUserSocketConsumption,
		PrepareOnly:                true,
	})
	if err != nil {
		return types.NewError(types.ErrTryAgainLater, "RPC failed", err.Error())
	}

	if response.Plan == nil {
		return types.NewError(types.ErrTryAgainLater, "RPC failed", "daemon returned no CNI plan")
	}
	executor := cniexec.NewExecutor(cniexec.ExecutorConfig{EnableArpDetectIPConflict: response.Plan.EnableArpDetectIPConflict})
	execution, err := executor.Add(response.Plan)
	if err != nil {
		return types.NewError(types.ErrTryAgainLater, "CNI execution failed", err.Error())
	}
	if err = client.Commit(request.CniRequest{Plan: response.Plan, Execution: execution}); err != nil {
		return types.NewError(types.ErrTryAgainLater, "CNI commit failed", err.Error())
	}

	result, err := cniexec.ResultFromPlan(response.Plan, execution)
	if err != nil {
		return err
	}
	return types.PrintResult(&result, cniVersion)
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
	applyDefaultProvider(netConf, args)
	if err = prepareExecutionTools(); err != nil {
		return err
	}
	response, err := client.PrepareDelete(request.CniRequest{
		CniType:                    netConf.Type,
		PodName:                    podName,
		PodNamespace:               podNamespace,
		ContainerID:                args.ContainerID,
		NetNs:                      args.Netns,
		IfName:                     args.IfName,
		Provider:                   netConf.Provider,
		DeviceID:                   netConf.DeviceID,
		VhostUserSocketVolumeName:  netConf.VhostUserSocketVolumeName,
		VhostUserSocketConsumption: netConf.VhostUserSocketConsumption,
		PrepareOnly:                true,
	})
	if err != nil {
		return types.NewError(types.ErrTryAgainLater, "RPC failed", err.Error())
	}
	if response.Plan == nil {
		return types.NewError(types.ErrTryAgainLater, "RPC failed", "daemon returned no CNI delete plan")
	}
	executor := cniexec.NewExecutor(cniexec.ExecutorConfig{EnableArpDetectIPConflict: response.Plan.EnableArpDetectIPConflict})
	if err = executor.Delete(response.Plan); err != nil {
		return types.NewError(types.ErrTryAgainLater, "CNI deletion failed", err.Error())
	}
	if err = client.Commit(request.CniRequest{Plan: response.Plan}); err != nil {
		return types.NewError(types.ErrTryAgainLater, "CNI commit failed", err.Error())
	}
	return nil
}

func applyDefaultProvider(netConf *netconf.NetConf, args *skel.CmdArgs) {
	if netConf.Provider == "" && netConf.Type == util.CniTypeName && args.IfName == "eth0" {
		netConf.Provider = util.OvnProvider
	}
}

func loadNetConf(bytes []byte) (*netconf.NetConf, string, error) {
	n := &netconf.NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", types.NewError(types.ErrDecodingFailure, "failed to load netconf", err.Error())
	}

	if n.Type != util.CniTypeName && n.IPAM != nil {
		n.Provider = n.IPAM.Provider
		n.ServerSocket = n.IPAM.ServerSocket
		n.Routes = n.IPAM.Routes
	}

	if n.ServerSocket == "" {
		return nil, "", types.NewError(types.ErrInvalidNetworkConfig, "Invalid Configuration", fmt.Sprintf("server_socket is required in cni.conf, %+v", n))
	}

	if n.Provider == "" {
		n.Provider = util.OvnProvider
	}

	n.PostLoad()
	return n, n.CNIVersion, nil
}

func parseValueFromArgs(key, argString string) (string, error) {
	if argString == "" {
		return "", types.NewError(types.ErrInvalidNetworkConfig, "Invalid Configuration", "CNI_ARGS is required")
	}
	for arg := range strings.SplitSeq(argString, ";") {
		if value, found := strings.CutPrefix(arg, key+"="); found && len(value) != 0 {
			return value, nil
		}
	}
	return "", types.NewError(types.ErrInvalidNetworkConfig, "Invalid Configuration", key+" is required in CNI_ARGS")
}
