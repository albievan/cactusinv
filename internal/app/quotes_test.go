package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

func (h *harness) quoteInput() app.QuoteInput {
	return app.QuoteInput{
		CustomerID: h.customer.ID, SalesPerson: "Albie", Reference: "Renewal",
		Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 14000}, {ProductID: h.integP.ID, Quantity: 1000, UnitPrice: p64(50000)}},
	}
}

func actorWith(perms ...domain.Permission) app.Actor {
	a := domain.AccessFor([]domain.Group{{ID: "g", Name: "Sales", Permissions: perms}})
	return app.Actor{ID: "u1", Name: "sam", Access: &a}
}

func TestQuoteSettingsAndNumbers(t *testing.T) {
	h := newHarness(t).seed()
	v := must(h.svc.GetQuoteSettings(h.ctx))
	if v.Settings.ValidDays != 30 || v.Settings.NumberFormat != "Q-{seq}/{yy}" || v.NextSequence != 1 {
		t.Fatalf("defaults = %+v", v)
	}
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	if q.Number != "Q-1/26" || q.ValidUntil != q.IssueDate.AddDays(30) || q.IssueDate != domain.NewDate(2026, 8, 21) {
		t.Fatalf("quote = %+v", q)
	}
	v = must(h.svc.UpdateQuoteSettings(h.ctx, domain.QuoteSettings{
		ValidDays: 14, NumberFormat: "QT{yyyy}-{seq}", Terms: "Prices exclude delivery.", EmailSubject: "Your quote {{quote_number}}",
	}, 100))
	if v.NextSequence != 100 || v.Settings.Version != 1 || v.Settings.EmailBody != domain.DefaultQuoteEmailBody {
		t.Fatalf("saved = %+v", v)
	}
	q = must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	if q.Number != "QT2026-100" || q.ValidUntil != q.IssueDate.AddDays(14) || q.Terms != "Prices exclude delivery." {
		t.Fatalf("quote with settings = %+v", q)
	}
	if _, err := h.svc.UpdateQuoteSettings(h.ctx, v.Settings, 50); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("the sequence went back: %v", err)
	}
	if _, err := h.svc.UpdateQuoteSettings(h.ctx, domain.QuoteSettings{ValidDays: 14, Version: 0}, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a stale save was accepted: %v", err)
	}
	if _, err := h.svc.UpdateQuoteSettings(h.ctx, domain.QuoteSettings{ValidDays: 0}, 0); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("0 days accepted: %v", err)
	}
	audit := h.store.Audit()
	if a := audit[len(audit)-1]; a.Action != "quote.created" {
		t.Fatalf("last audit = %+v", a)
	}
}

