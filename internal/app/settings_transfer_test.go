package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// Settings → Import/Export (M22, Albie 2026-10-17): customers, currencies, bank accounts,
// the business and the e-mail server move between installations as one file.

// withSMTP saves an enabled mail server with a password, and a second ZAR account that the
// customer is paid into.
func (h *harness) withSMTPAndOverride() *harness {
	h.svc = app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: h.ids, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: fakes.Cipher{}})
	pw := "app-password"
	must(h.svc.UpdateSMTPSettings(h.ctx, domain.SMTPSettings{
		Enabled: true, Host: "smtp.gmail.com", Port: 587, Username: "billing@example.com", From: "Example <billing@example.com>", TLS: "starttls",
	}, &pw))
	cheque := must(h.svc.CreateBankAccount(h.ctx, domain.BankAccount{
		Currency: "ZAR", Label: "FNB cheque", BankName: "FNB", AccountNumber: "62000000001", Active: true,
	}))
	c := h.customer
	c.BankAccountID = cheque.ID
	h.customer = must(h.svc.UpdateCustomer(h.ctx, c))
	return h
}

func TestSettingsMoveToANewInstallation(t *testing.T) {
	src := newHarness(t).seed().withSMTPAndOverride()
	file := must(src.svc.ExportSettings(src.ctx))
	if file.Business == nil || file.Business.NextSequence != 1195 || file.EmailServer == nil ||
		len(file.Currencies) != 2 || len(file.BankAccounts) != 2 || len(file.Customers) != 1 {
		t.Fatalf("export: %+v", file)
	}

	dst := newHarness(t)
	check := must(dst.svc.ImportSettings(dst.ctx, file, false))
	if check.Applied || check.Currencies.Created != 2 || check.BankAccounts.Created != 2 || check.Customers.Created != 1 ||
		check.Business.Updated != 1 || check.EmailServer.Created != 1 {
		t.Fatalf("check: %+v", check)
	}
	if cs := must(dst.svc.ListCurrencies(dst.ctx)); len(cs) != 0 {
		t.Fatalf("a check saved currencies: %v", cs)
	}
	if n := auditCount(dst, "settings.imported"); n != 0 {
		t.Fatalf("a check was audited %d times", n)
	}

	res := must(dst.svc.ImportSettings(dst.ctx, file, true))
	if !res.Applied || res.Business != (app.ImportCounts{Updated: 1}) || res.EmailServer != (app.ImportCounts{Created: 1}) ||
		res.Currencies != (app.ImportCounts{Created: 2}) || res.BankAccounts != (app.ImportCounts{Created: 2}) || res.Customers != (app.ImportCounts{Created: 1}) {
		t.Fatalf("import: %+v", res)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "switched off") {
		t.Fatalf("warnings: %v", res.Warnings)
	}

	b := must(dst.svc.GetBusiness(dst.ctx))
	want := file.Business.Profile
	want.Version = b.Profile.Version
	if b.Profile != want || b.NextSequence != 1195 {
		t.Fatalf("business %+v next %d", b.Profile, b.NextSequence)
	}
	smtp := must(dst.svc.GetSMTPSettings(dst.ctx))
	if smtp.Settings.Enabled || smtp.PasswordSet || smtp.Settings.Host != "smtp.gmail.com" || smtp.Settings.From != "Example <billing@example.com>" {
		t.Fatalf("e-mail server: %+v", smtp)
	}
	banks := must(dst.svc.ListBankAccounts(dst.ctx, "ZAR"))
	var cheque domain.BankAccount
	for _, a := range banks {
		if a.AccountNumber == "62000000001" {
			cheque = a
		}
	}
	if len(banks) != 2 || cheque.ID == "" {
		t.Fatalf("banks: %+v", banks)
	}
	cs, _ := must3(dst.svc.ListCustomers(dst.ctx, ports.CustomerFilter{IncludeInactive: true}))
	if len(cs) != 1 || cs[0].Code != "CD" || cs[0].BankAccountID != cheque.ID || cs[0].Name != src.customer.Name ||
		len(cs[0].CC) != 1 || cs[0].Terms != src.customer.Terms {
		t.Fatalf("customers: %+v (cheque %s)", cs, cheque.ID)
	}
	if n := auditCount(dst, "settings.imported"); n != 1 {
		t.Fatalf("import audited %d times", n)
	}
	if n := auditCount(src, "settings.exported"); n != 1 {
		t.Fatalf("export audited %d times", n)
	}
	// The imported installation invoices with the imported settings.
	p := must(dst.svc.CreateProduct(dst.ctx, domain.Product{SKU: "X", Name: "X", Prices: map[domain.CurrencyCode]int64{"ZAR": 100}, Active: true}))
	inv := must(dst.svc.CreateInvoice(dst.ctx, app.InvoiceInput{CustomerID: cs[0].ID, Lines: []app.LineInput{{ProductID: p.ID, Quantity: 1000}}}))
	inv = must(dst.svc.IssueInvoice(dst.ctx, inv.ID, domain.Date{}))
	if inv.Bank.AccountNumber != "62000000001" || inv.Number != "CD/1195/26" {
		t.Fatalf("invoice %s paid into %+v", inv.Number, inv.Bank)
	}
}

