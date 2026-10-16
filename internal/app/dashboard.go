package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Dashboard is the home-screen summary. Totals are per currency; currencies are never
// summed together (ADR-003).
type Dashboard struct {
	Today            domain.Date
	Totals           []ports.CurrencyTotals
	UpcomingEmails   []domain.EmailMessage
	UpcomingRuns     []domain.Schedule
	RecentInvoices   []domain.Invoice
	FailedEmailCount int
	// Months are the last 12 months ("2026-10"), oldest first; Invoiced and Paid are
	// per-currency monthly totals within them.
	Months     []string
	Invoiced   []ports.MonthTotal
	Paid       []ports.MonthTotal
	TopDebtors []ports.Debtor
	Activity   []domain.AuditEntry
}

// GetDashboard assembles the dashboard.
func (s *Service) GetDashboard(ctx context.Context) (Dashboard, error) {
	r := s.tx.Repos()
	b, err := business(ctx, r)
	if err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	d := Dashboard{Today: s.today(b)}
	if d.Totals, err = r.Invoices.Totals(ctx, d.Today); err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	pending, _, err := r.Emails.List(ctx, ports.EmailFilter{Status: domain.EmailPending, Page: ports.Page{Limit: 200}})
	if err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].NextAttemptAt.Before(pending[j].NextAttemptAt) })
	d.UpcomingEmails = pending[:min(5, len(pending))]
	_, d.FailedEmailCount, err = r.Emails.List(ctx, ports.EmailFilter{Status: domain.EmailFailed, Page: ports.Page{Limit: 1}})
	if err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	active, _, err := r.Schedules.List(ctx, ports.ScheduleFilter{Status: domain.ScheduleActive, Page: ports.Page{Limit: 200}})
	if err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	sort.Slice(active, func(i, j int) bool { return active[i].NextRunAt.Before(active[j].NextRunAt) })
	d.UpcomingRuns = active[:min(5, len(active))]
	if d.RecentInvoices, _, err = r.Invoices.List(ctx, ports.InvoiceFilter{Today: d.Today, Page: ports.Page{Limit: 5}}); err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	first := domain.NewDate(d.Today.Year, d.Today.Month-11, 1)
	for i := range 12 {
		m := domain.NewDate(first.Year, first.Month+time.Month(i), 1)
		d.Months = append(d.Months, fmt.Sprintf("%04d-%02d", m.Year, int(m.Month)))
	}
	if d.Invoiced, d.Paid, err = r.Invoices.Monthly(ctx, first, b.Location()); err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	if d.TopDebtors, err = r.Invoices.TopDebtors(ctx, 6); err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	if d.Activity, _, err = r.Audit.List(ctx, ports.AuditFilter{Page: ports.Page{Limit: 12}}); err != nil {
		return Dashboard{}, wrap("app.GetDashboard", err)
	}
	// The dashboard shows only what the user may also open (ADR-012).
	a := ActorFrom(ctx)
	if !a.Can(domain.PermEmailsView) {
		d.UpcomingEmails, d.FailedEmailCount = []domain.EmailMessage{}, 0
	}
	if !a.Can(domain.PermSchedulesView) {
		d.UpcomingRuns = []domain.Schedule{}
	}
	if !a.Can(domain.PermInvoicesView) {
		d.RecentInvoices = []domain.Invoice{}
	}
	if !a.Can(domain.PermAuditView) {
		d.Activity = []domain.AuditEntry{}
	}
	return d, nil
}

// OutboxStats exposes outbox depth for metrics.
func (s *Service) OutboxStats(ctx context.Context) (ports.OutboxStats, error) {
	st, err := s.tx.Repos().Emails.Stats(ctx)
	return st, wrap("app.OutboxStats", err)
}
