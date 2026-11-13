package app_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

var linkRe = regexp.MustCompile(`/q/([A-Za-z0-9_-]{43})\?answer=accept`)

// tokenIn is the response token in a quote e-mail's text.
func tokenIn(t *testing.T, body string) string {
	t.Helper()
	m := linkRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no Accept link in %q", body)
	}
	return m[1]
}

// The customer answers a quote through the links in its e-mail (M41).
func TestCustomerAnswersThroughTheEmailLinks(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "https://inv.example.com/"}))
	token := tokenIn(t, m.Body)
	if !strings.Contains(m.Body, "https://inv.example.com/q/"+token+"?answer=decline") {
		t.Fatalf("no Decline link: %q", m.Body)
	}
	must(h.svc.DispatchEmails(h.ctx, 5))
	sent := h.mailer.Sent[len(h.mailer.Sent)-1]
	if !strings.Contains(sent.HTMLBody, ">Accept quote<") || !strings.Contains(sent.HTMLBody, `href="https://inv.example.com/q/`+token+`?answer=decline"`) || sent.Body != m.Body {
		t.Fatalf("HTML = %q", sent.HTMLBody)
	}

	// Opening the page changes nothing; it needs no sign-in.
	v := must(h.svc.GetQuoteResponse(h.ctx, token))
	if v.Quote.ID != q.ID || v.Quote.Status != domain.QuoteSent || v.Expired || v.Business.Name == "" {
		t.Fatalf("view = %+v", v)
	}
	for _, bad := range []string{"", "short", strings.Repeat("A", 43), token[:42] + "?"} {
		if _, err := h.svc.GetQuoteResponse(h.ctx, bad); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("token %q: %v", bad, err)
		}
	}

	v = must(h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerAccept, ""))
	if v.Quote.Status != domain.QuoteAccepted || v.Quote.AnsweredBy != domain.AnsweredByCustomer {
		t.Fatalf("after accepting: %+v", v.Quote)
	}
	events := must(h.svc.GetQuote(h.ctx, q.ID)).Events
	if last := events[len(events)-1]; last.Action != "quote.accepted" || last.ActorName != "Customer (e-mail link)" || !strings.Contains(last.Summary, "by the customer") {
		t.Fatalf("audit = %+v", last)
	}
	must(h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerAccept, "")) // the same again: no change
	if _, err := h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerDecline, "changed my mind"); err == nil || !strings.Contains(err.Error(), "already accepted") {
		t.Fatalf("declined after accepting: %v", err)
	}
	if pdf, name := must2(h.svc.QuoteResponsePDF(h.ctx, token)); len(pdf) == 0 || !strings.HasPrefix(name, "Quote_") {
		t.Fatal("no PDF for the customer")
	}

	// The filter shows the quotes customers answered.
	other := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	must(h.svc.DeclineQuote(h.ctx, other.ID, "no"))
	if list, n := must3(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{AnsweredBy: domain.AnsweredByCustomer})); n != 1 || list[0].ID != q.ID {
		t.Fatalf("answered by customers = %d", n)
	}
	if list, n := must3(h.svc.ListQuotes(h.ctx, ports.QuoteFilter{AnsweredBy: domain.AnsweredByStaff})); n != 1 || list[0].ID != other.ID {
		t.Fatalf("answered by staff = %d", n)
	}
}

func TestCustomerDeclinesWithAReason(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	token := tokenIn(t, must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "http://localhost:8080"})).Body)
	v := must(h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerDecline, " too dear "))
	if v.Quote.Status != domain.QuoteDeclined || v.Quote.DeclineReason != "too dear" || v.Quote.AnsweredBy != domain.AnsweredByCustomer {
		t.Fatalf("declined = %+v", v.Quote)
	}
	if _, err := h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerAccept, ""); err == nil {
		t.Fatal("accepted after declining")
	}
}

