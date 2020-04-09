package daemon

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/vishvananda/netlink"
	"net"
	"net/http"
	"strings"
	"time"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/alauda/kube-ovn/pkg/client/clientset/versioned"
	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/alauda/kube-ovn/pkg/util"

	"github.com/emicklei/go-restful"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

type cniServerHandler struct {
	Config        *Configuration
	KubeClient    kubernetes.Interface
	KubeOvnClient clientset.Interface
}

func createCniServerHandler(config *Configuration) *cniServerHandler {
	csh := &cniServerHandler{KubeClient: config.KubeClient, KubeOvnClient: config.KubeOvnClient, Config: config}
	return csh
}

func (csh cniServerHandler) handleAdd(req *restful.Request, resp *restful.Response) {
	podRequest := request.CniRequest{}
	err := req.ReadEntity(&podRequest)
	if err != nil {
		errMsg := fmt.Errorf("parse add request failed %v", err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()})
		return
	}

	klog.Infof("add port request %v", podRequest)

	var macAddr, ip, ipAddr, cidr, gw, subnet, ingress, egress, networkType, vlanID, vlanRange, providerInterfaceName, hostInterfaceName string
	pod, err := csh.KubeClient.CoreV1().Pods(podRequest.PodNamespace).Get(podRequest.PodName, v1.GetOptions{})
	if err != nil {
		errMsg := fmt.Errorf("get pod %s/%s failed %v", podRequest.PodNamespace, podRequest.PodName, err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
		return
	}

	for i := 0; i < 10; i++ {
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podRequest.Provider)] != "true" {
			klog.Infof("wait address for  pod %s/%s ", podRequest.PodNamespace, podRequest.PodName)
			// wait controller assign an address
			time.Sleep(1 * time.Second)
			continue
		}

		if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
			klog.Errorf("validate pod %s/%s failed, %v", podRequest.PodNamespace, podRequest.PodName, err)
			// wait controller assign an address
			time.Sleep(1 * time.Second)
			continue
		}
		macAddr = pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podRequest.Provider)]
		ip = pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podRequest.Provider)]
		cidr = pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, podRequest.Provider)]
		gw = pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, podRequest.Provider)]
		subnet = pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podRequest.Provider)]
		ingress = pod.Annotations[util.IngressRateAnnotation]
		egress = pod.Annotations[util.EgressRateAnnotation]
		vlanID = pod.Annotations[util.VlanIdAnnotation]
		networkType = pod.Annotations[util.NetworkType]
		providerInterfaceName = pod.Annotations[util.ProviderInterfaceName]
		hostInterfaceName = pod.Annotations[util.HostInterfaceName]
		vlanRange = pod.Annotations[util.VlanRangeAnnotation]

		break
	}

	if macAddr == "" || ip == "" || cidr == "" || gw == "" {
		errMsg := fmt.Errorf("no available ip for pod %s/%s", podRequest.PodNamespace, podRequest.PodName)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
		return
	}

	ipCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := csh.KubeOvnClient.KubeovnV1().IPs().Create(&kubeovnv1.IP{
				ObjectMeta: v1.ObjectMeta{
					Name: fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace),
					Labels: map[string]string{
						util.SubnetNameLabel: subnet,
						subnet:               "",
					},
				},
				Spec: kubeovnv1.IPSpec{
					PodName:     podRequest.PodName,
					Namespace:   podRequest.PodNamespace,
					Subnet:      subnet,
					NodeName:    csh.Config.NodeName,
					IPAddress:   ip,
					MacAddress:  macAddr,
					ContainerID: podRequest.ContainerID,
				},
			})
			if err != nil {
				errMsg := fmt.Errorf("failed to create ip crd for %s, %v", ip, err)
				klog.Error(errMsg)
				resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
				return
			}
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}
	} else {
		ipCr.Labels[subnet] = ""
		ipCr.Spec.AttachSubnets = append(ipCr.Spec.AttachSubnets, subnet)
		ipCr.Spec.AttachIPs = append(ipCr.Spec.AttachIPs, ip)
		ipCr.Spec.AttachMacs = append(ipCr.Spec.AttachMacs, macAddr)
		_, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(ipCr)
		if err != nil {
			errMsg := fmt.Errorf("failed to update ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}
	}

	ipAddr = fmt.Sprintf("%s/%s", ip, strings.Split(cidr, "/")[1])
	if podRequest.Provider == util.OvnProvider {
		klog.Infof("create container mac %s, ip %s, cidr %s, gw %s", macAddr, ipAddr, cidr, gw)
		err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetNs, podRequest.ContainerID, macAddr, ipAddr, gw, ingress, egress)
		if err != nil {
			errMsg := fmt.Errorf("configure nic failed %v", err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}
	}

	if util.IsProviderVlan(networkType, providerInterfaceName) {
		//create patch port
		exists, err := providerBridgeExists()
		if err != nil {
			errMsg := fmt.Errorf("check provider bridge exists failed, %v", err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}

		if !exists {
			//create br-provider
			if err = configProviderPort(providerInterfaceName); err != nil {
				errMsg := fmt.Errorf("configure patch port br-provider failed %v", err)
				klog.Error(errMsg)
				resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
				return
			}

			//add a host nic to br-provider
			ifName := csh.getInterfaceName(hostInterfaceName)
			if ifName == "" {
				errMsg := fmt.Errorf("failed get host nic to add ovs br-provider")
				klog.Error(errMsg)
				resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
				return
			}

			if err = configProviderNic(ifName); err != nil {
				errMsg := fmt.Errorf("add nic %s to port br-provider failed %v", ifName, err)
				klog.Error(errMsg)
				resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
				return
			}
		}

		if err = csh.addRoute(ipAddr); err != nil {
			errMsg := fmt.Errorf("add pod route failed, %v", err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}
	}

	//set ovs port tag
	if util.IsNetworkVlan(networkType, vlanID, vlanRange) {
		hostNicName, _ := generateNicName(podRequest.ContainerID)
		if err := ovs.SetPortTag(hostNicName, vlanID); err != nil {
			errMsg := fmt.Errorf("configure port tag failed %v", err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}
	}

	resp.WriteHeaderAndEntity(http.StatusOK, request.CniResponse{Protocol: util.CheckProtocol(ipAddr), IpAddress: strings.Split(ipAddr, "/")[0], MacAddress: macAddr, CIDR: cidr, Gateway: gw})
}

func (csh cniServerHandler) handleDel(req *restful.Request, resp *restful.Response) {
	podRequest := request.CniRequest{}
	err := req.ReadEntity(&podRequest)

	if err != nil {
		errMsg := fmt.Errorf("parse del request failed %v", err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()})
		return
	}

	klog.Infof("delete port request %v", podRequest)
	if podRequest.Provider == util.OvnProvider {
		err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID)
		if err != nil {
			errMsg := fmt.Errorf("del nic failed %v", err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
			return
		}
	}

	err = csh.KubeOvnClient.KubeovnV1().IPs().Delete(fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace), &metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		errMsg := fmt.Errorf("del ipcrd for %s failed %v", fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace), err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()})
		return
	}

	resp.WriteHeader(http.StatusNoContent)
}

