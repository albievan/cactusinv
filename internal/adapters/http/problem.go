package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/albievan/cactusinv/internal/domain"
)

// problemFor maps an error to an RFC 9457 problem. Only this adapter knows HTTP status
// codes; the domain only knows sentinels.
func problemFor(err error) Problem {
	var ve *domain.ValidationError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return Problem{
			Type: problemType("payload-too-large"), Title: "Too large", Status: http.StatusRequestEntityTooLarge,
			Detail: fmt.Sprintf("The file is larger than %d MiB.", tooLarge.Limit>>20),
		}
	case errors.Is(err, domain.ErrBackupInvalid):
		return Problem{
			Type: problemType("backup-invalid"), Title: "The backup cannot be used", Status: http.StatusUnprocessableEntity,
			Detail: "It is damaged, was changed, or the passphrase (or this server's backup key) does not open it.",
		}
	case errors.Is(err, domain.ErrBackupTooNew):
		return Problem{
			Type: problemType("backup-too-new"), Title: "The backup is from a newer version", Status: http.StatusUnprocessableEntity,
			Detail: "Upgrade cactusInv to at least the version that made the backup, then restore it.",
		}
	case errors.Is(err, domain.ErrBackupUnsupported):
		return Problem{Type: problemType("backup-unsupported"), Title: "This backup format is not supported", Status: http.StatusUnprocessableEntity}
	case errors.Is(err, domain.ErrBusy):
		return Problem{
			Type: problemType("busy"), Title: "Busy", Status: http.StatusConflict,
			Detail: "Another backup or restore is running, or an e-mail is being sent. Try again in a minute.",
		}
	case errors.Is(err, domain.ErrNotConfigured):
		return Problem{
			Type: problemType("not-configured"), Title: "Not configured", Status: http.StatusServiceUnavailable,
			Detail: "This needs the server backup key (backup_key) or the restore staging database; see the README.",
		}
	case errors.Is(err, domain.ErrOutcomeUnknown):
		return Problem{
			Type: problemType("restore-outcome-unknown"), Title: "The restore's result is unknown", Status: http.StatusInternalServerError,
			Detail: "The database did not confirm the restore. Check the audit log and the data before trying again.",
		}
	case errors.As(err, &ve):
		return Problem{Type: problemType("validation"), Title: "Invalid input", Status: http.StatusUnprocessableEntity, Detail: ve.Error(), Field: ve.Field}
	case errors.Is(err, domain.ErrValidation):
		return Problem{Type: problemType("validation"), Title: "Invalid input", Status: http.StatusUnprocessableEntity, Detail: err.Error()}
	case errors.Is(err, domain.ErrPasswordChangeRequired):
		return Problem{
			Type: problemType("password-change-required"), Title: "Change your password first", Status: http.StatusForbidden,
			Detail: "Your password was set by someone else. Choose your own password to continue.",
		}
	case errors.Is(err, domain.ErrLastActiveUser):
		return Problem{
			Type: problemType("last-active-user"), Title: "The last active user cannot be disabled", Status: http.StatusConflict,
			Detail: "Add or enable another user first, so someone can still sign in.",
		}
	case errors.Is(err, domain.ErrNoAccess):
		return Problem{
			Type: problemType("no-access"), Title: "You do not have access to this", Status: http.StatusForbidden,
			Detail: "Ask an administrator to put you in a group that allows it.",
		}
	case errors.Is(err, domain.ErrForbidden):
		return Problem{
			Type: problemType("forbidden"), Title: "Not allowed", Status: http.StatusForbidden,
			Detail: "You cannot disable yourself, reset your own password here, remove yourself from Administrators, or delete the Administrators group.",
		}
	case errors.Is(err, domain.ErrUnauthenticated):
		return Problem{Type: problemType("unauthenticated"), Title: "Sign in required", Status: http.StatusUnauthorized}
	case errors.Is(err, domain.ErrNotFound):
		return Problem{Type: problemType("not-found"), Title: "Not found", Status: http.StatusNotFound}
	case errors.Is(err, domain.ErrConflict):
		return Problem{
			Type: problemType("conflict"), Title: "Conflict", Status: http.StatusConflict,
			Detail: "It was changed by someone else, already exists, or is still in use. Reload and try again.",
		}
	case errors.Is(err, domain.ErrInvalidTransition):
		return Problem{Type: problemType("invalid-transition"), Title: "Not allowed in the current state", Status: http.StatusConflict}
	case errors.Is(err, domain.ErrCurrencyMismatch):
		return Problem{
			Type: problemType("currency-mismatch"), Title: "Currency mismatch", Status: http.StatusUnprocessableEntity,
			Detail: "The bank account must be in the customer's currency.",
		}
	case errors.Is(err, domain.ErrNoPriceForCurrency):
		return Problem{
			Type: problemType("no-price"), Title: "No price in this currency", Status: http.StatusUnprocessableEntity,
			Detail: "Add a price for the customer's currency to the product, or enter a unit price.",
		}
	case errors.Is(err, domain.ErrNoBankAccountForCurrency):
		return Problem{
			Type: problemType("no-bank-account"), Title: "No bank account for this currency", Status: http.StatusUnprocessableEntity,
			Detail: "Add an active default bank account for the invoice currency.",
		}
	case errors.Is(err, domain.ErrInactive):
		return Problem{
			Type: problemType("inactive"), Title: "Inactive", Status: http.StatusUnprocessableEntity,
			Detail: "A referenced customer, currency or bank account is inactive.",
		}
	default:
		return Problem{Type: problemType("internal"), Title: "Something went wrong", Status: http.StatusInternalServerError}
	}
}

func problemType(code string) string { return "https://cactusinv/problems/" + code }

func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// responseErrorHandler renders handler errors as problems and logs unexpected ones.
func responseErrorHandler(log *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if att := attemptFrom(r.Context()); att != nil && wrongPassword(r, err) {
			att.failed.Store(true) // counted by the password rate limit
		}
		p := problemFor(err)
		if p.Status >= http.StatusInternalServerError {
			log.ErrorContext(r.Context(), "request failed", "request_id", requestID(r.Context()), "err", err)
		}
		writeProblem(w, p)
	}
}

// requestErrorHandler renders undecodable requests (bad JSON, bad parameters) as 400s.
func requestErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) { // a body over the limit is 413, as with an Idempotency-Key
		writeProblem(w, problemFor(tooLarge))
		return
	}
	writeProblem(w, Problem{Type: problemType("bad-request"), Title: "Malformed request", Status: http.StatusBadRequest, Detail: err.Error()})
}
