package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// LineInput is one line as submitted. With a ProductID, an empty Description uses the
// product's and a nil UnitPrice uses the product's price in the invoice currency.
type LineInput struct {
	ProductID   string
	Description string
	Quantity    domain.Quantity
	UnitPrice   *int64
	Discount    domain.BasisPoints
}

// InvoiceInput creates or replaces a draft. Nil pointers fall back to the customer's
// defaults.
type InvoiceInput struct {
	CustomerID string
	IssueDate  domain.Date
	Header     domain.InvoiceHeader
	BillTo     *domain.Party
	ShipTo     *domain.Party
	Notes      *string
	TaxRate    *domain.BasisPoints // VAT; nil → the business default VAT rate
	Terms      *domain.PaymentTerms
	Lines      []LineInput
}

// InvoiceDetail is an invoice with its audit trail and e-mails.
type InvoiceDetail struct {
	Invoice *domain.Invoice
	Events  []domain.InvoiceEvent
	Emails  []domain.EmailMessage
}

// ListInvoices returns a page of invoices (without lines) and the total count.
func (s *Service) ListInvoices(ctx context.Context, f ports.InvoiceFilter) ([]domain.Invoice, int, error) {
	r := s.tx.Repos()
	b, err := business(ctx, r)
	if err != nil {
		return nil, 0, wrap("app.ListInvoices", err)
	}
	f.Today = s.today(b)
	f.Page = f.Page.Normalize()
	out, n, err := r.Invoices.List(ctx, f)
	return out, n, wrap("app.ListInvoices", err)
}

// GetInvoice returns an invoice with lines, events and e-mails.
func (s *Service) GetInvoice(ctx context.Context, id string) (InvoiceDetail, error) {
	r := s.tx.Repos()
	inv, err := r.Invoices.Get(ctx, id)
	if err != nil {
		return InvoiceDetail{}, wrap("app.GetInvoice", err)
	}
	events, err := r.Invoices.ListEvents(ctx, id)
	if err != nil {
		return InvoiceDetail{}, wrap("app.GetInvoice", err)
	}
	emails, _, err := r.Emails.List(ctx, ports.EmailFilter{InvoiceID: id, Page: ports.Page{Limit: 50}})
	if err != nil {
		return InvoiceDetail{}, wrap("app.GetInvoice", err)
	}
	return InvoiceDetail{Invoice: inv, Events: events, Emails: emails}, nil
}

// CreateInvoice creates a draft for a customer, in the customer's currency.
func (s *Service) CreateInvoice(ctx context.Context, in InvoiceInput) (*domain.Invoice, error) {
	id := s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		c, err := r.Customers.Get(ctx, in.CustomerID)
		if err != nil {
			return err
		}
		if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
			return err
		}
		content, err := s.draftContent(ctx, r, c, in)
		if err != nil {
			return err
		}
		inv, err := domain.NewDraftInvoice(id, c, content, s.now())
		if err != nil {
			return err
		}
		if err := r.Invoices.Create(ctx, inv); err != nil {
			return err
		}
		return s.event(ctx, r, id, "created", "")
	})
	if err != nil {
		return nil, wrap("app.CreateInvoice", err)
	}
	inv, err := s.tx.Repos().Invoices.Get(ctx, id)
	return inv, wrap("app.CreateInvoice", err)
}

// UpdateInvoice replaces a draft's content. version must match (optimistic locking).
func (s *Service) UpdateInvoice(ctx context.Context, id string, version int, in InvoiceInput) (*domain.Invoice, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		inv, err := r.Invoices.Get(ctx, id)
		if err != nil {
			return err
		}
		if inv.Version != version {
			return domain.ErrConflict
		}
		if in.CustomerID != "" && in.CustomerID != inv.CustomerID {
			return &domain.ValidationError{Field: "customerId", Reason: "cannot change; create a new invoice"}
		}
		c, err := r.Customers.Get(ctx, inv.CustomerID)
		if err != nil {
			return err
		}
		c.Currency = inv.Currency // prices resolve in the invoice's frozen currency
		content, err := s.draftContent(ctx, r, c, in)
		if err != nil {
			return err
		}
		if err := inv.UpdateDraft(content, s.now()); err != nil {
			return err
		}
		if err := r.Invoices.Update(ctx, inv); err != nil {
			return err
		}
		return s.event(ctx, r, id, "updated", "")
	})
	if err != nil {
		return nil, wrap("app.UpdateInvoice", err)
	}
	inv, err := s.tx.Repos().Invoices.Get(ctx, id)
	return inv, wrap("app.UpdateInvoice", err)
}

