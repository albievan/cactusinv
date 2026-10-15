package worker

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

func TestRunnerPasses(t *testing.T) {
	ctx := context.Background()
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Now()}
	arch := &fakes.Archive{}
	svc := app.New(app.Deps{Tx: store, Clock: clock, IDs: &fakes.IDs{}, Mailer: &fakes.Mailer{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Archive: arch})
	_, _ = svc.CreateCurrency(ctx, "ZAR", "Rand", "R", 2, true)
	_, _ = svc.CreateBankAccount(ctx, domain.BankAccount{Currency: "ZAR", Label: "b", BankName: "b", AccountNumber: "1", Active: true})
	c, err := svc.CreateCustomer(ctx, domain.Customer{Code: "CD", Name: "Acme", Email: "a@b.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.CreateProduct(ctx, domain.Product{SKU: "S", Name: "Service", Prices: map[domain.CurrencyCode]int64{"ZAR": 100}, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.CreateSchedule(ctx, domain.Schedule{
		CustomerID: c.ID, Name: "m", AutoSend: true,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.DateOf(clock.Now())},
		Lines:      []domain.ScheduleLine{{ProductID: p.ID, Quantity: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunScheduleNow(ctx, d.Schedule.ID); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	r := New(svc, slog.New(slog.DiscardHandler), Intervals{}, reg)
	r.Schedules(ctx)
	r.Emails(ctx)
	r.Export(ctx)
	r.Housekeeping(ctx)
	if got := testutil.ToFloat64(r.runs.WithLabelValues("issued")); got != 1 {
		t.Fatalf("issued = %v", got)
	}
	if got := testutil.ToFloat64(r.sent.WithLabelValues("sent")); got != 1 {
		t.Fatalf("sent = %v", got)
	}
	if len(arch.Files) != 1 {
		t.Fatalf("archive = %v", arch.Files)
	}

	// Run returns promptly on cancellation.
	ctx2, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		New(svc, slog.New(slog.DiscardHandler), Intervals{Schedules: time.Millisecond, Emails: time.Millisecond, Export: time.Millisecond, Housekeeping: time.Millisecond}, prometheus.NewRegistry()).Run(ctx2)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

// TestBackupSlotOnceAcrossReplicas: two replicas' runners share the database and the
// backups directory; the due slot is taken once.
func TestBackupSlotOnceAcrossReplicas(t *testing.T) {
	ctx := context.Background()
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)}
	files := &fakes.Files{Clock: clock}
	times, _ := domain.ParseBackupTimes("02:00")
	replica := func() *Runner {
		svc := app.New(app.Deps{Tx: store, Clock: clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}, Backups: app.BackupDeps{
			Snapshotter: &fakes.Snapshotter{Store: store}, Codec: fakes.Codec{}, Sealer: fakes.Sealer{Key: true}, Files: files,
			Stager: &fakes.Stager{Store: store}, Times: times, Location: time.UTC,
		}})
		return New(svc, slog.New(slog.DiscardHandler), Intervals{}, prometheus.NewRegistry())
	}
	a, b := replica(), replica()
	a.Backups(ctx)
	b.Backups(ctx)
	a.Backups(ctx)
	fs, _ := files.List(ctx)
	if len(fs) != 1 {
		t.Fatalf("files = %+v", fs)
	}
	if got := testutil.ToFloat64(a.backups.WithLabelValues("ok")) + testutil.ToFloat64(b.backups.WithLabelValues("ok")); got != 1 {
		t.Fatalf("ok = %v", got)
	}
	files.FailCommit = errors.New("disk full")
	clock.Advance(24 * time.Hour)
	b.Backups(ctx)
	if got := testutil.ToFloat64(b.backups.WithLabelValues("failed")); got != 1 {
		t.Fatalf("failed = %v", got)
	}
}
