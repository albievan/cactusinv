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

// QuoteEmailHTML is the HTML version of a quote e-mail whose text carries response links
// (AppendTo): the text as paragraphs, and the links as an Accept and a Decline button. It
// returns "" when the text has no such links (the e-mail then goes as plain text only).
func QuoteEmailHTML(body, businessName string) string {
	var accept, decline string
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if u, ok := strings.CutPrefix(line, quoteAcceptLine); ok && quoteLinkRe.MatchString(u) && strings.HasSuffix(u, "=accept") {
			accept = u
			continue
		}
		if u, ok := strings.CutPrefix(line, quoteDeclineLine); ok && quoteLinkRe.MatchString(u) && strings.HasSuffix(u, "=decline") {
			decline = u
			continue
		}
		kept = append(kept, line)
	}
	if accept == "" || decline == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="margin:0;padding:24px;background:#f6f3ec;font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:1.5;color:#0f172a">`)
	b.WriteString(`<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e4dbc8;border-radius:12px;padding:24px">`)
	for _, p := range strings.Split(strings.TrimSpace(strings.Join(kept, "\n")), "\n\n") {
		b.WriteString(`<p style="margin:0 0 14px">` + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(p)), "\n", "<br>") + `</p>`)
	}
	b.WriteString(`<table role="presentation" cellspacing="0" cellpadding="0" style="margin:22px 0 8px"><tr>`)
	b.WriteString(button(accept, "Accept quote", "#2f5d46", "#ffffff", "#2f5d46"))
	b.WriteString(`<td style="width:12px"></td>`)
	b.WriteString(button(decline, "Decline quote", "#ffffff", "#9b2c2c", "#9b2c2c"))
	b.WriteString(`</tr></table>`)
	b.WriteString(`<p style="margin:14px 0 0;font-size:12px;color:#64748b">Each button opens a page where you confirm your answer` +
		html.EscapeString(" to "+businessName) + `. The quotation is attached as a PDF.</p>`)
	b.WriteString(`</div></body></html>`)
	return b.String()
}

func button(href, label, bg, fg, border string) string {
	return `<td style="border-radius:8px;background:` + bg + `"><a href="` + html.EscapeString(href) + `" style="display:inline-block;padding:12px 22px;border:1px solid ` +
		border + `;border-radius:8px;color:` + fg + `;background:` + bg + `;font-weight:bold;text-decoration:none">` + html.EscapeString(label) + `</a></td>`
}

// ParseQuoteAnswer reads an answer ("accept" or "decline").
func ParseQuoteAnswer(s string) (QuoteAnswer, error) {
	switch a := QuoteAnswer(strings.TrimSpace(s)); a {
	case QuoteAnswerAccept, QuoteAnswerDecline:
		return a, nil
	}
	return "", invalid("answer", "accept or decline")
}
