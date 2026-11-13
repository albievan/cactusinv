package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// QuoteInput creates or replaces a quote. Exactly one of CustomerID and AdhocCustomerID is
// set on create (the quote's party cannot change afterwards). Zero dates and nil pointers
// fall back to today, the Quote settings and the customer's details.
type QuoteInput struct {
	CustomerID      string
	AdhocCustomerID string
	IssueDate       domain.Date
	ValidUntil      domain.Date
	SalesPerson     string
	Reference       string
	BillTo          *domain.Party
	Notes           *string
	Terms           *string
	TaxRate         *domain.BasisPoints
	Lines           []LineInput
}

// QuoteDetail is a quote with its audit trail, e-mails and who it is for.
type QuoteDetail struct {
	Quote  *domain.Quote
	Events []domain.AuditEntry
	Emails []domain.EmailMessage
	// Adhoc is the ad-hoc customer the quote was made out to (nil for a customer's quote).
	Adhoc *domain.AdhocCustomer
	// InvoiceNumber is the number of the invoice made from the quote, once issued.
	InvoiceNumber string
	Today         domain.Date
}

// quoteEvent records a quote action in the audit trail.
func (s *Service) quoteEvent(ctx context.Context, r ports.Repos, quoteID, typ, detail string) error {
	return s.audit(ctx, r, "quote."+typ, "quote", quoteID, detail)
}

// ListQuotes returns a page of quotes (without lines) and the total count.
func (s *Service) ListQuotes(ctx context.Context, f ports.QuoteFilter) ([]domain.Quote, int, error) {
	r := s.tx.Repos()
	b, err := s.business(ctx, r)
	if err != nil {
		return nil, 0, wrap("app.ListQuotes", err)
	}
	f.Today = s.today(b)
	f.Page = f.Page.Normalize()
	out, n, err := r.Quotes.List(ctx, f)
	return out, n, wrap("app.ListQuotes", err)
}

// GetQuote returns a quote with lines, events and e-mails.
func (s *Service) GetQuote(ctx context.Context, id string) (QuoteDetail, error) {
	r := s.tx.Repos()
	q, err := r.Quotes.Get(ctx, id)
	if err != nil {
		return QuoteDetail{}, wrap("app.GetQuote", err)
	}
	d := QuoteDetail{Quote: q}
	if d.Events, err = r.Audit.ForEntity(ctx, "quote", id); err != nil {
		return QuoteDetail{}, wrap("app.GetQuote", err)
	}
	if d.Emails, _, err = r.Emails.List(ctx, ports.EmailFilter{QuoteID: id, Page: ports.Page{Limit: 50}}); err != nil {
		return QuoteDetail{}, wrap("app.GetQuote", err)
	}
	if q.AdhocCustomerID != "" {
		a, err := r.AdhocCustomers.Get(ctx, q.AdhocCustomerID)
		if err != nil {
			return QuoteDetail{}, wrap("app.GetQuote", err)
		}
		d.Adhoc = &a
	}
	if q.InvoiceID != "" {
		if inv, err := r.Invoices.Get(ctx, q.InvoiceID); err == nil {
			d.InvoiceNumber = inv.Number
		}
	}
	b, err := s.business(ctx, r)
	if err != nil {
		return QuoteDetail{}, wrap("app.GetQuote", err)
	}
	d.Today = s.today(b)
	return d, nil
}

