package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/cache"
	"k8s.io/dynamic-resource-allocation/resourceclaim"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/scylladb/go-set/strset"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// draNicAPIServed reports whether the API server serves the ResourceClaims and ResourceSlices of
// resource.k8s.io/v1 that the DRA NIC support watches. Errors other than the group version not being
// served are returned, so a transient discovery failure is not mistaken for a cluster without DRA.
func draNicAPIServed(client discovery.DiscoveryInterface) (bool, error) {
	list, err := client.ServerResourcesForGroupVersion(resourcev1.SchemeGroupVersion.String())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to discover %s: %w", resourcev1.SchemeGroupVersion, err)
	}
	served := strset.New()
	for _, r := range list.APIResources {
		served.Add(r.Name)
	}
	return served.Has("resourceclaims", "resourceslices"), nil
}

const (
	// draClaimPodIndex indexes ResourceClaims by the "namespace/name" of every pod in status.reservedFor.
	// The names of claims generated from templates are only in the pod's status.resourceClaimStatuses,
	// which the pod informer trims, so the claim side holds the pod-to-claim mapping.
	draClaimPodIndex = "draReservedForPod"
	// draSlicePoolIndex indexes ResourceSlices by "driver/pool".
	draSlicePoolIndex = "draDriverPool"
	// draClaimPoolIndex indexes allocated ResourceClaims by the "driver/pool" of their devices.
	draClaimPoolIndex = "draAllocatedPool"

	draAttrSubnetName = "subnetName"
)

func draClaimPodIndexFunc(obj any) ([]string, error) {
	claim, ok := obj.(*resourcev1.ResourceClaim)
	if !ok {
		return nil, nil
	}
	keys := make([]string, 0, len(claim.Status.ReservedFor))
	for _, ref := range claim.Status.ReservedFor {
		if ref.APIGroup == "" && ref.Resource == "pods" {
			keys = append(keys, cache.NewObjectName(claim.Namespace, ref.Name).String())
		}
	}
	return keys, nil
}

func draClaimPoolIndexFunc(obj any) ([]string, error) {
	claim, ok := obj.(*resourcev1.ResourceClaim)
	if !ok || claim.Status.Allocation == nil {
		return nil, nil
	}
	keys := strset.New()
	for _, result := range claim.Status.Allocation.Devices.Results {
		keys.Add(draPoolKey(result.Driver, result.Pool))
	}
	return keys.List(), nil
}

func draPoolKey(driver, pool string) string {
	return driver + "/" + pool
}

func draSlicePoolIndexFunc(obj any) ([]string, error) {
	slice, ok := obj.(*resourcev1.ResourceSlice)
	if !ok {
		return nil, nil
	}
	return []string{draPoolKey(slice.Spec.Driver, slice.Spec.Pool.Name)}, nil
}

// isClaimReservedForPod checks the pod UID so a claim still reserved for a previous pod
// with the same name (statefulset, virt-launcher) is not attributed to the new one.
func isClaimReservedForPod(claim *resourcev1.ResourceClaim, podName string, podUID types.UID) bool {
	for _, ref := range claim.Status.ReservedFor {
		if ref.APIGroup == "" && ref.Resource == "pods" && ref.Name == podName && ref.UID == podUID {
			return true
		}
	}
	return false
}

// getDRADevice returns the device as published in the pool's newest generation, or nil if that
// generation does not contain it. Slices of older generations are outdated as a whole, even while
// they are still in the cache, so a device only found there is not published anymore. While not
// all slices of the newest generation are in the cache, e.g. during a rollout, the pool is
// incomplete and an error is returned, so the pod is retried instead of using a partial view.
func (c *Controller) getDRADevice(driver, pool, name string) (*resourcev1.Device, error) {
	objs, err := c.resourceSliceIndexer.ByIndex(draSlicePoolIndex, draPoolKey(driver, pool))
	if err != nil {
		return nil, err
	}
	slices := make([]*resourcev1.ResourceSlice, 0, len(objs))
	generation := int64(-1)
	for _, obj := range objs {
		if slice, ok := obj.(*resourcev1.ResourceSlice); ok {
			slices = append(slices, slice)
			generation = max(generation, slice.Spec.Pool.Generation)
		}
	}
	var count, want int64
	for _, slice := range slices {
		if slice.Spec.Pool.Generation == generation {
			count++
			want = max(want, slice.Spec.Pool.ResourceSliceCount)
		}
	}
	if count < want {
		return nil, fmt.Errorf("pool %s of driver %s is incomplete: %d of %d slices of generation %d are known", pool, driver, count, want, generation)
	}
	for _, slice := range slices {
		if slice.Spec.Pool.Generation != generation {
			continue
		}
		for i := range slice.Spec.Devices {
			if slice.Spec.Devices[i].Name == name {
				return &slice.Spec.Devices[i], nil
			}
		}
	}
	return nil, nil
}

