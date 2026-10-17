package httpapi_test

import (
	"context"
	"testing"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// The settings file over HTTP (M22): administrators only, the file's own format checked,
// a check saves nothing, and a password in the file is ignored.
func TestSettingsFileOverHTTP(t *testing.T) {
	albie, _, _, svc := newServer(t)
	albie.login()
	ctx := context.Background()
	if _, err := svc.CreateCurrency(ctx, "ZAR", "Rand", "R", 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBankAccount(ctx, domainBank()); err != nil {
		t.Fatal(err)
	}
	cust, err := svc.CreateCustomer(ctx, domainCustomer())
	if err != nil {
		t.Fatal(err)
	}

	price := int64(1500)
	if _, err := svc.CreateProduct(ctx, domain.Product{SKU: "SUP", Name: "Support", Prices: map[domain.CurrencyCode]int64{"ZAR": 12345}, Active: true}); err != nil {
		t.Fatal(err)
	}
	prods, _, _ := svc.ListProducts(ctx, ports.ProductFilter{})
	if _, err := svc.CreateSchedule(ctx, domain.Schedule{
		CustomerID: cust.ID, Name: "Monthly support",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: prods[0].ID, Quantity: 1000, UnitPrice: &price}},
	}); err != nil {
		t.Fatal(err)
	}

	var file obj
	albie.ok(albie.do("POST", "/api/v1/settings/export", obj{}, &file), 200, "export")
	if file["format"] != "cactusinv-settings" || file["formatVersion"] != float64(1) || file["business"] == nil ||
		len(file["currencies"].([]any)) != 1 || len(file["bankAccounts"].([]any)) != 1 || len(file["customers"].([]any)) != 1 ||
		len(file["products"].([]any)) != 1 || len(file["schedules"].([]any)) != 1 {
		t.Fatalf("file = %v", file)
	}
	sched := file["schedules"].([]any)[0].(obj)
	if sched["customerCode"] != "CD" || sched["schedule"].(obj)["lines"].([]any)[0].(obj)["unitPrice"] != "15.00" ||
		file["products"].([]any)[0].(obj)["prices"].([]any)[0].(obj)["unitPrice"] != "123.45" {
		t.Fatalf("schedule = %v, products = %v", sched, file["products"])
	}
	if _, ok := file["emailServer"]; ok {
		t.Fatalf("no e-mail server was saved, yet the file has one: %v", file["emailServer"])
	}

	// A check: nothing is saved.
	file["customers"].([]any)[0].(obj)["name"] = "Acme Renamed"
	file["emailServer"] = obj{"enabled": false, "host": "smtp.example.com", "port": 587, "username": "u@example.com", "from": "", "tls": "starttls", "password": "in the file", "passwordSet": true, "warnings": []string{}, "version": 0}
	var res httpapi.SettingsImportResult
	albie.ok(albie.do("POST", "/api/v1/settings/import", obj{"file": file}, &res), 200, "check")
	if res.Applied || res.Customers.Updated != 1 || res.EmailServer.Created != 1 || res.Products.Unchanged != 1 || res.Schedules.Unchanged != 1 {
		t.Fatalf("check = %+v", res)
	}
	if c, _ := svc.GetCustomer(ctx, cust.ID); c.Name != "Acme" {
		t.Fatalf("a check saved %q", c.Name)
	}
	// Only the chosen sections: the e-mail server is left out.
	albie.ok(albie.do("POST", "/api/v1/settings/import", obj{"file": file, "sections": []string{"customers"}}, &res), 200, "check customers only")
	if res.Customers.Updated != 1 || res.EmailServer != (httpapi.ImportCounts{}) || res.Currencies != (httpapi.ImportCounts{}) {
		t.Fatalf("customers only = %+v", res)
	}
	albie.ok(albie.do("POST", "/api/v1/settings/import?apply=true", obj{"file": file}, &res, "Idempotency-Key", "imp-1"), 200, "import")
	if !res.Applied || res.Customers.Updated != 1 || res.Currencies.Unchanged != 1 || res.BankAccounts.Unchanged != 1 {
		t.Fatalf("import = %+v", res)
	}
	if c, _ := svc.GetCustomer(ctx, cust.ID); c.Name != "Acme Renamed" {
		t.Fatalf("customer = %q", c.Name)
	}
	var es httpapi.EmailSettings
	albie.ok(albie.do("GET", "/api/v1/settings/email", nil, &es), 200, "e-mail settings")
	if es.Host != "smtp.example.com" || es.PasswordSet {
		t.Fatalf("e-mail server = %+v (a password in the file must be ignored)", es)
	}
	if _, n, _ := svc.ListAudit(ctx, ports.AuditFilter{Action: "settings.exported"}); n != 1 {
		t.Fatalf("export audited %d times", n)
	}

	// A protected file: only the encrypted part, opened with its passphrase.
	var p httpapi.Problem
	var sealed obj
	albie.ok(albie.do("POST", "/api/v1/settings/export", obj{"passphrase": "short"}, &p), 422, "short passphrase")
	albie.ok(albie.do("POST", "/api/v1/settings/export", obj{"passphrase": "correct horse battery staple"}, &sealed), 200, "protected export")
	if sealed["encrypted"] == nil || sealed["customers"] != nil || sealed["format"] != "cactusinv-settings" {
		t.Fatalf("protected file = %v", sealed)
	}
	albie.ok(albie.do("POST", "/api/v1/settings/import", obj{"file": sealed}, &p), 422, "no passphrase")
	albie.ok(albie.do("POST", "/api/v1/settings/import", obj{"file": sealed, "passphrase": "wrong passphrase, long enough"}, &p), 422, "wrong passphrase")
	if p.Field != "passphrase" {
		t.Fatalf("problem = %+v", p)
	}
	albie.ok(albie.do("POST", "/api/v1/settings/import", obj{"file": sealed, "passphrase": "correct horse battery staple"}, &res), 200, "open")
	if res.Customers.Unchanged != 1 || res.Schedules.Unchanged != 1 || res.Products.Unchanged != 1 {
		t.Fatalf("protected check = %+v", res)
	}

	// Not a settings file, a newer one, or an error inside: 422 naming the field.
	for _, tc := range []struct {
		name string
		body obj
	}{
		{"backup file", obj{"format": "something-else", "formatVersion": 1}},
		{"newer format", obj{"format": "cactusinv-settings", "formatVersion": 2}},
		{"bad customer", obj{"format": "cactusinv-settings", "formatVersion": 1, "customers": []obj{{"code": "X", "name": "X", "email": "nope", "currency": "ZAR", "paymentTerms": obj{"kind": "on_receipt", "value": 0}}}}},
	} {
		albie.ok(albie.do("POST", "/api/v1/settings/import?apply=true", obj{"file": tc.body}, &p), 422, tc.name)
	}
	if p.Field != "customers[X].email" {
		t.Fatalf("problem = %+v", p)
	}

	// Not for a user without administration, however many settings they may change.
	var carol httpapi.User
	albie.ok(albie.do("POST", "/api/v1/users", obj{"email": "carol@example.com", "password": "chosen by albie", "currentPassword": albiePW}, &carol), 201, "add carol")
	all := []string{}
	for _, pi := range domain.Permissions {
		all = append(all, string(pi.Key))
	}
	albie.ok(albie.do("POST", "/api/v1/groups", obj{"name": "Everything", "permissions": all, "memberIds": []string{carol.Id}, "currentPassword": albiePW}, nil), 201, "group")
	cc := albie.second()
	cc.ok(cc.loginAs("carol@example.com", "chosen by albie"), 204, "carol signs in")
	cc.ok(cc.do("POST", "/api/v1/auth/password", obj{"currentPassword": "chosen by albie", "newPassword": "carol's own password"}, nil), 204, "carol's password")
	cc.ok(cc.do("POST", "/api/v1/settings/export", obj{}, &p), 403, "export")
	cc.ok(cc.do("POST", "/api/v1/settings/import", obj{"file": file}, &p), 403, "import")
}
