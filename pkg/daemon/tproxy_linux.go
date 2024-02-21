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
	"github.com/scylladb/go-set/strset"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
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

func (c *Controller) StartTProxyForwarding() {
	var err error
	addr := util.GetDefaultListenAddr()

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

	defer func() {
		if err := tcpListener.Close(); err != nil {
			klog.Errorf("Error tcpListener Close err: %v", err)
		}
	}()

	for {
		conn, err := tcpListener.Accept()
		if err != nil {
			klog.Fatalf("Unrecoverable error while accepting connection: %s", err)
			return
		}
		go handleRedirectFlow(conn)
	}
}

func (c *Controller) StartTProxyTCPPortProbe() {
	probePorts := strset.New()

	pods, err := c.getTProxyConditionPod(false)
	if err != nil {
		return
	}

	for _, pod := range pods {
		iface := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
		nsName, err := ovs.GetInterfacePodNs(iface)
		if err != nil || nsName == "" {
			klog.Infof("iface %s's namespace not found", iface)
			continue
		}

		for _, podIP := range pod.Status.PodIPs {
			customVPCPodIPToNs.Store(podIP.IP, nsName)
			for _, container := range pod.Spec.Containers {
				if container.ReadinessProbe != nil {
					if tcpSocket := container.ReadinessProbe.TCPSocket; tcpSocket != nil {
						if port := tcpSocket.Port.String(); port != "" {
							probePorts.Add(port)
						}
					}
				}

				if container.LivenessProbe != nil {
					if tcpSocket := container.LivenessProbe.TCPSocket; tcpSocket != nil {
						if port := tcpSocket.Port.String(); port != "" {
							probePorts.Add(port)
						}
					}
				}
			}

			probePortsList := probePorts.List()
			for _, port := range probePortsList {
				probePortInNs(podIP.IP, port, true, nil)
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
	family, err := util.ProtocolToFamily(protocol)
	if err != nil {
		klog.Errorf("get Protocol %s family failed", protocol)
		return
	}

	if err := addRuleIfNotExist(family, TProxyOutputMark, TProxyOutputMask, util.TProxyRouteTable); err != nil {
		klog.Errorf("add output rule failed: %v", err)
		return
	}

	if err := addRuleIfNotExist(family, TProxyPreroutingMark, TProxyPreroutingMask, util.TProxyRouteTable); err != nil {
		klog.Errorf("add prerouting rule failed: %v", err)
		return
	}

	dst := GetDefaultRouteDst(protocol)
	if err := addRouteIfNotExist(family, util.TProxyRouteTable, &dst); err != nil {
		klog.Errorf("add tproxy route failed: %v", err)
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
	family, err := util.ProtocolToFamily(protocol)
	if err != nil {
		klog.Errorf("get Protocol %s family failed", protocol)
		return
	}

	if err := deleteRuleIfExists(family, TProxyOutputMark); err != nil {
		klog.Errorf("delete tproxy route rule mark %v failed err: %v", TProxyOutputMark, err)
	}

	if err := deleteRuleIfExists(family, TProxyPreroutingMark); err != nil {
		klog.Errorf("delete tproxy route rule mark %v failed err: %v", TProxyPreroutingMark, err)
	}

	dst := GetDefaultRouteDst(protocol)
	if err := delRouteIfExist(family, util.TProxyRouteTable, &dst); err != nil {
		klog.Errorf("delete tproxy route rule mark %v failed err: %v", TProxyPreroutingMark, err)
	}
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
		klog.Errorf("add rule %v failed with err %v", rule, err)
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
		return errors.New("can't find device lo")
	}

	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Table:     table,
		Scope:     unix.RT_SCOPE_HOST,
		Type:      unix.RTN_LOCAL,
	}

	if err = netlink.RouteReplace(&route); err != nil && !errors.Is(err, syscall.EEXIST) {
		klog.Errorf("add route %v failed with err %v", route, err)
		return err
	}

	return nil
}

func delRouteIfExist(family, table int, dst *net.IPNet) error {
	curRoutes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: table}, netlink.RT_FILTER_TABLE)
	if err != nil {
		klog.Errorf("list routes with table %d failed with err: %v", table, err)
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
		klog.Errorf("del route %v failed with err %v", route, err)
		return err
	}

	return nil
}

func handleRedirectFlow(conn net.Conn) {
	klog.V(5).Infof("Accepting TCP connection from %v with destination of %v", conn.RemoteAddr().String(), conn.LocalAddr().String())
	defer func() {
		if err := conn.Close(); err != nil {
			klog.Errorf("conn Close err: %v", err)
		}
	}()

	podIPPort := conn.LocalAddr().String()
	podIP, probePort, err := net.SplitHostPort(podIPPort)
	if err != nil {
		klog.Errorf("Get %s Pod IP and Port failed err: %v", podIPPort, err)
		return
	}

	probePortInNs(podIP, probePort, false, conn)
}

func probePortInNs(podIP, probePort string, isTProxyProbe bool, conn net.Conn) {
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
		customVPCPodIPToNs.Delete(podIP)
		klog.Infof("ns %s already deleted", podNs)
		return
	}

	_ = ns.WithNetNSPath(podNS.Path(), func(_ ns.NetNS) error {
		// Packet's src and dst IP are both PodIP in netns
		localpodTCPAddr := net.TCPAddr{IP: net.ParseIP(podIP)}
		remotepodTCPAddr := net.TCPAddr{IP: net.ParseIP(podIP), Port: iprobePort}

		remoteConn, err := goTProxy.DialTCP(&localpodTCPAddr, &remotepodTCPAddr, !isTProxyProbe)
		if err != nil {
			if isTProxyProbe {
				customVPCPodTCPProbeIPPort.Store(getIPPortString(podIP, probePort), false)
			}
			return nil
		}

		if isTProxyProbe {
			customVPCPodTCPProbeIPPort.Store(getIPPortString(podIP, probePort), true)
			return nil
		}

		defer func() {
			if err := remoteConn.Close(); err != nil {
				klog.Errorf("remoteConn %v Close err: %v", remoteConn, err)
			}
		}()

		var streamWait sync.WaitGroup
		streamWait.Add(2)

		streamConn := func(dst io.Writer, src io.Reader) {
			if _, err := io.Copy(dst, src); err != nil {
				klog.Errorf("copy stream from dst %v to src %v failed err: %v", dst, src, err)
			}

			streamWait.Done()
		}

		go streamConn(remoteConn, conn)
		go streamConn(conn, remoteConn)

		streamWait.Wait()
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

func GetDefaultRouteDst(protocol string) net.IPNet {
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
