package app_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

type harness struct {
	t        *testing.T
	ctx      context.Context
	svc      *app.Service
	store    *fakes.Store
	clock    *fakes.Clock
	mailer   *fakes.Mailer
	customer domain.Customer
	usersP   domain.Product
	integP   domain.Product
	bank     domain.BankAccount
}

var start = time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC) // 08:00 in Johannesburg

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, ctx: context.Background(), store: fakes.NewStore(), clock: &fakes.Clock{T: start}, mailer: &fakes.Mailer{}}
	h.svc = app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	return h
}

// must, must2 and must3 fail the test (via panic) on unexpected errors.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func must2(b []byte, s string, err error) ([]byte, string) {
	if err != nil {
		panic(err)
	}
	return b, s
}

func must3[T any](v []T, n int, err error) ([]T, int) {
	if err != nil {
		panic(err)
	}
	return v, n
}

// seed reproduces the setup behind example/Inv_no_CD_1195_26.pdf.
func (h *harness) seed() *harness {
	must(h.svc.UpdateBusiness(h.ctx, domain.BusinessProfile{
		Name: "Example Software Consulting", AddressLines: "P. O. Box 1000\nNorthgate\n1616",
		Phone: "011 555 0100", Email: "billing@example.com", TimeZone: "Africa/Johannesburg",
		NumberFormat: "{code}/{seq}/{yy}", // the example invoice's format, saved before v1.4.0
	}, 1195))
	must(h.svc.CreateCurrency(h.ctx, "ZAR", "South African Rand", "R", 2, true))
	must(h.svc.CreateCurrency(h.ctx, "USD", "US Dollar", "$", 2, true))
	h.bank = must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{
		Currency: "ZAR", Label: "Capitec savings", BankName: "Capitecbank", Branch: "Northgate",
		AccountType: "Savings", AccountNumber: "1234567890", BranchCode: "470010", Active: true,
	}))
	h.usersP = must(h.svc.CreateProduct(h.ctx, domain.Product{
		SKU: "SS-USERS", Name: "Stock system users", Description: "Stock system usage for 14 concurrent users.",
		Prices: map[domain.CurrencyCode]int64{"ZAR": 19294, "USD": 1100}, Active: true,
	}))
	h.integP = must(h.svc.CreateProduct(h.ctx, domain.Product{
		SKU: "INTEG", Name: "Integration subscription", Description: "Alchamex integration - monthly subscription",
		Prices: map[domain.CurrencyCode]int64{"ZAR": 55000}, Active: true,
	}))
	h.customer = must(h.svc.CreateCustomer(h.ctx, domain.Customer{
		Code: "CD", Name: "Acme Commercial Spares (Pty) Limited", Email: "juanita@acme.example",
		CC: []string{"accounts@acme.example"}, Attention: "Jane", BillingAddress: "P O BOX 2000,\nGERMISTON,\n1401",
		ShippingAddress: "No. 1 Example Road", Currency: "ZAR",
		Terms: domain.PaymentTerms{Kind: domain.TermsDayOfNextMonth, Value: 7}, DefaultNotes: "Disclaimer: third party software.", Active: true,
	}))
	return h
}

func p64(v int64) *int64 { return &v }

func (h *harness) referenceInput() app.InvoiceInput {
	return app.InvoiceInput{
		CustomerID: h.customer.ID,
		Lines: []app.LineInput{
			{ProductID: h.usersP.ID, Quantity: 14000},
			{ProductID: h.integP.ID, Quantity: 1000},
		},
	}
}

func TestReferenceInvoiceEndToEnd(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if inv.Status != domain.InvoiceDraft || inv.Totals.Total != 325116 || inv.Currency != "ZAR" {
		t.Fatalf("draft = %+v", inv)
	}
	if inv.IssueDate != domain.NewDate(2026, 8, 21) || inv.Notes != "" {
		t.Fatalf("defaults: %v %q", inv.IssueDate, inv.Notes)
	}
	if inv.Lines[0].Description != "Stock system usage for 14 concurrent users." || inv.Lines[0].UnitPrice != 19294 {
		t.Fatalf("product line: %+v", inv.Lines[0])
	}
	inv = must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	if inv.Number != "CD/1195/26" || inv.DueDate != domain.NewDate(2026, 9, 7) || inv.Bank.AccountNumber != "1234567890" {
		t.Fatalf("issued = %q %v %+v", inv.Number, inv.DueDate, inv.Bank)
	}
	if got := app.PDFFilename(inv); got != "Inv_no_CD_1195_26.pdf" {
		t.Fatalf("filename = %q", got)
	}
	settings := must(h.svc.GetBusiness(h.ctx))
	if settings.NextSequence != 1196 {
		t.Fatalf("next sequence = %d", settings.NextSequence)
	}

	m := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{IdempotencyKey: "k1"}))
	if m.To[0] != "juanita@acme.example" || len(m.CC) != 1 || m.Subject != "Invoice CD/1195/26 from Example Software Consulting" {
		t.Fatalf("message = %+v", m)
	}
	again := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{IdempotencyKey: "k1"}))
	if again.ID != m.ID || len(h.store.Emails()) != 1 {
		t.Fatal("idempotency key must not queue a second e-mail")
	}
	res := must(h.svc.DispatchEmails(h.ctx, 10))
	if res.Sent != 1 || h.mailer.Count() != 1 {
		t.Fatalf("dispatch = %+v", res)
	}
	sent := h.mailer.Sent[0]
	if sent.Attachments[0].Filename != "Inv_no_CD_1195_26.pdf" || sent.ReplyTo != "billing@example.com" || sent.MessageID != m.ID {
		t.Fatalf("mail = %+v", sent)
	}
	detail := must(h.svc.GetInvoice(h.ctx, inv.ID))
	if detail.Invoice.Status != domain.InvoiceSent || detail.Emails[0].Status != domain.EmailSent {
		t.Fatalf("after send: %v %v", detail.Invoice.Status, detail.Emails[0].Status)
	}
	if len(detail.Events) < 4 {
		t.Fatalf("audit events = %+v", detail.Events)
	}
	paid := must(h.svc.MarkInvoicePaid(h.ctx, inv.ID))
	if paid.Status != domain.InvoicePaid {
		t.Fatal("not paid")
	}
	pdf, name := must2(h.svc.InvoicePDF(h.ctx, inv.ID))
	if string(pdf) != "%PDF-fake CD/1195/26" || name != "Inv_no_CD_1195_26.pdf" {
		t.Fatalf("pdf = %q %q", pdf, name)
	}
}

func TestScheduledSendLaterAndCancel(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	friday := start.Add(48 * time.Hour)
	m := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{SendAt: friday, To: []string{"other@x.example"}, Subject: "Hi {{customer_name}}", Body: "b"}))
	if m.To[0] != "other@x.example" || len(m.CC) != 0 || m.Subject != "Hi Acme Commercial Spares (Pty) Limited" {
		t.Fatalf("custom recipients/subject: %+v", m)
	}
	got := must(h.svc.GetInvoice(h.ctx, inv.ID))
	if got.Invoice.Status != domain.InvoiceIssued {
		t.Fatal("sending a draft issues it")
	}
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 0 {
		t.Fatal("not due yet")
	}
	h.clock.Set(friday)
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
		t.Fatal("due on Friday")
	}

	m2 := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{SendAt: friday.Add(24 * time.Hour)}))
	c := must(h.svc.CancelEmail(h.ctx, m2.ID))
	if c.Status != domain.EmailCancelled {
		t.Fatal("cancel")
	}
	h.clock.Set(friday.Add(48 * time.Hour))
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 0 {
		t.Fatal("cancelled must not send")
	}
	r := must(h.svc.RetryEmail(h.ctx, m2.ID))
	if r.Status != domain.EmailPending {
		t.Fatal("retry")
	}
	list, n := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{InvoiceID: inv.ID}))
	if n != 2 || len(list) != 2 {
		t.Fatalf("ListEmails = %d", n)
	}
}

func TestDispatchFailureBackoffAndVoid(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{}))
	h.mailer.Err = errors.New("connection refused")
	res := must(h.svc.DispatchEmails(h.ctx, 10))
	if res.Failed != 1 || res.Sent != 0 {
		t.Fatalf("res = %+v", res)
	}
	m := h.store.Emails()[0]
	if m.Status != domain.EmailPending || m.Attempts != 1 || m.LastError != "connection refused" || !m.NextAttemptAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("after failure: %+v", m)
	}
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Failed != 0 {
		t.Fatal("backoff respected")
	}
	h.mailer.Err = nil
	h.clock.Advance(time.Minute)
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
		t.Fatal("retried after backoff")
	}

	// Void cancels pending e-mails; a voided invoice is never mailed.
	inv2 := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	m2 := must(h.svc.SendInvoice(h.ctx, inv2.ID, app.SendInput{SendAt: h.clock.Now().Add(time.Hour)}))
	v := must(h.svc.VoidInvoice(h.ctx, inv2.ID, "wrong amount"))
	if v.Status != domain.InvoiceVoid {
		t.Fatal("void")
	}
	for _, e := range h.store.Emails() {
		if e.ID == m2.ID && e.Status != domain.EmailCancelled {
			t.Fatalf("pending e-mail not cancelled: %v", e.Status)
		}
	}
	if _, err := h.svc.SendInvoice(h.ctx, inv2.ID, app.SendInput{}); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("send void: %v", err)
	}
	// An e-mail retried after void fails permanently at dispatch.
	must(h.svc.RetryEmail(h.ctx, m2.ID))
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Failed != 1 {
		t.Fatal("void dispatch")
	}
	for _, e := range h.store.Emails() {
		if e.ID == m2.ID && e.Status != domain.EmailFailed {
			t.Fatalf("void e-mail should fail permanently: %v", e.Status)
		}
	}

	// Renderer failure is a delivery failure too.
	h2 := newHarness(t).seed()
	h2.svc = app.New(app.Deps{Tx: h2.store, Clock: h2.clock, IDs: &fakes.IDs{}, Mailer: h2.mailer, Renderer: fakes.Renderer{Err: errors.New("font")}, Hasher: fakes.Hasher{}})
	inv3 := must(h2.svc.CreateInvoice(h2.ctx, h2.referenceInput()))
	must(h2.svc.SendInvoice(h2.ctx, inv3.ID, app.SendInput{}))
	if res := must(h2.svc.DispatchEmails(h2.ctx, 10)); res.Failed != 1 {
		t.Fatal("renderer failure")
	}
	if _, _, err := h2.svc.InvoicePDF(h2.ctx, inv3.ID); err == nil {
		t.Fatal("pdf error expected")
	}
}

