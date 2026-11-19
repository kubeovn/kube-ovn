package controller

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// reconcileServiceNodePorts handles Pod traffic on its source logical switch.
// External destinations do not imply external sources: ETP and destination-node
// topology must not restrict this Cluster backend set.
func (c *Controller) reconcileServiceNodePorts(ctx *endpointSliceReconcileContext) error {
	if ctx.lbReconciliation == nil {
		return errors.New("NodePort load balancers require atomic reconciliation")
	}
	svc := ctx.service
	var matches []string
	for _, port := range svc.Spec.Ports {
		if port.NodePort == 0 {
			continue
		}
		name := serviceScopedLBNameForTrafficClass(svc, port.Protocol, serviceLBNodePortTraffic)
		lb := ctx.lbReconciliation.desired[name]
		for _, address := range ctx.profile.nodePortVIPs {
			vip := util.JoinHostPort(address, port.NodePort)
			backends := c.getEndpointBackend(ctx.endpointSlices, port, address, false)
			if len(backends) != 0 {
				slices.Sort(backends)
				lb.VIPs[vip] = strings.Join(slices.Compact(backends), ",")
			}
			protocol := strings.ToLower(string(port.Protocol))
			family := "ip4"
			if util.CheckProtocol(address) == "IPv6" {
				family = "ip6"
			}
			matches = append(matches, fmt.Sprintf("(%s.dst == %s && %s.dst == %d)", family, address, protocol, port.NodePort))
		}
	}
	if len(matches) == 0 {
		return nil
	}
	// Router ingress bypasses switch LB conntrack in OVN. Localnet ingress
	// does not: reject direct external OVN NodePort ingress before it can reach
	// a router-attached NodePort LB and bypass kube-proxy's external Local
	// policy. Keep this guard on every enabled Service-LB switch, including
	// logical-gateway and U2O switches whose NodePort LB is router-only.
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, subnet := range subnets {
		if subnet.Name == c.config.NodeSwitch || !isOvnSubnet(subnet) || subnet.Spec.Vpc != ctx.vpcName || !subnetEnablesServiceLB(subnet, c.config.EnableOvnLB) {
			continue
		}
		ctx.lbReconciliation.batch.IngressGuards = append(ctx.lbReconciliation.batch.IngressGuards, ovs.LoadBalancerIngressGuard{
			LogicalSwitch: subnet.Name,
			Match:         fmt.Sprintf("inport == %q && (%s)", ovs.GetLocalnetName(subnet.Name), strings.Join(matches, " || ")),
		})
	}
	return nil
}

func (c *Controller) gcServiceNodePortIngressGuards(services []*v1.Service) error {
	client, ok := c.OVNNbClient.(interface {
		resourceLoadBalancerClient
		ListAcls(string, map[string]string) ([]ovnnb.ACL, error)
	})
	if !ok {
		return nil
	}
	active := make(map[string]struct{}, len(services))
	for _, svc := range services {
		if serviceUsesScopedLB(svc) {
			active[string(svc.UID)] = struct{}{}
		}
	}
	acls, err := client.ListAcls("", map[string]string{ovs.LoadBalancerIngressGuardExternalID: "true"})
	if err != nil {
		return err
	}
	for _, acl := range acls {
		uid := acl.ExternalIDs[serviceLBOwnerExternalID]
		if _, ok := active[uid]; ok {
			continue
		}
		// Recheck the lister: a Service can be created after the GC snapshot.
		namespace, name := acl.ExternalIDs[serviceLBNamespaceID], acl.ExternalIDs[serviceLBNameExternalID]
		if c.config.EnableOvnLB {
			live, err := c.servicesLister.Services(namespace).Get(name)
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
			if err == nil && string(live.UID) == uid && serviceUsesScopedLB(live) {
				continue
			}
		}
		if uid == "" || acl.ExternalIDs[serviceLBOwnerKindID] != serviceLBOwnerKind {
			continue
		}
		stale := &v1.Service{Namespace: namespace, Name: name, UID: types.UID(uid)}
		// Missed deletion must remove the LB and its ingress boundary together.
		if err := client.ReconcileLoadBalancers(newServiceLoadBalancerReconciliation(stale).batch); err != nil {
			return err
		}
	}
	return nil
}
