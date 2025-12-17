package controller

import (
	"context"
	"fmt"
	"net"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddDNSNameResolver(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.DNSNameResolver)).String()
	klog.V(3).Infof("enqueue add dns name resolver %s", key)
	c.addOrUpdateDNSNameResolverQueue.Add(key)
}

func (c *Controller) enqueueUpdateDNSNameResolver(oldObj, newObj any) {
	oldDNSNameResolver := oldObj.(*kubeovnv1.DNSNameResolver)
	newDNSNameResolver := newObj.(*kubeovnv1.DNSNameResolver)

	if !isDNSNameResolverStatusEqual(oldDNSNameResolver.Status, newDNSNameResolver.Status) {
		key := cache.MetaObjectToName(newDNSNameResolver).String()
		klog.V(3).Infof("enqueue update dns name resolver %s due to status change", key)
		c.addOrUpdateDNSNameResolverQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteDNSNameResolver(obj any) {
	var dnsNameResolver *kubeovnv1.DNSNameResolver
	switch t := obj.(type) {
	case *kubeovnv1.DNSNameResolver:
		dnsNameResolver = t
	case cache.DeletedFinalStateUnknown:
		d, ok := t.Obj.(*kubeovnv1.DNSNameResolver)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		dnsNameResolver = d
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete dns name resolver %s", cache.MetaObjectToName(dnsNameResolver).String())
	c.deleteDNSNameResolverQueue.Add(dnsNameResolver.DeepCopy())
}

func (c *Controller) handleAddOrUpdateDNSNameResolver(key string) error {
	klog.Infof("DNSNameResolver add/update handler called for key: %s", key)

	dnsNameResolver, err := c.dnsNameResolversLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(3).Infof("DNSNameResolver %s not found, skipping", key)
			return nil
		}
		return fmt.Errorf("failed to get DNSNameResolver %s: %w", key, err)
	}

	anpName, exists := dnsNameResolver.Labels[adminNetworkPolicyKey]
	if !exists {
		klog.Warningf("DNSNameResolver %s does not have ANP label, skipping", key)
		return nil
	}

	domainName := string(dnsNameResolver.Spec.Name)
	v4Addresses, v6Addresses, err := getResolvedAddressesFromDNSNameResolver(dnsNameResolver)
	if err != nil {
		return fmt.Errorf("failed to get resolved addresses from DNSNameResolver: %w", err)
	}

	allAddresses := append(v4Addresses, v6Addresses...)
	klog.V(3).Infof("DNSNameResolver %s resolved addresses for %s: %v", key, domainName, allAddresses)

	c.updateAnpQueue.Add(&AdminNetworkPolicyChangedDelta{key: anpName, field: ChangedEgressRule, DNSReconcileDone: true})
	c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{key: anpName, field: ChangedEgressRule, DNSReconcileDone: true})
	klog.V(3).Infof("Triggered ANP %s re-sync after DNSNameResolver %s update", anpName, key)

	return nil
}

func (c *Controller) handleDeleteDNSNameResolver(dnsNameResolver *kubeovnv1.DNSNameResolver) error {
	klog.Infof("DNSNameResolver delete handler called for: %s", dnsNameResolver.Name)

	anpName, exists := dnsNameResolver.Labels[adminNetworkPolicyKey]
	if !exists {
		klog.Warningf("DNSNameResolver %s does not have ANP label, skipping", dnsNameResolver.Name)
		return nil
	}

	domainName := string(dnsNameResolver.Spec.Name)
	klog.V(3).Infof("DNSNameResolver %s deleted for domain %s", dnsNameResolver.Name, domainName)

	c.updateAnpQueue.Add(&AdminNetworkPolicyChangedDelta{key: anpName, field: ChangedEgressRule, DNSReconcileDone: true})
	c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{key: anpName, field: ChangedEgressRule, DNSReconcileDone: true})
	klog.V(3).Infof("Triggered ANP %s re-sync after DNSNameResolver %s deletion", anpName, dnsNameResolver.Name)

	return nil
}

