package ovs

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

const (
	// kubeOvnVersionKey is the key used to store kube-ovn version in NBGlobal ExternalIDs
	kubeOvnVersionKey = "kube-ovn-version"
)

// Naming patterns used by kube-ovn resources
var (
	// Security group port group pattern: ovn.sg.{name} (with dashes replaced by dots)
	// Requires at least one character after "ovn.sg."
	sgPortGroupPattern = regexp.MustCompile(`^ovn\.sg\..+`)

	// Security group address set patterns: ovn.sg.{name}.associated.v4/v6
	sgAddressSetPattern = regexp.MustCompile(`^ovn\.sg\..+\.associated\.v[46]$`)

	// Network policy address set patterns: {name}.{namespace}.{ingress|egress}.{allow|except}.{ip4|ip6|all}.{index}
	npAddressSetPattern = regexp.MustCompile(`\.(ingress|egress)\.(allow|except)\.(ip[46]|all)(\.\d+)?$`)

	// kube-ovn load balancer patterns
	clusterLBPattern = regexp.MustCompile(`^cluster-(tcp|udp|sctp)(-session)?-loadbalancer$`)
	vpcLBPattern     = regexp.MustCompile(`^vpc-.+-(tcp|udp|sctp)-(load|sess-load)$`)
)

// GetKubeOvnVersion retrieves the stored kube-ovn version from NBGlobal ExternalIDs
func (c *OVNNbClient) GetKubeOvnVersion() (string, error) {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return "", fmt.Errorf("failed to get NBGlobal: %w", err)
	}

	if nbGlobal.ExternalIDs == nil {
		return "", nil
	}

	return nbGlobal.ExternalIDs[kubeOvnVersionKey], nil
}

// SetKubeOvnVersion stores the kube-ovn version in NBGlobal ExternalIDs
func (c *OVNNbClient) SetKubeOvnVersion(version string) error {
	nbGlobal, err := c.GetNbGlobal()
	if err != nil {
		return fmt.Errorf("failed to get NBGlobal: %w", err)
	}

	if nbGlobal.ExternalIDs == nil {
		nbGlobal.ExternalIDs = make(map[string]string)
	}

	if nbGlobal.ExternalIDs[kubeOvnVersionKey] == version {
		return nil // already set to current version
	}

	nbGlobal.ExternalIDs[kubeOvnVersionKey] = version
	if err := c.UpdateNbGlobal(nbGlobal, &nbGlobal.ExternalIDs); err != nil {
		return fmt.Errorf("failed to update NBGlobal with kube-ovn version: %w", err)
	}

	klog.Infof("updated kube-ovn version in NBGlobal to %s", version)
	return nil
}

// needsVendorMigration checks if vendor migration is needed based on version comparison.
// Migration is needed if:
// 1. No version is stored (fresh install or upgrade from very old version)
// 2. Stored version is older than the version that introduced vendor tagging (v1.15.0)
func (c *OVNNbClient) needsVendorMigration() (bool, error) {
	storedVersion, err := c.GetKubeOvnVersion()
	if err != nil {
		return false, err
	}

	// No version stored - this is either a fresh install or an upgrade from an old version
	// In either case, we should run migration (it's idempotent and will skip if nothing to do)
	if storedVersion == "" {
		klog.Info("no kube-ovn version found in NBGlobal, migration may be needed")
		return true, nil
	}

	// If stored version matches current version, no migration needed
	// This handles the case where version is "unknown" during tests
	if storedVersion == versions.VERSION {
		klog.Infof("stored version %s matches current version, skipping vendor migration", storedVersion)
		return false, nil
	}

	// Strip 'v' prefix if present for comparison
	stored := strings.TrimPrefix(storedVersion, "v")
	vendorTagVersion := "1.15.0" // version that introduced vendor tagging

	// If stored version is older than v1.15.0, we need to migrate
	if util.CompareVersion(stored, vendorTagVersion) < 0 {
		klog.Infof("stored version %s is older than %s, vendor migration needed", storedVersion, vendorTagVersion)
		return true, nil
	}

	klog.Infof("stored version %s is >= %s, skipping vendor migration", storedVersion, vendorTagVersion)
	return false, nil
}

