package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// MaxPaymentReferenceLength is the longest reference a payment may carry.
const MaxPaymentReferenceLength = 200

// Payment is money received against an issued invoice (M58), in the invoice's currency.
// An invoice is paid when its payments cover its total.
type Payment struct {
	ID         string
	InvoiceID  string
	Amount     int64 // minor units of the invoice's currency
	ReceivedOn Date
	Reference  string // the customer's reference or a note, e.g. "EFT 4411"
	CreatedAt  time.Time
}

// Balance is what is still owed on the invoice: its total less the payments recorded.
func (inv *Invoice) Balance() int64 { return inv.Totals.Total - inv.AmountPaid }

// RecordPayment adds p to an open invoice: 0 < amount ≤ balance, received on a date. When
// the payments cover the total the invoice is paid, as of now. p is cleaned in place.
func (inv *Invoice) RecordPayment(p *Payment, now time.Time) error {
	if !inv.IsOpen() {
		return ErrInvalidTransition
	}
	if p.Amount <= 0 {
		return invalid("amount", "must be more than 0")
	}
	if p.Amount > inv.Balance() {
		return invalid("amount", "is more than the balance still owed")
	}
	if p.ReceivedOn.IsZero() {
		return invalid("receivedOn", "is required")
	}
	p.Reference = strings.TrimSpace(p.Reference)
	if utf8.RuneCountInString(p.Reference) > MaxPaymentReferenceLength {
		return invalid("reference", "max 200 characters")
	}
	p.InvoiceID, p.CreatedAt = inv.ID, now
	inv.AmountPaid += p.Amount
	if inv.Balance() == 0 {
		inv.Status, inv.PaidAt = InvoicePaid, now
	}
	inv.UpdatedAt = now
	return nil
}

// RemovePayment takes a recorded payment off the invoice (entered by mistake, or a payment
// that bounced). A paid invoice opens again as it was: overdue if it was marked overdue,
// else sent if it was e-mailed, else issued (the overdue check marks it when it falls due).
func (inv *Invoice) RemovePayment(p Payment, now time.Time) error {
	if (!inv.IsOpen() && inv.Status != InvoicePaid) || p.InvoiceID != inv.ID || p.Amount <= 0 || p.Amount > inv.AmountPaid {
		return ErrInvalidTransition
	}
	inv.AmountPaid -= p.Amount
	if inv.Status == InvoicePaid {
		inv.Status, inv.PaidAt = InvoiceIssued, time.Time{}
		switch {
		case !inv.OverdueAt.IsZero():
			inv.Status = InvoiceOverdue
		case !inv.SentAt.IsZero():
			inv.Status = InvoiceSent
		}
	}
	inv.UpdatedAt = now
	return nil
}
