package daemon

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emicklei/go-restful/v3"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	gatewayModeDisabled = iota
	gatewayCheckModePing
	gatewayCheckModeArping
)

type cniServerHandler struct {
	Config        *Configuration
	KubeClient    kubernetes.Interface
	KubeOvnClient clientset.Interface
	Controller    *Controller
	IPsQueue      workqueue.RateLimitingInterface
}

func createCniServerHandler(config *Configuration, controller *Controller) *cniServerHandler {
	csh := &cniServerHandler{KubeClient: config.KubeClient, KubeOvnClient: config.KubeOvnClient, Config: config, Controller: controller,
		IPsQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateIPS")}
	return csh
}

func (csh cniServerHandler) providerExists(provider string) bool {
	if provider == "" || strings.HasSuffix(provider, util.OvnProvider) {
		return true
	}
	subnets, _ := csh.Controller.subnetsLister.List(labels.Everything())
	for _, subnet := range subnets {
		if subnet.Spec.Provider == provider {
			return true
		}
	}
	return false
}

func (csh cniServerHandler) handleAdd(req *restful.Request, resp *restful.Response) {
	podRequest := request.CniRequest{}
	if err := req.ReadEntity(&podRequest); err != nil {
		errMsg := fmt.Errorf("parse add request failed %v", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}
	klog.V(5).Infof("request body is %v", podRequest)
	if exist := csh.providerExists(podRequest.Provider); !exist {
		errMsg := fmt.Errorf("provider %s not bind to any subnet", podRequest.Provider)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	klog.Infof("add port request: %v", podRequest)
	if err := csh.validatePodRequest(&podRequest); err != nil {
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	var gatewayCheckMode int
	var macAddr, ip, ipAddr, cidr, gw, subnet, ingress, egress, providerNetwork, ifName, nicType, podNicName, priority, vmName, latency, limit, loss string
	var isDefaultRoute bool
	var pod *v1.Pod
	var err error
	for i := 0; i < 20; i++ {
		if pod, err = csh.Controller.podsLister.Pods(podRequest.PodNamespace).Get(podRequest.PodName); err != nil {
			errMsg := fmt.Errorf("get pod %s/%s failed %v", podRequest.PodNamespace, podRequest.PodName, err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podRequest.Provider)] != "true" {
			klog.Infof("wait address for pod %s/%s provider %s", podRequest.PodNamespace, podRequest.PodName, podRequest.Provider)
			// wait controller assign an address
			cniWaitAddressResult.WithLabelValues(nodeName).Inc()
			time.Sleep(1 * time.Second)
			continue
		}

		if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
			klog.Errorf("validate pod %s/%s failed, %v", podRequest.PodNamespace, podRequest.PodName, err)
			// wait controller assign an address
			cniWaitAddressResult.WithLabelValues(nodeName).Inc()
			time.Sleep(1 * time.Second)
			continue
		}
		macAddr = pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podRequest.Provider)]
		ip = pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podRequest.Provider)]
		cidr = pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, podRequest.Provider)]
		gw = pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, podRequest.Provider)]
		subnet = pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podRequest.Provider)]
		ingress = pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, podRequest.Provider)]
		egress = pod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, podRequest.Provider)]
		priority = pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, podRequest.Provider)]
		latency = pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, podRequest.Provider)]
		limit = pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, podRequest.Provider)]
		loss = pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, podRequest.Provider)]
		providerNetwork = pod.Annotations[fmt.Sprintf(util.ProviderNetworkTemplate, podRequest.Provider)]
		vmName = pod.Annotations[fmt.Sprintf(util.VmTemplate, podRequest.Provider)]
		ipAddr = util.GetIpAddrWithMask(ip, cidr)
		if ifName = podRequest.IfName; ifName == "" {
			ifName = "eth0"
		}
		if podRequest.DeviceID != "" {
			nicType = util.OffloadType
		} else if podRequest.VhostUserSocketVolumeName != "" {
			nicType = util.DpdkType
			if err = createShortSharedDir(pod, podRequest.VhostUserSocketVolumeName); err != nil {
				klog.Error(err.Error())
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}
		} else {
			nicType = pod.Annotations[fmt.Sprintf(util.PodNicAnnotationTemplate, podRequest.Provider)]
		}

		switch pod.Annotations[fmt.Sprintf(util.DefaultRouteAnnotationTemplate, podRequest.Provider)] {
		case "true":
			isDefaultRoute = true
		case "false":
			isDefaultRoute = false
		default:
			isDefaultRoute = ifName == "eth0"
		}

		if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" && strings.HasSuffix(providerNetwork, util.OvnProvider) {
			klog.Infof("wait route ready for pod %s/%s provider %s", podRequest.PodNamespace, podRequest.PodName, podRequest.Provider)
			cniWaitRouteResult.WithLabelValues(nodeName).Inc()
			time.Sleep(1 * time.Second)
			continue
		}

		if vmName != "" {
			podRequest.PodName = vmName
		}

		break
	}

	if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podRequest.Provider)] != "true" {
		err := fmt.Errorf("no address allocated to pod %s/%s provider %s, please see kube-ovn-controller logs to find errors", pod.Namespace, pod.Name, podRequest.Provider)
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if err := csh.TryUpdateIPCr(podRequest, subnet, ip, macAddr); err != nil {
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" && strings.HasSuffix(providerNetwork, util.OvnProvider) {
		err := fmt.Errorf("route is not ready for pod %s/%s provider %s, please see kube-ovn-controller logs to find errors", pod.Namespace, pod.Name, podRequest.Provider)
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if strings.HasSuffix(podRequest.Provider, util.OvnProvider) && subnet != "" {
		podSubnet, err := csh.Controller.subnetsLister.Get(subnet)
		if err != nil {
			errMsg := fmt.Errorf("failed to get subnet %s: %v", subnet, err)
			klog.Error(errMsg)
			if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response: %v", err)
			}
			return
		}

		subnetPriority := csh.Controller.getSubnetQosPriority(subnet)
		if priority == "" && subnetPriority != "" {
			priority = subnetPriority
		}

		//skip ping check gateway for pods during live migration
		if pod.Annotations[fmt.Sprintf(util.LiveMigrationAnnotationTemplate, podRequest.Provider)] != "true" {
			if !podSubnet.Spec.DisableGatewayCheck {
				if podSubnet.Spec.Vlan != "" && !podSubnet.Spec.LogicalGateway {
					gatewayCheckMode = gatewayCheckModeArping
				} else {
					gatewayCheckMode = gatewayCheckModePing
				}
			}
		}

		var mtu int
		var node *v1.Node
		if providerNetwork != "" {
			if node, err = csh.Controller.nodesLister.Get(csh.Config.NodeName); err != nil {
				errMsg := fmt.Errorf("failed to get node %s: %v", csh.Config.NodeName, err)
				klog.Error(errMsg)
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}
			mtuStr := node.Labels[fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetwork)]
			if mtuStr != "" {
				if mtu, err = strconv.Atoi(mtuStr); err != nil || mtu <= 0 {
					errMsg := fmt.Errorf("failed to parse provider network MTU %s: %v", mtuStr, err)
					klog.Error(errMsg)
					if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
						klog.Errorf("failed to write response: %v", err)
					}
					return
				}
			}
		} else {
			mtu = csh.Config.MTU
		}

		// routes used for access from underlay to overlay
		var u2oRoutes []request.Route
		if podSubnet.Spec.U2oRouting && podSubnet.Spec.Vlan != "" &&
			!podSubnet.Spec.LogicalGateway && podSubnet.Spec.Vpc == util.DefaultVpc {
			subnets, err := csh.Controller.subnetsLister.List(labels.Everything())
			if err != nil {
				errMsg := fmt.Errorf("failed to list subnets: %v", err)
				klog.Error(errMsg)
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}

			if node == nil {
				if node, err = csh.Controller.nodesLister.Get(csh.Config.NodeName); err != nil {
					errMsg := fmt.Errorf("failed to get node %s: %v", csh.Config.NodeName, err)
					klog.Error(errMsg)
					if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
						klog.Errorf("failed to write response: %v", err)
					}
					return
				}
			}

			podCidrV4, podCidrV6 := util.SplitStringIP(cidr)
			nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
			v4Routing := util.CIDRContainIP(podCidrV4, nodeIPv4)
			v6Routing := util.CIDRContainIP(podCidrV6, nodeIPv6)
			for _, subnet := range subnets {
				if subnet.Spec.Vpc == util.DefaultVpc && (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) {
					if !subnet.Status.IsReady() {
						klog.V(5).Infof("subnet %s is not ready, skip", subnet.Name)
						continue
					}

					cidrV4, cidrV6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
					if v4Routing && cidrV4 != "" {
						u2oRoutes = append(u2oRoutes, request.Route{Destination: cidrV4, Gateway: nodeIPv4})
					}
					if v6Routing && cidrV6 != "" {
						u2oRoutes = append(u2oRoutes, request.Route{Destination: cidrV6, Gateway: nodeIPv6})
					}
				}
			}
		}

		klog.Infof("create container interface %s mac %s, ip %s, cidr %s, gw %s, u2o routes %v, custom routes %v", ifName, macAddr, ipAddr, cidr, gw, u2oRoutes, podRequest.Routes)
		allRoutes := append(u2oRoutes, podRequest.Routes...)
		if nicType == util.InternalType {
			podNicName, err = csh.configureNicWithInternalPort(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, allRoutes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, priority, podRequest.DeviceID, nicType, latency, limit, loss, gatewayCheckMode)
		} else if nicType == util.DpdkType {
			err = csh.configureDpdkNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, ingress, egress, priority, getShortSharedDir(pod.UID, podRequest.VhostUserSocketVolumeName), podRequest.VhostUserSocketName)
		} else {
			podNicName = ifName
			err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, podRequest.VfDriver, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, allRoutes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, priority, podRequest.DeviceID, nicType, latency, limit, loss, gatewayCheckMode)
		}
		if err != nil {
			errMsg := fmt.Errorf("configure nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}

		ifaceID := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
		if err = ovs.ConfigInterfaceMirror(csh.Config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID); err != nil {
			klog.Errorf("failed mirror to mirror0, %v", err)
			return
		}

		if err = csh.Controller.addEgressConfig(podSubnet, ip); err != nil {
			errMsg := fmt.Errorf("failed to add egress configuration: %v", err)
			klog.Error(errMsg)
			if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	response := &request.CniResponse{
		Protocol:   util.CheckProtocol(cidr),
		IpAddress:  ip,
		MacAddress: macAddr,
		CIDR:       cidr,
		PodNicName: podNicName,
	}
	if isDefaultRoute {
		response.Gateway = gw
	}
	if err := resp.WriteHeaderAndEntity(http.StatusOK, response); err != nil {
		klog.Errorf("failed to write response, %v", err)
	}
}

func (csh cniServerHandler) TryUpdateIPCr(podRequest request.CniRequest, subnet, ip, macAddr string) error {
	//	v4IP, v6IP := util.SplitStringIP(ip)
	ipCrName := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
	oriIpCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipCrName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Infof("not find IPS %v, adding in queue...", ipCrName)
			csh.IPsQueue.AddRateLimited(ipCrName + "//" + subnet)
			/*_, err := csh.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
				ObjectMeta: metav1.ObjectMeta{
					Name: ipCrName,
					Labels: map[string]string{
						util.SubnetNameLabel: subnet,
						subnet:               "",
					},
				},
				Spec: kubeovnv1.IPSpec{
					PodName:       podRequest.PodName,
					Namespace:     podRequest.PodNamespace,
					Subnet:        subnet,
					NodeName:      csh.Config.NodeName,
					IPAddress:     ip,
					V4IPAddress:   v4IP,
					V6IPAddress:   v6IP,
					MacAddress:    macAddr,
					ContainerID:   podRequest.ContainerID,
					AttachIPs:     []string{},
					AttachMacs:    []string{},
					AttachSubnets: []string{},
				},
			}, metav1.CreateOptions{})
			if err != nil {
				errMsg := fmt.Errorf("failed to create ip crd for %s, provider '%s', %v", ip, podRequest.Provider, err)
				klog.Error(errMsg)
				return errMsg
			}*/
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		ipCr := oriIpCr.DeepCopy()
		ipCr.Spec.NodeName = csh.Config.NodeName
		ipCr.Spec.AttachIPs = []string{}
		ipCr.Labels[subnet] = ""
		ipCr.Spec.AttachSubnets = []string{}
		ipCr.Spec.AttachMacs = []string{}
		if _, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ipCr, metav1.UpdateOptions{}); err != nil {
			errMsg := fmt.Errorf("failed to update ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			return errMsg
		}
	}
	return nil
}

func (csh cniServerHandler) handleDel(req *restful.Request, resp *restful.Response) {
	var podRequest request.CniRequest
	if err := req.ReadEntity(&podRequest); err != nil {
		errMsg := fmt.Errorf("parse del request failed %v", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	pod, err := csh.Controller.podsLister.Pods(podRequest.PodNamespace).Get(podRequest.PodName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			resp.WriteHeader(http.StatusNoContent)
			return
		}

		errMsg := fmt.Errorf("parse del request failed %v", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	klog.Infof("del port request: %v", podRequest)
	if err := csh.validatePodRequest(&podRequest); err != nil {
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if pod.Annotations != nil && (podRequest.Provider == util.OvnProvider || podRequest.CniType == util.CniTypeName) {
		subnet := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podRequest.Provider)]
		if subnet != "" {
			ip := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podRequest.Provider)]
			if err = csh.Controller.removeEgressConfig(subnet, ip); err != nil {
				errMsg := fmt.Errorf("failed to remove egress configuration: %v", err)
				klog.Error(errMsg)
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
					klog.Errorf("failed to write response, %v", err)
				}
				return
			}
		}

		var nicType string
		if podRequest.DeviceID != "" {
			nicType = util.OffloadType
		} else if podRequest.VhostUserSocketVolumeName != "" {
			nicType = util.DpdkType
			if err = removeShortSharedDir(pod, podRequest.VhostUserSocketVolumeName); err != nil {
				klog.Error(err.Error())
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}

		} else {
			nicType = pod.Annotations[fmt.Sprintf(util.PodNicAnnotationTemplate, podRequest.Provider)]
		}
		vmName := pod.Annotations[fmt.Sprintf(util.VmTemplate, podRequest.Provider)]
		if vmName != "" {
			podRequest.PodName = vmName
		}

		err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID, podRequest.NetNs, podRequest.DeviceID, podRequest.IfName, nicType)
		if err != nil {
			errMsg := fmt.Errorf("del nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	resp.WriteHeader(http.StatusNoContent)
}
