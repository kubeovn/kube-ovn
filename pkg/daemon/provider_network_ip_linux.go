package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Remove allocated VLAN addresses before bridge teardown. Unlike pre-existing host
// addresses, these IPAM-managed addresses must not be transferred back to the NIC.
// The daemon acknowledges cleanup in ProviderNetwork status before IPAM release.
func (c *Controller) cleanupProviderNetworkVlanSubinterfaceAddress(pn *kubeovnv1.ProviderNetwork, node *corev1.Node) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var addresses []string
	for _, allocation := range pn.Status.VlanSubinterfaceAddresses {
		if allocation.Node == node.Name {
			addresses = append(addresses, strings.Split(allocation.IPAddress, ",")...)
		}
	}
	if len(addresses) == 0 {
		// Status may temporarily omit an unconfirmed allocation. Check the IP CR
		// before teardown so its address cannot be transferred back to the NIC.
		ip, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(ctx, util.ProviderNetworkAddressName(pn.Name, node.Name), metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to check provider network allocation before teardown: %w", err)
		}
		owned := false
		for _, owner := range ip.OwnerReferences {
			if owner.APIVersion == kubeovnv1.SchemeGroupVersion.String() && owner.Kind == util.KindProviderNetwork && owner.Name == pn.Name && owner.UID == pn.UID {
				owned = true
			}
		}
		if !owned || ip.Spec.NodeName != node.Name || ip.Spec.PodType != util.KindProviderNetwork || ip.Spec.IPAddress == "" {
			return fmt.Errorf("provider network IP %s does not match the allocation being cleaned up", ip.Name)
		}
		addresses = strings.Split(ip.Spec.IPAddress, ",")
	}

	mappings, err := getOvnMappings("ovn-bridge-mappings")
	if err != nil {
		return fmt.Errorf("failed to get bridge mapping for provider network %s: %w", pn.Name, err)
	}
	bridgeName := mappings[pn.Name]
	if bridgeName == "" {
		bridgeName = util.ExternalBridgeName(pn.Name)
	}
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to get provider network bridge %s: %w", bridgeName, err)
	}
	return removeProviderNetworkVlanAddresses(bridge, addresses)
}

// Cleanup and initialization share the same local lock, so an acknowledged address
// cannot be reapplied by a concurrent provider network operation.
// The boolean result indicates whether normal initialization must be skipped.
func (c *Controller) processProviderNetworkAddressCleanup(pn *kubeovnv1.ProviderNetwork, node *corev1.Node) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c.providerNetworkMutex.Lock()
	defer c.providerNetworkMutex.Unlock()

	skipInitialization := false
	for _, address := range pn.Status.VlanSubinterfaceAddresses {
		if address.Node != node.Name || (address.Phase != kubeovnv1.ProviderNetworkAddressCleanupPending &&
			address.Phase != kubeovnv1.ProviderNetworkAddressCleanupSucceeded) {
			continue
		}
		skipInitialization = true
		if address.Phase == kubeovnv1.ProviderNetworkAddressCleanupSucceeded {
			continue
		}
		if address.IPUID == "" || address.IPName == "" {
			return true, fmt.Errorf("cleanup allocation for node %s has no IP identity", node.Name)
		}
		current, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		pending := false
		if current.UID == pn.UID {
			for _, a := range current.Status.VlanSubinterfaceAddresses {
				if a.IPName == address.IPName && a.IPUID == address.IPUID && a.Node == address.Node &&
					a.IPAddress == address.IPAddress && a.Subnet == address.Subnet && a.Phase == kubeovnv1.ProviderNetworkAddressCleanupPending {
					if a.CleanupAcknowledgedIPUID == a.IPUID {
						return true, nil
					}
					pending = true
				}
			}
		}
		if !pending {
			return true, nil
		}
		cleanupPN := pn.DeepCopy()
		cleanupPN.Status.VlanSubinterfaceAddresses = []kubeovnv1.ProviderNetworkNodeAddress{address}
		if err := c.cleanupProviderNetworkVlanSubinterfaceAddress(cleanupPN, node); err != nil {
			if statusErr := c.acknowledgeProviderNetworkAddressCleanup(pn, address, err.Error()); statusErr != nil {
				return true, errors.Join(err, statusErr)
			}
			return true, err
		}
		patch, err := json.Marshal(map[string]any{"metadata": map[string]any{
			"labels": map[string]any{fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name): nil},
		}})
		if err != nil {
			return true, err
		}
		if _, err := c.config.KubeClient.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return true, err
		}
		if err := c.acknowledgeProviderNetworkAddressCleanup(pn, address, ""); err != nil {
			return true, err
		}
	}
	return skipInitialization, nil
}

