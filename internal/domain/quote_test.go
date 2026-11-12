package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var quoteNow = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

func quoteContent() QuoteContent {
	issue := NewDate(2026, 11, 12)
	return QuoteContent{
		IssueDate: issue, ValidUntil: issue.AddDays(30), SalesPerson: " Albie ", Reference: "Website",
		BillTo: Party{Name: " Prospect Ltd ", Address: "1 Main Rd"}, Notes: "Includes setup.", Terms: " 50% deposit. ",
		TaxRate: 1500,
		Lines:   []Line{{ProductID: "p1", Description: "Consulting", Quantity: 2 * QuantityScale, UnitPrice: 10000, Discount: 1000}},
	}
}

func newTestQuote(t *testing.T, to QuoteFor) *Quote {
	t.Helper()
	q, err := NewQuote("q1", "Q-1/26", to, quoteContent(), quoteNow)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestNewQuote(t *testing.T) {
	q := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	if q.Status != QuoteDraft || q.BillTo.Name != "Prospect Ltd" || q.SalesPerson != "Albie" || q.Terms != "50% deposit." {
		t.Fatalf("quote = %+v", q)
	}
	// 2 × 100.00 = 200.00, less 10% = 180.00, VAT 15% = 27.00.
	if want := (Totals{Subtotal: 20000, Discount: 2000, Tax: 2700, Total: 20700}); q.Totals != want {
		t.Fatalf("totals = %+v, want %+v", q.Totals, want)
	}

	for name, tc := range map[string]struct {
		to     QuoteFor
		number string
		edit   func(*QuoteContent)
		field  string
	}{
		"neither customer":   {QuoteFor{Currency: "ZAR"}, "Q-1", nil, "customerId"},
		"both customers":     {QuoteFor{CustomerID: "c", AdhocCustomerID: "a", Currency: "ZAR"}, "Q-1", nil, "customerId"},
		"no number":          {QuoteFor{CustomerID: "c", Currency: "ZAR"}, " ", nil, "number"},
		"no lines":           {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.Lines = nil }, "lines"},
		"valid before issue": {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.ValidUntil = c.IssueDate.AddDays(-1) }, "validUntil"},
		"valid too long":     {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.ValidUntil = c.IssueDate.AddDays(400) }, "validUntil"},
		"no date":            {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.IssueDate = Date{} }, "issueDate"},
		"no name":            {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.BillTo.Name = " " }, "billTo.name"},
		"vat":                {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.TaxRate = 10001 }, "vatPercent"},
		"terms":              {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.Terms = strings.Repeat("x", 4001) }, "terms"},
		"reference":          {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.Reference = strings.Repeat("x", 121) }, "reference"},
		"line":               {QuoteFor{CustomerID: "c", Currency: "ZAR"}, "Q-1", func(c *QuoteContent) { c.Lines[0].Quantity = 0 }, "lines.quantity"},
	} {
		t.Run(name, func(t *testing.T) {
			c := quoteContent()
			if tc.edit != nil {
				tc.edit(&c)
			}
			_, err := NewQuote("q", tc.number, tc.to, c, quoteNow)
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Field != tc.field {
				t.Fatalf("err = %v, want a validation error on %s", err, tc.field)
			}
		})
	}
	if _, err := NewQuote("q", "Q-1", QuoteFor{CustomerID: "c", Currency: "zz"}, quoteContent(), quoteNow); err == nil {
		t.Fatal("a bad currency was accepted")
	}
}

