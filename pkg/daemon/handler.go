package daemon

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/request"
	"github.com/emicklei/go-restful"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"net/http"
	"strings"
	"time"
)

type CniServerHandler struct {
	KubeClient kubernetes.Interface
}

func createCniServerHandler(config *Configuration) (*CniServerHandler, error) {
	return &CniServerHandler{
		KubeClient: config.KubeClient,
	}, nil
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
	err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetNs, podRequest.ContainerID, macAddr, ipAddr)
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
