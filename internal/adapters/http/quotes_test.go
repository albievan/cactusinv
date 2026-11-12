package httpapi_test

import (
	"context"
	"io"
	"strings"
	"testing"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

func TestQuotesOverHTTP(t *testing.T) {
	c, _, _, svc := newServer(t)
	c.login()
	ctx := context.Background()
	if _, err := svc.CreateCurrency(ctx, "ZAR", "Rand", "R", 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBankAccount(ctx, domainBank()); err != nil {
		t.Fatal(err)
	}
	prod, err := svc.CreateProduct(ctx, domain.Product{SKU: "WEB", Name: "Website", Description: "Website build", Prices: map[domain.CurrencyCode]int64{"ZAR": 1500000}, Active: true})
	if err != nil {
		t.Fatal(err)
	}

	// Settings → Quotes.
	var qs httpapi.QuoteSettings
	c.ok(c.do("GET", "/api/v1/settings/quotes", nil, &qs), 200, "get quote settings")
	if qs.ValidDays != 30 || qs.NextSequence != 1 || qs.Version != 0 {
		t.Fatalf("defaults = %+v", qs)
	}
	qs.ValidDays, qs.Terms, qs.NumberFormat = 14, "50% deposit.", "QU{yy}-{seq}"
	c.ok(c.do("PUT", "/api/v1/settings/quotes", qs, &qs), 200, "put quote settings")
	var p httpapi.Problem
	c.ok(c.do("PUT", "/api/v1/settings/quotes", httpapi.QuoteSettings{ValidDays: 0, Version: qs.Version}, &p), 422, "0 days")
	if p.Field != "validDays" {
		t.Fatalf("problem = %+v", p)
	}

	// An ad-hoc customer and a quote for them.
	var adhoc httpapi.AdhocCustomer
	c.ok(c.do("POST", "/api/v1/adhoc-customers", obj{"name": "Prospect Ltd", "email": "", "attention": "Thandi", "address": "1 Main Rd", "phone": "", "taxNumber": "", "currency": "zar", "version": 0}, &adhoc), 201, "ad-hoc")
	if adhoc.Currency != "ZAR" || adhoc.Id == "" {
		t.Fatalf("ad-hoc = %+v", adhoc)
	}
	var q httpapi.Quote
	c.ok(c.do("POST", "/api/v1/quotes", obj{"adhocCustomerId": adhoc.Id, "reference": "Site", "lines": []obj{{"productId": prod.ID, "quantity": "1", "discountPercent": "10"}}}, &q), 201, "quote")
	if q.Number != "QU26-1" || q.Stage != "draft" || q.Totals.Total != "13500.00" || q.Terms != "50% deposit." || q.ValidUntil.String() != "2026-09-04" || q.Fingerprint == "" {
		t.Fatalf("quote = %+v", q)
	}
	c.ok(c.do("POST", "/api/v1/quotes", obj{"lines": []obj{{"productId": prod.ID, "quantity": "1"}}}, &p), 422, "nobody")
	c.ok(c.do("POST", "/api/v1/quotes", obj{"customerId": "nope", "lines": []obj{{"productId": prod.ID, "quantity": "1"}}}, &p), 422, "unknown customer")

	// Revise: a price override in the quote's currency.
	c.ok(c.do("PUT", "/api/v1/quotes/"+q.Id, obj{"version": q.Version, "reference": "Site", "lines": []obj{{"productId": prod.ID, "quantity": "2", "unitPrice": "12000"}}}, &q), 200, "update quote")
	if q.Totals.Total != "24000.00" || q.Number != "QU26-1" {
		t.Fatalf("revised = %+v", q)
	}
	c.ok(c.do("PUT", "/api/v1/quotes/"+q.Id, obj{"version": 1, "lines": []obj{{"productId": prod.ID, "quantity": "2"}}}, &p), 409, "stale")

	// E-mail it (no address yet: the request names one), and the PDF.
	var m httpapi.EmailMessage
	c.ok(c.do("POST", "/api/v1/quotes/"+q.Id+"/send", obj{}, &p), 422, "no address")
	c.ok(c.do("POST", "/api/v1/quotes/"+q.Id+"/send", obj{"to": []string{"thandi@prospect.example"}}, &m, "Idempotency-Key", "q-send-1"), 202, "send")
	if m.Kind != "quote" || m.QuoteId != q.Id || !strings.HasPrefix(m.Subject, "Quotation QU26-1") {
		t.Fatalf("message = %+v", m)
	}
	resp, err := c.get("/api/v1/quotes/" + q.Id + "/pdf")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" || !strings.Contains(resp.Header.Get("Content-Disposition"), "Quote_QU26-1.pdf") || string(body) != "%PDF-quote QU26-1" {
		t.Fatalf("pdf = %d %v %q", resp.StatusCode, resp.Header, body)
	}

	var d httpapi.QuoteDetail
	c.ok(c.do("GET", "/api/v1/quotes/"+q.Id, nil, &d), 200, "detail")
	if d.Quote.Stage != "sent" || len(d.Emails) != 1 || d.AdhocCustomer == nil || d.AdhocCustomer.Name != "Prospect Ltd" || len(d.Events) < 3 {
		t.Fatalf("detail = %+v", d)
	}

	// Accept; converting needs a customer first.
	c.ok(c.do("POST", "/api/v1/quotes/"+q.Id+"/accept", nil, &q), 200, "accept")
	c.ok(c.do("POST", "/api/v1/quotes/"+q.Id+"/convert", nil, &p), 422, "convert for an ad-hoc customer")
	var cust httpapi.Customer
	c.ok(c.do("POST", "/api/v1/adhoc-customers/"+adhoc.Id+"/convert", obj{"code": "PR", "email": "thandi@prospect.example", "paymentTerms": obj{"kind": "days_after", "value": 30}}, &cust), 201, "make customer")
	if cust.Code != "PR" || cust.Name != "Prospect Ltd" || cust.PaymentTerms.Value != 30 {
		t.Fatalf("customer = %+v", cust)
	}
	var inv httpapi.Invoice
	c.ok(c.do("POST", "/api/v1/quotes/"+q.Id+"/convert", nil, &inv), 201, "convert")
	if inv.Status != "draft" || inv.CustomerId != cust.Id || inv.Totals.Total != "24000.00" || inv.Header.Job != "Site" {
		t.Fatalf("invoice = %+v", inv)
	}

	var list httpapi.QuoteList
	c.ok(c.do("GET", "/api/v1/quotes?status=converted&q=QU26", nil, &list), 200, "list")
	if list.Total != 1 || list.Items[0].InvoiceId != inv.Id || list.Items[0].CustomerId != cust.Id {
		t.Fatalf("list = %+v", list)
	}
	var adhocs httpapi.AdhocCustomerList
	c.ok(c.do("GET", "/api/v1/adhoc-customers", nil, &adhocs), 200, "ad-hoc list")
	if adhocs.Total != 0 {
		t.Fatalf("a converted ad-hoc customer is listed: %+v", adhocs)
	}
	c.ok(c.do("GET", "/api/v1/adhoc-customers?includeConverted=true", nil, &adhocs), 200, "ad-hoc list all")
	if adhocs.Total != 1 || adhocs.Items[0].CustomerId != cust.Id {
		t.Fatalf("all = %+v", adhocs)
	}

	// A declined quote, and a deleted draft.
	var q2 httpapi.Quote
	c.ok(c.do("POST", "/api/v1/quotes", obj{"customerId": cust.Id, "lines": []obj{{"productId": prod.ID, "quantity": "1"}}}, &q2), 201, "quote 2")
	c.ok(c.do("POST", "/api/v1/quotes/"+q2.Id+"/decline", obj{"reason": "went elsewhere"}, &q2), 200, "decline")
	if q2.Status != "declined" || q2.DeclineReason != "went elsewhere" {
		t.Fatalf("declined = %+v", q2)
	}
	c.ok(c.do("DELETE", "/api/v1/quotes/"+q2.Id, nil, &p), 409, "delete declined")
	var q3 httpapi.Quote
	c.ok(c.do("POST", "/api/v1/quotes", obj{"customerId": cust.Id, "lines": []obj{{"productId": prod.ID, "quantity": "1"}}}, &q3), 201, "quote 3")
	c.ok(c.do("DELETE", "/api/v1/quotes/"+q3.Id, nil, nil), 204, "delete draft")

	var ad2 httpapi.AdhocCustomer
	c.ok(c.do("POST", "/api/v1/adhoc-customers", obj{"name": "Walk-in", "currency": "ZAR"}, &ad2), 201, "ad-hoc 2")
	ad2.Phone = "012"
	c.ok(c.do("PUT", "/api/v1/adhoc-customers/"+ad2.Id, ad2, &ad2), 200, "update ad-hoc")
	c.ok(c.do("GET", "/api/v1/adhoc-customers/"+ad2.Id, nil, &ad2), 200, "get ad-hoc")
	if ad2.Phone != "012" || ad2.Version != 2 {
		t.Fatalf("ad-hoc 2 = %+v", ad2)
	}
	c.ok(c.do("DELETE", "/api/v1/adhoc-customers/"+ad2.Id, nil, nil), 204, "delete ad-hoc")
}

func TestQuotePermissions(t *testing.T) {
	for op, want := range map[string]bool{
		"listquotes": true, "getquotepdf": true, "createquote": false, "convertquote": false, "convertadhoccustomer": false, "getquotesettings": false,
	} {
		access := domain.AccessFor([]domain.Group{{ID: "g", Name: "Viewers", Permissions: []domain.Permission{domain.PermQuotesView}}})
		err := app.SessionAllows(domain.User{Access: access}, op)
		if (err == nil) != want {
			t.Errorf("quotes.view on %s: %v", op, err)
		}
	}
	sales := domain.AccessFor([]domain.Group{{ID: "g", Name: "Sales", Permissions: []domain.Permission{domain.PermQuotesCreate}}})
	if app.SessionAllows(domain.User{Access: sales}, "getquotesettings") != nil || app.SessionAllows(domain.User{Access: sales}, "updatequotesettings") == nil {
		t.Fatal("a quote writer reads the quote defaults but does not change them")
	}
}
