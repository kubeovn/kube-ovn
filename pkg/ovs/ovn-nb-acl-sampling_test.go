package ovs

import (
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newACLSamplingTestClient(t *testing.T, schemaName string) *OVNNbClient {
	t.Helper()
	dbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, fmt.Sprintf("acl-sampling-%s-%d", schemaName, time.Now().UnixNano()), dbModel, ovnnb.Schema())
	client, err := newOvnNbClient(t, "unix:"+socket, 3)
	require.NoError(t, err)
	return client
}

func validACLSamplingConfig() aclsampling.ControllerConfig {
	return aclsampling.ControllerConfig{
		Enabled:                       true,
		SetID:                         aclsampling.DefaultSetID,
		AppIDNew:                      aclsampling.DefaultAppIDNew,
		AppIDEstablished:              aclsampling.DefaultAppIDEstablished,
		CollectorIDAllow:              aclsampling.DefaultCollectorIDAllow,
		CollectorIDDefaultDeny:        aclsampling.DefaultCollectorIDDefaultDeny,
		AllowProbabilityPercent:       aclsampling.DefaultAllowProbabilityPercent,
		DefaultDenyProbabilityPercent: aclsampling.DefaultDefaultDenyProbabilityPercent,
	}
}

func TestReconcileACLSamplingCreatesAndReusesObjects(t *testing.T) {
	client := newACLSamplingTestClient(t, "create")
	config := validACLSamplingConfig()

	require.NoError(t, client.ReconcileACLSampling(config))
	apps, collectors := waitForACLSamplingObjects(t, client)
	require.Len(t, apps, 2)
	require.Len(t, collectors, 2)

	appUUIDs := make(map[string]string, len(apps))
	for _, app := range apps {
		appUUIDs[app.Type] = app.UUID
		require.True(t, isOwnedACLSamplingObject(app.ExternalIDs))
	}
	require.Equal(t, int(config.AppIDNew), appByType(t, apps, ovnnb.SamplingAppTypeACLNew).ID)
	require.Equal(t, int(config.AppIDEstablished), appByType(t, apps, ovnnb.SamplingAppTypeACLEst).ID)

	allow := collectorByRole(t, collectors, aclSamplingRoleAllow)
	require.Equal(t, int(config.CollectorIDAllow), allow.ID)
	require.Equal(t, int(config.SetID), allow.SetID)
	require.Equal(t, 655, allow.Probability)
	defaultDeny := collectorByRole(t, collectors, aclSamplingRoleDefaultDeny)
	require.Equal(t, int(config.CollectorIDDefaultDeny), defaultDeny.ID)
	require.Equal(t, 65535, defaultDeny.Probability)

	require.NoError(t, client.ReconcileACLSampling(config))
	apps, collectors = waitForACLSamplingObjects(t, client)
	require.Len(t, apps, 2)
	require.Len(t, collectors, 2)
	for _, app := range apps {
		require.Equal(t, appUUIDs[app.Type], app.UUID)
	}
}

func TestReconcileACLSamplingReusesMatchingUnownedApplication(t *testing.T) {
	client := newACLSamplingTestClient(t, "reuse-app")
	externalIDs := map[string]string{"owner": "external-observer"}
	app := &ovnnb.SamplingApp{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: externalIDs,
		ID:          int(aclsampling.DefaultAppIDNew),
		Type:        ovnnb.SamplingAppTypeACLNew,
	}
	ops, err := client.Create(app)
	require.NoError(t, err)
	require.NoError(t, client.Transact("seed-unowned-sampling-app", ops))

	require.NoError(t, client.ReconcileACLSampling(validACLSamplingConfig()))
	apps, _ := waitForACLSamplingObjects(t, client)
	actual := appByType(t, apps, ovnnb.SamplingAppTypeACLNew)
	require.Equal(t, externalIDs, actual.ExternalIDs)
}

