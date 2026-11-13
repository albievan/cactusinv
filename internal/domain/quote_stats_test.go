package domain

import (
	"reflect"
	"testing"
)

func statQuote(num string, cur CurrencyCode, st QuoteStatus, issue, valid Date, total int64) Quote {
	return Quote{Number: num, Currency: cur, Status: st, IssueDate: issue, ValidUntil: valid, Totals: Totals{Total: total}}
}

func TestSummarizeQuotes(t *testing.T) {
	today := NewDate(2026, 11, 13)
	old := NewDate(2026, 10, 1)
	qs := []Quote{
		statQuote("Q1", "ZAR", QuoteDraft, today, today.AddDays(30), 1000),             // open, this month
		statQuote("Q2", "ZAR", QuoteSent, old, today.AddDays(3), 2000),                 // open, expiring soon
		statQuote("Q3", "ZAR", QuoteSent, old, today, 500),                             // open, last valid day: soon
		statQuote("Q4", "ZAR", QuoteSent, old, today.AddDays(-1), 9000),                // expired
		statQuote("Q5", "ZAR", QuoteAccepted, NewDate(2026, 11, 2), old, 3000),         // accepted (past validity is fine)
		statQuote("Q6", "ZAR", QuoteDeclined, old, old, 7000),                          // declined
		statQuote("Q7", "ZAR", QuoteConverted, old, old, 4000),                         // converted
		statQuote("U1", "USD", QuoteSent, NewDate(2025, 11, 13), today.AddDays(8), 50), // open, not soon; same month last year
	}
	got := SummarizeQuotes(qs, today, 7)
	want := []QuoteStats{
		{Currency: "USD", Open: 1, OpenValue: 50},
		{
			Currency: "ZAR", Open: 3, OpenValue: 3500, ExpiringSoon: 2, Expired: 1, Accepted: 1, AcceptedValue: 3000,
			Declined: 1, Converted: 1, IssuedThisMonth: 2, ThisMonthValue: 4000,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if z := got[1]; z.Won() != 2 || z.Decided() != 3 {
		t.Fatalf("won %d of %d", z.Won(), z.Decided())
	}
	if len(SummarizeQuotes(nil, today, 7)) != 0 {
		t.Fatal("stats without quotes")
	}
}

func TestExpiringQuotes(t *testing.T) {
	today := NewDate(2026, 11, 13)
	qs := []Quote{
		statQuote("Q9", "ZAR", QuoteSent, today, today.AddDays(5), 1),
		statQuote("Q8", "ZAR", QuoteDraft, today, today.AddDays(5), 1),
		statQuote("Q7", "ZAR", QuoteSent, today, today, 1),
		statQuote("Q6", "ZAR", QuoteSent, today, today.AddDays(8), 1),     // outside the window
		statQuote("Q5", "ZAR", QuoteSent, today, today.AddDays(-1), 1),    // already expired
		statQuote("Q4", "ZAR", QuoteAccepted, today, today.AddDays(1), 1), // answered
	}
	nums := func(qs []Quote) (out []string) {
		for _, q := range qs {
			out = append(out, q.Number)
		}
		return out
	}
	if got := nums(ExpiringQuotes(qs, today, 7, 5)); !reflect.DeepEqual(got, []string{"Q7", "Q8", "Q9"}) {
		t.Fatalf("expiring = %v", got)
	}
	if got := nums(ExpiringQuotes(qs, today, 7, 2)); !reflect.DeepEqual(got, []string{"Q7", "Q8"}) {
		t.Fatalf("limited = %v", got)
	}
}

func TestDueSoon(t *testing.T) {
	today := NewDate(2026, 11, 13)
	inv := func(num string, st InvoiceStatus, due Date) Invoice {
		return Invoice{Number: num, Status: st, DueDate: due}
	}
	invs := []Invoice{
		inv("I5", InvoiceSent, today.AddDays(7)),
		inv("I4", InvoiceIssued, today),
		inv("I3", InvoiceIssued, today.AddDays(8)),  // later
		inv("I2", InvoiceIssued, today.AddDays(-1)), // overdue
		inv("I1", InvoicePaid, today.AddDays(1)),    // paid
		inv("", InvoiceDraft, Date{}),               // draft
		inv("I0", InvoiceSent, today.AddDays(7)),
	}
	var got []string
	for _, i := range DueSoon(invs, today, 7, 5) {
		got = append(got, i.Number)
	}
	if !reflect.DeepEqual(got, []string{"I4", "I0", "I5"}) {
		t.Fatalf("due soon = %v", got)
	}
}
