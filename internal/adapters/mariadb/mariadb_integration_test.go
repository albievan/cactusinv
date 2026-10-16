//go:build integration

package mariadb_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcmariadb "github.com/testcontainers/testcontainers-go/modules/mariadb"

	"github.com/albievan/cactusinv/internal/adapters/idgen"
	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
	"github.com/albievan/cactusinv/migrations"
)

var (
	testDB  *sql.DB
	testCfg mariadb.Config
)

// TestMain starts one MariaDB 11 container for the package and applies migrations.
func TestMain(m *testing.M) {
	ctx := context.Background()
	ctr, err := tcmariadb.Run(ctx, "mariadb:11",
		tcmariadb.WithDatabase("cactusinv"), tcmariadb.WithUsername("cactusinv"), tcmariadb.WithPassword("test-only"),
		// The staging database for restores, granted to the app user (ADR-010).
		tcmariadb.WithScripts(filepath.Join("testdata", "10-restore-db.sql")))
	if err != nil {
		panic(err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		panic(err)
	}
	port, err := ctr.MappedPort(ctx, "3306/tcp")
	if err != nil {
		panic(err)
	}
	p, _ := strconv.Atoi(port.Port())
	testCfg = mariadb.Config{Host: host, Port: p, Name: "cactusinv", User: "cactusinv", Password: "test-only"}
	testDB, err = mariadb.Open(ctx, testCfg)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = testDB.Close()
	_ = testcontainers.TerminateContainer(ctr)
	os.Exit(code)
}

// reset migrates down and up, so each test starts from an empty schema — which also
// proves every Down works.
func reset(t *testing.T) *mariadb.Store {
	t.Helper()
	ctx := context.Background()
	if err := mariadb.Migrate(ctx, testDB, true); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	return mariadb.NewStore(testDB)
}

type env struct {
	ctx      context.Context
	svc      *app.Service
	store    *mariadb.Store
	clock    *fakes.Clock
	mailer   *fakes.Mailer
	customer domain.Customer
	product  domain.Product
	integ    domain.Product
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func setup(t *testing.T) *env {
	t.Helper()
	return seedEnv(reset(t))
}

// seedEnv creates the reference business, currencies, bank account, products and customer
// through the use cases on store (whatever schema it is migrated to).
func seedEnv(store *mariadb.Store) *env {
	e := &env{ctx: context.Background(), store: store, clock: &fakes.Clock{T: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}, mailer: &fakes.Mailer{}}
	e.svc = app.New(app.Deps{Tx: e.store, Clock: e.clock, IDs: idgen.UUIDv7{}, Mailer: e.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	must(e.svc.UpdateBusiness(e.ctx, domain.BusinessProfile{Name: "Example Software Consulting", TimeZone: "Africa/Johannesburg", NumberFormat: "{code}/{seq}/{yy}"}, 1195))
	must(e.svc.CreateCurrency(e.ctx, "ZAR", "South African Rand", "R", 2, true))
	must(e.svc.CreateCurrency(e.ctx, "USD", "US Dollar", "$", 2, true))
	must(e.svc.CreateBankAccount(e.ctx, domain.BankAccount{Currency: "ZAR", Label: "Capitec", BankName: "Capitecbank", Branch: "Northgate", AccountType: "Savings", AccountNumber: "1234567890", BranchCode: "470010", Active: true}))
	e.product = must(e.svc.CreateProduct(e.ctx, domain.Product{SKU: "SS-USERS", Name: "Stock system users", Description: "Stock system usage for 14 concurrent users.", Prices: map[domain.CurrencyCode]int64{"ZAR": 19294, "USD": 1100}, Active: true}))
	e.integ = must(e.svc.CreateProduct(e.ctx, domain.Product{SKU: "INTEG", Name: "Integration", Description: "Alchamex integration - monthly subscription", Prices: map[domain.CurrencyCode]int64{"ZAR": 55000}, Active: true}))
	e.customer = must(e.svc.CreateCustomer(e.ctx, domain.Customer{
		Code: "CD", Name: "Acme Commercial Spares (Pty) Limited", Email: "juanita@acme.example",
		CC: []string{"a@acme.example", "b@acme.example"}, Attention: "Jane",
		BillingAddress: "P O BOX 2000,\nGERMISTON,\n1401", Currency: "ZAR",
		Terms: domain.PaymentTerms{Kind: domain.TermsDayOfNextMonth, Value: 7}, Active: true,
	}))
	return e
}

func p64(v int64) *int64 { return &v }

func (e *env) reference() app.InvoiceInput {
	return app.InvoiceInput{CustomerID: e.customer.ID, Lines: []app.LineInput{
		{ProductID: e.product.ID, Quantity: 14000},
		{ProductID: e.integ.ID, Quantity: 1000},
	}}
}

func TestReferenceInvoiceRoundTrip(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.CreateInvoice(e.ctx, e.reference()))
	inv = must(e.svc.IssueInvoice(e.ctx, inv.ID, domain.Date{}))
	if inv.Number != "CD/1195/26" || inv.Totals.Total != 325116 || inv.DueDate != domain.NewDate(2026, 9, 7) {
		t.Fatalf("issued = %q %d %v", inv.Number, inv.Totals.Total, inv.DueDate)
	}
	if inv.Bank.AccountNumber != "1234567890" || len(inv.Lines) != 2 || inv.Lines[0].UnitPrice != 19294 {
		t.Fatalf("snapshot = %+v %+v", inv.Bank, inv.Lines)
	}
	got := must(e.svc.GetInvoice(e.ctx, inv.ID))
	if got.Invoice.BillTo.Address != "P O BOX 2000,\nGERMISTON,\n1401" || len(got.Events) != 2 {
		t.Fatalf("detail = %+v", got)
	}
	m := must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{IdempotencyKey: "abc"}))
	if len(m.CC) != 2 {
		t.Fatalf("cc = %v", m.CC)
	}
	if again := must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{IdempotencyKey: "abc"})); again.ID != m.ID {
		t.Fatal("idempotent send")
	}
	if res := must(e.svc.DispatchEmails(e.ctx, 5)); res.Sent != 1 {
		t.Fatalf("dispatch = %+v", res)
	}
	got = must(e.svc.GetInvoice(e.ctx, inv.ID))
	if got.Invoice.Status != domain.InvoiceSent || got.Emails[0].Status != domain.EmailSent {
		t.Fatalf("status = %v / %v", got.Invoice.Status, got.Emails[0].Status)
	}
	d := must(e.svc.GetDashboard(e.ctx))
	if len(d.Totals) != 1 || d.Totals[0].Outstanding != 325116 {
		t.Fatalf("totals = %+v", d.Totals)
	}
	e.clock.Set(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if over, n := must3(e.svc.ListInvoices(e.ctx, ports.InvoiceFilter{Overdue: true})); n != 1 || over[0].ID != inv.ID {
		t.Fatal("overdue query")
	}
	if _, n := must3(e.svc.ListInvoices(e.ctx, ports.InvoiceFilter{Query: "1195", Status: domain.InvoiceSent, CustomerID: e.customer.ID})); n != 1 {
		t.Fatal("filters")
	}
}

