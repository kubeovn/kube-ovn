// Package compat provides a small, policy-driven seam around libovsdb.
package compat

import (
	"context"
	"errors"
	"time"

	libcache "github.com/ovn-kubernetes/libovsdb/cache"
	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// Backend is the subset of a libovsdb client required by Client.
// Keeping this interface small makes the call layer easy to replace in tests
// and allows callers to avoid depending on the full libovsdb client surface.
type Backend interface {
	Get(context.Context, model.Model) error
	List(context.Context, any) error
	WhereCache(any) ConditionalAPI
	WhereCacheByUUIDs(any, ...string) ConditionalAPI
	Where(...model.Model) ConditionalAPI
	WhereAny(model.Model, ...model.Condition) ConditionalAPI
	WhereAll(model.Model, ...model.Condition) ConditionalAPI
	Select(model.Model, ...any) ([]ovsdb.Operation, error)
	Create(...model.Model) ([]ovsdb.Operation, error)
	Transact(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error)
	Cache() Cache
	Schema() ovsdb.DatabaseSchema
	Connected() bool
	NewMonitor(...MonitorOption) *Monitor
	Monitor(context.Context, *Monitor) (MonitorCookie, error)
	Echo(context.Context) error
	Close()
}

// ConditionalAPI is the operation-building surface returned by selectors.
// It intentionally mirrors only the stable libovsdb conditional operations.
type ConditionalAPI interface {
	List(context.Context, any) error
	Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error)
	Update(model.Model, ...any) ([]ovsdb.Operation, error)
	Delete() ([]ovsdb.Operation, error)
	Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error)
	Select(model.Model, ...any) ([]ovsdb.Operation, error)
}

type conditionalAPI struct {
	backend ConditionalAPI
	context func(context.Context) (context.Context, context.CancelFunc)
}

func (a conditionalAPI) List(ctx context.Context, result any) error {
	ctx, cancel := a.context(ctx)
	defer cancel()
	return a.backend.List(ctx, result)
}

func (a conditionalAPI) Mutate(m model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	return a.backend.Mutate(m, mutations...)
}

func (a conditionalAPI) Update(m model.Model, fields ...any) ([]ovsdb.Operation, error) {
	return a.backend.Update(m, fields...)
}

func (a conditionalAPI) Delete() ([]ovsdb.Operation, error) {
	return a.backend.Delete()
}

func (a conditionalAPI) Wait(condition ovsdb.WaitCondition, timeout *int, m model.Model, fields ...any) ([]ovsdb.Operation, error) {
	return a.backend.Wait(condition, timeout, m, fields...)
}

func (a conditionalAPI) Select(m model.Model, fields ...any) ([]ovsdb.Operation, error) {
	return a.backend.Select(m, fields...)
}

// ErrNotFound is returned when a model is absent from the monitored cache.
var ErrNotFound = client.ErrNotFound

// ErrNotConnected is returned when the backend is disconnected.
var ErrNotConnected = client.ErrNotConnected

// Cache exposes only the cache operation used by kube-ovn.
type Cache interface {
	AddEventHandler(EventHandler)
}

// EventHandler receives cache row changes.
type EventHandler interface {
	OnAdd(table string, model model.Model)
	OnUpdate(table string, old, newModel model.Model)
	OnDelete(table string, model model.Model)
}

// EventHandlerFuncs adapts callbacks to EventHandler.
type EventHandlerFuncs struct {
	AddFunc    func(table string, model model.Model)
	UpdateFunc func(table string, old, newModel model.Model)
	DeleteFunc func(table string, model model.Model)
}

func (e *EventHandlerFuncs) OnAdd(table string, row model.Model) {
	if e.AddFunc != nil {
		e.AddFunc(table, row)
	}
}

func (e *EventHandlerFuncs) OnUpdate(table string, old, newModel model.Model) {
	if e.UpdateFunc != nil {
		e.UpdateFunc(table, old, newModel)
	}
}

func (e *EventHandlerFuncs) OnDelete(table string, row model.Model) {
	if e.DeleteFunc != nil {
		e.DeleteFunc(table, row)
	}
}

// Monitor is the compatibility monitor configuration exposed to callers.
type Monitor struct {
	raw    *client.Monitor
	Method string
	Errors []error
}

// MonitorCookie correlates monitor updates with their originating request.
type MonitorCookie struct {
	raw client.MonitorCookie
}

type monitorConfig struct {
	tables []model.Model
}

// MonitorOption configures a compatibility monitor.
type MonitorOption func(*monitorConfig) error

// WithTable creates a monitor option for a model table.
func WithTable(m model.Model) MonitorOption {
	return func(config *monitorConfig) error {
		config.tables = append(config.tables, m)
		return nil
	}
}

// rawBackend adapts the libovsdb client to the compatibility backend. The raw
// client is intentionally confined to this adapter and the transport factory.
type rawBackend struct {
	client client.Client
}

type rawCache struct {
	cache *libcache.TableCache
}

func (c rawCache) AddEventHandler(handler EventHandler) {
	c.cache.AddEventHandler(handler)
}

