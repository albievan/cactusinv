package app_test

import (
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// The dashboard's quote figures, quotes running out, next scheduled invoices and invoices
// falling due (M39), each shown only to whom may open them.
func TestDashboardQuotesUpcomingAndDueSoon(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	declined := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	must(h.svc.DeclineQuote(h.ctx, declined.ID, "no"))
	accepted := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	must(h.svc.AcceptQuote(h.ctx, accepted.ID))
	must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
		CustomerID: h.customer.ID, Name: "Monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
	}))
	inv := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))

	d := must(h.svc.GetDashboard(h.ctx))
	if len(d.QuoteStats) != 1 {
		t.Fatalf("quote stats = %+v", d.QuoteStats)
	}
	s := d.QuoteStats[0]
	if s.Currency != "ZAR" || s.Open != 1 || s.OpenValue != q.Totals.Total || s.Accepted != 1 || s.AcceptedValue != accepted.Totals.Total ||
		s.Declined != 1 || s.Won() != 1 || s.Decided() != 2 || s.IssuedThisMonth != 3 || s.ExpiringSoon != 0 {
		t.Fatalf("stats = %+v", s)
	}
	if len(d.ExpiringQuotes) != 0 || len(d.UpcomingInvoices) != 1 || d.UpcomingInvoices[0].Schedule.Name != "Monthly" || d.UpcomingInvoices[0].Invoice == nil {
		t.Fatalf("expiring %d, upcoming %+v", len(d.ExpiringQuotes), d.UpcomingInvoices)
	}

	// A few days before the quote's last valid day and the invoice's due date.
	day := func(dt domain.Date) time.Time { return time.Date(dt.Year, dt.Month, dt.Day, 10, 0, 0, 0, time.UTC) }
	h.clock.Set(day(q.ValidUntil.AddDays(-3)))
	d = must(h.svc.GetDashboard(h.ctx))
	if len(d.ExpiringQuotes) != 1 || d.ExpiringQuotes[0].ID != q.ID || d.QuoteStats[0].ExpiringSoon != 1 {
		t.Fatalf("expiring = %+v", d.ExpiringQuotes)
	}
	h.clock.Set(day(inv.DueDate.AddDays(-2)))
	d = must(h.svc.GetDashboard(h.ctx))
	if len(d.DueSoon) != 1 || d.DueSoon[0].ID != inv.ID {
		t.Fatalf("due soon = %+v (due %s)", d.DueSoon, inv.DueDate)
	}

	// Only the totals for a user who may see the dashboard and nothing else.
	only := must(h.svc.GetDashboard(as(h.ctx, domain.PermDashboardView)))
	if len(only.QuoteStats) != 0 || len(only.ExpiringQuotes) != 0 || len(only.UpcomingInvoices) != 0 || len(only.DueSoon) != 0 {
		t.Fatalf("dashboard-only user sees %+v", only)
	}
	// Quotes alone.
	sales := must(h.svc.GetDashboard(app.WithActor(h.ctx, actorWith(domain.PermDashboardView, domain.PermQuotesView))))
	if len(sales.QuoteStats) != 1 || len(sales.DueSoon) != 0 || len(sales.UpcomingInvoices) != 0 {
		t.Fatalf("quotes user sees %+v", sales)
	}
}
