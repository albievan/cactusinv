package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// Part-payments (M58): the balance falls with each payment, statements and the dashboard
// show what is still owed, the rest marks the invoice paid, and removing a payment opens it
// again. Every change is audited with the amount.
func TestPartPayments(t *testing.T) {
	h := newHarness(t).seed()
	inv := must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	total := inv.Totals.Total // 325116

	if _, err := h.svc.RecordPayment(h.ctx, inv.ID, app.PaymentInput{Amount: total + 1}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("more than owed: %v", err)
	}
	if _, err := h.svc.RecordPayment(h.ctx, inv.ID, app.PaymentInput{Amount: 1, ReceivedOn: domain.NewDate(2026, 8, 22)}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("received tomorrow: %v", err)
	}

	got := must(h.svc.RecordPayment(h.ctx, inv.ID, app.PaymentInput{Amount: 100000, Reference: "EFT 1"}))
	if got.AmountPaid != 100000 || got.Balance() != total-100000 || got.Status != domain.InvoiceIssued {
		t.Fatalf("after part-payment: %+v", got)
	}
	st := must(h.svc.GetStatement(h.ctx, h.customer.ID))
	if st.Total != total-100000 || st.Lines[0].Amount != total-100000 {
		t.Fatalf("statement = %+v", st)
	}
	d := must(h.svc.GetDashboard(h.ctx))
	if d.Totals[0].Outstanding != total-100000 || d.TopDebtors[0].Outstanding != total-100000 {
		t.Fatalf("dashboard = %+v %+v", d.Totals, d.TopDebtors)
	}
	if _, err := h.svc.VoidInvoice(h.ctx, inv.ID, "wrong customer"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("void with a payment: %v", err)
	}

	h.clock.Set(start.Add(24 * time.Hour))
	paid := must(h.svc.MarkInvoicePaid(h.ctx, inv.ID))
	if paid.Status != domain.InvoicePaid || paid.Balance() != 0 {
		t.Fatalf("paid = %+v", paid)
	}
	detail := must(h.svc.GetInvoice(h.ctx, inv.ID))
	if len(detail.Payments) != 2 || detail.Payments[0].Reference != "EFT 1" || detail.Payments[1].Amount != total-100000 ||
		detail.Payments[1].ReceivedOn != domain.NewDate(2026, 8, 22) {
		t.Fatalf("payments = %+v", detail.Payments)
	}
	d = must(h.svc.GetDashboard(h.ctx))
	if len(d.Paid) != 1 || d.Paid[0].Total != total || d.Paid[0].Count != 2 {
		t.Fatalf("paid per month = %+v", d.Paid)
	}

	reopened := must(h.svc.RemovePayment(h.ctx, inv.ID, detail.Payments[0].ID))
	if reopened.Status != domain.InvoiceIssued || reopened.Balance() != 100000 {
		t.Fatalf("reopened = %+v", reopened)
	}
	if _, err := h.svc.RemovePayment(h.ctx, inv.ID, detail.Payments[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("removed twice: %v", err)
	}
	if ps := must(h.svc.ListPayments(h.ctx, inv.ID)); len(ps) != 1 {
		t.Fatalf("payments left = %+v", ps)
	}

	var details []string
	for _, e := range must(h.svc.GetInvoice(h.ctx, inv.ID)).Events {
		switch e.Action {
		case "invoice.payment_recorded", "invoice.paid", "invoice.payment_removed":
			details = append(details, e.Action+": "+e.Summary)
		}
	}
	want := []string{
		"invoice.payment_recorded: ZAR 1,000.00 received; balance ZAR 2,251.16",
		"invoice.paid: ZAR 2,251.16 received; balance ZAR 0.00",
		"invoice.payment_removed: ZAR 1,000.00 removed; balance ZAR 1,000.00",
	}
	if len(details) != len(want) {
		t.Fatalf("audit = %q", details)
	}
	for i := range want {
		if details[i] != want[i] {
			t.Fatalf("audit[%d] = %q, want %q", i, details[i], want[i])
		}
	}
}
