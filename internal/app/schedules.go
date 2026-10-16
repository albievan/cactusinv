package app

import (
	"context"
	"errors"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// timeT keeps method-expression signatures short.
type timeT = time.Time

// ScheduleDetail is a schedule with its next run dates and recent runs. LineProducts holds
// one entry per line: the line's product as it is now (name, state, price in the
// customer's currency), read from the product look-up.
type ScheduleDetail struct {
	Schedule     *domain.Schedule
	LineProducts []domain.ScheduleLineProduct
	Upcoming     []time.Time
	Runs         []domain.ScheduleRun
}

// ScheduleView is a listed schedule with its lines' products as they are now (see
// ScheduleDetail.LineProducts).
type ScheduleView struct {
	domain.Schedule
	LineProducts []domain.ScheduleLineProduct
}

// ListSchedules returns a page of schedules with their lines' products.
func (s *Service) ListSchedules(ctx context.Context, f ports.ScheduleFilter) ([]ScheduleView, int, error) {
	f.Page = f.Page.Normalize()
	r := s.tx.Repos()
	ss, n, err := r.Schedules.List(ctx, f)
	if err != nil {
		return nil, 0, wrap("app.ListSchedules", err)
	}
	ptrs := make([]*domain.Schedule, len(ss))
	for i := range ss {
		ptrs[i] = &ss[i]
	}
	views, err := lineProducts(ctx, r, ptrs)
	if err != nil {
		return nil, 0, wrap("app.ListSchedules", err)
	}
	out := make([]ScheduleView, len(ss))
	for i := range ss {
		out[i] = ScheduleView{Schedule: ss[i], LineProducts: views[i]}
	}
	return out, n, nil
}

// lineProducts describes every line's product of each schedule, priced in the currency
// of the schedule's customer. Products are read from the look-up, never from the line's
// stored description.
func lineProducts(ctx context.Context, r ports.Repos, ss []*domain.Schedule) ([][]domain.ScheduleLineProduct, error) {
	var ids, customerIDs []string
	for _, sc := range ss {
		if len(sc.Lines) == 0 {
			continue
		}
		customerIDs = append(customerIDs, sc.CustomerID)
		for _, l := range sc.Lines {
			if l.ProductID != "" {
				ids = append(ids, l.ProductID)
			}
		}
	}
	customers, err := r.Customers.GetMany(ctx, customerIDs)
	if err != nil {
		return nil, err
	}
	currencies := make(map[string]domain.CurrencyCode, len(customers))
	for id, c := range customers {
		currencies[id] = c.Currency
	}
	products, err := r.Products.GetMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([][]domain.ScheduleLineProduct, len(ss))
	for i, sc := range ss {
		out[i] = make([]domain.ScheduleLineProduct, len(sc.Lines))
		for j, l := range sc.Lines {
			out[i][j] = l.ProductView(products, currencies[sc.CustomerID])
		}
	}
	return out, nil
}

// GetSchedule returns a schedule with its lines' products and a preview of its next five
// runs.
func (s *Service) GetSchedule(ctx context.Context, id string) (ScheduleDetail, error) {
	r := s.tx.Repos()
	sc, err := r.Schedules.Get(ctx, id)
	if err != nil {
		return ScheduleDetail{}, wrap("app.GetSchedule", err)
	}
	runs, err := r.Schedules.ListRuns(ctx, id, 20)
	if err != nil {
		return ScheduleDetail{}, wrap("app.GetSchedule", err)
	}
	views, err := lineProducts(ctx, r, []*domain.Schedule{sc})
	if err != nil {
		return ScheduleDetail{}, wrap("app.GetSchedule", err)
	}
	var upcoming []time.Time
	if sc.Status == domain.ScheduleActive {
		upcoming = sc.UpcomingRuns(5)
	}
	return ScheduleDetail{Schedule: sc, LineProducts: views[0], Upcoming: upcoming, Runs: runs}, nil
}

// RunPreview is the next run times of an unsaved recurrence and the time zone they were
// computed in.
type RunPreview struct {
	Runs     []time.Time
	TimeZone string
	Weekend  domain.WeekendRule
}

// PreviewRecurrence lists the next n run times of an unsaved recurrence, after its weekend
// rule. A recurrence without a time zone is computed in the business time zone and one
// without a weekend rule gets the default, as creating it would.
func (s *Service) PreviewRecurrence(ctx context.Context, r domain.Recurrence, n int) (RunPreview, error) {
	if r.TimeZone == "" {
		b, err := business(ctx, s.tx.Repos())
		if err != nil {
			return RunPreview{}, wrap("app.PreviewRecurrence", err)
		}
		r.TimeZone = b.TimeZone
	}
	r.DefaultWeekendRule() // as creating it would
	if err := r.Validate(); err != nil {
		return RunPreview{}, wrap("app.PreviewRecurrence", err)
	}
	return RunPreview{Runs: r.Upcoming(s.now(), min(max(n, 1), 24)), TimeZone: r.TimeZone, Weekend: r.Weekend}, nil
}

// ScheduleEmailDefaults returns the suggested e-mail subject and body for a new schedule
// of each type; the recurring-invoice type uses the business's own invoice e-mail texts
// when it has set them.
func (s *Service) ScheduleEmailDefaults(ctx context.Context) (domain.ScheduleEmailDefaults, error) {
	b, err := business(ctx, s.tx.Repos())
	if err != nil {
		return domain.ScheduleEmailDefaults{}, wrap("app.ScheduleEmailDefaults", err)
	}
	return domain.ScheduleEmailsFor(b), nil
}

// CreateSchedule saves a new active schedule for a customer.
func (s *Service) CreateSchedule(ctx context.Context, sc domain.Schedule) (ScheduleDetail, error) {
	sc.ID = s.ids.NewID()
	sc.CreatedAt = s.now()
	sc.Recurrence.DefaultWeekendRule()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := s.checkSchedule(ctx, r, &sc); err != nil {
			return err
		}
		sc.Activate(s.now())
		if err := r.Schedules.Create(ctx, &sc); err != nil {
			return err
		}
		return s.audit(ctx, r, "schedule.created", "schedule", sc.ID, sc.Name)
	})
	if err != nil {
		return ScheduleDetail{}, wrap("app.CreateSchedule", err)
	}
	return s.GetSchedule(ctx, sc.ID)
}