func (c *Controller) acknowledgeProviderNetworkAddressCleanup(pn *kubeovnv1.ProviderNetwork, address kubeovnv1.ProviderNetworkNodeAddress, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if address.IPUID == "" {
		return errors.New("cannot acknowledge cleanup without an IP UID")
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.UID != pn.UID {
			return fmt.Errorf("provider network %s was replaced during cleanup", pn.Name)
		}
		for i := range current.Status.VlanSubinterfaceAddresses {
			a := &current.Status.VlanSubinterfaceAddresses[i]
			if a.Node != c.config.NodeName || a.IPUID != address.IPUID || a.IPName != address.IPName ||
				a.IPAddress != address.IPAddress || a.Subnet != address.Subnet || a.Phase != kubeovnv1.ProviderNetworkAddressCleanupPending {
				continue
			}
			// A successful acknowledgement is monotonic: a retried failure must
			// not overwrite it. Only the controller advances the lifecycle phase.
			if a.CleanupAcknowledgedIPUID == a.IPUID {
				return nil
			}
			a.Message = message
			if message == "" {
				a.CleanupAcknowledgedIPUID = a.IPUID
			}
			_, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(ctx, current, metav1.UpdateOptions{})
			return err
		}
		return fmt.Errorf("cleanup request for IP %s UID %s is no longer pending", address.IPName, address.IPUID)
	})
}

// Reconcile masks only for the allocated IPs. Other host addresses must remain
// untouched, and retrying an unchanged allocation must not delete it first.
func configureProviderNetworkVlanAddresses(bridge netlink.Link, desiredAddresses string) error {
	desiredMasks := make(map[string]string)
	var desired []*netlink.Addr
	for address := range strings.SplitSeq(desiredAddresses, ",") {
		if address == "" {
			continue
		}
		addr, err := netlink.ParseAddr(address)
		if err != nil {
			return fmt.Errorf("failed to parse VLAN subinterface address %s: %w", address, err)
		}
		desiredMasks[addr.IP.String()] = addr.IPNet.String()
		desired = append(desired, addr)
	}
	currentAddresses, err := util.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list addresses on provider network bridge %s: %w", bridge.Attrs().Name, err)
	}
	for _, address := range currentAddresses {
		desiredCIDR, allocated := desiredMasks[address.IP.String()]
		if !allocated || address.IPNet.String() == desiredCIDR {
			continue
		}
		if err := deleteProviderNetworkVlanAddress(bridge, &address); err != nil {
			return fmt.Errorf("failed to remove stale VLAN address %s from bridge %s: %w", address.String(), bridge.Attrs().Name, err)
		}
		klog.Infof("Removed stale VLAN address %s from provider network bridge %s", address.String(), bridge.Attrs().Name)
	}
	for _, address := range desired {
		if err := netlink.AddrReplace(bridge, address); err != nil {
			return fmt.Errorf("failed to configure address %s on provider network bridge %s: %w", address.String(), bridge.Attrs().Name, err)
		}
		klog.Infof("Configured VLAN subinterface address %s on provider network bridge %s", address.String(), bridge.Attrs().Name)
	}
	return nil
}

// Linux otherwise removes secondary IPv4 addresses in the same subnet when the
// primary address is deleted. Preserve them without permanently changing sysctls.
func deleteProviderNetworkVlanAddress(bridge netlink.Link, address *netlink.Addr) (err error) {
	if address.IP.To4() != nil {
		key := "net/ipv4/conf/" + bridge.Attrs().Name + "/promote_secondaries"
		previous, readErr := sysctl.Sysctl(key)
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", key, readErr)
		}
		if previous != "1" {
			if _, err := sysctl.Sysctl(key, "1"); err != nil {
				return fmt.Errorf("failed to enable %s: %w", key, err)
			}
			defer func() {
				if _, restoreErr := sysctl.Sysctl(key, previous); restoreErr != nil {
					err = errors.Join(err, fmt.Errorf("failed to restore %s: %w", key, restoreErr))
				}
			}()
		}
	}
	if err := netlink.AddrDel(bridge, address); err != nil && !errors.Is(err, unix.EADDRNOTAVAIL) {
		return err
	}
	return nil
}

func removeProviderNetworkVlanAddresses(bridge netlink.Link, addresses []string) error {
	allocatedIPs := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if address == "" {
			continue
		}
		ip, _, _ := strings.Cut(address, "/")
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return fmt.Errorf("invalid provider network VLAN address %q", address)
		}
		allocatedIPs[parsed.String()] = struct{}{}
	}
	currentAddresses, err := util.AddrList(bridge, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list addresses on provider network bridge %s: %w", bridge.Attrs().Name, err)
	}
	for _, address := range currentAddresses {
		if _, allocated := allocatedIPs[address.IP.String()]; !allocated {
			continue
		}
		if err := deleteProviderNetworkVlanAddress(bridge, &address); err != nil {
			return fmt.Errorf("failed to remove VLAN address %s from bridge %s: %w", address.String(), bridge.Attrs().Name, err)
		}
		klog.Infof("Removed allocated VLAN address %s from provider network bridge %s before teardown", address.String(), bridge.Attrs().Name)
	}
	return nil
}
