package app

import (
	"context"
	"errors"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// SMTPView is the e-mail server configuration as shown to the user (never the password).
type SMTPView struct {
	Settings    domain.SMTPSettings
	PasswordSet bool
	// Warnings are domain.EmailSenderWarnings for the saved settings, enabled or not (the
	// server may be enabled later); never nil.
	Warnings []string
}

// GetSMTPSettings returns the configured mail server, or the Gmail preset (disabled)
// before anything was saved.
func (s *Service) GetSMTPSettings(ctx context.Context) (SMTPView, error) {
	st, enc, err := s.tx.Repos().SMTP.Get(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		preset := domain.GmailSMTP()
		return SMTPView{Settings: preset, Warnings: domain.EmailSenderWarnings(preset)}, nil
	}
	if err != nil {
		return SMTPView{}, wrap("app.GetSMTPSettings", err)
	}
	return SMTPView{Settings: st, PasswordSet: len(enc) > 0, Warnings: domain.EmailSenderWarnings(st)}, nil
}

// UpdateSMTPSettings saves the mail server. password nil keeps the stored one, "" clears
// it, anything else replaces it (encrypted at rest).
func (s *Service) UpdateSMTPSettings(ctx context.Context, st domain.SMTPSettings, password *string) (SMTPView, error) {
	if err := st.Validate(); err != nil {
		return SMTPView{}, wrap("app.UpdateSMTPSettings", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		_, enc, err := r.SMTP.Get(ctx)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if password != nil {
			enc = nil
			if *password != "" {
				if s.cipher == nil {
					return &domain.ValidationError{Field: "password", Reason: "no encryption key (app_key secret) is configured on the server"}
				}
				if enc, err = s.cipher.Encrypt([]byte(*password)); err != nil {
					return err
				}
			}
		}
		if st.Enabled && st.Username != "" && len(enc) == 0 {
			return &domain.ValidationError{Field: "password", Reason: "is required when a username is set (for Gmail: an app password)"}
		}
		if err := r.SMTP.Save(ctx, st, enc); err != nil {
			return err
		}
		summary := st.Host + " as " + st.Username + ", " + activeText(st.Enabled)
		if password != nil {
			summary += ", password changed"
		}
		return s.audit(ctx, r, "settings.email_server_updated", "settings", "email", summary)
	})
	if err != nil {
		return SMTPView{}, wrap("app.UpdateSMTPSettings", err)
	}
	return s.GetSMTPSettings(ctx)
}

// SMTPServer returns the in-app mail server with its decrypted password; ok is false
// when none is enabled (the deployment's default server is used instead).
func (s *Service) SMTPServer(ctx context.Context) (domain.SMTPSettings, string, bool, error) {
	st, enc, err := s.tx.Repos().SMTP.Get(ctx)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !st.Enabled) {
		return domain.SMTPSettings{}, "", false, nil
	}
	if err != nil {
		return domain.SMTPSettings{}, "", false, wrap("app.SMTPServer", err)
	}
	if len(enc) == 0 {
		return st, "", true, nil
	}
	if s.cipher == nil {
		return domain.SMTPSettings{}, "", false, wrap("app.SMTPServer", errors.New("SMTP password stored but no app_key configured"))
	}
	pw, err := s.cipher.Decrypt(enc)
	if err != nil {
		return domain.SMTPSettings{}, "", false, wrap("app.SMTPServer", err)
	}
	return st, string(pw), true, nil
}

// SendTestEmail sends a test message straight away (not through the outbox) so the user
// sees immediately whether the mail server works. It returns the sender warnings
// (domain.EmailSenderWarnings) of the server that sent it: the in-app server's when it is
// enabled, none when the deployment's default server sent it; never nil. GetSMTPSettings,
// by contrast, warns for the saved settings even while they are disabled.
func (s *Service) SendTestEmail(ctx context.Context, to string) ([]string, error) {
	to = strings.TrimSpace(to)
	if _, err := domain.ParseEmailList("to", to); err != nil || to == "" || strings.ContainsAny(to, ",;") {
		return nil, wrap("app.SendTestEmail", &domain.ValidationError{Field: "to", Reason: "must be one e-mail address"})
	}
	r := s.tx.Repos()
	warnings := []string{}
	st, _, err := r.SMTP.Get(ctx)
	switch {
	case err == nil && st.Enabled:
		warnings = domain.EmailSenderWarnings(st)
	case err != nil && !errors.Is(err, domain.ErrNotFound):
		return nil, wrap("app.SendTestEmail", err)
	}
	b, err := business(ctx, r)
	if err != nil {
		return nil, wrap("app.SendTestEmail", err)
	}
	if err := s.mailer.Send(ctx, ports.Mail{
		MessageID: s.ids.NewID(), To: []string{to}, Subject: "cactusInv test e-mail",
		Body: "This is a test e-mail from " + b.Name + ". Your mail server settings work.",
	}); err != nil {
		_ = s.auditNow(ctx, "settings.test_email_failed", "settings", "email", "to "+to)
		return nil, wrap("app.SendTestEmail", &MailError{Err: err})
	}
	_ = s.auditNow(ctx, "settings.test_email_sent", "settings", "email", "to "+to)
	return warnings, nil
}

// MailError is a failure of the mail server itself (as opposed to a database or input
// error), so the API can say "the mail server refused" only when it did.
type MailError struct{ Err error }

func (e *MailError) Error() string { return e.Err.Error() }
func (e *MailError) Unwrap() error { return e.Err }
