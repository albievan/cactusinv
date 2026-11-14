package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// GetProfile returns the user's own details (Your account, M43).
func (s *Service) GetProfile(ctx context.Context, userID string) (domain.User, error) {
	u, err := s.tx.Repos().Users.Get(ctx, userID)
	return u, wrap("app.GetProfile", err)
}

// UpdateProfile saves the user's first and last name and e-mail address (Your account,
// M43). The login (user name) is not changed here.
func (s *Service) UpdateProfile(ctx context.Context, userID string, p domain.Profile) (domain.User, error) {
	if err := p.Validate(); err != nil {
		return domain.User{}, wrap("app.UpdateProfile", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		u, err := r.Users.Lock(ctx, userID)
		if err != nil {
			return err
		}
		if err := r.Users.UpdateProfile(ctx, userID, p); err != nil {
			return err
		}
		return s.audit(ctx, r, "user.profile_updated", "user", userID, u.Email+" updated their name and e-mail address")
	})
	if err != nil {
		return domain.User{}, wrap("app.UpdateProfile", err)
	}
	return s.GetProfile(ctx, userID)
}

// RequestPasswordReset e-mails a single-use link to choose a new password (M43), valid for
// domain.PasswordResetTTL, to the user with that login — only when the user exists, is
// active, has a password and an address to send it to (domain.User.ResetAddress), and no
// link was sent in the last domain.PasswordResetInterval. It answers the same whatever the
// outcome, so it tells nobody which logins exist. linkBase is the address the request came
// from (Settings → Quotes' public address wins).
func (s *Service) RequestPasswordReset(ctx context.Context, login, linkBase string) error {
	login = strings.TrimSpace(login)
	if login == "" || len(login) > 254 {
		return wrap("app.RequestPasswordReset", &domain.ValidationError{Field: "userName", Reason: "enter your user name"})
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		qs, err := s.quoteSettings(ctx, r)
		if err != nil {
			return err
		}
		base := qs.PublicURL
		if base == "" {
			if base, err = domain.ParsePublicURL(linkBase); err != nil {
				return err
			}
		}
		u, err := r.Users.GetByEmail(ctx, login)
		if errors.Is(err, domain.ErrNotFound) {
			return nil // the same answer as for a real user
		}
		if err != nil {
			return err
		}
		u, err = r.Users.Lock(ctx, u.ID)
		if err != nil {
			return err
		}
		actor := WithActor(ctx, Actor{ID: u.ID, Name: u.Email})
		to := u.ResetAddress()
		switch {
		case !u.Active() || u.PasswordHash == "":
			return nil
		case to == "" || base == "":
			return s.audit(actor, r, "auth.password_reset_refused", "user", u.ID, "a password reset was asked for "+u.Email+", who has no e-mail address to send it to")
		}
		last, err := r.Users.LastPasswordReset(ctx, u.ID)
		if err != nil {
			return err
		}
		if !last.IsZero() && s.now().Sub(last) < domain.PasswordResetInterval {
			return nil // one e-mail at a time
		}
		token, err := newToken()
		if err != nil {
			return err
		}
		now := s.now()
		if err := r.Users.AddPasswordReset(ctx, domain.PasswordReset{TokenHash: HashToken(token), UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(domain.PasswordResetTTL)}); err != nil {
			return err
		}
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		m, err := domain.NewEmailMessage(s.ids.NewID(), "", []string{to}, nil, domain.PasswordResetSubject(b.Name),
			domain.PasswordResetBody(u, b.Name, domain.PasswordResetLink(base, token)), time.Time{}, now, "")
		if err != nil {
			return err
		}
		m.Kind = domain.EmailKindPasswordReset
		if err := r.Emails.Create(ctx, m); err != nil {
			return err
		}
		return s.audit(actor, r, "auth.password_reset_requested", "user", u.ID, "a password reset link for "+u.Email+" was e-mailed to "+to)
	})
	return wrap("app.RequestPasswordReset", err)
}

// errResetLink is the answer for a reset link that does not work: unknown, used, expired
// or replaced; never which.
var errResetLink = &domain.ValidationError{Field: "token", Reason: "this link does not work any more: ask for a new one on the sign-in page"}

// CheckPasswordReset reports whether a reset link still works (the page asks before the
// user types a password).
func (s *Service) CheckPasswordReset(ctx context.Context, token string) error {
	if len(token) != 43 {
		return wrap("app.CheckPasswordReset", errResetLink)
	}
	p, err := s.tx.Repos().Users.GetPasswordReset(ctx, HashToken(token))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !p.Usable(s.now())) {
		return wrap("app.CheckPasswordReset", errResetLink)
	}
	return wrap("app.CheckPasswordReset", err)
}

// ResetPassword sets a new password through a reset link (M43): the link is used up (with
// every other link of the user), every session of the user ends, and the user signs in
// with the new password. It is the user's own choice, so it need not be changed again.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if err := domain.ValidatePassword("newPassword", password); err != nil {
		return wrap("app.ResetPassword", err)
	}
	if len(token) != 43 {
		return wrap("app.ResetPassword", errResetLink)
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return wrap("app.ResetPassword", err)
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		p, err := r.Users.LockPasswordReset(ctx, HashToken(token))
		if errors.Is(err, domain.ErrNotFound) || (err == nil && !p.Usable(s.now())) {
			return errResetLink
		}
		if err != nil {
			return err
		}
		u, err := r.Users.Lock(ctx, p.UserID)
		if err != nil {
			return err
		}
		if !u.Active() {
			return errResetLink
		}
		if err := r.Users.UpdatePassword(ctx, u.ID, hash, false); err != nil {
			return err
		}
		if err := r.Users.UsePasswordResets(ctx, u.ID, s.now()); err != nil {
			return err
		}
		if err := r.Sessions.DeleteForUser(ctx, u.ID, ""); err != nil {
			return err
		}
		return s.audit(WithActor(ctx, Actor{ID: u.ID, Name: u.Email}), r, "user.password_reset", "user", u.ID, u.Email+" chose a new password through an e-mailed reset link; every session signed out")
	})
	return wrap("app.ResetPassword", err)
}

// redactedResetBody replaces a password reset e-mail's body once it is sent, and is what
// the outbox shows of it before: the link is a key to the account.
const redactedResetBody = "(A password reset link. The text is hidden: the link is a key to the account.)"

// RedactedPasswordResetBody is what the outbox shows of a password reset e-mail.
func RedactedPasswordResetBody() string { return redactedResetBody }

// deliverPasswordReset sends a password reset e-mail, then clears the link from the outbox.
func (s *Service) deliverPasswordReset(ctx context.Context, m *domain.EmailMessage) error {
	b, err := s.business(ctx, s.tx.Repos())
	if err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, ports.Mail{MessageID: m.ID, To: m.To, ReplyTo: b.Email, Subject: m.Subject, Body: m.Body}); err != nil {
		return err
	}
	ctx, cancel := outcomeContext(ctx)
	defer cancel()
	if mine, err := s.recordSent(ctx, m.ID); err != nil {
		return &sentButNotRecorded{err}
	} else if !mine {
		return nil
	}
	if err := s.tx.Repos().Emails.RedactBody(ctx, m.ID, redactedResetBody); err != nil {
		return &sentButNotRecorded{err}
	}
	return nil
}