// quoteParty resolves who a new quote is for: an active customer or an ad-hoc customer
// not yet made a customer, and their currency, which must be active.
func quoteParty(ctx context.Context, r ports.Repos, in QuoteInput) (domain.QuoteFor, domain.Party, error) {
	switch {
	case (in.CustomerID == "") == (in.AdhocCustomerID == ""):
		return domain.QuoteFor{}, domain.Party{}, &domain.ValidationError{Field: "customerId", Reason: "choose a customer or an ad-hoc customer"}
	case in.CustomerID != "":
		c, err := r.Customers.Get(ctx, in.CustomerID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.QuoteFor{}, domain.Party{}, &domain.ValidationError{Field: "customerId", Reason: "unknown customer"}
		}
		if err != nil {
			return domain.QuoteFor{}, domain.Party{}, err
		}
		if !c.Active {
			return domain.QuoteFor{}, domain.Party{}, domain.ErrInactive
		}
		if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
			return domain.QuoteFor{}, domain.Party{}, err
		}
		return domain.QuoteFor{CustomerID: c.ID, Currency: c.Currency}, domain.Party{Name: c.Name, Address: c.BillingAddress, Attention: c.Attention}, nil
	default:
		a, err := r.AdhocCustomers.Get(ctx, in.AdhocCustomerID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.QuoteFor{}, domain.Party{}, &domain.ValidationError{Field: "adhocCustomerId", Reason: "unknown ad-hoc customer"}
		}
		if err != nil {
			return domain.QuoteFor{}, domain.Party{}, err
		}
		if a.Converted() {
			return domain.QuoteFor{}, domain.Party{}, &domain.ValidationError{Field: "adhocCustomerId", Reason: "this ad-hoc customer is now a customer: quote the customer"}
		}
		if _, err := activeCurrency(ctx, r, a.Currency); err != nil {
			return domain.QuoteFor{}, domain.Party{}, err
		}
		return domain.QuoteFor{AdhocCustomerID: a.ID, Currency: a.Currency}, domain.Party{Name: a.Name, Address: a.Address, Attention: a.Attention}, nil
	}
}

// quoteContent merges input over the defaults (today, the Quote settings, the business VAT
// and the party's address) and prices the lines in currency cur.
func (s *Service) quoteContent(ctx context.Context, r ports.Repos, cur domain.CurrencyCode, party domain.Party, in QuoteInput) (domain.QuoteContent, error) {
	b, err := s.business(ctx, r)
	if err != nil {
		return domain.QuoteContent{}, err
	}
	qs, err := s.quoteSettings(ctx, r)
	if err != nil {
		return domain.QuoteContent{}, err
	}
	c := domain.QuoteContent{
		IssueDate: in.IssueDate, ValidUntil: in.ValidUntil, SalesPerson: in.SalesPerson, Reference: in.Reference,
		BillTo: party, Terms: qs.Terms, TaxRate: b.VATRate,
	}
	if c.IssueDate.IsZero() {
		c.IssueDate = s.today(b)
	}
	if c.ValidUntil.IsZero() {
		c.ValidUntil = c.IssueDate.AddDays(qs.ValidDays)
	}
	if in.BillTo != nil {
		c.BillTo = *in.BillTo
	}
	if in.Notes != nil {
		c.Notes = *in.Notes
	}
	if in.Terms != nil {
		c.Terms = *in.Terms
	}
	if in.TaxRate != nil {
		c.TaxRate = *in.TaxRate
	}
	if c.Lines, err = s.priceLines(ctx, r, cur, in.Lines); err != nil {
		return domain.QuoteContent{}, err
	}
	return c, nil
}

// CreateQuote creates a draft quote, numbered from the quote sequence.
func (s *Service) CreateQuote(ctx context.Context, in QuoteInput) (*domain.Quote, error) {
	id := s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		to, party, err := quoteParty(ctx, r, in)
		if err != nil {
			return err
		}
		content, err := s.quoteContent(ctx, r, to.Currency, party, in)
		if err != nil {
			return err
		}
		qs, err := s.quoteSettings(ctx, r)
		if err != nil {
			return err
		}
		seq, err := r.QuoteSequences.Next(ctx)
		if err != nil {
			return err
		}
		number, err := domain.FormatQuoteNumber(qs.NumberFormat, seq, content.IssueDate)
		if err != nil {
			return err
		}
		q, err := domain.NewQuote(id, number, to, content, s.now())
		if err != nil {
			return err
		}
		if err := r.Quotes.Create(ctx, q); err != nil {
			return err
		}
		return s.quoteEvent(ctx, r, id, "created", number+" for "+q.BillTo.Name)
	})
	if err != nil {
		return nil, wrap("app.CreateQuote", err)
	}
	q, err := s.tx.Repos().Quotes.Get(ctx, id)
	return q, wrap("app.CreateQuote", err)
}

