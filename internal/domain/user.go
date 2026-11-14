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
	// FirstName, LastName and EmailAddress are the user's own details (Your account, M43).
	// EmailAddress is where a password reset link goes; empty = the login, when that is an
	// e-mail address.
	FirstName    string
	LastName     string
	EmailAddress string
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
	TokenHash  string
	UserID     string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time // zero: not used since it started
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

// Profile is what a user may change about themselves (Your account, M43).
type Profile struct {
	FirstName    string
	LastName     string
	EmailAddress string
}

// Validate trims the profile and checks it: names of at most 100 characters on one line,
// and an empty or valid e-mail address.
func (p *Profile) Validate() error {
	p.FirstName, p.LastName, p.EmailAddress = strings.TrimSpace(p.FirstName), strings.TrimSpace(p.LastName), strings.TrimSpace(p.EmailAddress)
	for field, v := range map[string]string{"firstName": p.FirstName, "lastName": p.LastName} {
		if utf8.RuneCountInString(v) > 100 || strings.ContainsAny(v, "\r\n") {
			return invalid(field, "one line, at most 100 characters")
		}
	}
	if p.EmailAddress != "" {
		if strings.ContainsAny(p.EmailAddress, ",;") {
			return invalid("emailAddress", "one e-mail address")
		}
		if err := validEmail("emailAddress", p.EmailAddress); err != nil {
			return err
		}
	}
	return nil
}

// ResetAddress is where u's password reset link goes: their e-mail address, else their
// login when it is one; "" when they have neither (no link can be sent).
func (u User) ResetAddress() string {
	if u.EmailAddress != "" {
		return u.EmailAddress
	}
	if validEmail("email", u.Email) == nil && !strings.ContainsAny(u.Email, ",;") {
		return u.Email
	}
	return ""
}

// PasswordResetTTL is how long a password reset link works; PasswordResetInterval the
// least time between two reset e-mails to one user.
const (
	PasswordResetTTL      = time.Hour
	PasswordResetInterval = 2 * time.Minute
)

// PasswordReset is a forgotten-password link (M43); only a hash of its token is stored.
type PasswordReset struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    time.Time // zero until used (or replaced by a newer reset)
}

// Usable reports whether the link still works at now: not used and not expired.
func (r PasswordReset) Usable(now time.Time) bool {
	return r.UsedAt.IsZero() && now.Before(r.ExpiresAt)
}

// PasswordResetLink is the link a reset e-mail carries: base is the address users reach
// cactusInv on (ParsePublicURL), token the reset's random token.
func PasswordResetLink(base, token string) string {
	return strings.TrimRight(base, "/") + "/reset-password/" + token
}

// PasswordResetSubject is a reset e-mail's subject.
func PasswordResetSubject(businessName string) string {
	if businessName == "" {
		return "Reset your cactusInv password"
	}
	return TruncateUTF8("Reset your cactusInv password for "+strings.NewReplacer("\r", " ", "\n", " ").Replace(businessName), MaxSubjectLength)
}

// PasswordResetBody is a reset e-mail's text: who it is for, the link and how long it works.
func PasswordResetBody(u User, businessName, link string) string {
	name := strings.TrimSpace(u.FirstName)
	if name == "" {
		name = u.Email
	}
	where := "cactusInv"
	if businessName != "" {
		where = "cactusInv for " + businessName
	}
	return "Hello " + name + ",\n\n" +
		"Someone asked to reset the password of the user " + u.Email + " in " + where + ".\n\n" +
		"To choose a new password, open this link within 60 minutes. It works once:\n\n" + link + "\n\n" +
		"If you did not ask for this, ignore this e-mail: your password stays as it is.\n"
}