func TestMonthlyScheduleRunsAndSends(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC))
	detail := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Acme monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 21, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
		Lines: []domain.ScheduleLine{
			{ProductID: h.usersP.ID, Quantity: 14000},
			{ProductID: h.integP.ID, Quantity: 1000},
		},
		AutoSend: true,
	}))
	sc := detail.Schedule
	if sc.Recurrence.TimeZone != "Africa/Johannesburg" {
		t.Fatalf("schedule inherits business tz: %q", sc.Recurrence.TimeZone)
	}
	if len(detail.Upcoming) != 5 || detail.Upcoming[0].UTC() != time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC) {
		t.Fatalf("upcoming = %v", detail.Upcoming)
	}
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 0 {
		t.Fatal("not due yet")
	}
	h.clock.Set(time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("run = %+v", res)
	}
	invs, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}))
	if n != 1 || invs[0].Number != "CD/1195/26" || invs[0].ScheduleID != sc.ID || invs[0].Totals.Total != 325116 {
		t.Fatalf("invoice = %+v", invs)
	}
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
		t.Fatal("auto-send")
	}
	got := must(h.svc.GetSchedule(h.ctx, sc.ID))
	if got.Schedule.Period() != domain.NewDate(2026, 9, 21) || len(got.Runs) != 1 {
		t.Fatalf("advanced to %v, runs %d", got.Schedule.Period(), len(got.Runs))
	}
	// Running again in the same minute does nothing.
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 0 {
		t.Fatal("idempotent")
	}
	// Server down for two months: both missed periods are invoiced, each dated its period.
	h.clock.Set(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 2 {
		t.Fatalf("catch-up = %+v", res)
	}
	invs, _ = must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{Query: "CD/1197"}))
	if len(invs) != 1 || invs[0].IssueDate != domain.NewDate(2026, 10, 21) {
		t.Fatalf("catch-up invoice = %+v", invs)
	}
}

func TestScheduleFailureAndControls(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC))
	// USD customer with a product priced only via the list: no USD bank account → issue fails.
	usdCust := must(h.svc.CreateCustomer(h.ctx, domain.Customer{Code: "US", Name: "Acme Inc", Email: "ap@acme.example", Currency: "USD", Terms: domain.PaymentTerms{Kind: domain.TermsDaysAfter, Value: 30}, Active: true}))
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: usdCust.ID, Name: "Acme",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
	}))
	id := d.Schedule.ID
	d = must(h.svc.RunScheduleNow(h.ctx, id))
	if !d.Schedule.NextRunAt.Equal(h.clock.Now()) {
		t.Fatal("run now")
	}
	res := must(h.svc.RunDueSchedules(h.ctx, 10))
	if res.Failed != 1 || res.Issued != 0 {
		t.Fatalf("expected failure: %+v", res)
	}
	d = must(h.svc.GetSchedule(h.ctx, id))
	if d.Schedule.FailureCount != 1 || d.Schedule.LastError == "" || len(d.Runs) != 0 {
		t.Fatalf("failure recorded: %+v", d.Schedule)
	}
	if _, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{})); n != 0 {
		t.Fatal("failed run must roll back its invoice")
	}
	// Fix it: add a USD bank account; the retry after backoff succeeds.
	must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{Currency: "USD", Label: "USD", BankName: "Wise", AccountNumber: "123", Active: true}))
	h.clock.Advance(time.Minute)
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("retry = %+v", res)
	}
	invs, _ := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{CustomerID: usdCust.ID}))
	if invs[0].Currency != "USD" || invs[0].Totals.Total != 1100 || invs[0].Number != "US/1195/26" {
		t.Fatalf("USD invoice = %+v", invs[0])
	}

	d = must(h.svc.PauseSchedule(h.ctx, id))
	if d.Schedule.Status != domain.SchedulePaused || d.Upcoming != nil {
		t.Fatal("pause")
	}
	if _, err := h.svc.RunScheduleNow(h.ctx, id); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("run-now on paused")
	}
	d = must(h.svc.ResumeSchedule(h.ctx, id))
	if d.Schedule.Status != domain.ScheduleActive {
		t.Fatal("resume")
	}
	upd := *d.Schedule
	upd.Name = "Acme monthly"
	upd.Recurrence.DayOfMonth = 15
	d = must(h.svc.UpdateSchedule(h.ctx, upd))
	if d.Schedule.Name != "Acme monthly" || d.Schedule.Period().Day != 15 {
		t.Fatalf("update: %+v", d.Schedule)
	}
	if _, err := h.svc.UpdateSchedule(h.ctx, upd); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale version")
	}
	list, n := must3(h.svc.ListSchedules(h.ctx, ports.ScheduleFilter{}))
	if n != 1 || len(list) != 1 {
		t.Fatal("list")
	}
	if err := h.svc.DeleteSchedule(h.ctx, id); err != nil {
		t.Fatal(err)
	}
	// Validation: product without a price in the customer's currency and no override.
	eur := must(h.svc.CreateCurrency(h.ctx, "EUR", "Euro", "€", 2, true))
	eurCust := must(h.svc.CreateCustomer(h.ctx, domain.Customer{Code: "EU", Name: "EU Co", Email: "a@eu.example", Currency: eur.Code, Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))
	_, err := h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: eurCust.ID, Name: "x",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
	})
	if !errors.Is(err, domain.ErrNoPriceForCurrency) {
		t.Fatalf("no EUR price: %v", err)
	}
	if _, err := h.svc.PreviewRecurrence(h.ctx, domain.Recurrence{Frequency: "x"}, 3); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("preview validation")
	}
	pv := must(h.svc.PreviewRecurrence(h.ctx, domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Monday, StartDate: domain.NewDate(2026, 8, 1)}, 100))
	if len(pv.Runs) != 24 {
		t.Fatalf("preview capped at 24: %d", len(pv.Runs))
	}
}

// TestPreviewRecurrenceTimeZone: a preview is computed in, and names, the zone saving the
// recurrence would use — the business time zone unless the recurrence names its own.
func TestPreviewRecurrenceTimeZone(t *testing.T) {
	h := newHarness(t).seed() // business time zone Africa/Johannesburg (UTC+2)
	h.clock.Set(time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC))
	fridays := domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Friday, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)}
	tests := []struct {
		name    string
		zone    string
		wantTZ  string
		wantUTC time.Time
	}{
		{"business zone when none given", "", "Africa/Johannesburg", time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)},
		{"the recurrence's own zone", "UTC", "UTC", time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)},
		{"London before the clocks change", "Europe/London", "Europe/London", time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := fridays
			r.TimeZone = tc.zone
			pv := must(h.svc.PreviewRecurrence(h.ctx, r, 3))
			if pv.TimeZone != tc.wantTZ || len(pv.Runs) != 3 || !pv.Runs[0].Equal(tc.wantUTC) {
				t.Fatalf("preview = %s %v, want %s %v", pv.TimeZone, pv.Runs, tc.wantTZ, tc.wantUTC)
			}
		})
	}
	if _, err := h.svc.PreviewRecurrence(h.ctx, domain.Recurrence{Frequency: domain.FrequencyWeekly, TimeZone: "Mars/Olympus", StartDate: domain.NewDate(2026, 10, 1)}, 3); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unknown zone: %v", err)
	}
}

// TestScheduleReadsShowProducts: reading a schedule (get and list) names each line's
// product from the look-up — not from the stored description — with its state and its
// current price in the customer's currency.
func TestScheduleReadsShowProducts(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC))
	fixed := int64(60000)
	usdOnly := must(h.svc.CreateProduct(h.ctx, domain.Product{SKU: "USD-ONLY", Name: "Dollar thing", Prices: map[domain.CurrencyCode]int64{"USD": 1000}, Active: true}))
	retired := must(h.svc.CreateProduct(h.ctx, domain.Product{SKU: "OLD", Name: "Old support plan", Prices: map[domain.CurrencyCode]int64{"ZAR": 25000}, Active: true}))
	gone := must(h.svc.CreateProduct(h.ctx, domain.Product{SKU: "GONE", Name: "Gone", Prices: map[domain.CurrencyCode]int64{"ZAR": 100}, Active: true}))
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Acme monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 21, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
		Lines: []domain.ScheduleLine{
			{ProductID: h.usersP.ID, Quantity: 14000},
			{ProductID: h.integP.ID, Quantity: 1000, UnitPrice: &fixed},
			{ProductID: usdOnly.ID, Quantity: 1000, UnitPrice: &fixed},
			{ProductID: retired.ID, Quantity: 1000},
			{ProductID: gone.ID, Quantity: 1000},
		},
	}))
	retired.Active = false
	must(h.svc.UpdateProduct(h.ctx, retired))
	if err := h.svc.DeleteProduct(h.ctx, gone.ID); err != nil { // the fake allows it; MariaDB refuses (FK)
		t.Fatal(err)
	}
	price := func(v int64) *int64 { return &v }
	want := []domain.ScheduleLineProduct{
		{Name: "Stock system users", State: domain.ProductActive, UnitPrice: price(19294)},
		{Name: "Integration subscription", State: domain.ProductActive, UnitPrice: price(55000)}, // fixed price stays on the line
		{Name: "Dollar thing", State: domain.ProductActive},                                      // no ZAR price
		{Name: "Old support plan", State: domain.ProductInactive, UnitPrice: price(25000)},
		{State: domain.ProductMissing},
	}
	got := must(h.svc.GetSchedule(h.ctx, d.Schedule.ID))
	list, _ := must3(h.svc.ListSchedules(h.ctx, ports.ScheduleFilter{}))
	if len(list) != 1 {
		t.Fatalf("list = %d", len(list))
	}
	for name, views := range map[string][]domain.ScheduleLineProduct{"get": got.LineProducts, "list": list[0].LineProducts} {
		if len(views) != len(want) {
			t.Fatalf("%s: %d views, want %d", name, len(views), len(want))
		}
		for i, w := range want {
			v := views[i]
			if v.Name != w.Name || v.State != w.State || (v.UnitPrice == nil) != (w.UnitPrice == nil) || (v.UnitPrice != nil && *v.UnitPrice != *w.UnitPrice) {
				t.Errorf("%s line %d = %+v, want %+v", name, i, v, w)
			}
		}
	}
	if got.Schedule.Lines[0].Description != "" || got.Schedule.Lines[1].UnitPrice == nil || *got.Schedule.Lines[1].UnitPrice != fixed {
		t.Fatalf("stored lines changed: %+v", got.Schedule.Lines)
	}
	// An e-mail schedule has no lines and needs no product look-up.
	e := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Statement", Kind: domain.ScheduleEmail, EmailScope: domain.EmailStatement,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
	}))
	if ge := must(h.svc.GetSchedule(h.ctx, e.Schedule.ID)); len(ge.LineProducts) != 0 {
		t.Fatalf("e-mail schedule views = %+v", ge.LineProducts)
	}
}

