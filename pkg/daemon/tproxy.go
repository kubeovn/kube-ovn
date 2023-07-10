package daemon

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	goTProxy "github.com/kubeovn/kube-ovn/pkg/tproxy"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	tcpListener net.Listener

	customVPCPodIPToNs         sync.Map
	customVPCPodTCPProbeIPPort sync.Map
)

func (c *Controller) StartTProxyForwarding(stopCh <-chan struct{}) {
	var err error
	addr := GetDefaultListenPort()

	protocol := "tcp"
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		addr = addr[1 : len(addr)-1]
		protocol = "tcp6"
	}

	tcpListener, err = goTProxy.ListenTCP(protocol, &net.TCPAddr{IP: net.ParseIP(addr), Port: util.TProxyListenPort})
	if err != nil {
		klog.Fatalf("Encountered error while binding listener: %s", err)
		return
	}

	defer tcpListener.Close()
	go listenTCP()

	<-stopCh
}

func (c *Controller) StartTProxyTCPPortProbe() {

	for {
		var probePorts []string
		pods, err := c.podsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list pods: %v", err)
			return
		}

		if len(pods) == 0 {
			return
		}

		for _, pod := range pods {
			podName := pod.Name
			subnetName, ok := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider)]
			if !ok {
				continue
			}

			podIP, ok := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, util.OvnProvider)]
			if !ok {
				continue
			}

			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Errorf("failed to get subnet '%s', err: %v", subnetName, err)
				continue
			}

			if subnet.Spec.Vpc == c.config.ClusterRouter {
				continue
			}

			iface := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
			nsName, err := ovs.GetInterfacePodNs(iface)
			if err != nil || nsName == "" {
				continue
			}

			customVPCPodIPToNs.Store(podIP, nsName)

			for _, container := range pod.Spec.Containers {
				if container.ReadinessProbe != nil {
					if tcpSocket := container.ReadinessProbe.TCPSocket; tcpSocket != nil {
						if port := tcpSocket.Port.String(); port != "" {
							probePorts = append(probePorts, port)
						}
					}
				}

				if container.LivenessProbe != nil {
					if tcpSocket := container.LivenessProbe.TCPSocket; tcpSocket != nil {
						if port := tcpSocket.Port.String(); port != "" {
							probePorts = append(probePorts, port)
						}
					}
				}
			}

			for _, port := range probePorts {
				probePortInNs(podIP, port, false, nil)
			}
		}
	}
}

func (c *Controller) runTProxyConfigWorker() {
	protocols := getProtocols(c.protocol)
	for _, protocol := range protocols {
		c.reconcileTProxyRoutes(protocol)
	}
}

func (c *Controller) reconcileTProxyRoutes(protocol string) {
	family := getFamily(protocol)
	if err := addRuleIfNotExist(family, TProxyPostroutingMark, TProxyPostroutingMask, util.TProxyRouteTable); err != nil {
		return
	}

	if err := addRuleIfNotExist(family, TProxyPreroutingMark, TProxyPreroutingMask, util.TProxyRouteTable); err != nil {
		return
	}

	dst := getDefaultRouteDst(protocol)
	if err := addRouteIfNotExist(family, util.TProxyRouteTable, &dst); err != nil {
		return
	}
}

func (c *Controller) cleanTProxyConfig() {
	protocols := getProtocols(c.protocol)
	for _, protocol := range protocols {
		c.cleanTProxyRoutes(protocol)
		c.cleanTProxyIPTableRules(protocol)
	}
}

func (c *Controller) cleanTProxyRoutes(protocol string) {
	family := getFamily(protocol)
	if err := deleteRuleIfExists(family, TProxyPostroutingMark); err != nil {
		klog.Errorf("delete tproxy route rule mark %v failed err: %v ", TProxyPostroutingMark, err)
	}

	if err := deleteRuleIfExists(family, TProxyPreroutingMark); err != nil {
		klog.Errorf("delete tproxy route rule mark %v failed err: %v ", TProxyPreroutingMark, err)
	}

	dst := getDefaultRouteDst(protocol)
	if err := delRouteIfExist(family, util.TProxyRouteTable, &dst); err != nil {
		klog.Errorf("delete tproxy route rule mark %v failed err: %v ", TProxyPreroutingMark, err)
	}
}

func getFamily(protocol string) int {
	family := netlink.FAMILY_ALL
	if protocol == kubeovnv1.ProtocolIPv4 {
		family = netlink.FAMILY_V4
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		family = netlink.FAMILY_V6
	}
	return family
}

func addRuleIfNotExist(family, mark, mask, table int) error {
	curRules, err := netlink.RuleListFiltered(family, &netlink.Rule{Mark: mark}, netlink.RT_FILTER_MARK)
	if err != nil {
		return fmt.Errorf("list rules with mark %x failed err: %v", mark, err)
	}

	if len(curRules) != 0 {
		return nil
	}

	rule := netlink.NewRule()
	rule.Mark = mark
	rule.Mask = mask
	rule.Table = table
	rule.Family = family

	if err = netlink.RuleAdd(rule); err != nil && !errors.Is(err, syscall.EEXIST) {
		klog.Errorf("add rule %v failed with err %v ", rule, err)
		return err
	}

	return nil
}

func deleteRuleIfExists(family, mark int) error {
	curRules, err := netlink.RuleListFiltered(family, &netlink.Rule{Mark: mark}, netlink.RT_FILTER_MARK)
	if err != nil {
		return fmt.Errorf("list rules with mark %x failed err: %v", mark, err)
	}

	if len(curRules) != 0 {
		for _, r := range curRules {
			if err := netlink.RuleDel(&r); err != nil && !errors.Is(err, syscall.ENOENT) {
				return fmt.Errorf("delete rule %v failed with err: %v", r, err)
			}
		}
	}
	return nil
}

