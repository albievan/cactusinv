package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/albievan/cactusinv/internal/domain"
)

// Bug hunt (M25): a subject made long by its placeholders is shortened, not refused.
func TestLongRenderedSubjectIsShortened(t *testing.T) {
	now := time.Now()
	m, err := domain.NewEmailMessage("m", "i", []string{"a@b.example"}, nil, "Invoice for "+strings.Repeat("Ä", 200), "body", now, now, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Subject) > domain.MaxSubjectLength || !utf8.ValidString(m.Subject) || !strings.HasSuffix(m.Subject, "…") {
		t.Fatalf("subject %d bytes: %q", len(m.Subject), m.Subject)
	}
}

func TestSMTPWithoutTLSNeedsNoUser(t *testing.T) {
	s := domain.SMTPSettings{Enabled: true, Host: "relay.lan", Port: 25, TLS: "none", Username: "u@example.com", From: "u@example.com"}
	if err := s.Validate(); err == nil {
		t.Fatal("a password over plain SMTP to a LAN host can never be sent")
	}
	s.Username = ""
	if err := s.Validate(); err != nil {
		t.Fatalf("no user: %v", err)
	}
	s.Host, s.Username = "localhost", "u@example.com"
	if err := s.Validate(); err != nil {
		t.Fatalf("localhost: %v", err)
	}
}

// A subtotal before discounts beyond the limit is refused even when the discounts bring
// the total under it (the stored subtotal and discount would overflow).
func TestHugeDiscountedSubtotalIsRefused(t *testing.T) {
	c := domain.Customer{ID: "c", Code: "CD", Name: "Acme", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}
	var lines []domain.Line
	for range 10 {
		lines = append(lines, domain.Line{Description: "x", Quantity: 1_000_000 * domain.QuantityScale, UnitPrice: 1_000_000_000_000, Discount: 10000})
	}
	_, err := domain.NewDraftInvoice("i", c, domain.DraftContent{IssueDate: domain.NewDate(2026, 10, 1), BillTo: domain.Party{Name: "Acme"}, Terms: c.Terms, Lines: lines}, time.Now())
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Field != "lines" {
		t.Fatalf("a subtotal that overflows: %v", err)
	}
}
