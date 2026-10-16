package domain

import (
	"strings"
	"time"
)

// EmailStatus is an outbox message's state (ADR-004).
type EmailStatus string

// E-mail statuses.
const (
	EmailPending   EmailStatus = "pending"
	EmailSending   EmailStatus = "sending"
	EmailSent      EmailStatus = "sent"
	EmailFailed    EmailStatus = "failed"
	EmailCancelled EmailStatus = "cancelled"
)

// Outbox delivery tuning.
const (
	MaxEmailAttempts = 8
	maxBackoff       = 6 * time.Hour
	// StaleSendingAfter is when a message stuck in "sending" is reclaimed.
	StaleSendingAfter = 10 * time.Minute
)

// Backoff is the retry delay after the given number of failed attempts: 1m·2^(n-1),
// capped at 6h.
func Backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 12 {
		return maxBackoff
	}
	return min(time.Minute<<(attempts-1), maxBackoff)
}

// EmailKind says what an outbox message carries.
type EmailKind string

// E-mail kinds.
const (
	EmailKindInvoice   EmailKind = "invoice"   // one invoice PDF
	EmailKindStatement EmailKind = "statement" // a customer's statement of account PDF
)

// EmailMessage is an invoice or statement e-mail in the transactional outbox.
type EmailMessage struct {
	ID             string
	Kind           EmailKind
	InvoiceID      string // invoice e-mails
	CustomerID     string // statement e-mails
	To             []string
	CC             []string
	Subject        string
	Body           string
	Status         EmailStatus
	Attempts       int
	SendAt         time.Time
	NextAttemptAt  time.Time
	LastError      string
	SentAt         time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	IdempotencyKey string
}

// NewEmailMessage validates and creates a pending outbox message to go out at sendAt
// (now or later).
func NewEmailMessage(id, invoiceID string, to, cc []string, subject, body string, sendAt, now time.Time, key string) (*EmailMessage, error) {
	if len(to) == 0 {
		return nil, invalid("to", "at least one recipient is required")
	}
	if len(to)+len(cc) > 20 {
		return nil, invalid("to", "at most 20 recipients")
	}
	for _, a := range append(append([]string{}, to...), cc...) {
		if err := validEmail("to", a); err != nil {
			return nil, err
		}
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || len(subject) > 300 || strings.ContainsAny(subject, "\r\n") {
		return nil, invalid("subject", "is required, single line, max 300 characters")
	}
	if len(body) > 20000 {
		return nil, invalid("body", "max 20000 characters")
	}
	if sendAt.IsZero() || sendAt.Before(now) {
		sendAt = now
	}
	if sendAt.After(now.AddDate(1, 0, 0)) {
		return nil, invalid("sendAt", "must be within one year")
	}
	return &EmailMessage{
		ID: id, Kind: EmailKindInvoice, InvoiceID: invoiceID, To: to, CC: cc, Subject: subject, Body: body,
		Status: EmailPending, SendAt: sendAt, NextAttemptAt: sendAt,
		CreatedAt: now, UpdatedAt: now, IdempotencyKey: key,
	}, nil
}

// Claim marks the message as being sent by a dispatcher.
func (m *EmailMessage) Claim(now time.Time) error {
	if !m.Claimable(now) {
		return ErrInvalidTransition
	}
	m.Status = EmailSending
	m.Attempts++
	m.UpdatedAt = now
	return nil
}

// Claimable reports whether a dispatcher may pick the message up at now.
func (m *EmailMessage) Claimable(now time.Time) bool {
	switch m.Status {
	case EmailPending:
		return !m.NextAttemptAt.After(now)
	case EmailSending:
		return now.Sub(m.UpdatedAt) >= StaleSendingAfter
	default:
		return false
	}
}

// Succeeded records delivery of a message being sent and reports whether it did: a
// message that is no longer "sending" (recorded by another dispatcher meanwhile) is left as
// it is.
func (m *EmailMessage) Succeeded(now time.Time) bool {
	if m.Status != EmailSending {
		return false
	}
	m.Status = EmailSent
	m.SentAt = now
	m.LastError = ""
	m.UpdatedAt = now
	return true
}

// Failed records a failed attempt: retry later with backoff, or give up.
func (m *EmailMessage) Failed(reason string, now time.Time) {
	m.LastError = truncate(reason, 1000)
	m.UpdatedAt = now
	if m.Attempts >= MaxEmailAttempts {
		m.Status = EmailFailed
		return
	}
	m.Status = EmailPending
	m.NextAttemptAt = now.Add(Backoff(m.Attempts))
}

// Cancel withdraws a message that has not been sent.
func (m *EmailMessage) Cancel(now time.Time) error {
	if m.Status != EmailPending && m.Status != EmailFailed {
		return ErrInvalidTransition
	}
	m.Status = EmailCancelled
	m.UpdatedAt = now
	return nil
}

// Retry re-queues a failed or cancelled message for immediate delivery.
func (m *EmailMessage) Retry(now time.Time) error {
	if m.Status != EmailFailed && m.Status != EmailCancelled {
		return ErrInvalidTransition
	}
	m.Status = EmailPending
	m.Attempts = 0
	m.NextAttemptAt = now
	m.UpdatedAt = now
	return nil
}

// TemplateData is what e-mail subject/body placeholders can refer to.
type TemplateData struct {
	InvoiceNumber string
	CustomerName  string
	Attention     string
	Total         string
	DueDate       string
	IssueDate     string
	BusinessName  string
}

// RenderTemplate substitutes {{invoice_number}}, {{customer_name}}, {{attention}},
// {{total}}, {{due_date}}, {{issue_date}}, {{date}} (the same as {{issue_date}}: the
// invoice's issue date or the statement's as-at date) and {{business_name}}. Unknown
// placeholders are left as written.
func RenderTemplate(tmpl string, d TemplateData) string {
	return strings.NewReplacer(
		"{{invoice_number}}", d.InvoiceNumber,
		"{{customer_name}}", d.CustomerName,
		"{{attention}}", d.Attention,
		"{{total}}", d.Total,
		"{{due_date}}", d.DueDate,
		"{{issue_date}}", d.IssueDate,
		"{{date}}", d.IssueDate,
		"{{business_name}}", d.BusinessName,
	).Replace(tmpl)
}

// SubjectLine renders a subject template and strips line breaks (header injection).
func SubjectLine(tmpl string, d TemplateData) string {
	s := RenderTemplate(tmpl, d)
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// TemplateDataFor builds placeholder values for an invoice.
func TemplateDataFor(inv *Invoice, cur Currency, business BusinessProfile) TemplateData {
	sym := cur.Symbol
	if sym == "" {
		sym = string(cur.Code)
	}
	return TemplateData{
		InvoiceNumber: inv.Number,
		CustomerName:  inv.BillTo.Name,
		Attention:     inv.BillTo.Attention,
		Total:         sym + " " + FormatGrouped(inv.Totals.Total, cur.MinorUnits, ","),
		DueDate:       inv.DueDate.Long(),
		IssueDate:     inv.IssueDate.Long(),
		BusinessName:  business.Name,
	}
}
