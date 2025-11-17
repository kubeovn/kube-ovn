package controller

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIPPool(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.IPPool)).String()
	klog.V(3).Infof("enqueue add ippool %s", key)
	c.addOrUpdateIPPoolQueue.Add(key)
}

func (c *Controller) enqueueDeleteIPPool(obj any) {
	var ippool *kubeovnv1.IPPool
	switch t := obj.(type) {
	case *kubeovnv1.IPPool:
		ippool = t
	case cache.DeletedFinalStateUnknown:
		i, ok := t.Obj.(*kubeovnv1.IPPool)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		ippool = i
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete ippool %s", cache.MetaObjectToName(ippool).String())
	c.deleteIPPoolQueue.Add(ippool)
}

func (c *Controller) enqueueUpdateIPPool(oldObj, newObj any) {
	oldIPPool := oldObj.(*kubeovnv1.IPPool)
	newIPPool := newObj.(*kubeovnv1.IPPool)
	if !slices.Equal(oldIPPool.Spec.Namespaces, newIPPool.Spec.Namespaces) ||
		!slices.Equal(oldIPPool.Spec.IPs, newIPPool.Spec.IPs) {
		key := cache.MetaObjectToName(newIPPool).String()
		klog.V(3).Infof("enqueue update ippool %s", key)
		c.addOrUpdateIPPoolQueue.Add(key)
	}
}

func (c *Controller) handleAddOrUpdateIPPool(key string) error {
	c.ippoolKeyMutex.LockKey(key)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(key) }()

	cachedIPPool, err := c.ippoolLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	klog.Infof("handle add/update ippool %s", cachedIPPool.Name)

	ippool := cachedIPPool.DeepCopy()
	if err = c.handleAddIPPoolFinalizer(cachedIPPool); err != nil {
		klog.Errorf("failed to add finalizer for ippool %s: %v", cachedIPPool.Name, err)
		return err
	}
	if err = c.reconcileIPPoolAddressSet(ippool); err != nil {
		klog.Errorf("failed to reconcile address set for ippool %s: %v", ippool.Name, err)
		return err
	}
	ippool.Status.EnsureStandardConditions()
	if err = c.ipam.AddOrUpdateIPPool(ippool.Spec.Subnet, ippool.Name, ippool.Spec.IPs); err != nil {
		klog.Errorf("failed to add/update ippool %s with IPs %v in subnet %s: %v", ippool.Name, ippool.Spec.IPs, ippool.Spec.Subnet, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "UpdateIPAMFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
		}
		return err
	}

	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us := c.ipam.IPPoolStatistics(ippool.Spec.Subnet, ippool.Name)
	ippool.Status.V4AvailableIPs = v4a
	ippool.Status.V4UsingIPs = v4u
	ippool.Status.V6AvailableIPs = v6a
	ippool.Status.V6UsingIPs = v6u
	ippool.Status.V4AvailableIPRange = v4as
	ippool.Status.V4UsingIPRange = v4us
	ippool.Status.V6AvailableIPRange = v6as
	ippool.Status.V6UsingIPRange = v6us

	if err = c.patchIPPoolStatusCondition(ippool, "UpdateIPAMSucceeded", ""); err != nil {
		klog.Error(err)
		return err
	}

	for _, ns := range ippool.Spec.Namespaces {
		c.addNamespaceQueue.Add(ns)
	}

	return nil
}