func TestSettingsReimportUpdatesAndNeverGoesBack(t *testing.T) {
	h := newHarness(t).seed().withSMTPAndOverride()
	file := must(h.svc.ExportSettings(h.ctx))

	// The same file again changes nothing.
	same := must(h.svc.ImportSettings(h.ctx, file, true))
	if same.Currencies != (app.ImportCounts{Unchanged: 2}) || same.BankAccounts != (app.ImportCounts{Unchanged: 2}) ||
		same.Customers != (app.ImportCounts{Unchanged: 1}) || same.Business != (app.ImportCounts{Unchanged: 1}) ||
		same.EmailServer != (app.ImportCounts{Unchanged: 1}) || len(same.Warnings) != 0 {
		t.Fatalf("same file: %+v", same)
	}

	// Numbers issued since the export: the file's next number is behind.
	must(h.svc.UpdateBusiness(h.ctx, must(h.svc.GetBusiness(h.ctx)).Profile, 1300))
	file.Customers[0].Name = "Acme Spares"
	file.Customers = append(file.Customers, domain.Customer{
		Code: "nw", Name: "New Co", Email: "a@new.example", Currency: "usd", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true,
	})
	file.Currencies[0].Name += " (renamed)"
	file.BankAccounts[0].Label = "Renamed"
	file.EmailServer.Port = 465
	file.EmailServer.TLS = "tls"
	res := must(h.svc.ImportSettings(h.ctx, file, true))
	if res.Customers != (app.ImportCounts{Created: 1, Updated: 1}) || res.Currencies != (app.ImportCounts{Updated: 1, Unchanged: 1}) ||
		res.BankAccounts != (app.ImportCounts{Updated: 1, Unchanged: 1}) || res.EmailServer != (app.ImportCounts{Updated: 1}) {
		t.Fatalf("changes: %+v", res)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "stays 1300") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	if b := must(h.svc.GetBusiness(h.ctx)); b.NextSequence != 1300 {
		t.Fatalf("next sequence went back to %d", b.NextSequence)
	}
	if c := must(h.svc.GetCustomer(h.ctx, h.customer.ID)); c.Name != "Acme Spares" {
		t.Fatalf("customer not updated in place: %+v", c)
	}
	// The stored password is kept, so the server stays on.
	if s := must(h.svc.GetSMTPSettings(h.ctx)); !s.Settings.Enabled || !s.PasswordSet || s.Settings.Port != 465 {
		t.Fatalf("e-mail server: %+v", s)
	}
	// A forward next number is taken.
	file.Business.NextSequence = 2000
	must(h.svc.ImportSettings(h.ctx, file, true))
	if b := must(h.svc.GetBusiness(h.ctx)); b.NextSequence != 2000 {
		t.Fatalf("next sequence %d", b.NextSequence)
	}
	// A section left out is not touched.
	partial := app.SettingsFile{Currencies: []domain.Currency{{Code: "EUR", Name: "Euro", Symbol: "€", MinorUnits: 2, Active: true}}}
	if r := must(h.svc.ImportSettings(h.ctx, partial, true)); r.Currencies.Created != 1 || r.Customers != (app.ImportCounts{}) || r.Business != (app.ImportCounts{}) {
		t.Fatalf("partial: %+v", r)
	}
}