func must3[T any](v []T, n int, err error) ([]T, int) {
	if err != nil {
		panic(err)
	}
	return v, n
}

func TestConstraintsMapToDomainErrors(t *testing.T) {
	e := setup(t)
	// Second default bank account for ZAR: the app moves the default, and the schema's
	// unique generated column backs it up if anything bypasses the app.
	b2 := must(e.svc.CreateBankAccount(e.ctx, domain.BankAccount{Currency: "ZAR", Label: "FNB", BankName: "FNB", AccountNumber: "1", Country: "South Africa", SwiftBIC: "FIRNZAJJ", Active: true, IsDefault: true}))
	if b2.Country != "South Africa" || b2.SwiftBIC != "FIRNZAJJ" {
		t.Fatalf("bank round trip: %+v", b2)
	}
	accts := must(e.svc.ListBankAccounts(e.ctx, "ZAR"))
	defaults := 0
	for _, a := range accts {
		if a.IsDefault {
			defaults++
		}
	}
	if defaults != 1 || !b2.IsDefault {
		t.Fatalf("defaults = %d", defaults)
	}
	err := e.store.Repos().Banks.Create(e.ctx, domain.BankAccount{ID: "raw-dup-default", Currency: "ZAR", Label: "x", BankName: "x", AccountNumber: "x", Active: true, IsDefault: true})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("schema default guard: %v", err)
	}
	if _, err := e.svc.CreateCustomer(e.ctx, e.customer); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate code: %v", err)
	}
	must(e.svc.CreateInvoice(e.ctx, e.reference()))
	if err := e.svc.DeleteCustomer(e.ctx, e.customer.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("FK restrict: %v", err)
	}
	c := e.customer
	c.Name = "Renamed"
	if _, err := e.svc.UpdateCustomer(e.ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateCustomer(e.ctx, c); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if _, err := e.svc.UpdateCurrency(e.ctx, "USD", "US Dollar", "$", true); err != nil {
		t.Fatalf("no-op update must not be not-found: %v", err)
	}
	p := e.product
	p.Prices = map[domain.CurrencyCode]int64{"ZAR": 20000}
	p = must(e.svc.UpdateProduct(e.ctx, p))
	if len(p.Prices) != 1 || p.Prices["ZAR"] != 20000 {
		t.Fatalf("prices replaced: %v", p.Prices)
	}
	if _, n := must3(e.svc.ListProducts(e.ctx, ports.ProductFilter{Currency: "USD"})); n != 0 {
		t.Fatal("currency filter")
	}
	if _, n := must3(e.svc.ListCustomers(e.ctx, ports.CustomerFilter{Query: "renam"})); n != 1 {
		t.Fatal("customer search")
	}
	settings := must(e.svc.GetBusiness(e.ctx))
	if _, err := e.svc.UpdateBusiness(e.ctx, settings.Profile, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateBusiness(e.ctx, settings.Profile, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale profile: %v", err)
	}
}

// TestConcurrentRunnersIssueOneInvoicePerPeriod runs the schedule runner from several
// goroutines at once (as several replicas would) and checks no period is invoiced twice
// and no invoice number is skipped.
func TestConcurrentRunnersIssueOneInvoicePerPeriod(t *testing.T) {
	e := setup(t)
	e.clock.Set(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	for i := range 4 {
		c := must(e.svc.CreateCustomer(e.ctx, domain.Customer{Code: "C" + strconv.Itoa(i), Name: "Customer " + strconv.Itoa(i), Email: "c@x.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))
		must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
			CustomerID: c.ID, Name: "monthly " + strconv.Itoa(i),
			Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 1, 1)},
			Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 1000}}, AutoSend: true,
		}))
	}
	// Created at 1 Jan 00:00, so the first run is 1 Feb (strictly after creation). The
	// server is down until mid-June: 4 schedules × 5 periods (Feb–Jun) to catch up.
	e.clock.Set(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	var wg sync.WaitGroup
	var mu sync.Mutex
	issued := 0
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				res, err := e.svc.RunDueSchedules(e.ctx, 3)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				issued += res.Issued
				mu.Unlock()
				if res.Issued == 0 && res.Failed == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	invs, n := must3(e.svc.ListInvoices(e.ctx, ports.InvoiceFilter{Page: ports.Page{Limit: 200}}))
	scheds, _ := must3(e.svc.ListSchedules(e.ctx, ports.ScheduleFilter{}))
	for _, s := range scheds {
		if s.FailureCount > 0 {
			t.Errorf("schedule %s failed %d times: %s", s.Name, s.FailureCount, s.LastError)
		}
	}
	if issued != 20 || n != 20 {
		t.Fatalf("issued %d, stored %d, want 20", issued, n)
	}
	numbers := map[string]bool{}
	for _, inv := range invs {
		numbers[inv.Number] = true
	}
	if len(numbers) != 20 {
		t.Fatalf("duplicate numbers: %d unique", len(numbers))
	}
	if next := must(e.svc.GetBusiness(e.ctx)).NextSequence; next != 1195+20 {
		t.Fatalf("gap in numbering: next = %d", next)
	}
	// Concurrent dispatchers deliver each e-mail exactly once.
	var dwg sync.WaitGroup
	for range 3 {
		dwg.Add(1)
		go func() {
			defer dwg.Done()
			for {
				res, err := e.svc.DispatchEmails(e.ctx, 5)
				if err != nil {
					t.Error(err)
					return
				}
				if res.Sent == 0 && res.Failed == 0 {
					return
				}
			}
		}()
	}
	dwg.Wait()
	if e.mailer.Count() != 20 {
		t.Fatalf("sent %d e-mails, want 20", e.mailer.Count())
	}
	// Concurrent writers still produce one unbroken hash chain.
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK || v.Entries < 20 {
		t.Fatalf("audit chain after concurrent runs: %+v", v)
	}
}