// MigrateVendorExternalIDs adds vendor=kube-ovn externalID to existing kube-ovn OVN resources
// that don't already have it. This is called during controller initialization to handle
// upgrades from versions prior to vendor tagging (v1.15.0).
//
// The migration only runs when:
// 1. No version is stored in NBGlobal (fresh install or very old upgrade)
// 2. Stored version is older than v1.15.0 (when vendor tagging was introduced)
//
// The migration uses several strategies to identify kube-ovn resources:
// 1. Resources with existing kube-ovn-specific externalIDs (lr, ls, parent, sg, etc.)
// 2. Resources with kube-ovn naming patterns
// 3. Resources associated with known kube-ovn logical routers/switches
//
// Resources that cannot be positively identified as kube-ovn resources are left untouched
// to avoid interfering with external systems like OpenStack Neutron.
//
// After successful migration, the current version is stored in NBGlobal to prevent
// re-running on subsequent restarts.
func (c *OVNNbClient) MigrateVendorExternalIDs() error {
	// Check if migration is needed based on version
	needsMigration, err := c.needsVendorMigration()
	if err != nil {
		klog.Errorf("failed to check if vendor migration is needed: %v", err)
		return err
	}

	if !needsMigration {
		// Still update version to current if it changed (e.g., patch upgrade within same major)
		return c.SetKubeOvnVersion(versions.VERSION)
	}

	klog.Info("starting migration of vendor externalIDs to kube-ovn resources")

	// Get all kube-ovn logical routers (they already have vendor tag from CreateLogicalRouter)
	kubeOvnRouters, err := c.getKubeOvnRouterNames()
	if err != nil {
		klog.Errorf("failed to get kube-ovn router names: %v", err)
		return err
	}
	klog.Infof("found %d kube-ovn logical routers", len(kubeOvnRouters))

	// Get all kube-ovn logical switches (they already have vendor tag)
	kubeOvnSwitches, err := c.getKubeOvnSwitchNames()
	if err != nil {
		klog.Errorf("failed to get kube-ovn switch names: %v", err)
		return err
	}
	klog.Infof("found %d kube-ovn logical switches", len(kubeOvnSwitches))

	// Migrate resources in order of dependencies
	if err := c.migrateLogicalRouterPorts(kubeOvnRouters); err != nil {
		return err
	}

	if err := c.migratePortGroups(); err != nil {
		return err
	}

	if err := c.migrateAddressSets(); err != nil {
		return err
	}

	if err := c.migrateLoadBalancers(); err != nil {
		return err
	}

	if err := c.migrateACLs(kubeOvnSwitches); err != nil {
		return err
	}

	klog.Info("completed migration of vendor externalIDs")

	// Store the current version to prevent re-running migration on next startup
	if err := c.SetKubeOvnVersion(versions.VERSION); err != nil {
		klog.Errorf("failed to store kube-ovn version after migration: %v", err)
		return err
	}

	return nil
}

// getKubeOvnRouterNames returns names of logical routers that belong to kube-ovn
func (c *OVNNbClient) getKubeOvnRouterNames() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var lrList []ovnnb.LogicalRouter
	if err := c.ovsDbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		// Include routers that already have vendor=kube-ovn
		if len(lr.ExternalIDs) > 0 && lr.ExternalIDs["vendor"] == util.CniTypeName {
			return true
		}
		return false
	}).List(ctx, &lrList); err != nil {
		return nil, fmt.Errorf("failed to list logical routers: %w", err)
	}

	names := make(map[string]bool, len(lrList))
	for _, lr := range lrList {
		names[lr.Name] = true
	}
	return names, nil
}