// Wrap adapts a libovsdb client to Backend.
func Wrap(raw client.Client) Backend {
	return &rawBackend{client: raw}
}

func (b *rawBackend) Get(ctx context.Context, result model.Model) error {
	return b.client.Get(ctx, result)
}

func (b *rawBackend) List(ctx context.Context, result any) error {
	return b.client.List(ctx, result)
}

func (b *rawBackend) WhereCache(predicate any) ConditionalAPI {
	return b.client.WhereCache(predicate)
}

func (b *rawBackend) WhereCacheByUUIDs(predicate any, uuids ...string) ConditionalAPI {
	return b.client.WhereCacheByUUIDs(predicate, uuids...)
}

func (b *rawBackend) Where(models ...model.Model) ConditionalAPI {
	return b.client.Where(models...)
}

func (b *rawBackend) WhereAny(m model.Model, conditions ...model.Condition) ConditionalAPI {
	return b.client.WhereAny(m, conditions...)
}

func (b *rawBackend) WhereAll(m model.Model, conditions ...model.Condition) ConditionalAPI {
	return b.client.WhereAll(m, conditions...)
}

func (b *rawBackend) Select(m model.Model, fields ...any) ([]ovsdb.Operation, error) {
	return b.client.Select(m, fields...)
}

func (b *rawBackend) Create(models ...model.Model) ([]ovsdb.Operation, error) {
	return b.client.Create(models...)
}

func (b *rawBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	return b.client.Transact(ctx, operations...)
}

func (b *rawBackend) Cache() Cache {
	return rawCache{cache: b.client.Cache()}
}

func (b *rawBackend) Schema() ovsdb.DatabaseSchema {
	return b.client.Schema()
}

func (b *rawBackend) Connected() bool {
	return b.client.Connected()
}

func (b *rawBackend) NewMonitor(options ...MonitorOption) *Monitor {
	config := monitorConfig{}
	monitor := &Monitor{}
	for _, option := range options {
		if err := option(&config); err != nil {
			monitor.Errors = append(monitor.Errors, err)
		}
	}
	rawOptions := make([]client.MonitorOption, 0, len(config.tables))
	for _, table := range config.tables {
		rawOptions = append(rawOptions, client.WithTable(table))
	}
	monitor.raw = b.client.NewMonitor(rawOptions...)
	monitor.Method = monitor.raw.Method
	monitor.Errors = append(monitor.Errors, monitor.raw.Errors...)
	return monitor
}

func (b *rawBackend) Monitor(ctx context.Context, monitor *Monitor) (MonitorCookie, error) {
	if monitor == nil || monitor.raw == nil {
		return MonitorCookie{}, errors.New("monitor is nil")
	}
	monitor.raw.Method = monitor.Method
	cookie, err := b.client.Monitor(ctx, monitor.raw)
	return MonitorCookie{raw: cookie}, err
}

func (b *rawBackend) Echo(ctx context.Context) error {
	return b.client.Echo(ctx)
}

func (b *rawBackend) Close() {
	b.client.Close()
}

// RetryPolicy controls retries for transient connection errors.
type RetryPolicy struct {
	Attempts int
	Delay    time.Duration
}

// Client centralizes timeout, retry, operation construction, and result
// checking for a libovsdb database connection.
type Client struct {
	backend Backend
	timeout time.Duration
	retry   RetryPolicy
}

// New creates a call layer over a libovsdb backend.
func New(backend Backend, timeout time.Duration, retry RetryPolicy) *Client {
	if retry.Attempts < 0 {
		retry.Attempts = 0
	}
	if retry.Delay <= 0 {
		retry.Delay = 50 * time.Millisecond
	}
	return &Client{backend: backend, timeout: timeout, retry: retry}
}

func (c *Client) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}

// Get reads one model from the monitored cache.
func (c *Client) Get(ctx context.Context, result model.Model) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	return c.backend.Get(ctx, result)
}

// List reads all monitored rows for a model type.
func (c *Client) List(ctx context.Context, result any) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	return c.backend.List(ctx, result)
}

// ListWhereCache reads rows selected by a cache predicate.
func (c *Client) ListWhereCache(ctx context.Context, predicate, result any) error {
	return c.WhereCache(predicate).List(ctx, result)
}

// ListWhereCacheByUUIDs reads rows selected by a predicate and UUID allowlist.
func (c *Client) ListWhereCacheByUUIDs(ctx context.Context, predicate, result any, uuids ...string) error {
	return c.WhereCacheByUUIDs(predicate, uuids...).List(ctx, result)
}

// WhereCacheByUUIDs returns a selector evaluated against a UUID allowlist.
func (c *Client) WhereCacheByUUIDs(predicate any, uuids ...string) ConditionalAPI {
	return c.wrapConditional(c.backend.WhereCacheByUUIDs(predicate, uuids...))
}

// Where returns a selector based on model indexes.
func (c *Client) Where(models ...model.Model) ConditionalAPI {
	return c.wrapConditional(c.backend.Where(models...))
}