// draftContent merges input over the customer's defaults and prices product lines.
func (s *Service) draftContent(ctx context.Context, r ports.Repos, c domain.Customer, in InvoiceInput) (domain.DraftContent, error) {
	b, err := business(ctx, r)
	if err != nil {
		return domain.DraftContent{}, err
	}
	issue := in.IssueDate
	if issue.IsZero() {
		issue = s.today(b)
	}
	content := domain.DefaultDraftContent(c, issue)
	if content.Notes == "" {
		content.Notes = b.DefaultNotes
	}
	content.Header = in.Header
	content.TaxRate = b.VATRate
	if in.TaxRate != nil {
		content.TaxRate = *in.TaxRate
	}
	if in.BillTo != nil {
		content.BillTo = *in.BillTo
	}
	if in.ShipTo != nil {
		content.ShipTo = *in.ShipTo
	}
	if in.Notes != nil {
		content.Notes = *in.Notes
	}
	if in.Terms != nil {
		content.Terms = *in.Terms
	}
	var ids []string
	for _, l := range in.Lines {
		if l.ProductID != "" {
			ids = append(ids, l.ProductID)
		}
	}
	products, err := r.Products.GetMany(ctx, ids)
	if err != nil {
		return domain.DraftContent{}, err
	}
	for _, l := range in.Lines {
		line := domain.Line{ProductID: l.ProductID, Description: l.Description, Quantity: l.Quantity, Discount: l.Discount}
		if l.UnitPrice != nil {
			line.UnitPrice = *l.UnitPrice
		}
		if l.ProductID != "" {
			p, ok := products[l.ProductID]
			if !ok {
				return domain.DraftContent{}, &domain.ValidationError{Field: "lines.productId", Reason: "unknown product"}
			}
			if strings.TrimSpace(line.Description) == "" {
				line.Description = p.LineDescription()
			}
			if l.UnitPrice == nil {
				price, err := p.PriceIn(c.Currency)
				if err != nil {
					return domain.DraftContent{}, err
				}
				line.UnitPrice = price.Amount
			}
		} else if l.UnitPrice == nil {
			return domain.DraftContent{}, &domain.ValidationError{Field: "lines.unitPrice", Reason: "is required without a product"}
		}
		content.Lines = append(content.Lines, line)
	}
	return content, nil
}

// IssueInvoice freezes a draft and gives it the next invoice number. A zero issueDate
// means today in the business time zone.
func (s *Service) IssueInvoice(ctx context.Context, id string, issueDate domain.Date) (*domain.Invoice, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		inv, err := r.Invoices.Get(ctx, id)
		if err != nil {
			return err
		}
		return s.issue(ctx, r, inv, issueDate)
	})
	if err != nil {
		return nil, wrap("app.IssueInvoice", err)
	}
	inv, err := s.tx.Repos().Invoices.Get(ctx, id)
	return inv, wrap("app.IssueInvoice", err)
}

// issue allocates the number, resolves the bank account and saves. Must run in a tx.
func (s *Service) issue(ctx context.Context, r ports.Repos, inv *domain.Invoice, issueDate domain.Date) error {
	if inv.Status != domain.InvoiceDraft {
		return domain.ErrInvalidTransition
	}
	b, err := business(ctx, r)
	if err != nil {
		return err
	}
	if issueDate.IsZero() {
		issueDate = inv.IssueDate
	}
	if issueDate.IsZero() {
		issueDate = s.today(b)
	}
	c, err := r.Customers.Get(ctx, inv.CustomerID)
	if err != nil {
		return err
	}
	c.Currency = inv.Currency
	accounts, err := r.Banks.List(ctx, inv.Currency)
	if err != nil {
		return err
	}
	acct, err := domain.ResolveBankAccount(c, accounts)
	if err != nil {
		return err
	}
	seq, err := r.Sequences.Next(ctx)
	if err != nil {
		return err
	}
	number, err := domain.FormatInvoiceNumber(b.NumberFormat, c.Code, seq, issueDate)
	if err != nil {
		return err
	}
	if err := inv.Issue(number, issueDate, acct.Details(), s.now()); err != nil {
		return err
	}
	if err := r.Invoices.Update(ctx, inv); err != nil {
		return err
	}
	return s.event(ctx, r, inv.ID, "issued", number)
}

