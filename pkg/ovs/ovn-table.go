package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func timeoutCtx(db *compat.Database) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), db.Timeout)
}

func matchExternalIDs(actual, wanted map[string]string) bool {
	return matchExternalIDsMode(actual, wanted, true)
}

func matchExternalIDsMode(actual, wanted map[string]string, emptyValueMeansPresent bool) bool {
	if len(actual) < len(wanted) {
		return false
	}
	if len(actual) == 0 {
		return true
	}
	for k, v := range wanted {
		if len(v) == 0 && emptyValueMeansPresent {
			if len(actual[k]) == 0 {
				return false
			}
			continue
		}
		if actual[k] != v {
			return false
		}
	}
	return true
}

func hasVendor(externalIDs map[string]string) bool {
	return len(externalIDs) != 0 && externalIDs["vendor"] == util.CniTypeName
}

func pointersOf[T any](rows []T) []*T {
	out := make([]*T, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out
}

func uniquePtrs[T any](rows []*T, ignoreNotFound bool, notFound, duplicated error) (*T, error) {
	switch len(rows) {
	case 0:
		if ignoreNotFound {
			return nil, nil
		}
		return nil, notFound
	case 1:
		return rows[0], nil
	default:
		return nil, duplicated
	}
}

func getIndexed[T any](db *compat.Database, row *T, ignoreNotFound bool) (*T, error) {
	ctx, cancel := timeoutCtx(db)
	defer cancel()
	if err := db.Table(row).Get(ctx, row); err != nil {
		if ignoreNotFound && errors.Is(err, compat.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func filterTimeout[T any](db *compat.Database, prototype model.Model, predicate func(*T) bool) ([]T, error) {
	ctx, cancel := timeoutCtx(db)
	defer cancel()
	return compat.Filter[T](ctx, db, prototype, predicate)
}

func filterLogged[T any](db *compat.Database, prototype model.Model, predicate func(*T) bool, wrap func(error) error) ([]T, error) {
	rows, err := filterTimeout(db, prototype, predicate)
	if err != nil {
		klog.Error(err)
		if wrap != nil {
			return nil, wrap(err)
		}
		return nil, err
	}
	return rows, nil
}

func filterWrap[T any](db *compat.Database, prototype model.Model, predicate func(*T) bool, wrap func(error) error) ([]T, error) {
	rows, err := filterTimeout(db, prototype, predicate)
	if err != nil && wrap != nil {
		return nil, wrap(err)
	}
	return rows, err
}

func filterAll[T any](db *compat.Database, prototype model.Model, wrap func(error) error) ([]T, error) {
	return filterWrap(db, prototype, func(*T) bool { return true }, wrap)
}

func getIndexedFmt[T any](db *compat.Database, row *T, ignoreNotFound bool, wrap func(error) error) (*T, error) {
	result, err := getIndexed(db, row, ignoreNotFound)
	if err != nil && wrap != nil {
		return nil, wrap(err)
	}
	return result, err
}

func getIndexedWrap[T any](db *compat.Database, row *T, ignoreNotFound bool, wrap func(error) error) (*T, error) {
	result, err := getIndexed(db, row, ignoreNotFound)
	if err != nil {
		klog.Error(err)
		if wrap != nil {
			return nil, wrap(err)
		}
		return nil, err
	}
	return result, nil
}

func getIndexedLogged[T any](db *compat.Database, row *T) (*T, error) {
	return getIndexedWrap(db, row, false, nil)
}

func logRet[T any](v T, err error) (T, error) {
	if err != nil {
		klog.Error(err)
	}
	return v, err
}

func logErr(err error) error {
	if err != nil {
		klog.Error(err)
	}
	return err
}

func logFmt(format string, args ...any) error {
	return logErr(fmt.Errorf(format, args...))
}

func logWrap(err error, wrap func(error) error) error {
	if err == nil {
		return nil
	}
	klog.Error(err)
	if wrap != nil {
		return wrap(err)
	}
	return err
}

func requireName(name, emptyErr string) error {
	return requireValue(name, errors.New(emptyErr))
}

func requireValue(v string, err error) error {
	if v != "" {
		return nil
	}
	return logErr(err)
}

func namesFrom[T any](rows []T, err error, names func([]T) []string) ([]string, error) {
	if err != nil {
		return nil, logErr(err)
	}
	return names(rows), nil
}

func listByUUIDs[T any](db *compat.Database, prototype model.Model, uuids []string, uuidOf func(*T) string, filter func(*T) bool) ([]*T, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	uuidSet := set.New(uuids...)
	rows, err := filterTimeout(db, prototype, func(row *T) bool {
		if !uuidSet.Has(uuidOf(row)) {
			return false
		}
		return filter == nil || filter(row)
	})
	if err != nil {
		return nil, err
	}
	out := make([]*T, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}

func modelsAndUUIDs[T any](rows []*T, uuidOf func(*T) string) ([]model.Model, []string) {
	models := make([]model.Model, 0, len(rows))
	uuids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		models = append(models, row)
		uuids = append(uuids, uuidOf(row))
	}
	return models, uuids
}

func appendOps(parts ...[]ovsdb.Operation) []ovsdb.Operation {
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	ops := make([]ovsdb.Operation, 0, n)
	for _, part := range parts {
		ops = append(ops, part...)
	}
	return ops
}

func createAndAttachOps(store namedStore, prototype model.Model, models []model.Model, attach func([]string) ([]ovsdb.Operation, error), uuids []string) ([]ovsdb.Operation, error) {
	if len(models) == 0 {
		return nil, nil
	}
	createOps, err := store.Table(prototype).CreateOps(models...)
	if err != nil {
		return nil, err
	}
	attachOps, err := attach(uuids)
	if err != nil {
		return nil, err
	}
	return appendOps(createOps, attachOps), nil
}

func createAndAttach[T any](store namedStore, method string, prototype model.Model, rows []*T, uuidOf func(*T) string, attach func([]string) ([]ovsdb.Operation, error)) error {
	models, uuids := modelsAndUUIDs(rows, uuidOf)
	ops, err := createAndAttachOps(store, prototype, models, attach, uuids)
	if err != nil {
		return err
	}
	plan := compat.NewTxPlan(method)
	plan.Add(ops...)
	return store.Execute(context.Background(), plan)
}

func (c *OVNNbClient) updateModel(method string, row model.Model, fields ...any) error {
	return c.Table(row).Update(context.Background(), method, row, row, fields...)
}

func (c *OVNNbClient) updateModelLogged(method string, row model.Model, wrap func(error) error, fields ...any) error {
	if err := c.updateModel(method, row, fields...); err != nil {
		klog.Error(err)
		if wrap != nil {
			return wrap(err)
		}
		return err
	}
	return nil
}

func connectOvsdb(dbName, addr string, dbModel model.ClientDBModel, monitors []compat.MonitorOption, connTimeout, inactivityTimeout, maxRetry int, logName string) (compat.Backend, error) {
	var (
		backend compat.Backend
		err     error
	)
	for try := 0; ; try++ {
		backend, err = ovsclient.NewOvsDbClient(dbName, addr, dbModel, monitors, connTimeout, inactivityTimeout)
		if err == nil {
			return backend, nil
		}
		klog.Errorf("failed to create %s client: %v", logName, err)
		if try >= maxRetry {
			return nil, err
		}
		time.Sleep(2 * time.Second)
	}
}

func newObservedDatabase(backend compat.Backend, timeout int, name string) *compat.Database {
	return compat.NewDatabase(backend, time.Duration(timeout)*time.Second, compat.RetryPolicy{},
		compat.WithDatabaseName(name), compat.WithTransactionObserver(ovsTransactionObserver{}))
}

func uniqueOwnerName[T any](rows []T, uuid, child, parent string, nameOf func(*T) string) (string, error) {
	if len(rows) == 0 {
		return "", fmt.Errorf("no %s found for %s %s", parent, child, uuid)
	}
	if len(rows) != 1 {
		names := make([]string, len(rows))
		for i := range rows {
			names[i] = nameOf(&rows[i])
		}
		return "", fmt.Errorf("multiple %s found for %s %s: %s", parent, child, uuid, strings.Join(names, ", "))
	}
	return nameOf(&rows[0]), nil
}

func findNamedRow[T any](ctx context.Context, provider compat.TableProvider, prototype model.Model, name, kind string, required bool, nameOf func(*T) string) (*T, error) {
	rows, err := compat.Filter[T](ctx, provider, prototype, func(row *T) bool {
		return nameOf(row) == name
	})
	if err != nil {
		return nil, fmt.Errorf("find OVS %s %q: %w", kind, name, err)
	}
	if required {
		if len(rows) != 1 {
			return nil, fmt.Errorf("expected one OVS %s %q, found %d", kind, name, len(rows))
		}
		return &rows[0], nil
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("expected at most one OVS %s %q, found %d", kind, name, len(rows))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func kubeOvnNames[T any](db *compat.Database, prototype model.Model, nameOf func(*T) string, externalIDsOf func(*T) map[string]string, listErr string) (map[string]bool, error) {
	rows, err := filterTimeout(db, prototype, func(row *T) bool {
		return hasVendor(externalIDsOf(row))
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", listErr, err)
	}
	names := make(map[string]bool, len(rows))
	for i := range rows {
		names[nameOf(&rows[i])] = true
	}
	return names, nil
}

func stampVendor(externalIDs *map[string]string) {
	if *externalIDs == nil {
		*externalIDs = make(map[string]string)
	}
	(*externalIDs)["vendor"] = util.CniTypeName
}

func clonedVendorIDs(externalIDs map[string]string) map[string]string {
	final := maps.Clone(externalIDs)
	stampVendor(&final)
	return final
}

func copyStringMapInto(dst *map[string]string, src map[string]string) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]string, len(src))
	}
	maps.Copy(*dst, src)
}

func migrateVendorRows[T any](
	c *OVNNbClient,
	prototype model.Model,
	pred func(*T) bool,
	method, kind string,
	nameOf func(*T) string,
	externalIDsOf func(*T) *map[string]string,
) error {
	rows, err := filterTimeout(c.Database, prototype, pred)
	if err != nil {
		return fmt.Errorf("failed to list %s for migration: %w", kind, err)
	}
	if len(rows) == 0 {
		klog.Infof("no %s need vendor migration", kind)
		return nil
	}
	klog.Infof("migrating %d %s to add vendor tag", len(rows), kind)

	ops := make([]ovsdb.Operation, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		ids := externalIDsOf(row)
		stampVendor(ids)
		op, err := c.Table(prototype).UpdateOps(row, row, ids)
		if err != nil {
			klog.Errorf("failed to generate update operation for %s %s: %v", kind, nameOf(row), err)
			continue
		}
		ops = append(ops, op...)
	}
	if len(ops) == 0 {
		return nil
	}
	if err := c.Transact(method, ops); err != nil {
		return fmt.Errorf("failed to migrate %s vendor tags: %w", kind, err)
	}
	klog.Infof("successfully migrated %d %s", len(rows), kind)
	return nil
}

type ovsTransactor interface {
	Transact(method string, operations []ovsdb.Operation) error
}

func transactOps(c ovsTransactor, method string, ops []ovsdb.Operation) error {
	if len(ops) == 0 {
		return nil
	}
	return c.Transact(method, ops)
}

type tableTransactor interface {
	Transact(ctx context.Context, method string, operations ...ovsdb.Operation) error
}

func transactTable(ctx context.Context, table tableTransactor, method string, ops []ovsdb.Operation) error {
	if len(ops) == 0 {
		return nil
	}
	return table.Transact(ctx, method, ops...)
}

func deleteWhereCache[T any](c *OVNNbClient, prototype model.Model, method string, filter func(*T) bool, wrapGen, wrapTx func(error) error) error {
	op, err := c.Database.Table(prototype).WhereCache(func(row *T) bool {
		return filter == nil || filter(row)
	}).Delete()
	if err != nil {
		klog.Error(err)
		if wrapGen != nil {
			return wrapGen(err)
		}
		return err
	}
	if err := c.Transact(method, op); err != nil {
		klog.Error(err)
		if wrapTx != nil {
			return wrapTx(err)
		}
		return err
	}
	return nil
}

func wrapErr(format string, args ...any) func(error) error {
	return func(err error) error {
		all := make([]any, len(args)+1)
		copy(all, args)
		all[len(args)] = err
		return fmt.Errorf(format, all...)
	}
}

func transactGeneratedOn(c ovsTransactor, method string, ops []ovsdb.Operation, err error, wrapGen, wrapTx func(error) error) error {
	if err != nil {
		klog.Error(err)
		if wrapGen != nil {
			return wrapGen(err)
		}
		return err
	}
	if txErr := transactOps(c, method, ops); txErr != nil {
		klog.Error(txErr)
		if wrapTx != nil {
			return wrapTx(txErr)
		}
		return txErr
	}
	return nil
}

func (c *OVNNbClient) transactGenerated(method string, ops []ovsdb.Operation, err error, wrapGen, wrapTx func(error) error) error {
	return transactGeneratedOn(c, method, ops, err, wrapGen, wrapTx)
}

func lookupUUIDs[T any](names []string, get func(string, bool) (*T, error), uuidOf func(*T) string) ([]string, error) {
	uuids := make([]string, 0, len(names))
	for _, name := range names {
		row, err := get(name, true)
		if err != nil {
			return nil, logErr(err)
		}
		if row != nil {
			uuids = append(uuids, uuidOf(row))
		}
	}
	return uuids, nil
}

func fieldSlice[T any](rows []T, field func(*T) string) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, field(&rows[i]))
	}
	return out
}