func (c *Controller) handleDeleteIPPool(ippool *kubeovnv1.IPPool) error {
	c.ippoolKeyMutex.LockKey(ippool.Name)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(ippool.Name) }()

	klog.Infof("handle delete ippool %s", ippool.Name)
	c.ipam.RemoveIPPool(ippool.Spec.Subnet, ippool.Name)
	if err := c.OVNNbClient.DeleteAddressSet(ippoolAddressSetName(ippool.Name)); err != nil {
		klog.Errorf("failed to delete address set for ippool %s: %v", ippool.Name, err)
		return err
	}

	namespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces: %v", err)
		return err
	}

	for _, ns := range namespaces {
		if len(ns.Annotations) == 0 {
			continue
		}
		if ns.Annotations[util.IPPoolAnnotation] == ippool.Name {
			c.enqueueAddNamespace(ns)
		}
	}

	if err := c.handleDelIPPoolFinalizer(ippool); err != nil {
		klog.Errorf("failed to remove finalizer for ippool %s: %v", ippool.Name, err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateIPPoolStatus(key string) error {
	c.ippoolKeyMutex.LockKey(key)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(key) }()

	cachedIPPool, err := c.ippoolLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	ippool := cachedIPPool.DeepCopy()
	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us := c.ipam.IPPoolStatistics(ippool.Spec.Subnet, ippool.Name)
	ippool.Status.V4AvailableIPs = v4a
	ippool.Status.V4UsingIPs = v4u
	ippool.Status.V6AvailableIPs = v6a
	ippool.Status.V6UsingIPs = v6u
	ippool.Status.V4AvailableIPRange = v4as
	ippool.Status.V4UsingIPRange = v4us
	ippool.Status.V6AvailableIPRange = v6as
	ippool.Status.V6UsingIPRange = v6us
	if reflect.DeepEqual(ippool.Status, cachedIPPool.Status) {
		return nil
	}

	return c.patchIPPoolStatus(ippool)
}

func (c Controller) patchIPPoolStatusCondition(ippool *kubeovnv1.IPPool, reason, errMsg string) error {
	if errMsg != "" {
		ippool.Status.SetError(reason, errMsg)
		ippool.Status.NotReady(reason, errMsg)
		c.recorder.Eventf(ippool, corev1.EventTypeWarning, reason, errMsg)
	} else {
		ippool.Status.Ready(reason, "")
		c.recorder.Eventf(ippool, corev1.EventTypeNormal, reason, errMsg)
	}

	return c.patchIPPoolStatus(ippool)
}

func (c Controller) patchIPPoolStatus(ippool *kubeovnv1.IPPool) error {
	bytes, err := ippool.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to generate json representation for status of ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch status of ippool %s: %v", ippool.Name, err)
		return err
	}

	return nil
}

