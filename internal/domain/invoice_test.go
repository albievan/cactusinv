package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)

// cityDeep is the customer on example/Inv_no_CD_1195_26.pdf.
func cityDeep() Customer {
	return Customer{
		ID: "cust-1", Code: "CD", Name: "Acme Commercial Spares (Pty) Limited",
		Email: "accounts@acme.example", Attention: "Jane",
		BillingAddress:  "P O BOX 2000,\nGERMISTON,\n1401",
		ShippingName:    "Acme Commercial Spares (Pty) Ltd",
		ShippingAddress: "No. 1 Example Road,\nIndustria Ext. 1,\nJohannesburg.\n2001",
		Currency:        "ZAR", Terms: PaymentTerms{Kind: TermsDayOfNextMonth, Value: 7}, Active: true,
	}
}

func referenceLines() []Line {
	return []Line{
		{Description: "Stock system usage for 14 concurrent users.", Quantity: 14000, UnitPrice: 19294},
		{Description: "Alchamex integration - monthly subscription", Quantity: 1000, UnitPrice: 55000},
	}
}

// TestReferenceInvoice reproduces example/Inv_no_CD_1195_26.pdf to the cent. Keep forever.
func TestReferenceInvoice(t *testing.T) {
	c := cityDeep()
	content := DefaultDraftContent(c, NewDate(2026, 8, 21))
	content.Lines = referenceLines()
	inv, err := NewDraftInvoice("inv-1", c, content, t0)
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Lines[0].Amount(); got != 270116 {
		t.Errorf("line 1 amount = %d, want 270116", got)
	}
	want := Totals{Subtotal: 325116, Discount: 0, Tax: 0, Total: 325116}
	if inv.Totals != want {
		t.Errorf("totals = %+v, want %+v", inv.Totals, want)
	}
	num, err := FormatInvoiceNumber("{code}/{seq}/{yy}", c.Code, 1195, NewDate(2026, 8, 21))
	if err != nil || num != "CD/1195/26" {
		t.Fatalf("number = %q, %v", num, err)
	}
	bank := BankDetails{BankName: "Capitecbank", Branch: "Northgate", AccountType: "Savings", AccountNumber: "1234567890", BranchCode: "470010"}
	if err := inv.Issue(num, NewDate(2026, 8, 21), bank, t0); err != nil {
		t.Fatal(err)
	}
	if inv.DueDate != NewDate(2026, 9, 7) {
		t.Errorf("due date = %v, want 2026-09-07", inv.DueDate)
	}
	if inv.TermsText != "Payment due before 7th of the next month" {
		t.Errorf("terms text = %q", inv.TermsText)
	}
	if inv.ShipTo.Name != c.ShippingName || inv.BillTo.Attention != "Jane" {
		t.Errorf("addresses not snapshotted: %+v %+v", inv.BillTo, inv.ShipTo)
	}
}

func TestTotalsWithDiscountAndTax(t *testing.T) {
	lines := []Line{
		{Description: "a", Quantity: 2000, UnitPrice: 10000, Discount: 1000}, // 200.00 - 20.00
		{Description: "b", Quantity: 1500, UnitPrice: 333},                   // 4.995 → 5.00
	}
	got := ComputeTotals(lines, 1500)
	want := Totals{Subtotal: 20500, Discount: 2000, Tax: 2775, Total: 21275}
	if got != want {
		t.Fatalf("ComputeTotals = %+v, want %+v", got, want)
	}
}