func TestSchedulePausesAfterRepeatedFailures(t *testing.T) {
	h := newHarness(t).seed()
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "x",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
	}))
	must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
	// Deactivate the currency's only bank account → every run fails.
	b := h.bank
	b.IsDefault = false
	b.Active = false
	must(h.svc.UpdateBankAccount(h.ctx, b))
	for range domain.MaxScheduleFailures {
		must(h.svc.RunDueSchedules(h.ctx, 1))
		h.clock.Advance(time.Hour)
	}
	got := must(h.svc.GetSchedule(h.ctx, d.Schedule.ID))
	if got.Schedule.Status != domain.SchedulePaused {
		t.Fatalf("status = %v (%d failures)", got.Schedule.Status, got.Schedule.FailureCount)
	}
}

func TestInvoiceEditing(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	in := h.referenceInput()
	notes := "No disclaimer"
	in.Notes = &notes
	in.BillTo = &domain.Party{Name: "Someone else"}
	in.ShipTo = &domain.Party{Name: "Depot", Address: "1 Road"}
	in.Terms = &domain.PaymentTerms{Kind: domain.TermsDaysAfter, Value: 14}
	vat := domain.BasisPoints(1500)
	in.TaxRate = &vat
	in.Header = domain.InvoiceHeader{SalesPerson: "Albie", Job: "J1"}
	in.IssueDate = domain.NewDate(2026, 8, 25)
	in.Lines = append(in.Lines, app.LineInput{ProductID: h.usersP.ID, Quantity: 1000, UnitPrice: p64(100), Discount: 5000})
	upd := must(h.svc.UpdateInvoice(h.ctx, inv.ID, inv.Version, in))
	if upd.Notes != notes || upd.BillTo.Name != "Someone else" || upd.TaxRate != 1500 || upd.Lines[2].Amount() != 50 || upd.Header.Job != "J1" {
		t.Fatalf("update = %+v", upd)
	}
	if _, err := h.svc.UpdateInvoice(h.ctx, inv.ID, inv.Version, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale version")
	}
	in.CustomerID = "other"
	if _, err := h.svc.UpdateInvoice(h.ctx, inv.ID, upd.Version, in); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("customer change")
	}
	issued := must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	if issued.IssueDate != domain.NewDate(2026, 8, 25) || issued.DueDate != domain.NewDate(2026, 9, 8) {
		t.Fatalf("issue uses draft date: %v %v", issued.IssueDate, issued.DueDate)
	}
	if _, err := h.svc.UpdateInvoice(h.ctx, inv.ID, issued.Version, h.referenceInput()); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("issued is immutable")
	}
	if err := h.svc.DeleteInvoice(h.ctx, inv.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("cannot delete issued")
	}
	if _, err := h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("issue twice")
	}
	draft := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if err := h.svc.DeleteInvoice(h.ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.GetInvoice(h.ctx, draft.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted")
	}
	pdf, name := must2(h.svc.InvoicePDF(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID))
	if len(pdf) == 0 || name[:6] != "Draft_" {
		t.Fatalf("draft pdf name %q", name)
	}

	bad := []app.InvoiceInput{
		{CustomerID: "nope", Lines: in.Lines},
		{CustomerID: h.customer.ID, Lines: []app.LineInput{{ProductID: "nope", Quantity: 1000}}},
		{CustomerID: h.customer.ID, Lines: []app.LineInput{{Quantity: 1000}}}, // free text is not allowed
		{CustomerID: h.customer.ID},
	}
	for i, b := range bad {
		if _, err := h.svc.CreateInvoice(h.ctx, b); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	if _, err := h.svc.VoidInvoice(h.ctx, "nope", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("void unknown")
	}
}

func TestBusinessDefaultVAT(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if inv.TaxRate != 0 || inv.Totals.Tax != 0 {
		t.Fatal("VAT defaults to 0")
	}
	s := must(h.svc.GetBusiness(h.ctx))
	p := s.Profile
	p.VATRate = 1500
	must(h.svc.UpdateBusiness(h.ctx, p, 0))
	inv = must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if inv.TaxRate != 1500 || inv.Totals.Tax != 48767 || inv.Totals.Total != 373883 {
		t.Fatalf("business VAT applied: %+v", inv.Totals)
	}
	in := h.referenceInput()
	zero := domain.BasisPoints(0)
	in.TaxRate = &zero
	if inv := must(h.svc.CreateInvoice(h.ctx, in)); inv.TaxRate != 0 {
		t.Fatal("explicit 0 overrides the default")
	}
}

func TestOverdueAndDashboard(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	must(h.svc.CreateInvoice(h.ctx, h.referenceInput())) // draft: not outstanding
	must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{SendAt: start.Add(time.Hour)}))
	d := must(h.svc.GetDashboard(h.ctx))
	if len(d.Totals) != 1 || d.Totals[0].Outstanding != 325116 || d.Totals[0].OverdueCount != 0 || len(d.UpcomingEmails) != 1 || len(d.RecentInvoices) != 2 {
		t.Fatalf("dashboard = %+v", d)
	}
	h.clock.Set(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	d = must(h.svc.GetDashboard(h.ctx))
	if d.Totals[0].OverdueCount != 1 || d.Totals[0].Overdue != 325116 {
		t.Fatalf("overdue = %+v", d.Totals)
	}
	over, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{Overdue: true}))
	if n != 1 || over[0].ID != inv.ID {
		t.Fatal("overdue filter")
	}
	st := must(h.svc.OutboxStats(h.ctx))
	if st.Pending != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestSettingsCurrenciesBanks(t *testing.T) {
	h := newHarness(t)
	s := must(h.svc.GetBusiness(h.ctx))
	if s.Profile.Name != "My Business" || s.NextSequence != 1 || s.Profile.Version != 0 {
		t.Fatalf("defaults = %+v", s)
	}
	h.seed()
	s = must(h.svc.GetBusiness(h.ctx))
	p := s.Profile
	p.Phone = "123"
	if _, err := h.svc.UpdateBusiness(h.ctx, p, 10); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("sequence may not go backwards")
	}
	s = must(h.svc.UpdateBusiness(h.ctx, p, 0))
	if s.Profile.Phone != "123" || s.NextSequence != 1195 {
		t.Fatalf("update = %+v", s)
	}
	if _, err := h.svc.UpdateBusiness(h.ctx, p, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale profile version")
	}
	if _, err := h.svc.UpdateBusiness(h.ctx, domain.BusinessProfile{}, 0); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("invalid profile")
	}

	if _, err := h.svc.CreateCurrency(h.ctx, "ZAR", "dup", "R", 2, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("dup currency")
	}
	if _, err := h.svc.CreateCurrency(h.ctx, "Z", "bad", "R", 2, true); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("bad currency")
	}
	c := must(h.svc.UpdateCurrency(h.ctx, "USD", "Dollar", "US$", false))
	if c.MinorUnits != 2 || c.Name != "Dollar" || c.Active {
		t.Fatalf("update currency = %+v", c)
	}
	if _, err := h.svc.UpdateCurrency(h.ctx, "GBP", "x", "", true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown currency")
	}
	cs := must(h.svc.ListCurrencies(h.ctx))
	if len(cs) != 2 {
		t.Fatal("list currencies")
	}
	// Inactive currency blocks new customers and invoices in it.
	if _, err := h.svc.CreateCustomer(h.ctx, domain.Customer{Code: "X", Name: "X", Email: "x@x.example", Currency: "USD", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}); !errors.Is(err, domain.ErrInactive) {
		t.Fatalf("inactive currency: %v", err)
	}

	// Bank accounts: first is default; a second default replaces it.
	if !h.bank.IsDefault {
		t.Fatal("first account becomes default")
	}
	second := must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{Currency: "ZAR", Label: "FNB", BankName: "FNB", AccountNumber: "62000000000", Active: true, IsDefault: true}))
	first := must(h.svc.GetBankAccount(h.ctx, h.bank.ID))
	if !second.IsDefault || first.IsDefault {
		t.Fatal("default moved")
	}
	accts := must(h.svc.ListBankAccounts(h.ctx, "ZAR"))
	if len(accts) != 2 {
		t.Fatal("list banks")
	}
	second.Currency = "USD"
	if _, err := h.svc.UpdateBankAccount(h.ctx, second); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("currency immutable")
	}
	if _, err := h.svc.CreateBankAccount(h.ctx, domain.BankAccount{Currency: "GBP", Label: "x", BankName: "x", AccountNumber: "1", Active: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("bank for unknown currency")
	}
	// Customer override to the non-default account → printed on the invoice.
	cust := h.customer
	cust.BankAccountID = first.ID
	cust = must(h.svc.UpdateCustomer(h.ctx, cust))
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	inv = must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	if inv.Bank.AccountNumber != "1234567890" {
		t.Fatalf("override not used: %+v", inv.Bank)
	}
	if err := h.svc.DeleteBankAccount(h.ctx, first.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("account in use by customer")
	}
	if err := h.svc.DeleteBankAccount(h.ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	usdBank := must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{Currency: "USD", Label: "x", BankName: "x", AccountNumber: "1", Active: true}))
	cust.BankAccountID = usdBank.ID
	if _, err := h.svc.UpdateCustomer(h.ctx, cust); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("override in other currency: %v", err)
	}
}

