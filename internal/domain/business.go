package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultNumberFormat is the invoice number template of a new deployment, e.g.
// "CD/2108-1195/26" for customer code CD, issue date 21 August 2026 and sequence 1195.
// A saved business profile keeps its own format; this only applies until one is saved.
const DefaultNumberFormat = "{code}/{dd}{mm}-{seq}/{yy}"

// numberPlaceholders lists the placeholders an invoice number template may use.
const numberPlaceholders = "{code} {seq} {dd} {mm} {yy} {yyyy}"

// DefaultEmailSubject and DefaultEmailBody are used when nothing else is configured.
const (
	DefaultEmailSubject = "Invoice {{invoice_number}} from {{business_name}}"
	DefaultEmailBody    = "Dear {{customer_name}},\n\nPlease find attached invoice {{invoice_number}} " +
		"for {{total}}, due {{due_date}}.\n\nKind regards,\n{{business_name}}"
)

// BusinessProfile is the issuing business, printed in the invoice header (single row).
type BusinessProfile struct {
	Name         string
	AddressLines string
	Phone        string
	Email        string
	TaxNumber    string
	NumberFormat string
	TimeZone     string
	DefaultNotes string
	EmailSubject string
	EmailBody    string
	VATRate      BasisPoints // default VAT for new invoices; 0 = no VAT
	Version      int
}

// Validate checks the profile's invariants.
func (b *BusinessProfile) Validate() error {
	b.Name = strings.TrimSpace(b.Name)
	b.NumberFormat = strings.TrimSpace(b.NumberFormat)
	if b.Name == "" || len(b.Name) > 200 {
		return invalid("name", "is required (max 200 characters)")
	}
	if b.Email != "" {
		if err := validEmail("email", strings.TrimSpace(b.Email)); err != nil {
			return err
		}
		b.Email = strings.TrimSpace(b.Email)
	}
	if b.NumberFormat == "" {
		b.NumberFormat = DefaultNumberFormat
	}
	if err := ValidateNumberFormat(b.NumberFormat); err != nil {
		return err
	}
	if b.VATRate < 0 || b.VATRate > 10000 {
		return invalid("vatPercent", "must be between 0 and 100 percent")
	}
	if b.TimeZone == "" {
		b.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(b.TimeZone); err != nil {
		return invalid("timeZone", "unknown IANA time zone")
	}
	if b.EmailSubject == "" {
		b.EmailSubject = DefaultEmailSubject
	}
	if b.EmailBody == "" {
		b.EmailBody = DefaultEmailBody
	}
	if len(b.EmailSubject) > 300 || len(b.EmailBody) > 10000 {
		return invalid("email template", "too long")
	}
	return nil
}

// Location returns the business time zone (UTC if unset or invalid).
func (b BusinessProfile) Location() *time.Location {
	if loc, err := time.LoadLocation(b.TimeZone); err == nil && b.TimeZone != "" {
		return loc
	}
	return time.UTC
}

// ValidateNumberFormat checks an invoice number template. It must contain {seq} and may
// contain {code}, {dd}, {mm}, {yy}, {yyyy} and literal text.
func ValidateNumberFormat(f string) error {
	if !strings.Contains(f, "{seq}") {
		return invalid("numberFormat", "must contain {seq}")
	}
	if len(f) > 64 {
		return invalid("numberFormat", "max 64 characters")
	}
	rest := numberReplacer("X", 1, Date{Year: 2000, Month: 1, Day: 1}).Replace(f)
	if strings.ContainsAny(rest, "{}") {
		return invalid("numberFormat", "unknown placeholder; use "+numberPlaceholders)
	}
	return nil
}

// FormatInvoiceNumber renders an invoice number, e.g. ("{code}/{seq}/{yy}", "CD", 1195,
// 2026-08-21) → "CD/1195/26" and ("{code}/{dd}{mm}-{seq}/{yy}", …) → "CD/2108-1195/26".
// {dd} and {mm} are the issue day and month, two digits each.
func FormatInvoiceNumber(format, code string, seq int64, issue Date) (string, error) {
	if err := ValidateNumberFormat(format); err != nil {
		return "", err
	}
	if seq <= 0 {
		return "", invalid("seq", "must be positive")
	}
	return numberReplacer(code, seq, issue).Replace(format), nil
}

func numberReplacer(code string, seq int64, d Date) *strings.Replacer {
	return strings.NewReplacer(
		"{code}", code,
		"{seq}", strconv.FormatInt(seq, 10),
		"{yyyy}", strconv.Itoa(d.Year),
		"{yy}", fmt.Sprintf("%02d", d.Year%100),
		"{mm}", fmt.Sprintf("%02d", int(d.Month)),
		"{dd}", fmt.Sprintf("%02d", d.Day),
	)
}
