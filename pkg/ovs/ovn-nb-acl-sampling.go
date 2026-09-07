package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var ErrACLSamplingUnsupported = errors.New("OVN northbound ACL sampling is unsupported")

const (
	aclSamplingFeatureExternalID = "kube-ovn.io/feature"
	aclSamplingFeature           = "acl-sampling"
	aclSamplingKindExternalID    = "kube-ovn.io/acl-sampling-kind"
	aclSamplingRoleExternalID    = "kube-ovn.io/acl-sampling-role"

	aclSamplingKindApplication = "application"
	aclSamplingKindCollector   = "collector"
	aclSamplingRoleAllow       = "allow"
	aclSamplingRoleDefaultDeny = "default-deny"
)

type desiredSamplingApp struct {
	appType ovnnb.SamplingAppType
	id      uint32
}

type desiredSampleCollector struct {
	role        string
	id          uint32
	name        string
	probability int
}

// ReconcileACLSampling idempotently creates or reconciles the OVN sampling
// applications and collectors owned by Kube-OVN. Matching unowned
// Sampling_App rows are reused without modification; unowned collectors are
// never adopted.
func (c *OVNNbClient) ReconcileACLSampling(config aclsampling.ControllerConfig) error {
	if !config.Enabled {
		return c.cleanupACLSampling()
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid ACL sampling configuration: %w", err)
	}
	if err := c.ensureACLSamplingMonitor(); err != nil {
		return err
	}

	allowProbability, err := aclsampling.ProbabilityFromPercent(config.AllowProbabilityPercent)
	if err != nil {
		return fmt.Errorf("convert allow sampling probability: %w", err)
	}
	defaultDenyProbability, err := aclsampling.ProbabilityFromPercent(config.DefaultDenyProbabilityPercent)
	if err != nil {
		return fmt.Errorf("convert default-deny sampling probability: %w", err)
	}

	apps, err := c.listSamplingApps()
	if err != nil {
		return err
	}
	collectors, err := c.listSampleCollectors()
	if err != nil {
		return err
	}

	desiredApps := []desiredSamplingApp{
		{appType: ovnnb.SamplingAppTypeACLNew, id: config.AppIDNew},
		{appType: ovnnb.SamplingAppTypeACLEst, id: config.AppIDEstablished},
	}
	desiredAppIDs := make(map[ovnnb.SamplingAppType]uint32, len(desiredApps))
	for _, desired := range desiredApps {
		desiredAppIDs[desired.appType] = desired.id
	}

	operations := make([]ovsdb.Operation, 0, 8)
	for _, desired := range desiredApps {
		ops, err := c.reconcileSamplingApp(apps, desired, desiredAppIDs)
		if err != nil {
			return err
		}
		operations = append(operations, ops...)
	}

	desiredCollectors := []desiredSampleCollector{
		{
			role:        aclSamplingRoleAllow,
			id:          config.CollectorIDAllow,
			name:        "kube-ovn-network-policy-allow",
			probability: allowProbability,
		},
		{
			role:        aclSamplingRoleDefaultDeny,
			id:          config.CollectorIDDefaultDeny,
			name:        "kube-ovn-network-policy-default-deny",
			probability: defaultDenyProbability,
		},
	}
	desiredCollectorIDs := make(map[string]uint32, len(desiredCollectors))
	for _, desired := range desiredCollectors {
		desiredCollectorIDs[desired.role] = desired.id
	}
	for _, desired := range desiredCollectors {
		ops, err := c.reconcileSampleCollector(collectors, desired, desiredCollectorIDs, config.SetID)
		if err != nil {
			return err
		}
		operations = append(operations, ops...)
	}

	if err := c.Transact("acl-sampling-reconcile", operations); err != nil {
		return fmt.Errorf("reconcile OVN ACL sampling objects: %w", err)
	}
	return nil
}

