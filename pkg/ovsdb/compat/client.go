// Package compat provides a small, policy-driven seam around libovsdb.
package compat

import (
	"context"
	"errors"
	"time"

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
	WhereCache(any) client.ConditionalAPI
	WhereCacheByUUIDs(any, ...string) client.ConditionalAPI
	Where(...model.Model) client.ConditionalAPI
	WhereAny(model.Model, ...model.Condition) client.ConditionalAPI
	WhereAll(model.Model, ...model.Condition) client.ConditionalAPI
	Select(model.Model, ...any) ([]ovsdb.Operation, error)
	Create(...model.Model) ([]ovsdb.Operation, error)
	Transact(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error)
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

// ErrNotFound is returned when a model is absent from the monitored cache.
var ErrNotFound = client.ErrNotFound

// Monitor aliases keep monitor setup behind the compatibility package.
type (
	Monitor       = client.Monitor
	MonitorCookie = client.MonitorCookie
	MonitorOption = client.MonitorOption
)

// WithTable creates a monitor option for a model table.
func WithTable(m model.Model) MonitorOption {
	return client.WithTable(m)
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
	ctx, cancel := c.context(ctx)
	defer cancel()
	return c.backend.WhereCache(predicate).List(ctx, result)
}

// ListWhereCacheByUUIDs reads rows selected by a predicate and UUID allowlist.
func (c *Client) ListWhereCacheByUUIDs(ctx context.Context, predicate, result any, uuids ...string) error {
	ctx, cancel := c.context(ctx)
	defer cancel()
	return c.backend.WhereCacheByUUIDs(predicate, uuids...).List(ctx, result)
}

// WhereCacheByUUIDs returns a selector evaluated against a UUID allowlist.
func (c *Client) WhereCacheByUUIDs(predicate any, uuids ...string) ConditionalAPI {
	return c.backend.WhereCacheByUUIDs(predicate, uuids...)
}

// Where returns a selector based on model indexes.
func (c *Client) Where(models ...model.Model) ConditionalAPI {
	return c.backend.Where(models...)
}

// WhereCache returns a selector evaluated against the local cache.
func (c *Client) WhereCache(predicate any) ConditionalAPI {
	return c.backend.WhereCache(predicate)
}

// WhereAny returns a selector matching any explicit condition.
func (c *Client) WhereAny(m model.Model, conditions ...model.Condition) ConditionalAPI {
	return c.backend.WhereAny(m, conditions...)
}

// WhereAll returns a selector matching all explicit conditions.
func (c *Client) WhereAll(m model.Model, conditions ...model.Condition) ConditionalAPI {
	return c.backend.WhereAll(m, conditions...)
}

// Select builds a select operation for a model.
func (c *Client) Select(m model.Model, fields ...any) ([]ovsdb.Operation, error) {
	return c.backend.Select(m, fields...)
}

// Create builds insert operations for models.
func (c *Client) Create(models ...model.Model) ([]ovsdb.Operation, error) {
	return c.backend.Create(models...)
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
	_, err := c.TransactResults(ctx, operations)
	return err
}

// TransactResults submits operations, retries transient disconnections, checks
// operation results, and returns the validated server response.
func (c *Client) TransactResults(ctx context.Context, operations []ovsdb.Operation) ([]ovsdb.OperationResult, error) {
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
		if !errors.Is(err, client.ErrNotConnected) || attempt >= c.retry.Attempts {
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
