package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/ports/fakes"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// One action, one permission (Albie, 2026-10-16): "add a customer but not edit; create an
// invoice but not edit, issue or delete".
func TestEachActionHasItsOwnPermission(t *testing.T) {
	writes := map[domain.Permission][]string{
		domain.PermInvoicesCreate:     {"createInvoice"},
		domain.PermInvoicesEdit:       {"updateInvoice"},
		domain.PermInvoicesIssue:      {"issueInvoice"},
		domain.PermInvoicesDelete:     {"deleteInvoice"},
		domain.PermInvoicesSend:       {"sendInvoice"},
		domain.PermInvoicesPay:        {"markInvoicePaid"},
		domain.PermInvoicesOverdue:    {"markInvoiceOverdue"},
		domain.PermInvoicesVoid:       {"voidInvoice"},
		domain.PermCustomersCreate:    {"createCustomer"},
		domain.PermCustomersEdit:      {"updateCustomer"},
		domain.PermCustomersDelete:    {"deleteCustomer"},
		domain.PermProductsCreate:     {"createProduct"},
		domain.PermProductsEdit:       {"updateProduct"},
		domain.PermProductsDelete:     {"deleteProduct"},
		domain.PermSchedulesCreate:    {"createSchedule"},
		domain.PermSchedulesEdit:      {"updateSchedule"},
		domain.PermSchedulesPause:     {"pauseSchedule", "resumeSchedule"},
		domain.PermSchedulesRun:       {"runScheduleNow"},
		domain.PermSchedulesDelete:    {"deleteSchedule"},
		domain.PermStatementsSend:     {"sendStatement", "sendStatements"},
		domain.PermEmailsCancel:       {"cancelEmail"},
		domain.PermEmailsRetry:        {"retryEmail"},
		domain.PermSettingsBusiness:   {"updateBusiness"},
		domain.PermSettingsEmail:      {"updateEmailSettings", "sendTestEmail"},
		domain.PermSettingsCurrencies: {"createCurrency", "updateCurrency"},
		domain.PermSettingsBanks:      {"createBankAccount", "updateBankAccount", "deleteBankAccount"},
	}
	for p := range writes {
		if !p.Known() {
			t.Fatalf("%s is not in the catalogue", p)
		}
		u := domain.User{Access: domain.AccessFor([]domain.Group{{ID: "g", Permissions: []domain.Permission{p}}})}
		for q, others := range writes {
			for _, op := range others {
				err := app.SessionAllows(u, op)
				if q == p && err != nil {
					t.Errorf("%s must allow %s: %v", p, op, err)
				}
				if q != p && !errors.Is(err, domain.ErrNoAccess) {
					t.Errorf("%s alone must not allow %s", p, op)
				}
			}
		}
	}
}

func TestSchedulePermissionsDoNotGrantWhatTheScheduleDoes(t *testing.T) {
	h := newHarness(t).seed()
	sc := domain.Schedule{
		CustomerID: h.customer.ID, Name: "monthly", Kind: domain.ScheduleInvoice,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
	}
	// Creating draft invoices is not issuing them: a schedule raises issued invoices.
	if _, err := h.svc.CreateSchedule(as(h.ctx, domain.PermSchedulesCreate, domain.PermInvoicesCreate), sc); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("a schedule must also need invoices.issue: %v", err)
	}
	must(h.svc.CreateSchedule(as(h.ctx, domain.PermSchedulesCreate, domain.PermInvoicesCreate, domain.PermInvoicesIssue), sc))
}

// Albie, 2026-10-16: a weekly schedule set for 08:00 ran at 08:00 UTC (10:00 in
// Johannesburg) because the business zone had defaulted to UTC. Before the profile is
// saved, the business zone is now the deployment's (TZ).
func TestBusinessZoneDefaultsToTheDeploymentZone(t *testing.T) {
	store := fakes.NewStore()
	svc := app.New(app.Deps{Tx: store, Clock: &fakes.Clock{T: time.Date(2026, 10, 16, 5, 0, 0, 0, time.UTC)}, IDs: &fakes.IDs{}, TimeZone: "Africa/Johannesburg"})
	b := must(svc.GetBusiness(context.Background()))
	if b.Profile.TimeZone != "Africa/Johannesburg" {
		t.Fatalf("zone %q, want the deployment's", b.Profile.TimeZone)
	}
	pv := must(svc.PreviewRecurrence(context.Background(), domain.Recurrence{Frequency: domain.FrequencyWeekly, Interval: 1, Weekday: time.Friday, SendHour: 8, StartDate: domain.NewDate(2026, 10, 16)}, 1))
	if want := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC); pv.TimeZone != "Africa/Johannesburg" || !pv.Runs[0].Equal(want) {
		t.Fatalf("first run %v in %s, want 08:00 Johannesburg (%v)", pv.Runs, pv.TimeZone, want)
	}
	bad := app.New(app.Deps{Tx: fakes.NewStore(), Clock: &fakes.Clock{}, IDs: &fakes.IDs{}, TimeZone: "Mars/Olympus"})
	if z := must(bad.GetBusiness(context.Background())).Profile.TimeZone; z != "UTC" {
		t.Fatalf("an unknown deployment zone falls back to UTC, got %q", z)
	}
}
