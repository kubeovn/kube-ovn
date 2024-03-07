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

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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

func (csh cniServerHandler) providerExists(provider string) (*kubeovnv1.Subnet, bool) {
	if provider == "" || strings.HasSuffix(provider, util.OvnProvider) {
		return nil, true
	}
	subnets, _ := csh.Controller.subnetsLister.List(labels.Everything())
	for _, subnet := range subnets {
		if subnet.Spec.Provider == provider {
			return subnet.DeepCopy(), true
		}
	}
	return nil, false
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
	podSubnet, exist := csh.providerExists(podRequest.Provider)
	if !exist {
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
	var macAddr, ip, ipAddr, cidr, gw, subnet, ingress, egress, providerNetwork, ifName, nicType, podNicName, vmName, latency, limit, loss, jitter, u2oInterconnectionIP, oldPodName string
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
		ip = pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podRequest.Provider)]
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
		vmName = pod.Annotations[fmt.Sprintf(util.VMTemplate, podRequest.Provider)]
		ipAddr, err = util.GetIPAddrWithMask(ip, cidr)
		if err != nil {
			errMsg := fmt.Errorf("failed to get ip address with mask, %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
		oldPodName = podRequest.PodName
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

		// For Support kubevirt hotplug dpdk nic, forbidden set the volume name
		if podRequest.VhostUserSocketConsumption == util.ConsumptionKubevirt {
			podRequest.VhostUserSocketVolumeName = util.VhostUserSocketVolumeName
		}

		switch {
		case podRequest.DeviceID != "":
			nicType = util.OffloadType
		case podRequest.VhostUserSocketVolumeName != "":
			nicType = util.DpdkType
			if err = createShortSharedDir(pod, podRequest.VhostUserSocketVolumeName, podRequest.VhostUserSocketConsumption, csh.Config.KubeletDir); err != nil {
				klog.Error(err.Error())
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}
		default:
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

	if subnet == "" && podSubnet != nil {
		subnet = podSubnet.Name
	}
	if err := csh.UpdateIPCR(podRequest, subnet, ip); err != nil {
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
			errMsg := fmt.Errorf("failed to generate u2o ip on subnet %s", podSubnet.Name)
			klog.Error(errMsg)
			return
		}

		if podSubnet.Status.U2OInterconnectionIP != "" && podSubnet.Spec.U2OInterconnection {
			u2oInterconnectionIP = podSubnet.Status.U2OInterconnectionIP
		}

		subnetHasVlan := podSubnet.Spec.Vlan != ""
		detectIPConflict := csh.Config.EnableArpDetectIPConflict && subnetHasVlan
		// skip ping check gateway for pods during live migration
		if pod.Annotations[fmt.Sprintf(util.LiveMigrationAnnotationTemplate, podRequest.Provider)] != "true" {
			if subnetHasVlan && !podSubnet.Spec.LogicalGateway {
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
		if podSubnet.Spec.Mtu > 0 {
			mtu = int(podSubnet.Spec.Mtu)
		} else {
			if providerNetwork != "" && !podSubnet.Spec.LogicalGateway && !podSubnet.Spec.U2OInterconnection {
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
		}

		routes = append(podRequest.Routes, routes...)
		macAddr = pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podRequest.Provider)]
		klog.Infof("create container interface %s mac %s, ip %s, cidr %s, gw %s, custom routes %v", ifName, macAddr, ipAddr, cidr, gw, routes)
		podNicName = ifName
		switch nicType {
		case util.InternalType:
			podNicName, err = csh.configureNicWithInternalPort(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, detectIPConflict, routes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, podRequest.DeviceID, nicType, latency, limit, loss, jitter, gatewayCheckMode, u2oInterconnectionIP)
		case util.DpdkType:
			err = csh.configureDpdkNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, ingress, egress, getShortSharedDir(pod.UID, podRequest.VhostUserSocketVolumeName), podRequest.VhostUserSocketName, podRequest.VhostUserSocketConsumption)
		default:
			err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, podRequest.VfDriver, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, detectIPConflict, routes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, podRequest.DeviceID, nicType, latency, limit, loss, jitter, gatewayCheckMode, u2oInterconnectionIP, oldPodName)
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
		if err = ovs.ConfigInterfaceMirror(csh.Config.EnableMirror, pod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, podRequest.Provider)], ifaceID); err != nil {
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
		IPAddress:  ip,
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

func (csh cniServerHandler) UpdateIPCR(podRequest request.CniRequest, subnet, ip string) error {
	ipCRName := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
	for i := 0; i < 20; i++ {
		ipCR, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipCRName, metav1.GetOptions{})
		if err != nil {
			err = fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
			// maybe create a backup pod with previous annotations
			klog.Error(err)
		} else if ipCR.Spec.NodeName != csh.Config.NodeName {
			ipCR := ipCR.DeepCopy()
			ipCR.Spec.NodeName = csh.Config.NodeName
			ipCR.Spec.AttachIPs = []string{}
			ipCR.Labels[subnet] = ""
			ipCR.Spec.AttachSubnets = []string{}
			ipCR.Spec.AttachMacs = []string{}
			if _, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ipCR, metav1.UpdateOptions{}); err != nil {
				err = fmt.Errorf("failed to update ip crd for %s, %v", ip, err)
				klog.Error(err)
			} else {
				return nil
			}
		}
		if err != nil {
			klog.Warningf("wait pod ip %s to be ready", ipCRName)
			time.Sleep(1 * time.Second)
		} else {
			return nil
		}
	}
	// update ip spec node is not that necessary, so we just log the error
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

	if podRequest.NetNs == "" {
		klog.Infof("skip del port request: %v", podRequest)
		resp.WriteHeader(http.StatusNoContent)
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
			ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podRequest.Provider)]
			if err = csh.Controller.removeEgressConfig(subnet, ip); err != nil {
				errMsg := fmt.Errorf("failed to remove egress configuration: %v", err)
				klog.Error(errMsg)
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
					klog.Errorf("failed to write response, %v", err)
				}
				return
			}
		}

		// For Support kubevirt hotplug dpdk nic, forbidden set the volume name
		if podRequest.VhostUserSocketConsumption == util.ConsumptionKubevirt {
			podRequest.VhostUserSocketVolumeName = util.VhostUserSocketVolumeName
		}

		var nicType string
		switch {
		case podRequest.DeviceID != "":
			nicType = util.OffloadType
		case podRequest.VhostUserSocketVolumeName != "":
			nicType = util.DpdkType
			if err = removeShortSharedDir(pod, podRequest.VhostUserSocketVolumeName, podRequest.VhostUserSocketConsumption); err != nil {
				klog.Error(err.Error())
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}
		default:
			nicType = pod.Annotations[fmt.Sprintf(util.PodNicAnnotationTemplate, podRequest.Provider)]
		}
		vmName := pod.Annotations[fmt.Sprintf(util.VMTemplate, podRequest.Provider)]
		if vmName != "" {
			podRequest.PodName = vmName
		}

		err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID, podRequest.NetNs, podRequest.DeviceID, podRequest.IfName, nicType, podRequest.Provider)
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
