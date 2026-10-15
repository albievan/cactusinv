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

	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
)

// MinPasswordLength is the shortest accepted password.
const MinPasswordLength = 12

// dummyHash equalises login timing for unknown e-mail addresses.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$Zm9vYmFyZm9vYmFyZm9vYmFyZm9vYmFyZm9vYmFy"

// Login checks credentials and starts a session. It returns the session token for the
// cookie (only its hash is stored) and its expiry.
func (s *Service) Login(ctx context.Context, email, password string) (string, time.Time, error) {
	r := s.tx.Repos()
	u, err := r.Users.GetByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, domain.ErrNotFound) {
		_, _ = s.hasher.Verify(dummyHash, password)
		return "", time.Time{}, wrap("app.Login", domain.ErrUnauthenticated)
	}
	if err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	ok, err := s.hasher.Verify(u.PasswordHash, password)
	if err != nil || !ok {
		return "", time.Time{}, wrap("app.Login", domain.ErrUnauthenticated)
	}
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	sess := domain.Session{TokenHash: HashToken(token), UserID: u.ID, CreatedAt: s.now(), ExpiresAt: s.now().Add(s.ttl)}
	if err := r.Sessions.Create(ctx, sess); err != nil {
		return "", time.Time{}, wrap("app.Login", err)
	}
	return token, sess.ExpiresAt, nil
}

// Logout ends a session.
func (s *Service) Logout(ctx context.Context, token string) error {
	return wrap("app.Logout", s.tx.Repos().Sessions.Delete(ctx, HashToken(token)))
}

// Authenticate resolves a session token to its user.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, domain.ErrUnauthenticated
	}
	r := s.tx.Repos()
	sess, err := r.Sessions.Get(ctx, HashToken(token))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !sess.ExpiresAt.After(s.now())) {
		return domain.User{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return domain.User{}, wrap("app.Authenticate", err)
	}
	u, err := r.Users.Get(ctx, sess.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrUnauthenticated
	}
	return u, wrap("app.Authenticate", err)
}

// CreateUser adds an administrator (used by the `cactusinv admin create` command).
func (s *Service) CreateUser(ctx context.Context, email, password string) (domain.User, error) {
	email = strings.TrimSpace(email)
	if _, err := domain.ParseEmailList("email", email); err != nil || email == "" || strings.ContainsAny(email, ",;") {
		return domain.User{}, wrap("app.CreateUser", &domain.ValidationError{Field: "email", Reason: "must be a valid e-mail address"})
	}
	if len(password) < MinPasswordLength {
		return domain.User{}, wrap("app.CreateUser", &domain.ValidationError{Field: "password", Reason: "must be at least 12 characters"})
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return domain.User{}, wrap("app.CreateUser", err)
	}
	u := domain.User{ID: s.ids.NewID(), Email: email, PasswordHash: hash, CreatedAt: s.now()}
	return u, wrap("app.CreateUser", s.tx.Repos().Users.Create(ctx, u))
}

// EnsureDefaultAdmin creates the default administrator when no user exists yet, so a
// fresh installation can sign in. It reports whether it created one. The default
// credentials are deliberately weak and must be changed after the first sign-in.
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
	err = r.Users.Create(ctx, domain.User{ID: s.ids.NewID(), Email: login, PasswordHash: hash, CreatedAt: s.now()})
	if errors.Is(err, domain.ErrConflict) { // another replica won the race
		return false, nil
	}
	return err == nil, wrap("app.EnsureDefaultAdmin", err)
}

// ChangePassword lets a signed-in user replace their password after proving the current one.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next string) error {
	if len(next) < MinPasswordLength {
		return wrap("app.ChangePassword", &domain.ValidationError{Field: "newPassword", Reason: "must be at least 12 characters"})
	}
	r := s.tx.Repos()
	u, err := r.Users.Get(ctx, userID)
	if err != nil {
		return wrap("app.ChangePassword", err)
	}
	if ok, err := s.hasher.Verify(u.PasswordHash, current); err != nil || !ok {
		return wrap("app.ChangePassword", &domain.ValidationError{Field: "currentPassword", Reason: "is not correct"})
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return wrap("app.ChangePassword", err)
	}
	return wrap("app.ChangePassword", r.Users.UpdatePassword(ctx, u.ID, hash))
}

// SetPassword replaces a user's password (CLI reset).
func (s *Service) SetPassword(ctx context.Context, email, password string) error {
	if len(password) < MinPasswordLength {
		return wrap("app.SetPassword", &domain.ValidationError{Field: "password", Reason: "must be at least 12 characters"})
	}
	r := s.tx.Repos()
	u, err := r.Users.GetByEmail(ctx, email)
	if err != nil {
		return wrap("app.SetPassword", err)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return wrap("app.SetPassword", err)
	}
	return wrap("app.SetPassword", r.Users.UpdatePassword(ctx, u.ID, hash))
}

// Housekeeping deletes expired sessions and idempotency records older than a day.
func (s *Service) Housekeeping(ctx context.Context) error {
	r := s.tx.Repos()
	if err := r.Sessions.DeleteExpired(ctx, s.now()); err != nil {
		return wrap("app.Housekeeping", err)
	}
	return wrap("app.Housekeeping", r.Idempotency.DeleteBefore(ctx, s.now().Add(-24*time.Hour)))
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
