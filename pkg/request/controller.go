package request

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/controller"
	"fmt"
	"github.com/parnurzeal/gorequest"
	"net/http"
)

type ControllerClient struct {
	ControllerAddress string
	*gorequest.SuperAgent
}

func NewControllerClient(controllerAddress string) ControllerClient {
	request := gorequest.New()
	return ControllerClient{
		SuperAgent:        request,
		ControllerAddress: controllerAddress,
	}
}

func (cc ControllerClient) AddPort(name, namespace string) (controller.AddPortResponse, error) {
	res := controller.AddPortResponse{}
	resp, output, errs := cc.Post("http://" + cc.ControllerAddress + "/api/v1/ports").
		Send(controller.CreatePortRequest{Name: fmt.Sprintf("%s.%s", name, namespace), Switch: namespace}).
		EndStruct(&res)
	if len(errs) != 0 {
		return res, errs[0]
	}
	if resp.StatusCode != http.StatusOK {
		return res, fmt.Errorf("add port failed %s", output)
	}

	return res, nil
}

func (cc ControllerClient) DelPort(name, namespace string) error {
	resp, output, errs := cc.Delete("http://" + cc.ControllerAddress + "/api/v1/ports/" + fmt.Sprintf("%s.%s", name, namespace)).End()
	if len(errs) != 0 {
		return errs[0]
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete port failed %s", output)
	}
	return nil
}