// UpdateQuote revises a draft or sent quote (a sent one becomes a draft again). version must
// match (optimistic locking). Nil fields fall back as on create, except the party.
func (s *Service) UpdateQuote(ctx context.Context, id string, version int, in QuoteInput) (*domain.Quote, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		q, err := r.Quotes.Get(ctx, id)
		if err != nil {
			return err
		}
		if q.Version != version {
			return domain.ErrConflict
		}
		if (in.CustomerID != "" && in.CustomerID != q.CustomerID) || (in.AdhocCustomerID != "" && in.AdhocCustomerID != q.AdhocCustomerID) {
			return &domain.ValidationError{Field: "customerId", Reason: "cannot change; create a new quote"}
		}
		if !q.Editable() {
			return domain.ErrInvalidTransition
		}
		if in.IssueDate.IsZero() {
			in.IssueDate = q.IssueDate
		}
		if in.ValidUntil.IsZero() {
			in.ValidUntil = q.ValidUntil
		}
		if in.Terms == nil {
			in.Terms = &q.Terms
		}
		content, err := s.quoteContent(ctx, r, q.Currency, q.BillTo, in)
		if err != nil {
			return err
		}
		if err := q.Update(content, s.now()); err != nil {
			return err
		}
		if err := r.Quotes.Update(ctx, q); err != nil {
			return err
		}
		return s.quoteEvent(ctx, r, id, "updated", q.Number)
	})
	if err != nil {
		return nil, wrap("app.UpdateQuote", err)
	}
	q, err := s.tx.Repos().Quotes.Get(ctx, id)
	return q, wrap("app.UpdateQuote", err)
}

// DeleteQuote deletes a draft that was never sent; its number is not reused.
func (s *Service) DeleteQuote(ctx context.Context, id string) error {
	return wrap("app.DeleteQuote", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		q, err := r.Quotes.Get(ctx, id)
		if err != nil {
			return err
		}
		if !q.CanDelete() {
			return domain.ErrInvalidTransition
		}
		if err := r.Quotes.Delete(ctx, id); err != nil {
			return err
		}
		return s.quoteEvent(ctx, r, id, "deleted", q.Number+" for "+q.BillTo.Name)
	}))
}

// AcceptQuote records that the customer accepted (staff in the app). An expired quote
// cannot be accepted: it is renewed instead (M41).
func (s *Service) AcceptQuote(ctx context.Context, id string) (*domain.Quote, error) {
	return s.quoteTransition(ctx, "app.AcceptQuote", id, "accepted", "", func(q *domain.Quote, today domain.Date) error {
		return q.Accept(today, domain.AnsweredByStaff, s.now())
	})
}

// DeclineQuote records that the customer turned the quote down (staff in the app).
func (s *Service) DeclineQuote(ctx context.Context, id, reason string) (*domain.Quote, error) {
	return s.quoteTransition(ctx, "app.DeclineQuote", id, "declined", strings.TrimSpace(reason), func(q *domain.Quote, _ domain.Date) error {
		return q.Decline(reason, domain.AnsweredByStaff, s.now())
	})
}

func (s *Service) quoteTransition(ctx context.Context, op, id, event, detail string, apply func(*domain.Quote, domain.Date) error) (*domain.Quote, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		q, err := r.Quotes.Get(ctx, id)
		if err != nil {
			return err
		}
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		if err := apply(q, s.today(b)); err != nil {
			return err
		}
		if err := r.Quotes.Update(ctx, q); err != nil {
			return err
		}
		if detail == "" {
			detail = q.Number
		}
		return s.quoteEvent(ctx, r, id, event, detail)
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	q, err := s.tx.Repos().Quotes.Get(ctx, id)
	return q, wrap(op, err)
}