// draDeviceStringAttribute reads an attribute either fully qualified with the driver name as domain
// or unqualified, which the DRA API treats as the same attribute.
func draDeviceStringAttribute(device *resourcev1.Device, driver, name string) string {
	for _, key := range []resourcev1.QualifiedName{resourcev1.QualifiedName(driver + "/" + name), resourcev1.QualifiedName(name)} {
		if attr, ok := device.Attributes[key]; ok && attr.StringValue != nil {
			return *attr.StringValue
		}
	}
	return ""
}

// getPodDRANets returns one kubeovnNet per device the kube-ovn DRA NIC driver allocated to the pod.
// It is the DRA counterpart of getPodAttachmentNet: the allocated device names a subnet, and the
// subnet's provider plays the role a NetworkAttachmentDefinition plays for Multus.
func (c *Controller) getPodDRANets(pod *v1.Pod) ([]*kubeovnNet, error) {
	if !c.config.EnableDRANic || c.resourceClaimIndexer == nil {
		return nil, nil
	}

	objs, err := c.resourceClaimIndexer.ByIndex(draClaimPodIndex, cache.MetaObjectToName(pod).String())
	if err != nil {
		klog.Errorf("failed to get resource claims of pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return nil, err
	}

	var result []*kubeovnNet
	for _, obj := range objs {
		claim, ok := obj.(*resourcev1.ResourceClaim)
		if !ok || claim.Status.Allocation == nil || !isClaimReservedForPod(claim, pod.Name, pod.UID) {
			continue
		}
		if err := c.checkSharedDRAClaim(pod, claim); err != nil {
			klog.Error(err)
			return nil, err
		}
		records, err := draNicRecords(claim)
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		recorded := len(records)
		for _, alloc := range claim.Status.Allocation.Devices.Results {
			if alloc.Driver != c.config.DRANicDriverName {
				continue
			}
			net, err := c.draNetForAllocation(pod, claim, alloc, records)
			if err != nil {
				return nil, err
			}
			if net != nil {
				result = append(result, net)
			}
		}
		if len(records) != recorded && pod.DeletionTimestamp.IsZero() {
			if err := c.recordDRANics(claim, records); err != nil {
				return nil, err
			}
		}
	}
	if err := nameDRANets(pod, result, c.config.DRANicDriverName); err != nil {
		return nil, err
	}
	return result, nil
}

// draNicRecord is the subnet and provider a DRA nic was allocated with. kube-ovn records it on the
// claim when it first resolves the nic, and from then on uses it instead of the current subnet and
// device: the provider keys the nic's annotations, port and ip CR, so a later change of the subnet's
// provider or of the published device must not move an allocated nic.
type draNicRecord struct {
	Subnet   string `json:"subnet"`
	Provider string `json:"provider"`
}

// draNicKey keys the record of an allocated device. Results on the same device, e.g. of a request
// with a count, share the record, as they share the subnet.
func draNicKey(alloc resourcev1.DeviceRequestAllocationResult) string {
	return alloc.Pool + "/" + alloc.Device
}

// draNicRecords returns the nics recorded on the claim by draNicKey.
func draNicRecords(claim *resourcev1.ResourceClaim) (map[string]draNicRecord, error) {
	records := make(map[string]draNicRecord)
	value := claim.Annotations[util.DRAProvidersAnnotation]
	if value == "" {
		return records, nil
	}
	if err := json.Unmarshal([]byte(value), &records); err != nil {
		return nil, fmt.Errorf("invalid %s annotation of resource claim %s/%s: %w", util.DRAProvidersAnnotation, claim.Namespace, claim.Name, err)
	}
	return records, nil
}

// recordDRANics writes the nics of the claim to its annotation.
func (c *Controller) recordDRANics(claim *resourcev1.ResourceClaim, records map[string]draNicRecord) error {
	value, err := json.Marshal(records)
	if err != nil {
		klog.Error(err)
		return err
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{util.DRAProvidersAnnotation: string(value)}}})
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err = c.config.KubeClient.ResourceV1().ResourceClaims(claim.Namespace).Patch(context.Background(), claim.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to record the dra nics of resource claim %s/%s: %v", claim.Namespace, claim.Name, err)
		return err
	}
	return nil
}

