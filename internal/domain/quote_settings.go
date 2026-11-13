package domain

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Quote defaults, used until Settings → Quotes is first saved.
const (
	DefaultQuoteValidDays    = 30
	DefaultQuoteNumberFormat = "Q-{seq}/{yy}"
	DefaultQuoteEmailSubject = "Quotation {{quote_number}} from {{business_name}}"
	DefaultQuoteEmailBody    = "Dear {{customer_name}},\n\nPlease find attached quotation {{quote_number}} " +
		"for {{total}}, valid until {{valid_until}}.\n\nKind regards,\n{{business_name}}"
	MaxQuoteValidDays = 366
)

// quotePlaceholders lists the placeholders a quote number template may use. There is no
// {code}: a quote may be for an ad-hoc customer, who has no invoice code.
const quotePlaceholders = "{seq} {dd} {mm} {yy} {yyyy}"

// QuoteSettings are the business's quote defaults (Settings → Quotes; single row).
type QuoteSettings struct {
	// ValidDays is how long a new quote is valid: ValidUntil = issue date + ValidDays.
	ValidDays    int
	NumberFormat string
	EmailSubject string
	EmailBody    string
	// Terms are printed on every quote (e.g. what the price includes); a new quote starts
	// with them and may change its own copy.
	Terms string
	// PublicURL is the address customers reach cactusInv on, for the Accept and Decline
	// links in quote e-mails (M41), e.g. "https://invoices.example.com". Empty = the address
	// the quote was sent from in the browser.
	PublicURL string
	Version   int
}

// DefaultQuoteSettings are the settings before any are saved.
func DefaultQuoteSettings() QuoteSettings {
	return QuoteSettings{
		ValidDays: DefaultQuoteValidDays, NumberFormat: DefaultQuoteNumberFormat,
		EmailSubject: DefaultQuoteEmailSubject, EmailBody: DefaultQuoteEmailBody,
	}
}

// Validate checks the settings and fills empty templates with the defaults.
func (q *QuoteSettings) Validate() error {
	q.NumberFormat = strings.TrimSpace(q.NumberFormat)
	q.EmailSubject = strings.TrimSpace(q.EmailSubject)
	q.Terms = strings.TrimSpace(q.Terms)
	if q.ValidDays < 1 || q.ValidDays > MaxQuoteValidDays {
		return invalid("validDays", "must be between 1 and "+strconv.Itoa(MaxQuoteValidDays)+" days")
	}
	if q.NumberFormat == "" {
		q.NumberFormat = DefaultQuoteNumberFormat
	}
	if err := ValidateQuoteNumberFormat(q.NumberFormat); err != nil {
		return err
	}
	if q.EmailSubject == "" {
		q.EmailSubject = DefaultQuoteEmailSubject
	}
	if strings.TrimSpace(q.EmailBody) == "" {
		q.EmailBody = DefaultQuoteEmailBody
	}
	if len(q.EmailSubject) > MaxSubjectLength || strings.ContainsAny(q.EmailSubject, "\r\n") {
		return invalid("emailSubject", "single line, max 300 characters")
	}
	if len(q.EmailBody) > 10000 {
		return invalid("emailBody", "max 10000 characters")
	}
	if len(q.Terms) > 4000 {
		return invalid("terms", "max 4000 characters")
	}
	u, err := ParsePublicURL(q.PublicURL)
	if err != nil {
		return err
	}
	q.PublicURL = u
	return nil
}

// ParsePublicURL checks an address customers reach cactusInv on: empty, or an absolute
// http(s) URL with a host and no query, fragment or user info (at most 300 characters). It
// returns it without a trailing slash.
func ParsePublicURL(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(s) > 300 {
		return "", invalid("publicUrl", "an address like https://invoices.example.com (http or https, no query)")
	}
	return s, nil
}

// ValidateQuoteNumberFormat checks a quote number template: it must contain {seq} and may
// contain {dd}, {mm}, {yy}, {yyyy} and literal text.
func ValidateQuoteNumberFormat(f string) error {
	if strings.Contains(f, "{code}") {
		return invalid("numberFormat", "{code} cannot be used: a quote may be for an ad-hoc customer; use "+quotePlaceholders)
	}
	if err := ValidateNumberFormat(f); err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return invalid("numberFormat", strings.Replace(ve.Reason, numberPlaceholders, quotePlaceholders, 1))
		}
		return err
	}
	return nil
}

// FormatQuoteNumber renders a quote number, e.g. ("Q-{seq}/{yy}", 17, 2026-11-12) → "Q-17/26".
func FormatQuoteNumber(format string, seq int64, issue Date) (string, error) {
	if err := ValidateQuoteNumberFormat(format); err != nil {
		return "", err
	}
	return FormatInvoiceNumber(format, "", seq, issue)
}
