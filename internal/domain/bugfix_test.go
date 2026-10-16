package domain

import (
	"errors"
	"strings"
	"testing"
)

func fieldErr(err error) string {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Field
	}
	return ""
}

func TestHugeInvoiceIsRefusedNotWrapped(t *testing.T) {
	lines := make([]Line, 10)
	for i := range lines {
		lines[i] = Line{Description: "x", Quantity: 1_000_000_000, UnitPrice: 1_000_000_000_000}
	}
	inv := &Invoice{}
	err := inv.applyContent(DraftContent{Lines: lines, TaxRate: 1500, Terms: PaymentTerms{Kind: TermsOnReceipt}, BillTo: Party{Name: "A"}})
	if fieldErr(err) != "lines" {
		t.Fatalf("err = %v", err)
	}
	ok := &Invoice{}
	if err := ok.applyContent(DraftContent{Lines: lines[:1], Terms: PaymentTerms{Kind: TermsOnReceipt}, BillTo: Party{Name: "A"}}); fieldErr(err) != "lines" {
		t.Fatalf("one line of 1e18 minor units is beyond the total limit too: %v", err)
	}
	small := []Line{{Description: "x", Quantity: 1000, UnitPrice: 999_999_999_999}}
	if err := ok.applyContent(DraftContent{Lines: small, TaxRate: 1500, Terms: PaymentTerms{Kind: TermsOnReceipt}, BillTo: Party{Name: "A"}}); err != nil {
		t.Fatalf("a large but sane invoice: %v", err)
	}
}

func TestParseFixedRefusesSignsAndAcceptsDecimalComma(t *testing.T) {
	for _, in := range []string{"-", "+", ".", "-.", "+.", "1,500", "1,2,3", "1,5.0"} {
		if _, err := ParseAmount(in, 2); err == nil {
			t.Errorf("%q must be refused", in)
		}
	}
	for in, want := range map[string]int64{"192,94": 19294, "1,5": 150, ",5": 50, "-2,25": -225, "1.": 100, "0.5": 50} {
		got, err := ParseAmount(in, 2)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v; want %d", in, got, err, want)
		}
	}
	if q, err := ParseQuantity("1,5"); err != nil || q != 1500 {
		t.Errorf("quantity 1,5 = %v %v", q, err)
	}
}

func TestCustomerCCCapAndDuplicates(t *testing.T) {
	c := Customer{Code: "AC", Name: "Acme", Email: "a@example.com", Currency: "ZAR", Terms: PaymentTerms{Kind: TermsOnReceipt}}
	for i := 0; i < 11; i++ {
		c.CC = append(c.CC, strings.Repeat("x", i+1)+"@example.com")
	}
	if fieldErr(c.Validate()) != "cc" {
		t.Fatal("11 CCs must be refused")
	}
	c.CC = []string{"b@example.com", "B@example.com", " b@example.com "}
	if err := c.Validate(); err != nil || len(c.CC) != 1 {
		t.Fatalf("duplicates are dropped: %v %v", err, c.CC)
	}
}

func TestNumberFormatMustFitTheColumn(t *testing.T) {
	if err := ValidateNumberFormat("ACME-HOLDINGS-INTERNATIONAL-LIMITED-TAX-INVOICE-NO-{code}{seq}"); fieldErr(err) != "numberFormat" {
		t.Fatalf("too long once expanded: %v", err)
	}
	if err := ValidateNumberFormat("{code}/{dd}{mm}-{seq}/{yyyy}"); err != nil {
		t.Fatal(err)
	}
}

func TestScheduleDescriptionCheckedExpanded(t *testing.T) {
	desc := strings.Repeat("{period}", 240) // 1920 bytes typed, 3360 printed
	s := cityDeepSchedule()
	s.Lines[0].Description = desc
	err := s.Validate()
	if fieldErr(err) != "lines.description" {
		t.Fatalf("err = %v", err)
	}
}
