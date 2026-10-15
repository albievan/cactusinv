// Package domain holds cactusInv's entities, value objects and business rules.
// It is pure Go: no I/O, no clocks, no imports outside the standard library.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel domain errors. Adapters map them to transport errors; nothing else
// should depend on their text.
var (
	// ErrNotFound means the requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a uniqueness rule or optimistic-lock version check failed.
	ErrConflict = errors.New("conflict")
	// ErrValidation means input broke a domain invariant. Use *ValidationError for details.
	ErrValidation = errors.New("validation failed")
	// ErrCurrencyMismatch means arithmetic was attempted across currencies.
	ErrCurrencyMismatch = errors.New("currency mismatch")
	// ErrNoPriceForCurrency means a product has no price in the requested currency.
	ErrNoPriceForCurrency = errors.New("product has no price in currency")
	// ErrNoBankAccountForCurrency means no usable bank account exists for a currency.
	ErrNoBankAccountForCurrency = errors.New("no bank account for currency")
	// ErrInvalidTransition means an invoice, schedule or e-mail state change is not allowed.
	ErrInvalidTransition = errors.New("invalid state transition")
	// ErrInactive means a referenced currency, customer, product or bank account is inactive.
	ErrInactive = errors.New("inactive")
	// ErrUnauthenticated means credentials or the session are missing or wrong.
	ErrUnauthenticated = errors.New("unauthenticated")
)

// ValidationError describes which field broke which rule. It matches ErrValidation
// with errors.Is.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// Is makes errors.Is(err, ErrValidation) true for every ValidationError.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// invalid builds a *ValidationError.
func invalid(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}