// getKubeOvnSwitchNames returns names of logical switches that belong to kube-ovn
func (c *OVNNbClient) getKubeOvnSwitchNames() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var lsList []ovnnb.LogicalSwitch
	if err := c.ovsDbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		// Include switches that already have vendor=kube-ovn
		if len(ls.ExternalIDs) > 0 && ls.ExternalIDs["vendor"] == util.CniTypeName {
			return true
		}
		return false
	}).List(ctx, &lsList); err != nil {
		return nil, fmt.Errorf("failed to list logical switches: %w", err)
	}

	names := make(map[string]bool, len(lsList))
	for _, ls := range lsList {
		names[ls.Name] = true
	}
	return names, nil
}

// migrateLogicalRouterPorts adds vendor tag to LRPs that belong to kube-ovn routers
func (c *OVNNbClient) migrateLogicalRouterPorts(kubeOvnRouters map[string]bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var lrpList []ovnnb.LogicalRouterPort
	if err := c.ovsDbClient.WhereCache(func(lrp *ovnnb.LogicalRouterPort) bool {
		// Skip if already has vendor tag
		if len(lrp.ExternalIDs) > 0 && lrp.ExternalIDs["vendor"] == util.CniTypeName {
			return false
		}
		// Include if it has 'lr' externalID pointing to a kube-ovn router
		if len(lrp.ExternalIDs) > 0 {
			if lrName, ok := lrp.ExternalIDs[logicalRouterKey]; ok && kubeOvnRouters[lrName] {
				return true
			}
		}
		return false
	}).List(ctx, &lrpList); err != nil {
		return fmt.Errorf("failed to list logical router ports for migration: %w", err)
	}

	if len(lrpList) == 0 {
		klog.Info("no logical router ports need vendor migration")
		return nil
	}

	klog.Infof("migrating %d logical router ports to add vendor tag", len(lrpList))

	ops := make([]ovsdb.Operation, 0, len(lrpList))
	for i := range lrpList {
		lrp := &lrpList[i]
		if lrp.ExternalIDs == nil {
			lrp.ExternalIDs = make(map[string]string)
		}
		lrp.ExternalIDs["vendor"] = util.CniTypeName

		op, err := c.Where(lrp).Update(lrp, &lrp.ExternalIDs)
		if err != nil {
			klog.Errorf("failed to generate update operation for LRP %s: %v", lrp.Name, err)
			continue
		}
		ops = append(ops, op...)
	}

	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("lrp-vendor-migrate", ops); err != nil {
		return fmt.Errorf("failed to migrate logical router port vendor tags: %w", err)
	}

	klog.Infof("successfully migrated %d logical router ports", len(lrpList))
	return nil
}

// migratePortGroups adds vendor tag to port groups that match kube-ovn patterns
func (c *OVNNbClient) migratePortGroups() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var pgList []ovnnb.PortGroup
	if err := c.ovsDbClient.WhereCache(func(pg *ovnnb.PortGroup) bool {
		// Skip if already has vendor tag
		if len(pg.ExternalIDs) > 0 && pg.ExternalIDs["vendor"] == util.CniTypeName {
			return false
		}

		// Security group port groups: ovn.sg.{name}
		if sgPortGroupPattern.MatchString(pg.Name) {
			return true
		}

		// Port groups with 'sg' or 'type' externalID (kube-ovn specific)
		if len(pg.ExternalIDs) > 0 {
			if _, hasSg := pg.ExternalIDs[sgKey]; hasSg {
				return true
			}
			if _, hasType := pg.ExternalIDs["type"]; hasType {
				return true
			}
		}

		// Network policy port groups have kube-ovn specific externalIDs
		// Don't use name patterns alone as they're too broad and risk mis-tagging
		// resources from other systems

		return false
	}).List(ctx, &pgList); err != nil {
		return fmt.Errorf("failed to list port groups for migration: %w", err)
	}

	if len(pgList) == 0 {
		klog.Info("no port groups need vendor migration")
		return nil
	}

	klog.Infof("migrating %d port groups to add vendor tag", len(pgList))

	ops := make([]ovsdb.Operation, 0, len(pgList))
	for i := range pgList {
		pg := &pgList[i]
		if pg.ExternalIDs == nil {
			pg.ExternalIDs = make(map[string]string)
		}
		pg.ExternalIDs["vendor"] = util.CniTypeName

		op, err := c.Where(pg).Update(pg, &pg.ExternalIDs)
		if err != nil {
			klog.Errorf("failed to generate update operation for PortGroup %s: %v", pg.Name, err)
			continue
		}
		ops = append(ops, op...)
	}

	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("pg-vendor-migrate", ops); err != nil {
		return fmt.Errorf("failed to migrate port group vendor tags: %w", err)
	}

	klog.Infof("successfully migrated %d port groups", len(pgList))
	return nil
}

