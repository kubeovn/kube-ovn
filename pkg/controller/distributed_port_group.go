package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type distributedPortOwner struct {
	pod      *v1.Pod
	provider string
	retained *kubeovnv1.IP
}

// Real table providers can expose authoritative membership transactions;
// legacy mocks continue to use the narrow NB client methods.
type distributedPortGroupClient interface {
	GetSubnetPortGroupMembership(context.Context, string) ([]ovnnb.PortGroup, []ovnnb.LogicalSwitchPort, error)
	ApplyPortGroupMembershipChanges(context.Context, []ovs.PortGroupMembershipChange, []string) error
	MovePortToPortGroup(context.Context, string, string, map[string]string, ...string) error
}

func (c *Controller) distributedPortGroups() distributedPortGroupClient {
	if client, ok := c.OVNNbTables.(distributedPortGroupClient); ok {
		return client
	}
	return c.OVNNbClient
}

func (c *Controller) isDistributedSubnet(subnet *kubeovnv1.Subnet) bool {
	return subnet.DeletionTimestamp.IsZero() && subnet.Spec.Vpc == c.config.ClusterRouter &&
		subnet.Name != c.config.NodeSwitch &&
		subnet.Spec.GatewayType == kubeovnv1.GWDistributedType &&
		(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway)
}

func isDistributedSubnetPortGroup(group ovnnb.PortGroup, subnet string) bool {
	node := group.ExternalIDs["node"]
	vendor := group.ExternalIDs["vendor"]
	return node != "" && (vendor == "" || vendor == util.CniTypeName) &&
		group.ExternalIDs["subnet"] == subnet &&
		group.ExternalIDs[networkPolicyKey] == subnet+"/"+node &&
		group.Name == getOverlaySubnetsPortGroupName(subnet, node)
}

// This sweep must not depend on node events: a lost PG membership does not
// change the Pod's routed annotation and is invisible to LSP inspection.
func (c *Controller) syncDistributedSubnetPortGroups(ctx context.Context) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets for port group reconciliation: %v", err)
		return
	}
	for _, subnet := range subnets {
		if ctx.Err() != nil {
			return
		}
		if !c.isDistributedSubnet(subnet) {
			continue
		}
		if controllerRuntimeSubnetWriterEnabled(c.config) {
			c.enqueueSubnetReconcile(subnet.Name)
			continue
		}
		if err := c.syncDistributedSubnetPortGroup(ctx, subnet.Name); err != nil {
			klog.Errorf("failed to reconcile subnet %s port groups: %v", subnet.Name, err)
			// The subnet worker also recreates missing groups and their routes.
			c.addOrUpdateSubnetQueue.AddRateLimited(subnet.Name)
		}
	}
}

func (c *Controller) syncDistributedSubnetPortGroup(ctx context.Context, name string) error {
	c.subnetKeyMutex.LockKey(name)
	defer func() { _ = c.subnetKeyMutex.UnlockKey(name) }()

	subnet, err := c.subnetsLister.Get(name)
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !c.isDistributedSubnet(subnet) {
		return nil
	}
	return c.reconcileDistributedSubnetPortGroups(ctx, name)
}

func (c *Controller) reconcileDistributedSubnetPortGroups(ctx context.Context, subnet string) error {
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}
	groups, ports, err := c.distributedPortGroups().GetSubnetPortGroupMembership(ctx, subnet)
	if err != nil {
		return err
	}
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}
	scopedGroups := make([]ovnnb.PortGroup, 0, len(groups))
	for _, group := range groups {
		if isDistributedSubnetPortGroup(group, subnet) {
			scopedGroups = append(scopedGroups, group)
		}
	}
	desired, owners, retirementErr := c.distributedPortGroupOwners(ctx, subnet, scopedGroups, ports)
	batches, planErr := planDistributedPortGroupMembership(scopedGroups, desired)
	for _, batch := range batches {
		if err := c.applyDistributedPortGroupBatch(ctx, subnet, batch, owners); err != nil {
			return err
		}
	}
	return errors.Join(planErr, retirementErr)
}

