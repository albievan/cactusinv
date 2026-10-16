package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ListUsers returns every user, disabled ones included, ordered by login.
func (s *Service) ListUsers(ctx context.Context) ([]domain.User, error) {
	us, err := s.tx.Repos().Users.List(ctx)
	return us, wrap("app.ListUsers", err)
}

// AddUser adds an administrator with a password the actor chose; the new user must change
// it at the first sign-in. The actor proves who they are with currentPassword (ADR-011).
func (s *Service) AddUser(ctx context.Context, login, password, currentPassword string) (domain.User, error) {
	login, err := domain.ValidateNewLogin(login)
	if err != nil {
		return domain.User{}, wrap("app.AddUser", err)
	}
	if err := domain.ValidatePassword("password", password); err != nil {
		return domain.User{}, wrap("app.AddUser", err)
	}
	if err := s.reauthUser(ctx, "adding "+login, currentPassword); err != nil {
		return domain.User{}, wrap("app.AddUser", err)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return domain.User{}, wrap("app.AddUser", err)
	}
	u := domain.User{ID: s.ids.NewID(), Email: login, PasswordHash: hash, CreatedAt: s.now(), MustChangePassword: true}
	return u, wrap("app.AddUser", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := lockActor(ctx, r); err != nil {
			return err
		}
		if err := r.Users.Create(ctx, u); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.created", "user", u.ID, u.Email+"; must change the password at the first sign-in")
	}))
}

// ResetUserPassword gives another user a password the actor chose: the user must change
// it at the next sign-in, and every session of theirs ends. Your own password is changed
// with ChangePassword instead (ErrForbidden here).
func (s *Service) ResetUserPassword(ctx context.Context, userID, password, currentPassword string) (domain.User, error) {
	if userID == ActorFrom(ctx).ID {
		return domain.User{}, wrap("app.ResetUserPassword", domain.ErrForbidden)
	}
	if err := domain.ValidatePassword("password", password); err != nil {
		return domain.User{}, wrap("app.ResetUserPassword", err)
	}
	u, err := s.tx.Repos().Users.Get(ctx, userID)
	if err != nil {
		return domain.User{}, wrap("app.ResetUserPassword", err)
	}
	if err := s.reauthUser(ctx, "resetting the password of "+u.Email, currentPassword); err != nil {
		return domain.User{}, wrap("app.ResetUserPassword", err)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return domain.User{}, wrap("app.ResetUserPassword", err)
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := lockActor(ctx, r); err != nil {
			return err
		}
		if err := r.Users.UpdatePassword(ctx, u.ID, hash, true); err != nil {
			return err
		}
		if err := r.Sessions.DeleteForUser(ctx, u.ID, ""); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.password_reset", "user", u.ID, "password reset for "+u.Email+"; must be changed at the next sign-in; signed out everywhere")
	})
	if err != nil {
		return domain.User{}, wrap("app.ResetUserPassword", err)
	}
	u.PasswordHash, u.MustChangePassword = hash, true
	return u, nil
}

// DisableUser stops a user from signing in and ends their sessions. Nobody disables
// themselves, and the last active user is never disabled: the check runs on the active
// users locked inside the transaction, so concurrent requests cannot both pass it.
// Disabling a disabled user changes nothing.
func (s *Service) DisableUser(ctx context.Context, userID, currentPassword string) (domain.User, error) {
	actor := ActorFrom(ctx)
	if userID == actor.ID {
		return domain.User{}, wrap("app.DisableUser", domain.ErrForbidden)
	}
	u, err := s.tx.Repos().Users.Get(ctx, userID)
	if err != nil {
		return domain.User{}, wrap("app.DisableUser", err)
	}
	if err := s.reauthUser(ctx, "disabling "+u.Email, currentPassword); err != nil {
		return domain.User{}, wrap("app.DisableUser", err)
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		active, err := r.Users.LockActive(ctx)
		if err != nil {
			return err
		}
		if u, err = r.Users.Get(ctx, userID); err != nil {
			return err
		}
		if err := domain.CheckCanDisable(actor.ID, u, active); err != nil {
			return err
		}
		if !slices.Contains(active, actor.ID) { // disabled meanwhile
			return domain.ErrUnauthenticated
		}
		if !u.Active() {
			return nil
		}
		u.DisabledAt = s.now()
		if err := r.Users.SetDisabled(ctx, u.ID, u.DisabledAt); err != nil {
			return err
		}
		if err := r.Sessions.DeleteForUser(ctx, u.ID, ""); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.disabled", "user", u.ID, u.Email+" disabled; signed out everywhere")
	})
	return u, wrap("app.DisableUser", err)
}

// EnableUser lets a disabled user sign in again with their existing password. Enabling an
// active user changes nothing. Like disabling, it needs the actor's current password.
func (s *Service) EnableUser(ctx context.Context, userID, currentPassword string) (domain.User, error) {
	u, err := s.tx.Repos().Users.Get(ctx, userID)
	if err != nil {
		return domain.User{}, wrap("app.EnableUser", err)
	}
	if err := s.reauthUser(ctx, "enabling "+u.Email, currentPassword); err != nil {
		return domain.User{}, wrap("app.EnableUser", err)
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := lockActor(ctx, r); err != nil {
			return err
		}
		if u, err = r.Users.Lock(ctx, userID); err != nil || u.Active() {
			return err
		}
		u.DisabledAt = time.Time{}
		if err := r.Users.SetDisabled(ctx, u.ID, time.Time{}); err != nil {
			return err
		}
		// No session of a disabled user may survive into the enabled one.
		if err := r.Sessions.DeleteForUser(ctx, u.ID, ""); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.enabled", "user", u.ID, u.Email+" enabled")
	})
	return u, wrap("app.EnableUser", err)
}

// lockActor locks the acting user's row and refuses an actor who was disabled (or
// removed by a restore) after re-authenticating: the change and the actor's state commit
// together.
func lockActor(ctx context.Context, r ports.Repos) error {
	a, err := r.Users.Lock(ctx, ActorFrom(ctx).ID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !a.Active()) {
		return domain.ErrUnauthenticated
	}
	return err
}

// reauthUser checks the actor's current password before a user-management action and
// records a wrong one as user.reauth_failed (its own transaction: the refusal is kept).
func (s *Service) reauthUser(ctx context.Context, what, password string) error {
	err := s.reauthenticate(ctx, password)
	if errors.Is(err, domain.ErrValidation) {
		a := ActorFrom(ctx)
		_ = s.auditNow(ctx, "user.reauth_failed", "user", a.ID, "wrong current password when "+what)
	}
	return err
}
