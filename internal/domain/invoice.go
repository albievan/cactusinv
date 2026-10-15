package domain

import (
	"strings"
	"time"
)

// InvoiceStatus is a state in the invoice lifecycle (docs/ARCHITECTURE.md §3.3).
type InvoiceStatus string

// Invoice statuses.
const (
	InvoiceDraft  InvoiceStatus = "draft"
	InvoiceIssued InvoiceStatus = "issued"
	InvoiceSent   InvoiceStatus = "sent"
	InvoicePaid   InvoiceStatus = "paid"
	InvoiceVoid   InvoiceStatus = "void"
)

// MaxInvoiceLines bounds the number of lines on one invoice.
const MaxInvoiceLines = 200

// Line is one invoice line. Description and price are snapshots, never live references.
type Line struct {
	ProductID   string
	Description string
	Quantity    Quantity
	UnitPrice   int64 // minor units of the invoice currency
	Discount    BasisPoints
}

// Gross is quantity × unit price, rounded half away from zero.
func (l Line) Gross() int64 { return MulDivRound(int64(l.Quantity), l.UnitPrice, QuantityScale) }

// DiscountAmount is the line discount in minor units.
func (l Line) DiscountAmount() int64 { return MulDivRound(l.Gross(), int64(l.Discount), 10000) }

// Amount is the line total after discount.
func (l Line) Amount() int64 { return l.Gross() - l.DiscountAmount() }

func (l *Line) validate() error {
	l.Description = strings.TrimSpace(l.Description)
	l.ProductID = strings.TrimSpace(l.ProductID)
	if l.Description == "" || len(l.Description) > 2000 {
		return invalid("lines.description", "is required (max 2000 characters)")
	}
	if l.Quantity <= 0 || l.Quantity > 1_000_000_000 {
		return invalid("lines.quantity", "must be greater than 0 and at most 1 000 000")
	}
	if l.UnitPrice < 0 || l.UnitPrice > 1_000_000_000_000 {
		return invalid("lines.unitPrice", "must be between 0 and 10 000 000 000")
	}
	if l.Discount < 0 || l.Discount > 10000 {
		return invalid("lines.discount", "must be between 0 and 100 percent")
	}
	return nil
}

// Totals are an invoice's computed sums in minor units (docs/ARCHITECTURE.md §3.4).
type Totals struct {
	Subtotal int64
	Discount int64
	Tax      int64
	Total    int64
}

// ComputeTotals sums lines and applies the tax rate.
func ComputeTotals(lines []Line, taxRate BasisPoints) Totals {
	var t Totals
	for _, l := range lines {
		t.Subtotal += l.Gross()
		t.Discount += l.DiscountAmount()
	}
	t.Tax = MulDivRound(t.Subtotal-t.Discount, int64(taxRate), 10000)
	t.Total = t.Subtotal - t.Discount + t.Tax
	return t
}

// Party is a printed name/address block (bill-to or ship-to).
type Party struct {
	Name      string
	Address   string
	Attention string
}

// InvoiceHeader holds the optional boxes printed under the addresses on the invoice.
type InvoiceHeader struct {
	SalesPerson    string
	Job            string
	ShippingMethod string
	ShippingTerms  string
	DeliveryDate   Date
}

// Invoice is the invoice aggregate.
type Invoice struct {
	ID         string
	Number     string // empty until issued
	Status     InvoiceStatus
	CustomerID string
	Currency   CurrencyCode
	IssueDate  Date // intended date while draft; actual date once issued
	DueDate    Date // computed at issue
	Terms      PaymentTerms
	TermsText  string
	Header     InvoiceHeader
	BillTo     Party
	ShipTo     Party
	Notes      string
	TaxRate    BasisPoints
	Lines      []Line
	Totals     Totals
	Bank       BankDetails // frozen at issue
	ScheduleID string
	IssuedAt   time.Time
	SentAt     time.Time
	PaidAt     time.Time
	VoidedAt   time.Time
	VoidReason string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int
}

// DraftContent is everything a user can edit on a draft.
type DraftContent struct {
	IssueDate Date
	Header    InvoiceHeader
	BillTo    Party
	ShipTo    Party
	Notes     string
	TaxRate   BasisPoints
	Terms     PaymentTerms
	Lines     []Line
}

