package daemon

import (
	"context"
	"encoding/json"
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
	gatewayCheckModePingNotConcerned
	gatewayCheckModeArpingNotConcerned
)

type cniServerHandler struct {
	Config        *Configuration
	KubeClient    kubernetes.Interface
	KubeOvnClient clientset.Interface
	Controller    *Controller
}

func createCniServerHandler(config *Configuration, controller *Controller) *cniServerHandler {
	csh := &cniServerHandler{KubeClient: config.KubeClient, KubeOvnClient: config.KubeOvnClient, Config: config, Controller: controller}
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
	var macAddr, ip, ipAddr, cidr, gw, subnet, ingress, egress, providerNetwork, ifName, nicType, podNicName, vmName, latency, limit, loss, jitter, u2oInterconnectionIP string
	var routes []request.Route
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
		latency = pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, podRequest.Provider)]
		limit = pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, podRequest.Provider)]
		loss = pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, podRequest.Provider)]
		jitter = pod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, podRequest.Provider)]
		providerNetwork = pod.Annotations[fmt.Sprintf(util.ProviderNetworkTemplate, podRequest.Provider)]
		vmName = pod.Annotations[fmt.Sprintf(util.VmTemplate, podRequest.Provider)]
		ipAddr = util.GetIpAddrWithMask(ip, cidr)
		if s := pod.Annotations[fmt.Sprintf(util.RoutesAnnotationTemplate, podRequest.Provider)]; s != "" {
			if err = json.Unmarshal([]byte(s), &routes); err != nil {
				errMsg := fmt.Errorf("invalid routes for pod %s/%s: %v", pod.Namespace, pod.Name, err)
				klog.Error(errMsg)
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}
		}
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

		if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" && strings.HasSuffix(podRequest.Provider, util.OvnProvider) {
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

	if err := csh.UpdateIPCr(podRequest, subnet, ip, macAddr); err != nil {
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" && strings.HasSuffix(podRequest.Provider, util.OvnProvider) {
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

		if podSubnet.Status.U2OInterconnectionIP == "" && podSubnet.Spec.U2OInterconnection {
			errMsg := fmt.Errorf("failed to generate u2o ip on subnet %s ", podSubnet.Name)
			klog.Error(errMsg)
			return
		}

		if podSubnet.Status.U2OInterconnectionIP != "" && podSubnet.Spec.U2OInterconnection {
			u2oInterconnectionIP = podSubnet.Status.U2OInterconnectionIP
		}

		detectIPConflict := podSubnet.Spec.Vlan != ""
		// skip ping check gateway for pods during live migration
		if pod.Annotations[fmt.Sprintf(util.LiveMigrationAnnotationTemplate, podRequest.Provider)] != "true" {
			if podSubnet.Spec.Vlan != "" && !podSubnet.Spec.LogicalGateway {
				if podSubnet.Spec.DisableGatewayCheck {
					gatewayCheckMode = gatewayCheckModeArpingNotConcerned
				} else {
					gatewayCheckMode = gatewayCheckModeArping
				}
			} else {
				if podSubnet.Spec.DisableGatewayCheck {
					gatewayCheckMode = gatewayCheckModePingNotConcerned
				} else {
					gatewayCheckMode = gatewayCheckModePing
				}
			}
		} else {
			// do not perform ipv4 conflict detection during VM live migration
			detectIPConflict = false
		}

		var mtu int
		if providerNetwork != "" {
			node, err := csh.Controller.nodesLister.Get(csh.Config.NodeName)
			if err != nil {
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

		routes = append(podRequest.Routes, routes...)
		klog.Infof("create container interface %s mac %s, ip %s, cidr %s, gw %s, custom routes %v", ifName, macAddr, ipAddr, cidr, gw, routes)
		if nicType == util.InternalType {
			podNicName, err = csh.configureNicWithInternalPort(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, detectIPConflict, routes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, podRequest.DeviceID, nicType, latency, limit, loss, jitter, gatewayCheckMode, u2oInterconnectionIP)
		} else if nicType == util.DpdkType {
			err = csh.configureDpdkNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, ingress, egress, getShortSharedDir(pod.UID, podRequest.VhostUserSocketVolumeName), podRequest.VhostUserSocketName)
		} else {
			podNicName = ifName
			err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, podRequest.VfDriver, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, detectIPConflict, routes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, podRequest.DeviceID, nicType, latency, limit, loss, jitter, gatewayCheckMode, u2oInterconnectionIP)
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

func (csh cniServerHandler) UpdateIPCr(podRequest request.CniRequest, subnet, ip, macAddr string) error {
	ipCrName := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
	oriIpCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipCrName, metav1.GetOptions{})
	if err != nil {
		errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
		klog.Error(errMsg)
		return errMsg
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