func TestReconcileACLSamplingRejectsUnownedConflicts(t *testing.T) {
	t.Run("application type uses another ID", func(t *testing.T) {
		client := newACLSamplingTestClient(t, "app-conflict")
		app := &ovnnb.SamplingApp{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: map[string]string{"owner": "external-observer"},
			ID:          200,
			Type:        ovnnb.SamplingAppTypeACLNew,
		}
		ops, err := client.Create(app)
		require.NoError(t, err)
		require.NoError(t, client.Transact("seed-conflicting-sampling-app", ops))

		err = client.ReconcileACLSampling(validACLSamplingConfig())
		require.ErrorContains(t, err, "unowned sampling application acl-new uses ID 200")
	})

	t.Run("collector ID is unowned", func(t *testing.T) {
		client := newACLSamplingTestClient(t, "collector-conflict")
		collector := &ovnnb.SampleCollector{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: map[string]string{"owner": "external-observer"},
			ID:          int(aclsampling.DefaultCollectorIDAllow),
			Name:        "external",
			Probability: 123,
			SetID:       int(aclsampling.DefaultSetID),
		}
		ops, err := client.Create(collector)
		require.NoError(t, err)
		require.NoError(t, client.Transact("seed-conflicting-sample-collector", ops))

		err = client.ReconcileACLSampling(validACLSamplingConfig())
		require.ErrorContains(t, err, "sample collector ID 1 is owned by another application")
		actual, listErr := client.listSampleCollectors()
		require.NoError(t, listErr)
		require.Len(t, actual, 1)
		require.Equal(t, "external", actual[0].Name)
		require.Equal(t, 123, actual[0].Probability)
	})
}

func TestReconcileACLSamplingUpdatesOwnedObjectsAndSwapsIDs(t *testing.T) {
	client := newACLSamplingTestClient(t, "update-owned")
	apps := []*ovnnb.SamplingApp{
		{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: ownedACLSamplingExternalIDs(aclSamplingKindApplication, ovnnb.SamplingAppTypeACLNew),
			ID:          int(aclsampling.DefaultAppIDEstablished),
			Type:        ovnnb.SamplingAppTypeACLNew,
		},
		{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: ownedACLSamplingExternalIDs(aclSamplingKindApplication, ovnnb.SamplingAppTypeACLEst),
			ID:          int(aclsampling.DefaultAppIDNew),
			Type:        ovnnb.SamplingAppTypeACLEst,
		},
	}
	collectors := []*ovnnb.SampleCollector{
		{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: ownedACLSamplingExternalIDs(aclSamplingKindCollector, aclSamplingRoleAllow),
			ID:          int(aclsampling.DefaultCollectorIDDefaultDeny),
			Name:        "stale-allow",
			Probability: 10,
			SetID:       100,
		},
		{
			UUID:        ovsclient.NamedUUID(),
			ExternalIDs: ownedACLSamplingExternalIDs(aclSamplingKindCollector, aclSamplingRoleDefaultDeny),
			ID:          int(aclsampling.DefaultCollectorIDAllow),
			Name:        "stale-deny",
			Probability: 20,
			SetID:       100,
		},
	}

	operations := make([]ovsdb.Operation, 0, 4)
	for _, object := range apps {
		ops, err := client.Create(object)
		require.NoError(t, err)
		operations = append(operations, ops...)
	}
	for _, object := range collectors {
		ops, err := client.Create(object)
		require.NoError(t, err)
		operations = append(operations, ops...)
	}
	require.NoError(t, client.Transact("seed-owned-acl-sampling-objects", operations))

	config := validACLSamplingConfig()
	require.NoError(t, client.ReconcileACLSampling(config))
	actualApps, actualCollectors := waitForACLSamplingObjects(t, client)
	require.Equal(t, int(config.AppIDNew), appByType(t, actualApps, ovnnb.SamplingAppTypeACLNew).ID)
	require.Equal(t, int(config.AppIDEstablished), appByType(t, actualApps, ovnnb.SamplingAppTypeACLEst).ID)
	allow := collectorByRole(t, actualCollectors, aclSamplingRoleAllow)
	require.Equal(t, int(config.CollectorIDAllow), allow.ID)
	require.Equal(t, 655, allow.Probability)
	require.Equal(t, int(config.SetID), allow.SetID)
	defaultDeny := collectorByRole(t, actualCollectors, aclSamplingRoleDefaultDeny)
	require.Equal(t, int(config.CollectorIDDefaultDeny), defaultDeny.ID)
	require.Equal(t, 65535, defaultDeny.Probability)
}