func TestScheduleAndOutboxPersistence(t *testing.T) {
	e := setup(t)
	d := must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
		CustomerID: e.customer.ID, Name: "Acme",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 21, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1), EndDate: domain.NewDate(2027, 1, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 14000}, {ProductID: e.integ.ID, Quantity: 1000, UnitPrice: p64(55000)}},
		Header:     domain.InvoiceHeader{SalesPerson: "Albie"}, AutoSend: true, EmailSubject: "Inv {{invoice_number}}",
	}))
	got := must(e.svc.GetSchedule(e.ctx, d.Schedule.ID))
	s := got.Schedule
	if len(s.Lines) != 2 || s.Lines[0].UnitPrice != nil || *s.Lines[1].UnitPrice != 55000 || s.Recurrence.EndDate != domain.NewDate(2027, 1, 1) {
		t.Fatalf("schedule round trip: %+v", s)
	}
	e.clock.Set(time.Date(2026, 9, 21, 7, 0, 0, 0, time.UTC))
	// Created 21 Aug 08:00 SAST; the 21 Aug and 21 Sep 09:00 SAST runs are both due now.
	must(e.svc.RunDueSchedules(e.ctx, 10))
	invs, n := must3(e.svc.ListInvoices(e.ctx, ports.InvoiceFilter{}))
	if n != 2 {
		t.Fatalf("invoices = %d", n)
	}
	if invs[0].Header.SalesPerson != "Albie" || invs[0].ScheduleID != s.ID {
		t.Fatalf("header = %+v", invs[0])
	}
	full := must(e.svc.GetInvoice(e.ctx, invs[0].ID))
	if full.Invoice.Lines[1].Description != "Alchamex integration - monthly subscription" {
		t.Fatalf("period text: %q", full.Invoice.Lines[1].Description)
	}
	runs := must(e.svc.GetSchedule(e.ctx, s.ID)).Runs
	if len(runs) != 2 || runs[0].Period != domain.NewDate(2026, 9, 21) {
		t.Fatalf("runs = %+v", runs)
	}
	ems, n := must3(e.svc.ListEmails(e.ctx, ports.EmailFilter{Status: domain.EmailPending}))
	if n != 2 || ems[0].Subject[:4] != "Inv " {
		t.Fatalf("outbox = %+v", ems)
	}
	st := must(e.svc.OutboxStats(e.ctx))
	if st.Pending != 2 || st.OldestPending.IsZero() {
		t.Fatalf("stats = %+v", st)
	}
	must(e.svc.CancelEmail(e.ctx, ems[0].ID))
	if res := must(e.svc.DispatchEmails(e.ctx, 10)); res.Sent != 1 {
		t.Fatalf("one cancelled, one sent: %+v", res)
	}
	must(e.svc.PauseSchedule(e.ctx, s.ID))
	if err := e.svc.DeleteSchedule(e.ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if inv := must(e.svc.GetInvoice(e.ctx, invs[0].ID)); inv.Invoice.ScheduleID != "" {
		t.Fatal("invoice keeps existing after schedule delete, unlinked")
	}
}

