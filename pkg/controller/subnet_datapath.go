package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

// ovn-northd allocates a tunnel key for every datapath (logical switch or
// router) and for every port of a datapath. When the key space is exhausted
// (4095 datapaths and 2047 ports per datapath with VXLAN encapsulation) it
// only logs "all datapath/port tunnel ids exhausted" and leaves the datapath
// or port out of the southbound database. The NB objects created by
// kube-ovn-controller look fine, so the Subnet used to report Ready although
// it carried no traffic. The periodic check below compares the NB objects of
// every OVN subnet with their SB bindings and reports missing ones on the
// Subnet's Ready condition and as a Warning event.
const (
	subnetDatapathCheckInterval = 10 * time.Second
	// a binding has to be missing on this many consecutive checks before the
	// subnet is reported, so that ovn-northd has time to process new NB rows.
	subnetDatapathMissThreshold = 2

	reasonDatapathNotBound   = "DatapathNotBound"
	reasonRouterPortNotBound = "RouterPortNotBound"
	reasonDatapathBound      = "DatapathBound"
)

type subnetDatapathIssue struct {
	reason  string
	message string
}

// subnetDatapathIssues keeps the consecutive miss count of each subnet and the
// subnets that are currently reported as not bound. The zero value is ready
// for use.
type subnetDatapathIssues struct {
	mu      sync.Mutex
	misses  map[string]int
	reports map[string]subnetDatapathIssue
}

// observe records the result of one check for a subnet. It returns the issue
// to report (nil when the subnet is fine or the miss is still within the
// grace period).
func (s *subnetDatapathIssues) observe(subnet string, issue *subnetDatapathIssue) *subnetDatapathIssue {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.misses == nil {
		s.misses = map[string]int{}
		s.reports = map[string]subnetDatapathIssue{}
	}
	if issue == nil {
		delete(s.misses, subnet)
		delete(s.reports, subnet)
		return nil
	}
	s.misses[subnet]++
	if s.misses[subnet] < subnetDatapathMissThreshold {
		return nil
	}
	s.reports[subnet] = *issue
	return issue
}

// get returns the issue currently reported for a subnet, if any.
func (s *subnetDatapathIssues) get(subnet string) (subnetDatapathIssue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	issue, ok := s.reports[subnet]
	return issue, ok
}

// forget drops the state of subnets that no longer exist.
func (s *subnetDatapathIssues) forget(existing map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name := range s.misses {
		if _, ok := existing[name]; !ok {
			delete(s.misses, name)
		}
	}
	for name := range s.reports {
		if _, ok := existing[name]; !ok {
			delete(s.reports, name)
		}
	}
}

// isLogicalSwitchDatapath reports whether a SB Datapath_Binding row belongs to
// a logical switch. OVN 26.03 and later use the type column, older releases
// store the NB UUID in external_ids:logical-switch.
func isLogicalSwitchDatapath(dp *ovnsb.DatapathBinding) bool {
	if dp.Type != nil {
		return *dp.Type == ovnsb.DatapathBindingTypeLogicalSwitch
	}
	return dp.ExternalIDs["logical-switch"] != ""
}

func (c *Controller) checkSubnetDatapathBindings() {
	if err := c.checkSubnetDatapathBindingsWithContext(context.Background()); err != nil {
		klog.Errorf("failed to check southbound bindings of subnets: %v", err)
	}
}

