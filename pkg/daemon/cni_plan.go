package daemon

import (
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newCNIPlan(req request.CniRequest, nicType, shortSharedDir, mac, ip, ipAddr, cidr, gateway string, mtu int, isDefaultRoute, vmMigration, routedSubnet, ipamOnly bool, gatewayCheckMode int, u2oIP, oldPodName, encapIP, localnetSubnet string, appendIfName bool, routes []request.Route, ingress, egress, ingressBurst, egressBurst, latency, limit, loss, jitter string) *request.CNIPlan {
	return &request.CNIPlan{
		CniType:                    req.CniType,
		PodName:                    req.PodName,
		PodNamespace:               req.PodNamespace,
		ContainerID:                req.ContainerID,
		NetNs:                      req.NetNs,
		IfName:                     req.IfName,
		Provider:                   req.Provider,
		Routes:                     routes,
		DNS:                        req.DNS,
		VfDriver:                   req.VfDriver,
		DeviceID:                   req.DeviceID,
		VhostUserSocketVolumeName:  req.VhostUserSocketVolumeName,
		VhostUserSocketName:        req.VhostUserSocketName,
		VhostUserSocketConsumption: req.VhostUserSocketConsumption,
		ShortSharedDir:             shortSharedDir,
		MacAddress:                 mac,
		IP:                         ip,
		IPAddr:                     ipAddr,
		CIDR:                       cidr,
		Gateway:                    gateway,
		MTU:                        mtu,
		IsDefaultRoute:             isDefaultRoute,
		VMMigration:                vmMigration,
		RoutedSubnet:               routedSubnet,
		IPAMOnly:                   ipamOnly,
		GatewayCheckMode:           gatewayCheckMode,
		U2OInterconnectionIP:       u2oIP,
		OldPodName:                 oldPodName,
		EncapIP:                    encapIP,
		LocalnetSubnet:             localnetSubnet,
		AppendIfName:               appendIfName,
		Ingress:                    ingress,
		Egress:                     egress,
		IngressBurst:               ingressBurst,
		EgressBurst:                egressBurst,
		Latency:                    latency,
		Limit:                      limit,
		Loss:                       loss,
		Jitter:                     jitter,
		NicType:                    nicType,
	}
}

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