// NewDraftInvoice creates a draft for customer c, in c's currency.
func NewDraftInvoice(id string, c Customer, content DraftContent, now time.Time) (*Invoice, error) {
	if !c.Active {
		return nil, ErrInactive
	}
	inv := &Invoice{
		ID: id, Status: InvoiceDraft, CustomerID: c.ID, Currency: c.Currency,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := inv.applyContent(content); err != nil {
		return nil, err
	}
	return inv, nil
}

// DefaultDraftContent pre-fills addresses, terms and notes from the customer.
func DefaultDraftContent(c Customer, issue Date) DraftContent {
	shipName := c.ShippingName
	if shipName == "" && c.ShippingAddress != "" {
		shipName = c.Name
	}
	return DraftContent{
		IssueDate: issue,
		BillTo:    Party{Name: c.Name, Address: c.BillingAddress, Attention: c.Attention},
		ShipTo:    Party{Name: shipName, Address: c.ShippingAddress},
		Notes:     c.DefaultNotes,
		Terms:     c.Terms,
	}
}

// UpdateDraft replaces a draft's editable content.
func (inv *Invoice) UpdateDraft(content DraftContent, now time.Time) error {
	if inv.Status != InvoiceDraft {
		return ErrInvalidTransition
	}
	if err := inv.applyContent(content); err != nil {
		return err
	}
	inv.UpdatedAt = now
	return nil
}

func (inv *Invoice) applyContent(c DraftContent) error {
	if len(c.Lines) == 0 {
		return invalid("lines", "at least one line is required")
	}
	if len(c.Lines) > MaxInvoiceLines {
		return invalid("lines", "too many lines")
	}
	lines := make([]Line, len(c.Lines))
	copy(lines, c.Lines)
	for i := range lines {
		if err := lines[i].validate(); err != nil {
			return err
		}
	}
	if c.TaxRate < 0 || c.TaxRate > 10000 {
		return invalid("vatPercent", "must be between 0 and 100 percent")
	}
	terms, err := NewPaymentTerms(c.Terms.Kind, c.Terms.Value)
	if err != nil {
		return err
	}
	c.BillTo.Name = strings.TrimSpace(c.BillTo.Name)
	if c.BillTo.Name == "" {
		return invalid("billTo.name", "is required")
	}
	if len(c.Notes) > 4000 {
		return invalid("notes", "max 4000 characters")
	}
	inv.IssueDate = c.IssueDate
	inv.Header = c.Header
	inv.BillTo = c.BillTo
	inv.ShipTo = c.ShipTo
	inv.Notes = strings.TrimSpace(c.Notes)
	inv.TaxRate = c.TaxRate
	inv.Terms = terms
	inv.Lines = lines
	inv.Totals = ComputeTotals(lines, c.TaxRate)
	return nil
}

// Issue freezes a draft: assigns the number and issue date, computes the due date and
// snapshots the bank details.
func (inv *Invoice) Issue(number string, issueDate Date, bank BankDetails, now time.Time) error {
	if inv.Status != InvoiceDraft {
		return ErrInvalidTransition
	}
	if strings.TrimSpace(number) == "" {
		return invalid("number", "is required")
	}
	if issueDate.IsZero() {
		return invalid("issueDate", "is required")
	}
	inv.Number = number
	inv.IssueDate = issueDate
	inv.DueDate = inv.Terms.DueDate(issueDate)
	inv.TermsText = inv.Terms.Text()
	inv.Bank = bank
	inv.Status = InvoiceIssued
	inv.IssuedAt = now
	inv.UpdatedAt = now
	return nil
}

// CanSend reports whether the invoice may be e-mailed.
func (inv *Invoice) CanSend() bool {
	return inv.Status == InvoiceIssued || inv.Status == InvoiceSent || inv.Status == InvoicePaid
}

// MarkSent records a delivered e-mail.
func (inv *Invoice) MarkSent(now time.Time) error {
	switch inv.Status {
	case InvoiceIssued:
		inv.Status = InvoiceSent
	case InvoiceSent, InvoicePaid:
	default:
		return ErrInvalidTransition
	}
	if inv.SentAt.IsZero() {
		inv.SentAt = now
	}
	inv.UpdatedAt = now
	return nil
}

// MarkPaid records payment.
func (inv *Invoice) MarkPaid(now time.Time) error {
	if inv.Status != InvoiceIssued && inv.Status != InvoiceSent {
		return ErrInvalidTransition
	}
	inv.Status = InvoicePaid
	inv.PaidAt = now
	inv.UpdatedAt = now
	return nil
}

// Void cancels an issued invoice. Its number stays used (gap-free numbering).
func (inv *Invoice) Void(reason string, now time.Time) error {
	if inv.Status != InvoiceIssued && inv.Status != InvoiceSent {
		return ErrInvalidTransition
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return invalid("reason", "is required (max 500 characters)")
	}
	inv.Status = InvoiceVoid
	inv.VoidReason = reason
	inv.VoidedAt = now
	inv.UpdatedAt = now
	return nil
}

// CanDelete reports whether the invoice may be deleted (drafts only).
func (inv *Invoice) CanDelete() bool { return inv.Status == InvoiceDraft }

// IsOverdue reports whether an unpaid, live invoice is past its due date on day today.
func (inv *Invoice) IsOverdue(today Date) bool {
	return (inv.Status == InvoiceIssued || inv.Status == InvoiceSent) && inv.DueDate.Before(today)
}

// InvoiceEvent is an audit-trail entry for an invoice.
type InvoiceEvent struct {
	ID        string
	InvoiceID string
	Type      string
	Detail    string
	At        time.Time
}
