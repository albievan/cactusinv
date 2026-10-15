package app_test

import (
	"context"
	"errors"
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
	if _, err := h.svc.PreviewRecurrence(domain.Recurrence{Frequency: "x"}, 3); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("preview validation")
	}
	pv := must(h.svc.PreviewRecurrence(domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Monday, StartDate: domain.NewDate(2026, 8, 1)}, 100))
	if len(pv) != 24 {
		t.Fatalf("preview capped at 24: %d", len(pv))
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
	if err := h.svc.ChangePassword(h.ctx, u.ID, "wrong", "another long password"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("wrong current password")
	}
	if err := h.svc.ChangePassword(h.ctx, u.ID, "correct horse battery", "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("short new password")
	}
	if err := h.svc.ChangePassword(h.ctx, u.ID, "correct horse battery", "a fresh long password"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ChangePassword(h.ctx, "nobody", "x", "a fresh long password"); !errors.Is(err, domain.ErrNotFound) {
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
type docRenderer struct{ docs []ports.InvoiceDocument }

func (r *docRenderer) RenderStatement(context.Context, ports.StatementDocument) ([]byte, error) {
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
	if err := svc.SendTestEmail(h.ctx, "me@gmail.com"); err != nil || h.mailer.Count() != 1 {
		t.Fatal("test e-mail")
	}
	if err := svc.SendTestEmail(h.ctx, "nope"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("bad recipient")
	}
	if _, err := svc.UpdateSMTPSettings(h.ctx, domain.SMTPSettings{Enabled: true, Port: 587}, nil); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("host required")
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
