package app

import (
	"context"
	"sort"

	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
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
	return d, nil
}

// OutboxStats exposes outbox depth for metrics.
func (s *Service) OutboxStats(ctx context.Context) (ports.OutboxStats, error) {
	st, err := s.tx.Repos().Emails.Stats(ctx)
	return st, wrap("app.OutboxStats", err)
}