// draNetForAllocation resolves one allocated device to its subnet: the recorded one, or for a new
// nic the subnet its subnetName attribute names, which is then added to records. It returns nil
// without an error for a deleting pod whose device or subnet is already gone.
func (c *Controller) draNetForAllocation(pod *v1.Pod, claim *resourcev1.ResourceClaim, alloc resourcev1.DeviceRequestAllocationResult, records map[string]draNicRecord) (*kubeovnNet, error) {
	if record, ok := records[draNicKey(alloc)]; ok {
		return c.recordedDRANet(pod, claim, alloc, record)
	}
	// ignore to return all existing subnets to clean its ip crd
	ignoreSubnetNotExist := !pod.DeletionTimestamp.IsZero()
	driver := c.config.DRANicDriverName
	device, err := c.getDRADevice(alloc.Driver, alloc.Pool, alloc.Device)
	if err != nil {
		klog.Errorf("failed to get device %s of claim %s/%s: %v", alloc.Device, claim.Namespace, claim.Name, err)
		return nil, err
	}
	if device == nil {
		err = fmt.Errorf("device %s in pool %s of claim %s/%s is not published by driver %s", alloc.Device, alloc.Pool, claim.Namespace, claim.Name, driver)
		if ignoreSubnetNotExist {
			klog.Errorf("deleting pod %s/%s: %v, gc will clean its ip cr", pod.Namespace, pod.Name, err)
			return nil, nil
		}
		klog.Error(err)
		return nil, err
	}

	subnetName := draDeviceStringAttribute(device, driver, draAttrSubnetName)
	if subnetName == "" {
		err = fmt.Errorf("device %s of driver %s has no %s attribute", alloc.Device, driver, draAttrSubnetName)
		klog.Error(err)
		return nil, err
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		if k8serrors.IsNotFound(err) && ignoreSubnetNotExist {
			klog.Errorf("deleting pod %s/%s dra subnet %s already not exist, gc will clean its ip cr", pod.Namespace, pod.Name, subnetName)
			return nil, nil
		}
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
		return nil, err
	}

	// The default provider names the pod's primary port, so a DRA NIC needs a dedicated
	// provider, the same "<name>.<namespace>.ovn" convention a NetworkAttachmentDefinition uses.
	if subnet.Spec.Provider == "" || subnet.Spec.Provider == util.OvnProvider {
		err = fmt.Errorf("subnet %s allocated by claim %s/%s uses the default provider and cannot back a secondary nic", subnet.Name, claim.Namespace, claim.Name)
		klog.Error(err)
		return nil, err
	}

	// The DRA NIC driver plugs every nic into br-int and binds it to the logical switch port,
	// which only exists for an OVN subnet.
	if !isOvnSubnet(subnet) {
		err = fmt.Errorf("subnet %s allocated by claim %s/%s uses the non-OVN provider %s, which a dra nic does not support", subnet.Name, claim.Namespace, claim.Name, subnet.Spec.Provider)
		klog.Error(err)
		return nil, err
	}
	records[draNicKey(alloc)] = draNicRecord{Subnet: subnet.Name, Provider: subnet.Spec.Provider}
	return draNet(pod, claim, alloc, subnet, subnet.Spec.Provider, driver), nil
}

// recordedDRANet resolves a nic with the subnet and provider it was allocated with. A subnet whose
// provider changed since keeps serving the nic under the recorded provider.
func (c *Controller) recordedDRANet(pod *v1.Pod, claim *resourcev1.ResourceClaim, alloc resourcev1.DeviceRequestAllocationResult, record draNicRecord) (*kubeovnNet, error) {
	subnet, err := c.subnetsLister.Get(record.Subnet)
	if err != nil {
		if k8serrors.IsNotFound(err) && !pod.DeletionTimestamp.IsZero() {
			klog.Errorf("deleting pod %s/%s dra subnet %s already not exist, gc will clean its ip cr", pod.Namespace, pod.Name, record.Subnet)
			return nil, nil
		}
		klog.Errorf("failed to get subnet %s recorded for claim %s/%s: %v", record.Subnet, claim.Namespace, claim.Name, err)
		return nil, err
	}
	if subnet.Spec.Provider != record.Provider {
		klog.Warningf("subnet %s changed its provider to %s, the dra nic of claim %s/%s on device %s keeps provider %s it was allocated with",
			subnet.Name, subnet.Spec.Provider, claim.Namespace, claim.Name, draNicKey(alloc), record.Provider)
	}
	return draNet(pod, claim, alloc, subnet, record.Provider, c.config.DRANicDriverName), nil
}

