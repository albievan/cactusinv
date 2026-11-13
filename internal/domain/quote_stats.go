package domain

import "sort"

// QuoteStats summarises the quotes in one currency for the dashboard (M39). Amounts are
// quote totals in minor units of Currency; currencies are never added together (ADR-003).
type QuoteStats struct {
	Currency CurrencyCode
	// Open quotes are drafts and sent quotes still valid today; OpenValue is their total.
	Open      int
	OpenValue int64
	// Accepted quotes are waiting to be turned into an invoice.
	Accepted      int
	AcceptedValue int64
	Declined      int
	Converted     int
	Expired       int // drafts and sent quotes past their validity
	// ExpiringSoon counts open quotes whose validity ends within the soon window.
	ExpiringSoon int
	// IssuedThisMonth counts quotes dated in today's month, ThisMonthValue their total.
	IssuedThisMonth int
	ThisMonthValue  int64
}

// Won is the number of quotes the customer said yes to (accepted, or already invoiced).
func (s QuoteStats) Won() int { return s.Accepted + s.Converted }

// Decided is the number of quotes with an answer, won or declined; with Won it gives the
// win rate. Expired quotes are not counted: nobody answered them.
func (s QuoteStats) Decided() int { return s.Won() + s.Declined }

// SummarizeQuotes counts and totals quotes per currency as of today, sorted by currency.
// An open quote is expiring soon when its validity ends within soonDays of today.
func SummarizeQuotes(qs []Quote, today Date, soonDays int) []QuoteStats {
	by := map[CurrencyCode]*QuoteStats{}
	soon := today.AddDays(soonDays)
	for i := range qs {
		q := &qs[i]
		s := by[q.Currency]
		if s == nil {
			s = &QuoteStats{Currency: q.Currency}
			by[q.Currency] = s
		}
		if q.IssueDate.Year == today.Year && q.IssueDate.Month == today.Month {
			s.IssuedThisMonth++
			s.ThisMonthValue += q.Totals.Total
		}
		switch q.Stage(today) {
		case QuoteStageDraft, QuoteStageSent:
			s.Open++
			s.OpenValue += q.Totals.Total
			if !q.ValidUntil.After(soon) {
				s.ExpiringSoon++
			}
		case QuoteStageAccepted:
			s.Accepted++
			s.AcceptedValue += q.Totals.Total
		case QuoteStageDeclined:
			s.Declined++
		case QuoteStageConverted:
			s.Converted++
		case QuoteStageExpired:
			s.Expired++
		}
	}
	out := make([]QuoteStats, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// ExpiringQuotes returns the open quotes whose validity ends within soonDays of today
// (today included), the soonest first and at most limit of them.
func ExpiringQuotes(qs []Quote, today Date, soonDays, limit int) []Quote {
	soon := today.AddDays(soonDays)
	var out []Quote
	for i := range qs {
		q := qs[i]
		if (q.Status == QuoteDraft || q.Status == QuoteSent) && !q.Expired(today) && !q.ValidUntil.After(soon) {
			out = append(out, q)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ValidUntil != out[j].ValidUntil {
			return out[i].ValidUntil.Before(out[j].ValidUntil)
		}
		return out[i].Number < out[j].Number
	})
	return out[:min(limit, len(out))]
}

// DueSoon returns the unpaid issued invoices due between today and soonDays from today,
// the soonest first and at most limit of them (overdue ones are counted elsewhere).
func DueSoon(invs []Invoice, today Date, soonDays, limit int) []Invoice {
	soon := today.AddDays(soonDays)
	var out []Invoice
	for i := range invs {
		inv := invs[i]
		if (inv.Status == InvoiceIssued || inv.Status == InvoiceSent) && !inv.DueDate.Before(today) && !inv.DueDate.After(soon) {
			out = append(out, inv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DueDate != out[j].DueDate {
			return out[i].DueDate.Before(out[j].DueDate)
		}
		return out[i].Number < out[j].Number
	})
	return out[:min(limit, len(out))]
}
