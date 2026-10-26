package controller

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func providerNetworkNodeAddress(ip *kubeovnv1.IP, phase kubeovnv1.ProviderNetworkAddressPhase) *kubeovnv1.ProviderNetworkNodeAddress {
	return &kubeovnv1.ProviderNetworkNodeAddress{
		Node: ip.Spec.NodeName, Subnet: ip.Spec.Subnet, IPAddress: ip.Spec.IPAddress,
		IPName: ip.Name, IPUID: string(ip.UID), Phase: phase,
	}
}

// Discover IPs even when an earlier status update failed. Status is only authoritative
// for cleanup acknowledgements, which must match the exact IP resource UID.
func (c *Controller) providerNetworkAddresses(pn *kubeovnv1.ProviderNetwork) (map[string]kubeovnv1.ProviderNetworkNodeAddress, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ips, err := c.config.KubeOvnClient.KubeovnV1().IPs().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	addresses := make(map[string]kubeovnv1.ProviderNetworkNodeAddress)
	for _, address := range pn.Status.VlanSubinterfaceAddresses {
		addresses[address.Node] = address
	}
	for i := range ips.Items {
		ip := &ips.Items[i]
		if providerNetworkOwnsNodeIP(pn, ip.Spec.NodeName, ip) {
			addresses[ip.Spec.NodeName] = *providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressAllocated)
		}
	}
	return addresses, nil
}

func providerNetworkCleanupAddress(pn *kubeovnv1.ProviderNetwork, ip *kubeovnv1.IP) *kubeovnv1.ProviderNetworkNodeAddress {
	address := providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressCleanupPending)
	for _, previous := range pn.Status.VlanSubinterfaceAddresses {
		if previous.IPName == ip.Name && previous.IPUID == string(ip.UID) && previous.Node == ip.Spec.NodeName &&
			previous.Subnet == ip.Spec.Subnet && previous.IPAddress == ip.Spec.IPAddress &&
			(previous.Phase == kubeovnv1.ProviderNetworkAddressCleanupPending || previous.Phase == kubeovnv1.ProviderNetworkAddressCleanupSucceeded) {
			// Preserve daemon-owned observations, but derive the phase ourselves.
			address.CleanupAcknowledgedIPUID = previous.CleanupAcknowledgedIPUID
			address.Message = previous.Message
			if address.IPUID != "" && address.CleanupAcknowledgedIPUID == address.IPUID {
				address.Phase = kubeovnv1.ProviderNetworkAddressCleanupSucceeded
			}
			break
		}
	}
	return address
}

func (c *Controller) reconcileProviderNetworkAddressCleanup(pn *kubeovnv1.ProviderNetwork, node string) (*kubeovnv1.ProviderNetworkNodeAddress, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := util.ProviderNetworkAddressName(pn.Name, node)
	ip, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider network address %s: %w", name, err)
	}
	if !providerNetworkOwnsNodeIP(pn, node, ip) {
		return nil, fmt.Errorf("provider network IP %s does not belong to provider network %s and node %s", name, pn.Name, node)
	}
	address := providerNetworkCleanupAddress(pn, ip)
	if ip.DeletionTimestamp.IsZero() {
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(ctx, name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &ip.UID},
		}); err != nil && !k8serrors.IsNotFound(err) {
			return address, fmt.Errorf("failed to delete provider network address %s: %w", name, err)
		}
	}
	if address.Phase == kubeovnv1.ProviderNetworkAddressCleanupSucceeded {
		c.updateIPQueue.Add(ip.Name)
	}
	return address, nil
}