func draNet(pod *v1.Pod, claim *resourcev1.ResourceClaim, alloc resourcev1.DeviceRequestAllocationResult, subnet *kubeovnv1.Subnet, provider, driver string) *kubeovnNet {
	return &kubeovnNet{
		Type:               providerTypeOriginal,
		ProviderName:       provider,
		Subnet:             subnet,
		AllowLiveMigration: pod.Annotations[kubevirtv1.MigrationJobNameAnnotation] != "",
		InterfaceName:      draInterfaceName(claim, alloc.Request, driver),
		IsDRA:              true,
	}
}

// nameDRANets keys nics sharing a provider by their interface name, like repeated Multus
// attachments of one NAD, so their annotations, port and ip CR differ, and rejects duplicates.
func nameDRANets(pod *v1.Pod, nets []*kubeovnNet, driver string) error {
	providerCounts := make(map[string]int, len(nets))
	for _, n := range nets {
		providerCounts[n.ProviderName]++
	}
	for _, n := range nets {
		if providerCounts[n.ProviderName] < 2 {
			continue
		}
		providerName := n.ProviderName + "." + n.InterfaceName
		if n.InterfaceName == "" || len(validation.IsDNS1123Subdomain(providerName)) != 0 {
			err := fmt.Errorf("pod %s/%s has more than one dra nic on provider %s, each needs a distinct interfaceName of lowercase alphanumerics and '-' in its %s claim config, got %q",
				pod.Namespace, pod.Name, n.ProviderName, driver, n.InterfaceName)
			klog.Error(err)
			return err
		}
		n.ProviderName = providerName
	}
	names := strset.NewWithSize(len(nets))
	for _, n := range nets {
		if names.Has(n.ProviderName) {
			err := fmt.Errorf("pod %s/%s has more than one dra nic named %s", pod.Namespace, pod.Name, n.ProviderName)
			klog.Error(err)
			return err
		}
		names.Add(n.ProviderName)
	}
	return nil
}

// draDefaultInterfaceName is the interface name the DRA NIC driver gives a nic without one.
const draDefaultInterfaceName = "net1"

