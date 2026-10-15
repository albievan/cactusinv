package domain

import (
	"fmt"
	"strconv"
)

// TermsKind selects how an invoice's due date is computed.
type TermsKind string

// Payment terms kinds.
const (
	TermsOnReceipt      TermsKind = "on_receipt"
	TermsDaysAfter      TermsKind = "days_after"
	TermsDayOfNextMonth TermsKind = "day_of_next_month"
)

// PaymentTerms decide an invoice's due date and the sentence printed on it.
type PaymentTerms struct {
	Kind  TermsKind
	Value int
}

// NewPaymentTerms validates payment terms.
func NewPaymentTerms(kind TermsKind, value int) (PaymentTerms, error) {
	switch kind {
	case TermsOnReceipt:
		return PaymentTerms{Kind: kind}, nil
	case TermsDaysAfter:
		if value < 0 || value > 365 {
			return PaymentTerms{}, invalid("paymentTerms.value", "days must be between 0 and 365")
		}
	case TermsDayOfNextMonth:
		if value < 1 || value > 31 {
			return PaymentTerms{}, invalid("paymentTerms.value", "day must be between 1 and 31")
		}
	default:
		return PaymentTerms{}, invalid("paymentTerms.kind", "must be on_receipt, days_after or day_of_next_month")
	}
	return PaymentTerms{Kind: kind, Value: value}, nil
}

// DueDate computes the due date for an invoice issued on issue.
func (t PaymentTerms) DueDate(issue Date) Date {
	switch t.Kind {
	case TermsDaysAfter:
		return issue.AddDays(t.Value)
	case TermsDayOfNextMonth:
		return clampedDate(issue.Year, issue.Month+1, t.Value)
	default:
		return issue
	}
}

// Text is the sentence printed in the invoice's "Payment Terms" box.
func (t PaymentTerms) Text() string {
	switch t.Kind {
	case TermsDaysAfter:
		if t.Value == 0 {
			return "Payment due on receipt"
		}
		return fmt.Sprintf("Payment due within %d days", t.Value)
	case TermsDayOfNextMonth:
		return fmt.Sprintf("Payment due before %s of the next month", Ordinal(t.Value))
	default:
		return "Payment due on receipt"
	}
}

// Ordinal renders 1 → "1st", 2 → "2nd", 11 → "11th", 23 → "23rd".
func Ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}
