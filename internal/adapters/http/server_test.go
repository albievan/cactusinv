package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

type client struct {
	t    *testing.T
	srv  *httptest.Server
	http *http.Client
}

func newServer(t *testing.T) (*client, *fakes.Store, *fakes.Clock, *app.Service) {
	t.Helper()
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}
	svc := app.New(app.Deps{Tx: store, Clock: clock, IDs: &fakes.IDs{}, Mailer: &fakes.Mailer{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	if _, err := svc.CreateUser(context.Background(), "albie@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>cactusInv</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := httpapi.NewHandler(httpapi.Options{
		Service: svc, Clock: clock, Web: web,
		Ready: map[string]httpapi.ReadyCheck{"db": func(context.Context) error { return nil }},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: srv, http: &http.Client{Jar: jar}}, store, clock, svc
}

// do sends a request with the CSRF header and decodes a JSON response into out.
func (c *client) do(method, path string, body any, out any, headers ...string) int {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, c.srv.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "cactusinv")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func (c *client) get(path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", c.srv.URL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.http.Do(req)
}

func (c *client) login() {
	c.t.Helper()
	if code := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "albie@example.com", "password": "correct horse battery"}, nil); code != 204 {
		c.t.Fatalf("login = %d", code)
	}
}

func (c *client) ok(code, want int, what string) {
	c.t.Helper()
	if code != want {
		c.t.Fatalf("%s: status %d, want %d", what, code, want)
	}
}

type obj = map[string]any

func TestAuthAndGuards(t *testing.T) {
	c, _, _, _ := newServer(t)
	var p httpapi.Problem
	c.ok(c.do("GET", "/api/v1/dashboard", nil, &p), 401, "unauthenticated")
	if p.Type != "https://cactusinv/problems/unauthenticated" {
		t.Fatalf("problem = %+v", p)
	}
	c.ok(c.do("POST", "/api/v1/auth/login", obj{"email": "albie@example.com", "password": "wrong password!"}, &p), 401, "wrong password")

	// CSRF: a mutation without the header is refused even with a valid session.
	c.login()
	req, _ := http.NewRequestWithContext(context.Background(), "POST", c.srv.URL+"/api/v1/currencies", strings.NewReader(`{}`))
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("csrf = %d", resp.StatusCode)
	}
	var me httpapi.Me
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	if me.Email != "albie@example.com" {
		t.Fatal(me)
	}
	c.ok(c.do("GET", "/api/v1/nope", nil, &p), 404, "unknown endpoint")
	c.ok(c.do("POST", "/api/v1/auth/password", obj{"currentPassword": "nope", "newPassword": "a fresh long password"}, &p), 422, "wrong current")
	c.ok(c.do("POST", "/api/v1/auth/password", obj{"currentPassword": "correct horse battery", "newPassword": "a fresh long password"}, nil), 204, "change password")
	c.ok(c.do("POST", "/api/v1/auth/logout", nil, nil), 204, "logout")
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &p), 401, "after logout")
}

func TestLoginRateLimit(t *testing.T) {
	c, _, clock, _ := newServer(t)
	codes := map[int]int{}
	for range 12 {
		codes[c.do("POST", "/api/v1/auth/login", obj{"email": "x@example.com", "password": "nope nope nope"}, nil)]++
	}
	if codes[429] != 2 || codes[401] != 10 {
		t.Fatalf("codes = %v", codes)
	}
	clock.Advance(time.Minute)
	c.login()
}

