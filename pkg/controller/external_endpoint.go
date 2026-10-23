package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// externalEndpointExternalIDKey is set on the logical switch port of an
// ExternalEndpoint to <namespace>/<name>. It is deliberately not the "pod" key,
// so pod and KubeVirt VM deletion never match the port.
const externalEndpointExternalIDKey = "external_endpoint"

func (c *Controller) enqueueAddExternalEndpoint(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.ExternalEndpoint)).String()
	klog.V(3).Infof("enqueue add external endpoint %s", key)
	c.addOrUpdateExternalEndpointQueue.Add(key)
}

func (c *Controller) enqueueUpdateExternalEndpoint(oldObj, newObj any) {
	oldEp, newEp := oldObj.(*kubeovnv1.ExternalEndpoint), newObj.(*kubeovnv1.ExternalEndpoint)
	if oldEp.Generation == newEp.Generation && newEp.DeletionTimestamp.IsZero() {
		// status-only update written by ourselves
		return
	}
	key := cache.MetaObjectToName(newEp).String()
	klog.V(3).Infof("enqueue update external endpoint %s", key)
	c.addOrUpdateExternalEndpointQueue.Add(key)
}

// externalEndpointPortName returns the logical switch port name, which is also
// the OVS iface-id the hypervisor must set.
func externalEndpointPortName(ep *kubeovnv1.ExternalEndpoint) string {
	if ep.Spec.InterfaceID != "" {
		return ep.Spec.InterfaceID
	}
	return string(ep.UID)
}

// externalEndpointIPAMKey returns the ipam owner key of the endpoint. Pod keys
// are <namespace>/<name>, so the kind prefix keeps a pod and an endpoint with the
// same name from sharing (and releasing) each other's addresses.
func externalEndpointIPAMKey(namespace, name string) string {
	return fmt.Sprintf("%s:%s/%s", util.KindExternalEndpoint, namespace, name)
}

