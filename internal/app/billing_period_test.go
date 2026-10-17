package app_test

import (
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

// Billing periods (M21, Albie 2026-10-17): periods are counted from the start date, an
// edited schedule continues in the next unbilled period, and a resumed schedule retries a
// period that had failed. Times are UTC; the schedules run at 09:00 Johannesburg (07:00 UTC).

func TestQuarterlyPeriodsCountFromTheStartDate(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2027, 1, 20, 8, 0, 0, 0, time.UTC))
	r := domain.Recurrence{Frequency: domain.FrequencyQuarterly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2027, 2, 1), Weekend: domain.WeekendNone}
	id := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(r))).Schedule.ID
	h.clock.Set(time.Date(2027, 2, 1, 8, 0, 0, 0, time.UTC))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("February run: %+v", res)
	}
	d := must(h.svc.GetSchedule(h.ctx, id))
	run := d.Runs[0]
	if run.PeriodStart != domain.NewDate(2027, 2, 1) || run.PeriodEnd != domain.NewDate(2027, 5, 1) {
		t.Fatalf("billed [%v, %v), want February to April", run.PeriodStart, run.PeriodEnd)
	}
	if d.Schedule.Period() != domain.NewDate(2027, 5, 1) {
		t.Fatalf("next period %v, want 1 May", d.Schedule.Period())
	}
}

func TestEditContinuesAfterTheBilledPeriod(t *testing.T) {
	tests := []struct {
		name   string
		r      domain.Recurrence
		ranAt  time.Time
		change func(r *domain.Recurrence)
		next   domain.Date // the pending period after the edit
	}{
		{
			"monthly billed for October, switched to quarterly on the 20th",
			domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1), Weekend: domain.WeekendNone},
			time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC),
			func(r *domain.Recurrence) { r.Frequency, r.DayOfMonth = domain.FrequencyQuarterly, 20 },
			// October is billed; the quarter from 1 October runs on 20 October, which is
			// partly billed, so the next whole quarter (January) is next.
			domain.NewDate(2027, 1, 20),
		},
		{
			"weekly billed on Thursday, moved to Friday of the same week",
			domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Thursday, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1), Weekend: domain.WeekendNone},
			time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC),
			func(r *domain.Recurrence) { r.Weekday = time.Friday },
			domain.NewDate(2026, 10, 23),
		},
		{
			"monthly billed on the 15th, day moved to the 20th",
			domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1), Weekend: domain.WeekendNone},
			time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC),
			func(r *domain.Recurrence) { r.DayOfMonth = 20 },
			domain.NewDate(2026, 11, 20),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).seed()
			h.clock.Set(tc.ranAt.AddDate(0, 0, -1))
			id := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(tc.r))).Schedule.ID
			h.clock.Set(tc.ranAt)
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
				t.Fatalf("first run: %+v", res)
			}
			upd := *must(h.svc.GetSchedule(h.ctx, id)).Schedule
			tc.change(&upd.Recurrence)
			// The editor's preview shows what saving will do.
			if pv := must(h.svc.PreviewScheduleEdit(h.ctx, id, upd.Recurrence, 3)); len(pv.Runs) != 3 || upd.Recurrence.LocalDate(pv.Runs[0]) != upd.Recurrence.RunDate(tc.next) {
				t.Fatalf("edit preview %v, want the first run on %v", pv.Runs, tc.next)
			}
			if got := must(h.svc.UpdateSchedule(h.ctx, upd)).Schedule.Period(); got != tc.next {
				t.Fatalf("next period %v, want %v", got, tc.next)
			}
			// Nothing more is raised before then.
			h.clock.Set(tc.next.Time(time.UTC).Add(-time.Hour))
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 0 {
				t.Fatalf("billed time billed again: %+v", res)
			}
			h.clock.Set(tc.next.Time(time.UTC).Add(8 * time.Hour))
			if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
				t.Fatalf("next period not raised: %+v", res)
			}
		})
	}
}

func TestResumeRetriesAPeriodThatFailedBeforeThePause(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC))
	r := domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1), Weekend: domain.WeekendNone}
	id := must(h.svc.CreateSchedule(h.ctx, h.weekendSchedule(r))).Schedule.ID
	b := h.bank
	b.IsDefault, b.Active = false, false
	b = must(h.svc.UpdateBankAccount(h.ctx, b))
	h.clock.Set(time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Failed != 1 {
		t.Fatalf("run should fail: %+v", res)
	}
	h.clock.Set(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	must(h.svc.PauseSchedule(h.ctx, id)) // by the user, while it was failing
	b.IsDefault, b.Active = true, true
	must(h.svc.UpdateBankAccount(h.ctx, b))
	resumed := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	h.clock.Set(resumed)
	// While paused, the editor's preview shows the runs as resuming now would set them.
	if pv := must(h.svc.PreviewScheduleEdit(h.ctx, id, r, 2)); len(pv.Runs) != 2 || !pv.Runs[0].Equal(resumed) {
		t.Fatalf("paused preview %v, want the retry now", pv.Runs)
	}
	got := must(h.svc.ResumeSchedule(h.ctx, id)).Schedule
	if got.Period() != domain.NewDate(2026, 10, 15) || !got.NextRunAt.Equal(resumed) {
		t.Fatalf("resumed: period %v at %v, want the failed 15 October now", got.Period(), got.NextRunAt)
	}
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("retry: %+v", res)
	}
	if p := must(h.svc.GetSchedule(h.ctx, id)).Schedule.Period(); p != domain.NewDate(2026, 11, 15) {
		t.Fatalf("after the retry: %v", p)
	}
}