//get host nic name
func (csh cniServerHandler) getInterfaceName(hostInterfaceName string) string {
	var interfaceName string

	node, err := csh.Config.KubeClient.CoreV1().Nodes().Get(csh.Config.NodeName, metav1.GetOptions{})
	if err == nil {
		labels := node.GetLabels()
		interfaceName = labels[util.HostInterfaceName]
	}

	if interfaceName != "" {
		return interfaceName
	}

	if hostInterfaceName != "" {
		return hostInterfaceName
	}

	if csh.Config.Iface != "" {
		return csh.Config.Iface
	}

	return ""
}

//add a static route. If it is not added, the pod will not receive packets from the host nic
func (csh cniServerHandler) addRoute(ipAddr string) error {
	nic, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Errorf("failed to get nic %s", util.NodeNic)
		return fmt.Errorf("failed to get nic %s", util.NodeNic)
	}

	existRoutes, err := netlink.RouteList(nic, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	_, cidr, _ := net.ParseCIDR(ipAddr)
	for _, route := range existRoutes {
		if route.Dst == cidr {
			return nil
		}
	}

	node, err := csh.Config.KubeClient.CoreV1().Nodes().Get(csh.Config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node %s %v", csh.Config.NodeName, err)
		return err
	}

	gateway, ok := node.Annotations[util.GatewayAnnotation]
	if !ok {
		klog.Errorf("annotation for node %s ovn.kubernetes.io/gateway not exists", node.Name)
		return fmt.Errorf("annotation for node ovn.kubernetes.io/gateway not exists")
	}

	gw := net.ParseIP(gateway)
	if err = netlink.RouteReplace(&netlink.Route{Dst: cidr, LinkIndex: nic.Attrs().Index, Scope: netlink.SCOPE_UNIVERSE, Gw: gw}); err != nil {
		klog.Errorf("failed to add route %v", err)
	}

	return err
}