func (c *Controller) handleAddOrUpdateExternalEndpoint(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	cachedEp, err := c.externalEndpointsLister.ExternalEndpoints(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedEp.DeletionTimestamp.IsZero() {
		return c.handleDelExternalEndpoint(cachedEp.DeepCopy())
	}

	ep := cachedEp.DeepCopy()
	if controllerutil.AddFinalizer(ep, util.KubeOVNControllerFinalizer) {
		if ep, err = c.patchExternalEndpointFinalizers(cachedEp, ep); err != nil {
			err = fmt.Errorf("failed to add finalizer to external endpoint %s: %w", key, err)
			klog.Error(err)
			return err
		}
	}

	reason, err := c.reconcileExternalEndpoint(ep)
	if err != nil {
		if old := meta.FindStatusCondition(cachedEp.Status.Conditions, kubeovnv1.ExternalEndpointConditionReady); old == nil ||
			old.Reason != reason || old.Message != err.Error() {
			// the periodic resync retries failed endpoints, so only report a failure once
			c.recorder.Eventf(ep, corev1.EventTypeWarning, reason, "%v", err)
		}
		meta.SetStatusCondition(&ep.Status.Conditions, metav1.Condition{
			Type:               kubeovnv1.ExternalEndpointConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            err.Error(),
			ObservedGeneration: ep.Generation,
		})
		if statusErr := c.updateExternalEndpointStatus(cachedEp, ep); statusErr != nil {
			err = errors.Join(err, statusErr)
		}
		if isPermanentExternalEndpointError(err) {
			// retrying will not help until the spec, the subnet or another owner of the
			// address changes; the periodic resync picks the endpoint up again
			klog.Warningf("external endpoint %s: %v", key, err)
			return nil
		}
		klog.Error(err)
		return err
	}
	return c.updateExternalEndpointStatus(cachedEp, ep)
}

var errExternalEndpointInvalid = errors.New("invalid external endpoint")

func isPermanentExternalEndpointError(err error) bool {
	return errors.Is(err, errExternalEndpointInvalid) || errors.Is(err, ipam.ErrConflict) ||
		errors.Is(err, ipam.ErrOutOfRange) || errors.Is(err, ipam.ErrNoAvailable) || errors.Is(err, ipam.ErrInvalidIPFamily)
}

// reconcileExternalEndpoint allocates the address, records it in an IP CR and
// converges the logical switch port. It fills ep.Status and returns the
// condition reason to report on failure.
func (c *Controller) reconcileExternalEndpoint(ep *kubeovnv1.ExternalEndpoint) (string, error) {
	subnet, err := c.subnetsLister.Get(ep.Spec.Subnet)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "SubnetNotFound", fmt.Errorf("%w: subnet %s not found", errExternalEndpointInvalid, ep.Spec.Subnet)
		}
		return "GetSubnetFailed", err
	}
	if !isOvnSubnet(subnet) {
		return "UnsupportedSubnet", fmt.Errorf("%w: subnet %s is not an ovn subnet", errExternalEndpointInvalid, subnet.Name)
	}
	if subnet.Spec.Vlan != "" || subnet.Spec.CIDRBlock == "" {
		// an underlay port would need the hypervisor's own provider bridge mappings
		return "UnsupportedSubnet", fmt.Errorf("%w: subnet %s is an underlay or mac-only subnet, only overlay subnets are supported", errExternalEndpointInvalid, subnet.Name)
	}
	if ep.Spec.Chassis == "" {
		return "ChassisRequired", fmt.Errorf("%w: spec.chassis is required", errExternalEndpointInvalid)
	}
	if len(subnet.Spec.Namespaces) != 0 && !slices.Contains(subnet.Spec.Namespaces, ep.Namespace) {
		return "NamespaceNotAllowed", fmt.Errorf("%w: subnet %s is not bound to namespace %s", errExternalEndpointInvalid, subnet.Name, ep.Namespace)
	}
	if !subnet.Status.IsReady() {
		return "SubnetNotReady", fmt.Errorf("subnet %s is not ready", subnet.Name)
	}

	portName := externalEndpointPortName(ep)
	if ep.Status.InterfaceID != "" && ep.Status.InterfaceID != portName {
		return "InterfaceIDChanged", fmt.Errorf("%w: interface id changed from %s to %s", errExternalEndpointInvalid, ep.Status.InterfaceID, portName)
	}

	v4IP, v6IP, mac, err := c.acquireExternalEndpointAddress(ep, subnet, portName)
	if err != nil {
		return "AcquireAddressFailed", err
	}
	newAddress := ep.Status.V4IP != v4IP || ep.Status.V6IP != v6IP
	ep.Status.InterfaceID, ep.Status.V4IP, ep.Status.V6IP, ep.Status.MacAddress = portName, v4IP, v6IP, mac
	ipStr := util.GetStringIP(v4IP, v6IP)

	if err = c.createOrUpdateExternalEndpointIPCR(ep, subnet.Name, portName, ipStr, mac); err != nil {
		return "CreateIPFailed", err
	}

	lsp, err := c.ensureExternalEndpointPort(ep, subnet, portName, ipStr, mac)
	if err != nil {
		return "CreateOVNPortFailed", err
	}

	ep.Status.Up = lsp.Up != nil && *lsp.Up
	meta.SetStatusCondition(&ep.Status.Conditions, metav1.Condition{
		Type:               kubeovnv1.ExternalEndpointConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            fmt.Sprintf("logical switch port %s is provisioned on %s", portName, subnet.Name),
		ObservedGeneration: ep.Generation,
	})
	bound := metav1.Condition{
		Type:               kubeovnv1.ExternalEndpointConditionBound,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForChassis",
		Message:            fmt.Sprintf("no chassis has claimed iface-id %s", portName),
		ObservedGeneration: ep.Generation,
	}
	if ep.Status.Up {
		bound.Status, bound.Reason, bound.Message = metav1.ConditionTrue, "PortUp", "the port is bound to a chassis"
	}
	meta.SetStatusCondition(&ep.Status.Conditions, bound)
	if newAddress {
		c.updateSubnetStatusQueue.Add(subnet.Name)
	}
	return "", nil
}