func TestCustomersAndProducts(t *testing.T) {
	h := newHarness(t).seed()
	list, n := must3(h.svc.ListCustomers(h.ctx, ports.CustomerFilter{Query: "acme"}))
	if n != 1 || list[0].Code != "CD" {
		t.Fatal("customer search")
	}
	if _, err := h.svc.CreateCustomer(h.ctx, h.customer); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("duplicate code")
	}
	bad := h.customer
	bad.Email = "nope"
	if _, err := h.svc.UpdateCustomer(h.ctx, bad); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("validation")
	}
	bad = h.customer
	bad.ID = "missing"
	if _, err := h.svc.UpdateCustomer(h.ctx, bad); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing")
	}
	must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if err := h.svc.DeleteCustomer(h.ctx, h.customer.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("customer with invoices cannot be deleted")
	}
	tmp := must(h.svc.CreateCustomer(h.ctx, domain.Customer{Code: "TMP", Name: "Tmp", Email: "t@x.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))
	if err := h.svc.DeleteCustomer(h.ctx, tmp.ID); err != nil {
		t.Fatal(err)
	}

	ps, n := must3(h.svc.ListProducts(h.ctx, ports.ProductFilter{Query: "stock", Currency: "USD"}))
	if n != 1 || ps[0].Prices["USD"] != 1100 {
		t.Fatal("lookup by currency")
	}
	if _, n := must3(h.svc.ListProducts(h.ctx, ports.ProductFilter{Currency: "EUR"})); n != 0 {
		t.Fatal("no EUR prices")
	}
	p := h.usersP
	p.Prices = map[domain.CurrencyCode]int64{"ZAR": 20000}
	p = must(h.svc.UpdateProduct(h.ctx, p))
	if p.Prices["ZAR"] != 20000 || len(p.Prices) != 1 {
		t.Fatal("price list replaced")
	}
	p.Prices = map[domain.CurrencyCode]int64{"GBP": 1}
	if _, err := h.svc.UpdateProduct(h.ctx, p); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown currency price")
	}
	if _, err := h.svc.CreateProduct(h.ctx, domain.Product{SKU: "SS-USERS", Name: "dup", Active: true, Prices: map[domain.CurrencyCode]int64{"ZAR": 1}}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("dup sku")
	}
	if _, err := h.svc.CreateProduct(h.ctx, domain.Product{Name: "no sku"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("validation")
	}
	if _, err := h.svc.UpdateProduct(h.ctx, domain.Product{ID: "x", SKU: "x", Name: "x", Prices: map[domain.CurrencyCode]int64{"ZAR": 1}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing")
	}
	if err := h.svc.DeleteProduct(h.ctx, h.usersP.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAuth(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.CreateUser(h.ctx, "albie@example.com", "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("short password")
	}
	if _, err := h.svc.CreateUser(h.ctx, "not-an-email", "long-enough-password"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("bad email")
	}
	u := must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
	if _, err := h.svc.CreateUser(h.ctx, "ALBIE@example.com", "correct horse battery"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("dup user")
	}
	if _, _, err := h.svc.Login(h.ctx, "albie@example.com", "wrong password!!"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("wrong password")
	}
	if _, _, err := h.svc.Login(h.ctx, "nobody@example.com", "x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("unknown user")
	}
	token, exp, err := h.svc.Login(h.ctx, " albie@example.com ", "correct horse battery")
	if err != nil || token == "" || !exp.Equal(start.Add(7*24*time.Hour)) {
		t.Fatalf("login: %v", err)
	}
	got := must(h.svc.Authenticate(h.ctx, token))
	if got.ID != u.ID {
		t.Fatal("authenticate")
	}
	if _, err := h.svc.Authenticate(h.ctx, ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("empty token")
	}
	if _, err := h.svc.Authenticate(h.ctx, "forged"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("forged token")
	}
	h.clock.Advance(8 * 24 * time.Hour)
	if _, err := h.svc.Authenticate(h.ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("expired")
	}
	if err := h.svc.Housekeeping(h.ctx); err != nil {
		t.Fatal(err)
	}
	token, _, _ = h.svc.Login(h.ctx, "albie@example.com", "correct horse battery")
	if err := h.svc.Logout(h.ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Authenticate(h.ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("logged out")
	}
	if err := h.svc.ChangePassword(h.ctx, u.ID, "", "wrong", "another long password"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("wrong current password")
	}
	if err := h.svc.ChangePassword(h.ctx, u.ID, "", "correct horse battery", "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("short new password")
	}
	if err := h.svc.ChangePassword(h.ctx, u.ID, "", "correct horse battery", "a fresh long password"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ChangePassword(h.ctx, "nobody", "", "x", "a fresh long password"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown user")
	}
	if err := h.svc.SetPassword(h.ctx, "albie@example.com", "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.svc.Login(h.ctx, "albie@example.com", "another long password"); err != nil {
		t.Fatal("new password")
	}
	if err := h.svc.SetPassword(h.ctx, "albie@example.com", "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("short reset")
	}
	if err := h.svc.SetPassword(h.ctx, "nobody@example.com", "another long password"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("reset unknown")
	}
	if h.svc.Idempotency() == nil {
		t.Fatal("idempotency store")
	}
}

func TestRepositoryErrorsPropagate(t *testing.T) {
	h := newHarness(t).seed()
	boom := errors.New("db down")
	calls := []func() error{
		func() error { _, err := h.svc.GetBusiness(h.ctx); return err },
		func() error { _, err := h.svc.ListCurrencies(h.ctx); return err },
		func() error { _, err := h.svc.ListBankAccounts(h.ctx, ""); return err },
		func() error { _, _, err := h.svc.ListCustomers(h.ctx, ports.CustomerFilter{}); return err },
		func() error { _, _, err := h.svc.ListProducts(h.ctx, ports.ProductFilter{}); return err },
		func() error { _, _, err := h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}); return err },
		func() error { _, _, err := h.svc.ListSchedules(h.ctx, ports.ScheduleFilter{}); return err },
		func() error { _, _, err := h.svc.ListEmails(h.ctx, ports.EmailFilter{}); return err },
		func() error { _, err := h.svc.GetDashboard(h.ctx); return err },
		func() error { _, err := h.svc.DispatchEmails(h.ctx, 1); return err },
		func() error { _, err := h.svc.RunDueSchedules(h.ctx, 1); return err },
		func() error { _, err := h.svc.GetInvoice(h.ctx, "x"); return err },
		func() error { _, err := h.svc.GetSchedule(h.ctx, "x"); return err },
		func() error { _, _, err := h.svc.Login(h.ctx, "a@b.example", "x"); return err },
		func() error { return h.svc.Housekeeping(h.ctx) },
		func() error { _, err := h.svc.OutboxStats(h.ctx); return err },
	}
	for i, call := range calls {
		h.store.FailNext = boom
		if err := call(); !errors.Is(err, boom) {
			t.Errorf("call %d: err = %v, want db down", i, err)
		}
	}
}

func TestExportInvoicesToArchive(t *testing.T) {
	h := newHarness(t).seed()
	arch := &fakes.Archive{}
	svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Archive: arch})
	inv := must(svc.CreateInvoice(h.ctx, h.referenceInput()))
	if res := must(svc.ExportInvoices(h.ctx, 10)); res.Exported != 0 {
		t.Fatal("drafts are not exported")
	}
	inv = must(svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	if res := must(svc.ExportInvoices(h.ctx, 10)); res.Exported != 1 {
		t.Fatalf("export = %+v", res)
	}
	if string(arch.Files["2026/Inv_no_CD_1195_26.pdf"]) != "%PDF-fake CD/1195/26" {
		t.Fatalf("files = %v", arch.Files)
	}
	if res := must(svc.ExportInvoices(h.ctx, 10)); res.Exported != 0 {
		t.Fatal("unchanged invoice is not re-exported")
	}
	must(svc.VoidInvoice(h.ctx, inv.ID, "wrong"))
	if res := must(svc.ExportInvoices(h.ctx, 10)); res.Exported != 1 {
		t.Fatal("status change re-exports")
	}
	arch.Err = errors.New("disk full")
	must(svc.MarkInvoicePaid(h.ctx, must(svc.IssueInvoice(h.ctx, must(svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{})).ID))
	if res := must(svc.ExportInvoices(h.ctx, 10)); res.Failed != 1 {
		t.Fatalf("failure counted: %+v", res)
	}
	if res := must(h.svc.ExportInvoices(h.ctx, 10)); res.Exported != 0 {
		t.Fatal("no archive configured → no-op")
	}
	if got := app.ArchivePath(inv); got != "2026/Inv_no_CD_1195_26.pdf" {
		t.Fatal(got)
	}
}

func TestEnsureDefaultAdmin(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.EnsureDefaultAdmin(h.ctx, "", ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("empty credentials")
	}
	if created := must(h.svc.EnsureDefaultAdmin(h.ctx, "admin", "admin")); !created {
		t.Fatal("first start creates admin")
	}
	if created := must(h.svc.EnsureDefaultAdmin(h.ctx, "admin", "admin")); created {
		t.Fatal("only once")
	}
	if _, _, err := h.svc.Login(h.ctx, "admin", "admin"); err != nil {
		t.Fatal(err)
	}
}

// docRenderer records the documents it renders.
type docRenderer struct {
	docs  []ports.InvoiceDocument
	stmts []ports.StatementDocument
}

func (r *docRenderer) RenderStatement(_ context.Context, d ports.StatementDocument) ([]byte, error) {
	r.stmts = append(r.stmts, d)
	return []byte("%PDF"), nil
}

func (r *docRenderer) RenderPDF(_ context.Context, d ports.InvoiceDocument) ([]byte, error) {
	r.docs = append(r.docs, d)
	return []byte("%PDF"), nil
}

func TestDisclaimerPulledOnEveryPDFAndOverdue(t *testing.T) {
	h := newHarness(t).seed()
	rr := &docRenderer{}
	svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: rr, Hasher: fakes.Hasher{}})
	inv := must(svc.CreateInvoice(h.ctx, h.referenceInput()))
	inv = must(svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	must2(svc.InvoicePDF(h.ctx, inv.ID))
	if rr.docs[0].Disclaimer != "Disclaimer: third party software." {
		t.Fatalf("customer disclaimer: %q", rr.docs[0].Disclaimer)
	}
	c := must(svc.GetCustomer(h.ctx, h.customer.ID))
	c.DefaultNotes = ""
	must(svc.UpdateCustomer(h.ctx, c))
	b := must(svc.GetBusiness(h.ctx)).Profile
	b.DefaultNotes = "New business disclaimer"
	must(svc.UpdateBusiness(h.ctx, b, 0))
	must2(svc.InvoicePDF(h.ctx, inv.ID))
	if rr.docs[1].Disclaimer != "New business disclaimer" {
		t.Fatalf("disclaimer must be re-read for an already issued invoice: %q", rr.docs[1].Disclaimer)
	}

	over := must(svc.MarkInvoiceOverdue(h.ctx, inv.ID))
	if over.Status != domain.InvoiceOverdue {
		t.Fatal("overdue")
	}
	if _, err := svc.MarkInvoiceOverdue(h.ctx, inv.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("already overdue")
	}
	d := must(svc.GetDashboard(h.ctx))
	if d.Totals[0].OverdueCount != 1 {
		t.Fatalf("dashboard counts manual overdue: %+v", d.Totals)
	}
	if paid := must(svc.MarkInvoicePaid(h.ctx, inv.ID)); paid.Status != domain.InvoicePaid {
		t.Fatal("overdue → paid")
	}

	_, err := svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "x",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{Description: "free text", Quantity: 1000, UnitPrice: p64(100)}},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("schedules need products too: %v", err)
	}
}

func TestCustomerEmailSchedules(t *testing.T) {
	h := newHarness(t).seed()
	issued := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	emailed := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.SendInvoice(h.ctx, emailed.ID, app.SendInput{}))
	must(h.svc.DispatchEmails(h.ctx, 10))
	paid := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.MarkInvoicePaid(h.ctx, paid.ID))
	must(h.svc.CreateInvoice(h.ctx, h.referenceInput())) // draft: never e-mailed by a schedule
	sent := h.mailer.Count()

	mk := func(scope domain.EmailScope) string {
		d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
			CustomerID: h.customer.ID, Name: "chase " + string(scope), Kind: domain.ScheduleEmail, EmailScope: scope,
			Recurrence: domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Monday, StartDate: domain.NewDate(2026, 8, 1)},
		}))
		must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
		return d.Schedule.ID
	}
	unsentID := mk(domain.EmailUnsent)
	res := must(h.svc.RunDueSchedules(h.ctx, 10))
	if res.Emailed != 1 || res.Issued != 0 {
		t.Fatalf("unsent run = %+v", res)
	}
	must(h.svc.DispatchEmails(h.ctx, 10))
	if h.mailer.Count() != sent+1 || h.mailer.Sent[len(h.mailer.Sent)-1].Attachments[0].Filename != app.PDFFilename(issued) {
		t.Fatal("the never-e-mailed invoice was sent")
	}
	mk(domain.EmailOutstanding)
	res = must(h.svc.RunDueSchedules(h.ctx, 10))
	if res.Emailed != 2 {
		t.Fatalf("outstanding run = %+v (issued + emailed, not paid/draft)", res)
	}
	must(h.svc.DispatchEmails(h.ctx, 10))
	// The unsent schedule's next run finds nothing left to send.
	must(h.svc.RunScheduleNow(h.ctx, unsentID))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Emailed != 0 {
		t.Fatalf("nothing unsent remains: %+v", res)
	}
	if invs, _ := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{})); len(invs) != 4 {
		t.Fatal("e-mail schedules never create invoices")
	}
}

