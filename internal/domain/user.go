package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Password length limits, in characters (ADR-011).
const (
	MinPasswordLength = 12
	MaxPasswordLength = 200
)

// User is someone who can sign in (ADR-011). What they may do comes from their access
// groups (ADR-012).
// Email is the login: an e-mail address for every user added in the app; logins made
// before v1.6.0 (the default "admin") need not be one.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	// DisabledAt is when the user was disabled; zero while the user is active.
	DisabledAt time.Time
	// MustChangePassword is set when someone else chose the password (an administrator
	// adding or resetting the user, or the default administrator): until it is changed,
	// the user's sessions reach only their own account and password change.
	MustChangePassword bool
	// LastLoginAt is the last successful sign-in; zero if never.
	LastLoginAt time.Time
	// GroupIDs are the access groups the user is in, sorted (ADR-012).
	GroupIDs []string
	// Access is what the user may do, computed from their groups when a session is
	// authenticated; it is not stored.
	Access Access
}

// Active reports whether the user may sign in.
func (u User) Active() bool { return u.DisabledAt.IsZero() }

// Session is a signed-in browser session; only a hash of its token is stored.
type Session struct {
	TokenHash string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// ValidateNewLogin checks the login of a user being added and returns it trimmed: one
// valid e-mail address of at most 254 bytes. Existing logins are not re-validated.
func ValidateNewLogin(login string) (string, error) {
	login = strings.TrimSpace(login)
	if login == "" || strings.ContainsAny(login, ",;") {
		return "", invalid("email", "must be a valid e-mail address")
	}
	if err := validEmail("email", login); err != nil {
		return "", err
	}
	return login, nil
}

// ValidatePassword checks a new password's length (12–200 characters). field names the
// input that carried it.
func ValidatePassword(field, password string) error {
	switch n := utf8.RuneCountInString(password); {
	case n < MinPasswordLength:
		return invalid(field, "must be at least 12 characters")
	case n > MaxPasswordLength:
		return invalid(field, "must be at most 200 characters")
	}
	return nil
}

// CheckCanDisable decides whether actorID may disable target, given the ids of the users
// that are active right now (read under a lock, so the answer cannot go stale before the
// change commits). Nobody disables themselves (ErrForbidden), and the last active user is
// never disabled (ErrLastActiveUser), so an installation can never lock itself out.
// Disabling a user who is already disabled is allowed (it changes nothing).
func CheckCanDisable(actorID string, target User, activeIDs []string) error {
	if target.ID == actorID {
		return ErrForbidden
	}
	if !target.Active() {
		return nil
	}
	for _, id := range activeIDs {
		if id != target.ID {
			return nil
		}
	}
	return ErrLastActiveUser
}