// UpdateSchedule edits a schedule. An active schedule's next run is recomputed from now.
func (s *Service) UpdateSchedule(ctx context.Context, sc domain.Schedule) (ScheduleDetail, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.Schedules.Get(ctx, sc.ID)
		if err != nil {
			return err
		}
		if cur.Version != sc.Version {
			return domain.ErrConflict
		}
		if sc.Recurrence.Weekend == "" { // a client that does not know the rule keeps it
			sc.Recurrence.Weekend = cur.Recurrence.Weekend
		}
		if err := s.checkSchedule(ctx, r, &sc); err != nil {
			return err
		}
		// The run state is the server's: a paused schedule keeps its pending occurrence and
		// failure count (Resume decides from them); an active one keeps an unrun overdue
		// occurrence (ActivateFrom).
		sc.Status, sc.CreatedAt, sc.LastRunAt = cur.Status, cur.CreatedAt, cur.LastRunAt
		sc.FailureCount, sc.LastError, sc.NextOccurrence, sc.NextRunAt = cur.FailureCount, cur.LastError, cur.NextOccurrence, cur.NextRunAt
		sc.UpdatedAt = s.now()
		if cur.Status != domain.SchedulePaused {
			sc.ActivateFrom(cur.NextOccurrence, s.now())
		}
		if err := r.Schedules.Update(ctx, &sc); err != nil {
			return err
		}
		return s.audit(ctx, r, "schedule.updated", "schedule", sc.ID, sc.Name)
	})
	if err != nil {
		return ScheduleDetail{}, wrap("app.UpdateSchedule", err)
	}
	return s.GetSchedule(ctx, sc.ID)
}

func (s *Service) checkSchedule(ctx context.Context, r ports.Repos, sc *domain.Schedule) error {
	if sc.Recurrence.TimeZone == "" {
		b, err := business(ctx, r)
		if err != nil {
			return err
		}
		sc.Recurrence.TimeZone = b.TimeZone
	}
	if err := sc.Validate(); err != nil {
		return err
	}
	c, err := r.Customers.Get(ctx, sc.CustomerID)
	if err != nil {
		return err
	}
	var ids []string
	for _, l := range sc.Lines {
		if l.ProductID != "" {
			ids = append(ids, l.ProductID)
		}
	}
	products, err := r.Products.GetMany(ctx, ids)
	if err != nil {
		return err
	}
	for _, l := range sc.Lines {
		if l.ProductID == "" {
			return &domain.ValidationError{Field: "lines.productId", Reason: "choose a product for every line"}
		}
		p, ok := products[l.ProductID]
		if !ok {
			return &domain.ValidationError{Field: "lines.productId", Reason: "unknown product"}
		}
		if _, err := p.PriceIn(c.Currency); err != nil && l.UnitPrice == nil {
			return err
		}
	}
	return nil
}

