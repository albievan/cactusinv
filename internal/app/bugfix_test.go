package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// as acts as a signed-in user whose groups grant perms.
func as(ctx context.Context, perms ...domain.Permission) context.Context {
	a := domain.AccessFor([]domain.Group{{ID: "g", Permissions: perms}})
	return app.WithActor(ctx, app.Actor{ID: "user-1", Name: "clerk@example.com", Access: &a})
}

func TestSendingADraftNeedsTheRightToIssue(t *testing.T) {
	h := newHarness(t).seed()
	in := h.referenceInput()
	in.IssueDate = domain.NewDate(2026, 7, 31)
	draft := must(h.svc.CreateInvoice(h.ctx, in))
	if _, err := h.svc.SendInvoice(as(h.ctx, domain.PermInvoicesSend), draft.ID, app.SendInput{}); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("send-only user issued a draft: %v", err)
	}
	if got := must(h.svc.GetInvoice(h.ctx, draft.ID)); got.Invoice.Status != domain.InvoiceDraft {
		t.Fatal("the draft must stay a draft")
	}
	must(h.svc.SendInvoice(as(h.ctx, domain.PermInvoicesSend, domain.PermInvoicesIssue), draft.ID, app.SendInput{}))
	got := must(h.svc.GetInvoice(h.ctx, draft.ID)).Invoice
	if got.Status != domain.InvoiceIssued || got.IssueDate != domain.NewDate(2026, 7, 31) {
		t.Fatalf("issued as %s on %v; the draft's own date is kept, as Issue does", got.Status, got.IssueDate)
	}
}

func TestDashboardShowsOnlyWhatTheUserMayOpen(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{SendAt: h.clock.Now().Add(48 * time.Hour)}))
	full := must(h.svc.GetDashboard(h.ctx))
	if len(full.UpcomingEmails) == 0 || len(full.RecentInvoices) == 0 || len(full.Activity) == 0 {
		t.Fatalf("the system sees everything: %+v", full)
	}
	d := must(h.svc.GetDashboard(as(h.ctx, domain.PermDashboardView)))
	if len(d.UpcomingEmails) != 0 || len(d.RecentInvoices) != 0 || len(d.Activity) != 0 || len(d.UpcomingRuns) != 0 || d.FailedEmailCount != 0 {
		t.Fatalf("dashboard-only user sees more than totals: %+v", d)
	}
	if len(d.Totals) == 0 {
		t.Fatal("the totals stay")
	}
}

func TestOutboxListWithoutTheOutboxPermission(t *testing.T) {
	h := newHarness(t).seed()
	ctx := as(h.ctx, domain.PermStatementsView)
	if _, _, err := h.svc.ListEmails(ctx, ports.EmailFilter{}); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("the whole outbox: %v", err)
	}
	inv := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{}))
	must(h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{}))
	list, _ := must3(h.svc.ListEmails(ctx, ports.EmailFilter{CustomerID: h.customer.ID, InvoiceID: inv.ID}))
	if len(list) != 1 || list[0].Kind != domain.EmailKindStatement {
		t.Fatalf("only the customer's statements: %+v", list)
	}
}

func TestSchedulesNeedTheRightsOfWhatTheyDo(t *testing.T) {
	h := newHarness(t).seed()
	invoiceSchedule := domain.Schedule{
		CustomerID: h.customer.ID, Name: "monthly", AutoSend: true,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
	}
	statement := domain.Schedule{
		CustomerID: h.customer.ID, Name: "statement", Kind: domain.ScheduleEmail, EmailScope: domain.EmailStatement,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
	}
	only := as(h.ctx, domain.PermSchedulesCreate)
	for _, sc := range []domain.Schedule{invoiceSchedule, statement} {
		if _, err := h.svc.CreateSchedule(only, sc); !errors.Is(err, domain.ErrNoAccess) {
			t.Fatalf("%s: schedules.edit alone must not invoice or e-mail: %v", sc.Name, err)
		}
	}
	if _, err := h.svc.CreateSchedule(as(h.ctx, domain.PermSchedulesCreate, domain.PermInvoicesCreate, domain.PermInvoicesIssue), invoiceSchedule); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("auto-send also needs invoices.send: %v", err)
	}
	d := must(h.svc.CreateSchedule(as(h.ctx, domain.PermSchedulesCreate, domain.PermInvoicesCreate, domain.PermInvoicesIssue, domain.PermInvoicesSend), invoiceSchedule))
	if _, err := h.svc.RunScheduleNow(only, d.Schedule.ID); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("run now: %v", err)
	}
	must(h.svc.CreateSchedule(as(h.ctx, domain.PermSchedulesCreate, domain.PermStatementsSend), statement))
}