// acquireExternalEndpointAddress reserves the endpoint's address in ipam. An
// address that is already recorded in the status is reserved again as is, which
// also restores the reservation of an endpoint whose ipam entry was released.
func (c *Controller) acquireExternalEndpointAddress(ep *kubeovnv1.ExternalEndpoint, subnet *kubeovnv1.Subnet, portName string) (string, string, string, error) {
	key := externalEndpointIPAMKey(ep.Namespace, ep.Name)
	if ip := util.GetStringIP(ep.Status.V4IP, ep.Status.V6IP); ip != "" {
		if c.ipamHoldsAddress(key, subnet.Name, ip, ep.Status.MacAddress) {
			// the common case on every resync: nothing to reserve
			return ep.Status.V4IP, ep.Status.V6IP, ep.Status.MacAddress, nil
		}
		mac := ep.Status.MacAddress
		return c.ipam.GetStaticAddress(key, portName, ip, &mac, subnet.Name, true)
	}
	// the status is written last: after a crash the address may only be recorded in
	// the endpoint's IP CR, which must be reused rather than allocating a new one
	if ipCR, err := c.ipsLister.Get(portName); err == nil && ipCR.Spec.IPAddress != "" && ownedByExternalEndpoint(ipCR, ep) {
		mac := ipCR.Spec.MacAddress
		return c.ipam.GetStaticAddress(key, portName, ipCR.Spec.IPAddress, &mac, subnet.Name, true)
	}

	var macPtr *string
	if ep.Spec.MacAddress != "" {
		mac := strings.ToLower(ep.Spec.MacAddress)
		macPtr = &mac
	}
	if ep.Spec.IPAddress != "" {
		ips := make([]string, 0, 2)
		for ip := range strings.SplitSeq(ep.Spec.IPAddress, ",") {
			parsed := net.ParseIP(strings.TrimSpace(ip))
			if parsed == nil {
				return "", "", "", fmt.Errorf("%w: %q is not a valid ip address", errExternalEndpointInvalid, ip)
			}
			// canonical form, so that the IP CR and the status pass validation for any
			// spelling of an IPv6 address
			ip = parsed.String()
			if !util.CIDRContainIP(subnet.Spec.CIDRBlock, ip) {
				return "", "", "", fmt.Errorf("%w: %s is not in the cidr %s of subnet %s", ipam.ErrOutOfRange, ip, subnet.Spec.CIDRBlock, subnet.Name)
			}
			ips = append(ips, ip)
		}
		return c.ipam.GetStaticAddress(key, portName, strings.Join(ips, ","), macPtr, subnet.Name, true)
	}
	return c.ipam.GetRandomAddress(key, portName, macPtr, subnet.Name, "", nil, true)
}

// ipamHoldsAddress reports whether ipam already holds exactly ip (comma separated
// for dual stack) and mac for the owner key in the subnet.
func (c *Controller) ipamHoldsAddress(key, subnetName, ip, mac string) bool {
	want := strings.Split(ip, ",")
	var got []string
	for _, addr := range c.ipam.GetPodAddress(key) {
		if addr.Subnet == nil || addr.Subnet.Name != subnetName || addr.Mac != mac {
			return false
		}
		got = append(got, addr.IP)
	}
	slices.Sort(want)
	slices.Sort(got)
	return slices.Equal(want, got)
}

// ownedByExternalEndpoint reports whether the IP CR records the address of ep. The
// IP CR is cluster scoped, so the owner is recorded in a label rather than in an
// owner reference to the namespaced endpoint, which the Kubernetes garbage
// collector does not accept.
func ownedByExternalEndpoint(ipCR *kubeovnv1.IP, ep *kubeovnv1.ExternalEndpoint) bool {
	return ipCR.Spec.PodType == util.KindExternalEndpoint && ipCR.Labels[util.ExternalEndpointUIDLabel] == string(ep.UID)
}

