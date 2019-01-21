package daemon

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/request"
	"github.com/emicklei/go-restful"
	"github.com/oilbeater/libovsdb"
	"k8s.io/klog"
	"net/http"
	"strings"
)

type CniServerHandler struct {
	ControllerClient request.ControllerClient
	OvsClient        *libovsdb.OvsdbClient
}

func createCniServerHandler(config *Configuration) (*CniServerHandler, error) {
	cc := request.NewControllerClient(config.ControllerAddress)
	ovs, err := libovsdb.ConnectWithUnixSocket(config.OvsSocket)
	if err != nil {
		return nil, err
	}
	return &CniServerHandler{
		ControllerClient: cc,
		OvsClient:        ovs,
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
	res, err := csh.ControllerClient.AddPort(podRequest.PodName, podRequest.PodNamespace)
	if err != nil {
		klog.Errorf("add port request to controller failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	// TODO
	cidr := strings.Split(res.CIDR, "/")[1]

	err = csh.configureNic(podRequest.PodName, podRequest.PodNamespace, podRequest.NetNs, podRequest.ContainerID, res.MacAddress, res.IpAddress+"/"+cidr)
	if err != nil {
		klog.Errorf("configure nic failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	resp.WriteHeaderAndEntity(http.StatusOK, request.PodResponse{IpAddress: res.IpAddress, MacAddress: res.MacAddress, CIDR: res.CIDR, Gateway: res.Gateway})

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
	err = csh.ControllerClient.DelPort(podRequest.PodName, podRequest.PodNamespace)
	if err != nil {
		klog.Errorf("del port request to controller failed %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	resp.WriteHeader(http.StatusNoContent)
}
