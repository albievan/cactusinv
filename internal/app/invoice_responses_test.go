package app_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

var paidLinkRe = regexp.MustCompile(`/i/([A-Za-z0-9_-]{43})\n`)

func paidToken(t *testing.T, body string) string {
	t.Helper()
	m := paidLinkRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no I-have-paid link in %q", body)
	}
	return m[1]
}

// The customer says they paid through the link in the invoice e-mail (M42): a flag with
// their note for the business, not a payment.
func TestCustomerMarksAnInvoicePaid(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	m := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{LinkBase: "https://inv.example.com/"}))
	token := paidToken(t, m.Body)
	if !strings.Contains(m.Body, "If you have paid this invoice, let us know: https://inv.example.com/i/"+token) {
		t.Fatalf("body = %q", m.Body)
	}
	must(h.svc.DispatchEmails(h.ctx, 5))
	if sent := h.mailer.Sent[len(h.mailer.Sent)-1]; !strings.Contains(sent.HTMLBody, ">Mark as paid<") || !strings.Contains(sent.HTMLBody, `href="https://inv.example.com/i/`+token+`"`) {
		t.Fatalf("HTML = %q", sent.HTMLBody)
	}

	v := must(h.svc.GetInvoiceResponse(h.ctx, token))
	if v.Invoice.ID != inv.ID || !v.Invoice.CustomerPaidAt.IsZero() || v.Business.Name == "" {
		t.Fatalf("view = %+v", v.Invoice)
	}
	for _, bad := range []string{"", strings.Repeat("A", 43), token[:42]} {
		if _, err := h.svc.GetInvoiceResponse(h.ctx, bad); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("token %q: %v", bad, err)
		}
	}
	if _, err := h.svc.MarkInvoicePaidByCustomer(h.ctx, token, strings.Repeat("x", 1001)); err == nil {
		t.Fatal("a 1001-character note")
	}

	v = must(h.svc.MarkInvoicePaidByCustomer(h.ctx, token, " Paid by EFT on 12 Nov, ref CD1195 "))
	got := v.Invoice
	if got.CustomerPaidAt.IsZero() || got.CustomerPaidNote != "Paid by EFT on 12 Nov, ref CD1195" || got.Status == domain.InvoicePaid || !got.PaidAt.IsZero() {
		t.Fatalf("after marking: %+v", got)
	}
	ev := must(h.svc.GetInvoice(h.ctx, inv.ID)).Events
	if last := ev[len(ev)-1]; last.Action != "invoice.customer_marked_paid" || last.ActorName != "Customer (e-mail link)" || !strings.Contains(last.Summary, "ref CD1195") {
		t.Fatalf("audit = %+v", last)
	}
	// Again: no change, the first note stays.
	if again := must(h.svc.MarkInvoicePaidByCustomer(h.ctx, token, "twice")); again.Invoice.CustomerPaidNote != got.CustomerPaidNote || again.Invoice.Version != got.Version {
		t.Fatalf("second mark changed it: %+v", again.Invoice)
	}
	// The business then records the payment; the flag stays as a record.
	must(h.svc.MarkInvoicePaid(h.ctx, inv.ID))
	paid := must(h.svc.GetInvoice(h.ctx, inv.ID)).Invoice
	if paid.Status != domain.InvoicePaid || paid.CustomerPaidNote != got.CustomerPaidNote {
		t.Fatalf("paid = %+v", paid)
	}
	must(h.svc.MarkInvoicePaidByCustomer(h.ctx, token, "late click")) // a paid invoice: nothing to do
	if pdf, name := must2(h.svc.InvoiceResponsePDF(h.ctx, token)); len(pdf) == 0 || name == "" {
		t.Fatal("no PDF")
	}
}

// Without an address there is no link; Settings → Quotes' public address is used for
// scheduled e-mails, which have no browser.
func TestInvoicePaidLinkAddress(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if m := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{})); strings.Contains(m.Body, "/i/") {
		t.Fatalf("a link without an address: %q", m.Body)
	}
	qs := domain.DefaultQuoteSettings()
	qs.PublicURL = "https://public.example.com"
	must(h.svc.UpdateQuoteSettings(h.ctx, qs, 0))
	m := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{}))
	if !strings.Contains(m.Body, "https://public.example.com/i/") {
		t.Fatalf("body = %q", m.Body)
	}
	must(h.svc.DispatchEmails(h.ctx, 10))
	for _, s := range h.mailer.Sent {
		if strings.Contains(s.Body, "/i/") != (s.HTMLBody != "") {
			t.Fatalf("HTML only with the link: %q / %q", s.Body, s.HTMLBody)
		}
	}
	// A paid invoice's e-mail carries no link.
	must(h.svc.MarkInvoicePaid(h.ctx, inv.ID))
	if m, err := h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{}); err == nil && strings.Contains(m.Body, "/i/") {
		t.Fatalf("a paid invoice's e-mail has the link: %q", m.Body)
	}
}
