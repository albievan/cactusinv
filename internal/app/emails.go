package app

import (
	"context"
	"errors"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ListEmails returns a page of outbox messages.
func (s *Service) ListEmails(ctx context.Context, f ports.EmailFilter) ([]domain.EmailMessage, int, error) {
	f.Page = f.Page.Normalize()
	out, n, err := s.tx.Repos().Emails.List(ctx, f)
	return out, n, wrap("app.ListEmails", err)
}

// CancelEmail withdraws a pending (e.g. scheduled) or failed message.
func (s *Service) CancelEmail(ctx context.Context, id string) (*domain.EmailMessage, error) {
	return s.emailTransition(ctx, "app.CancelEmail", id, "email_cancelled", (*domain.EmailMessage).Cancel)
}

// RetryEmail re-queues a failed or cancelled message for immediate delivery.
func (s *Service) RetryEmail(ctx context.Context, id string) (*domain.EmailMessage, error) {
	return s.emailTransition(ctx, "app.RetryEmail", id, "email_retried", (*domain.EmailMessage).Retry)
}

func (s *Service) emailTransition(ctx context.Context, op, id, event string, apply func(*domain.EmailMessage, timeT) error) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		m, err := r.Emails.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := apply(m, s.now()); err != nil {
			return err
		}
		if err := r.Emails.Update(ctx, m); err != nil {
			return err
		}
		out = m
		if m.InvoiceID == "" {
			return nil
		}
		return s.event(ctx, r, m.InvoiceID, event, "")
	})
	return out, wrap(op, err)
}

// DispatchResult counts what one dispatcher pass did.
type DispatchResult struct {
	Sent   int
	Failed int
}

// DispatchEmails delivers up to limit due outbox messages (ADR-004). Each message is
// claimed in its own short transaction, sent outside any transaction, then recorded.
func (s *Service) DispatchEmails(ctx context.Context, limit int) (DispatchResult, error) {
	var res DispatchResult
	for range limit {
		if stopping(ctx) {
			return res, nil
		}
		m, err := s.claimEmail(ctx)
		if err != nil {
			return res, wrap("app.DispatchEmails", err)
		}
		if m == nil {
			return res, nil
		}
		if err := s.deliver(ctx, m); err != nil {
			res.Failed++
			s.log.WarnContext(ctx, "email delivery failed", "email_id", m.ID, "invoice_id", m.InvoiceID, "attempt", m.Attempts, "err", err)
			if rerr := s.recordEmailFailure(ctx, m.ID, err); rerr != nil {
				return res, wrap("app.DispatchEmails", rerr)
			}
			continue
		}
		res.Sent++
	}
	return res, nil
}

func (s *Service) claimEmail(ctx context.Context) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		due, err := r.Emails.ClaimDue(ctx, s.now(), 1)
		if err != nil || len(due) == 0 {
			return err
		}
		m := due[0]
		if err := m.Claim(s.now()); err != nil {
			return err
		}
		out = m
		return r.Emails.Update(ctx, m)
	})
	return out, err
}

func (s *Service) deliver(ctx context.Context, m *domain.EmailMessage) error {
	if m.Kind == domain.EmailKindStatement {
		return s.deliverStatement(ctx, m)
	}
	r := s.tx.Repos()
	doc, err := s.document(ctx, r, m.InvoiceID)
	if err != nil {
		return err
	}
	if doc.Invoice.Status == domain.InvoiceVoid {
		return errVoided
	}
	pdf, err := s.renderer.RenderPDF(ctx, doc)
	if err != nil {
		return err
	}
	mail := ports.Mail{
		MessageID: m.ID, To: m.To, CC: m.CC, ReplyTo: doc.Business.Email,
		Subject: m.Subject, Body: m.Body,
		Attachments: []ports.Attachment{{Filename: PDFFilename(doc.Invoice), ContentType: "application/pdf", Data: pdf}},
	}
	if err := s.mailer.Send(ctx, mail); err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.Emails.Get(ctx, m.ID)
		if err != nil {
			return err
		}
		cur.Succeeded(s.now())
		if err := r.Emails.Update(ctx, cur); err != nil {
			return err
		}
		inv, err := r.Invoices.Get(ctx, m.InvoiceID)
		if err != nil {
			return err
		}
		if err := inv.MarkSent(s.now()); err == nil {
			if err := r.Invoices.Update(ctx, inv); err != nil {
				return err
			}
		}
		return s.event(ctx, r, m.InvoiceID, "email_sent", m.ID)
	})
}

var errVoided = errors.New("invoice was voided")

func (s *Service) recordEmailFailure(ctx context.Context, id string, cause error) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		m, err := r.Emails.Get(ctx, id)
		if err != nil {
			return err
		}
		if errors.Is(cause, errVoided) {
			m.Attempts = domain.MaxEmailAttempts
		}
		m.Failed(cause.Error(), s.now())
		if err := r.Emails.Update(ctx, m); err != nil {
			return err
		}
		if m.Status == domain.EmailFailed && m.InvoiceID != "" {
			return s.event(ctx, r, m.InvoiceID, "email_failed", m.LastError)
		}
		return nil
	})
}