func (c *Controller) syncIPPoolFinalizer(cl client.Client) error {
	ippools := &kubeovnv1.IPPoolList{}
	return migrateFinalizers(cl, ippools, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(ippools.Items) {
			return nil, nil
		}
		return ippools.Items[i].DeepCopy(), ippools.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIPPoolFinalizer(ippool *kubeovnv1.IPPool) error {
	if ippool == nil || !ippool.DeletionTimestamp.IsZero() {
		return nil
	}
	if controllerutil.ContainsFinalizer(ippool, util.KubeOVNControllerFinalizer) {
		return nil
	}

	newIPPool := ippool.DeepCopy()
	controllerutil.AddFinalizer(newIPPool, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(ippool, newIPPool)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ippool %s: %v", ippool.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIPPoolFinalizer(ippool *kubeovnv1.IPPool) error {
	if ippool == nil || len(ippool.GetFinalizers()) == 0 {
		return nil
	}

	newIPPool := ippool.DeepCopy()
	controllerutil.RemoveFinalizer(newIPPool, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newIPPool, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(ippool, newIPPool)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ippool %s: %v", ippool.Name, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileIPPoolAddressSet(ippool *kubeovnv1.IPPool) error {
	asName := ippoolAddressSetName(ippool.Name)

	if !ippool.Spec.EnableAddressSet {
		if err := c.OVNNbClient.DeleteAddressSet(asName); err != nil {
			err = fmt.Errorf("failed to delete address set %s: %w", asName, err)
			klog.Error(err)
			return err
		}
		return nil
	}

	addresses, err := expandIPPoolAddresses(ippool.Spec.IPs)
	if err != nil {
		err = fmt.Errorf("failed to build address set entries for ippool %s: %v", ippool.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.OVNNbClient.CreateAddressSet(asName, map[string]string{ippoolKey: ippool.Name}); err != nil {
		err = fmt.Errorf("failed to create address set for ippool %s: %v", ippool.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.OVNNbClient.AddressSetUpdateAddress(asName, addresses...); err != nil {
		err = fmt.Errorf("failed to update address set for ippool %s: %v", ippool.Name, err)
		klog.Error(err)
		return err
	}

	return nil
}

func expandIPPoolAddresses(entries []string) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{})
	addresses := make([]string, 0, len(entries))

	for _, raw := range entries {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		switch {
		case strings.Contains(value, ".."):
			parts := strings.Split(value, "..")
			if len(parts) != 2 {
				err := fmt.Errorf("invalid IP range %q", value)
				klog.Error(err)
				return nil, err
			}

			start, err := normalizeIP(parts[0])
			if err != nil {
				err = fmt.Errorf("invalid range start %q: %w", parts[0], err)
				klog.Error(err)
				return nil, err
			}
			end, err := normalizeIP(parts[1])
			if err != nil {
				err = fmt.Errorf("invalid range end %q: %w", parts[1], err)
				klog.Error(err)
				return nil, err
			}
			if (start.To4() != nil) != (end.To4() != nil) {
				klog.Errorf("range %q mixes IPv4 and IPv6 addresses", value)
				err = fmt.Errorf("range %q mixes IPv4 and IPv6 addresses", value)
				return nil, err
			}
			if compareIP(start, end) > 0 {
				klog.Errorf("range %q start is greater than end", value)
				err = fmt.Errorf("range %q start is greater than end", value)
				return nil, err
			}

			cidrs, err := ipRangeToCIDRs(start, end)
			if err != nil {
				err = fmt.Errorf("failed to convert IP range %q to CIDRs: %w", value, err)
				klog.Error(err)
				return nil, err
			}
			for _, cidr := range cidrs {
				if _, exists := seen[cidr]; exists {
					continue
				}
				seen[cidr] = struct{}{}
				addresses = append(addresses, cidr)
			}
		case strings.Contains(value, "/"):
			_, network, err := net.ParseCIDR(value)
			if err != nil {
				err = fmt.Errorf("invalid CIDR %q: %w", value, err)
				klog.Error(err)
				return nil, err
			}
			canonical := network.String()
			if _, exists := seen[canonical]; !exists {
				seen[canonical] = struct{}{}
				addresses = append(addresses, canonical)
			}
		default:
			ip, err := normalizeIP(value)
			if err != nil {
				err = fmt.Errorf("invalid IP address %q: %w", value, err)
				klog.Error(err)
				return nil, err
			}
			canonical := ip.String()
			if _, exists := seen[canonical]; !exists {
				seen[canonical] = struct{}{}
				addresses = append(addresses, canonical)
			}
		}
	}

	sort.Strings(addresses)
	return addresses, nil
}

func normalizeIP(value string) (net.IP, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		err := fmt.Errorf("invalid IP address %q", value)
		klog.Error(err)
		return nil, err
	}
	if v4 := ip.To4(); v4 != nil {
		return v4, nil
	}
	return ip.To16(), nil
}

func ipRangeToCIDRs(start, end net.IP) ([]string, error) {
	length := net.IPv4len
	totalBits := 32
	if start.To4() == nil {
		length = net.IPv6len
		totalBits = 128
	}

	startInt := ipToBigInt(start)
	endInt := ipToBigInt(end)
	if startInt.Cmp(endInt) > 0 {
		err := fmt.Errorf("range %q start is greater than end", fmt.Sprintf("%s..%s", start, end))
		klog.Error(err)
		return nil, err
	}

	result := make([]string, 0)
	tmp := new(big.Int)
	for startInt.Cmp(endInt) <= 0 {
		zeros := countTrailingZeros(startInt, totalBits)
		diff := tmp.Sub(endInt, startInt)
		diff.Add(diff, big.NewInt(1))

		var maxDiff uint
		if diff.Sign() > 0 {
			maxDiff = uint(diff.BitLen() - 1)
		}

		size := zeros
		if maxDiff < size {
			size = maxDiff
		}

		prefix := totalBits - int(size)
		networkInt := new(big.Int).Set(startInt)
		networkIP := bigIntToIP(networkInt, length)
		network := &net.IPNet{IP: networkIP, Mask: net.CIDRMask(prefix, totalBits)}
		result = append(result, network.String())

		increment := new(big.Int).Lsh(big.NewInt(1), size)
		startInt.Add(startInt, increment)
	}

	return result, nil
}

func ipToBigInt(ip net.IP) *big.Int {
	return new(big.Int).SetBytes(ip)
}

func bigIntToIP(value *big.Int, length int) net.IP {
	bytes := value.Bytes()
	if len(bytes) < length {
		padded := make([]byte, length)
		copy(padded[length-len(bytes):], bytes)
		bytes = padded
	} else if len(bytes) > length {
		bytes = bytes[len(bytes)-length:]
	}

	ip := make(net.IP, length)
	copy(ip, bytes)
	if length == net.IPv4len {
		return net.IP(ip).To4()
	}
	return net.IP(ip)
}

func countTrailingZeros(value *big.Int, totalBits int) uint {
	if value.Sign() == 0 {
		return uint(totalBits)
	}
	var zeros uint
	for zeros < uint(totalBits) && value.Bit(int(zeros)) == 0 {
		zeros++
	}
	return zeros
}

func compareIP(a, b net.IP) int {
	return bytes.Compare(a, b)
}

func ippoolAddressSetName(name string) string {
	return strings.ReplaceAll(name, "-", ".")
}