func TestValidateACLSamplingSchema(t *testing.T) {
	schema := ovnnb.Schema()
	require.NoError(t, validateACLSamplingSchema(schema))

	delete(schema.Tables, ovnnb.SampleTable)
	err := validateACLSamplingSchema(schema)
	require.ErrorIs(t, err, ErrACLSamplingUnsupported)
	require.ErrorContains(t, err, "table Sample is missing")
}

func TestReconcileACLSamplingDisabledCleansOwnedState(t *testing.T) {
	client := newACLSamplingTestClient(t, "cleanup-owned-state")
	config := validACLSamplingConfig()
	externalIDs := map[string]string{"owner": "external-observer"}
	externalApp := &ovnnb.SamplingApp{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: externalIDs,
		ID:          int(config.AppIDNew),
		Type:        ovnnb.SamplingAppTypeACLNew,
	}
	externalCollector := &ovnnb.SampleCollector{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: externalIDs,
		ID:          200,
		Name:        "external-collector",
		Probability: 100,
		SetID:       200,
	}
	operations, err := client.Create(externalApp)
	require.NoError(t, err)
	collectorOps, err := client.Create(externalCollector)
	require.NoError(t, err)
	operations = append(operations, collectorOps...)
	require.NoError(t, client.Transact("seed-unowned-sampling-objects", operations))

	require.NoError(t, client.ReconcileACLSampling(config))
	require.Eventually(t, func() bool {
		apps, appErr := client.listSamplingApps()
		collectors, collectorErr := client.listSampleCollectors()
		return appErr == nil && collectorErr == nil && len(apps) == 2 && len(collectors) == 3
	}, time.Second, 10*time.Millisecond)

	const pgName = "cleanup-policy.default"
	seedSampledNetworkPolicyACL(t, client, config, pgName, "cleanup-policy", "11111111-aaaa-bbbb-cccc-222222222222")

	config.Enabled = false
	require.NoError(t, client.ReconcileACLSampling(config))
	require.Eventually(t, func() bool {
		acls, aclErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		apps, appErr := client.listSamplingApps()
		collectors, collectorErr := client.listSampleCollectors()
		samples, sampleErr := client.listSamples()
		return aclErr == nil && appErr == nil && collectorErr == nil && sampleErr == nil &&
			len(acls) == 1 && acls[0].SampleNew == nil && acls[0].SampleEst == nil &&
			acls[0].ExternalIDs[sampleFeatureExternalID] == "" &&
			len(apps) == 1 && maps.Equal(apps[0].ExternalIDs, externalIDs) &&
			len(collectors) == 1 && maps.Equal(collectors[0].ExternalIDs, externalIDs) && len(samples) == 0
	}, 3*time.Second, 10*time.Millisecond)
}