func addRouteIfNotExist(family, table int, dst *net.IPNet) error {
	curRoutes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: table, Dst: dst}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
	if err != nil {
		return fmt.Errorf("list routes with table %d failed with err: %v", table, err)
	}

	if len(curRoutes) != 0 {
		return nil
	}

	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("can't find device lo")
	}

	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Table:     table,
		Scope:     unix.RT_SCOPE_HOST,
		Type:      unix.RTN_LOCAL,
	}

	if err = netlink.RouteReplace(&route); err != nil && !errors.Is(err, syscall.EEXIST) {
		klog.Errorf("add route %v failed with err %v ", route, err)
		return err
	}

	return nil
}

func delRouteIfExist(family, table int, dst *net.IPNet) error {
	curRoutes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: table}, netlink.RT_FILTER_TABLE)
	if err != nil {
		klog.Error("list routes with table %d failed with err: %v", table, err)
		return err
	}

	if len(curRoutes) == 0 {
		return nil
	}

	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("can't find device lo")
	}

	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Table:     table,
		Scope:     unix.RT_SCOPE_HOST,
		Type:      unix.RTN_LOCAL,
	}

	if err = netlink.RouteDel(&route); err != nil && !errors.Is(err, syscall.ENOENT) {
		klog.Errorf("del route %v failed with err %v ", route, err)
		return err
	}

	return nil
}

func listenTCP() {
	for {
		conn, err := tcpListener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok {
				klog.Errorf("Temporary error while accepting connection: %s", netErr)
			}
			klog.Fatalf("Unrecoverable error while accepting connection: %s", err)
			return
		}

		go handleRedirectFlow(conn)
	}
}

func handleRedirectFlow(conn net.Conn) {

	klog.V(5).Info("Accepting TCP connection from %v with destination of %v", conn.RemoteAddr().String(), conn.LocalAddr().String())
	defer conn.Close()
	podIPPort := conn.LocalAddr().String()
	var podIP, probePort string
	if strings.HasPrefix(podIPPort, "[") {
		podIP = podIPPort[1:strings.Index(podIPPort, "]")]
		probePort = podIPPort[strings.Index(podIPPort, "]:")+2:]
	} else {
		ret := strings.Split(podIPPort, ":")
		podIP = ret[0]
		probePort = ret[1]
	}

	probePortInNs(podIP, probePort, true, conn)
}

func probePortInNs(podIP, probePort string, transferHTTPMessage bool, conn net.Conn) {
	podNs, ok := customVPCPodIPToNs.Load(podIP)
	if !ok {
		return
	}

	iprobePort, err := strconv.Atoi(probePort)
	if err != nil {
		return
	}

	podNS, err := ns.GetNS(podNs.(string))
	if err != nil {
		klog.Errorf("Can't get ns %s with err: %v", podNs, err)
		return
	}

	protocol := util.CheckProtocol(podIP)

	_ = ns.WithNetNSPath(podNS.Path(), func(_ ns.NetNS) error {
		// Packet 's src and dst IP are both PodIP in netns
		var localpodTcpAddr, remotepodTcpAddr net.TCPAddr
		if protocol == kubeovnv1.ProtocolIPv6 {
			link, err := netlink.LinkByName("lo")
			if err != nil {
				klog.Error("can't find device lo")
				return err
			}

			ifIndex := fmt.Sprintf("%d", link.Attrs().Index)
			localpodTcpAddr = net.TCPAddr{IP: net.ParseIP(podIP), Zone: ifIndex}
			remotepodTcpAddr = net.TCPAddr{IP: net.ParseIP(podIP), Port: iprobePort, Zone: ifIndex}
		} else {
			localpodTcpAddr = net.TCPAddr{IP: net.ParseIP(podIP)}
			remotepodTcpAddr = net.TCPAddr{IP: net.ParseIP(podIP), Port: iprobePort}
		}

		remoteConn, err := goTProxy.DialTCP(&localpodTcpAddr, &remotepodTcpAddr, transferHTTPMessage)
		if err != nil {
			customVPCPodTCPProbeIPPort.Store(getIPPortString(podIP, probePort), false)
			return nil
		} else {
			customVPCPodTCPProbeIPPort.Store(getIPPortString(podIP, probePort), true)
		}

		defer remoteConn.Close()

		if transferHTTPMessage {
			var streamWait sync.WaitGroup
			streamWait.Add(2)

			streamConn := func(dst io.Writer, src io.Reader) {
				if _, err := io.Copy(dst, src); err != nil {
					klog.Errorf("copy stream from dst %v to src %v failed err: %v ", dst, src, err)
				}

				streamWait.Done()
			}

			go streamConn(remoteConn, conn)
			go streamConn(conn, remoteConn)

			streamWait.Wait()
			return nil
		}
		return nil
	})
}

func getIPPortString(podIP, port string) string {
	return fmt.Sprintf("%s|%s", podIP, port)
}

func getProtocols(protocol string) []string {
	var protocols []string
	if protocol == kubeovnv1.ProtocolDual {
		protocols = append(protocols, kubeovnv1.ProtocolIPv4)
		protocols = append(protocols, kubeovnv1.ProtocolIPv6)
	} else {
		protocols = append(protocols, protocol)
	}
	return protocols
}

func getDefaultRouteDst(protocol string) net.IPNet {
	var dst net.IPNet
	if protocol == kubeovnv1.ProtocolIPv4 {
		dst = net.IPNet{
			IP:   net.IPv4zero,
			Mask: net.CIDRMask(0, 0),
		}
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		dst = net.IPNet{
			IP:   net.IPv6zero,
			Mask: net.CIDRMask(0, 0),
		}
	}
	return dst
}