func getResolvedAddressesFromDNSNameResolver(dnsNameResolver *kubeovnv1.DNSNameResolver) ([]string, []string, error) {
	var v4Addresses, v6Addresses []string

	if len(dnsNameResolver.Status.ResolvedNames) == 0 {
		klog.V(3).Infof("No resolved names found in DNSNameResolver %s status", dnsNameResolver.Name)
		return v4Addresses, v6Addresses, nil
	}

	for _, resolvedName := range dnsNameResolver.Status.ResolvedNames {
		if resolvedName.ResolutionFailures > 0 {
			klog.Warningf("DNSNameResolver %s has %d resolution failures for %s",
				dnsNameResolver.Name, resolvedName.ResolutionFailures, resolvedName.DNSName)
			continue
		}

		for _, resolvedAddr := range resolvedName.ResolvedAddresses {
			ip := net.ParseIP(resolvedAddr.IP)
			if ip == nil {
				klog.Warningf("Invalid IP address in DNSNameResolver %s: %s", dnsNameResolver.Name, resolvedAddr.IP)
				continue
			}

			if ip.To4() != nil {
				v4Addresses = append(v4Addresses, resolvedAddr.IP)
			} else {
				v6Addresses = append(v6Addresses, resolvedAddr.IP)
			}
		}
	}

	klog.V(3).Infof("Extracted from DNSNameResolver %s: IPv4=%v, IPv6=%v",
		dnsNameResolver.Name, v4Addresses, v6Addresses)
	return v4Addresses, v6Addresses, nil
}

// reconcileDNSNameResolversForANP reconciles DNSNameResolver CRs for an ANP
// It ensures that only the desired domain names have corresponding DNSNameResolvers
func (c *Controller) reconcileDNSNameResolversForANP(anpName string, desiredDomainNames []string) error {
	return c.reconcileDNSNameResolversForNP(anpName, desiredDomainNames, adminNetworkPolicyKey)
}

func (c *Controller) reconcileDNSNameResolversForNP(npName string, desiredDomainNames []string, key string) error {
	// Get existing DNSNameResolvers for this NP
	labelSelector := labels.SelectorFromSet(labels.Set{key: npName})
	existingDNSResolvers, err := c.dnsNameResolversLister.List(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list existing DNSNameResolvers for NP %s: %w", npName, err)
	}

	// Create sets for comparison
	desiredDomainSet := strset.New(desiredDomainNames...)
	existingDomainSet := strset.New()

	// Build set of existing domains from DNSNameResolvers
	for _, dnsResolver := range existingDNSResolvers {
		existingDomainSet.Add(string(dnsResolver.Spec.Name))
	}

	// Find domains to delete (exist in DNSNameResolvers but not in desired list)
	domainsToDelete := strset.Difference(existingDomainSet, desiredDomainSet)

	// Find domains to create (exist in desired list but not in DNSNameResolvers)
	domainsToCreate := strset.Difference(desiredDomainSet, existingDomainSet)

	// Delete obsolete DNSNameResolvers
	for _, domainName := range domainsToDelete.List() {
		if err := c.deleteDNSNameResolver(npName, domainName); err != nil {
			return fmt.Errorf("failed to delete DNSNameResolver for domain %s: %w", domainName, err)
		}
	}

	// Create new DNSNameResolvers
	for _, domainName := range domainsToCreate.List() {
		if err := c.createOrUpdateDNSNameResolver(npName, domainName); err != nil {
			return fmt.Errorf("failed to create DNSNameResolver for domain %s: %w", domainName, err)
		}
	}

	return nil
}

