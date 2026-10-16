package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateNewLogin(t *testing.T) {
	long := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 60) + ".example"
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"plain", "albie@example.com", "albie@example.com", true},
		{"trimmed", "  albie@example.com\t", "albie@example.com", true},
		{"mixed case kept", "Albie@Example.com", "Albie@Example.com", true},
		{"empty", "", "", false},
		{"spaces only", "   ", "", false},
		{"bare login", "admin", "", false},
		{"two addresses", "a@example.com, b@example.com", "", false},
		{"semicolon", "a@example.com;", "", false},
		{"display name", "Albie <albie@example.com>", "", false},
		{"inner space", "al bie@example.com", "", false},
		{"no domain", "albie@", "", false},
		{"too long", long, "", false},
		{"newline", "albie@example.com\nx", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateNewLogin(tt.in)
			if tt.ok != (err == nil) || got != tt.want {
				t.Fatalf("ValidateNewLogin(%q) = %q, %v", tt.in, got, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("error %v is not a validation error", err)
			}
		})
	}
	if len(long) <= 254 {
		t.Fatalf("test address is only %d bytes", len(long))
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"empty", "", false},
		{"eleven", strings.Repeat("x", 11), false},
		{"twelve", strings.Repeat("x", 12), true},
		{"twelve multibyte", strings.Repeat("é", 12), true},
		{"eleven multibyte", strings.Repeat("é", 11), false},
		{"two hundred", strings.Repeat("x", 200), true},
		{"too long", strings.Repeat("x", 201), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword("newPassword", tt.pw)
			if tt.ok != (err == nil) {
				t.Fatalf("ValidatePassword(len %d) = %v", len(tt.pw), err)
			}
			var ve *ValidationError
			if err != nil && (!errors.As(err, &ve) || ve.Field != "newPassword") {
				t.Fatalf("error %v does not name the field", err)
			}
		})
	}
}

func TestCheckCanDisable(t *testing.T) {
	disabled := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	a := User{ID: "a"}
	b := User{ID: "b"}
	bOff := User{ID: "b", DisabledAt: disabled}
	tests := []struct {
		name   string
		actor  string
		target User
		active []string
		want   error
	}{
		{"another active user", "a", b, []string{"a", "b"}, nil},
		{"yourself", "a", a, []string{"a", "b"}, ErrForbidden},
		{"yourself even when last", "a", a, []string{"a"}, ErrForbidden},
		{"the last active user", "a", b, []string{"b"}, ErrLastActiveUser},
		{"no active user left (raced)", "a", b, nil, ErrLastActiveUser},
		{"one of three", "a", b, []string{"a", "b", "c"}, nil},
		{"already disabled", "a", bOff, []string{"a"}, nil},
		{"system actor", "system:operator", b, []string{"a", "b"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckCanDisable(tt.actor, tt.target, tt.active); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("CheckCanDisable = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestUserActive(t *testing.T) {
	if !(User{}).Active() {
		t.Fatal("a user without DisabledAt is active")
	}
	if (User{DisabledAt: time.Unix(1, 0)}).Active() {
		t.Fatal("a disabled user is not active")
	}
}
