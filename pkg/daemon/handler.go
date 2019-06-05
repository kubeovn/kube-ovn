package daemon

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/emicklei/go-restful"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

type cniServerHandler struct {
	Config     *Configuration
	KubeClient kubernetes.Interface
}

func createCniServerHandler(config *Configuration) *cniServerHandler {
	csh := &cniServerHandler{KubeClient: config.KubeClient, Config: config}
	return csh
}

func (csh cniServerHandler) handleAdd(req *restful.Request, resp *restful.Response) {
	podRequest := request.PodRequest{}
	err := req.ReadEntity(&podRequest)
	if err != nil {
		errMsg := fmt.Errorf("parse add request failed %v", err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, request.PodResponse{Err: errMsg.Error()})
		return
	}
	klog.Infof("add port request %v", podRequest)
	var macAddr, ip, ipAddr, cidr, gw, ingress, egress string
	for i := 0; i < 10; i++ {
		pod, err := csh.KubeClient.CoreV1().Pods(podRequest.PodNamespace).Get(podRequest.PodName, v1.GetOptions{})
		if err != nil {
			errMsg := fmt.Errorf("get pod %s/%s failed %v", podRequest.PodNamespace, podRequest.PodName, err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
			return
		}
		if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
			klog.Errorf("validate pod %s/%s failed, %v", podRequest.PodNamespace, podRequest.PodName, err)
			// wait controller assign an address
			time.Sleep(2 * time.Second)
			continue
		}
		macAddr = pod.Annotations[util.MacAddressAnnotation]
		ip = pod.Annotations[util.IpAddressAnnotation]
		cidr = pod.Annotations[util.CidrAnnotation]
		gw = pod.Annotations[util.GatewayAnnotation]
		ingress = pod.Annotations[util.IngressRateAnnotation]
		egress = pod.Annotations[util.EgressRateAnnotation]
		break
	}

	if macAddr == "" || ip == "" || cidr == "" || gw == "" {
		errMsg := fmt.Errorf("no available ip for pod %s/%s", podRequest.PodNamespace, podRequest.PodName)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
		return
	}

	ipAddr = fmt.Sprintf("%s/%s", ip, strings.Split(cidr, "/")[1])
	klog.Infof("create container mac %s, ip %s, cidr %s, gw %s", macAddr, ipAddr, cidr, gw)
	err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetNs, podRequest.ContainerID, macAddr, ipAddr, gw, ingress, egress)
	if err != nil {
		errMsg := fmt.Errorf("configure nic failed %v", err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
		return
	}
	resp.WriteHeaderAndEntity(http.StatusOK, request.PodResponse{IpAddress: strings.Split(ipAddr, "/")[0], MacAddress: macAddr, CIDR: cidr, Gateway: gw})
}

func (csh cniServerHandler) handleDel(req *restful.Request, resp *restful.Response) {
	podRequest := request.PodRequest{}
	err := req.ReadEntity(&podRequest)
	if err != nil {
		errMsg := fmt.Errorf("parse del request failed %v", err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, request.PodResponse{Err: errMsg.Error()})
		return
	}
	klog.Infof("delete port request %v", podRequest)
	err = csh.deleteNic(podRequest.NetNs, podRequest.PodName, podRequest.PodNamespace, podRequest.ContainerID)
	if err != nil {
		errMsg := fmt.Errorf("del nic failed %v", err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
		return
	}
	resp.WriteHeader(http.StatusNoContent)
}