// migrateAddressSets adds vendor tag to address sets that match kube-ovn patterns
func (c *OVNNbClient) migrateAddressSets() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var asList []ovnnb.AddressSet
	if err := c.ovsDbClient.WhereCache(func(as *ovnnb.AddressSet) bool {
		// Skip if already has vendor tag
		if len(as.ExternalIDs) > 0 && as.ExternalIDs["vendor"] == util.CniTypeName {
			return false
		}

		// Security group address sets: ovn.sg.{name}.associated.v4/v6
		if sgAddressSetPattern.MatchString(as.Name) {
			return true
		}

		// Network policy address sets: {name}.{namespace}.{direction}.{type}.{protocol}
		if npAddressSetPattern.MatchString(as.Name) {
			return true
		}

		// Address sets with 'sg' externalID (kube-ovn specific)
		if len(as.ExternalIDs) > 0 {
			if _, hasSg := as.ExternalIDs[sgKey]; hasSg {
				return true
			}
		}

		return false
	}).List(ctx, &asList); err != nil {
		return fmt.Errorf("failed to list address sets for migration: %w", err)
	}

	if len(asList) == 0 {
		klog.Info("no address sets need vendor migration")
		return nil
	}

	klog.Infof("migrating %d address sets to add vendor tag", len(asList))

	ops := make([]ovsdb.Operation, 0, len(asList))
	for i := range asList {
		as := &asList[i]
		if as.ExternalIDs == nil {
			as.ExternalIDs = make(map[string]string)
		}
		as.ExternalIDs["vendor"] = util.CniTypeName

		op, err := c.Where(as).Update(as, &as.ExternalIDs)
		if err != nil {
			klog.Errorf("failed to generate update operation for AddressSet %s: %v", as.Name, err)
			continue
		}
		ops = append(ops, op...)
	}

	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("as-vendor-migrate", ops); err != nil {
		return fmt.Errorf("failed to migrate address set vendor tags: %w", err)
	}

	klog.Infof("successfully migrated %d address sets", len(asList))
	return nil
}

// migrateLoadBalancers adds vendor tag to load balancers that match kube-ovn patterns
func (c *OVNNbClient) migrateLoadBalancers() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var lbList []ovnnb.LoadBalancer
	if err := c.ovsDbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool {
		// Skip if already has vendor tag
		if len(lb.ExternalIDs) > 0 && lb.ExternalIDs["vendor"] == util.CniTypeName {
			return false
		}

		// Cluster load balancers: cluster-{protocol}-loadbalancer or cluster-{protocol}-session-loadbalancer
		if clusterLBPattern.MatchString(lb.Name) {
			return true
		}

		// VPC load balancers: vpc-{name}-{protocol}-load or vpc-{name}-{protocol}-sess-load
		if vpcLBPattern.MatchString(lb.Name) {
			return true
		}

		return false
	}).List(ctx, &lbList); err != nil {
		return fmt.Errorf("failed to list load balancers for migration: %w", err)
	}

	if len(lbList) == 0 {
		klog.Info("no load balancers need vendor migration")
		return nil
	}

	klog.Infof("migrating %d load balancers to add vendor tag", len(lbList))

	ops := make([]ovsdb.Operation, 0, len(lbList))
	for i := range lbList {
		lb := &lbList[i]
		if lb.ExternalIDs == nil {
			lb.ExternalIDs = make(map[string]string)
		}
		lb.ExternalIDs["vendor"] = util.CniTypeName

		op, err := c.Where(lb).Update(lb, &lb.ExternalIDs)
		if err != nil {
			klog.Errorf("failed to generate update operation for LoadBalancer %s: %v", lb.Name, err)
			continue
		}
		ops = append(ops, op...)
	}

	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("lb-vendor-migrate", ops); err != nil {
		return fmt.Errorf("failed to migrate load balancer vendor tags: %w", err)
	}

	klog.Infof("successfully migrated %d load balancers", len(lbList))
	return nil
}

