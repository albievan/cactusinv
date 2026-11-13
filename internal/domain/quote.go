package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

// QuoteStatus is a state in a quote's life (docs/ARCHITECTURE.md §3.8).
type QuoteStatus string

// Quote statuses.
const (
	QuoteDraft     QuoteStatus = "draft"
	QuoteSent      QuoteStatus = "sent" // e-mailed (or queued to be)
	QuoteAccepted  QuoteStatus = "accepted"
	QuoteDeclined  QuoteStatus = "declined"
	QuoteConverted QuoteStatus = "converted" // an invoice was made from it
)

// Quote is an offer to a customer or an ad-hoc customer, priced from the products like an
// invoice. Unlike an invoice it is not a tax document: it is numbered when created, may be
// revised until it is accepted, and becomes an invoice only by ConvertQuote.
type Quote struct {
	ID     string
	Number string
	Status QuoteStatus
	// CustomerID is the customer the quote is for; AdhocCustomerID the ad-hoc customer it
	// was made out to. A quote for an ad-hoc customer gets the CustomerID when that ad-hoc
	// customer is made a customer.
	CustomerID      string
	AdhocCustomerID string
	Currency        CurrencyCode
	IssueDate       Date
	ValidUntil      Date
	SalesPerson     string
	Reference       string
	BillTo          Party
	Notes           string
	Terms           string
	TaxRate         BasisPoints
	Lines           []Line
	Totals          Totals
	InvoiceID       string // the invoice made from it (converted)
	SentAt          time.Time
	AcceptedAt      time.Time
	DeclinedAt      time.Time
	DeclineReason   string
	// AnsweredBy is who recorded the acceptance or decline (M41): staff in the app or the
	// customer through the link in the quote e-mail; "" while unanswered.
	AnsweredBy QuoteAnswerer
	// RenewedFrom is the expired quote this one was made from (M41), "" for a new quote.
	RenewedFrom string
	ConvertedAt time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

// QuoteContent is everything a user can edit on a quote.
type QuoteContent struct {
	IssueDate   Date
	ValidUntil  Date
	SalesPerson string
	Reference   string
	BillTo      Party
	Notes       string
	Terms       string
	TaxRate     BasisPoints
	Lines       []Line
}

// QuoteFor names who a quote is for: a customer or an ad-hoc customer, and the currency
// (theirs).
type QuoteFor struct {
	CustomerID      string
	AdhocCustomerID string
	Currency        CurrencyCode
}

// NewQuote creates a draft quote with its number.
func NewQuote(id, number string, to QuoteFor, content QuoteContent, now time.Time) (*Quote, error) {
	if (to.CustomerID == "") == (to.AdhocCustomerID == "") {
		return nil, invalid("customerId", "choose a customer or an ad-hoc customer")
	}
	if strings.TrimSpace(number) == "" {
		return nil, invalid("number", "is required")
	}
	if _, err := ParseCurrencyCode(string(to.Currency)); err != nil {
		return nil, err
	}
	q := &Quote{
		ID: id, Number: number, Status: QuoteDraft, CustomerID: to.CustomerID, AdhocCustomerID: to.AdhocCustomerID,
		Currency: to.Currency, CreatedAt: now, UpdatedAt: now,
	}
	if err := q.apply(content); err != nil {
		return nil, err
	}
	return q, nil
}

// Editable reports whether the quote's content may still change: until it is accepted,
// declined or converted.
func (q *Quote) Editable() bool { return q.Status == QuoteDraft || q.Status == QuoteSent }

// Update replaces the quote's content. A quote that was sent goes back to draft: the
// customer has not seen this version.
func (q *Quote) Update(content QuoteContent, now time.Time) error {
	if !q.Editable() {
		return ErrInvalidTransition
	}
	if err := q.apply(content); err != nil {
		return err
	}
	q.Status = QuoteDraft
	q.UpdatedAt = now
	return nil
}

func (q *Quote) apply(c QuoteContent) error {
	if len(c.Lines) == 0 {
		return invalid("lines", "at least one line is required")
	}
	if len(c.Lines) > MaxInvoiceLines {
		return invalid("lines", "too many lines")
	}
	lines := slices.Clone(c.Lines)
	for i := range lines {
		if err := lines[i].validate(); err != nil {
			return err
		}
	}
	if c.TaxRate < 0 || c.TaxRate > 10000 {
		return invalid("vatPercent", "must be between 0 and 100 percent")
	}
	if c.IssueDate.IsZero() {
		return invalid("issueDate", "is required")
	}
	if c.ValidUntil.IsZero() || c.ValidUntil.Before(c.IssueDate) {
		return invalid("validUntil", "must be on or after the quote date")
	}
	if c.ValidUntil.After(c.IssueDate.AddDays(MaxQuoteValidDays)) {
		return invalid("validUntil", "may be at most a year after the quote date")
	}
	c.BillTo.Name = strings.TrimSpace(c.BillTo.Name)
	if c.BillTo.Name == "" || len(c.BillTo.Name) > 200 {
		return invalid("billTo.name", "is required (max 200 characters)")
	}
	if len(c.BillTo.Address) > 1000 || len(c.BillTo.Attention) > 200 {
		return invalid("billTo", "address max 1000, attention max 200 characters")
	}
	c.SalesPerson, c.Reference = strings.TrimSpace(c.SalesPerson), strings.TrimSpace(c.Reference)
	if len(c.SalesPerson) > 120 || len(c.Reference) > 120 {
		return invalid("reference", "sales person and reference max 120 characters")
	}
	notes, err := CleanNotes("notes", strings.TrimSpace(c.Notes))
	if err != nil {
		return err
	}
	terms := strings.TrimSpace(c.Terms)
	if len(terms) > 4000 {
		return invalid("terms", "max 4000 characters")
	}
	if err := checkTotalsFit(lines, c.TaxRate); err != nil {
		return err
	}
	q.IssueDate, q.ValidUntil = c.IssueDate, c.ValidUntil
	q.SalesPerson, q.Reference = c.SalesPerson, c.Reference
	q.BillTo, q.Notes, q.Terms, q.TaxRate = c.BillTo, notes, terms, c.TaxRate
	q.Lines = lines
	q.Totals = ComputeTotals(lines, c.TaxRate)
	return nil
}

// Content is the quote's editable content (to start a revision from).
func (q *Quote) Content() QuoteContent {
	return QuoteContent{
		IssueDate: q.IssueDate, ValidUntil: q.ValidUntil, SalesPerson: q.SalesPerson, Reference: q.Reference,
		BillTo: q.BillTo, Notes: q.Notes, Terms: q.Terms, TaxRate: q.TaxRate, Lines: slices.Clone(q.Lines),
	}
}

// CanSend reports whether the quote may be e-mailed: a draft, or again once sent or
// accepted.
func (q *Quote) CanSend() bool {
	return q.Status == QuoteDraft || q.Status == QuoteSent || q.Status == QuoteAccepted
}

// MarkSent records that the quote was e-mailed (queued). An accepted quote stays accepted.
func (q *Quote) MarkSent(now time.Time) error {
	if !q.CanSend() {
		return ErrInvalidTransition
	}
	if q.Status == QuoteDraft {
		q.Status = QuoteSent
	}
	if q.SentAt.IsZero() {
		q.SentAt = now
	}
	q.UpdatedAt = now
	return nil
}

// QuoteAnswerer is who recorded a quote's acceptance or decline.
type QuoteAnswerer string

// Who answered a quote.
const (
	AnsweredByStaff    QuoteAnswerer = "staff"
	AnsweredByCustomer QuoteAnswerer = "customer"
)

// quoteExpired refuses to accept or convert a quote past its validity (M41): it must be
// renewed (RenewalContent) and sent again.
func quoteExpired(q *Quote) error {
	return invalid("validUntil", "the quote expired on "+q.ValidUntil.String()+": renew it and send the new quote")
}

// Accept records that the customer accepted the quote, as recorded by by. A quote past its
// validity cannot be accepted (M41).
func (q *Quote) Accept(today Date, by QuoteAnswerer, now time.Time) error {
	if q.Status != QuoteDraft && q.Status != QuoteSent {
		return ErrInvalidTransition
	}
	if q.Expired(today) {
		return quoteExpired(q)
	}
	q.Status = QuoteAccepted
	q.AcceptedAt = now
	q.AnsweredBy = by
	q.UpdatedAt = now
	return nil
}

// Decline records that the customer turned the quote down, as recorded by by.
func (q *Quote) Decline(reason string, by QuoteAnswerer, now time.Time) error {
	if q.Status != QuoteDraft && q.Status != QuoteSent && q.Status != QuoteAccepted {
		return ErrInvalidTransition
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		return invalid("reason", "max 500 characters")
	}
	q.Status = QuoteDeclined
	q.DeclineReason = reason
	q.DeclinedAt = now
	q.AnsweredBy = by
	q.UpdatedAt = now
	return nil
}

// CanConvert reports whether an invoice may be made from the quote today: it must be for a
// customer (an ad-hoc customer is made a customer first), not declined or converted, and
// not expired (M41; an accepted quote was answered in time and stays convertible).
func (q *Quote) CanConvert(today Date) error {
	if q.Status != QuoteDraft && q.Status != QuoteSent && q.Status != QuoteAccepted {
		return ErrInvalidTransition
	}
	if q.Expired(today) {
		return quoteExpired(q)
	}
	if q.CustomerID == "" {
		return invalid("customerId", "the quote is for an ad-hoc customer: make them a customer first")
	}
	return nil
}

// InvoiceContent is the draft invoice the quote becomes for customer c on date issue: the
// quote's lines, prices, VAT, notes, sales person and reference, addressed as the
// customer's invoices are.
func (q *Quote) InvoiceContent(c Customer, issue Date) (DraftContent, error) {
	if err := q.CanConvert(issue); err != nil {
		return DraftContent{}, err
	}
	if c.ID != q.CustomerID {
		return DraftContent{}, invalid("customerId", "is not the quote's customer")
	}
	if c.Currency != q.Currency {
		return DraftContent{}, invalid("currency", "the quote is in "+string(q.Currency)+" but the customer is billed in "+string(c.Currency))
	}
	content := DefaultDraftContent(c, issue)
	content.Header = InvoiceHeader{SalesPerson: q.SalesPerson, Job: q.Reference}
	content.Notes = q.Notes
	content.TaxRate = q.TaxRate
	content.Lines = slices.Clone(q.Lines)
	return content, nil
}

// Convert records the invoice made from the quote on today.
func (q *Quote) Convert(invoiceID string, today Date, now time.Time) error {
	if err := q.CanConvert(today); err != nil {
		return err
	}
	q.Status = QuoteConverted
	q.InvoiceID = invoiceID
	q.ConvertedAt = now
	q.UpdatedAt = now
	return nil
}

// InvoiceDeleted puts a converted quote back to accepted when the draft invoice made from
// it was deleted, so it can be converted again.
func (q *Quote) InvoiceDeleted(now time.Time) error {
	if q.Status != QuoteConverted {
		return ErrInvalidTransition
	}
	q.Status = QuoteAccepted
	q.InvoiceID = ""
	q.ConvertedAt = time.Time{}
	q.UpdatedAt = now
	return nil
}

// CustomerBecame records that the quote's ad-hoc customer was made customer c.
func (q *Quote) CustomerBecame(customerID string, now time.Time) {
	q.CustomerID = customerID
	q.UpdatedAt = now
}

// CanDelete reports whether the quote may be deleted (drafts only; one that was sent is
// declined instead, so its history stays).
func (q *Quote) CanDelete() bool { return q.Status == QuoteDraft && q.SentAt.IsZero() }

// QuoteStage is where a quote is in its life, as shown to the user.
type QuoteStage string

// Quote stages: the statuses, plus expired for a draft or sent quote past its date.
const (
	QuoteStageDraft     QuoteStage = "draft"
	QuoteStageSent      QuoteStage = "sent"
	QuoteStageAccepted  QuoteStage = "accepted"
	QuoteStageDeclined  QuoteStage = "declined"
	QuoteStageConverted QuoteStage = "converted"
	QuoteStageExpired   QuoteStage = "expired"
)

// Expired reports whether an open quote's validity ended before today.
func (q *Quote) Expired(today Date) bool {
	return (q.Status == QuoteDraft || q.Status == QuoteSent) && q.ValidUntil.Before(today)
}

// Stage summarises the status and validity for display.
func (q *Quote) Stage(today Date) QuoteStage {
	if q.Expired(today) {
		return QuoteStageExpired
	}
	return QuoteStage(q.Status)
}

// Fingerprint is a short code for the quote's printed content: number, customer,
// currency, dates, lines, totals, notes and terms. It is printed on the PDF and shown in
// the app, so a PDF that comes back can be checked against the quote it claims to be: any
// change to what was quoted gives another code.
func (q *Quote) Fingerprint() string {
	h := sha256.New()
	field := func(s string) { fmt.Fprintf(h, "%d:%s;", len(s), s) }
	for _, s := range []string{
		q.Number, string(q.Currency), q.IssueDate.String(), q.ValidUntil.String(), q.BillTo.Name, q.BillTo.Address,
		q.BillTo.Attention, q.SalesPerson, q.Reference, q.Notes, q.Terms, q.TaxRate.String(),
	} {
		field(s)
	}
	for _, l := range q.Lines {
		field(l.Description)
		fmt.Fprintf(h, "%d|%d|%d|%d;", l.Quantity, l.UnitPrice, l.Discount, l.Amount())
	}
	fmt.Fprintf(h, "%d|%d|%d|%d", q.Totals.Subtotal, q.Totals.Discount, q.Totals.Tax, q.Totals.Total)
	sum := strings.ToUpper(hex.EncodeToString(h.Sum(nil))[:12])
	return sum[:4] + "-" + sum[4:8] + "-" + sum[8:]
}

// QuoteTemplateData builds the e-mail placeholder values for a quote.
func QuoteTemplateData(q *Quote, cur Currency, business BusinessProfile) TemplateData {
	sym := cur.Symbol
	if sym == "" {
		sym = string(cur.Code)
	}
	return TemplateData{
		QuoteNumber:  q.Number,
		CustomerName: q.BillTo.Name,
		Attention:    q.BillTo.Attention,
		Total:        sym + " " + FormatGrouped(q.Totals.Total, cur.MinorUnits, ","),
		ValidUntil:   q.ValidUntil.Long(),
		IssueDate:    q.IssueDate.Long(),
		BusinessName: business.Name,
	}
}

// RenewalContent is the content of a new quote made from this expired (or declined) one
// (M41): the same lines, prices, VAT, people, notes and terms, issued on issue and valid
// for validDays. The new quote gets its own number and is sent as any other.
func (q *Quote) RenewalContent(issue Date, validDays int, today Date) (QuoteContent, error) {
	if !q.Expired(today) && q.Status != QuoteDeclined {
		return QuoteContent{}, invalid("status", "only an expired or declined quote is renewed; this one can still be answered")
	}
	c := q.Content()
	c.IssueDate = issue
	c.ValidUntil = issue.AddDays(validDays)
	c.Lines = slices.Clone(q.Lines)
	return c, nil
}
