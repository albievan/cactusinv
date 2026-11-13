//go:build integration

package mariadb_test

import (
	"regexp"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// The "I have paid" link of an invoice e-mail on MariaDB 11 (M42): the token's hash finds
// the invoice, the flag and note are stored without touching anything else, and a later
// update of the invoice keeps them.
func TestInvoiceCustomerPaidPersistence(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.CreateInvoice(e.ctx, e.reference()))
	m := must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{LinkBase: "https://inv.example.com"}))
	link := regexp.MustCompile(`/i/([A-Za-z0-9_-]{43})\n`).FindStringSubmatch(m.Body)
	if link == nil {
		t.Fatalf("no link in %q", m.Body)
	}
	v := must(e.svc.MarkInvoicePaidByCustomer(e.ctx, link[1], "Paid by EFT, ref 7"))
	if v.Invoice.CustomerPaidAt.IsZero() || v.Invoice.CustomerPaidNote != "Paid by EFT, ref 7" {
		t.Fatalf("after marking: %+v", v.Invoice)
	}
	must(e.svc.MarkInvoicePaid(e.ctx, inv.ID))
	got := must(e.svc.GetInvoice(e.ctx, inv.ID)).Invoice
	if got.Status != domain.InvoicePaid || got.CustomerPaidNote != "Paid by EFT, ref 7" || !got.CustomerPaidAt.Equal(v.Invoice.CustomerPaidAt) {
		t.Fatalf("after the business marked it paid: %+v", got)
	}
}
