package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// Regression tests for the v1.19.0 code review (M36).

// A schedule's fixed prices are amounts in its customer's currency: neither a currency
// change of the customer nor a move to a customer in another currency may carry them over.
func TestFixedPricesKeepTheirCurrency(t *testing.T) {
	h := newHarness(t).seed()
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Fixed",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000, UnitPrice: p64(150000)}},
	}))
	c := h.customer
	c.Currency = "USD"
	if _, err := h.svc.UpdateCustomer(h.ctx, c); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "fixed prices") {
		t.Fatalf("currency changed under fixed prices: %v", err)
	}

	usd := must(h.svc.CreateCustomer(h.ctx, domain.Customer{
		Code: "US", Name: "Yankee Inc", Email: "ap@yankee.example", Currency: "USD", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true,
	}))
	sc := d.Schedule
	sc.CustomerID = usd.ID
	if _, err := h.svc.UpdateSchedule(h.ctx, *sc); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "fixed prices are in ZAR") {
		t.Fatalf("schedule moved to a USD customer with ZAR prices: %v", err)
	}
	sc.Lines[0].UnitPrice = nil // priced from the product list instead: fine
	if _, err := h.svc.UpdateSchedule(h.ctx, *sc); err != nil {
		t.Fatal(err)
	}
}

// A draft is priced in the customer's currency too: it would be issued in the old one.
func TestCurrencyChangeWaitsForDrafts(t *testing.T) {
	h := newHarness(t).seed()
	must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	c := h.customer
	c.Currency = "USD"
	if _, err := h.svc.UpdateCustomer(h.ctx, c); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "drafts") {
		t.Fatalf("currency changed under a draft: %v", err)
	}
}

// An ad-hoc customer whose quotes are in one currency cannot become a customer billed in
// another (the quotes could never be invoiced).
func TestAdhocConversionKeepsQuoteCurrency(t *testing.T) {
	h := newHarness(t).seed()
	a := must(h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: "Prospect", Currency: "USD"}))
	q := must(h.svc.CreateQuote(h.ctx, app.QuoteInput{AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}}))
	if _, err := h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "PR", Currency: "ZAR"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("converted into another currency: %v", err)
	}
	must(h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "PR", Email: "pr@example.com"}))
	if inv := must(h.svc.ConvertQuote(h.ctx, q.ID)); inv.Currency != "USD" {
		t.Fatalf("invoice = %+v", inv)
	}
}

// A quote that missed its ad-hoc customer's conversion (created while it committed) still
// converts: it takes the customer they became.
func TestQuoteAdoptsConvertedCustomer(t *testing.T) {
	h := newHarness(t).seed()
	a := must(h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: "Prospect", Currency: "USD"}))
	q := must(h.svc.CreateQuote(h.ctx, app.QuoteInput{AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}}))
	c := must(h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "PR", Email: "pr@example.com"}))
	// The race: the quote row is left without the customer.
	stale := must(h.store.Repos().Quotes.Get(h.ctx, q.ID))
	stale.CustomerID = ""
	if err := h.store.Repos().Quotes.Update(h.ctx, stale); err != nil {
		t.Fatal(err)
	}
	if inv := must(h.svc.ConvertQuote(h.ctx, q.ID)); inv.CustomerID != c.ID {
		t.Fatalf("invoice for %q, want %q", inv.CustomerID, c.ID)
	}
}

// A quote e-mail queued for later is not sent once the quote was declined.
func TestDeclinedQuoteIsNotEmailed(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{SendAt: h.clock.Now().Add(24 * time.Hour)}))
	must(h.svc.DeclineQuote(h.ctx, q.ID, "too dear"))
	h.clock.Advance(25 * time.Hour)
	must(h.svc.DispatchEmails(h.ctx, 10))
	if h.mailer.Count() != 0 {
		t.Fatal("a declined quote was e-mailed")
	}
	got := must(h.store.Repos().Emails.Get(h.ctx, m.ID))
	if got.Status != domain.EmailFailed || !strings.Contains(got.LastError, "declined") {
		t.Fatalf("message = %+v", got)
	}
}

