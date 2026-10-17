package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// dummyHash equalises login timing for unknown e-mail addresses.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$Zm9vYmFyZm9vYmFyZm9vYmFyZm9vYmFyZm9vYmFy"

// Login checks credentials and starts a session. It returns the session token for the
// cookie (only its hash is stored) and its expiry. Every failure is the same
// ErrUnauthenticated; the reason (unknown login, wrong password, disabled user) is only in
// the audit trail (ADR-011).
func (s *Service) Login(ctx context.Context, email, password string) (string, time.Time, error) {
	r := s.tx.Repos()
	login := strings.TrimSpace(email)
	failed := func(userID, reason string) error {
		attempt := WithActor(ctx, Actor{ID: "", Name: truncateRunes(login, 100)})
		_ = s.auditNow(attempt, "auth.login_failed", "user", userID, "failed sign-in for "+truncateRunes(login, 100)+": "+reason)
		return wrap("app.Login", domain.ErrUnauthenticated)
	}
	u, err := r.Users.GetByEmail(ctx, login)
	if errors.Is(err, domain.ErrNotFound) {
		_, _ = s.hasher.Verify(dummyHash, password)
		return "", time.Time{}, failed("", "unknown login")
	}
	if err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	// The password is checked first, so a disabled account answers exactly like a wrong
	// password, in the same time.
	ok, err := s.hasher.Verify(u.PasswordHash, password)
	if err != nil || !ok {
		return "", time.Time{}, failed(u.ID, "wrong password")
	}
	if !u.Active() {
		return "", time.Time{}, failed(u.ID, "the user is disabled")
	}
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	now := s.now()
	sys, err := s.systemSettings(ctx, s.tx.Repos())
	if err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	sess := domain.Session{TokenHash: HashToken(token), UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(sys.SessionMax)}
	err = s.tx.WithinTx(WithActor(ctx, Actor{ID: u.ID, Name: u.Email}), func(ctx context.Context, r ports.Repos) error {
		// The password was checked outside the transaction. Lock the user and make sure it
		// still holds: a password change, reset or disable that committed meanwhile has
		// already ended the user's sessions, and this one must not outlive it (ADR-011).
		cur, err := r.Users.Lock(ctx, u.ID)
		if err != nil {
			return err
		}
		if !cur.Active() || cur.PasswordHash != u.PasswordHash {
			return errLoginRaced
		}
		if err := r.Sessions.Create(ctx, sess); err != nil {
			return err
		}
		if err := r.Users.TouchLogin(ctx, u.ID, now); err != nil {
			return err
		}
		return s.audit(ctx, r, "auth.login", "user", u.ID, "signed in")
	})
	if errors.Is(err, errLoginRaced) {
		return "", time.Time{}, failed(u.ID, "the password was changed or the user disabled during the sign-in")
	}
	if err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	return token, sess.ExpiresAt, nil
}

// sessionTouchEvery is how often a session's last use is written (an idle timeout is at
// least 5 minutes, so a minute's lag never matters).
const sessionTouchEvery = time.Minute

// errLoginRaced means the user's password or state changed between checking the password
// and starting the session.
var errLoginRaced = errors.New("sign-in raced a change to the user")

// Logout ends a session.
func (s *Service) Logout(ctx context.Context, token string) error {
	if err := s.tx.Repos().Sessions.Delete(ctx, HashToken(token)); err != nil {
		return wrap("app.Logout", err)
	}
	return wrap("app.Logout", s.auditNow(ctx, "auth.logout", "user", ActorFrom(ctx).ID, "signed out"))
}

// Authenticate resolves a session token to its user. A disabled user's session is refused.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, domain.ErrUnauthenticated
	}
	r := s.tx.Repos()
	hash := HashToken(token)
	sess, err := r.Sessions.Get(ctx, hash)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return domain.User{}, wrap("app.Authenticate", err)
	}
	// The timeouts in System Settings apply to every session, also those started before
	// they were changed (ADR-011 amendment, M23).
	sys, err := s.systemSettings(ctx, r)
	if err != nil {
		return domain.User{}, wrap("app.Authenticate", err)
	}
	now := s.now()
	if !sys.SessionAlive(sess, now) {
		_ = r.Sessions.Delete(ctx, hash)
		return domain.User{}, domain.ErrUnauthenticated
	}
	// Record use, at most once a minute per session, also without an idle timeout: one set
	// later must not sign out everyone who was active.
	if now.Sub(sess.LastSeenAt) >= sessionTouchEvery {
		if err := r.Sessions.Touch(ctx, hash, now); err != nil {
			return domain.User{}, wrap("app.Authenticate", err)
		}
	}
	u, err := r.Users.Get(ctx, sess.UserID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !u.Active()) {
		return domain.User{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return domain.User{}, wrap("app.Authenticate", err)
	}
	// Access is read on every request, so a group change applies at once (ADR-012).
	groups, err := r.Groups.ForUser(ctx, u.ID)
	if err != nil {
		return domain.User{}, wrap("app.Authenticate", err)
	}
	u.Access = domain.AccessFor(groups)
	for _, g := range groups {
		u.GroupIDs = append(u.GroupIDs, g.ID)
	}
	return u, nil
}

