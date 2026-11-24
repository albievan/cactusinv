package app

import (
	"context"
	"fmt"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// PaymentInput is a payment as entered (M58). A zero ReceivedOn means today in the
// business's time zone.
type PaymentInput struct {
	Amount     int64
	ReceivedOn domain.Date
	Reference  string
}

// RecordPayment records money received against an open invoice: a part-payment, or the
// rest, which marks it paid. Audited on the invoice with the amount and date.
func (s *Service) RecordPayment(ctx context.Context, invoiceID string, in PaymentInput) (*domain.Invoice, error) {
	return s.payment(ctx, "app.RecordPayment", invoiceID, func(ctx context.Context, r ports.Repos, inv *domain.Invoice, today domain.Date) (string, error) {
		p := domain.Payment{ID: s.ids.NewID(), Amount: in.Amount, ReceivedOn: in.ReceivedOn, Reference: in.Reference}
		if p.ReceivedOn.IsZero() {
			p.ReceivedOn = today
		}
		if today.Before(p.ReceivedOn) {
			return "", &domain.ValidationError{Field: "receivedOn", Reason: "cannot be in the future"}
		}
		if err := inv.RecordPayment(&p, s.now()); err != nil {
			return "", err
		}
		return "payment_recorded", r.Invoices.AddPayment(ctx, p)
	})
}

// RemovePayment takes a payment off an invoice (entered by mistake, or it bounced); a paid
// invoice opens again. Audited with the amount and date.
func (s *Service) RemovePayment(ctx context.Context, invoiceID, paymentID string) (*domain.Invoice, error) {
	return s.payment(ctx, "app.RemovePayment", invoiceID, func(ctx context.Context, r ports.Repos, inv *domain.Invoice, _ domain.Date) (string, error) {
		p, err := r.Invoices.GetPayment(ctx, invoiceID, paymentID)
		if err != nil {
			return "", err
		}
		if err := inv.RemovePayment(p, s.now()); err != nil {
			return "", err
		}
		return "payment_removed", r.Invoices.DeletePayment(ctx, invoiceID, paymentID)
	})
}

// ListPayments returns an invoice's payments, oldest received first.
func (s *Service) ListPayments(ctx context.Context, invoiceID string) ([]domain.Payment, error) {
	out, err := s.tx.Repos().Invoices.ListPayments(ctx, invoiceID)
	return out, wrap("app.ListPayments", err)
}

// payment runs change on an invoice in a transaction, saves it and audits the event change
// names, with the amount and date of the payment it added or removed.
func (s *Service) payment(ctx context.Context, op, invoiceID string, change func(context.Context, ports.Repos, *domain.Invoice, domain.Date) (string, error)) (*domain.Invoice, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		inv, err := r.Invoices.Get(ctx, invoiceID)
		if err != nil {
			return err
		}
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		paid := inv.AmountPaid
		event, err := change(ctx, r, inv, s.today(b))
		if err != nil {
			return err
		}
		if err := r.Invoices.Update(ctx, inv); err != nil {
			return err
		}
		detail, err := paymentDetail(ctx, r, inv, inv.AmountPaid-paid)
		if err != nil {
			return err
		}
		return s.event(ctx, r, invoiceID, event, detail)
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	inv, err := s.tx.Repos().Invoices.Get(ctx, invoiceID)
	return inv, wrap(op, err)
}

// paymentDetail is the audit detail of a payment change: the amount, signed, and what is
// still owed, in the invoice's currency (e.g. "ZAR 1,400.00 received; balance ZAR 600.00").
func paymentDetail(ctx context.Context, r ports.Repos, inv *domain.Invoice, delta int64) (string, error) {
	c, err := r.Currencies.Get(ctx, inv.Currency)
	if err != nil {
		return "", err
	}
	units := c.MinorUnits
	verb := "received"
	if delta < 0 {
		verb, delta = "removed", -delta
	}
	return fmt.Sprintf("%s %s %s; balance %s %s", inv.Currency, domain.FormatGrouped(delta, units, ","), verb,
		inv.Currency, domain.FormatGrouped(inv.Balance(), units, ",")), nil
}