func TestAuthAndIdempotencyPersistence(t *testing.T) {
	e := setup(t)
	must(e.svc.CreateUser(e.ctx, "albie@example.com", "correct horse battery"))
	tok, _, err := e.svc.Login(e.ctx, "albie@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if u := must(e.svc.Authenticate(e.ctx, tok)); u.Email != "albie@example.com" {
		t.Fatal("auth")
	}
	if err := e.svc.SetPassword(e.ctx, "albie@example.com", "another long password"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Logout(e.ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(e.ctx, tok); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("logout")
	}
	idem := e.svc.Idempotency()
	rec := ports.IdempotencyRecord{Key: "k", Fingerprint: "f", CreatedAt: e.clock.Now()}
	if err := idem.Reserve(e.ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := idem.Reserve(e.ctx, rec); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("dup key")
	}
	if err := idem.Complete(e.ctx, "k", 201, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if got := must(idem.Get(e.ctx, "k")); got.Status != 201 || string(got.Body) != `{"ok":true}` {
		t.Fatalf("record = %+v", got)
	}
	e.clock.Advance(48 * time.Hour)
	if err := e.svc.Housekeeping(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := idem.Get(e.ctx, "k"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expired key removed")
	}
	if err := e.store.Ping(e.ctx); err != nil {
		t.Fatal(err)
	}
}

// TestAuditLogIsAppendOnly proves the database itself refuses to change or delete audit
// entries, and that the chain survives a real MariaDB round trip.
func TestAuditLogIsAppendOnly(t *testing.T) {
	e := setup(t)
	ctx := app.WithActor(e.ctx, app.Actor{ID: "u1", Name: "albie"})
	inv := must(e.svc.CreateInvoice(ctx, e.reference()))
	must(e.svc.IssueInvoice(ctx, inv.ID, domain.Date{}))
	d := must(e.svc.GetInvoice(e.ctx, inv.ID))
	if len(d.Events) != 2 || d.Events[0].ActorName != "albie" || d.Events[1].Action != "invoice.issued" {
		t.Fatalf("events = %+v", d.Events)
	}
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK {
		t.Fatalf("verify = %+v", v)
	}
	if _, err := testDB.ExecContext(e.ctx, "UPDATE audit_log SET actor_name = 'mallory'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update must be rejected: %v", err)
	}
	if _, err := testDB.ExecContext(e.ctx, "DELETE FROM audit_log"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete must be rejected: %v", err)
	}
	if err := e.svc.DeleteInvoice(ctx, must(e.svc.CreateInvoice(ctx, e.reference())).ID); err != nil {
		t.Fatal(err)
	}
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK {
		t.Fatalf("verify after draft delete = %+v", v)
	}
}

// TestLegacyInvoiceEventsAreBackfilled migrates a database that still has invoice_events
// rows to the latest version and checks they land in a valid chain.
func TestLegacyInvoiceEventsAreBackfilled(t *testing.T) {
	ctx := context.Background()
	if err := mariadb.Migrate(ctx, testDB, true); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.MigrateTo(ctx, testDB, 7); err != nil {
		t.Fatal(err)
	}
	conn, err := testDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatal(err)
	}
	for i, typ := range []string{"created", "issued", "paid"} {
		if _, err := conn.ExecContext(ctx, "INSERT INTO invoice_events (id, invoice_id, type, detail, at) VALUES (?, 'inv-legacy', ?, '', ?)",
			fmt.Sprintf("ev-%d", i), typ, time.Date(2026, 8, 21, 9, i, 0, 123456000, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1")
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	svc := app.New(app.Deps{Tx: mariadb.NewStore(testDB), Clock: &fakes.Clock{T: time.Now()}, IDs: idgen.UUIDv7{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	entries, n := must3(svc.ListAudit(ctx, ports.AuditFilter{EntityID: "inv-legacy"}))
	if n != 3 || entries[2].Action != "invoice.created" || entries[0].ActorName != "unknown (before the audit log)" {
		t.Fatalf("backfill = %+v", entries)
	}
	if v := must(svc.VerifyAudit(ctx)); !v.OK || v.Entries != 3 {
		t.Fatalf("verify = %+v", v)
	}
}

// TestScheduleWeekendRulePersistence: migration 00016 stores the weekend rule (existing
// rows: none), and a run moved to the Friday before is claimed, issued that day and
// recorded for its nominal period.
func TestScheduleWeekendRulePersistence(t *testing.T) {
	e := setup(t)
	var def string
	if err := testDB.QueryRowContext(e.ctx, `SELECT COLUMN_DEFAULT FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'schedules' AND COLUMN_NAME = 'weekend_rule'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if strings.Trim(def, "'") != "none" {
		t.Fatalf("existing schedules must default to none, column default %q", def)
	}
	e.clock.Set(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	d := must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
		CustomerID: e.customer.ID, Name: "Monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 10, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 1000}},
	}))
	got := must(e.svc.GetSchedule(e.ctx, d.Schedule.ID)).Schedule
	if got.Recurrence.Weekend != domain.WeekendPreviousFriday || !got.NextRunAt.Equal(time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)) ||
		got.Period() != domain.NewDate(2026, 11, 1) {
		t.Fatalf("round trip: rule %q, next run %v, period %v", got.Recurrence.Weekend, got.NextRunAt, got.Period())
	}
	if _, err := testDB.ExecContext(e.ctx, `UPDATE schedules SET weekend_rule = 'monday-after' WHERE id = ?`, got.ID); err == nil {
		t.Fatal("an unknown weekend rule must be refused by the CHECK constraint")
	}
	e.clock.Set(time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC))
	if res := must(e.svc.RunDueSchedules(e.ctx, 10)); res.Issued != 1 {
		t.Fatalf("moved run: %+v", res)
	}
	if res := must(e.svc.RunDueSchedules(e.ctx, 10)); res.Issued != 0 {
		t.Fatalf("ran twice: %+v", res)
	}
	invs, _ := must3(e.svc.ListInvoices(e.ctx, ports.InvoiceFilter{}))
	runs := must(e.svc.GetSchedule(e.ctx, got.ID)).Runs
	if len(invs) != 1 || invs[0].IssueDate != domain.NewDate(2026, 10, 30) || len(runs) != 1 || runs[0].Period != domain.NewDate(2026, 11, 1) {
		t.Fatalf("invoices %+v, runs %+v", invs, runs)
	}
	upd := *must(e.svc.GetSchedule(e.ctx, got.ID)).Schedule
	upd.Recurrence.Weekend = domain.WeekendNone
	if s := must(e.svc.UpdateSchedule(e.ctx, upd)).Schedule; s.Recurrence.Weekend != domain.WeekendNone {
		t.Fatalf("none not stored: %q", s.Recurrence.Weekend)
	}
}

func TestCustomersGetMany(t *testing.T) {
	e := setup(t)
	other := must(e.svc.CreateCustomer(e.ctx, domain.Customer{Code: "US", Name: "Acme Inc", Email: "ap@acme.example", Currency: "USD", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))
	repo := e.store.Repos().Customers
	got := must(repo.GetMany(e.ctx, []string{e.customer.ID, other.ID, e.customer.ID, "missing"}))
	if len(got) != 2 || got[e.customer.ID].Currency != "ZAR" || got[other.ID].Name != "Acme Inc" || len(got[e.customer.ID].CC) != 2 {
		t.Fatalf("GetMany = %+v", got)
	}
	if none := must(repo.GetMany(e.ctx, nil)); len(none) != 0 {
		t.Fatalf("no ids: %+v", none)
	}
}

// TestScheduleRunPeriodKey: migration 00017 keys runs by billing period — existing runs are
// backfilled from their schedule's frequency, a month billed twice before keeps both runs
// (the later one under its date) — and moving the day within a billed month raises no
// second invoice while the next month is still raised.
func TestScheduleRunPeriodKey(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	monthly := must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
		CustomerID: e.customer.ID, Name: "Monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 15, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 1000}},
	})).Schedule
	weekly := must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
		CustomerID: e.customer.ID, Name: "Weekly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyWeekly, Weekday: time.Monday, SendHour: 9, StartDate: domain.NewDate(2026, 8, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 1000}},
	})).Schedule
	// Back to schema 16 (before the period key), where runs were keyed by their date.
	provider, err := goose.NewProvider(goose.DialectMySQL, testDB, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ id, period string }{
		{monthly.ID, "2026-09-15"}, {monthly.ID, "2026-09-20"}, // September billed twice under the old key
		{monthly.ID, "2026-10-15"}, {weekly.ID, "2026-09-07"}, {weekly.ID, "2026-09-14"},
	} {
		if _, err := testDB.ExecContext(ctx, `INSERT INTO schedule_runs (schedule_id, period, invoice_id, at) VALUES (?, ?, ?, NOW())`, r.id, r.period, "00000000-0000-0000-0000-000000000000"); err != nil {
			t.Fatal(err)
		}
	}
	if err := mariadb.MigrateTo(ctx, testDB, 17); err != nil {
		t.Fatal(err)
	}
	rows, err := testDB.QueryContext(ctx, `SELECT s.name, r.period, r.period_key FROM schedule_runs r JOIN schedules s ON s.id = r.schedule_id ORDER BY s.name, r.period`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name, key string
		var period time.Time
		if err := rows.Scan(&name, &period, &key); err != nil {
			t.Fatal(err)
		}
		got = append(got, name+" "+period.Format(time.DateOnly)+"="+key)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := "Monthly 2026-09-15=2026-09 Monthly 2026-09-20=2026-09-20 Monthly 2026-10-15=2026-10 Weekly 2026-09-07=2026-09-07 Weekly 2026-09-14=2026-09-14"
	if strings.Join(got, " ") != want {
		t.Fatalf("backfill:\n got %v\nwant %v", got, want)
	}

	// At head: October ran (backfilled); moving the day to the 20th raises no second October
	// invoice, and November is raised on the 20th.
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteSchedule(e.ctx, weekly.ID); err != nil { // only the monthly one runs below
		t.Fatal(err)
	}
	e.clock.Set(time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC))
	upd := *must(e.svc.GetSchedule(e.ctx, monthly.ID)).Schedule
	upd.Recurrence.DayOfMonth = 20
	upd.Recurrence.StartDate = domain.NewDate(2026, 10, 1)
	must(e.svc.UpdateSchedule(e.ctx, upd))
	e.clock.Set(time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC))
	if res := must(e.svc.RunDueSchedules(e.ctx, 10)); res.Issued != 0 {
		t.Fatalf("October billed again: %+v", res)
	}
	e.clock.Set(time.Date(2026, 11, 20, 7, 0, 0, 0, time.UTC))
	if res := must(e.svc.RunDueSchedules(e.ctx, 10)); res.Issued != 1 {
		t.Fatalf("November not raised once: %+v", res)
	}
	runs := must(e.svc.GetSchedule(e.ctx, monthly.ID)).Runs
	if runs[0].Period != domain.NewDate(2026, 11, 20) || runs[0].PeriodKey != "2026-11" {
		t.Fatalf("latest run = %+v", runs[0])
	}
}
