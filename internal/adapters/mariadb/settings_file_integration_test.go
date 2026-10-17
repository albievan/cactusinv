//go:build integration

package mariadb_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/agecrypt"
	"github.com/albievan/cactusinv/internal/adapters/idgen"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// TestSettingsFileImport: on MariaDB a check rolls back, the same file changes nothing
// (versions match), and a changed file updates in place, moves the currency's default
// account and points a customer at a new account by the file's id.
func TestSettingsFileImport(t *testing.T) {
	e := setup(t)
	price := int64(50000)
	must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
		CustomerID: e.customer.ID, Name: "Monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyQuarterly, DayOfMonth: 0, SendHour: 9, SendMinute: 30, StartDate: domain.NewDate(2026, 2, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 14000}, {ProductID: e.integ.ID, Quantity: 1000, UnitPrice: &price, Discount: 500}},
		AutoSend:   true, EmailDelayDays: 2, Notes: "Thanks",
	}))
	file := must(e.svc.ExportSettings(e.ctx))
	same := must(e.svc.ImportSettings(e.ctx, file, true))
	if same.Schedules != (app.ImportCounts{Unchanged: 1}) || same.Products != (app.ImportCounts{Unchanged: 2}) ||
		same.Customers != (app.ImportCounts{Unchanged: 1}) || same.BankAccounts != (app.ImportCounts{Unchanged: 1}) ||
		same.Currencies != (app.ImportCounts{Unchanged: 2}) || same.Business != (app.ImportCounts{Unchanged: 1}) {
		t.Fatalf("same file: %+v", same)
	}

	file.Customers[0].Name = "Acme Spares"
	file.Customers[0].BankAccountID = "file-id-of-the-new-account"
	file.BankAccounts = append(file.BankAccounts, domain.BankAccount{
		ID: "file-id-of-the-new-account", Currency: "ZAR", Label: "FNB", BankName: "FNB", AccountNumber: "62000000001", IsDefault: true, Active: true,
	})
	file.Business.Profile.Phone = "011 555 0100"
	check := must(e.svc.ImportSettings(e.ctx, file, false))
	if check.Applied || check.Customers.Updated != 1 || check.BankAccounts.Created != 1 {
		t.Fatalf("check: %+v", check)
	}
	if c := must(e.svc.GetCustomer(e.ctx, e.customer.ID)); c.Name != e.customer.Name {
		t.Fatalf("a check saved the customer: %q", c.Name)
	}
	if banks := must(e.svc.ListBankAccounts(e.ctx, "ZAR")); len(banks) != 1 {
		t.Fatalf("a check saved a bank account: %+v", banks)
	}

	res := must(e.svc.ImportSettings(e.ctx, file, true))
	if res.Customers.Updated != 1 || res.BankAccounts != (app.ImportCounts{Created: 1, Unchanged: 1}) || res.Business.Updated != 1 {
		t.Fatalf("import: %+v", res)
	}
	var fnb domain.BankAccount
	for _, b := range must(e.svc.ListBankAccounts(e.ctx, "ZAR")) {
		if b.AccountNumber == "62000000001" {
			fnb = b
		} else if b.IsDefault {
			t.Fatalf("the old account is still the default: %+v", b)
		}
	}
	if !fnb.IsDefault {
		t.Fatalf("new account: %+v", fnb)
	}
	c := must(e.svc.GetCustomer(e.ctx, e.customer.ID))
	if c.Name != "Acme Spares" || c.BankAccountID != fnb.ID || len(c.CC) != 2 {
		t.Fatalf("customer: %+v", c)
	}
	if b := must(e.svc.GetBusiness(e.ctx)); b.Profile.Phone != "011 555 0100" || b.NextSequence != 1195 {
		t.Fatalf("business: %+v", b)
	}

	// Real age encryption with a passphrase.
	sealer, err := agecrypt.New("", agecrypt.WithWorkFactor(10))
	if err != nil {
		t.Fatal(err)
	}
	svc := app.New(app.Deps{Tx: e.store, Clock: e.clock, IDs: idgen.UUIDv7{}, Mailer: e.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Backups: app.BackupDeps{Sealer: sealer}})
	plain := []byte(`{"format":"cactusinv-settings","formatVersion":1}`)
	sealed := must(svc.SealSettings(plain, "correct horse battery staple"))
	if !bytes.HasPrefix(sealed, []byte("age-encryption.org/v1")) {
		t.Fatalf("not an age file: %q", sealed[:20])
	}
	if got := must(svc.OpenSettings(sealed, "correct horse battery staple")); !bytes.Equal(got, plain) {
		t.Fatalf("opened %q", got)
	}
	if _, err := svc.OpenSettings(sealed, "the wrong passphrase, long"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("wrong passphrase: %v", err)
	}
}
