package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	gatewayCheckModeDisabled = iota
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
	if util.IsOvnProvider(provider) {
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
		errMsg := fmt.Errorf("parse add request failed %w", err)
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
	var isDefaultRoute, noIPAM bool
	var pod *v1.Pod
	var err error
	for range 20 {
		if pod, err = csh.Controller.podsLister.Pods(podRequest.PodNamespace).Get(podRequest.PodName); err != nil {
			errMsg := fmt.Errorf("get pod %s/%s failed %w", podRequest.PodNamespace, podRequest.PodName, err)
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
		vmName = pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, podRequest.Provider)]
		ipAddr, noIPAM, err = util.GetIPAddrWithMaskForCNI(ip, cidr)
		if err != nil {
			errMsg := fmt.Errorf("failed to get ip address with mask, %w", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
		oldPodName = podRequest.PodName
		if s := pod.Annotations[fmt.Sprintf(util.RoutesAnnotationTemplate, podRequest.Provider)]; s != "" {
			if err = json.Unmarshal([]byte(s), &routes); err != nil {
				errMsg := fmt.Errorf("invalid routes for pod %s/%s: %w", pod.Namespace, pod.Name, err)
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

		if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" && util.IsOvnProvider(podRequest.Provider) {
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
	if !noIPAM {
		if err := csh.UpdateIPCR(podRequest, subnet, ip); err != nil {
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" && util.IsOvnProvider(podRequest.Provider) {
		err := fmt.Errorf("route is not ready for pod %s/%s provider %s, please see kube-ovn-controller logs to find errors", pod.Namespace, pod.Name, podRequest.Provider)
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	var mtu int
	routes = append(podRequest.Routes, routes...)
	if strings.HasSuffix(podRequest.Provider, util.OvnProvider) && subnet != "" {
		podSubnet, err := csh.Controller.subnetsLister.Get(subnet)
		if err != nil {
			errMsg := fmt.Errorf("failed to get subnet %s: %w", subnet, err)
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

		var vmMigration bool
		subnetHasVlan := podSubnet.Spec.Vlan != ""
		// skip ping check gateway for pods during live migration
		if pod.Annotations[util.MigrationJobAnnotation] == "" {
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
			vmMigration = true
		}
		if pod.Annotations[fmt.Sprintf(util.ActivationStrategyTemplate, podRequest.Provider)] != "" {
			gatewayCheckMode = gatewayCheckModeDisabled
		}

		if podSubnet.Spec.Mtu > 0 {
			mtu = int(podSubnet.Spec.Mtu)
		} else {
			if providerNetwork != "" && !podSubnet.Spec.LogicalGateway && !podSubnet.Spec.U2OInterconnection {
				node, err := csh.Controller.nodesLister.Get(csh.Config.NodeName)
				if err != nil {
					errMsg := fmt.Errorf("failed to get node %s: %w", csh.Config.NodeName, err)
					klog.Error(errMsg)
					if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
						klog.Errorf("failed to write response: %v", err)
					}
					return
				}
				mtuStr := node.Labels[fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetwork)]
				if mtuStr != "" {
					if mtu, err = strconv.Atoi(mtuStr); err != nil || mtu <= 0 {
						errMsg := fmt.Errorf("failed to parse provider network MTU %s: %w", mtuStr, err)
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

		macAddr = pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podRequest.Provider)]
		klog.Infof("create container interface %s mac %s, ip %s, cidr %s, gw %s, custom routes %v", ifName, macAddr, ipAddr, cidr, gw, routes)
		podNicName = ifName
		switch nicType {
		case util.InternalType:
			podNicName, routes, err = csh.configureNicWithInternalPort(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, vmMigration, routes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, podRequest.DeviceID, nicType, latency, limit, loss, jitter, gatewayCheckMode, u2oInterconnectionIP)
		case util.DpdkType:
			err = csh.configureDpdkNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, mtu, ipAddr, gw, ingress, egress, getShortSharedDir(pod.UID, podRequest.VhostUserSocketVolumeName), podRequest.VhostUserSocketName, podRequest.VhostUserSocketConsumption)
			routes = nil
		default:
			routes, err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, podRequest.VfDriver, ifName, macAddr, mtu, ipAddr, gw, isDefaultRoute, vmMigration, routes, podRequest.DNS.Nameservers, podRequest.DNS.Search, ingress, egress, podRequest.DeviceID, nicType, latency, limit, loss, jitter, gatewayCheckMode, u2oInterconnectionIP, oldPodName)
		}
		if err != nil {
			errMsg := fmt.Errorf("configure nic %s for pod %s/%s failed: %w", ifName, podRequest.PodName, podRequest.PodNamespace, err)
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
			errMsg := fmt.Errorf("failed to add egress configuration: %w", err)
			klog.Error(errMsg)
			if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	} else if len(routes) != 0 {
		hasDefaultRoute := make(map[string]bool, 2)
		for _, r := range routes {
			if r.Destination == "" {
				hasDefaultRoute[util.CheckProtocol(r.Gateway)] = true
				continue
			}
			if _, cidr, err := net.ParseCIDR(r.Destination); err == nil {
				if ones, _ := cidr.Mask.Size(); ones == 0 {
					hasDefaultRoute[util.CheckProtocol(r.Gateway)] = true
				}
			}
		}
		if len(hasDefaultRoute) != 0 {
			// remove existing default route so other CNI plugins, such as macvlan, can add the new default route correctly
			if err = csh.removeDefaultRoute(podRequest.NetNs, hasDefaultRoute[kubeovnv1.ProtocolIPv4], hasDefaultRoute[kubeovnv1.ProtocolIPv6]); err != nil {
				errMsg := fmt.Errorf("failed to remove existing default route for interface %s of pod %s/%s: %w", podRequest.IfName, podRequest.PodNamespace, podRequest.PodName, err)
				klog.Error(errMsg)
				if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
					klog.Errorf("failed to write response: %v", err)
				}
				return
			}
		}
	}

	response := &request.CniResponse{
		Protocol:   util.CheckProtocol(cidr),
		IPAddress:  ip,
		MacAddress: macAddr,
		CIDR:       cidr,
		PodNicName: podNicName,
		Routes:     routes,
		Mtu:        mtu,
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
	for range 20 {
		ipCR, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipCRName, metav1.GetOptions{})
		if err != nil {
			err = fmt.Errorf("failed to get ip crd for %s, %w", ip, err)
			// maybe create a backup pod with previous annotations
			klog.Error(err)
		} else if ipCR.Spec.NodeName != csh.Config.NodeName {
			ipCR := ipCR.DeepCopy()
			if ipCR.Labels == nil {
				ipCR.Labels = map[string]string{}
			}
			ipCR.Spec.NodeName = csh.Config.NodeName
			ipCR.Spec.AttachIPs = []string{}
			ipCR.Labels[subnet] = ""
			ipCR.Labels[util.NodeNameLabel] = csh.Config.NodeName
			ipCR.Spec.AttachSubnets = []string{}
			ipCR.Spec.AttachMacs = []string{}
			if _, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ipCR, metav1.UpdateOptions{}); err != nil {
				err = fmt.Errorf("failed to update ip crd for %s, %w", ip, err)
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
		errMsg := fmt.Errorf("parse del request failed %w", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	// Try to get the Pod, but if it fails due to not being found, log a warning and continue.
	pod, err := csh.Controller.podsLister.Pods(podRequest.PodNamespace).Get(podRequest.PodName)
	if err != nil && !k8serrors.IsNotFound(err) {
		errMsg := fmt.Errorf("failed to retrieve Pod %s/%s: %w", podRequest.PodNamespace, podRequest.PodName, err)
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

	var nicType string
	var vmName string

	// If the Pod was found, process its annotations and labels.
	if pod != nil {
		if pod.Annotations != nil && (util.IsOvnProvider(podRequest.Provider) || podRequest.CniType == util.CniTypeName) {
			subnet := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podRequest.Provider)]
			if subnet != "" {
				ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podRequest.Provider)]
				if err = csh.Controller.removeEgressConfig(subnet, ip); err != nil {
					errMsg := fmt.Errorf("failed to remove egress configuration: %w", err)
					klog.Error(errMsg)
					if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
						klog.Errorf("failed to write response, %v", err)
					}
					return
				}
			}

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

			vmName = pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, podRequest.Provider)]
			if vmName != "" {
				podRequest.PodName = vmName
			}
		}
	} else {
		// If the Pod is not found, assign a default value.
		klog.Warningf("Pod %s not found, proceeding with NIC deletion using ContainerID and NetNs", podRequest.PodName)
		switch {
		case podRequest.DeviceID != "":
			nicType = util.OffloadType
		case podRequest.VhostUserSocketVolumeName != "":
			nicType = util.DpdkType
		default:
			nicType = util.VethType
		}
	}

	// For Support kubevirt hotplug dpdk nic, forbidden set the volume name
	if podRequest.VhostUserSocketConsumption == util.ConsumptionKubevirt {
		podRequest.VhostUserSocketVolumeName = util.VhostUserSocketVolumeName
	}

	// Proceed to delete the NIC regardless of whether the Pod was found or not.
	err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID, podRequest.NetNs, podRequest.DeviceID, podRequest.IfName, nicType)
	if err != nil {
		errMsg := fmt.Errorf("del nic failed %w", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}
	resp.WriteHeader(http.StatusNoContent)
}