// ConvertQuote makes a draft invoice from a quote: its lines at the quoted prices, its VAT,
// notes, sales person and reference, for the quote's customer (an ad-hoc customer must be
// made a customer first). The quote becomes converted and points at the invoice. Making
// the invoice needs the right to create invoices as well.
func (s *Service) ConvertQuote(ctx context.Context, id string) (*domain.Invoice, error) {
	if !ActorFrom(ctx).Can(domain.PermInvoicesCreate) {
		return nil, wrap("app.ConvertQuote", domain.ErrNoAccess)
	}
	invID := s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		q, err := r.Quotes.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := s.adoptCustomer(ctx, r, q); err != nil {
			return err
		}
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		if err := q.CanConvert(s.today(b)); err != nil {
			return err
		}
		c, err := r.Customers.Get(ctx, q.CustomerID)
		if err != nil {
			return err
		}
		if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
			return err
		}
		content, err := q.InvoiceContent(c, s.today(b))
		if err != nil {
			return err
		}
		inv, err := domain.NewDraftInvoice(invID, c, content, s.now())
		if err != nil {
			return err
		}
		if err := r.Invoices.Create(ctx, inv); err != nil {
			return err
		}
		if err := s.event(ctx, r, invID, "created", "from quote "+q.Number); err != nil {
			return err
		}
		if err := q.Convert(invID, s.today(b), s.now()); err != nil {
			return err
		}
		if err := r.Quotes.Update(ctx, q); err != nil {
			return err
		}
		return s.quoteEvent(ctx, r, id, "converted", q.Number+" into a draft invoice")
	})
	if err != nil {
		return nil, wrap("app.ConvertQuote", err)
	}
	inv, err := s.tx.Repos().Invoices.Get(ctx, invID)
	return inv, wrap("app.ConvertQuote", err)
}

