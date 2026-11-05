package ovs

import (
	"fmt"
	"regexp"
	"strings"

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
	return kubeOvnNames(c.Database, &ovnnb.LogicalRouter{},
		func(lr *ovnnb.LogicalRouter) string { return lr.Name },
		func(lr *ovnnb.LogicalRouter) map[string]string { return lr.ExternalIDs },
		"failed to list logical routers")
}

func (c *OVNNbClient) getKubeOvnSwitchNames() (map[string]bool, error) {
	return kubeOvnNames(c.Database, &ovnnb.LogicalSwitch{},
		func(ls *ovnnb.LogicalSwitch) string { return ls.Name },
		func(ls *ovnnb.LogicalSwitch) map[string]string { return ls.ExternalIDs },
		"failed to list logical switches")
}

func (c *OVNNbClient) migrateLogicalRouterPorts(kubeOvnRouters map[string]bool) error {
	return migrateVendorRows(c, &ovnnb.LogicalRouterPort{}, func(lrp *ovnnb.LogicalRouterPort) bool {
		if hasVendor(lrp.ExternalIDs) {
			return false
		}
		if lrName, ok := lrp.ExternalIDs[logicalRouterKey]; ok && kubeOvnRouters[lrName] {
			return true
		}
		return false
	}, "lrp-vendor-migrate", "logical router ports",
		func(lrp *ovnnb.LogicalRouterPort) string { return lrp.Name },
		func(lrp *ovnnb.LogicalRouterPort) *map[string]string { return &lrp.ExternalIDs })
}

func (c *OVNNbClient) migratePortGroups() error {
	return migrateVendorRows(c, &ovnnb.PortGroup{}, func(pg *ovnnb.PortGroup) bool {
		if hasVendor(pg.ExternalIDs) {
			return false
		}
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
		return false
	}, "pg-vendor-migrate", "port groups",
		func(pg *ovnnb.PortGroup) string { return pg.Name },
		func(pg *ovnnb.PortGroup) *map[string]string { return &pg.ExternalIDs })
}

func (c *OVNNbClient) migrateAddressSets() error {
	return migrateVendorRows(c, &ovnnb.AddressSet{}, func(as *ovnnb.AddressSet) bool {
		if hasVendor(as.ExternalIDs) {
			return false
		}
		if sgAddressSetPattern.MatchString(as.Name) {
			return true
		}
		if npAddressSetPattern.MatchString(as.Name) {
			return true
		}
		if _, hasSg := as.ExternalIDs[sgKey]; hasSg {
			return true
		}
		return false
	}, "as-vendor-migrate", "address sets",
		func(as *ovnnb.AddressSet) string { return as.Name },
		func(as *ovnnb.AddressSet) *map[string]string { return &as.ExternalIDs })
}

func (c *OVNNbClient) migrateLoadBalancers() error {
	return migrateVendorRows(c, &ovnnb.LoadBalancer{}, func(lb *ovnnb.LoadBalancer) bool {
		if hasVendor(lb.ExternalIDs) {
			return false
		}
		return clusterLBPattern.MatchString(lb.Name) || vpcLBPattern.MatchString(lb.Name)
	}, "lb-vendor-migrate", "load balancers",
		func(lb *ovnnb.LoadBalancer) string { return lb.Name },
		func(lb *ovnnb.LoadBalancer) *map[string]string { return &lb.ExternalIDs })
}

func (c *OVNNbClient) migrateACLs(kubeOvnSwitches map[string]bool) error {
	pgList, err := filterTimeout(c.Database, &ovnnb.PortGroup{}, func(pg *ovnnb.PortGroup) bool {
		if hasVendor(pg.ExternalIDs) || sgPortGroupPattern.MatchString(pg.Name) {
			return true
		}
		if _, hasSg := pg.ExternalIDs[sgKey]; hasSg {
			return true
		}
		_, hasType := pg.ExternalIDs["type"]
		return hasType
	})
	if err != nil {
		return fmt.Errorf("failed to list port groups: %w", err)
	}
	kubeOvnPortGroups := make(map[string]bool, len(pgList))
	for _, pg := range pgList {
		kubeOvnPortGroups[pg.Name] = true
	}

	return migrateVendorRows(c, &ovnnb.ACL{}, func(acl *ovnnb.ACL) bool {
		if hasVendor(acl.ExternalIDs) {
			return false
		}
		if parent, ok := acl.ExternalIDs[aclParentKey]; ok && (kubeOvnPortGroups[parent] || kubeOvnSwitches[parent]) {
			return true
		}
		subnet, ok := acl.ExternalIDs["subnet"]
		return ok && kubeOvnSwitches[subnet]
	}, "acl-vendor-migrate", "ACLs",
		func(acl *ovnnb.ACL) string { return acl.UUID },
		func(acl *ovnnb.ACL) *map[string]string { return &acl.ExternalIDs })
}