func TestReconcileACLSamplingDisabledRetainsReferencedOwnedCollector(t *testing.T) {
	client := newACLSamplingTestClient(t, "cleanup-retained-collector")
	config := validACLSamplingConfig()
	require.NoError(t, client.ReconcileACLSampling(config))
	waitForACLSamplingObjects(t, client)

	const pgName = "retained-policy.default"
	sampled := seedSampledNetworkPolicyACL(t, client, config, pgName, "retained-policy", "33333333-aaaa-bbbb-cccc-444444444444")
	external := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, ovnnb.ACLActionAllowRelated, "external-reference")
	external.ExternalIDs[ExternalIDVendor] = "external-observer"
	external.ExternalIDs[sampleFeatureExternalID] = networkPolicySampleFeature
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, external))
	waitForACLCount(t, client, pgName, 2)
	acls, err := client.ListAcls("", map[string]string{aclParentKey: pgName})
	require.NoError(t, err)
	externalACL := aclByName(t, acls, "external-reference")
	externalACL.SampleNew = sampled.SampleNew
	updateOps, err := client.Database.Where(&externalACL).Update(&externalACL, &externalACL.SampleNew)
	require.NoError(t, err)
	require.NoError(t, client.Transact("seed-external-sample-reference", updateOps))
	require.Eventually(t, func() bool {
		current, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		return listErr == nil && aclByName(t, current, "external-reference").SampleNew != nil
	}, time.Second, 10*time.Millisecond)

	config.Enabled = false
	require.NoError(t, client.ReconcileACLSampling(config))
	require.Eventually(t, func() bool {
		collectors, collectorErr := client.listSampleCollectors()
		apps, appErr := client.listSamplingApps()
		current, aclErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		if collectorErr != nil || appErr != nil || aclErr != nil {
			return false
		}
		externalACL = aclByName(t, current, "external-reference")
		return len(collectors) == 1 && collectors[0].ExternalIDs[aclSamplingRoleExternalID] == aclSamplingRoleAllow &&
			len(apps) == 0 && externalACL.SampleNew != nil &&
			externalACL.ExternalIDs[ExternalIDVendor] == "external-observer" &&
			externalACL.ExternalIDs[sampleFeatureExternalID] == networkPolicySampleFeature &&
			aclByName(t, current, "np/retained-policy.default/ingress/IPv4/0").SampleNew == nil
	}, 3*time.Second, 10*time.Millisecond)
}

func seedSampledNetworkPolicyACL(t *testing.T, client *OVNNbClient, config aclsampling.ControllerConfig, pgName, policyName, policyUID string) ovnnb.ACL {
	t.Helper()
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	waitForPortGroup(t, client, pgName)
	aclName := fmt.Sprintf("np/%s.default/ingress/IPv4/0", policyName)
	acl := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, aclName)
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, acl))
	waitForACLCount(t, client, pgName, 1)
	request, err := client.PrepareNetworkPolicyACLSampling(pgName, "default", policyName, policyUID)
	require.NoError(t, err)
	require.NoError(t, client.ApplyNetworkPolicyACLSampling(config, request))

	var sampled ovnnb.ACL
	require.Eventually(t, func() bool {
		acls, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		if listErr != nil || len(acls) != 1 {
			return false
		}
		sampled = acls[0]
		return sampled.SampleNew != nil && sampled.SampleEst != nil
	}, time.Second, 10*time.Millisecond)
	return sampled
}

func waitForACLSamplingObjects(t *testing.T, client *OVNNbClient) ([]ovnnb.SamplingApp, []ovnnb.SampleCollector) {
	t.Helper()
	var apps []ovnnb.SamplingApp
	var collectors []ovnnb.SampleCollector
	require.Eventually(t, func() bool {
		var err error
		apps, err = client.listSamplingApps()
		if err != nil || len(apps) != 2 {
			return false
		}
		collectors, err = client.listSampleCollectors()
		return err == nil && len(collectors) == 2
	}, time.Second, 10*time.Millisecond)
	return apps, collectors
}

func appByType(t *testing.T, apps []ovnnb.SamplingApp, appType string) ovnnb.SamplingApp {
	t.Helper()
	for _, app := range apps {
		if app.Type == appType {
			return app
		}
	}
	t.Fatalf("sampling application %s not found", appType)
	return ovnnb.SamplingApp{}
}

func collectorByRole(t *testing.T, collectors []ovnnb.SampleCollector, role string) ovnnb.SampleCollector {
	t.Helper()
	for _, collector := range collectors {
		if collector.ExternalIDs[aclSamplingRoleExternalID] == role &&
			maps.Equal(collector.ExternalIDs, ownedACLSamplingExternalIDs(aclSamplingKindCollector, role)) {
			return collector
		}
	}
	t.Fatalf("sample collector with role %s not found", role)
	return ovnnb.SampleCollector{}
}
