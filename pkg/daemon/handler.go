package daemon

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/alauda/kube-ovn/pkg/client/clientset/versioned"
	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/emicklei/go-restful"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

type cniServerHandler struct {
	Config        *Configuration
	KubeClient    kubernetes.Interface
	KubeOvnClient clientset.Interface
}

func createCniServerHandler(config *Configuration) *cniServerHandler {
	csh := &cniServerHandler{KubeClient: config.KubeClient, KubeOvnClient: config.KubeOvnClient, Config: config}
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
	var macAddr, ip, ipAddr, cidr, gw, subnet, ingress, egress string
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
			time.Sleep(1 * time.Second)
			continue
		}
		macAddr = pod.Annotations[util.MacAddressAnnotation]
		ip = pod.Annotations[util.IpAddressAnnotation]
		cidr = pod.Annotations[util.CidrAnnotation]
		gw = pod.Annotations[util.GatewayAnnotation]
		subnet = pod.Annotations[util.LogicalSwitchAnnotation]
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
	subnetCr, err := csh.KubeOvnClient.KubeovnV1().Subnets().Get(subnet, metav1.GetOptions{})
	if err != nil {
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: err.Error()})
		return
	}
	ipName := fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace)
	ipCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(ipName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		_, err := csh.KubeOvnClient.KubeovnV1().IPs().Create(&kubeovnv1.IP{
			ObjectMeta: v1.ObjectMeta{
				Name: ipName,
				Labels: map[string]string{
					util.SubnetNameLabel: subnet,
				},
			},
			Spec: kubeovnv1.IPSpec{
				PodName:     podRequest.PodName,
				Namespace:   podRequest.PodNamespace,
				Subnet:      subnet,
				NodeName:    csh.Config.NodeName,
				IPAddress:   ip,
				MacAddress:  macAddr,
				ContainerID: podRequest.ContainerID,
			},
		})
		if err != nil {
			errMsg := fmt.Errorf("failed to create ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
			return
		}
		subnetCr.Status.AcquireIP()
		csh.KubeOvnClient.KubeovnV1().Subnets().UpdateStatus(subnetCr)
	} else {
		if err != nil {
			if ipCr.Labels != nil {
				ipCr.Labels[util.SubnetNameLabel] = subnet
			} else {
				ipCr.Labels = map[string]string{
					util.SubnetNameLabel: subnet,
				}
			}
			ipCr.Spec.PodName = podRequest.PodName
			ipCr.Spec.Namespace = podRequest.PodNamespace
			ipCr.Spec.Subnet = subnet
			ipCr.Spec.NodeName = csh.Config.NodeName
			ipCr.Spec.IPAddress = ip
			ipCr.Spec.MacAddress = macAddr
			ipCr.Spec.ContainerID = podRequest.ContainerID
			_, err := csh.KubeOvnClient.KubeovnV1().IPs().Update(ipCr)
			if err != nil {
				errMsg := fmt.Errorf("failed to create ip crd for %s, %v", ip, err)
				klog.Error(errMsg)
				resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
				return
			}
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
			return
		}
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
	ipName := fmt.Sprintf("%s.%s", podRequest.PodName, podRequest.PodNamespace)
	ipCr, err := csh.KubeOvnClient.KubeovnV1().IPs().Get(ipName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			resp.WriteHeader(http.StatusNoContent)
		} else {
			resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: err.Error()})
			return
		}
	}
	err = csh.KubeOvnClient.KubeovnV1().IPs().Delete(ipName, &metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		errMsg := fmt.Errorf("del ipcrd for %s failed %v", ipName, err)
		klog.Error(errMsg)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: errMsg.Error()})
		return
	}
	subnet, err := csh.KubeOvnClient.KubeovnV1().Subnets().Get(ipCr.Spec.Subnet, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, request.PodResponse{Err: err.Error()})
		return
	}
	subnet.Status.ReleaseIP()
	csh.KubeOvnClient.KubeovnV1().Subnets().UpdateStatus(subnet)
	resp.WriteHeader(http.StatusNoContent)
}
