package controller

import (
	"fmt"

	v1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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

func (c *Controller) processNextCheckVpcResourceWorkItem() bool {
	obj, shutdown := c.checkVpcResourceQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.checkVpcResourceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.checkVpcResourceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleCheckVpcResource(key); err != nil {
			c.checkVpcResourceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.checkVpcResourceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) createCustomVpc(name, defaultLs string) (*Vpc, error) {
	klog.Infof("init custom vpc %s", name)
	vpc := &Vpc{
		Name:                   name,
		DefaultLogicalSwitch:   defaultLs,
		Router:                 fmt.Sprintf("vpc-%s", name),
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
	klog.Infof("delete custom vpc %s", name)
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
	return c.ovnClient.DeleteLogicalRouter(lr)
}

func (c *Controller) parseSubnetVpc(subnet *v1.Subnet) (*Vpc, bool) {
	var vpcName string
	var customVpc bool
	if 0 == len(subnet.Annotations) {
		vpcName = "default"
	} else {
		vpcName, customVpc = subnet.Annotations[util.CustomVpcAnnotation]
		if !customVpc {
			vpcName = "default"
		}
	}
	vpc, ok := c.vpcs.Load(vpcName)
	if !ok {
		klog.Infof("vpc %s not found", vpcName)
		return nil, false
	}
	return vpc.(*Vpc), true
}
