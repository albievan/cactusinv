package domain

import (
	"regexp"
	"strings"
	"time"
)

// The customer's account page (M59): a statement e-mail carries a link to a page, without
// sign-in, with what the customer owes, their recent payments and the PDFs.

// PortalLinkValidity is how long an account link works after its e-mail was queued.
const PortalLinkValidity = 90 * 24 * time.Hour

// accountLine is the line a statement e-mail carries its account link on.
const accountLine = "View your account and download your invoices: "

// PortalPath is the web app's account page for a token ("/a/<token>").
func PortalPath(token string) string { return "/a/" + token }

var accountLinkRe = regexp.MustCompile(`^https?://[^\s"'<>]+/a/[A-Za-z0-9_-]{43}$`)

// PortalLink is a statement e-mail's account link: base is the address customers reach
// cactusInv on (ParsePublicURL), token the e-mail's random token.
func PortalLink(base, token string) string {
	return strings.TrimRight(base, "/") + PortalPath(token)
}

// AppendPortalLink adds the account link to a statement e-mail's text, after a blank line.
func AppendPortalLink(body, link string) string {
	return strings.TrimRight(body, "\r\n ") + "\n\n" + accountLine + link + "\n"
}

// StatementEmailHTML is the HTML version of a statement e-mail whose text carries the
// account link: the text as paragraphs and the link as a button. "" without the link.
func StatementEmailHTML(body, businessName string) string {
	return responseEmailHTML(body, "The button opens your account with "+businessName+": what is owed, invoice by invoice, with each invoice and this statement as a PDF. The statement is attached as a PDF.", []emailButton{
		{prefix: accountLine, label: "View your account", bg: "#2f5d46", fg: "#ffffff", border: "#2f5d46", ok: accountLinkRe.MatchString},
	})
}