func TestSMTPSettings(t *testing.T) {
	h := newHarness(t).seed()
	v := must(h.svc.GetSMTPSettings(h.ctx))
	if v.Settings.Host != "smtp.gmail.com" || v.Settings.Port != 587 || v.Settings.Enabled || v.PasswordSet {
		t.Fatalf("Gmail preset by default: %+v", v)
	}
	st := v.Settings
	st.Enabled, st.Username = true, "me@gmail.com"
	if _, err := h.svc.UpdateSMTPSettings(h.ctx, st, nil); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("password required with a username")
	}
	pw := "abcd efgh ijkl mnop"
	if _, err := h.svc.UpdateSMTPSettings(h.ctx, st, &pw); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("no cipher → cannot store a password")
	}
	svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: fakes.Cipher{}})
	v = must(svc.UpdateSMTPSettings(h.ctx, st, &pw))
	if !v.PasswordSet || v.Settings.From != "me@gmail.com" {
		t.Fatalf("saved: %+v", v)
	}
	got, pass, ok, err := svc.SMTPServer(h.ctx)
	if err != nil || !ok || pass != pw || got.Host != "smtp.gmail.com" {
		t.Fatalf("SMTPServer = %+v %q %v %v", got, pass, ok, err)
	}
	v.Settings.Port = 465
	v.Settings.TLS = "tls"
	v = must(svc.UpdateSMTPSettings(h.ctx, v.Settings, nil))
	if !v.PasswordSet || v.Settings.Port != 465 {
		t.Fatal("nil password keeps the stored one")
	}
	if _, _, ok, _ := h.svc.SMTPServer(h.ctx); ok {
		t.Fatal("without the cipher the stored password cannot be used")
	}
	v.Settings.Enabled = false
	must(svc.UpdateSMTPSettings(h.ctx, v.Settings, nil))
	if _, _, ok, _ := svc.SMTPServer(h.ctx); ok {
		t.Fatal("disabled → fallback")
	}
	if w, err := svc.SendTestEmail(h.ctx, "me@gmail.com"); err != nil || h.mailer.Count() != 1 || w == nil || len(w) != 0 {
		t.Fatal("test e-mail")
	}
	if _, err := svc.SendTestEmail(h.ctx, "nope"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("bad recipient")
	}
	if _, err := svc.UpdateSMTPSettings(h.ctx, domain.SMTPSettings{Enabled: true, Port: 587}, nil); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("host required")
	}
}

func TestSendTestEmailWarnings(t *testing.T) {
	const gmailWarning = "Gmail sends as albievan@gmail.com unless accounts@primesoft.example is a verified \"Send mail as\" address of that account (Gmail → Settings → Accounts → Send mail as)."
	gmail := func(enabled bool, from string) *domain.SMTPSettings {
		return &domain.SMTPSettings{Enabled: enabled, Host: "smtp.gmail.com", Port: 587, TLS: "starttls", Username: "albievan@gmail.com", From: from}
	}
	tests := []struct {
		name      string
		saved     *domain.SMTPSettings // nil: nothing saved
		to        string
		storeErr  error
		mailErr   error
		want      []string
		wantView  []string // GetSMTPSettings: the saved settings, enabled or not
		wantErr   error
		wantMails int
	}{
		{name: "nothing saved: the deployment's server", to: "me@example.com", want: []string{}, wantView: []string{}, wantMails: 1},
		{name: "gmail sending as another address", saved: gmail(true, "Accounts <accounts@primesoft.example>"), to: "me@example.com", want: []string{gmailWarning}, wantView: []string{gmailWarning}, wantMails: 1},
		{name: "gmail sending as the user name", saved: gmail(true, "albievan@gmail.com"), to: "me@example.com", want: []string{}, wantView: []string{}, wantMails: 1},
		{name: "disabled in-app server: the view warns, the test e-mail (deployment's server) does not", saved: gmail(false, "accounts@primesoft.example"), to: "me@example.com", want: []string{}, wantView: []string{gmailWarning}, wantMails: 1},
		{name: "bad recipient", to: "a@example.com, b@example.com", wantErr: domain.ErrValidation},
		{name: "settings unreadable", to: "me@example.com", storeErr: domain.ErrConflict, wantErr: domain.ErrConflict},
		{name: "mail server refuses", saved: gmail(true, "accounts@primesoft.example"), to: "me@example.com", mailErr: errors.New("535 bad credentials"), wantMails: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t).seed()
			svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: fakes.Cipher{}})
			if tt.saved != nil {
				pw := "abcd efgh ijkl mnop"
				must(svc.UpdateSMTPSettings(h.ctx, *tt.saved, &pw))
			}
			if tt.wantView != nil {
				if v := must(svc.GetSMTPSettings(h.ctx)); v.Warnings == nil || !slices.Equal(v.Warnings, tt.wantView) {
					t.Fatalf("GetSMTPSettings warnings = %q, want %q", v.Warnings, tt.wantView)
				}
			}
			before := h.mailer.Count()
			h.mailer.Err, h.store.FailNext = tt.mailErr, tt.storeErr
			got, err := svc.SendTestEmail(h.ctx, tt.to)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) || got != nil {
					t.Fatalf("SendTestEmail = %q, %v; want error %v", got, err, tt.wantErr)
				}
			case tt.mailErr != nil:
				if err == nil || got != nil {
					t.Fatalf("SendTestEmail = %q, %v; want the mail server's error", got, err)
				}
			default:
				if err != nil || got == nil || !slices.Equal(got, tt.want) {
					t.Fatalf("SendTestEmail = %q, %v; want %q", got, err, tt.want)
				}
			}
			if n := h.mailer.Count() - before; n != tt.wantMails {
				t.Fatalf("mails sent = %d, want %d", n, tt.wantMails)
			}
		})
	}
}

func TestStatements(t *testing.T) {
	h := newHarness(t).seed()
	inv1 := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	inv2 := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.MarkInvoicePaid(h.ctx, inv2.ID))
	must(h.svc.CreateInvoice(h.ctx, h.referenceInput())) // draft: not on the statement

	st := must(h.svc.GetStatement(h.ctx, h.customer.ID))
	if st.Total != inv1.Totals.Total || len(st.Lines) != 1 || st.Lines[0].Number != inv1.Number {
		t.Fatalf("statement = %+v", st)
	}
	pdf, name := must2(h.svc.StatementPDF(h.ctx, h.customer.ID))
	if string(pdf) != "%PDF-statement CD" || name != "Statement_CD_2026-08-21.pdf" {
		t.Fatalf("pdf %q %q", pdf, name)
	}
	m := must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{IdempotencyKey: "st-1"}))
	if m.Kind != domain.EmailKindStatement || m.CustomerID != h.customer.ID || m.InvoiceID != "" ||
		!strings.HasPrefix(m.Subject, "Statement of account from ") {
		t.Fatalf("statement mail = %+v", m)
	}
	if again := must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{IdempotencyKey: "st-1"})); again.ID != m.ID {
		t.Fatal("idempotent")
	}
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
		t.Fatalf("dispatch = %+v", res)
	}
	sent := h.mailer.Sent[len(h.mailer.Sent)-1]
	if sent.Attachments[0].Filename != "Statement_CD_2026-08-21.pdf" || !strings.Contains(sent.Body, "R 3,251.16") {
		t.Fatalf("statement e-mail = %+v", sent)
	}
	if list, n := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{CustomerID: h.customer.ID})); n != 1 || list[0].Status != domain.EmailSent {
		t.Fatal("statement e-mail listed by customer")
	}
	// A statement schedule sends only while something is owed.
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "monthly statement", Kind: domain.ScheduleEmail, EmailScope: domain.EmailStatement,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 8, 1)},
	}))
	must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Emailed != 1 {
		t.Fatalf("statement schedule = %+v", res)
	}
	must(h.svc.MarkInvoicePaid(h.ctx, inv1.ID))
	must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Emailed != 0 {
		t.Fatal("nothing owed: no statement")
	}
	if _, err := h.svc.GetStatement(h.ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown customer")
	}
	if _, _, err := h.svc.StatementPDF(h.ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown customer pdf")
	}
	if _, err := h.svc.SendStatement(h.ctx, "nope", app.SendInput{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown customer send")
	}
	// Renderer failure on a statement e-mail is retried like any other.
	h2 := newHarness(t).seed()
	svc := app.New(app.Deps{Tx: h2.store, Clock: h2.clock, IDs: &fakes.IDs{}, Mailer: h2.mailer, Renderer: fakes.Renderer{Err: errors.New("font")}, Hasher: fakes.Hasher{}})
	must(svc.SendStatement(h2.ctx, h2.customer.ID, app.SendInput{}))
	if res := must(svc.DispatchEmails(h2.ctx, 10)); res.Failed != 1 {
		t.Fatal("statement render failure")
	}
	if _, _, err := svc.StatementPDF(h2.ctx, h2.customer.ID); err == nil {
		t.Fatal("pdf error")
	}
}