func (c *Controller) checkSubnetDatapathBindingsWithContext(ctx context.Context) error {
	if c.OVNSbTables == nil {
		return nil
	}
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list subnets: %w", err)
	}
	existing := make(map[string]struct{}, len(subnets))
	for _, subnet := range subnets {
		existing[subnet.Name] = struct{}{}
	}
	c.subnetDatapathIssues.forget(existing)

	nbSwitches, err := c.listLogicalSwitchNames(false, nil)
	if err != nil {
		return fmt.Errorf("list logical switches: %w", err)
	}
	nbSwitchSet := make(map[string]struct{}, len(nbSwitches))
	for _, name := range nbSwitches {
		nbSwitchSet[name] = struct{}{}
	}

	datapaths, err := c.listOn(c.OVNSbTables, &ovnsb.DatapathBinding{},
		func() ([]ovnsb.DatapathBinding, error) { return nil, errors.New("OVN SB table provider is nil") },
		isLogicalSwitchDatapath)
	if err != nil {
		return fmt.Errorf("list southbound datapath bindings: %w", err)
	}
	boundSwitches := make(map[string]struct{}, len(datapaths))
	for _, dp := range datapaths {
		if name := dp.ExternalIDs["name"]; name != "" {
			boundSwitches[name] = struct{}{}
		}
	}

	// router ports kube-ovn created for the subnets, keyed by subnet name
	routerPorts := make(map[string]string, len(subnets))
	for _, subnet := range subnets {
		if router := c.subnetRouterName(subnet); router != "" {
			routerPorts[subnet.Name] = ovs.LogicalRouterPortName(router, subnet.Name)
		}
	}
	wantedPorts := make(map[string]struct{}, len(routerPorts))
	for _, name := range routerPorts {
		wantedPorts[name] = struct{}{}
	}
	nbRouterPorts, err := c.listFiltered(&ovnnb.LogicalRouterPort{},
		func() ([]ovnnb.LogicalRouterPort, error) {
			return c.OVNNbClient.ListLogicalRouterPorts(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
				_, ok := wantedPorts[lrp.Name]
				return ok
			})
		},
		func(lrp *ovnnb.LogicalRouterPort) bool {
			_, ok := wantedPorts[lrp.Name]
			return ok
		})
	if err != nil {
		return fmt.Errorf("list logical router ports: %w", err)
	}
	nbRouterPortSet := make(map[string]struct{}, len(nbRouterPorts))
	for _, lrp := range nbRouterPorts {
		nbRouterPortSet[lrp.Name] = struct{}{}
	}
	portBindings, err := c.listOn(c.OVNSbTables, &ovnsb.PortBinding{},
		func() ([]ovnsb.PortBinding, error) { return nil, errors.New("OVN SB table provider is nil") },
		func(pb *ovnsb.PortBinding) bool {
			_, ok := wantedPorts[pb.LogicalPort]
			return ok
		})
	if err != nil {
		return fmt.Errorf("list southbound port bindings: %w", err)
	}
	boundPorts := make(map[string]struct{}, len(portBindings))
	for _, pb := range portBindings {
		boundPorts[pb.LogicalPort] = struct{}{}
	}

	maxTunID := c.datapathMaxTunnelID()
	for _, subnet := range subnets {
		if !isOvnSubnet(subnet) || !subnet.DeletionTimestamp.IsZero() || !subnet.Status.IsValidated() {
			continue
		}
		if _, ok := nbSwitchSet[subnet.Name]; !ok {
			// not created yet, nothing to compare
			continue
		}
		var issue *subnetDatapathIssue
		if _, ok := boundSwitches[subnet.Name]; !ok {
			issue = &subnetDatapathIssue{
				reason: reasonDatapathNotBound,
				message: fmt.Sprintf("ovn-northd did not bind logical switch %s to a southbound datapath, so the subnet carries no traffic. "+
					"The datapath tunnel keys are probably exhausted (max_tunid=%s; VXLAN encapsulation allows 4095 datapaths, Geneve about 16 million). "+
					"Look for %q in the ovn-northd log", subnet.Name, maxTunID, "all datapath tunnel ids exhausted"),
			}
		} else if lrp := routerPorts[subnet.Name]; lrp != "" {
			if _, inNB := nbRouterPortSet[lrp]; inNB {
				if _, bound := boundPorts[lrp]; !bound {
					issue = &subnetDatapathIssue{
						reason: reasonRouterPortNotBound,
						message: fmt.Sprintf("ovn-northd did not bind logical router port %s to the southbound database, so the subnet is not routed. "+
							"The port tunnel keys of the router are probably exhausted (VXLAN encapsulation allows 2047 ports per router, Geneve 32767). "+
							"Look for %q in the ovn-northd log", lrp, "all port tunnel ids exhausted"),
					}
				}
			}
		}

		reported := c.subnetDatapathIssues.observe(subnet.Name, issue)
		if err := c.syncSubnetDatapathCondition(ctx, subnet, issue, reported); err != nil {
			klog.Errorf("failed to update datapath binding condition of subnet %s: %v", subnet.Name, err)
		}
	}
	return nil
}

// syncSubnetDatapathCondition sets the Ready condition of a subnet whose
// binding is reported missing, and restores it once the binding shows up.
func (c *Controller) syncSubnetDatapathCondition(ctx context.Context, cached *kubeovnv1.Subnet, issue, reported *subnetDatapathIssue) error {
	ready := cached.Status.GetCondition(kubeovnv1.Ready)
	switch {
	case reported != nil:
		if ready != nil && ready.Status == v1.ConditionFalse && ready.Reason == reported.reason && ready.Message == reported.message {
			return nil
		}
		subnet := cached.DeepCopy()
		subnet.Status.NotReady(reported.reason, reported.message)
		if err := c.patchSubnetConditions(ctx, subnet); err != nil {
			return err
		}
		klog.Warningf("subnet %s: %s", subnet.Name, reported.message)
		c.recordResourceEvent(subnet, v1.EventTypeWarning, reported.reason, reported.message)
	case issue == nil:
		if ready == nil || ready.Status != v1.ConditionFalse ||
			(ready.Reason != reasonDatapathNotBound && ready.Reason != reasonRouterPortNotBound) {
			return nil
		}
		subnet := cached.DeepCopy()
		subnet.Status.Ready(reasonDatapathBound, "")
		if err := c.patchSubnetConditions(ctx, subnet); err != nil {
			return err
		}
		c.recordResourceEvent(subnet, v1.EventTypeNormal, reasonDatapathBound,
			fmt.Sprintf("Subnet %s is bound to a southbound datapath", subnet.Name))
	}
	return nil
}

func (c *Controller) patchSubnetConditions(ctx context.Context, subnet *kubeovnv1.Subnet) error {
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}
	bytes, err := subnet.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(ctx, subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	return err
}

// subnetRouterName returns the logical router a subnet is attached to.
func (c *Controller) subnetRouterName(subnet *kubeovnv1.Subnet) string {
	vpcName := subnet.Spec.Vpc
	if vpcName == "" {
		vpcName = c.config.ClusterRouter
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil || vpc.Status.Router == "" {
		return ""
	}
	return vpc.Status.Router
}

// datapathMaxTunnelID returns the datapath tunnel key limit that ovn-northd
// publishes in NB_Global options:max_tunid, or "unknown".
func (c *Controller) datapathMaxTunnelID() string {
	rows, err := c.listFiltered(&ovnnb.NBGlobal{},
		func() ([]ovnnb.NBGlobal, error) {
			nbGlobal, err := c.OVNNbClient.GetNbGlobal()
			if err != nil || nbGlobal == nil {
				return nil, err
			}
			return []ovnnb.NBGlobal{*nbGlobal}, nil
		},
		func(*ovnnb.NBGlobal) bool { return true })
	if err != nil || len(rows) == 0 || rows[0].Options["max_tunid"] == "" {
		return "unknown"
	}
	return rows[0].Options["max_tunid"]
}
