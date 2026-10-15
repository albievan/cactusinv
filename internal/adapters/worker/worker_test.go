package worker

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/albievan/simpleinvoice/internal/app"
	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports/fakes"
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
	price := int64(100)
	d, err := svc.CreateSchedule(ctx, domain.Schedule{
		CustomerID: c.ID, Name: "m", AutoSend: true,
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.DateOf(clock.Now())},
		Lines:      []domain.ScheduleLine{{Description: "x", Quantity: 1000, UnitPrice: &price}},
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