// checkDRAInterfaceNames rejects a dra nic whose interface name the pod already uses for its
// primary interface or a Multus attachment, which Multus names "net<index>" unless the network
// selection requests a name. Both would collide in the pod's network namespace.
func checkDRAInterfaceNames(pod *v1.Pod, draNets []*kubeovnNet) error {
	if len(draNets) == 0 {
		return nil
	}
	used := strset.New("eth0")
	if annotation := pod.Annotations[nadv1.NetworkAttachmentAnnot]; annotation != "" {
		attachments, err := nadutils.ParseNetworkAnnotation(annotation, pod.Namespace)
		if err != nil {
			return fmt.Errorf("failed to parse attach net for pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		for i, attachment := range attachments {
			if attachment.InterfaceRequest != "" {
				used.Add(attachment.InterfaceRequest)
			} else {
				used.Add(fmt.Sprintf("net%d", i+1))
			}
		}
	}
	for _, n := range draNets {
		name := n.InterfaceName
		if name == "" {
			name = draDefaultInterfaceName
		}
		if used.Has(name) {
			return fmt.Errorf("pod %s/%s already uses interface %s for its primary network or a network attachment, set another interfaceName in the claim config of the dra nic on provider %s",
				pod.Namespace, pod.Name, name, n.ProviderName)
		}
	}
	return nil
}

// draInterfaceName returns the interfaceName of the driver's opaque config that applies to the
// request. Configs apply in order, so a later one overrides an earlier one.
// A config of a firstAvailable request also applies to its subrequests, the rule of
// resourceclaim.ConfigForResult, which the DRA NIC driver applies as well.
func draInterfaceName(claim *resourcev1.ResourceClaim, request, driver string) string {
	var name string
	result := resourcev1.DeviceRequestAllocationResult{Request: request}
	for _, config := range resourceclaim.ConfigForResult(claim.Status.Allocation.Devices.Config, result) {
		if config.Opaque.Driver != driver {
			continue
		}
		var params struct {
			InterfaceName string `json:"interfaceName"`
		}
		if err := json.Unmarshal(config.Opaque.Parameters.Raw, &params); err != nil {
			klog.Warningf("failed to parse %s config of claim %s/%s: %v", driver, claim.Namespace, claim.Name, err)
			continue
		}
		if params.InterfaceName != "" {
			name = params.InterfaceName
		}
	}
	return name
}

// checkSharedDRAClaim rejects a claim the pod shares with a pod of another VM. A claim stands for
// one nic with one address, which only the virt-launcher pods of one KubeVirt VM can share: the
// source and the target pod of a live migration, which keep the VM's address (keep-vm-ip).
// Consumers that are already gone are ignored.
func (c *Controller) checkSharedDRAClaim(pod *v1.Pod, claim *resourcev1.ResourceClaim) error {
	_, vmName := isVMPod(pod)
	for _, ref := range claim.Status.ReservedFor {
		if ref.APIGroup != "" || ref.Resource != "pods" || ref.UID == pod.UID {
			continue
		}
		other, err := c.podsLister.Pods(claim.Namespace).Get(ref.Name)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get pod %s/%s sharing claim %s: %w", claim.Namespace, ref.Name, claim.Name, err)
		}
		if other.UID != ref.UID {
			continue
		}
		if _, otherVM := isVMPod(other); vmName == "" || otherVM != vmName {
			return fmt.Errorf("pod %s/%s shares claim %s with pod %s, a dra nic claim can only be shared by the pods of one kubevirt vm",
				pod.Namespace, pod.Name, claim.Name, other.Name)
		}
	}
	return nil
}

// draSliceChanged reports whether a slice update can change how claims resolve: the pool
// generation or the devices other than their capacity, which the DRA NIC driver updates on
// every address allocation.
func draSliceChanged(oldSlice, newSlice *resourcev1.ResourceSlice) bool {
	if oldSlice.Spec.Pool != newSlice.Spec.Pool || len(oldSlice.Spec.Devices) != len(newSlice.Spec.Devices) {
		return true
	}
	for i := range newSlice.Spec.Devices {
		oldDevice, newDevice := oldSlice.Spec.Devices[i], newSlice.Spec.Devices[i]
		if oldDevice.Name != newDevice.Name || !maps.EqualFunc(oldDevice.Attributes, newDevice.Attributes, func(a, b resourcev1.DeviceAttribute) bool {
			return equality.Semantic.DeepEqual(a, b)
		}) {
			return true
		}
	}
	return false
}

// enqueueSlicePods requeues the pods of the claims allocated from the slice's pool, so a pod that
// failed while the pool was incomplete or its device unpublished is resolved again.
func (c *Controller) enqueueSlicePods(slice *resourcev1.ResourceSlice) {
	if slice.Spec.Driver != c.config.DRANicDriverName || c.resourceClaimIndexer == nil {
		return
	}
	objs, err := c.resourceClaimIndexer.ByIndex(draClaimPoolIndex, draPoolKey(slice.Spec.Driver, slice.Spec.Pool.Name))
	if err != nil {
		klog.Errorf("failed to get resource claims of pool %s: %v", slice.Spec.Pool.Name, err)
		return
	}
	for _, obj := range objs {
		if claim, ok := obj.(*resourcev1.ResourceClaim); ok {
			c.enqueueClaimPods(claim)
		}
	}
}

func (c *Controller) enqueueAddResourceSlice(obj any) {
	c.enqueueSlicePods(obj.(*resourcev1.ResourceSlice))
}

func (c *Controller) enqueueUpdateResourceSlice(oldObj, newObj any) {
	newSlice := newObj.(*resourcev1.ResourceSlice)
	if draSliceChanged(oldObj.(*resourcev1.ResourceSlice), newSlice) {
		c.enqueueSlicePods(newSlice)
	}
}