// quoteInvoiceDeleted puts the quote a deleted draft invoice was made from back to accepted.
// Must run in the deleting transaction.
func (s *Service) quoteInvoiceDeleted(ctx context.Context, r ports.Repos, invoiceID string) error {
	q, err := r.Quotes.GetByInvoice(ctx, invoiceID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := q.InvoiceDeleted(s.now()); err != nil {
		return err
	}
	if err := r.Quotes.Update(ctx, q); err != nil {
		return err
	}
	return s.quoteEvent(ctx, r, q.ID, "invoice_deleted", q.Number+": its draft invoice was deleted; accepted again")
}

// adoptCustomer gives a quote for an ad-hoc customer who has since become a customer that
// customer. Converting the ad-hoc customer does this for their quotes, but a quote created
// while the conversion was committing is missed by it.
func (s *Service) adoptCustomer(ctx context.Context, r ports.Repos, q *domain.Quote) error {
	if q.CustomerID != "" || q.AdhocCustomerID == "" {
		return nil
	}
	a, err := r.AdhocCustomers.Get(ctx, q.AdhocCustomerID)
	if err != nil {
		return err
	}
	if a.Converted() {
		q.CustomerBecame(a.CustomerID, s.now())
	}
	return nil
}

// QuotePDF renders a quote. It returns the PDF and a file name.
func (s *Service) QuotePDF(ctx context.Context, id string) ([]byte, string, error) {
	doc, err := s.quoteDocument(ctx, s.tx.Repos(), id)
	if err != nil {
		return nil, "", wrap("app.QuotePDF", err)
	}
	pdf, err := s.renderer.RenderQuote(ctx, doc)
	if err != nil {
		return nil, "", wrap("app.QuotePDF", err)
	}
	if err := s.auditNow(ctx, "quote.pdf_viewed", "quote", id, doc.Quote.Number); err != nil {
		return nil, "", wrap("app.QuotePDF", err)
	}
	return pdf, QuoteFilename(doc.Quote), nil
}

// QuoteFilename is "Quote_Q-17_26.pdf" for "Q-17/26".
func QuoteFilename(q *domain.Quote) string { return "Quote_" + safeFileName(q.Number) + ".pdf" }

func (s *Service) quoteDocument(ctx context.Context, r ports.Repos, id string) (ports.QuoteDocument, error) {
	q, err := r.Quotes.Get(ctx, id)
	if err != nil {
		return ports.QuoteDocument{}, err
	}
	b, err := s.business(ctx, r)
	if err != nil {
		return ports.QuoteDocument{}, err
	}
	cur, err := r.Currencies.Get(ctx, q.Currency)
	if err != nil {
		return ports.QuoteDocument{}, err
	}
	return ports.QuoteDocument{Quote: q, Business: b, Currency: cur, Expired: q.Expired(s.today(b))}, nil
}

// SendQuote queues an e-mail of the quote in the outbox (now or later), with the Quote
// settings' e-mail template unless the request gives a subject or body. Empty recipients
// use the customer's (or ad-hoc customer's) address. A draft becomes sent. Repeating a
// request with the same IdempotencyKey returns the original message.
func (s *Service) SendQuote(ctx context.Context, id string, in SendInput) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if in.IdempotencyKey != "" {
			if m, err := r.Emails.GetByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
				if m.QuoteID != id {
					return domain.ErrConflict
				}
				out = m
				return nil
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		q, err := r.Quotes.Get(ctx, id)
		if err != nil {
			return err
		}
		if !q.CanSend() {
			return domain.ErrInvalidTransition
		}
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		if q.Expired(s.today(b)) {
			return &domain.ValidationError{Field: "validUntil", Reason: "the quote expired on " + q.ValidUntil.String() + ": renew it and send the new quote"}
		}
		to, cc := in.To, in.CC
		if len(to) == 0 {
			addr, ccs, err := quoteRecipients(ctx, r, q)
			if err != nil {
				return err
			}
			to = []string{addr}
			if len(cc) == 0 {
				cc = ccs
			}
		}
		qs, err := s.quoteSettings(ctx, r)
		if err != nil {
			return err
		}
		cur, err := r.Currencies.Get(ctx, q.Currency)
		if err != nil {
			return err
		}
		data := domain.QuoteTemplateData(q, cur, b)
		subject, body := in.Subject, in.Body
		if subject == "" {
			subject = qs.EmailSubject
		}
		if body == "" {
			body = qs.EmailBody
		}
		text := domain.RenderTemplate(body, data)
		// The customer's Accept and Decline links (M41), while the quote can still be answered,
		// at the address Settings → Quotes names, else the one it was sent from.
		base := qs.PublicURL
		if base == "" {
			if base, err = domain.ParsePublicURL(in.LinkBase); err != nil {
				return err
			}
		}
		if base != "" && (q.Status == domain.QuoteDraft || q.Status == domain.QuoteSent) {
			token, err := newToken()
			if err != nil {
				return err
			}
			if err := r.Quotes.AddResponseToken(ctx, q.ID, HashToken(token), s.now()); err != nil {
				return err
			}
			text = domain.NewQuoteResponseLinks(base, token).AppendTo(text)
		}
		m, err := domain.NewEmailMessage(s.ids.NewID(), "", to, cc, domain.SubjectLine(subject, data),
			text, in.SendAt, s.now(), in.IdempotencyKey)
		if err != nil {
			return err
		}
		m.Kind, m.QuoteID = domain.EmailKindQuote, q.ID
		if err := r.Emails.Create(ctx, m); err != nil {
			return err
		}
		if err := q.MarkSent(s.now()); err != nil {
			return err
		}
		if err := r.Quotes.Update(ctx, q); err != nil {
			return err
		}
		out = m
		detail := "queued"
		if m.SendAt.After(s.now()) {
			detail = "scheduled for " + m.SendAt.In(b.Location()).Format("2006-01-02 15:04 MST")
		}
		return s.quoteEvent(ctx, r, q.ID, "email_queued", detail+" to "+strings.Join(to, ", "))
	})
	return out, wrap("app.SendQuote", err)
}

// quoteRecipients is who a quote goes to when the request names nobody: the customer and
// their CCs, or the ad-hoc customer.
func quoteRecipients(ctx context.Context, r ports.Repos, q *domain.Quote) (string, []string, error) {
	if q.CustomerID != "" {
		c, err := r.Customers.Get(ctx, q.CustomerID)
		if err != nil {
			return "", nil, err
		}
		return c.Email, c.CC, nil
	}
	a, err := r.AdhocCustomers.Get(ctx, q.AdhocCustomerID)
	if err != nil {
		return "", nil, err
	}
	if a.Email == "" {
		return "", nil, &domain.ValidationError{Field: "to", Reason: "the ad-hoc customer has no e-mail address: add one or type the recipient"}
	}
	return a.Email, nil, nil
}