func TestUnsentScheduleSkipsAnInvoiceWithAQueuedEmail(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{SendAt: h.clock.Now().Add(24 * time.Hour)})) // queued for tomorrow
	d := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "unsent", Kind: domain.ScheduleEmail, EmailScope: domain.EmailUnsent,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Monday, StartDate: domain.NewDate(2026, 8, 1)},
	}))
	must(h.svc.RunScheduleNow(h.ctx, d.Schedule.ID))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Emailed != 0 {
		t.Fatalf("queued a second e-mail of the same invoice: %+v", res)
	}
	if _, n := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{InvoiceID: inv.ID})); n != 1 {
		t.Fatalf("e-mails of the invoice = %d", n)
	}
}

func TestSendKeyBelongsToItsInvoice(t *testing.T) {
	h := newHarness(t).seed()
	a := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	b := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	must(h.svc.SendInvoice(h.ctx, a.ID, app.SendInput{IdempotencyKey: "k"}))
	if _, err := h.svc.SendInvoice(h.ctx, b.ID, app.SendInput{IdempotencyKey: "k"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a key reused for another invoice answered with the first one's e-mail: %v", err)
	}
	if _, err := h.svc.SendStatement(h.ctx, h.customer.ID, app.SendInput{IdempotencyKey: "k"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a statement with an invoice's key: %v", err)
	}
}

func TestFailedTestEmailIsAuditedAsFailed(t *testing.T) {
	h := newHarness(t).seed()
	h.mailer.Err = errors.New("smtp down")
	_, err := h.svc.SendTestEmail(h.ctx, "x@example.com")
	var me *app.MailError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v", err)
	}
	if h.auditCount("settings.test_email_sent") != 0 || h.auditCount("settings.test_email_failed") != 1 {
		t.Fatal("a failed test e-mail is not recorded as sent")
	}
}

func TestAFailingExportDoesNotBlockTheOthers(t *testing.T) {
	h := newHarness(t).seed()
	arch := &fakes.Archive{}
	svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: &failFor{fakes.Renderer{}}, Hasher: fakes.Hasher{}, Archive: arch})
	r := svc
	bad := must(r.IssueInvoice(h.ctx, must(r.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	failing[bad.Number] = true
	defer delete(failing, bad.Number)
	h.clock.T = h.clock.T.Add(time.Second)
	good := must(r.IssueInvoice(h.ctx, must(r.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	if res := must(r.ExportInvoices(h.ctx, 1)); res.Failed != 1 {
		t.Fatalf("first pass = %+v", res)
	}
	if res := must(r.ExportInvoices(h.ctx, 1)); res.Exported != 1 {
		t.Fatalf("the failing invoice must not block the next one: %+v", res)
	}
	if _, ok := arch.Files[app.ArchivePath(good)]; !ok {
		t.Fatal("the good invoice is archived")
	}
	h.clock.T = h.clock.T.Add(time.Hour)
	delete(failing, bad.Number)
	if res := must(r.ExportInvoices(h.ctx, 5)); res.Exported != 1 {
		t.Fatalf("the failed one is tried again later: %+v", res)
	}
}

// failing lists invoice numbers whose PDF cannot be rendered (for failFor).
var failing = map[string]bool{}

type failFor struct{ fakes.Renderer }

func (f *failFor) RenderPDF(ctx context.Context, doc ports.InvoiceDocument) ([]byte, error) {
	if failing[doc.Invoice.Number] {
		return nil, errors.New("render failed")
	}
	return f.Renderer.RenderPDF(ctx, doc)
}
