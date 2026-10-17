//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/migrations"
)

// TestScheduleRunBillingPeriod: migration 00020 backfills each run's billing period exactly
// as domain.Recurrence.BillingPeriod computes it (anchored on the start date), and the
// repository refuses a run whose period overlaps a billed one.
func TestScheduleRunBillingPeriod(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	type tc struct {
		name string
		r    domain.Recurrence
		runs []domain.Date
	}
	cases := []tc{
		{
			"monthly",
			domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
			[]domain.Date{domain.NewDate(2026, 9, 15), domain.NewDate(2026, 10, 15)},
		},
		{
			"every 2 months from February",
			domain.Recurrence{Frequency: domain.FrequencyMonthly, Interval: 2, DayOfMonth: 10, SendHour: 9, StartDate: domain.NewDate(2026, 2, 1)},
			[]domain.Date{domain.NewDate(2026, 4, 10), domain.NewDate(2026, 6, 10)},
		},
		{
			"quarterly from February",
			domain.Recurrence{Frequency: domain.FrequencyQuarterly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 2, 1)},
			[]domain.Date{domain.NewDate(2026, 11, 1), domain.NewDate(2027, 2, 1)},
		},
		{
			"yearly from April",
			domain.Recurrence{Frequency: domain.FrequencyYearly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2025, 4, 1)},
			[]domain.Date{domain.NewDate(2025, 4, 1), domain.NewDate(2026, 4, 1)},
		},
		{
			"every 2 weeks from a Thursday",
			domain.Recurrence{Frequency: domain.FrequencyWeekly, Interval: 2, Weekday: time.Thursday, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)},
			[]domain.Date{domain.NewDate(2026, 10, 1), domain.NewDate(2026, 10, 15)},
		},
		{
			"once",
			domain.Recurrence{Frequency: domain.FrequencyOnce, SendHour: 9, StartDate: domain.NewDate(2026, 12, 3)},
			[]domain.Date{domain.NewDate(2026, 12, 3)},
		},
	}
	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
			CustomerID: e.customer.ID, Name: c.name, Recurrence: c.r,
			Lines: []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 1000}},
		})).Schedule.ID
	}
	provider, err := goose.NewProvider(goose.DialectMySQL, testDB, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 19); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mariadb.Migrate(ctx, testDB, false) })
	for i, c := range cases {
		for k, d := range c.runs {
			if _, err := testDB.ExecContext(ctx, `INSERT INTO schedule_runs (schedule_id, period, period_key, invoice_id, at) VALUES (?, ?, ?, ?, NOW())`,
				ids[i], d.String(), fmt.Sprintf("k%d", k), "00000000-0000-0000-0000-000000000000"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	repo := e.store.Repos().Schedules
	for i, c := range cases {
		sc := must(repo.Get(ctx, ids[i])) // the recurrence as stored (interval normalised)
		runs := must(repo.ListRuns(ctx, ids[i], 10))
		if len(runs) != len(c.runs) {
			t.Fatalf("%s: %d runs", c.name, len(runs))
		}
		var until domain.Date
		for _, run := range runs {
			s, end := sc.Recurrence.BillingPeriod(run.Period)
			if run.PeriodStart != s || run.PeriodEnd != end {
				t.Errorf("%s run %v: backfilled [%v, %v), domain says [%v, %v)", c.name, run.Period, run.PeriodStart, run.PeriodEnd, s, end)
			}
			if end.After(until) {
				until = end
			}
		}
		if got := must(repo.BilledUntil(ctx, ids[i])); got != until {
			t.Errorf("%s: billed until %v, want %v", c.name, got, until)
		}
	}
	if got := must(repo.BilledUntil(ctx, "00000000-0000-0000-0000-000000000000")); !got.IsZero() {
		t.Fatalf("nothing billed: %v", got)
	}

	// Overlap: the monthly schedule billed September and October.
	run := func(key string, s, end domain.Date) domain.ScheduleRun {
		return domain.ScheduleRun{ScheduleID: ids[0], Period: s, PeriodKey: key, PeriodStart: s, PeriodEnd: end, InvoiceID: "00000000-0000-0000-0000-000000000000", At: time.Now()}
	}
	if err := repo.InsertRun(ctx, run("x1", domain.NewDate(2026, 10, 20), domain.NewDate(2026, 10, 27))); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a week inside billed October: %v", err)
	}
	if err := repo.InsertRun(ctx, run("x2", domain.NewDate(2026, 10, 1), domain.NewDate(2027, 1, 1))); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a quarter overlapping October: %v", err)
	}
	if err := repo.InsertRun(ctx, run("x3", domain.NewDate(2026, 11, 1), domain.NewDate(2026, 12, 1))); err != nil {
		t.Fatalf("November (adjacent): %v", err)
	}
	// A label used before does not block a period that was never billed.
	if err := repo.InsertRun(ctx, run("x3", domain.NewDate(2027, 5, 1), domain.NewDate(2027, 6, 1))); err != nil {
		t.Fatalf("same label, unbilled period: %v", err)
	}
	if got := must(repo.BilledUntil(ctx, ids[0])); got != domain.NewDate(2027, 6, 1) {
		t.Fatalf("billed until %v", got)
	}
	// Down: a label used twice keeps its first run, the other takes its date; then up again.
	if _, err := provider.DownTo(ctx, 19); err != nil {
		t.Fatal(err)
	}
	var keys []string
	rows, err := testDB.QueryContext(ctx, `SELECT period_key FROM schedule_runs WHERE schedule_id = ? AND period >= '2026-11-01' ORDER BY period`, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(keys) != "[x3 2027-05-01]" {
		t.Fatalf("labels after down: %v", keys)
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
}
