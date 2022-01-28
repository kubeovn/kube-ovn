package ovs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c Client) AddLbToLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error {
	if err := c.addLoadBalancerToLogicalSwitch(tcpLb, ls); err != nil {
		klog.Errorf("failed to add tcp lb to %s, %v", ls, err)
		return err
	}

	if err := c.addLoadBalancerToLogicalSwitch(udpLb, ls); err != nil {
		klog.Errorf("failed to add udp lb to %s, %v", ls, err)
		return err
	}

	if err := c.addLoadBalancerToLogicalSwitch(tcpSessLb, ls); err != nil {
		klog.Errorf("failed to add tcp session lb to %s, %v", ls, err)
		return err
	}

	if err := c.addLoadBalancerToLogicalSwitch(udpSessLb, ls); err != nil {
		klog.Errorf("failed to add udp session lb to %s, %v", ls, err)
		return err
	}

	return nil
}

func (c Client) RemoveLbFromLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error {
	if err := c.removeLoadBalancerFromLogicalSwitch(tcpLb, ls); err != nil {
		klog.Errorf("failed to remove tcp lb from %s, %v", ls, err)
		return err
	}

	if err := c.removeLoadBalancerFromLogicalSwitch(udpLb, ls); err != nil {
		klog.Errorf("failed to remove udp lb from %s, %v", ls, err)
		return err
	}

	if err := c.removeLoadBalancerFromLogicalSwitch(tcpSessLb, ls); err != nil {
		klog.Errorf("failed to remove tcp session lb from %s, %v", ls, err)
		return err
	}

	if err := c.removeLoadBalancerFromLogicalSwitch(udpSessLb, ls); err != nil {
		klog.Errorf("failed to remove udp session lb from %s, %v", ls, err)
		return err
	}

	return nil
}

// DeleteLoadBalancer delete loadbalancer in ovn
func (c Client) DeleteLoadBalancer(names ...string) error {
	lbList := make([]ovnnb.LoadBalancer, 0, 1)
	if err := c.nbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool { return util.ContainsString(names, lb.Name) }).List(context.TODO(), &lbList); err != nil {
		return fmt.Errorf("failed to list load balancer: %v", err)
	}

	var ops []ovsdb.Operation
	for _, lb := range lbList {
		delOps, err := c.nbClient.Where(&lb).Delete()
		if err != nil {
			return fmt.Errorf("failed to generate delete operations for load balancer %s: %v", lb.Name, err)
		}
		ops = append(ops, delOps...)
	}
	if len(ops) == 0 {
		return nil
	}

	if err := Transact(c.nbClient, "destroy", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete load balancers %s: %v", strings.Join(names, ","), err)
	}

	return nil
}

// ListLoadBalancer list loadbalancer names
func (c Client) ListLoadBalancer() ([]string, error) {
	lbList := make([]ovnnb.LoadBalancer, 0)
	if err := c.nbClient.List(context.TODO(), &lbList); err != nil {
		klog.Errorf("failed to list load balancer: %v", err)
		return nil, err
	}

	result := make([]string, 0, len(lbList))
	for _, lb := range lbList {
		result = append(result, lb.Name)
	}
	return result, nil
}

// FindLoadbalancer find ovn loadbalancer uuid by name
func (c Client) FindLoadbalancer(name string) (string, error) {
	lbList := make([]ovnnb.LoadBalancer, 0, 1)
	if err := c.nbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == name }).List(context.TODO(), &lbList); err != nil {
		return "", fmt.Errorf("failed to list load balancer with name %s: %v", name, err)
	}
	switch len(lbList) {
	case 0:
		return "", nil
	case 1:
		return lbList[0].UUID, nil
	default:
		return "", fmt.Errorf("found multiple load balancers with the same name %s", name)
	}
}

func (c Client) GetLoadBalancer(name string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error) {
	lbList := make([]ovnnb.LoadBalancer, 0, 1)
	if err := c.nbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == name }).List(context.TODO(), &lbList); err != nil {
		return nil, fmt.Errorf("failed to list load balancer with name %s: %v", name, err)
	}
	switch len(lbList) {
	case 0:
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("load balancer %s does not exist", name)
	case 1:
		return &lbList[0], nil
	default:
		return nil, fmt.Errorf("found multiple load balancers with the same name %s", name)
	}
}

// CreateLoadBalancer create loadbalancer in ovn
func (c Client) CreateLoadBalancer(name string, protocol ovnnb.LoadBalancerProtocol, selectFields ...ovnnb.LoadBalancerSelectionFields) error {
	lb := &ovnnb.LoadBalancer{Name: name, Protocol: &protocol, SelectionFields: selectFields}
	ops, err := c.nbClient.Create(lb)
	if err != nil {
		return fmt.Errorf("failed to generate create operations for load balancer %s: %v", name, err)
	}

	if err = Transact(c.nbClient, "create", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create load balancer %s: %v", name, err)
	}

	return nil
}

// CreateLoadBalancerRule create loadbalancer rul in ovn
func (c Client) CreateLoadBalancerRule(lb, vip, ips, protocol string) error {
	_, err := c.ovnNbCommand(MayExist, "lb-add", lb, vip, ips, strings.ToLower(protocol))
	return err
}

func (c Client) addLoadBalancerToLogicalSwitch(lb, ls string) error {
	_, err := c.ovnNbCommand(MayExist, "ls-lb-add", ls, lb)
	return err
}

func (c Client) removeLoadBalancerFromLogicalSwitch(lb, ls string) error {
	if lb == "" {
		return nil
	}
	_lb, err := c.GetLoadBalancer(lb, true)
	if err != nil {
		return err
	}
	if _lb == nil {
		return nil
	}

	_, err = c.ovnNbCommand(IfExists, "ls-lb-del", ls, lb)
	return err
}

// DeleteLoadBalancerVip delete a vip rule from loadbalancer
func (c Client) DeleteLoadBalancerVip(vip, lb string) error {
	_lb, err := c.GetLoadBalancer(lb, false)
	if err != nil {
		return err
	}
	// vip is empty or delete last rule will destroy the loadbalancer
	if len(_lb.Vips) == 0 || len(_lb.Vips) == 1 {
		return nil
	}
	if _, ok := _lb.Vips[vip]; !ok {
		return nil
	}

	delete(_lb.Vips, vip)
	ops, err := c.nbClient.Where(_lb).Update(_lb, &_lb.Vips)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for load balancer %s: %v", lb, err)
	}
	if err = Transact(c.nbClient, "lb-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to update load balancer %s: %v", lb, err)
	}

	return nil
}

// GetLoadBalancerVips return vips of a loadbalancer
func (c Client) GetLoadBalancerVips(lb string) (map[string]string, error) {
	output, err := c.ovnNbCommand("--data=bare", "--no-heading",
		"get", "load_balancer", lb, "vips")
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	err = json.Unmarshal([]byte(strings.Replace(output, "=", ":", -1)), &result)
	return result, err
}