// migrateACLs adds vendor tag to ACLs that belong to kube-ovn
func (c *OVNNbClient) migrateACLs(kubeOvnSwitches map[string]bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	// First, get all port groups that belong to kube-ovn (either already tagged or matching patterns)
	kubeOvnPortGroups := make(map[string]bool)
	var pgList []ovnnb.PortGroup
	if err := c.ovsDbClient.WhereCache(func(pg *ovnnb.PortGroup) bool {
		// Include port groups with vendor tag
		if len(pg.ExternalIDs) > 0 && pg.ExternalIDs["vendor"] == util.CniTypeName {
			return true
		}
		// Include port groups matching kube-ovn patterns
		if sgPortGroupPattern.MatchString(pg.Name) {
			return true
		}
		if len(pg.ExternalIDs) > 0 {
			if _, hasSg := pg.ExternalIDs[sgKey]; hasSg {
				return true
			}
			if _, hasType := pg.ExternalIDs["type"]; hasType {
				return true
			}
		}
		// Network policy port groups have kube-ovn specific externalIDs
		// Don't use name patterns alone as they're too broad
		return false
	}).List(ctx, &pgList); err != nil {
		return fmt.Errorf("failed to list port groups: %w", err)
	}

	for _, pg := range pgList {
		kubeOvnPortGroups[pg.Name] = true
	}

	var aclList []ovnnb.ACL
	if err := c.ovsDbClient.WhereCache(func(acl *ovnnb.ACL) bool {
		// Skip if already has vendor tag
		if len(acl.ExternalIDs) > 0 && acl.ExternalIDs["vendor"] == util.CniTypeName {
			return false
		}

		// ACLs with 'parent' externalID pointing to kube-ovn port group or switch
		if len(acl.ExternalIDs) > 0 {
			if parent, ok := acl.ExternalIDs[aclParentKey]; ok {
				if kubeOvnPortGroups[parent] || kubeOvnSwitches[parent] {
					return true
				}
			}
			// ACLs with 'subnet' externalID pointing to kube-ovn switch
			if subnet, ok := acl.ExternalIDs["subnet"]; ok && kubeOvnSwitches[subnet] {
				return true
			}
		}

		return false
	}).List(ctx, &aclList); err != nil {
		return fmt.Errorf("failed to list ACLs for migration: %w", err)
	}

	if len(aclList) == 0 {
		klog.Info("no ACLs need vendor migration")
		return nil
	}

	klog.Infof("migrating %d ACLs to add vendor tag", len(aclList))

	ops := make([]ovsdb.Operation, 0, len(aclList))
	for i := range aclList {
		acl := &aclList[i]
		if acl.ExternalIDs == nil {
			acl.ExternalIDs = make(map[string]string)
		}
		acl.ExternalIDs["vendor"] = util.CniTypeName

		op, err := c.Where(acl).Update(acl, &acl.ExternalIDs)
		if err != nil {
			klog.Errorf("failed to generate update operation for ACL %s: %v", acl.UUID, err)
			continue
		}
		ops = append(ops, op...)
	}

	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("acl-vendor-migrate", ops); err != nil {
		return fmt.Errorf("failed to migrate ACL vendor tags: %w", err)
	}

	klog.Infof("successfully migrated %d ACLs", len(aclList))
	return nil
}
