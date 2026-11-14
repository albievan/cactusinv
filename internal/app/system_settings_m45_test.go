package app_test

import (
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// Links in e-mails point at System Settings' external address first, then an address an
// older version kept under Settings → Quotes, then the sending browser's (M45).
func TestExternalAddressForLinks(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	send := func() string {
		return must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "http://10.101.18.101:30080"})).Body
	}
	if b := send(); !strings.Contains(b, "http://10.101.18.101:30080/q/") {
		t.Fatalf("browser's address: %q", b)
	}
	qs := domain.DefaultQuoteSettings()
	qs.PublicURL = "https://old.example.com"
	must(h.svc.UpdateQuoteSettings(h.ctx, qs, 0))
	if b := send(); !strings.Contains(b, "https://old.example.com/q/") {
		t.Fatalf("quote settings' address: %q", b)
	}
	st := must(h.svc.GetSystemSettings(h.ctx)).Settings
	st.ExternalURL, st.HSTS = "https://invoice.primesoft.co.za/", true
	saved := must(h.svc.UpdateSystemSettings(h.ctx, st)).Settings
	if saved.ExternalURL != "https://invoice.primesoft.co.za" || !h.svc.HSTSEnabled(h.ctx) {
		t.Fatalf("saved = %+v", saved)
	}
	if b := send(); !strings.Contains(b, "https://invoice.primesoft.co.za/q/") {
		t.Fatalf("external address: %q", b)
	}
	// Invoices and password resets use it too.
	inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
	if b := must(h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{LinkBase: "http://10.0.0.1"})).Body; !strings.Contains(b, "https://invoice.primesoft.co.za/i/") {
		t.Fatalf("invoice link: %q", b)
	}
	if e := h.audited("settings.system_updated"); !strings.Contains(e.Summary, "external address https://invoice.primesoft.co.za; HTTPS only (HSTS) on") {
		t.Fatalf("audit = %q", e.Summary)
	}
}
