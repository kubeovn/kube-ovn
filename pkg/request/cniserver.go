package request

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/parnurzeal/gorequest"
)

// CniServerClient is the client to visit cniserver
type CniServerClient struct {
	*gorequest.SuperAgent
}

// CniRequest is the cniserver request format
type CniRequest struct {
	PodName      string `json:"pod_name"`
	PodNamespace string `json:"pod_namespace"`
	ContainerID  string `json:"container_id"`
	NetNs        string `json:"net_ns"`
	Provider     string `json:"provider"`
}

// CniResponse is the cniserver response format
type CniResponse struct {
	Protocol   string `json:"protocol"`
	IpAddress  string `json:"address"`
	MacAddress string `json:"mac_address"`
	CIDR       string `json:"cidr"`
	Gateway    string `json:"gateway"`
	Mtu        int    `json:"mtu"`
	Err        string `json:"error"`
}

// NewCniServerClient return a new cniserver client
func NewCniServerClient(socketAddress string) CniServerClient {
	request := gorequest.New()
	request.Transport = &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", socketAddress)
	}}
	return CniServerClient{request}
}

// Add pod request
func (csc CniServerClient) Add(podRequest CniRequest) (*CniResponse, error) {
	resp := CniResponse{}
	res, _, errors := csc.Post("http://dummy/api/v1/add").Send(podRequest).EndStruct(&resp)
	if len(errors) != 0 {
		return nil, errors[0]
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("request ip return %d %s", res.StatusCode, resp.Err)
	}
	return &resp, nil
}

// Del pod request
func (csc CniServerClient) Del(podRequest CniRequest) error {
	res, body, errors := csc.Post("http://dummy/api/v1/del").Send(podRequest).End()
	if len(errors) != 0 {
		return errors[0]
	}
	if res.StatusCode != 204 {
		return fmt.Errorf("delete ip return %d %s", res.StatusCode, body)
	}
	return nil
}