func TestQuoteForCustomerToInvoice(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	// Priced from the products in the customer's currency; an explicit price wins.
	if q.Lines[0].UnitPrice != 19294 || q.Lines[0].Description != "Stock system usage for 14 concurrent users." ||
		q.Lines[1].UnitPrice != 50000 || q.Totals.Total != 320116 {
		t.Fatalf("lines = %+v", q.Lines)
	}
	if q.BillTo.Name != h.customer.Name || q.BillTo.Attention != "Jane" || q.Currency != "ZAR" {
		t.Fatalf("bill to = %+v", q.BillTo)
	}
	// Revising: the party cannot change; a stale version is refused.
	in := h.quoteInput()
	in.Lines = in.Lines[:1]
	if _, err := h.svc.UpdateQuote(h.ctx, q.ID, q.Version+1, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}
	in.CustomerID = "someone-else"
	if _, err := h.svc.UpdateQuote(h.ctx, q.ID, q.Version, in); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("party change: %v", err)
	}
	in.CustomerID = ""
	q = must(h.svc.UpdateQuote(h.ctx, q.ID, q.Version, in))
	if len(q.Lines) != 1 || q.Number != "Q-1/26" || q.SalesPerson != "Albie" {
		t.Fatalf("updated = %+v", q)
	}

	// E-mail it: the Quote settings template, to the customer and their CCs, through the outbox.
	m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{IdempotencyKey: "k1"}))
	if m.Kind != domain.EmailKindQuote || m.QuoteID != q.ID || m.InvoiceID != "" || m.To[0] != "juanita@acme.example" ||
		m.CC[0] != "accounts@acme.example" || m.Subject != "Quotation Q-1/26 from Example Software Consulting" ||
		!strings.Contains(m.Body, "valid until 20 September 2026") {
		t.Fatalf("message = %+v", m)
	}
	if again := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{IdempotencyKey: "k1"})); again.ID != m.ID {
		t.Fatal("a repeated request queued a second e-mail")
	}
	d := must(h.svc.GetQuote(h.ctx, q.ID))
	if d.Quote.Status != domain.QuoteSent || len(d.Emails) != 1 || d.Today != domain.NewDate(2026, 8, 21) {
		t.Fatalf("detail = %+v", d)
	}
	if err := h.svc.DeleteQuote(h.ctx, q.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("a sent quote was deleted: %v", err)
	}
	res := must(h.svc.DispatchEmails(h.ctx, 10))
	if res.Sent != 1 || h.mailer.Count() != 1 {
		t.Fatalf("dispatch = %+v", res)
	}
	sent := h.mailer.Sent[0]
	if len(sent.Attachments) != 1 || sent.Attachments[0].Filename != "Quote_Q-1_26.pdf" || string(sent.Attachments[0].Data) != "%PDF-quote Q-1/26" {
		t.Fatalf("attachment = %+v", sent.Attachments)
	}

	// Accept and convert: a draft invoice with the quoted prices.
	q = must(h.svc.AcceptQuote(h.ctx, q.ID))
	inv := must(h.svc.ConvertQuote(h.ctx, q.ID))
	if inv.Status != domain.InvoiceDraft || inv.CustomerID != h.customer.ID || inv.Totals != q.Totals ||
		inv.Header.SalesPerson != "Albie" || inv.Header.Job != "Renewal" || inv.Terms.Kind != domain.TermsDayOfNextMonth {
		t.Fatalf("invoice = %+v", inv)
	}
	d = must(h.svc.GetQuote(h.ctx, q.ID))
	if d.Quote.Status != domain.QuoteConverted || d.Quote.InvoiceID != inv.ID || d.InvoiceNumber != "" {
		t.Fatalf("converted = %+v", d.Quote)
	}
	if _, err := h.svc.ConvertQuote(h.ctx, q.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("converted twice: %v", err)
	}
	inv = must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	if d = must(h.svc.GetQuote(h.ctx, q.ID)); d.InvoiceNumber != inv.Number {
		t.Fatalf("invoice number = %q", d.InvoiceNumber)
	}
	events := ""
	for _, e := range d.Events {
		events += e.Action + " "
	}
	for _, want := range []string{"quote.created", "quote.updated", "quote.email_queued", "quote.email_sent", "quote.accepted", "quote.converted"} {
		if !strings.Contains(events, want) {
			t.Errorf("events %q lack %s", events, want)
		}
	}
}