// createOrUpdateExternalEndpointIPCR records the endpoint's address in an IP CR.
// Unlike createOrUpdateIPCR it marks the address reserved, carries no node, and
// refuses to update an IP CR that another object owns.
func (c *Controller) createOrUpdateExternalEndpointIPCR(ep *kubeovnv1.ExternalEndpoint, subnetName, portName, ipStr, mac string) error {
	v4IP, v6IP := util.SplitStringIP(ipStr)
	ipLabels := map[string]string{
		util.SubnetNameLabel:          subnetName,
		subnetName:                    "",
		util.IPReservedLabel:          "true",
		util.ExternalEndpointUIDLabel: string(ep.UID),
	}
	spec := kubeovnv1.IPSpec{
		PodName:       ep.Name,
		Namespace:     ep.Namespace,
		Subnet:        subnetName,
		IPAddress:     ipStr,
		V4IPAddress:   v4IP,
		V6IPAddress:   v6IP,
		MacAddress:    mac,
		AttachIPs:     []string{},
		AttachMacs:    []string{},
		AttachSubnets: []string{},
		PodType:       util.KindExternalEndpoint,
	}

	ipCR, err := c.ipsLister.Get(portName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		ipCR = &kubeovnv1.IP{
			Name:       portName,
			Labels:     ipLabels,
			Finalizers: []string{util.KubeOVNControllerFinalizer},
			Spec:       spec,
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), ipCR, metav1.CreateOptions{}); err != nil {
			err = fmt.Errorf("failed to create ip CR %s: %w", portName, err)
			klog.Error(err)
			return err
		}
		return nil
	}
	if !ipCR.DeletionTimestamp.IsZero() {
		// recreated once the ip handler has released it
		return fmt.Errorf("ip CR %s is being deleted", portName)
	}
	if !ownedByExternalEndpoint(ipCR, ep) {
		return fmt.Errorf("%w: ip CR %s is owned by another object", errExternalEndpointInvalid, portName)
	}

	newIPCR := ipCR.DeepCopy()
	if newIPCR.Labels == nil {
		newIPCR.Labels = make(map[string]string, len(ipLabels))
	}
	maps.Copy(newIPCR.Labels, ipLabels)
	newIPCR.Spec = spec
	controllerutil.AddFinalizer(newIPCR, util.KubeOVNControllerFinalizer)
	if maps.Equal(newIPCR.Labels, ipCR.Labels) && reflect.DeepEqual(newIPCR.Spec, ipCR.Spec) &&
		slices.Equal(newIPCR.Finalizers, ipCR.Finalizers) {
		return nil
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), newIPCR, metav1.UpdateOptions{}); err != nil {
		err = fmt.Errorf("failed to update ip CR %s: %w", portName, err)
		klog.Error(err)
		return err
	}
	return nil
}

// ensureExternalEndpointPort converges the logical switch port and only writes
// to the NB database when the port differs from the spec, so that the periodic
// resync does not generate NB transactions for endpoints that did not change.
func (c *Controller) ensureExternalEndpointPort(ep *kubeovnv1.ExternalEndpoint, subnet *kubeovnv1.Subnet, portName, ipStr, mac string) (*ovnnb.LogicalSwitchPort, error) {
	dhcpOptions, _, _ := dhcpOptionsForPodIPFamily(subnetDHCPOptionsUUIDs(subnet), ipStr, "", "")
	enableDHCP := subnet.Spec.EnableDHCP
	portSecurity := ep.Spec.EnablePortSecurity()
	securityGroups := slices.Sorted(slices.Values(ep.Spec.SecurityGroups))
	owner := cache.MetaObjectToName(ep).String()

	existing, err := c.OVNNbClient.GetLogicalSwitchPort(portName, true)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	var oldSgs []string
	if existing != nil {
		if err = checkExternalEndpointPortOwner(existing, owner); err != nil {
			return nil, err
		}
		oldSgs, _ = c.getPortSg(existing)
	}
	if existing == nil || !externalEndpointPortUpToDate(existing, owner, subnet.Name, ipStr, mac, ep.Spec.Chassis, portSecurity, enableDHCP, dhcpOptions, securityGroups) {
		klog.Infof("create or update logical switch port %s for external endpoint %s", portName, owner)
		if err = c.OVNNbClient.CreateLogicalSwitchPortWithOwner(subnet.Name, portName, ipStr, mac, portSecurity,
			strings.Join(securityGroups, ","), enableDHCP, dhcpOptions, subnet.Spec.Vpc,
			map[string]string{externalEndpointExternalIDKey: owner}, ep.Spec.Chassis); err != nil {
			klog.Error(err)
			return nil, err
		}
		for _, sg := range util.UnionStringSlice(oldSgs, securityGroups) {
			c.syncSgPortsQueue.Add(sg)
		}
	}

	lsp, err := c.OVNNbClient.GetLogicalSwitchPort(portName, false)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return lsp, nil
}

// checkExternalEndpointPortOwner refuses to take over a port that the endpoint
// does not own, whoever created it.
func checkExternalEndpointPortOwner(lsp *ovnnb.LogicalSwitchPort, owner string) error {
	if o := lsp.ExternalIDs[externalEndpointExternalIDKey]; o != owner {
		return fmt.Errorf("%w: logical switch port %s already exists and is not owned by external endpoint %s", errExternalEndpointInvalid, lsp.Name, owner)
	}
	return nil
}

