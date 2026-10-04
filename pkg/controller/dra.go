package controller

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/scylladb/go-set/strset"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	// draClaimPodIndex indexes ResourceClaims by the "namespace/name" of every pod in status.reservedFor.
	// The names of claims generated from templates are only in the pod's status.resourceClaimStatuses,
	// which the pod informer trims, so the claim side holds the pod-to-claim mapping.
	draClaimPodIndex = "draReservedForPod"
	// draSlicePoolIndex indexes ResourceSlices by "driver/pool".
	draSlicePoolIndex = "draDriverPool"

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
// they are still in the cache, so a device only found there is not published anymore.
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
		for _, alloc := range claim.Status.Allocation.Devices.Results {
			if alloc.Driver != c.config.DRANicDriverName {
				continue
			}
			net, err := c.draNetForAllocation(pod, claim, alloc)
			if err != nil {
				return nil, err
			}
			if net != nil {
				result = append(result, net)
			}
		}
	}
	if err := nameDRANets(pod, result, c.config.DRANicDriverName); err != nil {
		return nil, err
	}
	return result, nil
}

// draNetForAllocation resolves one allocated device to the subnet its subnetName attribute names.
// It returns nil without an error for a deleting pod whose device or subnet is already gone.
func (c *Controller) draNetForAllocation(pod *v1.Pod, claim *resourcev1.ResourceClaim, alloc resourcev1.DeviceRequestAllocationResult) (*kubeovnNet, error) {
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

	netType := providerTypeIPAM
	if isOvnSubnet(subnet) {
		netType = providerTypeOriginal
	}
	return &kubeovnNet{
		Type:               netType,
		ProviderName:       subnet.Spec.Provider,
		Subnet:             subnet,
		AllowLiveMigration: pod.Annotations[kubevirtv1.MigrationJobNameAnnotation] != "",
		InterfaceName:      draInterfaceName(claim, alloc.Request, driver),
		IsDRA:              true,
	}, nil
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

// draInterfaceName returns the interfaceName of the driver's opaque config that applies to the
// request. Configs apply in order, so a later one overrides an earlier one.
func draInterfaceName(claim *resourcev1.ResourceClaim, request, driver string) string {
	parent, _, _ := strings.Cut(request, "/")
	var name string
	for _, config := range claim.Status.Allocation.Devices.Config {
		if config.Opaque == nil || config.Opaque.Driver != driver {
			continue
		}
		if len(config.Requests) != 0 && !slices.Contains(config.Requests, request) && !slices.Contains(config.Requests, parent) {
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
	if (oldClaim.Status.Allocation == nil) == (newClaim.Status.Allocation == nil) &&
		slices.Equal(oldClaim.Status.ReservedFor, newClaim.Status.ReservedFor) {
		return
	}
	c.enqueueClaimPods(newClaim)
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
