package mariadb

import (
	"context"
	"database/sql"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type scheduleRepo struct{ q *sqlcgen.Queries }

func toSchedule(r sqlcgen.Schedule) *domain.Schedule {
	return &domain.Schedule{
		ID: r.ID, CustomerID: r.CustomerID, Name: r.Name, Status: domain.ScheduleStatus(r.Status),
		Recurrence: domain.Recurrence{
			Frequency: domain.Frequency(r.Frequency), Interval: int(r.IntervalCount), DayOfMonth: int(r.DayOfMonth),
			Weekday: time.Weekday(r.Weekday), SendHour: int(r.SendHour), SendMinute: int(r.SendMinute),
			TimeZone: r.TimeZone, StartDate: fromDate(r.StartDate), EndDate: fromNullDate(r.EndDate),
		},
		Header:         domain.InvoiceHeader{SalesPerson: r.SalesPerson, Job: r.Job, ShippingMethod: r.ShippingMethod, ShippingTerms: r.ShippingTerms},
		Notes:          r.Notes,
		TaxRate:        optionalRate(r.TaxRateBp),
		AutoSend:       r.AutoSend,
		EmailSubject:   r.EmailSubject,
		EmailBody:      r.EmailBody,
		NextOccurrence: fromNullTime(r.NextOccurrence), NextRunAt: fromNullTime(r.NextRunAt), LastRunAt: fromNullTime(r.LastRunAt),
		FailureCount: int(r.FailureCount), LastError: r.LastError, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		Version: int(r.Version),
	}
}

// withLines loads the line templates of schedules.
func (r scheduleRepo) withLines(ctx context.Context, rows []sqlcgen.Schedule) ([]*domain.Schedule, error) {
	out := make([]*domain.Schedule, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, len(rows))
	idx := map[string]int{}
	for i, row := range rows {
		out[i] = toSchedule(row)
		ids[i] = row.ID
		idx[row.ID] = i
	}
	lines, err := r.q.ListScheduleLines(ctx, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	for _, l := range lines {
		sl := domain.ScheduleLine{
			ProductID: l.ProductID.String, Description: l.Description, Quantity: domain.Quantity(l.QuantityMilli),
			Discount: domain.BasisPoints(l.DiscountBp),
		}
		if l.UnitPriceMinor.Valid {
			v := l.UnitPriceMinor.Int64
			sl.UnitPrice = &v
		}
		s := out[idx[l.ScheduleID]]
		s.Lines = append(s.Lines, sl)
	}
	return out, nil
}

func (r scheduleRepo) List(ctx context.Context, f ports.ScheduleFilter) ([]domain.Schedule, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListSchedules(ctx, sqlcgen.ListSchedulesParams{
		CustomerID: f.CustomerID, Status: string(f.Status), Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountSchedules(ctx, sqlcgen.CountSchedulesParams{CustomerID: f.CustomerID, Status: string(f.Status)})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	ss, err := r.withLines(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.Schedule, len(ss))
	for i, s := range ss {
		out[i] = *s
	}
	return out, int(total), nil
}

func (r scheduleRepo) Get(ctx context.Context, id string) (*domain.Schedule, error) {
	row, err := r.q.GetSchedule(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	ss, err := r.withLines(ctx, []sqlcgen.Schedule{row})
	if err != nil {
		return nil, err
	}
	return ss[0], nil
}

func (r scheduleRepo) Create(ctx context.Context, s *domain.Schedule) error {
	rc := s.Recurrence
	err := r.q.InsertSchedule(ctx, sqlcgen.InsertScheduleParams{
		ID: s.ID, CustomerID: s.CustomerID, Name: s.Name, Status: string(s.Status), Frequency: string(rc.Frequency),
		IntervalCount: i32(rc.Interval), DayOfMonth: i32(rc.DayOfMonth), Weekday: i32(int(rc.Weekday)),
		SendHour: i32(rc.SendHour), SendMinute: i32(rc.SendMinute), TimeZone: rc.TimeZone,
		StartDate: dateValue(rc.StartDate), EndDate: nullDate(rc.EndDate), SalesPerson: s.Header.SalesPerson,
		Job: s.Header.Job, ShippingMethod: s.Header.ShippingMethod, ShippingTerms: s.Header.ShippingTerms,
		Notes: s.Notes, TaxRateBp: nullRate(s.TaxRate), AutoSend: s.AutoSend, EmailSubject: s.EmailSubject,
		EmailBody: s.EmailBody, NextOccurrence: nullTime(s.NextOccurrence), NextRunAt: nullTime(s.NextRunAt),
		LastRunAt: nullTime(s.LastRunAt), FailureCount: i32(s.FailureCount), LastError: s.LastError,
		CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
	})
	if err != nil {
		return mapErr(err)
	}
	s.Version = 1
	return r.insertLines(ctx, s)
}

func (r scheduleRepo) Update(ctx context.Context, s *domain.Schedule) error {
	rc := s.Recurrence
	n, err := r.q.UpdateSchedule(ctx, sqlcgen.UpdateScheduleParams{
		CustomerID: s.CustomerID, Name: s.Name, Status: string(s.Status), Frequency: string(rc.Frequency),
		IntervalCount: i32(rc.Interval), DayOfMonth: i32(rc.DayOfMonth), Weekday: i32(int(rc.Weekday)),
		SendHour: i32(rc.SendHour), SendMinute: i32(rc.SendMinute), TimeZone: rc.TimeZone,
		StartDate: dateValue(rc.StartDate), EndDate: nullDate(rc.EndDate), SalesPerson: s.Header.SalesPerson,
		Job: s.Header.Job, ShippingMethod: s.Header.ShippingMethod, ShippingTerms: s.Header.ShippingTerms,
		Notes: s.Notes, TaxRateBp: nullRate(s.TaxRate), AutoSend: s.AutoSend, EmailSubject: s.EmailSubject,
		EmailBody: s.EmailBody, NextOccurrence: nullTime(s.NextOccurrence), NextRunAt: nullTime(s.NextRunAt),
		LastRunAt: nullTime(s.LastRunAt), FailureCount: i32(s.FailureCount), LastError: s.LastError,
		UpdatedAt: s.UpdatedAt.UTC(), ID: s.ID, Version: i32(s.Version),
	})
	if err := affected(n, err, domain.ErrConflict); err != nil {
		return err
	}
	s.Version++
	if err := r.q.DeleteScheduleLines(ctx, s.ID); err != nil {
		return mapErr(err)
	}
	return r.insertLines(ctx, s)
}

func (r scheduleRepo) insertLines(ctx context.Context, s *domain.Schedule) error {
	for i, l := range s.Lines {
		price := sql.NullInt64{}
		if l.UnitPrice != nil {
			price = sql.NullInt64{Int64: *l.UnitPrice, Valid: true}
		}
		if err := r.q.InsertScheduleLine(ctx, sqlcgen.InsertScheduleLineParams{
			ScheduleID: s.ID, Position: i32(i), ProductID: nullString(l.ProductID), Description: l.Description,
			QuantityMilli: int64(l.Quantity), UnitPriceMinor: price, DiscountBp: i32(int(l.Discount)),
		}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r scheduleRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteSchedule(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

func (r scheduleRepo) ClaimDue(ctx context.Context, now time.Time, limit int) ([]*domain.Schedule, error) {
	rows, err := r.q.ClaimDueSchedules(ctx, sqlcgen.ClaimDueSchedulesParams{NextRunAt: nullTime(now), Limit: i32(limit)})
	if err != nil {
		return nil, mapErr(err)
	}
	return r.withLines(ctx, rows)
}

func (r scheduleRepo) InsertRun(ctx context.Context, run domain.ScheduleRun) error {
	return mapErr(r.q.InsertScheduleRun(ctx, sqlcgen.InsertScheduleRunParams{
		ScheduleID: run.ScheduleID, Period: dateValue(run.Period), InvoiceID: run.InvoiceID, At: run.At.UTC(),
	}))
}

func (r scheduleRepo) ListRuns(ctx context.Context, scheduleID string, limit int) ([]domain.ScheduleRun, error) {
	rows, err := r.q.ListScheduleRuns(ctx, sqlcgen.ListScheduleRunsParams{ScheduleID: scheduleID, Limit: i32(limit)})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.ScheduleRun, len(rows))
	for i, x := range rows {
		out[i] = domain.ScheduleRun{ScheduleID: x.ScheduleID, Period: fromDate(x.Period), InvoiceID: x.InvoiceID, At: x.At.UTC()}
	}
	return out, nil
}

func optionalRate(v sql.NullInt32) *domain.BasisPoints {
	if !v.Valid {
		return nil
	}
	r := domain.BasisPoints(v.Int32)
	return &r
}

func nullRate(r *domain.BasisPoints) sql.NullInt32 {
	if r == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: i32(int(*r)), Valid: true}
}