func TestInvoicingFlowOverHTTP(t *testing.T) {
	c, store, clock, svc := newServer(t)
	c.login()

	var settings httpapi.BusinessSettings
	c.ok(c.do("GET", "/api/v1/settings/business", nil, &settings), 200, "get business")
	if settings.NumberFormat != "{code}/{dd}{mm}-{seq}/{yy}" {
		t.Fatalf("a new deployment's number format = %q", settings.NumberFormat)
	}
	settings.NumberFormat = "{code}/{seq}/{yy}" // the example invoice's format
	settings.Name = "Example Software Consulting"
	settings.TimeZone = "Africa/Johannesburg"
	settings.NextSequence = 1195
	settings.VatPercent = "0"
	c.ok(c.do("PUT", "/api/v1/settings/business", settings, &settings), 200, "put business")
	if settings.NextSequence != 1195 || settings.Version != 1 {
		t.Fatalf("settings = %+v", settings)
	}

	c.ok(c.do("POST", "/api/v1/currencies", obj{"code": "zar", "name": "South African Rand", "symbol": "R", "minorUnits": 2, "active": true}, nil), 201, "currency")
	c.ok(c.do("POST", "/api/v1/currencies", obj{"code": "JPY", "name": "Yen", "symbol": "¥", "minorUnits": 0, "active": true}, nil), 201, "currency")
	var cur httpapi.Currency
	c.ok(c.do("PUT", "/api/v1/currencies/JPY", obj{"code": "JPY", "name": "Japanese Yen", "symbol": "¥", "minorUnits": 0, "active": true}, &cur), 200, "update currency")
	if cur.MinorUnits != 0 || cur.Name != "Japanese Yen" {
		t.Fatalf("currency = %+v", cur)
	}
	var curs httpapi.CurrencyList
	c.ok(c.do("GET", "/api/v1/currencies", nil, &curs), 200, "list currencies")

	var bank httpapi.BankAccount
	c.ok(c.do("POST", "/api/v1/bank-accounts", obj{
		"currency": "ZAR", "label": "Capitec savings", "bankName": "Capitecbank", "branch": "Northgate",
		"branchCode": "470010", "accountName": "", "accountNumber": "1234567890", "accountType": "Savings",
		"swiftBic": "CABLZAJJ", "iban": "", "country": "South Africa", "isDefault": false, "active": true, "version": 0,
	}, &bank), 201, "bank")
	if !bank.IsDefault || bank.Country != "South Africa" || bank.SwiftBic != "CABLZAJJ" {
		t.Fatal("first account is default")
	}
	c.ok(c.do("GET", "/api/v1/bank-accounts/"+bank.Id, nil, &bank), 200, "get bank")
	bank.Label = "Capitec"
	c.ok(c.do("PUT", "/api/v1/bank-accounts/"+bank.Id, bank, &bank), 200, "put bank")
	var banks httpapi.BankAccountList
	c.ok(c.do("GET", "/api/v1/bank-accounts?currency=zar", nil, &banks), 200, "list banks")
	if len(banks.Items) != 1 {
		t.Fatal("banks")
	}

	var product httpapi.Product
	c.ok(c.do("POST", "/api/v1/products", obj{
		"sku": "SS-USERS", "name": "Stock system users", "description": "Stock system usage for 14 concurrent users.",
		"unit": "user", "active": true, "version": 0, "prices": []obj{{"currency": "ZAR", "unitPrice": "192.94"}, {"currency": "JPY", "unitPrice": "2000"}},
	}, &product), 201, "product")
	var p httpapi.Problem
	var integ httpapi.Product
	c.ok(c.do("POST", "/api/v1/products", obj{
		"sku": "INTEG", "name": "Integration", "description": "Alchamex integration - monthly subscription",
		"unit": "month", "active": true, "version": 0, "prices": []obj{{"currency": "ZAR", "unitPrice": "550"}},
	}, &integ), 201, "product 2")
	c.ok(c.do("POST", "/api/v1/products", obj{"sku": "NOPRICE", "name": "x", "unit": "", "description": "", "active": true, "version": 0, "prices": []obj{}}, &p), 422, "unit cost required")
	if p.Field != "prices" {
		t.Fatalf("problem = %+v", p)
	}
	c.ok(c.do("POST", "/api/v1/products", obj{"sku": "X", "name": "x", "prices": []obj{{"currency": "ZAR", "unitPrice": "1.234"}}}, &p), 422, "too many decimals")
	if p.Field != "prices.unitPrice" {
		t.Fatalf("problem = %+v", p)
	}
	c.ok(c.do("POST", "/api/v1/products", obj{"sku": "X", "name": "x", "prices": []obj{{"currency": "JPY", "unitPrice": "10.5"}}}, &p), 422, "JPY has no decimals")
	var products httpapi.ProductList
	c.ok(c.do("GET", "/api/v1/products?q=stock&currency=JPY", nil, &products), 200, "lookup")
	if products.Total != 1 || products.Items[0].Prices[0].Currency != "JPY" || products.Items[0].Prices[0].UnitPrice != "2000" {
		t.Fatalf("lookup = %+v", products)
	}

	var customer httpapi.Customer
	c.ok(c.do("POST", "/api/v1/customers", obj{
		"code": "CD", "name": "Acme Commercial Spares (Pty) Limited", "email": "juanita@acme.example",
		"cc": []string{}, "attention": "Jane", "billingAddress": "P O BOX 2000,\nGERMISTON,\n1401", "shippingName": "",
		"shippingAddress": "", "phone": "", "taxNumber": "", "currency": "ZAR", "bankAccountId": "",
		"paymentTerms": obj{"kind": "day_of_next_month", "value": 7}, "defaultNotes": "Disclaimer", "active": true, "version": 0,
	}, &customer), 201, "customer")
	if customer.PaymentTerms.Text != "Payment due before 7th of the next month" {
		t.Fatalf("terms text = %q", customer.PaymentTerms.Text)
	}
	if customer.DisclaimerSource != httpapi.CustomerDisclaimerSourceCustomer {
		t.Fatalf("disclaimer source = %q", customer.DisclaimerSource)
	}
	var customers httpapi.CustomerList
	c.ok(c.do("GET", "/api/v1/customers?q=acme&limit=10", nil, &customers), 200, "customers")

	var inv httpapi.Invoice
	c.ok(c.do("POST", "/api/v1/invoices", obj{
		"customerId": customer.Id,
		"lines": []obj{
			{"productId": product.Id, "quantity": "14"},
			{"productId": integ.Id, "quantity": "1"},
		},
	}, &inv), 201, "create invoice")
	if inv.Totals.Total != "3251.16" || inv.Lines[0].Amount != "2701.16" || inv.Lines[0].UnitPrice != "192.94" || inv.Status != "draft" || inv.Stage != "draft" ||
		inv.Lines[1].Description != "Alchamex integration - monthly subscription" {
		t.Fatalf("invoice = %+v", inv)
	}
	inv.Notes = "Updated"
	c.ok(c.do("PUT", "/api/v1/invoices/"+inv.Id, obj{"version": inv.Version, "notes": "Updated", "lines": []obj{{"productId": product.Id, "quantity": "14"}, {"productId": integ.Id, "quantity": "1", "discountPercent": "0"}}}, &inv), 200, "update invoice")
	c.ok(c.do("PUT", "/api/v1/invoices/"+inv.Id, obj{"version": 1, "lines": []obj{{"productId": integ.Id, "quantity": "1"}}}, &p), 409, "stale version")
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/issue", obj{}, &inv), 200, "issue")
	if inv.Stage != "issued" || inv.Number != "CD/1195/26" || inv.DueDate == nil || inv.DueDate.String() != "2026-09-07" || inv.Bank.AccountNumber != "1234567890" {
		t.Fatalf("issued = %+v", inv)
	}
	c.ok(c.do("DELETE", "/api/v1/invoices/"+inv.Id, nil, &p), 409, "delete issued")

	// Send with an idempotency key: the second call replays the first response.
	var m1, m2 httpapi.EmailMessage
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/send", obj{}, &m1, "Idempotency-Key", "send-1"), 202, "send")
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/send", obj{}, &m2, "Idempotency-Key", "send-1"), 202, "replay")
	if m1.Id != m2.Id || len(store.Emails()) != 1 {
		t.Fatalf("idempotency: %s vs %s, %d e-mails", m1.Id, m2.Id, len(store.Emails()))
	}
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/send", obj{"subject": "different"}, &p, "Idempotency-Key", "send-1"), 422, "key reuse with other body")
	later := clock.Now().Add(72 * time.Hour)
	var scheduled httpapi.EmailMessage
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/send", obj{"sendAt": later, "to": []string{"other@x.example"}}, &scheduled), 202, "send later")
	if !scheduled.SendAt.Equal(later) || scheduled.Status != "pending" {
		t.Fatalf("scheduled = %+v", scheduled)
	}
	var emails httpapi.EmailList
	c.ok(c.do("GET", "/api/v1/emails?status=pending", nil, &emails), 200, "outbox")
	if emails.Total != 2 {
		t.Fatalf("outbox = %d", emails.Total)
	}
	c.ok(c.do("POST", "/api/v1/emails/"+scheduled.Id+"/cancel", nil, &scheduled), 200, "cancel")
	c.ok(c.do("POST", "/api/v1/emails/"+scheduled.Id+"/retry", nil, &scheduled), 200, "retry")
	if _, err := svc.DispatchEmails(context.Background(), 10); err != nil {
		t.Fatal(err)
	}

	var detail httpapi.InvoiceDetail
	c.ok(c.do("GET", "/api/v1/invoices/"+inv.Id, nil, &detail), 200, "detail")
	if detail.Events[0].Actor != "albie@example.com" || detail.Events[0].Action != "invoice.created" {
		t.Fatalf("events carry the actor: %+v", detail.Events[0])
	}
	var audit httpapi.AuditList
	c.ok(c.do("GET", "/api/v1/audit?entityType=invoice&limit=5", nil, &audit), 200, "audit list")
	if audit.Total == 0 || audit.Items[0].Hash == "" {
		t.Fatalf("audit = %+v", audit)
	}
	var verify httpapi.AuditVerification
	c.ok(c.do("GET", "/api/v1/audit/verify", nil, &verify), 200, "audit verify")
	if !verify.Ok {
		t.Fatalf("verify = %+v", verify)
	}
	if detail.Invoice.Status != "sent" || detail.Invoice.Stage != "emailed" || len(detail.Events) == 0 || len(detail.Emails) != 2 {
		t.Fatalf("detail = %+v", detail.Invoice.Status)
	}

	// PDF
	resp, err := c.get("/api/v1/invoices/" + inv.Id + "/pdf")
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" ||
		resp.Header.Get("Content-Disposition") != `inline; filename="Inv_no_CD_1195_26.pdf"` || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf: %d %v", resp.StatusCode, resp.Header)
	}

	var list httpapi.InvoiceList
	c.ok(c.do("GET", "/api/v1/invoices?status=sent&q=1195", nil, &list), 200, "list invoices")
	if list.Total != 1 || len(list.Items[0].Lines) != 0 {
		t.Fatalf("list = %+v", list)
	}
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/mark-overdue", nil, &inv), 200, "overdue")
	if inv.Status != "overdue" || inv.Stage != "overdue" || inv.OverdueAt == nil {
		t.Fatalf("overdue = %+v", inv)
	}
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/mark-paid", nil, &inv), 200, "paid")
	if inv.Stage != "paid" {
		t.Fatal("stage paid")
	}
	c.ok(c.do("POST", "/api/v1/invoices/"+inv.Id+"/void", obj{"reason": "x"}, &p), 409, "void paid")

	// A second invoice, voided.
	var inv2 httpapi.Invoice
	c.ok(c.do("POST", "/api/v1/invoices", obj{"customerId": customer.Id, "lines": []obj{{"productId": integ.Id, "quantity": "1"}}}, &inv2), 201, "inv2")
	c.ok(c.do("POST", "/api/v1/invoices/"+inv2.Id+"/issue", obj{"issueDate": "2026-08-22"}, &inv2), 200, "issue2")
	c.ok(c.do("POST", "/api/v1/invoices/"+inv2.Id+"/void", obj{"reason": "duplicate"}, &inv2), 200, "void")
	var inv3 httpapi.Invoice
	c.ok(c.do("POST", "/api/v1/invoices", obj{"customerId": customer.Id, "lines": []obj{{"productId": integ.Id, "quantity": "1"}}}, &inv3), 201, "inv3")
	c.ok(c.do("DELETE", "/api/v1/invoices/"+inv3.Id, nil, nil), 204, "delete draft")
	c.ok(c.do("POST", "/api/v1/invoices", obj{"lines": []obj{}}, &p), 422, "no customer")
	c.ok(c.do("POST", "/api/v1/invoices", obj{"customerId": customer.Id, "lines": []obj{{"productId": integ.Id, "quantity": "abc"}}}, &p), 422, "bad quantity")
	c.ok(c.do("POST", "/api/v1/invoices", obj{"customerId": customer.Id, "vatPercent": "200", "lines": []obj{{"productId": integ.Id, "quantity": "1"}}}, &p), 422, "bad tax")
	c.ok(c.do("POST", "/api/v1/invoices", obj{"customerId": customer.Id, "lines": []obj{{"productId": "", "quantity": "1"}}}, &p), 422, "free text rejected")

	// Schedules.
	var sched httpapi.ScheduleDetail
	c.ok(c.do("POST", "/api/v1/schedules", obj{
		"customerId": customer.Id, "name": "Acme monthly", "autoSend": true, "notes": "",
		"emailSubject": "", "emailBody": "", "version": 0, "header": obj{},
		"recurrence": obj{"frequency": "monthly", "interval": 1, "dayOfMonth": 21, "weekday": 0, "sendTime": "09:00", "startDate": "2026-09-01"},
		"lines":      []obj{{"productId": product.Id, "quantity": "14"}, {"productId": integ.Id, "quantity": "1", "unitPrice": "550"}},
	}, &sched), 201, "schedule")
	if len(sched.Upcoming) != 5 || sched.Schedule.Lines[1].UnitPrice == nil || *sched.Schedule.Lines[1].UnitPrice != "550.00" || sched.Schedule.Recurrence.SendTime != "09:00" {
		t.Fatalf("schedule = %+v", sched)
	}
	id := sched.Schedule.Id
	c.ok(c.do("POST", "/api/v1/schedules/"+id+"/pause", nil, &sched), 200, "pause")
	c.ok(c.do("POST", "/api/v1/schedules/"+id+"/resume", nil, &sched), 200, "resume")
	c.ok(c.do("POST", "/api/v1/schedules/"+id+"/run-now", nil, &sched), 200, "run now")
	s := sched.Schedule
	s.Name = "Renamed"
	c.ok(c.do("PUT", "/api/v1/schedules/"+id, s, &sched), 200, "update schedule")
	c.ok(c.do("GET", "/api/v1/schedules/"+id, nil, &sched), 200, "get schedule")
	if sched.Schedule.Name != "Renamed" {
		t.Fatal("rename")
	}
	var scheds httpapi.ScheduleList
	c.ok(c.do("GET", "/api/v1/schedules?status=active", nil, &scheds), 200, "list schedules")
	var preview httpapi.RunPreview
	c.ok(c.do("POST", "/api/v1/schedules/preview", obj{"frequency": "weekly", "interval": 1, "dayOfMonth": 0, "weekday": 5, "sendTime": "08:30", "startDate": "2026-09-01"}, &preview), 200, "preview")
	if len(preview.Runs) != 6 || preview.Runs[0].Weekday() != time.Friday {
		t.Fatalf("preview = %v", preview.Runs)
	}
	c.ok(c.do("POST", "/api/v1/schedules/preview", obj{"frequency": "weekly", "interval": 1, "dayOfMonth": 0, "weekday": 5, "sendTime": "25:00", "startDate": "2026-09-01"}, &p), 422, "bad time")

	var dash httpapi.Dashboard
	c.ok(c.do("GET", "/api/v1/dashboard", nil, &dash), 200, "dashboard")
	if len(dash.UpcomingRuns) != 1 || len(dash.RecentInvoices) == 0 {
		t.Fatalf("dashboard = %+v", dash)
	}
	c.ok(c.do("DELETE", "/api/v1/schedules/"+id, nil, nil), 204, "delete schedule")

	// Deletes and conflicts.
	c.ok(c.do("DELETE", "/api/v1/customers/"+customer.Id, nil, &p), 409, "customer in use")
	c.ok(c.do("GET", "/api/v1/customers/"+customer.Id, nil, &customer), 200, "get customer")
	customer.Active = false
	customer.DefaultNotes = "  "
	c.ok(c.do("PUT", "/api/v1/customers/"+customer.Id, customer, &customer), 200, "deactivate")
	if customer.DisclaimerSource != httpapi.CustomerDisclaimerSourceBusiness {
		t.Fatalf("a customer without a disclaimer uses the business one: %q", customer.DisclaimerSource)
	}
	c.ok(c.do("GET", "/api/v1/products/"+product.Id, nil, &product), 200, "get product")
	c.ok(c.do("PUT", "/api/v1/products/"+product.Id, product, &product), 200, "put product")
	c.ok(c.do("DELETE", "/api/v1/products/"+product.Id, nil, nil), 204, "delete product")
	c.ok(c.do("GET", "/api/v1/products/"+product.Id, nil, &p), 404, "gone")
	c.ok(c.do("DELETE", "/api/v1/bank-accounts/"+bank.Id, nil, nil), 204, "delete bank")
	c.ok(c.do("POST", "/api/v1/currencies", obj{"code": "ZAR", "name": "dup", "symbol": "R", "minorUnits": 2, "active": true}, &p), 409, "dup currency")
	c.ok(c.do("PUT", "/api/v1/currencies/ZZ1", obj{"code": "ZZ1", "name": "x", "symbol": "", "minorUnits": 2, "active": true}, &p), 422, "bad code")
	c.ok(c.do("POST", "/api/v1/currencies", "not json", &p), 400, "bad body")
}