func externalEndpointPortUpToDate(lsp *ovnnb.LogicalSwitchPort, owner, lsName, ipStr, mac, chassis string, portSecurity, enableDHCP bool, dhcpOptions *ovs.DHCPOptionsUUIDs, securityGroups []string) bool {
	addresses := strings.TrimSpace(mac + " " + strings.ReplaceAll(ipStr, ",", " "))
	if lsp.ExternalIDs[externalEndpointExternalIDKey] != owner || lsp.ExternalIDs[ovs.LogicalSwitchKey] != lsName ||
		lsp.Options["requested-chassis"] != chassis || !slices.Equal(lsp.Addresses, []string{addresses}) {
		return false
	}
	if portSecurity && !slices.Equal(lsp.PortSecurity, []string{addresses}) || !portSecurity && len(lsp.PortSecurity) != 0 {
		return false
	}
	var wantV4, wantV6 string
	if enableDHCP && dhcpOptions != nil {
		wantV4, wantV6 = dhcpOptions.DHCPv4OptionsUUID, dhcpOptions.DHCPv6OptionsUUID
	}
	if ptrValue(lsp.Dhcpv4Options) != wantV4 || ptrValue(lsp.Dhcpv6Options) != wantV6 {
		return false
	}
	var sgs []string
	for key, value := range lsp.ExternalIDs {
		if value == "true" && strings.HasPrefix(key, "associated_sg_") {
			sgs = append(sgs, strings.TrimPrefix(key, "associated_sg_"))
		}
	}
	slices.Sort(sgs)
	return slices.Equal(sgs, securityGroups)
}

func ptrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// patchExternalEndpointFinalizers writes the finalizers with a merge patch, like
// handleDelIPFinalizer does, so a stale informer copy does not fail with a
// resourceVersion conflict.
func (c *Controller) patchExternalEndpointFinalizers(cachedEp, ep *kubeovnv1.ExternalEndpoint) (*kubeovnv1.ExternalEndpoint, error) {
	patch, err := util.GenerateMergePatchPayload(cachedEp, ep)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return c.config.KubeOvnClient.KubeovnV1().ExternalEndpoints(ep.Namespace).Patch(context.Background(), ep.Name, types.MergePatchType, patch, metav1.PatchOptions{})
}

func (c *Controller) updateExternalEndpointStatus(cachedEp, ep *kubeovnv1.ExternalEndpoint) error {
	if reflect.DeepEqual(cachedEp.Status, ep.Status) {
		return nil
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().ExternalEndpoints(ep.Namespace).UpdateStatus(context.Background(), ep, metav1.UpdateOptions{}); err != nil {
		err = fmt.Errorf("failed to update status of external endpoint %s/%s: %w", ep.Namespace, ep.Name, err)
		klog.Error(err)
		return err
	}
	return nil
}

// handleDelExternalEndpoint deletes the logical switch port, releases the
// address and removes the IP CR before it releases the finalizer.
func (c *Controller) handleDelExternalEndpoint(ep *kubeovnv1.ExternalEndpoint) error {
	key := cache.MetaObjectToName(ep).String()
	if !controllerutil.ContainsFinalizer(ep, util.KubeOVNControllerFinalizer) {
		return nil
	}
	klog.Infof("handle deleting external endpoint %s", key)

	portName := externalEndpointPortName(ep)
	if ep.Status.InterfaceID != "" {
		portName = ep.Status.InterfaceID
	}
	lsp, err := c.OVNNbClient.GetLogicalSwitchPort(portName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if lsp != nil && checkExternalEndpointPortOwner(lsp, key) == nil {
		sgs, _ := c.getPortSg(lsp)
		if err = c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
			klog.Errorf("failed to delete logical switch port %s of external endpoint %s: %v", portName, key, err)
			return err
		}
		for _, sg := range sgs {
			c.syncSgPortsQueue.Add(sg)
		}
	}

	// like pod deletion, delete the IP CR before releasing the address: if deleting it
	// fails and the controller restarts, the address is still restored to this endpoint
	// rather than handed to a pod in between
	ipCR, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), portName, metav1.GetOptions{})
	switch {
	case err == nil && ownedByExternalEndpoint(ipCR, ep):
		uid := ipCR.UID
		if err = c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName,
			metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete ip CR %s of external endpoint %s: %v", portName, key, err)
			return err
		}
	case err != nil && !k8serrors.IsNotFound(err):
		// read from the API server rather than the informer cache: a cache miss right
		// before the finalizer is removed would leave the ip CR behind
		klog.Errorf("failed to get ip CR %s of external endpoint %s: %v", portName, key, err)
		return err
	}
	c.ipam.ReleaseAddressByNic(externalEndpointIPAMKey(ep.Namespace, ep.Name), portName, ep.Spec.Subnet)

	newEp := ep.DeepCopy()
	controllerutil.RemoveFinalizer(newEp, util.KubeOVNControllerFinalizer)
	if _, err = c.patchExternalEndpointFinalizers(ep, newEp); err != nil && !k8serrors.IsNotFound(err) {
		err = fmt.Errorf("failed to remove finalizer from external endpoint %s: %w", key, err)
		klog.Error(err)
		return err
	}
	c.updateSubnetStatusQueue.Add(ep.Spec.Subnet)
	return nil
}

