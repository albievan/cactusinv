package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// InvoiceResponseView is what the customer's page shows for the "I have paid" link in an
// invoice e-mail (M42).
type InvoiceResponseView struct {
	Invoice  *domain.Invoice
	Business domain.BusinessProfile
	Today    domain.Date // the business's today, for the invoice's stage
}

// invoiceForToken finds the invoice a link token belongs to. Unknown, malformed and
// deleted invoices' tokens are all ErrNotFound.
func (s *Service) invoiceForToken(ctx context.Context, r ports.Repos, token string) (*domain.Invoice, error) {
	if len(token) != 43 {
		return nil, domain.ErrNotFound
	}
	id, err := r.Invoices.InvoiceIDByResponseToken(ctx, HashToken(token))
	if err != nil {
		return nil, err
	}
	return r.Invoices.Get(ctx, id)
}

func (s *Service) invoiceResponseView(ctx context.Context, r ports.Repos, token string) (InvoiceResponseView, error) {
	inv, err := s.invoiceForToken(ctx, r, token)
	if err != nil {
		return InvoiceResponseView{}, err
	}
	b, err := s.business(ctx, r)
	if err != nil {
		return InvoiceResponseView{}, err
	}
	return InvoiceResponseView{Invoice: inv, Business: b, Today: s.today(b)}, nil
}

// GetInvoiceResponse is the customer's page for an invoice e-mail's link: no sign-in, the
// token is the only key. Opening it changes nothing.
func (s *Service) GetInvoiceResponse(ctx context.Context, token string) (InvoiceResponseView, error) {
	v, err := s.invoiceResponseView(ctx, s.tx.Repos(), token)
	return v, wrap("app.GetInvoiceResponse", err)
}

// MarkInvoicePaidByCustomer records, through the e-mail link, that the customer says they
// paid, with their optional note (M42). It records no payment: the invoice stays unpaid
// until the business marks it paid. Marking again is no change.
func (s *Service) MarkInvoicePaidByCustomer(ctx context.Context, token, note string) (InvoiceResponseView, error) {
	ctx = WithActor(ctx, ActorCustomerLink)
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		inv, err := s.invoiceForToken(ctx, r, token)
		if err != nil {
			return err
		}
		if inv.Status == domain.InvoicePaid {
			return nil // the business has it already
		}
		changed, err := inv.MarkPaidByCustomer(note, s.now())
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if err := r.Invoices.SetCustomerPaid(ctx, inv); err != nil {
			return err
		}
		detail := inv.Number + " (e-mail link)"
		if inv.CustomerPaidNote != "" {
			detail += ": " + domain.TruncateUTF8(inv.CustomerPaidNote, 400)
		}
		return s.event(ctx, r, inv.ID, "customer_marked_paid", detail)
	})
	if err != nil {
		return InvoiceResponseView{}, wrap("app.MarkInvoicePaidByCustomer", err)
	}
	v, err := s.invoiceResponseView(ctx, s.tx.Repos(), token)
	return v, wrap("app.MarkInvoicePaidByCustomer", err)
}

// InvoiceResponsePDF is the invoice's PDF for the customer's page; viewing it is audited
// as the customer's.
func (s *Service) InvoiceResponsePDF(ctx context.Context, token string) ([]byte, string, error) {
	inv, err := s.invoiceForToken(ctx, s.tx.Repos(), token)
	if err != nil {
		return nil, "", wrap("app.InvoiceResponsePDF", err)
	}
	return s.InvoicePDF(WithActor(ctx, ActorCustomerLink), inv.ID)
}