// DeleteSchedule removes a schedule. Invoices it created are kept.
func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	return wrap("app.DeleteSchedule", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		sc, err := r.Schedules.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := r.Schedules.Delete(ctx, id); err != nil {
			return err
		}
		return s.audit(ctx, r, "schedule.deleted", "schedule", id, sc.Name)
	}))
}

// PauseSchedule stops a schedule.
func (s *Service) PauseSchedule(ctx context.Context, id string) (ScheduleDetail, error) {
	return s.scheduleTransition(ctx, "app.PauseSchedule", "schedule.paused", id, func(sc *domain.Schedule) error { return sc.Pause(s.now()) })
}

// ResumeSchedule restarts a paused schedule from the next occurrence after now.
func (s *Service) ResumeSchedule(ctx context.Context, id string) (ScheduleDetail, error) {
	return s.scheduleTransition(ctx, "app.ResumeSchedule", "schedule.resumed", id, func(sc *domain.Schedule) error { return sc.Resume(s.now()) })
}

// RunScheduleNow makes the schedule's pending occurrence due immediately: the next
// invoice is issued (and sent if auto-send) on the runner's next pass.
func (s *Service) RunScheduleNow(ctx context.Context, id string) (ScheduleDetail, error) {
	return s.scheduleTransition(ctx, "app.RunScheduleNow", "schedule.run_now", id, func(sc *domain.Schedule) error {
		if sc.Status != domain.ScheduleActive {
			return domain.ErrInvalidTransition
		}
		sc.NextRunAt = s.now()
		return nil
	})
}

func (s *Service) scheduleTransition(ctx context.Context, op, action, id string, apply func(*domain.Schedule) error) (ScheduleDetail, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		sc, err := r.Schedules.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := apply(sc); err != nil {
			return err
		}
		if err := r.Schedules.Update(ctx, sc); err != nil {
			return err
		}
		return s.audit(ctx, r, action, "schedule", sc.ID, sc.Name)
	})
	if err != nil {
		return ScheduleDetail{}, wrap(op, err)
	}
	return s.GetSchedule(ctx, id)
}

// RunResult counts what one runner pass did.
type RunResult struct {
	Issued  int
	Emailed int // e-mails queued by e-mail schedules
	Failed  int
}

// RunDueSchedules processes up to limit due schedule occurrences (ADR-004). Each
// occurrence is processed in its own transaction; the (schedule, period) unique run
// record guarantees at most one invoice per period even with concurrent runners.
func (s *Service) RunDueSchedules(ctx context.Context, limit int) (RunResult, error) {
	var res RunResult
	for range limit {
		if stopping(ctx) {
			return res, nil
		}
		var claimed *domain.Schedule
		err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
			due, err := r.Schedules.ClaimDue(ctx, s.now(), 1)
			if err != nil || len(due) == 0 {
				return err
			}
			claimed = due[0]
			return s.runOccurrence(ctx, r, claimed, &res)
		})
		if claimed == nil {
			return res, wrap("app.RunDueSchedules", err)
		}
		if err != nil {
			res.Failed++
			s.log.WarnContext(ctx, "schedule run failed", "schedule_id", claimed.ID, "period", claimed.Period().String(), "err", err)
			if ferr := s.recordScheduleFailure(ctx, claimed.ID, err); ferr != nil {
				return res, wrap("app.RunDueSchedules", ferr)
			}
		}
	}
	return res, nil
}

