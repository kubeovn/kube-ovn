package daemon

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/request"
	"fmt"
	"github.com/emicklei/go-restful"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type CniServerHandler struct {
	Config     *Configuration
	KubeClient kubernetes.Interface
}

func createCniServerHandler(config *Configuration) (*CniServerHandler, error) {
	csh := &CniServerHandler{KubeClient: config.KubeClient, Config: config}
	err := csh.initNodeGateway()
	if err != nil {
		return nil, err
	}
	return csh, nil
}

func (csh CniServerHandler) handleAdd(req *restful.Request, resp *restful.Response) {
	podRequest := request.PodRequest{}
	err := req.ReadEntity(&podRequest)
	if err != nil {
		klog.Errorf("parse add request failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, err)
		return
	}
	klog.Infof("add port request %v", podRequest)
	var macAddr, ipAddr, cidr, gw string
	for i := 0; i < 10; i++ {
		pod, err := csh.KubeClient.CoreV1().Pods(podRequest.PodNamespace).Get(podRequest.PodName, v1.GetOptions{})
		if err != nil {
			klog.Errorf("get pod %s/%s failed %v", podRequest.PodNamespace, podRequest.PodName, err)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, err)
			return
		}
		macAddr = pod.GetAnnotations()["ovn.kubernetes.io/mac_address"]
		ipAddr = pod.GetAnnotations()["ovn.kubernetes.io/ip_address"]
		cidr = pod.GetAnnotations()["ovn.kubernetes.io/cidr"]
		gw = pod.GetAnnotations()["ovn.kubernetes.io/gateway"]

		if macAddr == "" || ipAddr == "" || cidr == "" || gw == "" {
			// wait controller assign an address
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}

	klog.Infof("create container mac %s, ip %s, cidr %s, gw %s", macAddr, ipAddr, cidr, gw)
	err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetNs, podRequest.ContainerID, macAddr, ipAddr, gw)
	if err != nil {
		klog.Errorf("configure nic failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	resp.WriteHeaderAndEntity(http.StatusOK, request.PodResponse{IpAddress: strings.Split(ipAddr, "/")[0], MacAddress: macAddr, CIDR: "10.16.0.0/16", Gateway: gw})

}

func (csh CniServerHandler) handleDel(req *restful.Request, resp *restful.Response) {
	podRequest := request.PodRequest{}
	err := req.ReadEntity(&podRequest)
	if err != nil {
		klog.Errorf("parse del request failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, err)
		return
	}
	klog.Infof("delete port request %v", podRequest)
	err = csh.deleteNic(podRequest.NetNs, podRequest.ContainerID)
	if err != nil {
		klog.Errorf("del nic failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	resp.WriteHeader(http.StatusNoContent)
}

func (csh CniServerHandler) initNodeGateway() error {
	nodeName := csh.Config.NodeName
	node, err := csh.KubeClient.CoreV1().Nodes().Get(nodeName, v1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node %s info %v", nodeName, err)
		return err
	}
	macAddr := node.GetAnnotations()["ovn.kubernetes.io/mac_address"]
	ipAddr := node.GetAnnotations()["ovn.kubernetes.io/ip_address"]
	portName := node.GetAnnotations()["ovn.kubernetes.io/port_name"]
	if macAddr == "" || ipAddr == "" || portName == "" {
		return fmt.Errorf("can not find macAddr, ipAddr and portName")
	}
	return configureNodeNic(portName, ipAddr, macAddr)
}

func configureNodeNic(nicName, ip, mac string) error {
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("failed to parse mac %s %v", macAddr, err)
	}

	raw, err := exec.Command(
		"ovs-vsctl", "add-port", "br-int", nicName, "--",
		"set", "interface", nicName, "type=internal", "--",
		"set", "interface", nicName, fmt.Sprintf("external_ids:iface-id=%s", nicName)).CombinedOutput()
	if err != nil && !strings.Contains(string(raw), "already exists") {
		klog.Errorf("failed to configure node nic %s %s", nicName, string(raw))
		return fmt.Errorf(string(raw))
	}

	nodeLink, err := netlink.LinkByName(nicName)
	if err != nil {
		return fmt.Errorf("can not find node nic %s %v", nicName, err)
	}

	ipAddr, err := netlink.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("can not parse %s %v", ip, err)
	}
	err = netlink.AddrAdd(nodeLink, ipAddr)
	if err != nil {
		return fmt.Errorf("can not add address to node nic %v", err)
	}

	err = netlink.LinkSetHardwareAddr(nodeLink, macAddr)
	if err != nil {
		return fmt.Errorf("can not set mac address to node nic %v", err)
	}
	err = netlink.LinkSetUp(nodeLink)
	if err != nil {
		return fmt.Errorf("can not set node nic %s up %v", nicName, err)
	}

	return nil
}
