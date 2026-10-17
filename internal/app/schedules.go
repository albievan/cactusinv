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
		b, err := s.business(ctx, s.tx.Repos())
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

// PreviewScheduleEdit lists the next n run times the schedule id would have with the
// recurrence r, as saving it would set them: an unrun overdue period is kept and billed
// periods are not run again (ActivateFrom); a paused schedule shows them as resuming now
// would. A recurrence without a weekend rule keeps the schedule's.
func (s *Service) PreviewScheduleEdit(ctx context.Context, id string, r domain.Recurrence, n int) (RunPreview, error) {
	repos := s.tx.Repos()
	cur, err := repos.Schedules.Get(ctx, id)
	if err != nil {
		return RunPreview{}, wrap("app.PreviewScheduleEdit", err)
	}
	if r.Weekend == "" {
		r.Weekend = cur.Recurrence.Weekend
	}
	pv, err := s.PreviewRecurrence(ctx, r, n)
	if err != nil {
		return RunPreview{}, err
	}
	billed, err := repos.Schedules.BilledUntil(ctx, id)
	if err != nil {
		return RunPreview{}, wrap("app.PreviewScheduleEdit", err)
	}
	r.TimeZone, r.Weekend = pv.TimeZone, pv.Weekend
	if err := r.Validate(); err != nil { // normalises it as saving would (valid: previewed)
		return RunPreview{}, wrap("app.PreviewScheduleEdit", err)
	}
	sc := *cur
	sc.Recurrence = r
	if sc.Status == domain.SchedulePaused {
		_ = sc.Resume(s.now(), billed) // a paused schedule always resumes
	} else {
		sc.ActivateFrom(cur.NextOccurrence, billed, s.now())
	}
	pv.Runs = []time.Time{}
	if sc.Status == domain.ScheduleActive {
		pv.Runs = sc.UpcomingRuns(min(max(n, 1), 24))
	}
	return pv, nil
}

// ScheduleEmailDefaults returns the suggested e-mail subject and body for a new schedule
// of each type; the recurring-invoice type uses the business's own invoice e-mail texts
// when it has set them.
func (s *Service) ScheduleEmailDefaults(ctx context.Context) (domain.ScheduleEmailDefaults, error) {
	b, err := s.business(ctx, s.tx.Repos())
	if err != nil {
		return domain.ScheduleEmailDefaults{}, wrap("app.ScheduleEmailDefaults", err)
	}
	return domain.ScheduleEmailsFor(b), nil
}

// CreateSchedule saves a new active schedule for a customer.
func (s *Service) CreateSchedule(ctx context.Context, sc domain.Schedule) (ScheduleDetail, error) {
	if err := checkScheduleAccess(ctx, sc); err != nil {
		return ScheduleDetail{}, wrap("app.CreateSchedule", err)
	}
	sc.ID = s.ids.NewID()
	sc.CreatedAt = s.now()
	sc.Recurrence.DefaultWeekendRule()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		return s.createSchedule(ctx, r, &sc, false)
	})
	if err != nil {
		return ScheduleDetail{}, wrap("app.CreateSchedule", err)
	}
	return s.GetSchedule(ctx, sc.ID)
}

// createSchedule checks and saves a new schedule (ID and CreatedAt set), active or, with
// paused, paused at its first occurrence.
func (s *Service) createSchedule(ctx context.Context, r ports.Repos, sc *domain.Schedule, paused bool) error {
	if err := s.checkSchedule(ctx, r, sc); err != nil {
		return err
	}
	sc.Activate(s.now())
	if paused && sc.Status == domain.ScheduleActive {
		if err := sc.Pause(s.now()); err != nil {
			return err
		}
	}
	if err := r.Schedules.Create(ctx, sc); err != nil {
		return err
	}
	return s.audit(ctx, r, "schedule.created", "schedule", sc.ID, sc.Name)
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
		if err := checkScheduleAccess(ctx, *cur); err != nil {
			return err
		}
		if err := checkScheduleAccess(ctx, sc); err != nil {
			return err
		}
		return s.editSchedule(ctx, r, cur, &sc)
	})
	if err != nil {
		return ScheduleDetail{}, wrap("app.UpdateSchedule", err)
	}
	return s.GetSchedule(ctx, sc.ID)
}

// editSchedule saves sc as the new definition of cur, keeping cur's run state.
func (s *Service) editSchedule(ctx context.Context, r ports.Repos, cur *domain.Schedule, sc *domain.Schedule) error {
	if sc.Recurrence.Weekend == "" { // a client that does not know the rule keeps it
		sc.Recurrence.Weekend = cur.Recurrence.Weekend
	}
	if err := s.checkSchedule(ctx, r, sc); err != nil {
		return err
	}
	// The run state is the server's: a paused schedule keeps its pending occurrence and
	// failure count (Resume decides from them); an active one keeps an unrun overdue
	// occurrence and continues after what was billed (ActivateFrom).
	sc.Status, sc.CreatedAt, sc.LastRunAt = cur.Status, cur.CreatedAt, cur.LastRunAt
	sc.FailureCount, sc.LastError, sc.NextOccurrence, sc.NextRunAt = cur.FailureCount, cur.LastError, cur.NextOccurrence, cur.NextRunAt
	sc.UpdatedAt = s.now()
	if cur.Status != domain.SchedulePaused {
		billed, err := r.Schedules.BilledUntil(ctx, sc.ID)
		if err != nil {
			return err
		}
		sc.ActivateFrom(cur.NextOccurrence, billed, s.now())
	}
	if err := r.Schedules.Update(ctx, sc); err != nil {
		return err
	}
	return s.audit(ctx, r, "schedule.updated", "schedule", sc.ID, sc.Name)
}

