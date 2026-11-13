//go:build integration

package mariadb_test

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// TestQuotesPersistence runs the quote life on MariaDB 11: settings, numbering, an ad-hoc
// customer, the outbox, conversion into a customer and an invoice, and the constraints.
func TestQuotesPersistence(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	qs := must(e.svc.GetQuoteSettings(ctx))
	if qs.Settings.ValidDays != 30 || qs.NextSequence != 1 {
		t.Fatalf("defaults = %+v", qs)
	}
	qs = must(e.svc.UpdateQuoteSettings(ctx, domain.QuoteSettings{ValidDays: 21, NumberFormat: "Q{yy}-{seq}", Terms: "Deposit 50%."}, 41))
	if qs.Settings.Version != 1 || qs.NextSequence != 41 {
		t.Fatalf("saved = %+v", qs)
	}
	qs.Settings.ValidDays = 10
	qs = must(e.svc.UpdateQuoteSettings(ctx, qs.Settings, 0))
	if qs.Settings.Version != 2 || qs.Settings.ValidDays != 10 {
		t.Fatalf("updated = %+v", qs)
	}
	if _, err := e.svc.UpdateQuoteSettings(ctx, domain.QuoteSettings{ValidDays: 5, Version: 1}, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}

	a := must(e.svc.CreateAdhocCustomer(ctx, domain.AdhocCustomer{Name: "Prospect Ltd", Address: "1 Main Rd", Currency: "USD"}))
	notes := `<p>Hosting for <b>12</b> months.</p>`
	q := must(e.svc.CreateQuote(ctx, app.QuoteInput{
		AdhocCustomerID: a.ID, SalesPerson: "Albie", Reference: "Site", Notes: &notes,
		Lines: []app.LineInput{{ProductID: e.product.ID, Quantity: 2000, Discount: 500}},
	}))
	got := must(e.svc.GetQuote(ctx, q.ID)).Quote
	if got.Number != "Q26-41" || got.Currency != "USD" || got.ValidUntil != got.IssueDate.AddDays(10) || got.Terms != "Deposit 50%." ||
		len(got.Lines) != 1 || got.Lines[0].Discount != 500 || got.Totals != q.Totals || got.Notes != q.Notes || got.Fingerprint() != q.Fingerprint() {
		t.Fatalf("round trip = %+v", got)
	}

	m := must(e.svc.SendQuote(ctx, q.ID, app.SendInput{To: []string{"p@prospect.example"}, IdempotencyKey: "quote-k", LinkBase: "https://inv.example.com"}))
	// The e-mail's response token is stored as its hash and finds the quote (M41).
	link := regexp.MustCompile(`/q/([A-Za-z0-9_-]{43})\?answer=accept`).FindStringSubmatch(m.Body)
	if link == nil {
		t.Fatalf("no link in %q", m.Body)
	}
	if v := must(e.svc.GetQuoteResponse(ctx, link[1])); v.Quote.ID != q.ID {
		t.Fatalf("token → %s", v.Quote.ID)
	}
	if m2 := must(e.svc.SendQuote(ctx, q.ID, app.SendInput{IdempotencyKey: "quote-k"})); m2.ID != m.ID {
		t.Fatal("idempotency key not honoured")
	}
	ms, n := must3(e.svc.ListEmails(ctx, ports.EmailFilter{QuoteID: q.ID}))
	if n != 1 || ms[0].Kind != domain.EmailKindQuote || ms[0].QuoteID != q.ID || ms[0].InvoiceID != "" {
		t.Fatalf("outbox = %+v", ms)
	}
	if res := must(e.svc.DispatchEmails(ctx, 5)); res.Sent != 1 || e.mailer.Count() != 1 {
		t.Fatalf("dispatch = %+v", res)
	}

	// An ad-hoc customer with quotes cannot be deleted; converting moves the quotes.
	if err := e.svc.DeleteAdhocCustomer(ctx, a.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("delete = %v", err)
	}
	must(e.svc.CreateBankAccount(ctx, domain.BankAccount{Currency: "USD", Label: "USD", BankName: "B", AccountNumber: "9", Active: true}))
	c := must(e.svc.ConvertAdhocCustomer(ctx, a.ID, domain.CustomerDetails{Code: "PR", Email: "p@prospect.example"}))
	got = must(e.svc.GetQuote(ctx, q.ID)).Quote
	if got.CustomerID != c.ID || got.AdhocCustomerID != a.ID {
		t.Fatalf("quote after conversion = %+v", got)
	}
	// Search terms match literally: _ and % are not wildcards (M36).
	for _, term := range []string{"_", "%", "Q_6", `\`} {
		if _, n := must3(e.svc.ListQuotes(ctx, ports.QuoteFilter{Query: term})); n != 0 {
			t.Fatalf("search %q matched %d quotes", term, n)
		}
	}
	if list, n := must3(e.svc.ListQuotes(ctx, ports.QuoteFilter{CustomerID: c.ID, Query: "Q26"})); n != 1 || list[0].ID != q.ID {
		t.Fatalf("customer's quotes = %d", n)
	}
	if v := must(e.svc.RespondToQuote(ctx, link[1], domain.QuoteAnswerAccept, "")); v.Quote.AnsweredBy != domain.AnsweredByCustomer {
		t.Fatalf("answered by = %q", v.Quote.AnsweredBy)
	}
	if _, n := must3(e.svc.ListQuotes(ctx, ports.QuoteFilter{AnsweredBy: domain.AnsweredByCustomer})); n != 1 {
		t.Fatalf("answered by customers = %d", n)
	}
	inv := must(e.svc.ConvertQuote(ctx, q.ID))
	if inv.Totals != q.Totals || inv.Currency != "USD" || inv.Header.Job != "Site" {
		t.Fatalf("invoice = %+v", inv)
	}
	// The customer is referenced by the quote: it cannot be deleted.
	if err := e.svc.DeleteCustomer(ctx, c.ID); err == nil {
		t.Fatal("a quoted customer was deleted")
	}
	// Deleting the draft invoice puts the quote back to accepted (and the FK allows it).
	if err := e.svc.DeleteInvoice(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	if got = must(e.svc.GetQuote(ctx, q.ID)).Quote; got.Status != domain.QuoteAccepted || got.InvoiceID != "" {
		t.Fatalf("after delete = %+v", got)
	}

	// Expired filter and a declined quote.
	q2 := must(e.svc.CreateQuote(ctx, app.QuoteInput{CustomerID: e.customer.ID, Lines: []app.LineInput{{ProductID: e.integ.ID, Quantity: 1000}}}))
	e.clock.Advance(15 * 24 * time.Hour)
	if list, n := must3(e.svc.ListQuotes(ctx, ports.QuoteFilter{Expired: true})); n != 1 || list[0].ID != q2.ID {
		t.Fatalf("expired = %d", n)
	}
	if _, err := e.svc.AcceptQuote(ctx, q2.ID); err == nil {
		t.Fatal("an expired quote was accepted")
	}
	// Renewed: a new number, dated today, pointing at the old one (M41).
	r2 := must(e.svc.RenewQuote(ctx, q2.ID))
	if got := must(e.svc.GetQuote(ctx, r2.ID)).Quote; got.RenewedFrom != q2.ID || got.Number == q2.Number || len(got.Lines) != 1 {
		t.Fatalf("renewal = %+v", got)
	}
	must(e.svc.DeclineQuote(ctx, q2.ID, "no"))
	if _, n := must3(e.svc.ListQuotes(ctx, ports.QuoteFilter{Status: domain.QuoteDeclined})); n != 1 {
		t.Fatal("declined filter")
	}
	if _, n := must3(e.svc.ListQuotes(ctx, ports.QuoteFilter{AnsweredBy: domain.AnsweredByStaff})); n != 1 {
		t.Fatal("answered by staff filter")
	}
	qs = must(e.svc.GetQuoteSettings(ctx))
	qs.Settings.PublicURL = "https://public.example.com/"
	if got := must(e.svc.UpdateQuoteSettings(ctx, qs.Settings, 0)); got.Settings.PublicURL != "https://public.example.com" {
		t.Fatalf("public address = %q", got.Settings.PublicURL)
	}
	q3 := must(e.svc.CreateQuote(ctx, app.QuoteInput{CustomerID: e.customer.ID, Lines: []app.LineInput{{ProductID: e.integ.ID, Quantity: 1000}}}))
	if err := e.svc.DeleteQuote(ctx, q3.ID); err != nil {
		t.Fatal(err)
	}
	b := must(e.svc.CreateAdhocCustomer(ctx, domain.AdhocCustomer{Name: "Walk-in", Currency: "ZAR"}))
	b.Phone = "011"
	b = must(e.svc.UpdateAdhocCustomer(ctx, b))
	if b.Version != 2 {
		t.Fatalf("version = %d", b.Version)
	}
	if err := e.svc.DeleteAdhocCustomer(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, n := must3(e.svc.ListAdhocCustomers(ctx, ports.AdhocCustomerFilter{IncludeConverted: true})); n != 1 {
		t.Fatalf("ad-hoc customers = %d", n)
	}
}