// CreateUser adds an administrator (a member of Administrators) from the server's command line (`cactusinv admin
// create`). Shell access is the authority, so the password is not flagged must-change.
func (s *Service) CreateUser(ctx context.Context, email, password string) (domain.User, error) {
	login, err := domain.ValidateNewLogin(email)
	if err != nil {
		return domain.User{}, wrap("app.CreateUser", err)
	}
	if err := domain.ValidatePassword("password", password); err != nil {
		return domain.User{}, wrap("app.CreateUser", err)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return domain.User{}, wrap("app.CreateUser", err)
	}
	u := domain.User{ID: s.ids.NewID(), Email: login, PasswordHash: hash, CreatedAt: s.now()}
	return u, wrap("app.CreateUser", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Users.Create(ctx, u); err != nil {
			return err
		}
		if err := r.Groups.SetUserGroups(ctx, u.ID, []string{domain.AdministratorsGroupID}); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.created", "user", u.ID, u.Email+"; in Administrators")
	}))
}

// EnsureDefaultAdmin creates the default administrator when no user exists yet, so a
// fresh installation can sign in. It reports whether it created one. The default
// credentials are deliberately weak, so the user must change the password at the first
// sign-in (ADR-011).
func (s *Service) EnsureDefaultAdmin(ctx context.Context, login, password string) (bool, error) {
	r := s.tx.Repos()
	n, err := r.Users.Count(ctx)
	if err != nil || n > 0 {
		return false, wrap("app.EnsureDefaultAdmin", err)
	}
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return false, wrap("app.EnsureDefaultAdmin", &domain.ValidationError{Field: "login", Reason: "login and password are required"})
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return false, wrap("app.EnsureDefaultAdmin", err)
	}
	u := domain.User{ID: s.ids.NewID(), Email: login, PasswordHash: hash, CreatedAt: s.now(), MustChangePassword: true}
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Users.Create(ctx, u); err != nil {
			return err
		}
		if err := r.Groups.SetUserGroups(ctx, u.ID, []string{domain.AdministratorsGroupID}); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.created", "user", u.ID, "default administrator "+u.Email+"; must change the password at the first sign-in")
	})
	if errors.Is(err, domain.ErrConflict) { // another replica won the race
		return false, nil
	}
	return err == nil, wrap("app.EnsureDefaultAdmin", err)
}

// ChangePassword lets a signed-in user replace their password after proving the current
// one. It clears the must-change flag and signs the user out of every other session; the
// session whose token is keepToken stays signed in ("" keeps none).
func (s *Service) ChangePassword(ctx context.Context, userID, keepToken, current, next string) error {
	if err := domain.ValidatePassword("newPassword", next); err != nil {
		return wrap("app.ChangePassword", err)
	}
	r := s.tx.Repos()
	u, err := r.Users.Get(ctx, userID)
	if err != nil {
		return wrap("app.ChangePassword", err)
	}
	if ok, err := s.hasher.Verify(u.PasswordHash, current); err != nil || !ok {
		_ = s.auditNow(ctx, "user.reauth_failed", "user", u.ID, "wrong current password when changing the password of "+u.Email)
		return wrap("app.ChangePassword", &domain.ValidationError{Field: "currentPassword", Reason: "is not correct"})
	}
	if same, err := s.hasher.Verify(u.PasswordHash, next); err == nil && same {
		return wrap("app.ChangePassword", &domain.ValidationError{Field: "newPassword", Reason: "must be different from the current password"})
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return wrap("app.ChangePassword", err)
	}
	keep := ""
	if keepToken != "" {
		keep = HashToken(keepToken)
	}
	return wrap("app.ChangePassword", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Users.UpdatePassword(ctx, u.ID, hash, false); err != nil {
			return err
		}
		if err := r.Sessions.DeleteForUser(ctx, u.ID, keep); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.password_changed", "user", u.ID, u.Email+" changed their password; other sessions signed out")
	}))
}

// SetPassword replaces a user's password from the server's command line (`cactusinv admin
// set-password`), the way back in when every password is forgotten (ADR-011). Shell
// access is the authority, so the password is not flagged must-change; the user's
// sessions are ended.
func (s *Service) SetPassword(ctx context.Context, email, password string) error {
	if err := domain.ValidatePassword("password", password); err != nil {
		return wrap("app.SetPassword", err)
	}
	r := s.tx.Repos()
	u, err := r.Users.GetByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return wrap("app.SetPassword", err)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return wrap("app.SetPassword", err)
	}
	return wrap("app.SetPassword", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Users.UpdatePassword(ctx, u.ID, hash, false); err != nil {
			return err
		}
		if err := r.Sessions.DeleteForUser(ctx, u.ID, ""); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.password_reset", "user", u.ID, "password reset on the server for "+u.Email)
	}))
}

// Housekeeping deletes expired sessions, idempotency records older than a day and an
// expired staged restore.
func (s *Service) Housekeeping(ctx context.Context) error {
	r := s.tx.Repos()
	if err := r.Sessions.DeleteExpired(ctx, s.now()); err != nil {
		return wrap("app.Housekeeping", err)
	}
	if err := r.Idempotency.DeleteBefore(ctx, s.now().Add(-24*time.Hour)); err != nil {
		return wrap("app.Housekeeping", err)
	}
	return wrap("app.Housekeeping", s.discardExpiredRestore(ctx))
}

// HashToken is the stored form of a session token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Idempotency exposes the idempotency store to the HTTP adapter's middleware.
func (s *Service) Idempotency() ports.IdempotencyRepository { return s.tx.Repos().Idempotency }

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
