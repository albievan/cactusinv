package domain

import (
	"html"
	"regexp"
	"strings"
)

// QuoteAnswer is a customer's answer to a quote through its e-mail link (M41).
type QuoteAnswer string

// The answers a link records.
const (
	QuoteAnswerAccept  QuoteAnswer = "accept"
	QuoteAnswerDecline QuoteAnswer = "decline"
)

// Lines a quote e-mail carries its links on (plain text); QuoteEmailHTML turns them into
// buttons.
const (
	quoteAcceptLine  = "To accept this quote, open: "
	quoteDeclineLine = "To decline it, open: "
)

// QuoteResponsePath is the web app's page for a response token ("/q/<token>").
func QuoteResponsePath(token string) string { return "/q/" + token }

// QuoteResponseLinks are a quote e-mail's Accept and Decline links: base is the address
// customers reach cactusInv on (ParsePublicURL), token the e-mail's random token.
type QuoteResponseLinks struct{ Accept, Decline string }

// NewQuoteResponseLinks builds the links for token under base.
func NewQuoteResponseLinks(base, token string) QuoteResponseLinks {
	u := strings.TrimRight(base, "/") + QuoteResponsePath(token)
	return QuoteResponseLinks{Accept: u + "?answer=" + string(QuoteAnswerAccept), Decline: u + "?answer=" + string(QuoteAnswerDecline)}
}

// AppendTo adds the links to a quote e-mail's text, after a blank line.
func (l QuoteResponseLinks) AppendTo(body string) string {
	return strings.TrimRight(body, "\r\n ") + "\n\n" + quoteAcceptLine + l.Accept + "\n" + quoteDeclineLine + l.Decline + "\n"
}

// quoteLinkRe is a link this application wrote: http(s), a /q/ token of 43 base64url
// characters, and the answer. Anything else on such a line stays text.
var quoteLinkRe = regexp.MustCompile(`^https?://[^\s"'<>]+/q/[A-Za-z0-9_-]{43}\?answer=(accept|decline)$`)

// emailButton is a link line of an e-mail's text that the HTML version shows as a button.
type emailButton struct {
	prefix, label, bg, fg, border string
	ok                            func(url string) bool
}

// responseEmailHTML is the HTML version of an e-mail whose text carries the buttons'
// link lines: the rest of the text as paragraphs, then the buttons in order, then the
// footer. It returns "" unless every button's line is present and valid.
func responseEmailHTML(body, footer string, buttons []emailButton) string {
	urls := make([]string, len(buttons))
	var kept []string
lines:
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		for i, btn := range buttons {
			if u, ok := strings.CutPrefix(line, btn.prefix); ok && btn.ok(u) {
				urls[i] = u
				continue lines
			}
		}
		kept = append(kept, line)
	}
	for _, u := range urls {
		if u == "" {
			return ""
		}
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="margin:0;padding:24px;background:#f6f3ec;font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:1.5;color:#0f172a">`)
	b.WriteString(`<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e4dbc8;border-radius:12px;padding:24px">`)
	for _, p := range strings.Split(strings.TrimSpace(strings.Join(kept, "\n")), "\n\n") {
		b.WriteString(`<p style="margin:0 0 14px">` + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(p)), "\n", "<br>") + `</p>`)
	}
	b.WriteString(`<table role="presentation" cellspacing="0" cellpadding="0" style="margin:22px 0 8px"><tr>`)
	for i, btn := range buttons {
		if i > 0 {
			b.WriteString(`<td style="width:12px"></td>`)
		}
		b.WriteString(button(urls[i], btn.label, btn.bg, btn.fg, btn.border))
	}
	b.WriteString(`</tr></table>`)
	b.WriteString(`<p style="margin:14px 0 0;font-size:12px;color:#64748b">` + html.EscapeString(footer) + `</p>`)
	b.WriteString(`</div></body></html>`)
	return b.String()
}

// QuoteEmailHTML is the HTML version of a quote e-mail whose text carries response links
// (AppendTo): the text as paragraphs, and the links as an Accept and a Decline button. It
// returns "" when the text has no such links (the e-mail then goes as plain text only).
func QuoteEmailHTML(body, businessName string) string {
	isLink := func(answer string) func(string) bool {
		return func(u string) bool { return quoteLinkRe.MatchString(u) && strings.HasSuffix(u, "="+answer) }
	}
	return responseEmailHTML(body, "Each button opens a page where you confirm your answer to "+businessName+". The quotation is attached as a PDF.", []emailButton{
		{prefix: quoteAcceptLine, label: "Accept quote", bg: "#2f5d46", fg: "#ffffff", border: "#2f5d46", ok: isLink("accept")},
		{prefix: quoteDeclineLine, label: "Decline quote", bg: "#ffffff", fg: "#9b2c2c", border: "#9b2c2c", ok: isLink("decline")},
	})
}

func button(href, label, bg, fg, border string) string {
	return `<td style="border-radius:8px;background:` + bg + `"><a href="` + html.EscapeString(href) + `" style="display:inline-block;padding:12px 22px;border:1px solid ` +
		border + `;border-radius:8px;color:` + fg + `;background:` + bg + `;font-weight:bold;text-decoration:none">` + html.EscapeString(label) + `</a></td>`
}

// ---- invoices: "I have paid" (M42) ----

// invoicePaidLine is the line an invoice e-mail carries its "I have paid" link on.
const invoicePaidLine = "If you have paid this invoice, let us know: "

// InvoiceResponsePath is the web app's page for an invoice e-mail's token ("/i/<token>").
func InvoiceResponsePath(token string) string { return "/i/" + token }

var invoiceLinkRe = regexp.MustCompile(`^https?://[^\s"'<>]+/i/[A-Za-z0-9_-]{43}$`)

// InvoicePaidLink is an invoice e-mail's "I have paid" link: base is the address customers
// reach cactusInv on (ParsePublicURL), token the e-mail's random token.
func InvoicePaidLink(base, token string) string {
	return strings.TrimRight(base, "/") + InvoiceResponsePath(token)
}

// AppendInvoicePaidLink adds the link to an invoice e-mail's text, after a blank line.
func AppendInvoicePaidLink(body, link string) string {
	return strings.TrimRight(body, "\r\n ") + "\n\n" + invoicePaidLine + link + "\n"
}

// InvoiceEmailHTML is the HTML version of an invoice e-mail whose text carries the "I have
// paid" link: the text as paragraphs and the link as a button. "" without the link.
func InvoiceEmailHTML(body, businessName string) string {
	return responseEmailHTML(body, "The button opens a page where you can tell "+businessName+" that you paid, with a note if you like. The invoice is attached as a PDF.", []emailButton{
		{prefix: invoicePaidLine, label: "Mark as paid", bg: "#2f5d46", fg: "#ffffff", border: "#2f5d46", ok: invoiceLinkRe.MatchString},
	})
}

// ParseQuoteAnswer reads an answer ("accept" or "decline").
func ParseQuoteAnswer(s string) (QuoteAnswer, error) {
	switch a := QuoteAnswer(strings.TrimSpace(s)); a {
	case QuoteAnswerAccept, QuoteAnswerDecline:
		return a, nil
	}
	return "", invalid("answer", "accept or decline")
}
