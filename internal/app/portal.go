package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// PortalRecentPaid is how many paid invoices the customer's account page lists.
const PortalRecentPaid = 20

// PortalView is the customer's account page for a statement e-mail's link (M59): their
// statement (what is owed, invoice by invoice) and their latest paid invoices.
type PortalView struct {
	Statement domain.Statement
	Business  domain.BusinessProfile
	Currency  domain.Currency
	Paid      []domain.Invoice // newest first, at most PortalRecentPaid
}

// customerForPortal finds the customer a valid account-link token belongs to. Unknown,
// malformed, expired and deleted customers' tokens are all ErrNotFound.
func (s *Service) customerForPortal(ctx context.Context, r ports.Repos, token string) (string, error) {
	if len(token) != 43 {
		return "", domain.ErrNotFound
	}
	return r.Customers.CustomerIDByPortalToken(ctx, HashToken(token), s.now())
}

// GetPortal is the customer's account page: no sign-in, the token is the only key.
// Opening it changes nothing.
func (s *Service) GetPortal(ctx context.Context, token string) (PortalView, error) {
	r := s.tx.Repos()
	id, err := s.customerForPortal(ctx, r, token)
	if err != nil {
		return PortalView{}, wrap("app.GetPortal", err)
	}
	doc, err := s.statementDoc(ctx, r, id)
	if err != nil {
		return PortalView{}, wrap("app.GetPortal", err)
	}
	paid, _, err := r.Invoices.List(ctx, ports.InvoiceFilter{CustomerID: id, Status: domain.InvoicePaid, Page: ports.Page{Limit: PortalRecentPaid}})
	if err != nil {
		return PortalView{}, wrap("app.GetPortal", err)
	}
	return PortalView{Statement: doc.Statement, Business: doc.Business, Currency: doc.Currency, Paid: paid}, nil
}

// PortalStatementPDF is the customer's statement as of today; viewing it is audited as the
// customer's.
func (s *Service) PortalStatementPDF(ctx context.Context, token string) ([]byte, string, error) {
	id, err := s.customerForPortal(ctx, s.tx.Repos(), token)
	if err != nil {
		return nil, "", wrap("app.PortalStatementPDF", err)
	}
	return s.StatementPDF(WithActor(ctx, ActorCustomerLink), id)
}

// PortalInvoicePDF is one of the customer's issued invoices (not a draft or a void one);
// another customer's invoice is ErrNotFound. Viewing it is audited as the customer's.
func (s *Service) PortalInvoicePDF(ctx context.Context, token, invoiceID string) ([]byte, string, error) {
	r := s.tx.Repos()
	id, err := s.customerForPortal(ctx, r, token)
	if err != nil {
		return nil, "", wrap("app.PortalInvoicePDF", err)
	}
	inv, err := r.Invoices.Get(ctx, invoiceID)
	if err != nil {
		return nil, "", wrap("app.PortalInvoicePDF", err)
	}
	if inv.CustomerID != id || inv.Status == domain.InvoiceDraft || inv.Status == domain.InvoiceVoid {
		return nil, "", wrap("app.PortalInvoicePDF", domain.ErrNotFound)
	}
	return s.InvoicePDF(WithActor(ctx, ActorCustomerLink), inv.ID)
}

// addPortalLink gives a statement e-mail its account link (M59), at the address customers
// reach the app on (linkBase); without one the e-mail has none.
func (s *Service) addPortalLink(ctx context.Context, r ports.Repos, customerID, requestBase, body string) (string, error) {
	base, err := s.linkBase(ctx, r, requestBase)
	if err != nil || base == "" {
		return body, err
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	if err := r.Customers.AddPortalToken(ctx, customerID, HashToken(token), s.now(), s.now().Add(domain.PortalLinkValidity)); err != nil {
		return "", err
	}
	return domain.AppendPortalLink(body, domain.PortalLink(base, token)), nil
}
