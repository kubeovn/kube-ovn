package cni

import (
	"net"

	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func cniResponseForPlan(plan *request.CNIPlan) request.CniResponse {
	response := request.CniResponse{
		MacAddress: plan.MacAddress,
		PodNicName: plan.IfName,
		Routes:     plan.Routes,
		Mtu:        plan.MTU,
		DNS:        plan.DNS,
		Plan:       plan,
	}
	v4IP, v6IP := util.SplitStringIP(plan.IP)
	v4CIDR, v6CIDR := util.SplitStringIP(plan.CIDR)
	if plan.RoutedSubnet && plan.IPAddr != "" {
		v4Addr, v6Addr := util.SplitStringIP(plan.IPAddr)
		if v4Addr != "" {
			v4CIDR = v4Addr
		}
		if v6Addr != "" {
			v6CIDR = v6Addr
		}
	}
	v4GW, v6GW := util.SplitStringIP(plan.Gateway)
	if v4IP != "" {
		ip := request.IPConfig{Protocol: kubeovnv1.ProtocolIPv4, IP: v4IP, CIDR: v4CIDR}
		if plan.IsDefaultRoute {
			ip.Gateway = v4GW
		}
		response.IPs = append(response.IPs, ip)
	}
	if v6IP != "" {
		ip := request.IPConfig{Protocol: kubeovnv1.ProtocolIPv6, IP: v6IP, CIDR: v6CIDR}
		if plan.IsDefaultRoute {
			ip.Gateway = v6GW
		}
		response.IPs = append(response.IPs, ip)
	}
	return response
}

// ResultFromPlan converts the observed execution result into the CNI result.
func ResultFromPlan(plan *request.CNIPlan, execution *request.CNIExecutionResult) (current.Result, error) {
	response := cniResponseForPlan(plan)
	if execution != nil && len(execution.Routes) > 0 {
		response.Routes = execution.Routes
	}
	result := current.Result{
		CNIVersion: current.ImplementedSpecVersion,
		DNS:        response.DNS,
		Routes:     parseCNIPlanRoutes(response.Routes),
	}
	for _, ipConfig := range response.IPs {
		ip, route, err := cniIPConfig(ipConfig)
		if err != nil {
			return result, err
		}
		result.IPs = append(result.IPs, ip)
		if len(result.Routes) == 0 && route != nil {
			result.Routes = append(result.Routes, route)
		}
	}
	result.Interfaces = []*current.Interface{{Name: response.PodNicName, Mac: response.MacAddress, Mtu: response.Mtu, Sandbox: plan.NetNs}}
	return result, nil
}

func parseCNIPlanRoutes(routes []request.Route) []*types.Route {
	parsed := make([]*types.Route, len(routes))
	for index, route := range routes {
		destination := route.Destination
		if destination == "" {
			destination = "0.0.0.0/0"
			if util.CheckProtocol(route.Gateway) == kubeovnv1.ProtocolIPv6 {
				destination = "::/0"
			}
		}
		_, cidr, err := net.ParseCIDR(destination)
		if err != nil {
			continue
		}
		parsed[index] = &types.Route{Dst: *cidr, GW: net.ParseIP(route.Gateway)}
	}
	return parsed
}

func cniIPConfig(config request.IPConfig) (*current.IPConfig, *types.Route, error) {
	_, cidr, err := net.ParseCIDR(config.CIDR)
	if err != nil {
		return nil, nil, err
	}
	ip := net.ParseIP(config.IP)
	if ip == nil {
		return nil, nil, net.InvalidAddrError(config.IP)
	}
	gateway := net.ParseIP(config.Gateway)
	result := &current.IPConfig{Address: net.IPNet{IP: ip, Mask: cidr.Mask}, Gateway: gateway, Interface: new(0)}
	if gateway == nil {
		return result, nil, nil
	}
	defaultDestination := net.IPNet{IP: net.IPv4zero.To4(), Mask: net.CIDRMask(0, 32)}
	if config.Protocol == kubeovnv1.ProtocolIPv6 {
		defaultDestination = net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	}
	return result, &types.Route{Dst: defaultDestination, GW: gateway}, nil
}
