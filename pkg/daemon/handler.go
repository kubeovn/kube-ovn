package daemon

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/emicklei/go-restful"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

	if exist := csh.providerExists(podRequest.Provider); !exist {
		errMsg := fmt.Errorf("provider %s not bind to any subnet", podRequest.Provider)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusBadRequest, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	klog.Infof("add port request %v", podRequest)
	var macAddr, ip, ipAddr, cidr, gw, subnet, ingress, egress, vlanID, ifName, nicType, netns string
	var pod *v1.Pod
	var err error
	for i := 0; i < 15; i++ {
		pod, err = csh.Controller.podsLister.Pods(podRequest.PodNamespace).Get(podRequest.PodName)
		if err != nil {
			errMsg := fmt.Errorf("get pod %s/%s failed %v", podRequest.PodNamespace, podRequest.PodName, err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podRequest.Provider)] != "true" {
			klog.Infof("wait address for pod %s/%s ", podRequest.PodNamespace, podRequest.PodName)
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
		vlanID = pod.Annotations[fmt.Sprintf(util.VlanIdAnnotationTemplate, podRequest.Provider)]
		ipAddr = util.GetIpAddrWithMask(ip, cidr)
		ifName = podRequest.IfName
		nicType = pod.Annotations[util.PodNicAnnotation]
		netns = podRequest.NetNs
		break
	}

	if ifName == "" {
		ifName = "eth0"
	}
	if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podRequest.Provider)] != "true" {
		err := fmt.Errorf("no address allocated to pod %s/%s, please see kube-ovn-controller logs to find errors", pod.Namespace, pod.Name)
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

	if strings.HasSuffix(podRequest.Provider, util.OvnProvider) && subnet != "" {
		klog.Infof("create container interface %s mac %s, ip %s, cidr %s, gw %s", ifName, macAddr, ipAddr, cidr, gw)
		nsArray := strings.Split(netns, "/")
		podNetns := nsArray[len(nsArray)-1]
		if nicType == util.InternalType {
			err = csh.configureNicWithInternalPort(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, ipAddr, gw, ingress, egress, vlanID, podRequest.DeviceID, nicType, podNetns)
		} else {
			err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider, podRequest.NetNs, podRequest.ContainerID, ifName, macAddr, ipAddr, gw, ingress, egress, vlanID, podRequest.DeviceID, nicType, podNetns)
		}
		if err != nil {
			errMsg := fmt.Errorf("configure nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}

		if err = csh.Controller.addEgressConfig(subnet, ip); err != nil {
			errMsg := fmt.Errorf("failed to add egress configuration: %v", err)
			klog.Error(errMsg)
			if err = resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	if err := resp.WriteHeaderAndEntity(http.StatusOK, request.CniResponse{Protocol: util.CheckProtocol(cidr), IpAddress: ip, MacAddress: macAddr, CIDR: cidr, Gateway: gw}); err != nil {
		klog.Errorf("failed to write response, %v", err)
	}
}

func (csh cniServerHandler) createOrUpdateIPCr(podRequest request.CniRequest, subnet, ip, macAddr string) error {
	v4IP, v6IP := util.SplitStringIP(ip)
	ipCrName := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
	ipCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipCrName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := csh.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
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
			}
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		ipCr.Spec.AttachIPs = append(ipCr.Spec.AttachIPs, strings.Split(ip, ",")...)
		ipCr.Labels[subnet] = ""
		ipCr.Spec.AttachSubnets = append(ipCr.Spec.AttachSubnets, subnet)
		ipCr.Spec.AttachMacs = append(ipCr.Spec.AttachMacs, macAddr)
		if _, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ipCr, metav1.UpdateOptions{}); err != nil {
			errMsg := fmt.Errorf("failed to update ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			return errMsg
		}
	}
	return nil
}

func (csh cniServerHandler) handleDel(req *restful.Request, resp *restful.Response) {
	podRequest := request.CniRequest{}
	err := req.ReadEntity(&podRequest)

	if err != nil {
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

		nicType := pod.Annotations[util.PodNicAnnotation]
		err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID, podRequest.DeviceID, podRequest.IfName, nicType)
		if err != nil {
			errMsg := fmt.Errorf("del nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	ipCrName := ovs.PodNameToPortName(podRequest.PodName, podRequest.PodNamespace, podRequest.Provider)
	err = csh.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), ipCrName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		errMsg := fmt.Errorf("del ip crd for %s failed %v", ipCrName, err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	resp.WriteHeader(http.StatusNoContent)
}
