package controller

import (
	"fmt"

	v1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	"k8s.io/klog"
)

type Vpc struct {
	Default                bool
	Name                   string
	DefaultLogicalSwitch   string
	Router                 string
	TcpLoadBalancer        string
	UdpLoadBalancer        string
	TcpSessionLoadBalancer string
	UdpSessionLoadBalancer string
}

func (c *Controller) initCustomVpc(name, defaultLs string) (*Vpc, error) {
	vpc := &Vpc{
		Name:                   name,
		DefaultLogicalSwitch:   defaultLs,
		Router:                 fmt.Sprintf("ovn-vpc-%s", name),
		TcpLoadBalancer:        fmt.Sprintf("vpc-%s-tcp-load", name),
		UdpLoadBalancer:        fmt.Sprintf("vpc-%s-udp-load", name),
		TcpSessionLoadBalancer: fmt.Sprintf("vpc-%s-tcp-sess", name),
		UdpSessionLoadBalancer: fmt.Sprintf("vpc-%s-udp-sess", name),
	}

	if err := c.createVpcRouter(vpc.Router); err != nil {
		klog.Errorf("init router failed %v", err)
		return nil, err
	}
	c.vpcs.Store(name, vpc)
	return vpc, nil
}

func (c *Controller) deleteCustomVpc(name string) error {
	vpcObj, ok := c.vpcs.Load(name)
	if !ok {
		err := fmt.Errorf("vpc '%s' not found", name)
		klog.Error(err)
		return err
	}
	vpc := vpcObj.(*Vpc)
	err := c.deleteVpcRouter(vpc.Router)
	if err != nil {
		klog.Errorf("delete router failed %v", err)
		return err
	}
	c.vpcs.Delete(vpc.Name)
	return nil
}

// createVpcRouter create router to connect logical switches in vpc
func (c *Controller) createVpcRouter(lr string) error {
	lrs, err := c.ovnClient.ListLogicalRouter()
	if err != nil {
		return err
	}
	klog.Infof("exists routers %v", lrs)
	for _, r := range lrs {
		if lr == r {
			return nil
		}
	}
	return c.ovnClient.CreateLogicalRouter(lr)
}

// deleteVpcRouter delete router to connect logical switches in vpc
func (c *Controller) deleteVpcRouter(lr string) error {
	lrs, err := c.ovnClient.ListLogicalRouter()
	if err != nil {
		return err
	}
	klog.Infof("exists routers %v", lrs)
	for _, r := range lrs {
		if lr == r {
			return nil
		}
	}
	return c.ovnClient.DeleteLogicalRouter(lr)
}

func (c *Controller) parseSubnetVpc(subnet *v1.Subnet) (*Vpc, error) {
	vpcName, customVpc := subnet.Annotations[util.CustomVpcAnnotation]
	if !customVpc {
		vpcName = "default"
	}

	vpc, ok := c.vpcs.Load(vpcName)
	if !ok {
		err := fmt.Errorf("vpc %s not found", vpcName)
		klog.Error(err)
		return nil, err
	}
	return vpc.(*Vpc), nil
}
