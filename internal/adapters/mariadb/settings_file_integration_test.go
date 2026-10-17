//go:build integration

package mariadb_test

import (
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// TestSettingsFileImport: on MariaDB a check rolls back, the same file changes nothing
// (versions match), and a changed file updates in place, moves the currency's default
// account and points a customer at a new account by the file's id.
func TestSettingsFileImport(t *testing.T) {
	e := setup(t)
	file := must(e.svc.ExportSettings(e.ctx))
	same := must(e.svc.ImportSettings(e.ctx, file, true))
	if same.Customers != (app.ImportCounts{Unchanged: 1}) || same.BankAccounts != (app.ImportCounts{Unchanged: 1}) ||
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
}