func TestSMTPServerErrorsAndCancelStatement(t *testing.T) {
	h := newHarness(t).seed()
	svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: fakes.Cipher{}})
	pw := "secret"
	must(svc.UpdateSMTPSettings(h.ctx, domain.SMTPSettings{Enabled: true, Host: "smtp.example.com", Port: 587, Username: "u@example.com", From: "u@example.com"}, &pw))
	empty := ""
	v := must(svc.UpdateSMTPSettings(h.ctx, domain.SMTPSettings{Host: "smtp.example.com", Port: 587, Version: 1}, &empty))
	if v.PasswordSet {
		t.Fatal("empty password clears it")
	}
	if _, pass, ok, err := svc.SMTPServer(h.ctx); ok || pass != "" || err != nil {
		t.Fatal("disabled")
	}
	h.store.FailNext = errors.New("db down")
	if _, err := svc.GetSMTPSettings(h.ctx); err == nil {
		t.Fatal("db error")
	}
	m := must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{SendAt: h.clock.Now().Add(time.Hour)}))
	if c := must(h.svc.CancelEmail(h.ctx, m.ID)); c.Status != domain.EmailCancelled {
		t.Fatal("cancel statement e-mail (no invoice event)")
	}
}

func TestAuditTrail(t *testing.T) {
	h := newHarness(t).seed()
	ctx := app.WithActor(h.ctx, app.Actor{ID: "u1", Name: "albie"})
	inv := must(h.svc.CreateInvoice(ctx, h.referenceInput()))
	inv = must(h.svc.IssueInvoice(ctx, inv.ID, domain.Date{}))
	must(h.svc.SendInvoice(ctx, inv.ID, app.SendInput{}))
	must(h.svc.DispatchEmails(app.WithActor(h.ctx, app.ActorDispatcher), 10))
	must(h.svc.MarkInvoiceOverdue(ctx, inv.ID))
	must2(h.svc.InvoicePDF(ctx, inv.ID))
	must(h.svc.VoidInvoice(app.WithActor(h.ctx, app.Actor{ID: "u2", Name: "jane"}), inv.ID, "wrong amount"))

	d := must(h.svc.GetInvoice(h.ctx, inv.ID))
	var trail []string
	for _, e := range d.Events {
		trail = append(trail, e.Action+" by "+e.ActorName)
	}
	want := []string{
		"invoice.created by albie", "invoice.issued by albie", "invoice.email_queued by albie",
		"invoice.email_sent by E-mail dispatcher", "invoice.overdue by albie", "invoice.pdf_viewed by albie",
		"invoice.voided by jane",
	}
	if strings.Join(trail, "|") != strings.Join(want, "|") {
		t.Fatalf("trail =\n%s\nwant\n%s", strings.Join(trail, "\n"), strings.Join(want, "\n"))
	}

	// A deleted draft keeps its history.
	draft := must(h.svc.CreateInvoice(ctx, h.referenceInput()))
	if err := h.svc.DeleteInvoice(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	gone, _ := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{EntityType: "invoice", EntityID: draft.ID}))
	if len(gone) != 2 || gone[0].Action != "invoice.deleted" {
		t.Fatalf("deleted draft history = %+v", gone)
	}

	// Everything else is audited too, and the whole chain verifies.
	_, _, _ = h.svc.Login(h.ctx, "nobody@example.com", "wrong")
	if fails, n := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Action: "auth.login_failed"})); n != 1 || fails[0].ActorName != "nobody@example.com" {
		t.Fatalf("failed login = %+v", fails)
	}
	if seeded, n := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{EntityType: "customer"})); n == 0 || seeded[0].ActorName != "System" {
		t.Fatalf("customer creation audited: %+v", seeded)
	}
	if byJane, n := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Actor: "jan"})); n != 1 || byJane[0].Action != "invoice.voided" {
		t.Fatal("actor filter")
	}
	v := must(h.svc.VerifyAudit(h.ctx))
	if !v.OK || v.Entries != len(h.store.Audit()) {
		t.Fatalf("verify = %+v", v)
	}
	h.store.TamperAudit(3, "edited")
	if v := must(h.svc.VerifyAudit(h.ctx)); v.OK || v.BrokenAt != 3 {
		t.Fatalf("tamper detected: %+v", v)
	}
	if app.ActorFrom(h.ctx).Name != "System" {
		t.Fatal("default actor")
	}
}

func TestRecurringInvoiceWorkflowRaisesThenEmailsLater(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC))
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Monthly subscription", AutoSend: true, EmailDelayDays: 2,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 14000}, {ProductID: h.integP.ID, Quantity: 1000}},
	}))
	raiseAt := d.Schedule.NextOccurrence // 1 Sep 09:00 Johannesburg
	h.clock.Set(raiseAt)
	if res := must(h.svc.RunDueSchedules(app.WithActor(h.ctx, app.ActorScheduler), 10)); res.Issued != 1 {
		t.Fatalf("raised = %+v", res)
	}
	emails := h.store.Emails()
	if len(emails) != 1 || !emails[0].SendAt.Equal(raiseAt.AddDate(0, 0, 2)) {
		t.Fatalf("e-mail scheduled for %v, want %v", emails[0].SendAt, raiseAt.AddDate(0, 0, 2))
	}
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 0 {
		t.Fatal("not e-mailed before the delay")
	}
	h.clock.Set(raiseAt.AddDate(0, 0, 2))
	if res := must(h.svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
		t.Fatal("e-mailed after the delay")
	}
	invs, _ := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}))
	ev := must(h.svc.GetInvoice(h.ctx, invs[0].ID)).Events
	if ev[0].ActorName != "Scheduler" || ev[0].Action != "invoice.created" {
		t.Fatalf("raised by the scheduler: %+v", ev[0])
	}
	bad := must(h.svc.GetSchedule(h.ctx, d.Schedule.ID)).Schedule
	bad.EmailDelayDays = 91
	if _, err := h.svc.UpdateSchedule(h.ctx, *bad); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("delay bound")
	}
}

func TestStatementOverviewAndBulkSend(t *testing.T) {
	h := newHarness(t).seed()
	other := must(h.svc.CreateCustomer(h.ctx, domain.Customer{
		Code: "ZZ", Name: "Zulu Holdings", Email: "ap@zulu.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsDaysAfter, Value: 30}, Active: true,
	}))
	none := must(h.svc.CreateCustomer(h.ctx, domain.Customer{
		Code: "AA", Name: "Alpha Paid Up", Email: "ap@alpha.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsDaysAfter, Value: 30}, Active: true,
	}))
	inv := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	in := h.referenceInput()
	in.CustomerID = other.ID
	must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, in)).ID, domain.Date{}))
	must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{}))

	list := must(h.svc.ListStatements(h.ctx))
	if len(list) != 3 || list[2].Statement.Customer.ID != none.ID || list[2].Statement.Total != 0 {
		t.Fatalf("owing first, paid-up last: %+v", list)
	}
	for _, s := range list[:2] {
		if s.Statement.Total != inv.Totals.Total {
			t.Fatalf("balance %+v", s.Statement)
		}
	}
	for _, s := range list {
		if (s.Statement.Customer.ID == h.customer.ID) != (s.LastEmail != nil) {
			t.Fatalf("last e-mail only for the customer sent one: %+v", s)
		}
	}

	res := must(h.svc.SendStatements(h.ctx, []string{h.customer.ID, other.ID, none.ID, "nope"}, false))
	if len(res.Queued) != 2 || res.Queued[0] != h.customer.ID || res.Queued[1] != other.ID ||
		res.Skipped[none.ID] != "nothing owed" || res.Skipped["nope"] != "customer not found" {
		t.Fatalf("bulk = %+v", res)
	}
	if res := must(h.svc.SendStatements(h.ctx, []string{none.ID}, true)); len(res.Queued) != 1 {
		t.Fatal("include zero balances")
	}
	if _, err := h.svc.SendStatements(h.ctx, nil, false); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("no customers")
	}
}

// TestBusinessDisclaimerWhenTheCustomerHasNone renders a PDF on every path that produces
// one, for a customer without a disclaimer of its own: each prints the business disclaimer.
func TestBusinessDisclaimerWhenTheCustomerHasNone(t *testing.T) {
	const business = "Business disclaimer: no liability for third-party software."
	tests := []struct {
		name string
		// render produces one PDF and returns the disclaimer it printed.
		render func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string
	}{
		{"invoice PDF", func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string {
			inv := must(svc.IssueInvoice(h.ctx, must(svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
			must2(svc.InvoicePDF(h.ctx, inv.ID))
			return rr.docs[len(rr.docs)-1].Disclaimer
		}},
		{"invoice e-mail", func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string {
			inv := must(svc.IssueInvoice(h.ctx, must(svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
			must(svc.SendInvoice(h.ctx, inv.ID, app.SendInput{IdempotencyKey: "k1"}))
			if res := must(svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
				t.Fatalf("dispatch = %+v", res)
			}
			return rr.docs[len(rr.docs)-1].Disclaimer
		}},
		{"archive export", func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string {
			must(svc.IssueInvoice(h.ctx, must(svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
			if res := must(svc.ExportInvoices(h.ctx, 10)); res.Exported != 1 {
				t.Fatalf("export = %+v", res)
			}
			return rr.docs[len(rr.docs)-1].Disclaimer
		}},
		{"scheduled invoice", func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string {
			h.clock.Set(time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC))
			must(svc.CreateSchedule(h.ctx, domain.Schedule{
				CustomerID: h.customer.ID, Name: "Acme monthly",
				Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 21, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
				Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 14000}},
				AutoSend:   true,
			}))
			h.clock.Set(time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC))
			if res := must(svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
				t.Fatalf("run = %+v", res)
			}
			if res := must(svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
				t.Fatalf("dispatch = %+v", res)
			}
			return rr.docs[len(rr.docs)-1].Disclaimer
		}},
		{"statement PDF", func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string {
			must2(svc.StatementPDF(h.ctx, h.customer.ID))
			return rr.stmts[len(rr.stmts)-1].Disclaimer
		}},
		{"statement e-mail", func(t *testing.T, h *harness, svc *app.Service, rr *docRenderer) string {
			must(svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{IdempotencyKey: "s1"}))
			if res := must(svc.DispatchEmails(h.ctx, 10)); res.Sent != 1 {
				t.Fatalf("dispatch = %+v", res)
			}
			return rr.stmts[len(rr.stmts)-1].Disclaimer
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t).seed()
			rr := &docRenderer{}
			svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: rr, Hasher: fakes.Hasher{}, Archive: &fakes.Archive{}})
			c := must(svc.GetCustomer(h.ctx, h.customer.ID))
			c.DefaultNotes = ""
			must(svc.UpdateCustomer(h.ctx, c))
			b := must(svc.GetBusiness(h.ctx)).Profile
			b.DefaultNotes = business
			must(svc.UpdateBusiness(h.ctx, b, 0))
			if got := tt.render(t, h, svc, rr); got != business {
				t.Fatalf("%s printed %q, want the business disclaimer", tt.name, got)
			}
		})
	}
}

// TestNumberFormatOfNewAndExistingDeployments: a saved format keeps numbering as before;
// a profile saved without one takes the default with the issue day and month.
func TestNumberFormatOfNewAndExistingDeployments(t *testing.T) {
	tests := []struct {
		name, format, want string
	}{
		{"existing deployment keeps its stored format", "{code}/{seq}/{yy}", "CD/1195/26"},
		{"new default with day and month", "", "CD/2108-1195/26"},
		{"the requested format entered by hand", "{code}/{dd}{mm}-{seq}/{yy}", "CD/2108-1195/26"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t).seed()
			b := must(h.svc.GetBusiness(h.ctx)).Profile
			b.NumberFormat = tt.format
			saved := must(h.svc.UpdateBusiness(h.ctx, b, 0)).Profile
			if tt.format != "" && saved.NumberFormat != tt.format {
				t.Fatalf("stored format rewritten to %q", saved.NumberFormat)
			}
			first := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
			if first.Number != tt.want {
				t.Fatalf("number = %q, want %q", first.Number, tt.want)
			}
			// Gap-free: the next invoice takes the next sequence value.
			second := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
			if !strings.Contains(second.Number, "1196") {
				t.Fatalf("next number = %q", second.Number)
			}
		})
	}
}

