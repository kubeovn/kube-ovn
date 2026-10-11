package table

import (
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

func cacheStaleError(cause error) error {
	if cause == nil {
		return ErrCacheStale
	}
	return fmt.Errorf("%w: %w", ErrCacheStale, cause)
}

// Sentinel errors classify failures at the table boundary. Callers should use
// errors.Is instead of matching the human-readable error text.
var (
	ErrTransaction          = errors.New("ovsdb transaction failed")
	ErrCommitNotApplied     = errors.New("ovsdb transaction was not applied")
	ErrCacheStale           = errors.New("ovsdb cache is stale")
	ErrInvalidModel         = errors.New("ovsdb invalid model")
	ErrInvalidResult        = errors.New("ovsdb invalid result")
	ErrInvalidPredicate     = errors.New("ovsdb invalid predicate")
	ErrCacheSyncUnsupported = errors.New("ovsdb cache synchronization unsupported")
)

// TransactionError adds the logical method and operation count to a
// transaction failure while preserving the original libovsdb error chain.
// OperationErrors are exposed through Unwrap so errors.As can identify the
// operation that caused a transaction to fail.
type TransactionError struct {
	Method          string
	OperationCount  int
	Err             error
	OperationErrors []*OperationError
}

func (e *TransactionError) Error() string {
	if e == nil {
		return ErrTransaction.Error()
	}
	if e.Err == nil {
		return ErrTransaction.Error()
	}
	return e.Err.Error()
}

func (e *TransactionError) Is(target error) bool {
	return target == ErrTransaction || e != nil && errors.Is(e.Err, target)
}

func (e *TransactionError) Unwrap() error {
	if e == nil {
		return nil
	}
	if len(e.OperationErrors) == 0 {
		return e.Err
	}
	causes := make([]error, 0, len(e.OperationErrors)+1)
	if e.Err != nil {
		causes = append(causes, e.Err)
	}
	for _, operationErr := range e.OperationErrors {
		causes = append(causes, operationErr)
	}
	return errors.Join(causes...)
}

// wrapTransactionError retains the original error and annotates any OVSDB
// operation errors with their transaction method and operation index.
func wrapTransactionError(method string, operations []ovsdb.Operation, err error) error {
	if err == nil {
		return nil
	}
	transactionErr := &TransactionError{
		Method:         method,
		OperationCount: len(operations),
		Err:            err,
	}
	for _, operationErr := range collectOperationErrors(err) {
		operation := operationErr.Operation()
		if operation == nil {
			continue
		}
		index := operationIndex(operations, operation)
		if index < 0 {
			continue
		}
		transactionErr.OperationErrors = append(transactionErr.OperationErrors, &OperationError{
			Method:        method,
			Index:         index,
			OperationType: operation.Op,
			Err:           operationErr,
		})
	}
	return transactionErr
}

func operationIndex(operations []ovsdb.Operation, operation *ovsdb.Operation) int {
	for index := range operations {
		if operation == &operations[index] {
			return index
		}
	}
	return -1
}

func collectOperationErrors(err error) []ovsdb.OperationError {
	collected := make([]ovsdb.OperationError, 0, 1)
	var walk func(error)
	walk = func(current error) {
		if current == nil {
			return
		}
		if operationErr, ok := current.(ovsdb.OperationError); ok {
			collected = append(collected, operationErr)
		}
		switch unwrapped := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range unwrapped.Unwrap() {
				walk(child)
			}
		case interface{ Unwrap() error }:
			walk(unwrapped.Unwrap())
		}
	}
	walk(err)
	return collected
}

// OperationError identifies the operation that failed inside a transaction.
// Err remains the original ovsdb error, so callers can still distinguish
// ovsdb.TimedOut and the other libovsdb operation error types with errors.As.
type OperationError struct {
	Method        string
	Index         int
	OperationType string
	Err           error
}

func (e *OperationError) Error() string {
	if e == nil {
		return "ovsdb operation failed"
	}
	if e.Err == nil {
		return fmt.Sprintf("ovsdb transaction %q operation %d (%s) failed", e.Method, e.Index, e.OperationType)
	}
	return e.Err.Error()
}

func (e *OperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *OperationError) Is(target error) bool {
	return e != nil && errors.Is(e.Err, target)
}
