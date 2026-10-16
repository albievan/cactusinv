//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// A cancel that holds the message's lock keeps the dispatcher off it (SKIP LOCKED), and a
// message the dispatcher has claimed can no longer be cancelled: never both.
func TestEmailLockSerialisesCancelAndClaim(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, e.reference())).ID, domain.Date{}))
	m := must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{}))
	locked := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = e.store.WithinTx(e.ctx, func(ctx context.Context, r ports.Repos) error {
			if _, err := r.Emails.Lock(ctx, m.ID); err != nil {
				t.Error(err)
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	err := e.store.WithinTx(e.ctx, func(ctx context.Context, r ports.Repos) error {
		due, err := r.Emails.ClaimDue(ctx, e.clock.Now().Add(time.Minute), 10)
		if err == nil && len(due) != 0 {
			t.Errorf("a locked message was claimed")
		}
		return err
	})
	close(release)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	must(e.svc.DispatchEmails(e.ctx, 10))
	if _, err := e.svc.CancelEmail(e.ctx, m.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("a sent message cannot be cancelled: %v", err)
	}
}

// Paid months are the business's months; top debtors are ranked within each currency.
func TestDashboardQueriesUseLocalMonthsAndCurrencies(t *testing.T) {
	e := setup(t)
	r := e.store.Repos()
	joburg, _ := time.LoadLocation("Africa/Johannesburg")
	// Paid at 00:30 on 1 September in Johannesburg = 22:30 UTC on 31 August.
	e.clock.T = time.Date(2026, 8, 31, 22, 30, 0, 0, time.UTC)
	inv := must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, e.reference())).ID, domain.Date{}))
	must(e.svc.MarkInvoicePaid(e.ctx, inv.ID))
	_, paid := must2(r.Invoices.Monthly(e.ctx, domain.NewDate(2026, 1, 1), joburg))
	if len(paid) != 1 || paid[0].Month != "2026-09" {
		t.Fatalf("paid months = %+v (want September, the local month)", paid)
	}
	// USD customers (each in a smaller amount than any ZAR one) still get their top places.
	usd := must(e.svc.CreateCustomer(e.ctx, domain.Customer{Code: "US", Name: "Yankee", Email: "y@example.com", Currency: "USD", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))
	must(e.svc.CreateBankAccount(e.ctx, domain.BankAccount{Currency: "USD", Label: "USD", BankName: "B", AccountNumber: "1", Active: true}))
	for range 7 {
		must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, e.reference())).ID, domain.Date{}))
	}
	must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, app.InvoiceInput{CustomerID: usd.ID, Lines: []app.LineInput{{ProductID: e.product.ID, Quantity: 1000}}})).ID, domain.Date{}))
	debtors := must(r.Invoices.TopDebtors(e.ctx, 1))
	cur := map[domain.CurrencyCode]int{}
	for _, d := range debtors {
		cur[d.Currency]++
	}
	if cur["USD"] != 1 || cur["ZAR"] != 1 {
		t.Fatalf("debtors = %+v (one per currency)", debtors)
	}
}

func must2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

// A value longer than its column is a validation error naming the column, not a 500.
func TestTooLongValueIsAValidationError(t *testing.T) {
	e := setup(t)
	c := e.customer
	c.Phone = strings.Repeat("9", 300)
	_, err := e.svc.UpdateCustomer(e.ctx, c)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Field != "phone" {
		t.Fatalf("err = %v", err)
	}
}

// Two processes migrating at once take turns (GET_LOCK), so neither fails half-way.
func TestMigrationsTakeALock(t *testing.T) {
	reset(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = mariadb.Migrate(context.Background(), testDB, false)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
