package daemon

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emicklei/go-restful/v3"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (csh cniServerHandler) handleAdd(req *restful.Request, resp *restful.Response) {
	var podRequest request.CniRequest
	if err := req.ReadEntity(&podRequest); err != nil {
		errMsg := fmt.Errorf("parse add request failed %v", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}
	klog.V(5).Infof("request body is %v", podRequest)

	if podRequest.DeviceID != "" {
		errMsg := fmt.Errorf("SR-IOV is not supported on Windows")
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if !csh.providerExists(podRequest.Provider) {
		errMsg := fmt.Errorf("provider %s not bind to any subnet", podRequest.Provider)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	klog.Infof("add port request %v", podRequest)
	var gatewayCheckMode int
	var macAddr, ip, cidr, gw, subnet, ingress, egress, priority string
	var isDefaultRoute bool = true
	var pod *v1.Pod
	var err error
	for i := 0; i < 15; i++ {
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

	if err := csh.createOrUpdateIPCr(podRequest, subnet, ip, macAddr); err != nil {
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if isDefaultRoute && pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podRequest.Provider)] != "true" {
		err := fmt.Errorf("route is not ready for pod %s/%s provider %s, please see kube-ovn-controller logs to find errors", pod.Namespace, pod.Name, podRequest.Provider)
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	var podNicName string
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

		if !podSubnet.Spec.DisableGatewayCheck {
			if podSubnet.Spec.Vlan != "" && !podSubnet.Spec.LogicalGateway {
				gatewayCheckMode = gatewayCheckModeArping
			} else {
				gatewayCheckMode = gatewayCheckModePing
			}
		}

		var mtu int = csh.Config.MTU
		klog.Infof("create container interface mac %s, ip %s, cidr %s, gw %s, custom routes %v", macAddr, ip, cidr, gw, podRequest.Routes)
		podNicName, err = csh.configureNic(podRequest.NetworkName, podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, macAddr, mtu, ip, cidr, gw, isDefaultRoute, podRequest.Routes, ingress, egress, priority, gatewayCheckMode)
		if err != nil {
			errMsg := fmt.Errorf("configure nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}

		// ifaceID := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
		// if err = ovs.ConfigInterfaceMirror(csh.Config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID); err != nil {
		// 	klog.Errorf("failed mirror to mirror0, %v", err)
		// 	return
		// }
	}

	if err := resp.WriteHeaderAndEntity(http.StatusOK, request.CniResponse{Protocol: util.CheckProtocol(cidr), IpAddress: ip, MacAddress: macAddr, CIDR: cidr, Gateway: gw, PodNicName: podNicName}); err != nil {
		klog.Errorf("failed to write response, %v", err)
	}
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
	if err != nil && !k8serrors.IsNotFound(err) {
		errMsg := fmt.Errorf("parse del request failed %v", err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if k8serrors.IsNotFound(err) {
		resp.WriteHeader(http.StatusNoContent)
		return
	}

	// check if it's a sriov device
	for _, container := range pod.Spec.Containers {
		if _, ok := container.Resources.Requests[util.SRIOVResourceName]; ok {
			podRequest.DeviceID = util.SRIOVResourceName
		}
	}

	klog.Infof("delete port request %v", podRequest)
	if pod.Annotations != nil && (podRequest.Provider == util.OvnProvider || podRequest.CniType == util.CniTypeName) {
		err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetworkName, podRequest.NetNs, podRequest.ContainerID)
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
