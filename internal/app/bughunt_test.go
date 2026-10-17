package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// Bug hunt (M25, 2026-10-17): regressions for what the full review found.

// conflictAfterSend makes every transaction but the first after the mail server accepted
// a message fail with a version conflict (a payment recorded on the invoice meanwhile).
type conflictAfterSend struct {
	*fakes.Store
	sent *bool
	n    int
}

func (c *conflictAfterSend) WithinTx(ctx context.Context, fn func(context.Context, ports.Repos) error) error {
	if *c.sent {
		c.n++
		if c.n > 1 && c.n <= 4 { // the three tries to mark the invoice sent
			return domain.ErrConflict
		}
	}
	return c.Store.WithinTx(ctx, fn)
}

type hookMailer struct {
	fakes.Mailer
	sent *bool
}

func (m *hookMailer) Send(ctx context.Context, mail ports.Mail) error {
	*m.sent = true
	return m.Mailer.Send(ctx, mail)
}

func TestAnAcceptedEmailIsNeverSentAgain(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	inv = must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{IdempotencyKey: "k1"}))
	sent := false
	mailer := &hookMailer{sent: &sent}
	svc := app.New(app.Deps{Tx: &conflictAfterSend{Store: h.store, sent: &sent}, Clock: h.clock, IDs: h.ids, Mailer: mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	if res := must(svc.DispatchEmails(h.ctx, 5)); res.Sent != 1 || res.Failed != 0 {
		t.Fatalf("dispatch: %+v", res)
	}
	h.clock.Advance(time.Hour)
	must(h.svc.DispatchEmails(h.ctx, 5))
	if n := mailer.Count() + h.mailer.Count(); n != 1 {
		t.Fatalf("sent %d times", n)
	}
	ms, _ := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{InvoiceID: inv.ID}))
	if len(ms) != 1 || ms[0].Status != domain.EmailSent {
		t.Fatalf("message %+v", ms)
	}
}

// A schedule in another zone than the business dates its invoice on its own run day.
func TestScheduleInvoiceIsDatedInTheSchedulesZone(t *testing.T) {
	h := newHarness(t).seed()
	b := must(h.svc.GetBusiness(h.ctx))
	b.Profile.TimeZone = "UTC"
	must(h.svc.UpdateBusiness(h.ctx, b.Profile, 0))
	h.clock.Set(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	r := domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, TimeZone: "Pacific/Auckland", StartDate: domain.NewDate(2026, 10, 1), Weekend: domain.WeekendPreviousFriday}
	must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(r)))
	// 1 November 2026 is a Sunday: the run is Friday 30 October 09:00 NZDT = 29 October 20:00 UTC.
	h.clock.Set(time.Date(2026, 10, 29, 20, 30, 0, 0, time.UTC))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("run: %+v", res)
	}
	invs, _ := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}))
	if len(invs) != 1 || invs[0].IssueDate != domain.NewDate(2026, 10, 30) {
		t.Fatalf("issued %+v, want dated 30 October", invs)
	}
}

// A once schedule whose time has passed is refused, not saved as ended.
func TestAOnceScheduleInThePastIsRefused(t *testing.T) {
	h := newHarness(t).seed()
	r := domain.Recurrence{Frequency: domain.FrequencyOnce, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)}
	if _, err := h.svc.CreateSchedule(h.ctx, h.weekendSchedule(r)); fieldOf(err) != "recurrence.startDate" {
		t.Fatalf("past once schedule: %v", err)
	}
}

// Deactivating a currency or a customer's bank account leaves the customer editable, and
// its invoices are paid into the currency default instead.
func TestDeactivatedCurrencyOrAccountDoesNotLockCustomers(t *testing.T) {
	h := newHarness(t).seed()
	other := must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{Currency: "ZAR", Label: "Old", BankName: "FNB", AccountNumber: "999", Active: true}))
	c := h.customer
	c.BankAccountID = other.ID
	c = must(h.svc.UpdateCustomer(h.ctx, c))
	other.Active, other.IsDefault = false, false
	must(h.svc.UpdateBankAccount(h.ctx, other))
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	inv = must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	if inv.Bank.AccountNumber != h.bank.AccountNumber {
		t.Fatalf("paid into %+v, want the currency default", inv.Bank)
	}
	must(h.svc.UpdateCurrency(h.ctx, "ZAR", "Rand", "R", false))
	c.Attention = "Accounts"
	if _, err := h.svc.UpdateCustomer(h.ctx, c); err != nil {
		t.Fatalf("editing a customer in a deactivated currency: %v", err)
	}
	// Choosing a deactivated currency anew is still refused.
	n := domain.Customer{Code: "NW", Name: "New", Email: "n@new.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}
	if _, err := h.svc.CreateCustomer(h.ctx, n); err == nil {
		t.Fatal("a new customer in a deactivated currency")
	}
}

// A customer's currency cannot change while invoices in the old one are unpaid: they
// would drop off the statement, which is in one currency.
func TestCurrencyChangeWaitsForOpenInvoices(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	inv = must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	c := must(h.svc.GetCustomer(h.ctx, h.customer.ID))
	c.Currency = "USD"
	if _, err := h.svc.UpdateCustomer(h.ctx, c); fieldOf(err) != "currency" {
		t.Fatalf("currency change with an unpaid invoice: %v", err)
	}
	must(h.svc.MarkInvoicePaid(h.ctx, inv.ID))
	if _, err := h.svc.UpdateCustomer(h.ctx, c); err != nil {
		t.Fatalf("after payment: %v", err)
	}
}

// A settings file with a schedule that has ended (a once schedule that ran) imports: it
// arrives ended, not paused, and the rest of the file is not refused.
func TestImportCopiesAnEndedSchedule(t *testing.T) {
	src := newHarness(t).seed()
	src.clock.Set(time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC))
	must(src.svc.CreateSchedule(src.ctx, src.weekendSchedule(domain.Recurrence{Frequency: domain.FrequencyOnce, SendHour: 9, StartDate: domain.NewDate(2026, 10, 2)})))
	src.clock.Set(time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC))
	must(src.svc.RunDueSchedules(src.ctx, 10))
	file := must(src.svc.ExportSettings(src.ctx))
	dst := newHarness(t)
	dst.clock.Set(time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC))
	res := must(dst.svc.ImportSettings(dst.ctx, file, true))
	if res.Schedules.Created != 1 {
		t.Fatalf("import: %+v", res)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "imported paused") {
			t.Fatalf("an ended schedule counted as paused: %v", res.Warnings)
		}
	}
}

// An e-mail server that could never send is imported switched off, not refused.
func TestImportSwitchesOffAServerThatCannotSend(t *testing.T) {
	h := newHarness(t).seed()
	st := domain.SMTPSettings{Enabled: true, Host: "relay.lan", Port: 25, TLS: "none", Username: "u@example.com", From: "u@example.com"}
	res := must(h.svc.ImportSettings(h.ctx, app.SettingsFile{EmailServer: &st}, true))
	if res.EmailServer.Created != 1 || len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "switched off") {
		t.Fatalf("import: %+v", res)
	}
	if s := must(h.svc.GetSMTPSettings(h.ctx)); s.Settings.Enabled {
		t.Fatalf("imported enabled: %+v", s.Settings)
	}
}
