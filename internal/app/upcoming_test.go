package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// The Scheduled list shows every schedule's next invoice not raised yet, paused ones too;
// its sample PDF stores nothing, takes no number and sends nothing (M32).
func TestUpcomingInvoicesAndSamples(t *testing.T) {
	h := newHarness(t).seed()
	h.clock.Set(time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC))
	monthly := domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 21, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)}
	lines := []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 14000}, {ProductID: h.integP.ID, Quantity: 1000}}
	a := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{CustomerID: h.customer.ID, Name: "Acme monthly", Recurrence: monthly, Lines: lines, AutoSend: true})).Schedule
	early := monthly
	early.DayOfMonth = 15
	b := must(h.svc.CreateSchedule(h.ctx, domain.Schedule{CustomerID: h.customer.ID, Name: "Acme paused", Recurrence: early, Lines: lines})).Schedule
	must(h.svc.PauseSchedule(h.ctx, b.ID))

	ups := must(h.svc.ListUpcomingInvoices(h.ctx))
	if len(ups) != 2 || ups[0].Schedule.ID != b.ID || ups[1].Schedule.ID != a.ID {
		t.Fatalf("upcoming = %+v", ups)
	}
	if ups[0].Schedule.Status != domain.SchedulePaused || ups[0].Period != domain.NewDate(2026, 8, 15) || ups[0].Invoice == nil {
		t.Fatalf("paused = %+v", ups[0])
	}
	if u := ups[1]; u.Period != domain.NewDate(2026, 8, 21) || u.Invoice.Totals.Total != 325116 || u.Customer != h.customer.Name || u.Problem != "" {
		t.Fatalf("active = %+v", u)
	}

	pdf, name, err := h.svc.ScheduleSamplePDF(h.ctx, a.ID)
	if err != nil || !strings.Contains(string(pdf), domain.SampleNumber) || !strings.HasPrefix(name, "Sample_") {
		t.Fatalf("sample: %q %q %v", pdf, name, err)
	}
	if _, n := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{})); n != 0 {
		t.Fatalf("a sample was stored: %d invoices", n)
	}

	// The real run still gets the first number, so the sample took none.
	h.clock.Set(time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC))
	if res := must(h.svc.RunDueSchedules(h.ctx, 10)); res.Issued != 1 {
		t.Fatalf("run = %+v", res)
	}
	invs, _ := must3(h.svc.ListInvoices(h.ctx, ports.InvoiceFilter{}))
	if invs[0].Number != "CD/1195/26" {
		t.Fatalf("the sample used a number: %s", invs[0].Number)
	}
	// Raised: the list moves on to September's.
	ups = must(h.svc.ListUpcomingInvoices(h.ctx))
	if len(ups) != 2 || ups[1].Schedule.ID != a.ID || ups[1].Period != domain.NewDate(2026, 9, 21) {
		t.Fatalf("after the run = %+v", ups)
	}

	// An inactive product: listed with the reason, and no sample.
	p := h.usersP
	p.Active = false
	must(h.svc.UpdateProduct(h.ctx, p))
	ups = must(h.svc.ListUpcomingInvoices(h.ctx))
	if ups[0].Invoice != nil || !strings.Contains(ups[0].Problem, "no longer available") {
		t.Fatalf("inactive product = %+v", ups[0])
	}
	if _, _, err := h.svc.ScheduleSamplePDF(h.ctx, a.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("sample with an inactive product: %v", err)
	}
}
