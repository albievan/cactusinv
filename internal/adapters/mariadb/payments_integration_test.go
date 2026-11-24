//go:build integration

package mariadb_test

import (
	"context"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// Part-payments on MariaDB 11 (M58): payments are stored with the invoice's amount paid,
// totals, top debtors and statements count the balance, the paid-per-month figures count
// payments by the date received, and removing one opens the invoice again.
func TestPaymentsPersistence(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, e.reference())).ID, domain.Date{}))
	total := inv.Totals.Total

	part := must(e.svc.RecordPayment(e.ctx, inv.ID, app.PaymentInput{Amount: 1000, Reference: "EFT ünï"}))
	if part.AmountPaid != 1000 || part.Status != domain.InvoiceIssued {
		t.Fatalf("part = %+v", part)
	}
	d := must(e.svc.GetDashboard(e.ctx))
	if d.Totals[0].Outstanding != total-1000 || d.TopDebtors[0].Outstanding != total-1000 {
		t.Fatalf("dashboard = %+v %+v", d.Totals, d.TopDebtors)
	}
	if st := must(e.svc.GetStatement(e.ctx, inv.CustomerID)); st.Total != total-1000 {
		t.Fatalf("statement = %+v", st)
	}

	paid := must(e.svc.MarkInvoicePaid(e.ctx, inv.ID))
	if paid.Status != domain.InvoicePaid || paid.AmountPaid != total {
		t.Fatalf("paid = %+v", paid)
	}
	ps := must(e.svc.ListPayments(e.ctx, inv.ID))
	if len(ps) != 2 || ps[0].Reference != "EFT ünï" || ps[1].Amount != total-1000 || ps[0].ReceivedOn.IsZero() {
		t.Fatalf("payments = %+v", ps)
	}
	d = must(e.svc.GetDashboard(e.ctx))
	var sum int64
	for _, m := range d.Paid {
		sum += m.Total
	}
	if sum != total {
		t.Fatalf("paid per month = %+v", d.Paid)
	}

	open := must(e.svc.RemovePayment(e.ctx, inv.ID, ps[1].ID))
	if open.Status != domain.InvoiceIssued || open.AmountPaid != 1000 {
		t.Fatalf("reopened = %+v", open)
	}
	if left := must(e.svc.ListPayments(e.ctx, inv.ID)); len(left) != 1 || left[0].ID != ps[0].ID {
		t.Fatalf("left = %+v", left)
	}
}

// Migration 00031 gives every invoice already paid one payment of its total, received on
// the day it was marked paid in the business's time zone, so the balance, statements and
// paid-per-month figures of an upgraded install are as before; running it again adds none.
func TestPaymentsMigrationBackfill(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, e.reference())).ID, domain.Date{}))
	paid := must(e.svc.MarkInvoicePaid(e.ctx, inv.ID))
	ctx := context.Background()
	if err := mariadb.MigrateDownTo(ctx, testDB, 29); err != nil {
		t.Fatal(err)
	}
	// Paid at 23:30 UTC: 01:30 the next day in Johannesburg, the business's zone.
	if _, err := testDB.ExecContext(ctx, `UPDATE invoices SET paid_at = '2026-08-20 23:30:00' WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	got := must(e.svc.GetInvoice(e.ctx, inv.ID))
	if got.Invoice.AmountPaid != paid.Totals.Total || got.Invoice.Balance() != 0 || len(got.Payments) != 1 ||
		got.Payments[0].Amount != paid.Totals.Total || got.Payments[0].ReceivedOn != domain.NewDate(2026, 8, 21) {
		t.Fatalf("after the migration: %+v %+v", got.Invoice, got.Payments)
	}
	// Down to 30 and up again: the backfill runs again and adds nothing.
	if err := mariadb.MigrateDownTo(ctx, testDB, 30); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	if again := must(e.svc.GetInvoice(e.ctx, inv.ID)); len(again.Payments) != 1 || again.Invoice.AmountPaid != paid.Totals.Total {
		t.Fatalf("after running it again: %+v", again.Payments)
	}
}