// The stored mail password is only sent to the server it was saved for: changing the host
// or account needs it typed again.
func TestSMTPPasswordStaysWithItsServer(t *testing.T) {
	h := newHarness(t).seed()
	svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: fakes.Cipher{}})
	st := must(svc.GetSMTPSettings(h.ctx)).Settings
	st.Enabled, st.Username = true, "me@gmail.com"
	pw := "abcd efgh ijkl mnop"
	st = must(svc.UpdateSMTPSettings(h.ctx, st, &pw)).Settings
	for name, change := range map[string]func(*domain.SMTPSettings){
		"host":     func(s *domain.SMTPSettings) { s.Host = "smtp.attacker.example" },
		"username": func(s *domain.SMTPSettings) { s.Username = "other@gmail.com" },
	} {
		moved := st
		change(&moved)
		if _, err := svc.UpdateSMTPSettings(h.ctx, moved, nil); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "password again") {
			t.Fatalf("%s changed with the stored password kept: %v", name, err)
		}
		v := must(svc.UpdateSMTPSettings(h.ctx, moved, &pw))
		if !v.PasswordSet {
			t.Fatalf("%s: retyped password not saved", name)
		}
		back := v.Settings
		back.Host, back.Username = st.Host, st.Username
		st = must(svc.UpdateSMTPSettings(h.ctx, back, &pw)).Settings
	}
	same := st
	same.Host = strings.ToUpper(st.Host) // the same server
	must(svc.UpdateSMTPSettings(h.ctx, same, nil))
}

// Settings → Quotes and the next quote number travel in a settings file; the number only
// moves forward.
func TestSettingsFileCarriesQuoteSettings(t *testing.T) {
	src := newHarness(t).seed()
	must(src.svc.UpdateQuoteSettings(src.ctx, domain.QuoteSettings{ValidDays: 10, NumberFormat: "QT-{seq}", Terms: "Ex works.", EmailSubject: "Q {{quote_number}}", EmailBody: "Hi"}, 40))
	file := must(src.svc.ExportSettings(src.ctx))
	if file.QuoteSettings == nil || file.QuoteSettings.NextSequence != 40 || file.QuoteSettings.Settings.Terms != "Ex works." {
		t.Fatalf("exported = %+v", file.QuoteSettings)
	}
	dst := newHarness(t).seed()
	res := must(dst.svc.ImportSettings(dst.ctx, app.SettingsFile{QuoteSettings: file.QuoteSettings}, true))
	if res.QuoteSettings.Updated != 1 {
		t.Fatalf("result = %+v", res)
	}
	v := must(dst.svc.GetQuoteSettings(dst.ctx))
	if v.NextSequence != 40 || v.Settings.NumberFormat != "QT-{seq}" || v.Settings.ValidDays != 10 {
		t.Fatalf("imported = %+v", v)
	}
	again := must(dst.svc.ImportSettings(dst.ctx, app.SettingsFile{QuoteSettings: &app.QuoteSettingsImport{Settings: v.Settings, NextSequence: 5}}, true))
	if again.QuoteSettings.Unchanged != 1 || len(again.Warnings) != 1 || must(dst.svc.GetQuoteSettings(dst.ctx)).NextSequence != 40 {
		t.Fatalf("the quote sequence went back: %+v", again)
	}
	sales := app.WithActor(dst.ctx, actorWith(domain.PermSettingsImport))
	file.QuoteSettings.Settings.ValidDays = 20
	if _, err := dst.svc.ImportSettings(sales, app.SettingsFile{QuoteSettings: file.QuoteSettings}, true); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("imported without the right to change Quote settings: %v", err)
	}
}

// A restore keeps quote numbers that were used after the backup, like invoice numbers.
func TestRestoreKeepsTheQuoteSequence(t *testing.T) {
	b := newBackupHarness(t, nil, true, fakes.Cipher{})
	name := b.backupNow()
	b.clock.Advance(time.Hour)
	must(b.svc.UpdateQuoteSettings(b.ctx, domain.DefaultQuoteSettings(), 30))
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
	if next := must(b.svc.GetQuoteSettings(b.ctx)).NextSequence; next != 30 {
		t.Fatalf("quote sequence rewound to %d", next)
	}
}