// deliverQuote sends one quote e-mail (DispatchEmails): the PDF is rendered now, so it
// shows the quote as it is when the e-mail goes out.
func (s *Service) deliverQuote(ctx context.Context, m *domain.EmailMessage) error {
	doc, err := s.quoteDocument(ctx, s.tx.Repos(), m.QuoteID)
	if err != nil {
		return err
	}
	if doc.Quote.Status == domain.QuoteDeclined {
		return errQuoteDeclined // a quote declined after the e-mail was queued is not sent
	}
	pdf, err := s.renderer.RenderQuote(ctx, doc)
	if err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, ports.Mail{
		MessageID: m.ID, To: m.To, CC: m.CC, ReplyTo: doc.Business.Email, Subject: m.Subject, Body: m.Body,
		HTMLBody:    domain.QuoteEmailHTML(m.Body, doc.Business.Name), // "" without response links
		Attachments: []ports.Attachment{{Filename: QuoteFilename(doc.Quote), ContentType: "application/pdf", Data: pdf}},
	}); err != nil {
		return err
	}
	ctx, cancel := outcomeContext(ctx)
	defer cancel()
	if mine, err := s.recordSent(ctx, m.ID); err != nil {
		return &sentButNotRecorded{err}
	} else if !mine {
		return nil
	}
	if err := s.auditNow(ctx, "quote.email_sent", "quote", m.QuoteID, "to "+strings.Join(m.To, ", ")); err != nil {
		return &sentButNotRecorded{err}
	}
	return nil
}

// QuoteSettingsView is Settings → Quotes with the next quote number's sequence value.
type QuoteSettingsView struct {
	Settings     domain.QuoteSettings
	NextSequence int64
}

// quoteSettings returns the saved Quote settings, or the defaults before the first save.
func (s *Service) quoteSettings(ctx context.Context, r ports.Repos) (domain.QuoteSettings, error) {
	qs, err := r.QuoteSettings.Get(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.DefaultQuoteSettings(), nil
	}
	return qs, err
}

// GetQuoteSettings returns Settings → Quotes.
func (s *Service) GetQuoteSettings(ctx context.Context) (QuoteSettingsView, error) {
	r := s.tx.Repos()
	qs, err := s.quoteSettings(ctx, r)
	if err != nil {
		return QuoteSettingsView{}, wrap("app.GetQuoteSettings", err)
	}
	next, err := r.QuoteSequences.Peek(ctx)
	if err != nil {
		return QuoteSettingsView{}, wrap("app.GetQuoteSettings", err)
	}
	return QuoteSettingsView{Settings: qs, NextSequence: next}, nil
}

// UpdateQuoteSettings saves Settings → Quotes. nextSequence (0 = unchanged) may only move
// forward, so a quote number is never given twice.
func (s *Service) UpdateQuoteSettings(ctx context.Context, qs domain.QuoteSettings, nextSequence int64) (QuoteSettingsView, error) {
	if err := qs.Validate(); err != nil {
		return QuoteSettingsView{}, wrap("app.UpdateQuoteSettings", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if nextSequence != 0 {
			if err := r.QuoteSequences.Lock(ctx); err != nil {
				return err
			}
		}
		if err := r.QuoteSettings.Save(ctx, qs); err != nil {
			return err
		}
		if err := s.audit(ctx, r, "settings.quotes_updated", "settings", "quotes", quoteSettingsSummary(qs)); err != nil {
			return err
		}
		if nextSequence == 0 {
			return nil
		}
		cur, err := r.QuoteSequences.Peek(ctx)
		if err != nil {
			return err
		}
		if nextSequence < cur {
			return &domain.ValidationError{Field: "nextSequence", Reason: "may only increase (numbers already used)"}
		}
		if err := r.QuoteSequences.Set(ctx, nextSequence); err != nil {
			return err
		}
		return s.audit(ctx, r, "settings.next_quote_number_set", "settings", "quotes", "next quote sequence "+strconv.FormatInt(nextSequence, 10))
	})
	if err != nil {
		return QuoteSettingsView{}, wrap("app.UpdateQuoteSettings", err)
	}
	return s.GetQuoteSettings(ctx)
}

func quoteSettingsSummary(qs domain.QuoteSettings) string {
	return "valid " + strconv.Itoa(qs.ValidDays) + " days, numbers " + qs.NumberFormat
}
