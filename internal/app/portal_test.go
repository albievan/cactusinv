package app_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

var accountLinkRe = regexp.MustCompile(`/a/([A-Za-z0-9_-]{43})\n`)

// The customer's account page through the link in a statement e-mail (M59): what is owed
// invoice by invoice, recent paid invoices, their PDFs (the customer's own issued invoices
// only), audited as the customer's; the link stops working after 90 days.
func TestCustomerPortal(t *testing.T) {
	h := newHarness(t).seed()
	open := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.RecordPayment(h.ctx, open.ID, app.PaymentInput{Amount: 100000}))
	paid := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.MarkInvoicePaid(h.ctx, paid.ID))
	draft := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))

	// Without an address customers reach, no link.
	plain := must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{}))
	if strings.Contains(plain.Body, "/a/") {
		t.Fatalf("link without an address: %q", plain.Body)
	}
	m := must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{LinkBase: "https://inv.example.com"}))
	link := accountLinkRe.FindStringSubmatch(m.Body)
	if link == nil || !strings.Contains(m.Body, "View your account and download your invoices: https://inv.example.com/a/") {
		t.Fatalf("body = %q", m.Body)
	}
	token := link[1]
	must(h.svc.DispatchEmails(h.ctx, 5))
	var withLink, plainHTML int // both statements go out; only the one with the link has HTML
	for _, sent := range h.mailer.Sent {
		switch {
		case strings.Contains(sent.HTMLBody, ">View your account<") && len(sent.Attachments) == 1:
			withLink++
		case sent.HTMLBody == "":
			plainHTML++
		}
	}
	if withLink != 1 || plainHTML != 1 {
		t.Fatalf("statement e-mails = %+v", h.mailer.Sent)
	}

	v := must(h.svc.GetPortal(h.ctx, token))
	if v.Statement.Customer.ID != h.customer.ID || len(v.Statement.Lines) != 1 || v.Statement.Lines[0].InvoiceID != open.ID ||
		v.Statement.Lines[0].Amount != open.Totals.Total-100000 || v.Statement.Lines[0].Total != open.Totals.Total ||
		len(v.Paid) != 1 || v.Paid[0].ID != paid.ID || v.Business.Name == "" {
		t.Fatalf("portal = %+v", v)
	}
	if _, name := must2(h.svc.PortalInvoicePDF(h.ctx, token, paid.ID)); name == "" {
		t.Fatal("no PDF name")
	}
	if _, name := must2(h.svc.PortalStatementPDF(h.ctx, token)); !strings.HasPrefix(name, "Statement_") {
		t.Fatalf("statement = %q", name)
	}
	if _, _, err := h.svc.PortalInvoicePDF(h.ctx, token, draft.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a draft: %v", err)
	}
	var viewed int
	for _, e := range must(h.svc.GetInvoice(h.ctx, paid.ID)).Events {
		if e.Action == "invoice.pdf_viewed" && e.ActorName == app.ActorCustomerLink.Name {
			viewed++
		}
	}
	if viewed != 1 {
		t.Fatalf("PDF views audited as the customer's: %d", viewed)
	}

	for _, bad := range []string{"short", strings.Repeat("x", 43)} {
		if _, err := h.svc.GetPortal(h.ctx, bad); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	h.clock.Set(h.clock.Now().Add(domain.PortalLinkValidity + time.Minute))
	if _, err := h.svc.GetPortal(h.ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
}
