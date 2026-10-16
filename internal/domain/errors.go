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
	// ErrBackupInvalid means a backup could not be opened or did not verify: it is corrupt,
	// truncated, tampered with, or the passphrase or key is wrong. Deliberately one error,
	// so it does not say which check failed (ADR-010).
	ErrBackupInvalid = errors.New("backup is invalid, damaged or the passphrase is wrong")
	// ErrBackupTooNew means a backup comes from a newer database schema than this version.
	ErrBackupTooNew = errors.New("backup is from a newer version")
	// ErrBackupUnsupported means a backup's format version or schema is not supported.
	ErrBackupUnsupported = errors.New("backup format is not supported")
	// ErrBusy means the operation cannot run now (another backup or restore is running, or
	// an e-mail is being sent); try again shortly.
	ErrBusy = errors.New("busy")
	// ErrOutcomeUnknown means an operation's result cannot be determined (e.g. a restore's
	// COMMIT failed in a way that leaves it open whether it took effect); check before
	// retrying.
	ErrOutcomeUnknown = errors.New("outcome unknown")
	// ErrForbidden means the signed-in user may not do this (e.g. disable or reset their
	// own account).
	ErrForbidden = errors.New("forbidden")
	// ErrNoAccess means the signed-in user's groups do not give them the permission an
	// operation needs (ADR-012).
	ErrNoAccess = errors.New("no access")
	// ErrLastActiveUser means the change would leave no active user (ADR-011).
	ErrLastActiveUser = errors.New("the last active user cannot be disabled")
	// ErrPasswordChangeRequired means the session's user must change their password before
	// doing anything else (ADR-011).
	ErrPasswordChangeRequired = errors.New("password change required")
	// ErrNotConfigured means a required setting or secret is missing (e.g. backup_key).
	ErrNotConfigured = errors.New("not configured")
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