func (s *Service) runOccurrence(ctx context.Context, r ports.Repos, sc *domain.Schedule, res *RunResult) error {
	period := sc.Period()
	invoiceID := ""
	if sc.Kind != domain.ScheduleEmail {
		invoiceID = s.ids.NewID()
	}
	err := r.Schedules.InsertRun(ctx, domain.ScheduleRun{ScheduleID: sc.ID, Period: period, PeriodKey: sc.PeriodKey(), InvoiceID: invoiceID, At: s.now()})
	switch {
	case errors.Is(err, domain.ErrConflict):
		// This billing period already ran (a previous run committed but did not advance, or
		// the day was moved within the period after it ran): just advance.
		sc.Advance(s.now())
		return r.Schedules.Update(ctx, sc)
	case err != nil:
		return err
	}
	if sc.Kind == domain.ScheduleEmail {
		return s.runEmailOccurrence(ctx, r, sc, period, res)
	}
	c, err := r.Customers.Get(ctx, sc.CustomerID)
	if err != nil {
		return err
	}
	if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
		return err
	}
	var ids []string
	for _, l := range sc.Lines {
		if l.ProductID != "" {
			ids = append(ids, l.ProductID)
		}
	}
	products, err := r.Products.GetMany(ctx, ids)
	if err != nil {
		return err
	}
	b, err := business(ctx, r)
	if err != nil {
		return err
	}
	issueDate := sc.IssueDate(s.today(b))
	content, err := sc.BuildDraft(c, products, b.VATRate, issueDate)
	if err != nil {
		return err
	}
	inv, err := domain.NewDraftInvoice(invoiceID, c, content, s.now())
	if err != nil {
		return err
	}
	inv.ScheduleID = sc.ID
	if err := r.Invoices.Create(ctx, inv); err != nil {
		return err
	}
	if err := s.event(ctx, r, inv.ID, "created", "schedule "+sc.Name); err != nil {
		return err
	}
	if err := s.issue(ctx, r, inv, issueDate); err != nil {
		return err
	}
	if sc.AutoSend {
		key := "schedule:" + sc.ID + ":" + period.String()
		if _, err := s.enqueue(ctx, r, inv, SendInput{Subject: sc.EmailSubject, Body: sc.EmailBody, IdempotencyKey: key, SendAt: sc.EmailAt(s.now())}); err != nil {
			return err
		}
	}
	sc.Advance(s.now())
	if err := r.Schedules.Update(ctx, sc); err != nil {
		return err
	}
	res.Issued++
	return nil
}

// runEmailOccurrence queues an e-mail for each of the customer's invoices selected by the
// schedule's scope (never-e-mailed, or all outstanding). Must run in a tx.
func (s *Service) runEmailOccurrence(ctx context.Context, r ports.Repos, sc *domain.Schedule, period domain.Date, res *RunResult) error {
	if sc.EmailScope == domain.EmailStatement {
		doc, err := s.statementDoc(ctx, r, sc.CustomerID)
		if err != nil {
			return err
		}
		if doc.Statement.Total > 0 { // nothing owed: no statement
			key := "schedule:" + sc.ID + ":" + period.String()
			if _, err := s.enqueueStatement(ctx, r, sc.CustomerID, SendInput{Subject: sc.EmailSubject, Body: sc.EmailBody, IdempotencyKey: key}); err != nil {
				return err
			}
			res.Emailed++
		}
		sc.Advance(s.now())
		return r.Schedules.Update(ctx, sc)
	}
	invs, _, err := r.Invoices.List(ctx, ports.InvoiceFilter{CustomerID: sc.CustomerID, Page: ports.Page{Limit: 200}})
	if err != nil {
		return err
	}
	for i := range invs {
		if !sc.EmailScope.Selects(&invs[i]) {
			continue
		}
		key := "schedule:" + sc.ID + ":" + period.String() + ":" + invs[i].ID
		if _, err := r.Emails.GetByIdempotencyKey(ctx, key); err == nil {
			continue
		}
		if _, err := s.enqueue(ctx, r, &invs[i], SendInput{Subject: sc.EmailSubject, Body: sc.EmailBody, IdempotencyKey: key}); err != nil {
			return err
		}
		res.Emailed++
	}
	sc.Advance(s.now())
	return r.Schedules.Update(ctx, sc)
}

func (s *Service) recordScheduleFailure(ctx context.Context, id string, cause error) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		sc, err := r.Schedules.Get(ctx, id)
		if err != nil {
			return err
		}
		sc.RecordFailure(cause.Error(), s.now())
		if err := r.Schedules.Update(ctx, sc); err != nil {
			return err
		}
		return s.audit(ctx, r, "schedule.run_failed", "schedule", sc.ID, sc.LastError)
	})
}