func existsByGet[T any](get func(string, bool) (*T, error), name string) (bool, error) {
	row, err := get(name, true)
	return row != nil, err
}

func modelsOf[T any](rows []*T) []model.Model {
	models := make([]model.Model, len(rows))
	for i, row := range rows {
		models[i] = row
	}
	return models
}

func modelsFrom[T any](rows []T) []model.Model {
	models := make([]model.Model, len(rows))
	for i := range rows {
		models[i] = &rows[i]
	}
	return models
}

func deleteNamedRows[T any](c *OVNNbClient, names []string, get func(string, bool) (*T, error), prototype model.Model, method, getFmt, delFmt string) error {
	delList := make([]*T, 0, len(names))
	for _, name := range names {
		row, err := get(name, true)
		if err != nil {
			return fmt.Errorf(getFmt, name, err)
		}
		if row == nil {
			continue
		}
		delList = append(delList, row)
	}
	if len(delList) == 0 {
		return nil
	}
	if err := c.Database.Table(prototype).Delete(context.Background(), method, modelsOf(delList)...); err != nil {
		return logWrap(err, wrapErr(delFmt, names))
	}
	return nil
}

func (c *OVNNbClient) loadBalancerUUIDs(lbNames ...string) ([]string, error) {
	return lookupUUIDs(lbNames, c.GetLoadBalancer, func(lb *ovnnb.LoadBalancer) string { return lb.UUID })
}