func TestDraftValidation(t *testing.T) {
	c := cityDeep()
	base := func() DraftContent {
		d := DefaultDraftContent(c, NewDate(2026, 8, 21))
		d.Lines = referenceLines()
		return d
	}
	tests := []struct {
		name   string
		mutate func(*DraftContent)
	}{
		{"no lines", func(d *DraftContent) { d.Lines = nil }},
		{"empty description", func(d *DraftContent) { d.Lines[0].Description = " " }},
		{"zero quantity", func(d *DraftContent) { d.Lines[0].Quantity = 0 }},
		{"negative price", func(d *DraftContent) { d.Lines[0].UnitPrice = -1 }},
		{"discount > 100", func(d *DraftContent) { d.Lines[0].Discount = 10001 }},
		{"tax > 100", func(d *DraftContent) { d.TaxRate = 10001 }},
		{"bad terms", func(d *DraftContent) { d.Terms = PaymentTerms{Kind: "nope"} }},
		{"no bill-to", func(d *DraftContent) { d.BillTo.Name = "" }},
		{"too many lines", func(d *DraftContent) { d.Lines = make([]Line, MaxInvoiceLines+1) }},
		{"long notes", func(d *DraftContent) { d.Notes = string(make([]byte, 4001)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := base()
			tt.mutate(&d)
			if _, err := NewDraftInvoice("x", c, d, t0); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
	inactive := c
	inactive.Active = false
	if _, err := NewDraftInvoice("x", inactive, base(), t0); !errors.Is(err, ErrInactive) {
		t.Fatalf("inactive customer: err = %v", err)
	}
}

func TestInvoiceLifecycle(t *testing.T) {
	c := cityDeep()
	d := DefaultDraftContent(c, NewDate(2026, 8, 21))
	d.Lines = referenceLines()
	newInv := func() *Invoice {
		inv, err := NewDraftInvoice("i", c, d, t0)
		if err != nil {
			t.Fatal(err)
		}
		return inv
	}

	inv := newInv()
	if !inv.CanDelete() || inv.CanSend() {
		t.Fatal("draft should be deletable and not sendable")
	}
	if err := inv.MarkSent(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("draft cannot be sent")
	}
	if err := inv.MarkPaid(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("draft cannot be paid")
	}
	if err := inv.Void("x", t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("draft cannot be voided")
	}
	if err := inv.Issue("", NewDate(2026, 8, 21), BankDetails{}, t0); !errors.Is(err, ErrValidation) {
		t.Fatal("number required")
	}
	if err := inv.Issue("N1", Date{}, BankDetails{}, t0); !errors.Is(err, ErrValidation) {
		t.Fatal("issue date required")
	}
	if err := inv.Issue("N1", NewDate(2026, 8, 21), BankDetails{}, t0); err != nil {
		t.Fatal(err)
	}
	if err := inv.Issue("N2", NewDate(2026, 8, 21), BankDetails{}, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("cannot issue twice")
	}
	if err := inv.UpdateDraft(d, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("issued invoice is immutable")
	}
	if inv.CanDelete() || !inv.CanSend() {
		t.Fatal("issued: not deletable, sendable")
	}
	if !inv.IsOverdue(NewDate(2026, 9, 8)) || inv.IsOverdue(NewDate(2026, 9, 7)) {
		t.Fatal("overdue boundary wrong")
	}
	later := t0.Add(time.Hour)
	if err := inv.MarkSent(later); err != nil || inv.Status != InvoiceSent || !inv.SentAt.Equal(later) {
		t.Fatalf("MarkSent: %v %v", err, inv.Status)
	}
	if err := inv.MarkSent(later.Add(time.Hour)); err != nil || !inv.SentAt.Equal(later) {
		t.Fatal("re-send keeps first SentAt")
	}
	if err := inv.MarkPaid(later); err != nil || inv.Status != InvoicePaid {
		t.Fatal("MarkPaid failed")
	}
	if inv.IsOverdue(NewDate(2027, 1, 1)) {
		t.Fatal("paid invoice is never overdue")
	}
	if err := inv.MarkSent(later); err != nil || inv.Status != InvoicePaid {
		t.Fatal("re-sending a paid invoice keeps it paid")
	}
	if err := inv.Void("x", later); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("paid cannot be voided")
	}

	inv = newInv()
	_ = inv.Issue("N3", NewDate(2026, 8, 21), BankDetails{}, t0)
	if err := inv.Void(" ", t0); !errors.Is(err, ErrValidation) {
		t.Fatal("void needs a reason")
	}
	if err := inv.Void("duplicate", t0); err != nil || inv.Status != InvoiceVoid {
		t.Fatal("void failed")
	}
	if err := inv.MarkSent(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("void cannot be sent")
	}

	inv = newInv()
	d2 := d
	d2.Lines = []Line{{Description: "x", Quantity: 1000, UnitPrice: 100}}
	if err := inv.UpdateDraft(d2, t0); err != nil || inv.Totals.Total != 100 {
		t.Fatalf("UpdateDraft: %v %+v", err, inv.Totals)
	}
}

func TestPaymentTerms(t *testing.T) {
	issue := NewDate(2026, 1, 31)
	tests := []struct {
		terms PaymentTerms
		due   Date
		text  string
	}{
		{PaymentTerms{Kind: TermsOnReceipt}, issue, "Payment due on receipt"},
		{PaymentTerms{Kind: TermsDaysAfter, Value: 30}, NewDate(2026, 3, 2), "Payment due within 30 days"},
		{PaymentTerms{Kind: TermsDaysAfter, Value: 0}, issue, "Payment due on receipt"},
		{PaymentTerms{Kind: TermsDayOfNextMonth, Value: 31}, NewDate(2026, 2, 28), "Payment due before 31st of the next month"},
		{PaymentTerms{Kind: TermsDayOfNextMonth, Value: 2}, NewDate(2026, 2, 2), "Payment due before 2nd of the next month"},
	}
	for _, tt := range tests {
		if got := tt.terms.DueDate(issue); got != tt.due {
			t.Errorf("%+v DueDate = %v, want %v", tt.terms, got, tt.due)
		}
		if got := tt.terms.Text(); got != tt.text {
			t.Errorf("%+v Text = %q, want %q", tt.terms, got, tt.text)
		}
	}
	if d := (PaymentTerms{Kind: TermsDayOfNextMonth, Value: 7}).DueDate(NewDate(2026, 12, 15)); d != NewDate(2027, 1, 7) {
		t.Errorf("December rolls into next year: %v", d)
	}
	for _, bad := range []PaymentTerms{{Kind: TermsDaysAfter, Value: -1}, {Kind: TermsDaysAfter, Value: 366}, {Kind: TermsDayOfNextMonth, Value: 0}, {Kind: TermsDayOfNextMonth, Value: 32}, {Kind: "x"}} {
		if _, err := NewPaymentTerms(bad.Kind, bad.Value); err == nil {
			t.Errorf("NewPaymentTerms(%+v) expected error", bad)
		}
	}
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 101: "101st", 111: "111th"} {
		if got := Ordinal(n); got != want {
			t.Errorf("Ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestInvoiceNumbering(t *testing.T) {
	d := NewDate(2026, 8, 21)
	tests := []struct {
		name, format string
		issue        Date
		seq          int64
		want         string
		wantErr      bool
	}{
		{"example invoice", "{code}/{seq}/{yy}", d, 1195, "CD/1195/26", false},
		{"day and month", "{code}/{dd}{mm}-{seq}/{yy}", d, 1195, "CD/2108-1195/26", false},
		{"default format", DefaultNumberFormat, d, 1195, "CD/2108-1195/26", false},
		{"single-digit day and month are padded", "{code}/{dd}{mm}-{seq}/{yy}", NewDate(2027, 1, 5), 1196, "CD/0501-1196/27", false},
		{"last day of the year", "{code}/{dd}{mm}-{seq}/{yy}", NewDate(2026, 12, 31), 7, "CD/3112-7/26", false},
		{"day beside the full year", "{yyyy}{mm}{dd}-{seq}", d, 1195, "20260821-1195", false},
		{"month and full year", "INV-{yyyy}{mm}-{seq}", d, 1195, "INV-202608-1195", false},
		{"no sequence", "{code}-{dd}{mm}", d, 1195, "", true},
		{"unknown placeholder", "{seq}-{oops}", d, 1195, "", true},
		{"three-letter day is not a placeholder", "{seq}-{ddd}", d, 1195, "", true},
		{"one-letter day is not a placeholder", "{seq}-{d}", d, 1195, "", true},
		{"placeholders are lower case", "{seq}-{DD}", d, 1195, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatInvoiceNumber(tt.format, "CD", tt.seq, tt.issue)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("FormatInvoiceNumber(%q) = %q, %v; want %q", tt.format, got, err, tt.want)
			}
		})
	}
	if err := ValidateNumberFormat("{seq}-{oops}"); err == nil || !strings.Contains(err.Error(), "{dd}") {
		t.Errorf("the unknown-placeholder error names {dd}: %v", err)
	}
	if DefaultNumberFormat != "{code}/{dd}{mm}-{seq}/{yy}" {
		t.Errorf("default format = %q", DefaultNumberFormat)
	}
	if _, err := FormatInvoiceNumber(DefaultNumberFormat, "CD", 0, d); err == nil {
		t.Error("seq 0 must fail")
	}
}

func TestBusinessProfileValidate(t *testing.T) {
	b := BusinessProfile{Name: "Example Software Consulting", TimeZone: "Africa/Johannesburg"}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	if b.NumberFormat != DefaultNumberFormat || b.EmailSubject == "" || b.EmailBody == "" {
		t.Fatal("defaults not applied")
	}
	if b.Location().String() != "Africa/Johannesburg" {
		t.Fatal("location")
	}
	for _, bad := range []BusinessProfile{
		{Name: ""},
		{Name: "x", Email: "nope"},
		{Name: "x", NumberFormat: "{code}"},
		{Name: "x", TimeZone: "Mars/Olympus"},
		{Name: "x", VATRate: 10001},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("Validate(%+v) = %v", bad, err)
		}
	}
	if (BusinessProfile{}).Location() != time.UTC {
		t.Error("empty tz → UTC")
	}
}

func TestDates(t *testing.T) {
	d, err := ParseDate("2026-08-21")
	if err != nil || d.String() != "2026-08-21" || d.Long() != "21 August 2026" {
		t.Fatalf("ParseDate: %v %v", d, err)
	}
	if _, err := ParseDate("21/08/2026"); err == nil {
		t.Fatal("bad format accepted")
	}
	if !d.Before(d.AddDays(1)) || !d.AddDays(1).After(d) || d.Before(d) {
		t.Fatal("ordering")
	}
	if DaysIn(2028, time.February) != 29 || DaysIn(2026, time.February) != 28 {
		t.Fatal("leap years")
	}
	if !(Date{}).IsZero() || d.IsZero() {
		t.Fatal("IsZero")
	}
}

func TestOverdueStageAndDisclaimer(t *testing.T) {
	c := cityDeep()
	d := DefaultDraftContent(c, NewDate(2026, 8, 21))
	d.Lines = referenceLines()
	inv, _ := NewDraftInvoice("i", c, d, t0)
	today := NewDate(2026, 8, 22)
	if inv.Stage(today) != StageDraft {
		t.Fatal("draft")
	}
	if err := inv.MarkOverdue(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("draft cannot be overdue")
	}
	_ = inv.Issue("N", NewDate(2026, 8, 21), BankDetails{}, t0)
	if inv.Stage(today) != StageIssued {
		t.Fatal("issued")
	}
	_ = inv.MarkSent(t0)
	if inv.Stage(today) != StageEmailed {
		t.Fatal("emailed")
	}
	if inv.Stage(NewDate(2026, 9, 8)) != StageOverdue {
		t.Fatal("past due shows overdue")
	}
	if err := inv.MarkOverdue(t0); err != nil || inv.Status != InvoiceOverdue || inv.Stage(today) != StageOverdue || !inv.IsOverdue(today) {
		t.Fatalf("mark overdue: %v %v", err, inv.Status)
	}
	if err := inv.MarkOverdue(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("already overdue")
	}
	if err := inv.MarkSent(t0); err != nil || inv.Status != InvoiceOverdue || !inv.CanSend() {
		t.Fatal("re-sending keeps overdue")
	}
	if err := inv.MarkPaid(t0); err != nil || inv.Stage(today) != StagePaid {
		t.Fatal("overdue can be paid")
	}
	inv2, _ := NewDraftInvoice("j", c, d, t0)
	_ = inv2.Issue("M", NewDate(2026, 8, 21), BankDetails{}, t0)
	_ = inv2.MarkOverdue(t0)
	if err := inv2.Void("x", t0); err != nil || inv2.Stage(today) != StageVoid {
		t.Fatal("overdue can be voided")
	}
	if inv.Notes != "" {
		t.Fatal("the disclaimer is not copied onto the invoice")
	}
	b := BusinessProfile{DefaultNotes: " Business disclaimer "}
	if Disclaimer(Customer{}, b) != "Business disclaimer" {
		t.Fatal("business disclaimer")
	}
	if Disclaimer(Customer{DefaultNotes: "Customer disclaimer"}, b) != "Customer disclaimer" {
		t.Fatal("customer disclaimer wins")
	}
	for _, tt := range []struct {
		notes string
		own   bool
	}{{"", false}, {"  \n\t", false}, {"Customer disclaimer", true}} {
		if got := UsesOwnDisclaimer(Customer{DefaultNotes: tt.notes}); got != tt.own {
			t.Errorf("UsesOwnDisclaimer(%q) = %v", tt.notes, got)
		}
		want := "Business disclaimer"
		if tt.own {
			want = tt.notes
		}
		if got := Disclaimer(Customer{DefaultNotes: tt.notes}, b); got != want {
			t.Errorf("Disclaimer(%q) = %q, want %q", tt.notes, got, want)
		}
	}
}
