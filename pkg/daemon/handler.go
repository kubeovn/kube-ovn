package daemon

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emicklei/go-restful"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/alauda/kube-ovn/pkg/client/clientset/versioned"
	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/alauda/kube-ovn/pkg/util"
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

	klog.Infof("add port request %v", podRequest)
	var macAddr, ip, cidr, gw, subnet, ingress, egress, vlanID string
	var ipDual, cidrDual, gwDual kubeovnv1.DualStack
	var ipAddrDual = kubeovnv1.DualStack{}
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
			klog.Infof("wait address for  pod %s/%s ", podRequest.PodNamespace, podRequest.PodName)
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
		ingress = pod.Annotations[util.IngressRateAnnotation]
		egress = pod.Annotations[util.EgressRateAnnotation]
		vlanID = pod.Annotations[util.VlanIdAnnotation]
		ipDual, _ = util.StringToDualStack(ip)
		cidrDual, _ = util.StringToDualStack(cidr)
		gwDual, _ = util.StringToDualStack(gw)

		if len(ipDual) != len(cidrDual) || len(ipDual) != len(gwDual) {
			errMsg := fmt.Errorf("pod address annotation %s/%s invalid", podRequest.PodNamespace, podRequest.PodName)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
		for proto, i := range ipDual {
			ipAddrDual[proto] = fmt.Sprintf("%s/%s", i, strings.Split(cidrDual[proto], "/")[1])
		}
		break
	}

	if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podRequest.Provider)] != "true" {
		err := fmt.Errorf("no address allocated to pod %s/%s, please see kube-ovn-controller logs to find errors", pod.Namespace, pod.Name)
		klog.Error(err)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if err := csh.createOrUpdateIPCr(podRequest, subnet, macAddr, ipDual); err != nil {
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: err.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	if podRequest.Provider == util.OvnProvider {
		klog.Infof("create container mac %s, ip %s, cidr %s, gw %s", macAddr, ipAddrDual, cidrDual, gwDual)
		err := csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetNs, podRequest.ContainerID, macAddr, ipAddrDual, gwDual, ingress, egress, vlanID)
		if err != nil {
			errMsg := fmt.Errorf("configure nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	if err := resp.WriteHeaderAndEntity(http.StatusOK, request.CniResponse{Protocol: util.CheckProtocolDual(ipDual), IpAddress: ipDual, MacAddress: macAddr, CIDR: cidrDual, Gateway: gwDual}); err != nil {
		klog.Errorf("failed to write response, %v", err)
	}
}

func (csh cniServerHandler) createOrUpdateIPCr(podRequest request.CniRequest, subnet, macAddr string, ipDual kubeovnv1.DualStack) error {
	ipCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := csh.KubeOvnClient.KubeovnV1().IPs().Create(&kubeovnv1.IP{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace),
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
					IPAddress:     ipDual,
					MacAddress:    macAddr,
					ContainerID:   podRequest.ContainerID,
					AttachIPs:     []string{},
					AttachMacs:    []string{},
					AttachSubnets: []string{},
				},
			})
			if err != nil {
				errMsg := fmt.Errorf("failed to create ip crd for %s, %v", ipDual, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ipDual, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		ipCr.Labels[subnet] = ""
		ipCr.Spec.AttachSubnets = append(ipCr.Spec.AttachSubnets, strings.Split(subnet, ",")...)
		ipCr.Spec.AttachIPs = append(ipCr.Spec.AttachIPs, strings.Split(util.DualStackToString(ipDual), ",")...)
		ipCr.Spec.AttachSubnets = append(ipCr.Spec.AttachSubnets, subnet)
		if _, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(ipCr); err != nil {
			errMsg := fmt.Errorf("failed to update ip crd for %s, %v", ipDual, err)
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
	// check if it's a sriov device
	if pod != nil {
		for _, container := range pod.Spec.Containers {
			if _, ok := container.Resources.Requests[util.SRIOVResourceName]; ok {
				podRequest.DeviceID = util.SRIOVResourceName
			}
		}
	}

	klog.Infof("delete port request %v", podRequest)
	if podRequest.Provider == util.OvnProvider {
		err = csh.deleteNic(podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID, podRequest.DeviceID)
		if err != nil {
			errMsg := fmt.Errorf("del nic failed %v", err)
			klog.Error(errMsg)
			if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
				klog.Errorf("failed to write response, %v", err)
			}
			return
		}
	}

	err = csh.KubeOvnClient.KubeovnV1().IPs().Delete(fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace), &metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		errMsg := fmt.Errorf("del ipcrd for %s failed %v", fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace), err)
		klog.Error(errMsg)
		if err := resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.CniResponse{Err: errMsg.Error()}); err != nil {
			klog.Errorf("failed to write response, %v", err)
		}
		return
	}

	resp.WriteHeader(http.StatusNoContent)
}