// WhereCache returns a selector evaluated against the local cache.
func (c *Client) WhereCache(predicate any) ConditionalAPI {
	return c.wrapConditional(c.backend.WhereCache(predicate))
}

// WhereAny returns a selector matching any explicit condition.
func (c *Client) WhereAny(m model.Model, conditions ...model.Condition) ConditionalAPI {
	return c.wrapConditional(c.backend.WhereAny(m, conditions...))
}

// WhereAll returns a selector matching all explicit conditions.
func (c *Client) WhereAll(m model.Model, conditions ...model.Condition) ConditionalAPI {
	return c.wrapConditional(c.backend.WhereAll(m, conditions...))
}

func (c *Client) wrapConditional(backend ConditionalAPI) ConditionalAPI {
	return conditionalAPI{backend: backend, context: c.context}
}

// Select builds a select operation for a model.
func (c *Client) Select(m model.Model, fields ...any) ([]ovsdb.Operation, error) {
	return c.backend.Select(m, fields...)
}

// Create builds insert operations for models.
func (c *Client) Create(models ...model.Model) ([]ovsdb.Operation, error) {
	return c.backend.Create(models...)
}

// Cache returns the monitored table cache.
func (c *Client) Cache() Cache {
	return c.backend.Cache()
}

// Schema returns the database schema used by the backend.
func (c *Client) Schema() ovsdb.DatabaseSchema {
	return c.backend.Schema()
}

// Connected reports whether the backend is connected.
func (c *Client) Connected() bool {
	return c.backend.Connected()
}

// NewMonitor creates a monitor through the backend.
func (c *Client) NewMonitor(options ...MonitorOption) *Monitor {
	return c.backend.NewMonitor(options...)
}

// Monitor installs a monitor through the backend.
func (c *Client) Monitor(ctx context.Context, monitor *Monitor) (MonitorCookie, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()
	return c.backend.Monitor(ctx, monitor)
}

// Echo checks the backend connection.
func (c *Client) Echo(ctx context.Context) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	return c.backend.Echo(ctx)
}

// Close closes the backend connection.
func (c *Client) Close() {
	c.backend.Close()
}

// CreateAndTransact builds insert operations and submits them atomically.
func (c *Client) CreateAndTransact(ctx context.Context, method string, models ...model.Model) error {
	operations, err := c.backend.Create(models...)
	if err != nil {
		return err
	}
	return c.Transact(ctx, method, operations)
}

// UpdateAndTransact builds update operations for selector and submits them.
func (c *Client) UpdateAndTransact(ctx context.Context, method string, selector, update model.Model, fields ...any) error {
	operations, err := c.backend.Where(selector).Update(update, fields...)
	if err != nil {
		return err
	}
	return c.Transact(ctx, method, operations)
}

// MutateAndTransact builds mutation operations and submits them atomically.
func (c *Client) MutateAndTransact(ctx context.Context, method string, selector model.Model, mutations ...model.Mutation) error {
	operations, err := c.Mutate(selector, mutations...)
	if err != nil {
		return err
	}
	return c.Transact(ctx, method, operations)
}

// Mutate builds mutation operations without submitting them. Callers can
// combine operations from multiple models into one transaction.
func (c *Client) Mutate(selector model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	return c.backend.Where(selector).Mutate(selector, mutations...)
}

// DeleteAndTransact deletes rows selected by one or more indexed models.
func (c *Client) DeleteAndTransact(ctx context.Context, method string, selectors ...model.Model) error {
	if len(selectors) == 0 {
		return nil
	}
	operations, err := c.backend.Where(selectors...).Delete()
	if err != nil {
		return err
	}
	return c.Transact(ctx, method, operations)
}

// DeleteWhereCacheAndTransact deletes rows selected by a cache predicate.
func (c *Client) DeleteWhereCacheAndTransact(ctx context.Context, method string, predicate any) error {
	operations, err := c.backend.WhereCache(predicate).Delete()
	if err != nil {
		return err
	}
	return c.Transact(ctx, method, operations)
}

// Transact submits operations, retries only transient disconnections, and
// validates every operation result before returning.
func (c *Client) Transact(ctx context.Context, _ string, operations []ovsdb.Operation) error {
	_, err := c.TransactResults(ctx, operations...)
	return err
}

// TransactResults submits operations, retries transient disconnections, checks
// operation results, and returns the validated server response.
func (c *Client) TransactResults(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	if len(operations) == 0 {
		return nil, nil
	}
	ctx, cancel := c.context(ctx)
	defer cancel()

	for attempt := 0; ; attempt++ {
		results, err := c.backend.Transact(ctx, operations...)
		if err == nil {
			_, err = ovsdb.CheckOperationResults(results, operations)
		}
		if err == nil {
			return results, nil
		}
		if !errors.Is(err, ErrNotConnected) || attempt >= c.retry.Attempts {
			return nil, err
		}
		if err := c.wait(ctx, attempt); err != nil {
			return nil, err
		}
	}
}

func (c *Client) wait(ctx context.Context, attempt int) error {
	delay := c.retry.Delay * time.Duration(1<<min(attempt, 6))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
