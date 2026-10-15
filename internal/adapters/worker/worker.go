// Package worker drives the background use cases on timers: the schedule runner, the
// e-mail dispatcher (ADR-004), the invoice exporter and housekeeping. Every replica may
// run them; row locks make that safe.
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/albievan/cactusinv/internal/app"
)

// Intervals between passes.
type Intervals struct {
	Schedules    time.Duration
	Emails       time.Duration
	Export       time.Duration
	Housekeeping time.Duration
}

// DefaultIntervals are used for zero fields.
var DefaultIntervals = Intervals{Schedules: time.Minute, Emails: 15 * time.Second, Export: 30 * time.Second, Housekeeping: time.Hour}

// Runner runs the loops until its context is cancelled.
type Runner struct {
	svc       *app.Service
	log       *slog.Logger
	every     Intervals
	runs      *prometheus.CounterVec
	sent      *prometheus.CounterVec
	exported  *prometheus.CounterVec
	pending   prometheus.Gauge
	oldestAge prometheus.Gauge
}

// New builds a Runner and registers its metrics.
func New(svc *app.Service, log *slog.Logger, every Intervals, reg prometheus.Registerer) *Runner {
	if every.Schedules == 0 {
		every.Schedules = DefaultIntervals.Schedules
	}
	if every.Emails == 0 {
		every.Emails = DefaultIntervals.Emails
	}
	if every.Export == 0 {
		every.Export = DefaultIntervals.Export
	}
	if every.Housekeeping == 0 {
		every.Housekeeping = DefaultIntervals.Housekeeping
	}
	r := &Runner{
		svc: svc, log: log, every: every,
		runs:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cactusinv_schedule_runs_total", Help: "Schedule occurrences processed."}, []string{"result"}),
		sent:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cactusinv_emails_sent_total", Help: "Outbox delivery attempts."}, []string{"result"}),
		exported:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cactusinv_invoices_exported_total", Help: "Invoice PDFs written to the archive."}, []string{"result"}),
		pending:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "cactusinv_outbox_pending", Help: "Pending outbox messages."}),
		oldestAge: prometheus.NewGauge(prometheus.GaugeOpts{Name: "cactusinv_outbox_oldest_pending_seconds", Help: "Age of the oldest due pending message."}),
	}
	reg.MustRegister(r.runs, r.sent, r.exported, r.pending, r.oldestAge)
	return r
}

// Run blocks until ctx is cancelled and the current passes finish.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	loop := func(name string, every time.Duration, pass func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				pass(ctx)
				select {
				case <-ctx.Done():
					r.log.Info("worker stopped", "worker", name)
					return
				case <-t.C:
				}
			}
		}()
	}
	loop("schedules", r.every.Schedules, r.Schedules)
	loop("emails", r.every.Emails, r.Emails)
	loop("export", r.every.Export, r.Export)
	loop("housekeeping", r.every.Housekeeping, r.Housekeeping)
	wg.Wait()
}

// Schedules runs one schedule pass.
func (r *Runner) Schedules(ctx context.Context) {
	res, err := r.svc.RunDueSchedules(app.WithActor(ctx, app.ActorScheduler), 50)
	r.runs.WithLabelValues("issued").Add(float64(res.Issued))
	r.runs.WithLabelValues("failed").Add(float64(res.Failed))
	if err != nil {
		r.log.ErrorContext(ctx, "schedule runner", "err", err)
	}
}

// Emails runs one dispatcher pass and refreshes the outbox gauges.
func (r *Runner) Emails(ctx context.Context) {
	res, err := r.svc.DispatchEmails(app.WithActor(ctx, app.ActorDispatcher), 50)
	r.sent.WithLabelValues("sent").Add(float64(res.Sent))
	r.sent.WithLabelValues("failed").Add(float64(res.Failed))
	if err != nil {
		r.log.ErrorContext(ctx, "email dispatcher", "err", err)
	}
	if st, err := r.svc.OutboxStats(ctx); err == nil {
		r.pending.Set(float64(st.Pending))
		age := 0.0
		if !st.OldestPending.IsZero() && time.Since(st.OldestPending) > 0 {
			age = time.Since(st.OldestPending).Seconds()
		}
		r.oldestAge.Set(age)
	}
}

// Export runs one invoice-archive pass.
func (r *Runner) Export(ctx context.Context) {
	res, err := r.svc.ExportInvoices(app.WithActor(ctx, app.ActorExporter), 100)
	r.exported.WithLabelValues("ok").Add(float64(res.Exported))
	r.exported.WithLabelValues("failed").Add(float64(res.Failed))
	if err != nil {
		r.log.ErrorContext(ctx, "invoice export", "err", err)
	}
}

// Housekeeping deletes expired sessions and idempotency keys.
func (r *Runner) Housekeeping(ctx context.Context) {
	if err := r.svc.Housekeeping(ctx); err != nil {
		r.log.ErrorContext(ctx, "housekeeping", "err", err)
	}
}
