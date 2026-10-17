package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/albievan/cactusinv/internal/domain"
)

// TestUserProblems: the user-management refusals (ADR-011) map to their own problem types.
func TestUserProblems(t *testing.T) {
	tests := []struct {
		err    error
		code   string
		status int
	}{
		{domain.ErrPasswordChangeRequired, "password-change-required", http.StatusForbidden},
		{domain.ErrLastActiveUser, "last-active-user", http.StatusConflict},
		{domain.ErrForbidden, "forbidden", http.StatusForbidden},
	}
	for _, tt := range tests {
		p := problemFor(fmt.Errorf("app.DisableUser: %w", tt.err))
		if p.Type != problemType(tt.code) || p.Status != tt.status || p.Detail == "" {
			t.Errorf("%v → %+v", tt.err, p)
		}
	}
}

func TestReauthPath(t *testing.T) {
	tests := []struct {
		method, path string
		want         bool
	}{
		{"POST", "/api/v1/auth/login", true},
		{"POST", "/api/v1/auth/password", true},
		{"POST", "/api/v1/users", true},
		{"GET", "/api/v1/users", false},
		{"POST", "/api/v1/users/u1/reset-password", true},
		{"POST", "/api/v1/users/u1/disable", true},
		{"POST", "/api/v1/users/u1/enable", true},
		{"POST", "/api/v1/restores/r1/apply", true},
		{"POST", "/api/v1/restores", false},
		{"POST", "/api/v1/currencies", false},
		{"POST", "/api/v1/auth/logout", false},
		{"PUT", "/api/v1/groups/g1", true},
		{"PUT", "/api/v1/groups/x%2Fy", true}, // an escaped slash is still the update route
		{"PUT", "/api/v1/users/u1/groups", true},
	}
	for _, tt := range tests {
		r, _ := http.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
		if got := reauthPath(r); got != tt.want {
			t.Errorf("%s %s = %v", tt.method, tt.path, got)
		}
		if tt.want && !skipsIdempotency(r) {
			t.Errorf("%s %s carries a password and must not be fingerprinted", tt.method, tt.path)
		}
	}
}