func TestQuoteLifecycle(t *testing.T) {
	q := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	if !q.CanDelete() || !q.CanSend() || !q.Editable() {
		t.Fatal("a new draft can be deleted, sent and edited")
	}
	later := quoteNow.Add(time.Hour)
	if err := q.MarkSent(later); err != nil || q.Status != QuoteSent || !q.SentAt.Equal(later) {
		t.Fatalf("sent: %v %+v", err, q)
	}
	if q.CanDelete() {
		t.Fatal("a sent quote cannot be deleted")
	}
	// A revision of a sent quote is a draft again, and keeps when it was first sent.
	c := q.Content()
	c.Lines[0].Quantity = 3 * QuantityScale
	if err := q.Update(c, later.Add(time.Hour)); err != nil || q.Status != QuoteDraft || q.Totals.Subtotal != 30000 {
		t.Fatalf("update: %v %+v", err, q)
	}
	if err := q.MarkSent(later.Add(2 * time.Hour)); err != nil || !q.SentAt.Equal(later) {
		t.Fatalf("second send: %v, sentAt %v", err, q.SentAt)
	}
	if err := q.Accept(later); err != nil || q.Status != QuoteAccepted {
		t.Fatalf("accept: %v", err)
	}
	if q.Editable() || !errors.Is(q.Update(quoteContent(), later), ErrInvalidTransition) {
		t.Fatal("an accepted quote cannot be edited")
	}
	if err := q.MarkSent(later); err != nil || q.Status != QuoteAccepted {
		t.Fatalf("an accepted quote may be e-mailed again and stays accepted: %v %s", err, q.Status)
	}
	if !errors.Is(q.Accept(later), ErrInvalidTransition) {
		t.Fatal("accepted twice")
	}
	if err := q.Convert("inv1", later); err != nil || q.Status != QuoteConverted || q.InvoiceID != "inv1" {
		t.Fatalf("convert: %v %+v", err, q)
	}
	if !errors.Is(q.Convert("inv2", later), ErrInvalidTransition) || !errors.Is(q.Decline("", later), ErrInvalidTransition) || q.CanSend() {
		t.Fatal("a converted quote is final")
	}
	if err := q.InvoiceDeleted(later); err != nil || q.Status != QuoteAccepted || q.InvoiceID != "" || !q.ConvertedAt.IsZero() {
		t.Fatalf("invoice deleted: %v %+v", err, q)
	}
	if !errors.Is(q.InvoiceDeleted(later), ErrInvalidTransition) {
		t.Fatal("only a converted quote goes back")
	}

	d := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	if err := d.Decline(strings.Repeat("x", 501), later); err == nil {
		t.Fatal("a long reason was accepted")
	}
	if err := d.Decline(" too dear ", later); err != nil || d.Status != QuoteDeclined || d.DeclineReason != "too dear" {
		t.Fatalf("decline: %v %+v", err, d)
	}
	if !errors.Is(d.Accept(later), ErrInvalidTransition) || !errors.Is(d.MarkSent(later), ErrInvalidTransition) || !errors.Is(d.CanConvert(), ErrInvalidTransition) {
		t.Fatal("a declined quote is final")
	}
}

func TestQuoteStage(t *testing.T) {
	q := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	if q.Stage(q.ValidUntil) != QuoteStageDraft {
		t.Fatal("valid on its last day")
	}
	if q.Stage(q.ValidUntil.AddDays(1)) != QuoteStageExpired {
		t.Fatal("expired the day after")
	}
	_ = q.Accept(quoteNow)
	if q.Stage(q.ValidUntil.AddDays(1)) != QuoteStageAccepted {
		t.Fatal("an accepted quote does not expire")
	}
}