func TestQuoteInvoiceDeletedGoesBack(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	inv := must(h.svc.ConvertQuote(h.ctx, q.ID))
	if err := h.svc.DeleteInvoice(h.ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	d := must(h.svc.GetQuote(h.ctx, q.ID))
	if d.Quote.Status != domain.QuoteAccepted || d.Quote.InvoiceID != "" {
		t.Fatalf("after the invoice was deleted = %+v", d.Quote)
	}
	again := must(h.svc.ConvertQuote(h.ctx, q.ID))
	if again.ID == inv.ID {
		t.Fatal("the same invoice id")
	}
	// An invoice not made from a quote deletes as before.
	plain := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if err := h.svc.DeleteInvoice(h.ctx, plain.ID); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteForAdhocCustomer(t *testing.T) {
	h := newHarness(t).seed()
	if _, err := h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: "Prospect", Currency: "EUR"}); err == nil {
		t.Fatal("an ad-hoc customer in an unknown currency")
	}
	a := must(h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: " Prospect Ltd ", Address: "1 Main Rd", Attention: "Thandi", Currency: "USD"}))
	if a.Name != "Prospect Ltd" || a.Version != 1 || a.ID == "" {
		t.Fatalf("ad-hoc = %+v", a)
	}
	q := must(h.svc.CreateQuote(h.ctx, app.QuoteInput{AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 2000}}}))
	if q.Currency != "USD" || q.Lines[0].UnitPrice != 1100 || q.BillTo.Name != "Prospect Ltd" || q.BillTo.Attention != "Thandi" || q.CustomerID != "" {
		t.Fatalf("quote = %+v", q)
	}
	// Not priced in the ad-hoc customer's currency: refused.
	if _, err := h.svc.CreateQuote(h.ctx, app.QuoteInput{AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.integP.ID, Quantity: 1000}}}); err == nil {
		t.Fatal("a product without a USD price was quoted in USD")
	}
	for name, in := range map[string]app.QuoteInput{
		"nobody":  {Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}},
		"both":    {CustomerID: h.customer.ID, AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}},
		"unknown": {AdhocCustomerID: "nope", Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}},
		"free":    {CustomerID: h.customer.ID, Lines: []app.LineInput{{Description: "free text", Quantity: 1000}}},
	} {
		if _, err := h.svc.CreateQuote(h.ctx, in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// No e-mail address yet: sending names the problem, or goes to whoever is typed.
	if _, err := h.svc.SendQuote(h.ctx, q.ID, app.SendInput{}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("sent with no address: %v", err)
	}
	m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{To: []string{"thandi@prospect.example"}}))
	if m.To[0] != "thandi@prospect.example" {
		t.Fatalf("to = %v", m.To)
	}

	// The currency is fixed once quoted; other details may change.
	a.Currency = "ZAR"
	if _, err := h.svc.UpdateAdhocCustomer(h.ctx, a); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("currency changed under a quote: %v", err)
	}
	a.Currency, a.Email = "USD", "thandi@prospect.example"
	a = must(h.svc.UpdateAdhocCustomer(h.ctx, a))
	if a.Version != 2 || a.Email != "thandi@prospect.example" {
		t.Fatalf("updated = %+v", a)
	}
	if err := h.svc.DeleteAdhocCustomer(h.ctx, a.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("an ad-hoc customer with quotes was deleted: %v", err)
	}
	list, n := must2l(h.svc.ListAdhocCustomers(h.ctx, ports.AdhocCustomerFilter{Query: "prosp"}))
	if n != 1 || list[0].ID != a.ID {
		t.Fatalf("list = %+v", list)
	}

	// It cannot become an invoice until the ad-hoc customer is a customer.
	_ = must(h.svc.AcceptQuote(h.ctx, q.ID))
	if _, err := h.svc.ConvertQuote(h.ctx, q.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("converted for an ad-hoc customer: %v", err)
	}
	// Making a customer needs the right to add customers, and a free invoice code.
	sales := app.WithActor(h.ctx, actorWith(domain.PermQuotesEdit))
	if _, err := h.svc.ConvertAdhocCustomer(sales, a.ID, domain.CustomerDetails{Code: "PR"}); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("converted without customers.create: %v", err)
	}
	if _, err := h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "CD"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a used code was accepted: %v", err)
	}
	c := must(h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "pr", Terms: domain.PaymentTerms{Kind: domain.TermsDaysAfter, Value: 30}}))
	if c.Code != "PR" || c.Name != "Prospect Ltd" || c.Email != "thandi@prospect.example" || c.Currency != "USD" || c.BillingAddress != "1 Main Rd" || !c.Active {
		t.Fatalf("customer = %+v", c)
	}
	a = must(h.svc.GetAdhocCustomer(h.ctx, a.ID))
	if a.CustomerID != c.ID || a.ConvertedAt.IsZero() {
		t.Fatalf("ad-hoc after = %+v", a)
	}
	if _, err := h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "PX"}); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("converted twice: %v", err)
	}
	if _, err := h.svc.UpdateAdhocCustomer(h.ctx, a); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("a converted ad-hoc customer was edited: %v", err)
	}
	if list, n = must2l(h.svc.ListAdhocCustomers(h.ctx, ports.AdhocCustomerFilter{})); n != 0 {
		t.Fatalf("the converted one is still listed: %+v", list)
	}
	if _, n = must2l(h.svc.ListAdhocCustomers(h.ctx, ports.AdhocCustomerFilter{IncludeConverted: true})); n != 1 {
		t.Fatal("the converted one is gone")
	}
	// New quotes go to the customer, not the ad-hoc customer.
	if _, err := h.svc.CreateQuote(h.ctx, app.QuoteInput{AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("quoted a converted ad-hoc customer: %v", err)
	}

	// The quote now belongs to the customer and becomes their invoice, in USD.
	d := must(h.svc.GetQuote(h.ctx, q.ID))
	if d.Quote.CustomerID != c.ID || d.Adhoc == nil || d.Adhoc.ID != a.ID {
		t.Fatalf("quote after = %+v", d)
	}
	must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{Currency: "USD", Label: "USD", BankName: "B", AccountNumber: "1", Active: true}))
	inv := must(h.svc.ConvertQuote(h.ctx, q.ID))
	if inv.CustomerID != c.ID || inv.Currency != "USD" || inv.Totals.Total != q.Totals.Total || inv.BillTo.Name != "Prospect Ltd" {
		t.Fatalf("invoice = %+v", inv)
	}
	if qs, n := must2q(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{CustomerID: c.ID})); n != 1 || qs[0].ID != q.ID {
		t.Fatalf("customer's quotes = %d", n)
	}
	// An ad-hoc customer with no quotes can be deleted.
	b := must(h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: "Walk-in", Currency: "ZAR"}))
	if err := h.svc.DeleteAdhocCustomer(h.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteOutcomesAndList(t *testing.T) {
	h := newHarness(t).seed()
	draft := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	declined := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	declined = must(h.svc.DeclineQuote(h.ctx, declined.ID, " too dear "))
	if declined.DeclineReason != "too dear" {
		t.Fatalf("declined = %+v", declined)
	}
	if _, err := h.svc.UpdateQuote(h.ctx, declined.ID, declined.Version, h.quoteInput()); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("a declined quote was edited: %v", err)
	}
	if _, err := h.svc.SendQuote(h.ctx, declined.ID, app.SendInput{}); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("a declined quote was sent: %v", err)
	}
	if _, n := must2q(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{Status: domain.QuoteDeclined})); n != 1 {
		t.Fatal("declined filter")
	}
	if _, n := must2q(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{Query: "q-1"})); n != 1 {
		t.Fatal("number search")
	}
	if _, n := must2q(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{Expired: true})); n != 0 {
		t.Fatal("nothing has expired yet")
	}
	h.clock.Advance(31 * 24 * time.Hour)
	if qs, n := must2q(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{Expired: true})); n != 1 || qs[0].ID != draft.ID {
		t.Fatalf("expired = %d", n)
	}
	// An expired quote may still be accepted (the customer's word counts), and deleted.
	if err := h.svc.DeleteQuote(h.ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.GetQuote(h.ctx, draft.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted quote still there")
	}
	pdf, name := must2(h.svc.QuotePDF(h.ctx, declined.ID))
	if string(pdf) != "%PDF-quote Q-2/26" || name != "Quote_Q-2_26.pdf" {
		t.Fatalf("pdf %q %q", pdf, name)
	}
}