func (c *OVNNbClient) ensureACLSamplingMonitor() error {
	c.aclSamplingMonitorMu.Lock()
	defer c.aclSamplingMonitorMu.Unlock()

	if c.aclSamplingMonitored {
		return nil
	}
	if err := validateACLSamplingSchema(c.Schema()); err != nil {
		return err
	}

	monitor := c.NewMonitor(
		compat.WithTable(&ovnnb.SamplingApp{}),
		compat.WithTable(&ovnnb.SampleCollector{}),
		compat.WithTable(&ovnnb.Sample{}),
	)
	if len(monitor.Errors) != 0 {
		return fmt.Errorf("build OVN ACL sampling monitor: %w", errors.Join(monitor.Errors...))
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	if _, err := c.Monitor(ctx, monitor); err != nil {
		return fmt.Errorf("monitor OVN ACL sampling tables: %w", err)
	}
	c.aclSamplingMonitored = true
	return nil
}

func validateACLSamplingSchema(schema ovsdb.DatabaseSchema) error {
	required := map[string][]string{
		ovnnb.ACLTable:             {"external_ids", "sample_new", "sample_est"},
		ovnnb.SamplingAppTable:     {"external_ids", "id", "type"},
		ovnnb.SampleCollectorTable: {"external_ids", "id", "name", "probability", "set_id"},
		ovnnb.SampleTable:          {"collectors", "metadata"},
	}
	for tableName, columns := range required {
		table, ok := schema.Tables[tableName]
		if !ok {
			return fmt.Errorf("%w: table %s is missing", ErrACLSamplingUnsupported, tableName)
		}
		for _, column := range columns {
			if _, ok := table.Columns[column]; !ok {
				return fmt.Errorf("%w: column %s.%s is missing", ErrACLSamplingUnsupported, tableName, column)
			}
		}
	}
	return nil
}

func (c *OVNNbClient) listSamplingApps() ([]ovnnb.SamplingApp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	apps := make([]ovnnb.SamplingApp, 0)
	if err := c.Database.Table(&ovnnb.SamplingApp{}).Filter(ctx, func(*ovnnb.SamplingApp) bool { return true }, &apps); err != nil {
		return nil, fmt.Errorf("list OVN sampling applications: %w", err)
	}
	return apps, nil
}

func (c *OVNNbClient) listSampleCollectors() ([]ovnnb.SampleCollector, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	collectors := make([]ovnnb.SampleCollector, 0)
	if err := c.Database.Table(&ovnnb.SampleCollector{}).Filter(ctx, func(*ovnnb.SampleCollector) bool { return true }, &collectors); err != nil {
		return nil, fmt.Errorf("list OVN sample collectors: %w", err)
	}
	return collectors, nil
}

func (c *OVNNbClient) reconcileSamplingApp(apps []ovnnb.SamplingApp, desired desiredSamplingApp, desiredIDs map[ovnnb.SamplingAppType]uint32) ([]ovsdb.Operation, error) {
	var current *ovnnb.SamplingApp
	for i := range apps {
		app := &apps[i]
		if app.ID == int(desired.id) && app.Type != desired.appType {
			targetID, managed := desiredIDs[app.Type]
			if !isOwnedACLSamplingObject(app.ExternalIDs) || !managed || targetID == desired.id {
				return nil, fmt.Errorf("sampling application ID %d is already used by type %s", desired.id, app.Type)
			}
		}
		if app.Type == desired.appType {
			current = app
		}
	}

	desiredExternalIDs := ownedACLSamplingExternalIDs(aclSamplingKindApplication, desired.appType)
	if current == nil {
		app := &ovnnb.SamplingApp{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: desiredExternalIDs,
			ID:          int(desired.id),
			Type:        desired.appType,
		}
		ops, err := c.Database.Table(&ovnnb.SamplingApp{}).CreateOps(app)
		if err != nil {
			return nil, fmt.Errorf("build create operation for sampling application %s: %w", desired.appType, err)
		}
		return ops, nil
	}

	if !isOwnedACLSamplingObject(current.ExternalIDs) {
		if current.ID != int(desired.id) {
			return nil, fmt.Errorf("unowned sampling application %s uses ID %d instead of configured ID %d", current.Type, current.ID, desired.id)
		}
		return nil, nil
	}
	if current.ID == int(desired.id) && maps.Equal(current.ExternalIDs, desiredExternalIDs) {
		return nil, nil
	}

	current.ID = int(desired.id)
	current.ExternalIDs = desiredExternalIDs
	ops, err := c.Database.WhereTable(current).Update(current, &current.ID, &current.ExternalIDs)
	if err != nil {
		return nil, fmt.Errorf("build update operation for sampling application %s: %w", desired.appType, err)
	}
	return ops, nil
}

func (c *OVNNbClient) reconcileSampleCollector(collectors []ovnnb.SampleCollector, desired desiredSampleCollector, desiredIDs map[string]uint32, setID uint32) ([]ovsdb.Operation, error) {
	var current *ovnnb.SampleCollector
	for i := range collectors {
		collector := &collectors[i]
		ownedRole := collector.ExternalIDs[aclSamplingRoleExternalID]
		if collector.ID == int(desired.id) && (!isOwnedACLSamplingObject(collector.ExternalIDs) || ownedRole != desired.role) {
			targetID, managed := desiredIDs[ownedRole]
			if !isOwnedACLSamplingObject(collector.ExternalIDs) || !managed || targetID == desired.id {
				return nil, fmt.Errorf("sample collector ID %d is owned by another application", desired.id)
			}
		}
		if isOwnedACLSamplingObject(collector.ExternalIDs) && ownedRole == desired.role {
			if current != nil {
				return nil, fmt.Errorf("multiple owned sample collectors have role %s", desired.role)
			}
			current = collector
		}
	}

	desiredExternalIDs := ownedACLSamplingExternalIDs(aclSamplingKindCollector, desired.role)
	if current == nil {
		collector := &ovnnb.SampleCollector{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: desiredExternalIDs,
			ID:          int(desired.id),
			Name:        desired.name,
			Probability: desired.probability,
			SetID:       int(setID),
		}
		ops, err := c.Database.Table(&ovnnb.SampleCollector{}).CreateOps(collector)
		if err != nil {
			return nil, fmt.Errorf("build create operation for %s sample collector: %w", desired.role, err)
		}
		return ops, nil
	}

	if current.ID == int(desired.id) && current.Name == desired.name &&
		current.Probability == desired.probability && current.SetID == int(setID) &&
		maps.Equal(current.ExternalIDs, desiredExternalIDs) {
		return nil, nil
	}

	current.ID = int(desired.id)
	current.Name = desired.name
	current.Probability = desired.probability
	current.SetID = int(setID)
	current.ExternalIDs = desiredExternalIDs
	ops, err := c.Database.WhereTable(current).Update(current,
		&current.ID,
		&current.Name,
		&current.Probability,
		&current.SetID,
		&current.ExternalIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("build update operation for %s sample collector: %w", desired.role, err)
	}
	return ops, nil
}

func ownedACLSamplingExternalIDs(kind, role string) map[string]string {
	return map[string]string{
		ExternalIDVendor:             util.CniTypeName,
		aclSamplingFeatureExternalID: aclSamplingFeature,
		aclSamplingKindExternalID:    kind,
		aclSamplingRoleExternalID:    role,
	}
}

func isOwnedACLSamplingObject(externalIDs map[string]string) bool {
	return externalIDs[ExternalIDVendor] == util.CniTypeName &&
		externalIDs[aclSamplingFeatureExternalID] == aclSamplingFeature
}

func (c *OVNNbClient) cleanupACLSampling() error {
	if err := validateACLSamplingSchema(c.Schema()); err != nil {
		if errors.Is(err, ErrACLSamplingUnsupported) {
			return nil
		}
		return err
	}
	if err := c.ensureACLSamplingMonitor(); err != nil {
		return err
	}

	acls, err := c.ListAcls("", map[string]string{sampleFeatureExternalID: networkPolicySampleFeature})
	if err != nil {
		return fmt.Errorf("list owned ACL sampling references: %w", err)
	}
	clearOps := make([]ovsdb.Operation, 0, len(acls))
	for i := range acls {
		if !isOwnedNetworkPolicySamplingACL(acls[i].ExternalIDs) {
			continue
		}
		ops, err := c.clearNetworkPolicySamplingOps(&acls[i])
		if err != nil {
			return err
		}
		clearOps = append(clearOps, ops...)
	}
	if len(clearOps) != 0 {
		if err := c.Transact("acl-sampling-cleanup-references", clearOps); err != nil {
			return fmt.Errorf("clear owned ACL sampling references: %w", err)
		}
	}

	collectors, err := c.listSampleCollectors()
	if err != nil {
		return err
	}
	ownedCollectors := make(map[string]struct{}, len(collectors))
	for i := range collectors {
		if isOwnedACLSamplingObject(collectors[i].ExternalIDs) {
			ownedCollectors[collectors[i].UUID] = struct{}{}
		}
	}
	retainedCollectors, err := c.waitForACLSamplingSampleGC(ownedCollectors)
	if err != nil {
		return err
	}
	if err := c.deleteUnreferencedACLSamplingCollectors(collectors, retainedCollectors); err != nil {
		return err
	}
	return c.deleteOwnedACLSamplingApps()
}

func (c *OVNNbClient) waitForACLSamplingSampleGC(ownedCollectors map[string]struct{}) (map[string]struct{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		retained, pending, err := c.aclSamplingCollectorReferences(ownedCollectors)
		if err != nil {
			return nil, err
		}
		if !pending {
			return retained, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for owned ACL samples to be garbage collected: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *OVNNbClient) aclSamplingCollectorReferences(ownedCollectors map[string]struct{}) (map[string]struct{}, bool, error) {
	acls, err := c.ListAcls("", nil)
	if err != nil {
		return nil, false, fmt.Errorf("list ACL references during sampling cleanup: %w", err)
	}
	referencedSamples := make(map[string]struct{})
	for _, acl := range acls {
		if isOwnedNetworkPolicySamplingACL(acl.ExternalIDs) {
			return nil, true, nil
		}
		for _, sampleUUID := range compactSampleReferences(acl) {
			referencedSamples[sampleUUID] = struct{}{}
		}
	}

	samples, err := c.listSamples()
	if err != nil {
		return nil, false, err
	}
	retained := make(map[string]struct{})
	pending := false
	for _, sample := range samples {
		_, referenced := referencedSamples[sample.UUID]
		for _, collectorUUID := range sample.Collectors {
			if _, owned := ownedCollectors[collectorUUID]; !owned {
				continue
			}
			if referenced {
				retained[collectorUUID] = struct{}{}
			} else {
				pending = true
			}
		}
	}
	return retained, pending, nil
}

func (c *OVNNbClient) deleteUnreferencedACLSamplingCollectors(collectors []ovnnb.SampleCollector, retained map[string]struct{}) error {
	operations := make([]ovsdb.Operation, 0, len(collectors))
	for i := range collectors {
		collector := &collectors[i]
		if !isOwnedACLSamplingObject(collector.ExternalIDs) {
			continue
		}
		if _, ok := retained[collector.UUID]; ok {
			continue
		}
		ops, err := c.Database.Table(&ovnnb.SampleCollector{}).Where(collector).Delete()
		if err != nil {
			return fmt.Errorf("build delete operation for owned sample collector %s: %w", collector.UUID, err)
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	if err := c.Transact("acl-sampling-cleanup-collectors", operations); err != nil {
		return fmt.Errorf("delete unreferenced owned sample collectors: %w", err)
	}
	return nil
}

func (c *OVNNbClient) deleteOwnedACLSamplingApps() error {
	apps, err := c.listSamplingApps()
	if err != nil {
		return err
	}
	operations := make([]ovsdb.Operation, 0, len(apps))
	for i := range apps {
		app := &apps[i]
		if !isOwnedACLSamplingObject(app.ExternalIDs) {
			continue
		}
		ops, err := c.Database.WhereTable(app).Delete()
		if err != nil {
			return fmt.Errorf("build delete operation for owned sampling application %s: %w", app.UUID, err)
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	if err := c.Transact("acl-sampling-cleanup-applications", operations); err != nil {
		return fmt.Errorf("delete owned sampling applications: %w", err)
	}
	return nil
}