func TestQuoteInvoiceContent(t *testing.T) {
	adhoc := newTestQuote(t, QuoteFor{AdhocCustomerID: "a1", Currency: "ZAR"})
	var ve *ValidationError
	if _, err := adhoc.InvoiceContent(Customer{ID: "c1", Currency: "ZAR"}, NewDate(2026, 12, 1)); !errors.As(err, &ve) || ve.Field != "customerId" {
		t.Fatalf("an ad-hoc customer's quote converted: %v", err)
	}
	adhoc.CustomerBecame("c1", quoteNow)

	c := Customer{ID: "c1", Name: "Prospect Ltd (Pty)", BillingAddress: "2 Side St", Attention: "Ann", Currency: "USD", Terms: PaymentTerms{Kind: TermsDaysAfter, Value: 30}}
	if _, err := adhoc.InvoiceContent(c, NewDate(2026, 12, 1)); !errors.As(err, &ve) || ve.Field != "currency" {
		t.Fatalf("currency mismatch: %v", err)
	}
	if _, err := adhoc.InvoiceContent(Customer{ID: "other", Currency: "ZAR"}, NewDate(2026, 12, 1)); err == nil {
		t.Fatal("another customer accepted")
	}
	c.Currency = "ZAR"
	got, err := adhoc.InvoiceContent(c, NewDate(2026, 12, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.BillTo.Name != "Prospect Ltd (Pty)" || got.BillTo.Attention != "Ann" || got.Terms.Value != 30 ||
		got.Header.SalesPerson != "Albie" || got.Header.Job != "Website" || got.TaxRate != 1500 ||
		got.Notes != "Includes setup." || len(got.Lines) != 1 || got.Lines[0].UnitPrice != 10000 || got.IssueDate != NewDate(2026, 12, 1) {
		t.Fatalf("content = %+v", got)
	}
	inv, err := NewDraftInvoice("i1", Customer{ID: "c1", Currency: "ZAR", Active: true}, got, quoteNow)
	if err != nil || inv.Totals != adhoc.Totals {
		t.Fatalf("the invoice totals differ from the quote's: %v %+v", err, inv)
	}
	got.Lines[0].UnitPrice = 1
	if adhoc.Lines[0].UnitPrice != 10000 {
		t.Fatal("the invoice's lines share the quote's")
	}
}

func TestQuoteSettings(t *testing.T) {
	s := QuoteSettings{ValidDays: 14}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.NumberFormat != DefaultQuoteNumberFormat || s.EmailSubject != DefaultQuoteEmailSubject || s.EmailBody != DefaultQuoteEmailBody {
		t.Fatalf("defaults not filled: %+v", s)
	}
	d := DefaultQuoteSettings()
	if err := d.Validate(); err != nil || d.ValidDays != 30 {
		t.Fatalf("defaults: %v %+v", err, d)
	}
	for name, s := range map[string]QuoteSettings{
		"no days":      {ValidDays: 0},
		"too many":     {ValidDays: 367},
		"no seq":       {ValidDays: 1, NumberFormat: "Q-{yy}"},
		"code":         {ValidDays: 1, NumberFormat: "{code}-{seq}"},
		"unknown":      {ValidDays: 1, NumberFormat: "{seq}{x}"},
		"subject":      {ValidDays: 1, EmailSubject: "a\nb"},
		"long subject": {ValidDays: 1, EmailSubject: strings.Repeat("x", 301)},
		"body":         {ValidDays: 1, EmailBody: strings.Repeat("x", 10001)},
		"terms":        {ValidDays: 1, Terms: strings.Repeat("x", 4001)},
	} {
		if err := s.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	err := (&QuoteSettings{ValidDays: 1, NumberFormat: "{seq}{x}"}).Validate()
	if !strings.Contains(err.Error(), "{seq} {dd} {mm} {yy} {yyyy}") || strings.Contains(err.Error(), "{code}") {
		t.Fatalf("the placeholders named are an invoice's: %v", err)
	}
	n, err := FormatQuoteNumber("Q{yyyy}{mm}{dd}-{seq}", 7, NewDate(2026, 11, 2))
	if err != nil || n != "Q20261102-7" {
		t.Fatalf("number = %q, %v", n, err)
	}
	if _, err := FormatQuoteNumber("{code}{seq}", 7, NewDate(2026, 11, 2)); err == nil {
		t.Fatal("{code} accepted")
	}
}

func TestAdhocCustomer(t *testing.T) {
	a := AdhocCustomer{ID: "a1", Name: " Prospect ", Address: "1 Main Rd", Attention: "Ann", Phone: "012", TaxNumber: "VAT1", Currency: "ZAR"}
	if err := a.Validate(); err != nil || a.Name != "Prospect" {
		t.Fatalf("validate: %v %+v", err, a)
	}
	for name, b := range map[string]AdhocCustomer{
		"no name":  {Currency: "ZAR"},
		"email":    {Name: "P", Email: "nope", Currency: "ZAR"},
		"currency": {Name: "P", Currency: "Z"},
		"address":  {Name: "P", Address: strings.Repeat("x", 1001), Currency: "ZAR"},
		"phone":    {Name: "P", Phone: strings.Repeat("1", 101), Currency: "ZAR"},
	} {
		if err := b.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}

	// No e-mail address yet: converting needs one.
	if _, err := a.ToCustomer("c1", CustomerDetails{Code: "PR"}); err == nil {
		t.Fatal("a customer without an e-mail address was made")
	}
	c, err := a.ToCustomer("c1", CustomerDetails{Code: "pr", Email: "ann@prospect.example"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "PR" || c.Name != "Prospect" || c.BillingAddress != "1 Main Rd" || c.Attention != "Ann" || c.Phone != "012" ||
		c.TaxNumber != "VAT1" || c.Currency != "ZAR" || c.Terms.Kind != TermsOnReceipt || !c.Active {
		t.Fatalf("customer = %+v", c)
	}
	c, err = a.ToCustomer("c1", CustomerDetails{Code: "PR", Email: "a@b.example", Currency: "USD", Terms: PaymentTerms{Kind: TermsDaysAfter, Value: 30}})
	if err != nil || c.Currency != "USD" || c.Terms.Value != 30 {
		t.Fatalf("details: %v %+v", err, c)
	}
	if err := a.MarkConverted("c1", quoteNow); err != nil || !a.Converted() || !a.ConvertedAt.Equal(quoteNow) {
		t.Fatalf("mark: %v %+v", err, a)
	}
	if !errors.Is(a.MarkConverted("c2", quoteNow), ErrInvalidTransition) {
		t.Fatal("converted twice")
	}
	if _, err := a.ToCustomer("c2", CustomerDetails{Code: "PR", Email: "a@b.example"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("a converted ad-hoc customer converted again: %v", err)
	}
}

func TestQuoteTemplate(t *testing.T) {
	q := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	data := QuoteTemplateData(q, Currency{Code: "ZAR", Symbol: "R", MinorUnits: 2}, BusinessProfile{Name: "Cactus"})
	got := RenderTemplate(DefaultQuoteEmailBody, data)
	want := "Dear Prospect Ltd,\n\nPlease find attached quotation Q-1/26 for R 207.00, valid until 12 December 2026.\n\nKind regards,\nCactus"
	if got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if s := SubjectLine(DefaultQuoteEmailSubject, data); s != "Quotation Q-1/26 from Cactus" {
		t.Fatalf("subject = %q", s)
	}
	if d := QuoteTemplateData(q, Currency{Code: "ZAR", MinorUnits: 2}, BusinessProfile{}); !strings.HasPrefix(d.Total, "ZAR ") {
		t.Fatalf("total without a symbol = %q", d.Total)
	}
}

func TestQuoteFingerprint(t *testing.T) {
	q := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	fp := q.Fingerprint()
	if len(fp) != 14 || fp[4] != '-' || fp[9] != '-' || fp != strings.ToUpper(fp) {
		t.Fatalf("fingerprint = %q", fp)
	}
	same := newTestQuote(t, QuoteFor{CustomerID: "c1", Currency: "ZAR"})
	_ = same.MarkSent(quoteNow) // the status is not printed content
	if same.Fingerprint() != fp {
		t.Fatal("the same content gave another code")
	}
	for name, edit := range map[string]func(*QuoteContent){
		"price":       func(c *QuoteContent) { c.Lines[0].UnitPrice++ },
		"description": func(c *QuoteContent) { c.Lines[0].Description += "s" },
		"valid until": func(c *QuoteContent) { c.ValidUntil = c.ValidUntil.AddDays(1) },
		"terms":       func(c *QuoteContent) { c.Terms += "!" },
		"vat":         func(c *QuoteContent) { c.TaxRate = 0 },
		// Field boundaries count: moving text from one field to the next changes the code.
		"boundary": func(c *QuoteContent) { c.SalesPerson, c.Reference = "AlbieW", "ebsite" },
	} {
		c := quoteContent()
		edit(&c)
		other, err := NewQuote("q1", "Q-1/26", QuoteFor{CustomerID: "c1", Currency: "ZAR"}, c, quoteNow)
		if err != nil {
			t.Fatal(err)
		}
		if other.Fingerprint() == fp {
			t.Errorf("%s: the code did not change", name)
		}
	}
}