func (c *OVNNbClient) logicalSwitchPortUUIDs(lspNames ...string) ([]string, error) {
	return lookupUUIDs(lspNames, c.GetLogicalSwitchPort, func(lsp *ovnnb.LogicalSwitchPort) string { return lsp.UUID })
}

func mapDeleteMutation(field *map[string]string, key, value string) model.Mutation {
	return model.Mutation{Field: field, Value: map[string]string{key: value}, Mutator: ovsdb.MutateOperationDelete}
}

func mapInsertMutation(field *map[string]string, key, value string) model.Mutation {
	return model.Mutation{Field: field, Value: map[string]string{key: value}, Mutator: ovsdb.MutateOperationInsert}
}

func mapReplaceMutations(field *map[string]string, current map[string]string, key, newValue string) []model.Mutation {
	if current[key] == newValue {
		return nil
	}
	mutations := make([]model.Mutation, 0, 2)
	if old, ok := current[key]; ok {
		mutations = append(mutations, mapDeleteMutation(field, key, old))
	}
	if newValue != "" {
		mutations = append(mutations, mapInsertMutation(field, key, newValue))
	}
	return mutations
}

func pollUntil[T any](db *compat.Database, fn func() (T, bool, error), timeoutWrap func(error) error) (T, error) {
	ctx, cancel := timeoutCtx(db)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var zero T
	for {
		value, done, err := fn()
		if err != nil {
			return zero, err
		}
		if done {
			return value, nil
		}
		select {
		case <-ctx.Done():
			if timeoutWrap != nil {
				return zero, timeoutWrap(ctx.Err())
			}
			return zero, ctx.Err()
		case <-ticker.C:
		}
	}
}

func listWhere[T any](db *compat.Database, prototype model.Model, indexes ...model.Model) ([]*T, error) {
	if len(indexes) == 0 {
		return nil, nil
	}
	ctx, cancel := timeoutCtx(db)
	defer cancel()
	rows := make([]*T, 0)
	if err := db.Table(prototype).Where(indexes...).List(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func rowUUIDs[T any](rows []*T, uuidOf func(*T) string) []string {
	uuids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			uuids = append(uuids, uuidOf(row))
		}
	}
	return uuids
}
