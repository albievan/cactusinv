package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/albievan/cactusinv/internal/domain"
)

// problemFor maps an error to an RFC 9457 problem. Only this adapter knows HTTP status
// codes; the domain only knows sentinels.
func problemFor(err error) Problem {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		return Problem{Type: problemType("validation"), Title: "Invalid input", Status: http.StatusUnprocessableEntity, Detail: ve.Error(), Field: ve.Field}
	case errors.Is(err, domain.ErrValidation):
		return Problem{Type: problemType("validation"), Title: "Invalid input", Status: http.StatusUnprocessableEntity, Detail: err.Error()}
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
		p := problemFor(err)
		if p.Status >= http.StatusInternalServerError {
			log.ErrorContext(r.Context(), "request failed", "request_id", requestID(r.Context()), "err", err)
		}
		writeProblem(w, p)
	}
}

// requestErrorHandler renders undecodable requests (bad JSON, bad parameters) as 400s.
func requestErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, Problem{Type: problemType("bad-request"), Title: "Malformed request", Status: http.StatusBadRequest, Detail: err.Error()})
}