func (s *Service) checkSchedule(ctx context.Context, r ports.Repos, sc *domain.Schedule) error {
	if sc.Recurrence.TimeZone == "" {
		b, err := s.business(ctx, r)
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
	return s.scheduleTransition(ctx, "app.PauseSchedule", "schedule.paused", id, func(sc *domain.Schedule, _ ports.Repos) error { return sc.Pause(s.now()) })
}

// ResumeSchedule restarts a paused schedule: a failed pending period runs now, otherwise
// it continues with the next occurrence after now; never inside a billed period.
func (s *Service) ResumeSchedule(ctx context.Context, id string) (ScheduleDetail, error) {
	return s.scheduleTransition(ctx, "app.ResumeSchedule", "schedule.resumed", id, func(sc *domain.Schedule, r ports.Repos) error {
		if err := checkScheduleAccess(ctx, *sc); err != nil {
			return err
		}
		billed, err := r.Schedules.BilledUntil(ctx, sc.ID)
		if err != nil {
			return err
		}
		return sc.Resume(s.now(), billed)
	})
}

// RunScheduleNow makes the schedule's pending occurrence due immediately: the next
// invoice is issued (and sent if auto-send) on the runner's next pass.
func (s *Service) RunScheduleNow(ctx context.Context, id string) (ScheduleDetail, error) {
	return s.scheduleTransition(ctx, "app.RunScheduleNow", "schedule.run_now", id, func(sc *domain.Schedule, _ ports.Repos) error {
		if err := checkScheduleAccess(ctx, *sc); err != nil {
			return err
		}
		if sc.Status != domain.ScheduleActive {
			return domain.ErrInvalidTransition
		}
		sc.NextRunAt = s.now()
		return nil
	})
}

func (s *Service) scheduleTransition(ctx context.Context, op, action, id string, apply func(*domain.Schedule, ports.Repos) error) (ScheduleDetail, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		sc, err := r.Schedules.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := apply(sc, r); err != nil {
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
// occurrence is processed in its own transaction; its run record (refused when its billing
// period overlaps one already billed) guarantees at most one invoice for any stretch of
// time, even with concurrent runners.
func (s *Service) RunDueSchedules(ctx context.Context, limit int) (RunResult, error) {
	var res RunResult
	for range limit {
		if stopping(ctx) {
			return res, nil
		}
		var claimed *domain.Schedule
		var one RunResult // counted per attempt; added only once the transaction committed
		err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
			one = RunResult{}
			due, err := r.Schedules.ClaimDue(ctx, s.now(), 1)
			if err != nil || len(due) == 0 {
				return err
			}
			claimed = due[0]
			return s.runOccurrence(ctx, r, claimed, &one)
		})
		if err == nil {
			res.Issued += one.Issued
			res.Emailed += one.Emailed
		}
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
	start, end := sc.BillingPeriod()
	err := r.Schedules.InsertRun(ctx, domain.ScheduleRun{
		ScheduleID: sc.ID, Period: period, PeriodKey: sc.PeriodKey(), PeriodStart: start, PeriodEnd: end, InvoiceID: invoiceID, At: s.now(),
	})
	switch {
	case errors.Is(err, domain.ErrConflict):
		// This billing period, or part of it, was already billed (a previous run committed
		// but did not advance, or the recurrence changed after it ran): just advance.
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
	b, err := s.business(ctx, r)
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
	// The invoice number's lock first, then the audit head (s.event), as IssueInvoice does.
	if err := r.Sequences.Lock(ctx); err != nil {
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
		// An e-mail of this invoice that is already on its way (queued for later, sending or
		// retrying) counts as sent: "unsent" must not e-mail it a second time.
		if sc.EmailScope == domain.EmailUnsent {
			queued, err := s.emailQueued(ctx, r, invs[i].ID)
			if err != nil {
				return err
			}
			if queued {
				continue
			}
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

// checkScheduleAccess refuses a schedule that would do, on its own, what the user may not do
// directly (ADR-012): raising invoices creates and issues them, so it needs invoices.create
// and invoices.issue (and invoices.send to e-mail them); e-mailing invoices needs
// invoices.send, statements statements.send.
func checkScheduleAccess(ctx context.Context, sc domain.Schedule) error {
	a := ActorFrom(ctx)
	var need []domain.Permission
	switch {
	case sc.Kind == domain.ScheduleInvoice:
		need = append(need, domain.PermInvoicesCreate, domain.PermInvoicesIssue)
		if sc.AutoSend {
			need = append(need, domain.PermInvoicesSend)
		}
	case sc.EmailScope == domain.EmailStatement:
		need = append(need, domain.PermStatementsSend)
	default:
		need = append(need, domain.PermInvoicesSend)
	}
	for _, p := range need {
		if !a.Can(p) {
			return domain.ErrNoAccess
		}
	}
	return nil
}

// emailQueued reports whether an e-mail of the invoice is pending or being sent.
func (s *Service) emailQueued(ctx context.Context, r ports.Repos, invoiceID string) (bool, error) {
	for _, st := range []domain.EmailStatus{domain.EmailPending, domain.EmailSending} {
		_, n, err := r.Emails.List(ctx, ports.EmailFilter{InvoiceID: invoiceID, Status: st, Page: ports.Page{Limit: 1}})
		if err != nil || n > 0 {
			return n > 0, err
		}
	}
	return false, nil
}