func TestScheduleEmailDefaults(t *testing.T) {
	h := newHarness(t)
	d := must(h.svc.ScheduleEmailDefaults(h.ctx)) // no business profile yet: the templates
	for name, tmpl := range map[string]domain.EmailTemplate{"invoice": d.Invoice, "unsent": d.Unsent, "outstanding": d.Outstanding, "statement": d.Statement} {
		if tmpl.Subject == "" || tmpl.Body == "" || len(tmpl.Placeholders) == 0 {
			t.Errorf("%s = %+v", name, tmpl)
		}
	}
}

// TestScheduleWeekendRule: a run moved to the Friday before is for its nominal period,
// issued on the day it runs, never runs twice and is not skipped on catch-up.
func TestScheduleWeekendRule(t *testing.T) {
	h := newHarness(t).seed() // business zone Africa/Johannesburg (UTC+2)
	h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	support := must(h.svc.CreateProduct(h.ctx, domain.Product{SKU: "SUP", Name: "Support", Description: "Support for {period}", Prices: map[domain.CurrencyCode]int64{"ZAR": 100000}, Active: true}))
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Support monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: support.ID, Quantity: 1000}},
	}))
	id := d.Schedule.ID
	if d.Schedule.Recurrence.Weekend != domain.WeekendPreviousFriday {
		t.Fatalf("new monthly schedule rule = %q", d.Schedule.Recurrence.Weekend)
	}
	// 1 Nov 2026 is a Sunday: it runs Fri 30 Oct 09:00 SAST; 1 Dec Tue; 1 Jan 2027 Fri; 1 Feb Mon.
	wantUpcoming := []time.Time{
		time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 7, 0, 0, 0, time.UTC), time.Date(2027, 2, 1, 7, 0, 0, 0, time.UTC),
	}
	for i, w := range wantUpcoming {
		if !d.Upcoming[i].Equal(w) {
			t.Fatalf("upcoming[%d] = %v, want %v", i, d.Upcoming[i].UTC(), w)
		}
	}
	h.clock.Set(time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC))
	tests := []struct {
		name      string
		at        time.Time
		issued    int
		wantIssue []domain.Date // issue dates of the invoices raised, oldest first
	}{
		{"moved run on Friday", time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC), 1, []domain.Date{domain.NewDate(2026, 10, 30)}},
		{"again the same day: nothing", time.Date(2026, 10, 30, 7, 1, 0, 0, time.UTC), 0, nil},
		{"on the nominal Sunday: nothing", time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC), 0, nil},
		{"server back on 2 Feb: Dec, Jan and Feb caught up", time.Date(2027, 2, 2, 7, 0, 0, 0, time.UTC), 3, []domain.Date{domain.NewDate(2026, 12, 1), domain.NewDate(2027, 1, 1), domain.NewDate(2027, 2, 1)}},
	}
	seen := 0
	for _, tc := range tests {
		h.clock.Set(tc.at)
		if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != tc.issued || res.Failed != 0 {
			t.Fatalf("%s: %+v", tc.name, res)
		}
		invs, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{CustomerID: h.customer.ID, Page: ports.Page{Limit: 50}}))
		if n != seen+tc.issued {
			t.Fatalf("%s: %d invoices", tc.name, n)
		}
		slices.SortFunc(invs, func(a, b domain.Invoice) int { return strings.Compare(a.Number, b.Number) })
		for i, w := range tc.wantIssue {
			if inv := invs[seen+i]; inv.IssueDate != w {
				t.Fatalf("%s: invoice %s issued %v, want %v", tc.name, inv.Number, inv.IssueDate, w)
			}
		}
		seen = n
	}
	got := must(h.svc.GetSchedule(h.ctx, id))
	var periods []string
	for _, r := range got.Runs {
		periods = append(periods, r.Period.String())
	}
	if strings.Join(periods, " ") != "2027-02-01 2027-01-01 2026-12-01 2026-11-01" {
		t.Fatalf("run periods = %v", periods)
	}
	first := must(h.svc.GetInvoice(h.ctx, got.Runs[3].InvoiceID))
	if first.Invoice.Lines[0].Description != "Support for November 2026" {
		t.Fatalf("November invoice raised in October reads %q", first.Invoice.Lines[0].Description)
	}

	// An update that names no rule keeps the stored one; an explicit none is kept.
	upd := *got.Schedule
	upd.Recurrence.Weekend = ""
	d = must(h.svc.UpdateSchedule(h.ctx, upd))
	if d.Schedule.Recurrence.Weekend != domain.WeekendPreviousFriday {
		t.Fatalf("update dropped the rule: %q", d.Schedule.Recurrence.Weekend)
	}
	upd = *d.Schedule
	upd.Recurrence.Weekend = domain.WeekendNone
	if d = must(h.svc.UpdateSchedule(h.ctx, upd)); d.Schedule.Recurrence.Weekend != domain.WeekendNone {
		t.Fatalf("none not kept: %q", d.Schedule.Recurrence.Weekend)
	}
	// A preview without a rule gets the default; with none it runs on the weekend day.
	h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	rec := domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)}
	if pv := must(h.svc.PreviewRecurrence(h.ctx, rec, 1)); !pv.Runs[0].Equal(wantUpcoming[0]) || pv.Weekend != domain.WeekendPreviousFriday {
		t.Fatalf("preview default = %v", pv.Runs)
	}
	rec.Weekend = domain.WeekendNone
	if pv := must(h.svc.PreviewRecurrence(h.ctx, rec, 1)); !pv.Runs[0].Equal(time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("preview none = %v", pv.Runs)
	}
}

// TestWeekendRuleNeverSkips: an occurrence whose weekend-moved run time has passed while its
// nominal day is still ahead runs as soon as the schedule is activated — after a rule switch,
// a late creation, an edit after a failed Friday run, or a resume on Saturday — and one that
// already ran early is not raised again.
func TestWeekendRuleNeverSkips(t *testing.T) {
	fri := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC) // 09:00 SAST; 1 Nov 2026 is a Sunday
	sat := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	monthly := func(rule domain.WeekendRule, start domain.Date) domain.Recurrence {
		return domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: start, Weekend: rule}
	}
	tests := []struct {
		name  string
		setup func(h *harness) string // returns the schedule id, leaves the clock on Saturday
		want  int                     // invoices for November after the runner's pass
		next  domain.Date             // the pending period before the pass (zero: 1 November)
	}{
		{"none switched to previous-friday on Saturday", func(h *harness) string {
			h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
			d := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(monthly(domain.WeekendNone, domain.NewDate(2026, 10, 1)))))
			h.clock.Set(sat)
			upd := *d.Schedule
			upd.Recurrence.Weekend = domain.WeekendPreviousFriday
			return must(h.svc.UpdateSchedule(h.ctx, upd)).Schedule.ID
		}, 1, domain.Date{}},
		{"created on Saturday, starting 1 November", func(h *harness) string {
			h.clock.Set(sat)
			return must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(monthly("", domain.NewDate(2026, 11, 1))))).Schedule.ID
		}, 1, domain.Date{}},
		{"Friday run failed, edited on Saturday", func(h *harness) string {
			h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
			d := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(monthly("", domain.NewDate(2026, 10, 1)))))
			b := h.bank
			b.IsDefault, b.Active = false, false
			b = must(h.svc.UpdateBankAccount(h.ctx, b))
			h.clock.Set(fri)
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Failed != 1 {
				t.Fatalf("Friday run should fail: %+v", res)
			}
			b.IsDefault, b.Active = true, true
			must(h.svc.UpdateBankAccount(h.ctx, b))
			h.clock.Set(sat)
			upd := *must(h.svc.GetSchedule(h.ctx, d.Schedule.ID)).Schedule
			upd.Name = "Edited"
			return must(h.svc.UpdateSchedule(h.ctx, upd)).Schedule.ID
		}, 1, domain.Date{}},
		{"paused Thursday, resumed Saturday", func(h *harness) string {
			h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
			d := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(monthly("", domain.NewDate(2026, 10, 1)))))
			h.clock.Set(time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC))
			must(h.svc.PauseSchedule(h.ctx, d.Schedule.ID))
			h.clock.Set(sat)
			return must(h.svc.ResumeSchedule(h.ctx, d.Schedule.ID)).Schedule.ID
		}, 1, domain.Date{}},
		{"ran early on Friday, edited on Saturday: not again", func(h *harness) string {
			h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
			d := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(monthly("", domain.NewDate(2026, 10, 1)))))
			h.clock.Set(fri)
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
				t.Fatalf("Friday run: %+v", res)
			}
			h.clock.Set(sat)
			upd := *must(h.svc.GetSchedule(h.ctx, d.Schedule.ID)).Schedule
			upd.Name = "Edited"
			return must(h.svc.UpdateSchedule(h.ctx, upd)).Schedule.ID
		}, 1, domain.NewDate(2026, 12, 1)}, // November is billed: the edit continues after it
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).seed()
			id := tc.setup(h)
			want := tc.next
			if want.IsZero() {
				want = domain.NewDate(2026, 11, 1)
			}
			if got := must(h.svc.GetSchedule(h.ctx, id)).Schedule; got.Period() != want {
				t.Fatalf("pending period %v, want %v", got.Period(), want)
			}
			must(h.svc.RunDueSchedules(h.ctx, 10))
			invs, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{CustomerID: h.customer.ID}))
			if n != tc.want {
				t.Fatalf("%d invoices, want %d", n, tc.want)
			}
			if d := invs[0].IssueDate; d != domain.NewDate(2026, 10, 30) { // its scheduled run day (before today)
				t.Fatalf("issued %v", d)
			}
			after := must(h.svc.GetSchedule(h.ctx, id))
			if after.Schedule.Period() != domain.NewDate(2026, 12, 1) || len(after.Runs) != 1 || after.Runs[0].Period != domain.NewDate(2026, 11, 1) {
				t.Fatalf("after the pass: period %v runs %+v", after.Schedule.Period(), after.Runs)
			}
		})
	}
}

