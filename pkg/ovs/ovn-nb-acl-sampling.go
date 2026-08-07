package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
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
		return nil
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid ACL sampling configuration: %w", err)
	}
	if err := c.ensureACLSamplingMonitor(); err != nil {
		return err
	}

	allowProbability, _ := aclsampling.ProbabilityFromPercent(config.AllowProbabilityPercent)
	defaultDenyProbability, _ := aclsampling.ProbabilityFromPercent(config.DefaultDenyProbabilityPercent)

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
		client.WithTable(&ovnnb.SamplingApp{}),
		client.WithTable(&ovnnb.SampleCollector{}),
		client.WithTable(&ovnnb.Sample{}),
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
	if err := c.WhereCache(func(*ovnnb.SamplingApp) bool { return true }).List(ctx, &apps); err != nil {
		return nil, fmt.Errorf("list OVN sampling applications: %w", err)
	}
	return apps, nil
}

func (c *OVNNbClient) listSampleCollectors() ([]ovnnb.SampleCollector, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	collectors := make([]ovnnb.SampleCollector, 0)
	if err := c.WhereCache(func(*ovnnb.SampleCollector) bool { return true }).List(ctx, &collectors); err != nil {
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
		ops, err := c.Create(app)
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
	ops, err := c.Where(current).Update(current, &current.ID, &current.ExternalIDs)
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
		ops, err := c.Create(collector)
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
	ops, err := c.Where(current).Update(current,
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