func TestSettingsImportIsAllOrNothing(t *testing.T) {
	h := newHarness(t).seed()
	good := domain.Currency{Code: "EUR", Name: "Euro", Symbol: "€", MinorUnits: 2, Active: true}
	cust := func(code, email string) domain.Customer {
		return domain.Customer{Code: code, Name: "X", Email: email, Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}
	}
	tests := []struct {
		name  string
		file  app.SettingsFile
		field string
	}{
		{"bad e-mail", app.SettingsFile{Currencies: []domain.Currency{good}, Customers: []domain.Customer{cust("ok", "a@b.example"), cust("BAD", "nope")}}, "customers[BAD].email"},
		{"customer twice", app.SettingsFile{Customers: []domain.Customer{cust("AA", "a@b.example"), cust("aa", "c@d.example")}}, "customers[AA].code"},
		{"minor units changed", app.SettingsFile{Currencies: []domain.Currency{good, {Code: "ZAR", Name: "Rand", MinorUnits: 0, Active: true}}}, "currencies[ZAR].minorUnits"},
		{"unknown currency", app.SettingsFile{BankAccounts: []domain.BankAccount{{Currency: "GBP", Label: "L", BankName: "B", AccountNumber: "1", Active: true}}}, "bankAccounts[L]"},
		{"unknown bank account", app.SettingsFile{Customers: []domain.Customer{func() domain.Customer { c := cust("ZZ", "z@z.example"); c.BankAccountID = "elsewhere"; return c }()}}, "customers[ZZ]"},
		{"bank account in another currency", app.SettingsFile{BankAccounts: []domain.BankAccount{{ID: h.bank.ID, Currency: "USD", Label: "L", BankName: "B", AccountNumber: "1", Active: true}}}, "bankAccounts[L].currency"},
		{"one account twice", app.SettingsFile{BankAccounts: []domain.BankAccount{
			{ID: h.bank.ID, Currency: "ZAR", Label: "A", BankName: "B", AccountNumber: "999", Active: true},
			{Currency: "ZAR", Label: "C", BankName: "B", AccountNumber: h.bank.AccountNumber, Active: true},
		}}, "bankAccounts[C].id"},
		{"bad number format", app.SettingsFile{Currencies: []domain.Currency{good}, Business: &app.BusinessSettings{Profile: domain.BusinessProfile{Name: "B", NumberFormat: "{code}"}}}, "business.numberFormat"},
		{"bad e-mail server", app.SettingsFile{EmailServer: &domain.SMTPSettings{TLS: "ssl"}}, "emailServer.tls"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.svc.ImportSettings(h.ctx, tc.file, true)
			var ve *domain.ValidationError
			if !errors.As(err, &ve) || ve.Field != tc.field {
				t.Fatalf("err %v, want field %s", err, tc.field)
			}
			if cs := must(h.svc.ListCurrencies(h.ctx)); len(cs) != 2 {
				t.Fatalf("partly imported: %v", cs)
			}
			if _, n := must3(h.svc.ListCustomers(h.ctx, ports.CustomerFilter{IncludeInactive: true})); n != 1 {
				t.Fatalf("partly imported: %d customers", n)
			}
		})
	}
}

func auditCount(h *harness, action string) int {
	_, n := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Action: action}))
	return n
}