// resyncExternalEndpoints enqueues every endpoint. Nothing in Kubernetes changes
// when the port is bound or unbound, or when the NB port is changed or deleted
// out of band, so this is how the status follows the chassis and how drift is
// repaired.
func (c *Controller) resyncExternalEndpoints() {
	eps, err := c.externalEndpointsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list external endpoints: %v", err)
		return
	}
	for _, ep := range eps {
		c.addOrUpdateExternalEndpointQueue.Add(cache.MetaObjectToName(ep).String())
	}
}

// externalEndpointPortNames returns the logical switch ports owned by
// external endpoints, which the lsp gc must keep.
func (c *Controller) externalEndpointPortNames() (map[string]struct{}, error) {
	eps, err := c.externalEndpointsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list external endpoints: %v", err)
		return nil, err
	}
	ports := make(map[string]struct{}, len(eps))
	for _, ep := range eps {
		ports[externalEndpointPortName(ep)] = struct{}{}
		if ep.Status.InterfaceID != "" {
			ports[ep.Status.InterfaceID] = struct{}{}
		}
	}
	return ports, nil
}

// externalEndpointChassis returns the chassis names and hostnames that the ports of
// external endpoints are pinned to, which the chassis gc must keep. They are read
// from the NB ports rather than the endpoints, so that the pins of ports that are
// kept while the feature is disabled are honoured too.
func (c *Controller) externalEndpointChassis() (map[string]struct{}, error) {
	lsps, err := c.OVNNbClient.ListLogicalSwitchPorts(true, nil, func(lsp *ovnnb.LogicalSwitchPort) bool {
		return lsp.ExternalIDs[externalEndpointExternalIDKey] != ""
	})
	if err != nil {
		klog.Errorf("failed to list the logical switch ports of external endpoints: %v", err)
		return nil, err
	}
	chassis := make(map[string]struct{}, len(lsps))
	for _, lsp := range lsps {
		if name := lsp.Options["requested-chassis"]; name != "" {
			chassis[name] = struct{}{}
		}
	}
	return chassis, nil
}

// releaseOrphanedExternalEndpointAddress is called when the IP CR of an external
// endpoint is deleted. While the endpoint exists, the port and the address stay
// with it and the endpoint recreates its IP CR. When the endpoint is gone (deleted
// without its finalizer having run), nothing else releases them, so the port and the
// address are released here, like the pod branch of handleUpdateIP does for pods.
// While the feature is disabled the endpoints cannot be read and nothing is released.
func (c *Controller) releaseOrphanedExternalEndpointAddress(ip *kubeovnv1.IP) error {
	if !c.config.EnableExternalEndpoint {
		return nil
	}
	ep, err := c.config.KubeOvnClient.KubeovnV1().ExternalEndpoints(ip.Spec.Namespace).Get(context.Background(), ip.Spec.PodName, metav1.GetOptions{})
	switch {
	case err == nil && ownedByExternalEndpoint(ip, ep):
		return nil
	case err != nil && !k8serrors.IsNotFound(err):
		klog.Errorf("failed to get external endpoint %s/%s: %v", ip.Spec.Namespace, ip.Spec.PodName, err)
		return err
	}

	owner := cache.NewObjectName(ip.Spec.Namespace, ip.Spec.PodName).String()
	lsp, err := c.OVNNbClient.GetLogicalSwitchPort(ip.Name, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if lsp != nil && lsp.ExternalIDs[externalEndpointExternalIDKey] == owner {
		klog.Infof("delete logical switch port %s of deleted external endpoint %s", ip.Name, owner)
		if err = c.OVNNbClient.DeleteLogicalSwitchPort(ip.Name); err != nil {
			klog.Errorf("failed to delete logical switch port %s: %v", ip.Name, err)
			return err
		}
	}
	c.ipam.ReleaseAddressByNic(externalEndpointIPAMKey(ip.Spec.Namespace, ip.Spec.PodName), ip.Name, ip.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(ip.Spec.Subnet)
	return nil
}