func (h *harness) weekendSchedule(r domain.Recurrence) domain.Schedule {
	return domain.Schedule{CustomerID: h.customer.ID, Name: "Monthly", Recurrence: r, Lines: []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}}}
}

// TestScheduleIssueDateAndInactiveProduct: Run now ahead of the scheduled day dates the
// invoice today, not in the future; a line whose product was deactivated fails the run with
// the reason instead of invoicing it.
func TestScheduleIssueDateAndInactiveProduct(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	d := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)})))
	must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("run now: %+v", res)
	}
	invs, _ := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}))
	if invs[0].IssueDate != domain.NewDate(2026, 10, 20) {
		t.Fatalf("run now issued %v, want today", invs[0].IssueDate)
	}
	p := h.usersP
	p.Active = false
	must(h.svc.UpdateProduct(h.ctx, p))
	must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Failed != 1 || res.Issued != 0 {
		t.Fatalf("inactive product: %+v", res)
	}
	got := must(h.svc.GetSchedule(h.ctx, d.Schedule.ID)).Schedule
	if got.LastRunResult() != domain.RunFailed || !strings.Contains(got.LastError, `"Stock system users" is no longer available`) {
		t.Fatalf("failure not recorded clearly: %q", got.LastError)
	}
	if _, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{})); n != 1 {
		t.Fatalf("an inactive product was invoiced: %d invoices", n)
	}
}

// TestScheduleEmailDefaultsRender: every suggested text, put through the real invoice and
// statement e-mail paths of its schedule type, leaves no placeholder unfilled; the
// recurring-invoice texts follow the business's own invoice e-mail texts once it sets them.
func TestScheduleEmailDefaultsRender(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	d := must(h.svc.ScheduleEmailDefaults(h.ctx))
	if d.Invoice.Subject != domain.DefaultScheduleEmails().Invoice.Subject {
		t.Fatalf("built-in business texts must not replace the template: %q", d.Invoice.Subject)
	}
	// An issued, never e-mailed invoice for the e-mail schedules to send.
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	must(h.svc.IssueInvoice(h.ctx, inv.ID, domain.Date{}))
	monthly := domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 25, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)}
	tests := []struct {
		name  string
		kind  domain.ScheduleKind
		scope domain.EmailScope
		tmpl  domain.EmailTemplate
	}{
		{"recurring invoice", domain.ScheduleInvoice, "", d.Invoice},
		{"unsent invoices", domain.ScheduleEmail, domain.EmailUnsent, d.Unsent},
		{"outstanding invoices", domain.ScheduleEmail, domain.EmailOutstanding, d.Outstanding},
		{"statement", domain.ScheduleEmail, domain.EmailStatement, d.Statement},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := domain.Schedule{
				CustomerID: h.customer.ID, Name: tc.name, Kind: tc.kind, EmailScope: tc.scope, Recurrence: monthly,
				AutoSend: true, EmailSubject: tc.tmpl.Subject, EmailBody: tc.tmpl.Body,
			}
			if tc.kind == domain.ScheduleInvoice {
				sc.Lines = []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}}
			}
			before := len(h.store.Emails())
			c := must(h.svc.CreateSchedule(h.ctx, sc))
			must(h.svc.RunScheduleNow(h.ctx, c.Schedule.ID))
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Failed != 0 {
				t.Fatalf("run: %+v", res)
			}
			ems := h.store.Emails()
			if len(ems) <= before {
				t.Fatal("nothing queued")
			}
			for _, m := range ems[before:] {
				if strings.Contains(m.Subject+m.Body, "{{") || !strings.Contains(m.Body, "Dear Acme Commercial Spares") || !strings.HasSuffix(m.Body, "Example Software Consulting") {
					t.Fatalf("rendered:\n%s\n%s", m.Subject, m.Body)
				}
			}
			must(h.svc.PauseSchedule(h.ctx, c.Schedule.ID))
		})
	}
	// The business's own invoice e-mail texts become the recurring-invoice suggestion.
	bs := must(h.svc.GetBusiness(h.ctx))
	bs.Profile.EmailSubject, bs.Profile.EmailBody = "Your invoice {{invoice_number}}", "Hello {{customer_name}}"
	must(h.svc.UpdateBusiness(h.ctx, bs.Profile, 0))
	d2 := must(h.svc.ScheduleEmailDefaults(h.ctx))
	if d2.Invoice.Subject != "Your invoice {{invoice_number}}" || d2.Invoice.Body != "Hello {{customer_name}}" || d2.Unsent.Subject != d.Unsent.Subject || d2.Statement.Body != d.Statement.Body {
		t.Fatalf("defaults with business texts = %+v", d2)
	}
}

// TestEditOrResumeKeepsUnrunPeriod: an occurrence that has not run is not lost when the
// schedule is edited or resumed after its time — except after a user's pause, where missed
// periods are skipped by design — and moving the day within a billed period never bills it
// twice.
func TestEditOrResumeKeepsUnrunPeriod(t *testing.T) {
	sast := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h-2, 0, 0, 0, time.UTC) } // SAST = UTC+2
	tests := []struct {
		name     string
		downtime bool // the server is down from creation: September is not run first
		act      func(t *testing.T, h *harness, id string)
		periods  []string // run periods after the runner's last pass, newest first
	}{
		{"failed on the 15th (inactive product), fixed and edited at 11:00", false, func(t *testing.T, h *harness, id string) {
			p := h.usersP
			p.Active = false
			p = must(h.svc.UpdateProduct(h.ctx, p))
			h.clock.Set(sast(10, 15, 9))
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Failed != 1 {
				t.Fatalf("should fail: %+v", res)
			}
			p.Active = true
			must(h.svc.UpdateProduct(h.ctx, p))
			h.clock.Set(sast(10, 15, 11))
			editName(t, h, id, "Edited")
		}, []string{"2026-10-15"}},
		{"auto-paused after five failures, then resumed", false, func(t *testing.T, h *harness, id string) {
			b := h.bank
			b.IsDefault, b.Active = false, false
			b = must(h.svc.UpdateBankAccount(h.ctx, b))
			h.clock.Set(sast(10, 15, 9))
			for range domain.MaxScheduleFailures {
				must(h.svc.RunDueSchedules(h.ctx, 10))
				h.clock.Advance(time.Hour)
			}
			if got := must(h.svc.GetSchedule(h.ctx, id)).Schedule; got.Status != domain.SchedulePaused || !got.HasFailedPending() {
				t.Fatalf("not auto-paused: %+v", got)
			}
			b.IsDefault, b.Active = true, true
			must(h.svc.UpdateBankAccount(h.ctx, b))
			editName(t, h, id, "Edited while paused") // keeps the failure state
			h.clock.Set(sast(10, 16, 10))
			must(h.svc.ResumeSchedule(h.ctx, id))
		}, []string{"2026-10-15"}},
		{"paused by the user, resumed after the 15th: skipped", false, func(t *testing.T, h *harness, id string) {
			h.clock.Set(sast(10, 14, 10))
			must(h.svc.PauseSchedule(h.ctx, id))
			h.clock.Set(sast(10, 16, 10))
			must(h.svc.ResumeSchedule(h.ctx, id))
		}, nil},
		{"server down since August, edited before the runner caught up", true, func(t *testing.T, h *harness, id string) {
			h.clock.Set(sast(10, 16, 10))
			editName(t, h, id, "Edited")
		}, []string{"2026-10-15", "2026-09-15"}},
		{"day moved within a billed month: no second invoice, next month raised", false, func(t *testing.T, h *harness, id string) {
			h.clock.Set(sast(10, 15, 9))
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
				t.Fatalf("October: %+v", res)
			}
			h.clock.Set(sast(10, 16, 10))
			upd := *must(h.svc.GetSchedule(h.ctx, id)).Schedule
			upd.Recurrence.DayOfMonth = 20 // 20 Oct is still October
			must(h.svc.UpdateSchedule(h.ctx, upd))
			h.clock.Set(sast(10, 20, 9))
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 0 {
				t.Fatalf("October billed twice: %+v", res)
			}
			h.clock.Set(sast(11, 20, 9))
		}, []string{"2026-11-20", "2026-10-15"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).seed()
			h.clock.Set(sast(8, 20, 10)) // created 20 Aug: first run 15 Sep
			d := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)})))
			if !tc.downtime {
				h.clock.Set(sast(9, 15, 9))
				if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 { // September, so only later periods are in question
					t.Fatalf("September: %+v", res)
				}
			}
			tc.act(t, h, d.Schedule.ID)
			must(h.svc.RunDueSchedules(h.ctx, 10))
			var got []string
			for _, r := range must(h.svc.GetSchedule(h.ctx, d.Schedule.ID)).Runs {
				got = append(got, r.Period.String())
			}
			want := tc.periods
			if !tc.downtime {
				want = append(slices.Clone(want), "2026-09-15")
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("run periods = %v, want %v", got, want)
			}
			_, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}))
			if n != len(want) {
				t.Fatalf("%d invoices for %d periods", n, len(want))
			}
		})
	}
}

func editName(t *testing.T, h *harness, id, name string) {
	t.Helper()
	upd := *must(h.svc.GetSchedule(h.ctx, id)).Schedule
	upd.Name = name
	must(h.svc.UpdateSchedule(h.ctx, upd))
}
