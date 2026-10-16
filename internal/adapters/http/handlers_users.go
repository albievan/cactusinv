package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// ---- users (ADR-011) ----

func toAPIUser(u domain.User) User {
	out := User{Id: u.ID, Email: u.Email, CreatedAt: u.CreatedAt, Active: u.Active(), MustChangePassword: u.MustChangePassword}
	if !u.DisabledAt.IsZero() {
		t := u.DisabledAt
		out.DisabledAt = &t
	}
	if !u.LastLoginAt.IsZero() {
		t := u.LastLoginAt
		out.LastLoginAt = &t
	}
	return out
}

func (h *handlers) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	us, err := h.svc.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, len(us))
	for i, u := range us {
		out[i] = toAPIUser(u)
	}
	return ListUsers200JSONResponse{Items: out}, nil
}

func (h *handlers) AddUser(ctx context.Context, req AddUserRequestObject) (AddUserResponseObject, error) {
	u, err := h.svc.AddUser(ctx, req.Body.Email, req.Body.Password, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return AddUser201JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) ResetUserPassword(ctx context.Context, req ResetUserPasswordRequestObject) (ResetUserPasswordResponseObject, error) {
	u, err := h.svc.ResetUserPassword(ctx, req.Id, req.Body.Password, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return ResetUserPassword200JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) DisableUser(ctx context.Context, req DisableUserRequestObject) (DisableUserResponseObject, error) {
	u, err := h.svc.DisableUser(ctx, req.Id, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return DisableUser200JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) EnableUser(ctx context.Context, req EnableUserRequestObject) (EnableUserResponseObject, error) {
	u, err := h.svc.EnableUser(ctx, req.Id, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return EnableUser200JSONResponse(toAPIUser(u)), nil
}

// sessionGate refuses every operation a signed-in user's session may not run yet — while
// the user must change their password, everything but me, changePassword and logout
// (app.SessionAllows). It is a strict-server middleware because only there is the
// operation known; it runs before the handler reads an upload or export body.
func sessionGate(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		if u := currentUser(ctx); u.ID != "" {
			if err := app.SessionAllows(u, operationID); err != nil {
				return nil, err
			}
		}
		return f(ctx, w, r, request)
	}
}

// reauthPaths are the POST routes that check a password and so share the sign-in rate
// limit and are never fingerprinted for Idempotency-Key (an unsalted hash of a password
// could be guessed).
func reauthPath(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	p := r.URL.Path
	if p == "/api/v1/auth/login" || p == "/api/v1/auth/password" || p == "/api/v1/users" {
		return true
	}
	if rest, ok := strings.CutPrefix(p, "/api/v1/users/"); ok {
		return strings.HasSuffix(rest, "/reset-password") || strings.HasSuffix(rest, "/disable") || strings.HasSuffix(rest, "/enable")
	}
	return strings.HasPrefix(p, "/api/v1/restores/") && strings.HasSuffix(p, "/apply")
}