func (c *Controller) createOrUpdateDNSNameResolver(anpName, domainName string) error {
	dnsNameResolverName := generateDNSNameResolverName(anpName, domainName)

	// Check if DNSNameResolver already exists
	existing, err := c.dnsNameResolversLister.Get(dnsNameResolverName)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to get DNSNameResolver %s: %w", dnsNameResolverName, err)
	}

	klog.Infof("Creating or updating DNSNameResolver %s for domain %s in ANP %s", dnsNameResolverName, domainName, anpName)
	dnsNameResolver := &kubeovnv1.DNSNameResolver{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DNSNameResolver",
			APIVersion: "kubeovn.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dnsNameResolverName,
			Labels: map[string]string{
				adminNetworkPolicyKey: anpName,
			},
		},
		Spec: kubeovnv1.DNSNameResolverSpec{
			Name: kubeovnv1.DNSName(domainName),
		},
	}

	if k8serrors.IsNotFound(err) {
		// Create new DNSNameResolver
		_, err = c.config.KubeOvnClient.KubeovnV1().DNSNameResolvers().Create(context.TODO(), dnsNameResolver, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create DNSNameResolver %s: %w", dnsNameResolverName, err)
		}
		klog.Infof("Created DNSNameResolver %s for domain %s in ANP %s", dnsNameResolverName, domainName, anpName)
	} else if existing.Spec.Name != kubeovnv1.DNSName(domainName) {
		// Update existing DNSNameResolver if needed
		dnsNameResolver.ResourceVersion = existing.ResourceVersion
		_, err = c.config.KubeOvnClient.KubeovnV1().DNSNameResolvers().Update(context.TODO(), dnsNameResolver, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update DNSNameResolver %s: %w", dnsNameResolverName, err)
		}
		klog.Infof("Updated DNSNameResolver %s for domain %s in ANP %s", dnsNameResolverName, domainName, anpName)
	}

	return nil
}

// deleteDNSNameResolver deletes DNSNameResolver CR
func (c *Controller) deleteDNSNameResolver(anpName, domainName string) error {
	dnsNameResolverName := generateDNSNameResolverName(anpName, domainName)

	err := c.config.KubeOvnClient.KubeovnV1().DNSNameResolvers().Delete(context.TODO(), dnsNameResolverName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete DNSNameResolver %s: %w", dnsNameResolverName, err)
	}

	if err == nil {
		klog.Infof("Deleted DNSNameResolver %s for domain %s in ANP %s", dnsNameResolverName, domainName, anpName)
	}

	return nil
}

func generateDNSNameResolverName(anpName, domainName string) string {
	hash := util.Sha256Hash([]byte(domainName))[:8]
	return fmt.Sprintf("anp-%s-%s", anpName, hash)
}

// isDNSNameResolverStatusEqual compares two DNSNameResolverStatus to check if they are equal
// Only compares IP addresses since that's what matters for ANP address set updates
func isDNSNameResolverStatusEqual(oldStatus, newStatus kubeovnv1.DNSNameResolverStatus) bool {
	// Get all IP addresses from old status
	oldIPs := extractAllIPsFromStatus(oldStatus)

	// Get all IP addresses from new status
	newIPs := extractAllIPsFromStatus(newStatus)

	// Compare the sets of IP addresses
	return areStringSlicesEqual(oldIPs, newIPs)
}

// extractAllIPsFromStatus extracts all IP addresses from DNSNameResolverStatus
func extractAllIPsFromStatus(status kubeovnv1.DNSNameResolverStatus) []string {
	var ips []string

	for _, resolvedName := range status.ResolvedNames {
		// Skip if there are resolution failures
		if resolvedName.ResolutionFailures > 0 {
			continue
		}

		for _, resolvedAddr := range resolvedName.ResolvedAddresses {
			ips = append(ips, resolvedAddr.IP)
		}
	}

	return ips
}

// areStringSlicesEqual compares two string slices for equality (order independent)
func areStringSlicesEqual(slice1, slice2 []string) bool {
	if len(slice1) != len(slice2) {
		return false
	}

	// Create maps to count occurrences
	count1 := make(map[string]int)
	count2 := make(map[string]int)

	for _, s := range slice1 {
		count1[s]++
	}
	for _, s := range slice2 {
		count2[s]++
	}

	// Compare the maps
	for key, count := range count1 {
		if count2[key] != count {
			return false
		}
	}

	return true
}
