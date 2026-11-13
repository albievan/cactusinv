package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInvoicePaidLinkAndHTML(t *testing.T) {
	link := InvoicePaidLink("https://inv.example.com/", testToken)
	if link != "https://inv.example.com/i/"+testToken {
		t.Fatalf("link = %q", link)
	}
	body := AppendInvoicePaidLink("Dear <Acme>,\n\nPlease find invoice 7.\n", link)
	if !strings.HasSuffix(body, "invoice 7.\n\nIf you have paid this invoice, let us know: "+link+"\n") {
		t.Fatalf("text = %q", body)
	}
	h := InvoiceEmailHTML(body, "Example & Sons")
	for _, want := range []string{`href="` + link + `"`, ">Mark as paid<", "Dear &lt;Acme&gt;,", "Example &amp; Sons"} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if strings.Contains(h, "let us know:") {
		t.Error("the link line stays in the HTML")
	}
	for _, b := range []string{"Hello", "If you have paid this invoice, let us know: https://evil.example/x", "If you have paid this invoice, let us know: " + link + "\"x"} {
		if InvoiceEmailHTML(b, "B") != "" {
			t.Errorf("HTML made from %q", b)
		}
	}
	// A quote link does not make an invoice button, nor the other way round.
	if InvoiceEmailHTML(NewQuoteResponseLinks("https://x", testToken).AppendTo("Hi"), "B") != "" || QuoteEmailHTML(body, "B") != "" {
		t.Error("links of one kind made buttons of the other")
	}
}

func TestMarkPaidByCustomer(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	inv := Invoice{Status: InvoiceSent}
	if changed, err := inv.MarkPaidByCustomer("  Paid on Friday by EFT  ", now); err != nil || !changed {
		t.Fatalf("mark: %v %v", changed, err)
	}
	if !inv.CustomerPaidAt.Equal(now) || inv.CustomerPaidNote != "Paid on Friday by EFT" || inv.Status != InvoiceSent {
		t.Fatalf("invoice = %+v", inv)
	}
	if changed, err := inv.MarkPaidByCustomer("again", now.Add(time.Hour)); err != nil || changed || inv.CustomerPaidNote != "Paid on Friday by EFT" {
		t.Fatalf("second mark: %v %v %q", changed, err, inv.CustomerPaidNote)
	}
	long := Invoice{Status: InvoiceOverdue}
	if _, err := long.MarkPaidByCustomer(strings.Repeat("é", 1001), now); err == nil {
		t.Fatal("a 1001-character note")
	}
	if _, err := long.MarkPaidByCustomer(strings.Repeat("é", 1000), now); err != nil {
		t.Fatalf("1000 characters: %v", err)
	}
	for _, st := range []InvoiceStatus{InvoiceDraft, InvoicePaid, InvoiceVoid} {
		i := Invoice{Status: st}
		if _, err := i.MarkPaidByCustomer("", now); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s: %v", st, err)
		}
	}
}