func (c *Controller) distributedPortGroupOwners(ctx context.Context, subnet string, groups []ovnnb.PortGroup, ports []ovnnb.LogicalSwitchPort) (map[string]string, map[string]distributedPortOwner, error) {
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}
	podsByOwner := make(map[string]*v1.Pod, len(pods))
	for _, pod := range pods {
		if !isPodAlive(pod) || pod.Spec.HostNetwork {
			continue
		}
		key := pod.Namespace + "/" + c.getNameByPod(pod)
		if _, exists := podsByOwner[key]; exists {
			// A VM undergoing live migration can have two alive launchers sharing one LSP.
			podsByOwner[key] = nil
		} else {
			podsByOwner[key] = pod
		}
	}

	members := make(map[string]struct{})
	for _, group := range groups {
		for _, uuid := range group.Ports {
			members[uuid] = struct{}{}
		}
	}
	desired := make(map[string]string, len(ports))
	owners := make(map[string]distributedPortOwner, len(ports))
	var retirementErr error
	for _, port := range ports {
		key := port.ExternalIDs["pod"]
		if port.Type != "" || key == "" || port.ExternalIDs[ovs.LogicalSwitchKey] != subnet {
			continue
		}
		if pod, exists := podsByOwner[key]; exists {
			if pod != nil {
				if provider := c.distributedPortProvider(pod, subnet, port.Name); provider != "" {
					desired[port.UUID] = getOverlaySubnetsPortGroupName(subnet, pod.Spec.NodeName)
					owners[port.UUID] = distributedPortOwner{pod: pod, provider: provider}
				}
			}
			continue
		}
		if _, member := members[port.UUID]; !member {
			continue
		}
		// The normal orphan LSP GC removes ordinary Pods' ports and their weak
		// membership references. StatefulSets and VMs can retain the same LSP.
		ip, err := c.ipsLister.Get(port.Name)
		if k8serrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if ip.Spec.Subnet == subnet && key == ip.Spec.Namespace+"/"+ip.Spec.PodName &&
			(ip.Spec.PodType == util.KindStatefulSet || (c.config.EnableKeepVMIP && ip.Spec.PodType == util.KindVirtualMachine)) {
			inactive, err := c.isInactiveRetainedPort(ctx, ip)
			if err != nil {
				retirementErr = fmt.Errorf("verify retired port %s: %w", port.Name, err)
				continue
			}
			if inactive {
				desired[port.UUID] = ""
				owners[port.UUID] = distributedPortOwner{retained: ip}
			}
		}
	}
	return desired, owners, retirementErr
}

func (c *Controller) distributedPortProvider(pod *v1.Pod, subnet, portName string) string {
	if pod.Spec.NodeName == "" {
		return ""
	}
	provider := c.podPortProvider(pod, subnet, portName)
	if provider == "" {
		return ""
	}
	externalGateway, err := c.podExternalGateway(pod)
	if err != nil {
		// An incomplete gateway configuration cannot establish the desired
		// membership. Let Pod route reconciliation retry without changing it.
		return ""
	}
	for ip := range strings.SplitSeq(pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)], ",") {
		protocol := util.CheckProtocol(ip)
		nextHop := pod.Annotations[util.NorthGatewayAnnotation]
		if c.config.EnableEipSnat && (util.CheckProtocol(pod.Annotations[util.EipAnnotation]) == protocol ||
			util.CheckProtocol(pod.Annotations[util.SnatAnnotation]) == protocol) {
			nextHop = externalGateway
		}
		if len(policyRouteSrcMatches(ip, nextHop)) == 0 {
			return provider
		}
	}
	return ""
}

func (c *Controller) podPortProvider(pod *v1.Pod, subnet, portName string) string {
	podName := c.getNameByPod(pod)
	for annotation, value := range pod.Annotations {
		provider, ok := strings.CutSuffix(annotation, util.LogicalSwitchAnnotationTemplate[len("%s"):])
		if ok && value == subnet && pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)] != "" &&
			ovs.PodNameToPortName(podName, pod.Namespace, provider) == portName {
			return provider
		}
	}
	return ""
}

func (c *Controller) applyDistributedPortGroupBatch(ctx context.Context, subnet string, batch distributedPortGroupBatch, owners map[string]distributedPortOwner) error {
	return c.withDistributedPortGroupLock(subnet, func() error {
		if err := c.checkSubnetRuntimeFence(ctx); err != nil {
			return err
		}
		snapshot, err := c.loadDistributedPortOwners(ctx, batch.lspUUIDs, owners)
		if err != nil {
			return err
		}
		for _, uuid := range batch.lspUUIDs {
			if !c.distributedPortOwnerMatches(subnet, owners[uuid], snapshot) {
				return fmt.Errorf("ownership of logical switch port %s in subnet %s changed during reconciliation", uuid, subnet)
			}
		}
		if err := c.checkSubnetRuntimeFence(ctx); err != nil {
			return err
		}
		return c.distributedPortGroups().ApplyPortGroupMembershipChanges(ctx, batch.changes, batch.lspUUIDs)
	})
}