func (c *Controller) reconcileProviderNetworkNodeAddress(pn *kubeovnv1.ProviderNetwork, node string) (*kubeovnv1.ProviderNetworkNodeAddress, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := util.ProviderNetworkAddressName(pn.Name, node)
	if errs := validation.IsDNS1123Subdomain(name); len(errs) != 0 {
		return nil, fmt.Errorf("invalid provider network IP name %q: %s", name, strings.Join(errs, "; "))
	}
	ips := c.config.KubeOvnClient.KubeovnV1().IPs()
	ip, err := ips.Get(ctx, name, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get provider network address %s: %w", name, err)
	}
	if err == nil {
		if !providerNetworkOwnsNodeIP(pn, node, ip) {
			return nil, fmt.Errorf("provider network IP %s does not belong to provider network %s and node %s", name, pn.Name, node)
		}
		cleanup := providerNetworkCleanupAddress(pn, ip)
		cleanupStarted := false
		for _, previous := range pn.Status.VlanSubinterfaceAddresses {
			if previous.IPName == ip.Name && previous.IPUID == string(ip.UID) &&
				(previous.Phase == kubeovnv1.ProviderNetworkAddressCleanupPending || previous.Phase == kubeovnv1.ProviderNetworkAddressCleanupSucceeded) {
				cleanupStarted = true
			}
		}
		if !ip.DeletionTimestamp.IsZero() || ip.Spec.Subnet != pn.Spec.DefaultInterfaceSubnet || cleanupStarted {
			return c.reconcileProviderNetworkAddressCleanup(pn, node)
		}
		for address := range strings.SplitSeq(ip.Spec.IPAddress, ",") {
			if net.ParseIP(address) == nil {
				return nil, fmt.Errorf("provider network IP %s has invalid address %q", name, ip.Spec.IPAddress)
			}
		}
		cleanup.Phase = kubeovnv1.ProviderNetworkAddressAllocated
		return cleanup, nil
	}

	v4IP, v6IP, mac, err := c.ipam.GetRandomAddress(name, name, nil, pn.Spec.DefaultInterfaceSubnet, "", nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate address for node %s from subnet %s: %w", node, pn.Spec.DefaultInterfaceSubnet, err)
	}
	address := util.GetStringIP(v4IP, v6IP)
	if err := c.createOrUpdateIPCR("", pn.Name, address, mac, pn.Spec.DefaultInterfaceSubnet, "", node, util.KindProviderNetwork); err != nil {
		// Keep the reservation on an ambiguous API failure.
		return nil, err
	}
	ip, err = ips.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if !providerNetworkOwnsNodeIP(pn, node, ip) || !ip.DeletionTimestamp.IsZero() || ip.Spec.IPAddress != address || ip.Spec.Subnet != pn.Spec.DefaultInterfaceSubnet {
		return nil, fmt.Errorf("provider network IP %s changed during allocation", name)
	}
	return providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressAllocated), nil
}

func (c *Controller) providerNetworkIPCleanupComplete(ip *kubeovnv1.IP) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, owner := range ip.OwnerReferences {
		if owner.Kind != util.KindProviderNetwork || owner.APIVersion != kubeovnv1.SchemeGroupVersion.String() {
			continue
		}
		pn, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(ctx, owner.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("cannot confirm cleanup of IP %s: %w", ip.Name, err)
		}
		if pn.UID != owner.UID {
			return fmt.Errorf("provider network owner changed for IP %s", ip.Name)
		}
		for _, address := range pn.Status.VlanSubinterfaceAddresses {
			if address.IPName == ip.Name && address.IPUID == string(ip.UID) && address.Node == ip.Spec.NodeName &&
				address.Subnet == ip.Spec.Subnet && address.IPAddress == ip.Spec.IPAddress &&
				address.Phase == kubeovnv1.ProviderNetworkAddressCleanupSucceeded &&
				address.IPUID != "" && address.CleanupAcknowledgedIPUID == address.IPUID {
				return nil
			}
		}
		return fmt.Errorf("waiting for node %s to confirm cleanup of IP %s", ip.Spec.NodeName, ip.Name)
	}
	return fmt.Errorf("provider network owner is missing for IP %s", ip.Name)
}
