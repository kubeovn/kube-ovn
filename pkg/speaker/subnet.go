//nolint:staticcheck
package speaker

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"strconv"
	"strings"

	bgpapi "github.com/osrg/gobgp/v3/api"
	bgpapiutil "github.com/osrg/gobgp/v3/pkg/apiutil"
	"github.com/osrg/gobgp/v3/pkg/packet/bgp"
	"github.com/vishvananda/netlink"
	"google.golang.org/protobuf/types/known/anypb"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func isPodAlive(p *v1.Pod) bool {
	if p.Status.Phase == v1.PodSucceeded && p.Spec.RestartPolicy != v1.RestartPolicyAlways {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Spec.RestartPolicy == v1.RestartPolicyNever {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Status.Reason == "Evicted" {
		return false
	}
	return true
}

func isClusterIPService(svc *v1.Service) bool {
	return svc.Spec.Type == v1.ServiceTypeClusterIP &&
		svc.Spec.ClusterIP != v1.ClusterIPNone &&
		len(svc.Spec.ClusterIP) != 0
}

// TODO: ipv4 only, need ipv6/dual-stack support later
func (c *Controller) syncSubnetRoutes() {
	bgpExpected, bgpExists := []string{}, []string{}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets, %v", err)
		return
	}
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return
	}

	if c.config.AnnounceClusterIP {
		services, err := c.servicesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list services, %v", err)
			return
		}
		for _, svc := range services {
			if svc.Annotations != nil && svc.Annotations[util.BgpAnnotation] == "true" && isClusterIPService(svc) {
				bgpExpected = append(bgpExpected, fmt.Sprintf("%s/32", svc.Spec.ClusterIP))
			}
		}
	}

	for _, subnet := range subnets {
		if subnet.Status.IsReady() && subnet.Annotations != nil && subnet.Annotations[util.BgpAnnotation] == "true" {
			bgpExpected = append(bgpExpected, subnet.Spec.CIDRBlock)
		}
	}

	for _, pod := range pods {
		if isPodAlive(pod) && !pod.Spec.HostNetwork && pod.Annotations[util.BgpAnnotation] == "true" && pod.Status.PodIP != "" {
			bgpExpected = append(bgpExpected, fmt.Sprintf("%s/32", pod.Status.PodIP))
		}
	}

	klog.V(5).Infof("expected routes %v", bgpExpected)
	listPathRequest := &bgpapi.ListPathRequest{
		TableType: bgpapi.TableType_GLOBAL,
		Family:    &bgpapi.Family{Afi: bgpapi.Family_AFI_IP, Safi: bgpapi.Family_SAFI_UNICAST},
	}
	fn := func(d *bgpapi.Destination) {
		for _, path := range d.Paths {
			attrInterfaces, _ := bgpapiutil.UnmarshalPathAttributes(path.Pattrs)
			nextHop := getNextHopFromPathAttributes(attrInterfaces)
			klog.V(5).Infof("nexthop is %s, routerID is %s", nextHop.String(), c.config.RouterId)
			route, _ := netlink.RouteGet(nextHop)
			if len(route) == 1 && route[0].Type == unix.RTN_LOCAL || nextHop.String() == c.config.RouterId {
				bgpExists = append(bgpExists, d.Prefix)
				return
			}
		}
	}
	if err := c.config.BgpServer.ListPath(context.Background(), listPathRequest, fn); err != nil {
		klog.Errorf("failed to list exist route, %v", err)
		return
	}

	klog.V(5).Infof("exists routes %v", bgpExists)
	toAdd, toDel := routeDiff(bgpExpected, bgpExists)
	klog.V(5).Infof("toAdd routes %v", toAdd)
	for _, route := range toAdd {
		if err := c.addRoute(route); err != nil {
			klog.Error(err)
		}
	}
	klog.V(5).Infof("toDel routes %v", toDel)
	for _, route := range toDel {
		if err := c.delRoute(route); err != nil {
			klog.Error(err)
		}
	}
}

func routeDiff(expected, exists []string) (toAdd []string, toDel []string) {
	expectedMap, existsMap := map[string]bool{}, map[string]bool{}
	for _, e := range expected {
		expectedMap[e] = true
	}
	for _, e := range exists {
		existsMap[e] = true
	}

	for e := range expectedMap {
		if !existsMap[e] {
			toAdd = append(toAdd, e)
		}
	}

	for e := range existsMap {
		if !expectedMap[e] {
			toDel = append(toDel, e)
		}
	}
	return toAdd, toDel
}

func parseRoute(route string) (string, uint32, error) {
	var prefixLen uint32 = 32
	prefix := route
	if strings.Contains(route, "/") {
		prefix = strings.Split(route, "/")[0]
		strLen := strings.Split(route, "/")[1]
		intLen, err := strconv.Atoi(strLen)
		if err != nil {
			return "", 0, err
		}
		prefixLen = uint32(intLen)
	}
	return prefix, prefixLen, nil
}

func (c *Controller) addRoute(route string) error {
	nlri, attrs, err := c.getNlriAndAttrs(route)
	if err != nil {
		return err
	}
	_, err = c.config.BgpServer.AddPath(context.Background(), &bgpapi.AddPathRequest{
		Path: &bgpapi.Path{
			Family: &bgpapi.Family{Afi: bgpapi.Family_AFI_IP, Safi: bgpapi.Family_SAFI_UNICAST},
			Nlri:   nlri,
			Pattrs: attrs,
		},
	})
	if err != nil {
		klog.Errorf("add path failed, %v", err)
		return err
	}
	return nil
}

func (c *Controller) getNlriAndAttrs(route string) (*anypb.Any, []*anypb.Any, error) {
	prefix, prefixLen, err := parseRoute(route)
	if err != nil {
		return nil, nil, err
	}
	nlri, _ := anypb.New(&bgpapi.IPAddressPrefix{
		Prefix:    prefix,
		PrefixLen: prefixLen,
	})
	a1, _ := anypb.New(&bgpapi.OriginAttribute{
		Origin: 0,
	})
	a2, _ := anypb.New(&bgpapi.NextHopAttribute{
		NextHop: getNextHopAttribute(c.config.NeighborAddress, c.config.RouterId),
	})
	attrs := []*anypb.Any{a1, a2}
	return nlri, attrs, err
}

func (c *Controller) delRoute(route string) error {
	nlri, attrs, err := c.getNlriAndAttrs(route)
	if err != nil {
		return err
	}
	err = c.config.BgpServer.DeletePath(context.Background(), &bgpapi.DeletePathRequest{
		Path: &bgpapi.Path{
			Family: &bgpapi.Family{Afi: bgpapi.Family_AFI_IP, Safi: bgpapi.Family_SAFI_UNICAST},
			Nlri:   nlri,
			Pattrs: attrs,
		},
	})
	if err != nil {
		klog.Errorf("del path failed, %v", err)
		return err
	}
	return nil
}

func getNextHopFromPathAttributes(attrs []bgp.PathAttributeInterface) net.IP {
	for _, attr := range attrs {
		switch a := attr.(type) {
		case *bgp.PathAttributeNextHop:
			return a.Value
		case *bgp.PathAttributeMpReachNLRI:
			return a.Nexthop
		}
	}
	return nil
}
func getNextHopAttribute(NeighborAddress string, RouteId string) string {
	nextHop := RouteId
	routes, err := netlink.RouteGet(net.ParseIP(NeighborAddress))
	if err == nil && len(routes) == 1 && routes[0].Src != nil {
		nextHop = routes[0].Src.String()
	}
	return nextHop
}