func (c *Controller) withDistributedPortGroupLock(subnet string, apply func() error) error {
	c.distributedSubnetPortsMutex.LockKey(subnet)
	defer func() { _ = c.distributedSubnetPortsMutex.UnlockKey(subnet) }()
	return apply()
}

func (c *Controller) currentPodPortOwner(ctx context.Context, subnet string, owner distributedPortOwner) (*v1.Pod, error) {
	pod, err := c.config.KubeClient.CoreV1().Pods(owner.pod.Namespace).Get(ctx, owner.pod.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !c.podPortOwnerMatches(pod, subnet, owner) {
		return nil, nil
	}
	return pod, nil
}

func (c *Controller) podPortOwnerMatches(pod *v1.Pod, subnet string, owner distributedPortOwner) bool {
	if pod == nil || owner.pod == nil || owner.provider == "" {
		return false
	}
	portName := ovs.PodNameToPortName(c.getNameByPod(owner.pod), owner.pod.Namespace, owner.provider)
	return pod.UID == owner.pod.UID && pod.Spec.NodeName == owner.pod.Spec.NodeName &&
		!pod.Spec.HostNetwork && isPodAlive(pod) && c.podPortProvider(pod, subnet, portName) == owner.provider
}

func (c *Controller) isInactiveRetainedPort(ctx context.Context, ip *kubeovnv1.IP) (bool, error) {
	if ip == nil {
		return false, errors.New("missing retained port owner")
	}
	ns, name := ip.Spec.Namespace, ip.Spec.PodName
	if ip.Spec.PodType == util.KindStatefulSet {
		pod, err := c.config.KubeClient.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return true, nil
		}
		return err == nil && !isPodAlive(pod), err
	}
	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(ns).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) || (err == nil && vmi.IsFinal()) {
		return c.noOtherAliveVMLauncher(ctx, ns, name, "")
	}
	return false, err
}

func (c *Controller) noOtherAliveVMLauncher(ctx context.Context, namespace, name, exclude string) (bool, error) {
	pods, err := c.config.KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	items := make([]*v1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		items = append(items, &pods.Items[i])
	}
	return !hasOtherAliveVMPortOwner(items, name, exclude), nil
}

func (c *Controller) movePodPortToPortGroup(pod *v1.Pod, subnet, group, port string, externalIDs map[string]string, absentExternalIDs ...string) error {
	return c.withDistributedPortGroupLock(subnet, func() error {
		ctx := context.Background()
		owner := distributedPortOwner{pod: pod, provider: c.podPortProvider(pod, subnet, port)}
		current, err := c.currentPodPortOwner(ctx, subnet, owner)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("ownership of logical switch port %s changed before assignment", port)
		}
		// Two live VM launchers may share this port during migration. The
		// destination's routing event is allowed to move it; the sweep waits
		// until the owner is unambiguous before attempting a repair.
		return c.distributedPortGroups().MovePortToPortGroup(ctx, port, group, externalIDs, absentExternalIDs...)
	})
}

func (c *Controller) removeRetainedPodPort(pod *v1.Pod, port ovnnb.LogicalSwitchPort) error {
	return c.withDistributedPortGroupLock(port.ExternalIDs[ovs.LogicalSwitchKey], func() error {
		ctx := context.Background()
		current, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if err == nil && current.UID != pod.UID {
			return nil
		}
		if isVM, name := isVMPod(pod); c.config.EnableKeepVMIP && isVM {
			inactive, err := c.noOtherAliveVMLauncher(ctx, pod.Namespace, name, pod.Name)
			if err != nil {
				return err
			}
			if !inactive {
				return nil
			}
		}
		// Recheck ownership inside the same lock as assignment and repair so
		// a delayed delete cannot clear a newly claimed retained port.
		klog.Infof("remove retained lsp %s from all port groups", port.Name)
		return c.removePortFromPortGroups(port.Name)
	})
}

func hasOtherAliveVMPortOwner(pods []*v1.Pod, name, exclude string) bool {
	for _, pod := range pods {
		if pod.Name != exclude && isAliveVMPod(pod, name) {
			return true
		}
	}
	return false
}