func (c *Controller) enqueueDeleteResourceSlice(obj any) {
	var slice *resourcev1.ResourceSlice
	switch t := obj.(type) {
	case *resourcev1.ResourceSlice:
		slice = t
	case cache.DeletedFinalStateUnknown:
		s, ok := t.Obj.(*resourcev1.ResourceSlice)
		if !ok {
			return
		}
		slice = s
	default:
		return
	}
	c.enqueueSlicePods(slice)
}

// hasPendingDRAClaims reports whether some of the pod's resource claims are not yet allocated
// and reserved for it, so the pod's DRA nics cannot be resolved yet.
func (c *Controller) hasPendingDRAClaims(pod *v1.Pod) bool {
	if !c.config.EnableDRANic || c.resourceClaimIndexer == nil || len(pod.Spec.ResourceClaims) == 0 {
		return false
	}
	objs, err := c.resourceClaimIndexer.ByIndex(draClaimPodIndex, cache.MetaObjectToName(pod).String())
	if err != nil {
		klog.Errorf("failed to get resource claims of pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return true
	}
	ready := 0
	for _, obj := range objs {
		if claim, ok := obj.(*resourcev1.ResourceClaim); ok && claim.Status.Allocation != nil && isClaimReservedForPod(claim, pod.Name, pod.UID) {
			ready++
		}
	}
	return ready < len(pod.Spec.ResourceClaims)
}

// enqueueClaimPods requeues the pods a claim is reserved for. The scheduler allocates and reserves
// claims after the pod was first reconciled, so this is what makes the controller notice new DRA
// nics, the same way an annotation update does for Multus attachments. Claims of other drivers count
// too: the pod's DRA nics are only resolved once all of its claims are ready (hasPendingDRAClaims).
func (c *Controller) enqueueClaimPods(claim *resourcev1.ResourceClaim) {
	if claim.Status.Allocation == nil {
		return
	}
	for _, ref := range claim.Status.ReservedFor {
		if ref.APIGroup == "" && ref.Resource == "pods" {
			key := cache.NewObjectName(claim.Namespace, ref.Name).String()
			klog.V(3).Infof("enqueue update pod %s for resource claim %s/%s", key, claim.Namespace, claim.Name)
			c.addOrUpdatePodQueue.Add(key)
		}
	}
}

func (c *Controller) enqueueAddResourceClaim(obj any) {
	c.enqueueClaimPods(obj.(*resourcev1.ResourceClaim))
}

func (c *Controller) enqueueUpdateResourceClaim(oldObj, newObj any) {
	oldClaim := oldObj.(*resourcev1.ResourceClaim)
	newClaim := newObj.(*resourcev1.ResourceClaim)
	if (oldClaim.Status.Allocation == nil) != (newClaim.Status.Allocation == nil) {
		c.enqueueClaimPods(newClaim)
		return
	}
	if newClaim.Status.Allocation == nil {
		return
	}
	// Only a new consumer has something to notice. kube-controller-manager keeps adding and removing
	// the finished source pod of a migrated VM on a claim the target pod still uses, several times a
	// second, which must not requeue the pods each time.
	for _, ref := range newClaim.Status.ReservedFor {
		if slices.Contains(oldClaim.Status.ReservedFor, ref) || ref.APIGroup != "" || ref.Resource != "pods" {
			continue
		}
		if pod, err := c.podsLister.Pods(newClaim.Namespace).Get(ref.Name); err == nil && pod.UID == ref.UID && !isPodAlive(pod) {
			continue
		}
		key := cache.NewObjectName(newClaim.Namespace, ref.Name).String()
		klog.V(3).Infof("enqueue update pod %s for resource claim %s/%s", key, newClaim.Namespace, newClaim.Name)
		c.addOrUpdatePodQueue.Add(key)
	}
}

func hasDRANetwork(networks []kubevirtv1.Network) bool {
	return slices.ContainsFunc(networks, func(network kubevirtv1.Network) bool { return network.ResourceClaim != nil })
}

// vmIPNames maps "namespace/vm" to the names of the VM's ip CRs, which are also its port names.
func (c *Controller) vmIPNames() (map[string][]string, error) {
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list ips: %w", err)
	}
	result := make(map[string][]string)
	for _, ip := range ips {
		if ip.Spec.PodType != util.KindVirtualMachine {
			continue
		}
		key := cache.NewObjectName(ip.Spec.Namespace, ip.Spec.PodName).String()
		result[key] = append(result[key], ip.Name)
	}
	return result, nil
}