func TestConvertQuoteNeedsInvoicesCreate(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	sales := app.WithActor(h.ctx, actorWith(domain.PermQuotesConvert))
	if _, err := h.svc.ConvertQuote(sales, q.ID); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("converted without invoices.create: %v", err)
	}
	both := app.WithActor(h.ctx, actorWith(domain.PermQuotesConvert, domain.PermInvoicesCreate))
	if _, err := h.svc.ConvertQuote(both, q.ID); err != nil {
		t.Fatal(err)
	}
	// An inactive customer's quote cannot become an invoice.
	q2 := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	c := must(h.svc.GetCustomer(h.ctx, h.customer.ID))
	c.Active = false
	must(h.svc.UpdateCustomer(h.ctx, c))
	if _, err := h.svc.ConvertQuote(h.ctx, q2.ID); !errors.Is(err, domain.ErrInactive) {
		t.Fatalf("converted for an inactive customer: %v", err)
	}
	if _, err := h.svc.CreateQuote(h.ctx, h.quoteInput()); !errors.Is(err, domain.ErrInactive) {
		t.Fatalf("quoted an inactive customer: %v", err)
	}
}

func TestQuoteEmailFailureAndCancel(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{SendAt: start.Add(time.Hour)}))
	if _, err := h.svc.CancelEmail(h.ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	h.mailer.Err = errors.New("smtp down")
	m2 := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{}))
	for range domain.MaxEmailAttempts {
		must(h.svc.DispatchEmails(h.ctx, 1))
		h.clock.Advance(7 * time.Hour)
	}
	d := must(h.svc.GetQuote(h.ctx, q.ID))
	failed := false
	for _, e := range d.Emails {
		failed = failed || (e.ID == m2.ID && e.Status == domain.EmailFailed)
	}
	events := ""
	for _, e := range d.Events {
		events += e.Action + " "
	}
	if !failed || !strings.Contains(events, "quote.email_cancelled") || !strings.Contains(events, "quote.email_failed") {
		t.Fatalf("emails %+v, events %s", d.Emails, events)
	}
}

func must2l(v []domain.AdhocCustomer, n int, err error) ([]domain.AdhocCustomer, int) {
	if err != nil {
		panic(err)
	}
	return v, n
}

func must2q(v []domain.Quote, n int, err error) ([]domain.Quote, int) {
	if err != nil {
		panic(err)
	}
	return v, n
}