func TestOpsEndpointsAndSPA(t *testing.T) {
	c, _, _, _ := newServer(t)
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 200, "/metrics": 200, "/": 200, "/invoices/123": 200, "/assets/app.js": 200} {
		resp, err := c.get(path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s = %d", path, resp.StatusCode)
		}
		if path == "/invoices/123" && !strings.Contains(string(body), "cactusInv") {
			t.Error("SPA fallback serves index.html")
		}
		if path == "/assets/app.js" && !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
			t.Error("assets are immutable")
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing security headers", path)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", c.srv.URL+"/", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("POST / = %d", resp.StatusCode)
	}
}

func TestReadyzUnavailable(t *testing.T) {
	store := fakes.NewStore()
	svc := app.New(app.Deps{Tx: store, Clock: &fakes.Clock{}, IDs: &fakes.IDs{}})
	h := httpapi.NewHandler(httpapi.Options{Service: svc, Clock: &fakes.Clock{}, Ready: map[string]httpapi.ReadyCheck{"db": func(context.Context) error { return errors.New("down") }}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "unavailable") {
		t.Fatalf("readyz = %d %s", rec.Code, rec.Body)
	}
}

func TestVersionSettingsAndStatements(t *testing.T) {
	c, _, _, svc := newServer(t)
	var v httpapi.Version
	c.ok(c.do("GET", "/api/v1/version", nil, &v), 200, "version is public")
	c.login()
	var es httpapi.EmailSettings
	c.ok(c.do("GET", "/api/v1/settings/email", nil, &es), 200, "email settings")
	if es.Host != "smtp.gmail.com" || es.Port != 587 || es.Enabled {
		t.Fatalf("Gmail preset: %+v", es)
	}
	var p httpapi.Problem
	es.Enabled, es.Username = true, "me@gmail.com"
	c.ok(c.do("PUT", "/api/v1/settings/email", es, &p), 422, "password needed (and no cipher in this test)")
	es.Enabled, es.Username = false, ""
	c.ok(c.do("PUT", "/api/v1/settings/email", es, &es), 200, "save disabled")
	c.ok(c.do("POST", "/api/v1/settings/email/test", obj{"to": "me@example.com"}, nil), 204, "test e-mail")
	c.ok(c.do("POST", "/api/v1/settings/email/test", obj{"to": "nope"}, &p), 422, "bad recipient")

	ctx := context.Background()
	_, _ = svc.CreateCurrency(ctx, "ZAR", "Rand", "R", 2, true)
	_, _ = svc.CreateBankAccount(ctx, domainBank())
	cust, err := svc.CreateCustomer(ctx, domainCustomer())
	if err != nil {
		t.Fatal(err)
	}
	var st httpapi.Statement
	c.ok(c.do("GET", "/api/v1/customers/"+cust.ID+"/statement", nil, &st), 200, "statement")
	if st.Total != "0.00" || len(st.Lines) != 0 {
		t.Fatalf("empty statement: %+v", st)
	}
	resp, err := c.get("/api/v1/customers/" + cust.ID + "/statement/pdf")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), `inline; filename="Statement_CD_`) {
		t.Fatalf("statement pdf: %d %v", resp.StatusCode, resp.Header)
	}
	var m httpapi.EmailMessage
	c.ok(c.do("POST", "/api/v1/customers/"+cust.ID+"/statement/send", obj{}, &m), 202, "send statement")
	if m.Kind != "statement" || m.CustomerId != cust.ID || m.InvoiceId != "" {
		t.Fatalf("statement e-mail: %+v", m)
	}
	var sl httpapi.StatementList
	c.ok(c.do("GET", "/api/v1/statements", nil, &sl), 200, "statements")
	if len(sl.Items) != 1 || sl.Items[0].CustomerId != cust.ID || sl.Items[0].LastEmail == nil || sl.Items[0].LastEmail.Status != "pending" {
		t.Fatalf("statements: %+v", sl)
	}
	var bulk httpapi.SendStatements202JSONResponse
	c.ok(c.do("POST", "/api/v1/statements/send", obj{"customerIds": []string{cust.ID, "nope"}}, &bulk), 202, "bulk send")
	if len(bulk.Queued) != 0 || len(bulk.Skipped) != 2 || bulk.Skipped[0].Reason != "nothing owed" || bulk.Skipped[1].Reason != "customer not found" {
		t.Fatalf("bulk: %+v", bulk)
	}
	c.ok(c.do("POST", "/api/v1/statements/send", obj{"customerIds": []string{cust.ID}, "includeZero": true}, &bulk), 202, "bulk send zero")
	if len(bulk.Queued) != 1 {
		t.Fatalf("bulk zero: %+v", bulk)
	}
	c.ok(c.do("POST", "/api/v1/statements/send", obj{"customerIds": []string{}}, nil), 422, "empty bulk")
	var sd httpapi.ScheduleDetail
	c.ok(c.do("POST", "/api/v1/schedules", obj{
		"customerId": cust.ID, "name": "Monthly statement", "kind": "email", "emailScope": "statement",
		"autoSend": true, "notes": "", "emailSubject": "", "emailBody": "", "version": 0, "header": obj{}, "lines": []obj{},
		"recurrence": obj{"frequency": "monthly", "interval": 1, "dayOfMonth": 1, "weekday": 0, "sendTime": "08:00", "startDate": "2026-09-01"},
	}, &sd), 201, "email schedule")
	if sd.Schedule.Kind != "email" || sd.Schedule.EmailScope != "statement" {
		t.Fatalf("schedule kind: %+v", sd.Schedule)
	}
}

func domainBank() domain.BankAccount {
	return domain.BankAccount{Currency: "ZAR", Label: "Main", BankName: "Example Bank", AccountNumber: "1", Active: true}
}

func domainCustomer() domain.Customer {
	return domain.Customer{Code: "CD", Name: "Acme", Email: "a@acme.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}
}