// VoidInvoice cancels an issued invoice; its number stays used.
func (s *Service) VoidInvoice(ctx context.Context, id, reason string) (*domain.Invoice, error) {
	return s.transition(ctx, "app.VoidInvoice", id, "voided", reason, func(inv *domain.Invoice) error {
		return inv.Void(reason, s.now())
	}, func(ctx context.Context, r ports.Repos) error {
		// Withdraw anything still waiting to be sent.
		pending, _, err := r.Emails.List(ctx, ports.EmailFilter{InvoiceID: id, Status: domain.EmailPending, Page: ports.Page{Limit: 200}})
		if err != nil {
			return err
		}
		for i := range pending {
			m := pending[i]
			if err := m.Cancel(s.now()); err != nil {
				return err
			}
			if err := r.Emails.Update(ctx, &m); err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkInvoicePaid records payment.
func (s *Service) MarkInvoicePaid(ctx context.Context, id string) (*domain.Invoice, error) {
	return s.transition(ctx, "app.MarkInvoicePaid", id, "paid", "", func(inv *domain.Invoice) error {
		return inv.MarkPaid(s.now())
	}, nil)
}

func (s *Service) transition(ctx context.Context, op, id, event, detail string, apply func(*domain.Invoice) error, after func(context.Context, ports.Repos) error) (*domain.Invoice, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		inv, err := r.Invoices.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := apply(inv); err != nil {
			return err
		}
		if err := r.Invoices.Update(ctx, inv); err != nil {
			return err
		}
		if after != nil {
			if err := after(ctx, r); err != nil {
				return err
			}
		}
		return s.event(ctx, r, id, event, detail)
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	inv, err := s.tx.Repos().Invoices.Get(ctx, id)
	return inv, wrap(op, err)
}

// DeleteInvoice deletes a draft. Issued invoices are voided, never deleted.
func (s *Service) DeleteInvoice(ctx context.Context, id string) error {
	return wrap("app.DeleteInvoice", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		inv, err := r.Invoices.Get(ctx, id)
		if err != nil {
			return err
		}
		if !inv.CanDelete() {
			return domain.ErrInvalidTransition
		}
		return r.Invoices.Delete(ctx, id)
	}))
}

// InvoicePDF renders an invoice (drafts are watermarked by the renderer). It returns the
// PDF and a file name.
func (s *Service) InvoicePDF(ctx context.Context, id string) ([]byte, string, error) {
	r := s.tx.Repos()
	doc, err := s.document(ctx, r, id)
	if err != nil {
		return nil, "", wrap("app.InvoicePDF", err)
	}
	pdf, err := s.renderer.RenderPDF(ctx, doc)
	if err != nil {
		return nil, "", wrap("app.InvoicePDF", err)
	}
	return pdf, PDFFilename(doc.Invoice), nil
}

func (s *Service) document(ctx context.Context, r ports.Repos, id string) (ports.InvoiceDocument, error) {
	inv, err := r.Invoices.Get(ctx, id)
	if err != nil {
		return ports.InvoiceDocument{}, err
	}
	b, err := business(ctx, r)
	if err != nil {
		return ports.InvoiceDocument{}, err
	}
	cur, err := r.Currencies.Get(ctx, inv.Currency)
	if err != nil {
		return ports.InvoiceDocument{}, err
	}
	return ports.InvoiceDocument{Invoice: inv, Business: b, Currency: cur}, nil
}

// PDFFilename is "Inv_no_CD_1195_26.pdf" for "CD/1195/26", like the example file.
func PDFFilename(inv *domain.Invoice) string {
	if inv.Number == "" {
		return "Draft_" + inv.ID + ".pdf"
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '_'
	}, inv.Number)
	return "Inv_no_" + safe + ".pdf"
}

// SendInput describes an e-mail of an invoice. Empty fields use the customer's addresses
// and the business templates; a zero SendAt means now.
type SendInput struct {
	To             []string
	CC             []string
	Subject        string
	Body           string
	SendAt         time.Time
	IdempotencyKey string
}

// SendInvoice queues an e-mail of the invoice in the outbox (now or later). A draft is
// issued first, in the same transaction. Repeating a request with the same
// IdempotencyKey returns the original message.
func (s *Service) SendInvoice(ctx context.Context, id string, in SendInput) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if in.IdempotencyKey != "" {
			if m, err := r.Emails.GetByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
				out = m
				return nil
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		inv, err := r.Invoices.Get(ctx, id)
		if err != nil {
			return err
		}
		if inv.Status == domain.InvoiceDraft {
			b, err := business(ctx, r)
			if err != nil {
				return err
			}
			if err := s.issue(ctx, r, inv, s.today(b)); err != nil {
				return err
			}
		}
		m, err := s.enqueue(ctx, r, inv, in)
		out = m
		return err
	})
	return out, wrap("app.SendInvoice", err)
}

// enqueue writes the outbox row for inv. Must run in a tx.
func (s *Service) enqueue(ctx context.Context, r ports.Repos, inv *domain.Invoice, in SendInput) (*domain.EmailMessage, error) {
	if !inv.CanSend() {
		return nil, domain.ErrInvalidTransition
	}
	c, err := r.Customers.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}
	b, err := business(ctx, r)
	if err != nil {
		return nil, err
	}
	cur, err := r.Currencies.Get(ctx, inv.Currency)
	if err != nil {
		return nil, err
	}
	to, cc := in.To, in.CC
	if len(to) == 0 {
		to = []string{c.Email}
		if len(cc) == 0 {
			cc = c.CC
		}
	}
	data := domain.TemplateDataFor(inv, cur, b)
	subject, body := in.Subject, in.Body
	if subject == "" {
		subject = b.EmailSubject
	}
	if body == "" {
		body = b.EmailBody
	}
	m, err := domain.NewEmailMessage(s.ids.NewID(), inv.ID, to, cc,
		domain.SubjectLine(subject, data), domain.RenderTemplate(body, data),
		in.SendAt, s.now(), in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if err := r.Emails.Create(ctx, m); err != nil {
		return nil, err
	}
	detail := "queued"
	if m.SendAt.After(s.now()) {
		detail = "scheduled for " + m.SendAt.Format("2006-01-02 15:04 MST")
	}
	return m, s.event(ctx, r, inv.ID, "email_queued", detail)
}