// Settings → Quotes' public address wins over the browser's; without either, no links.
func TestQuoteLinkAddress(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	if m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{})); strings.Contains(m.Body, "/q/") {
		t.Fatalf("links without an address: %q", m.Body)
	}
	if _, err := h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "javascript:alert(1)"}); err == nil {
		t.Fatal("a bad link address was used")
	}
	qs := domain.DefaultQuoteSettings()
	qs.PublicURL = "https://public.example.com"
	must(h.svc.UpdateQuoteSettings(h.ctx, qs, 0))
	m := must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "http://192.168.1.5:8080"}))
	if !strings.Contains(m.Body, "https://public.example.com/q/") || strings.Contains(m.Body, "192.168") {
		t.Fatalf("body = %q", m.Body)
	}
	// Each e-mail has its own token, and the earlier one keeps working.
	first := tokenIn(t, m.Body)
	second := tokenIn(t, must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{})).Body)
	if first == second || must(h.svc.GetQuoteResponse(h.ctx, first)).Quote.ID != q.ID {
		t.Fatal("tokens")
	}
	must(h.svc.DispatchEmails(h.ctx, 10))
	for _, s := range h.mailer.Sent {
		if strings.Contains(s.Body, "/q/") != (s.HTMLBody != "") {
			t.Fatalf("HTML only with links: %q / %q", s.Body, s.HTMLBody)
		}
	}
}

// An expired quote cannot be accepted, converted, sent or answered; it is renewed (M41).
func TestExpiredQuoteIsRenewed(t *testing.T) {
	h := newHarness(t).seed()
	q := must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	token := tokenIn(t, must(h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "https://inv.example.com"})).Body)
	if _, err := h.svc.RenewQuote(h.ctx, q.ID); err == nil {
		t.Fatal("a quote still open was renewed")
	}
	day := q.ValidUntil.AddDays(1)
	h.clock.Set(time.Date(day.Year, day.Month, day.Day, 10, 0, 0, 0, time.UTC))

	if _, err := h.svc.AcceptQuote(h.ctx, q.ID); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("accept: %v", err)
	}
	if _, err := h.svc.ConvertQuote(h.ctx, q.ID); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("convert: %v", err)
	}
	if _, err := h.svc.SendQuote(h.ctx, q.ID, app.SendInput{}); err == nil || !strings.Contains(err.Error(), "renew") {
		t.Fatalf("send: %v", err)
	}
	if v := must(h.svc.GetQuoteResponse(h.ctx, token)); !v.Expired || v.Stage != domain.QuoteStageExpired {
		t.Fatalf("customer page: %+v", v)
	}
	if _, err := h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerAccept, ""); err == nil {
		t.Fatal("the customer accepted an expired quote")
	}

	n := must(h.svc.RenewQuote(h.ctx, q.ID))
	if n.ID == q.ID || n.Number == q.Number || n.RenewedFrom != q.ID || n.Status != domain.QuoteDraft || n.CustomerID != q.CustomerID ||
		n.IssueDate != day || n.ValidUntil != day.AddDays(domain.DefaultQuoteValidDays) || n.Totals != q.Totals || len(n.Lines) != len(q.Lines) {
		t.Fatalf("renewal = %+v", n)
	}
	if ev := must(h.svc.GetQuote(h.ctx, q.ID)).Events; ev[len(ev)-1].Action != "quote.renewed" || !strings.Contains(ev[len(ev)-1].Summary, n.Number) {
		t.Fatalf("old quote's events = %+v", ev)
	}
	// The renewal is e-mailed and answered as any quote.
	nt := tokenIn(t, must(h.svc.SendQuote(h.ctx, n.ID, app.SendInput{LinkBase: "https://inv.example.com"})).Body)
	if v := must(h.svc.RespondToQuote(h.ctx, nt, domain.QuoteAnswerAccept, "")); v.Quote.Status != domain.QuoteAccepted {
		t.Fatalf("renewal answered: %+v", v.Quote)
	}
	must(h.svc.ConvertQuote(h.ctx, n.ID))
}
