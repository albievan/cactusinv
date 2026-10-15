package pdf

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// reference rebuilds the reference invoice with fictitious details.
func reference(t *testing.T) ports.InvoiceDocument {
	t.Helper()
	c := domain.Customer{
		ID: "c", Code: "CD", Name: "Acme Commercial Spares (Pty) Limited", Attention: "Jane",
		BillingAddress:  "P O BOX 2000,\nGERMISTON,\n1400",
		ShippingName:    "Acme Commercial Spares (Pty) Ltd",
		ShippingAddress: "No. 1 Example Road,\nIndustria Ext. 1,\nJohannesburg.\n2001",
		Currency:        "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsDayOfNextMonth, Value: 7}, Active: true,
		DefaultNotes: "Disclaimer:\nWe accept no responsibility for any 3rd Party software integrated with the stock system that has not been supplied by PrimeSoft.",
	}
	content := domain.DefaultDraftContent(c, domain.NewDate(2026, 8, 21))
	content.Lines = []domain.Line{
		{Description: "Stock system usage for 14 concurrent users.", Quantity: 14000, UnitPrice: 19294},
		{Description: "Alchamex integration - monthly subscription", Quantity: 1000, UnitPrice: 55000},
	}
	inv, err := domain.NewDraftInvoice("i", c, content, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.Issue("CD/1195/26", domain.NewDate(2026, 8, 21), domain.BankDetails{
		BankName: "Capitecbank", Branch: "Northgate", AccountType: "Savings", AccountNumber: "1234567890", BranchCode: "470010",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	b := domain.BusinessProfile{Name: "Example Software Consulting", AddressLines: "P. O. Box 1000\nNorthgate\n1616", Phone: "011 555 0100"}
	return ports.InvoiceDocument{
		Invoice: inv, Business: b, Disclaimer: domain.Disclaimer(c, b),
		Currency: domain.Currency{Code: "ZAR", Name: "South African Rand", Symbol: "R", MinorUnits: 2},
	}
}

func render(t *testing.T, doc ports.InvoiceDocument) []byte {
	t.Helper()
	out, err := Renderer{Uncompressed: true}.RenderPDF(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
	return out
}

func save(t *testing.T, name string, out []byte) {
	if dir := os.Getenv("CACTUSINV_PDF_OUT"); dir != "" {
		if err := os.WriteFile(dir+"/"+name, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderReferenceInvoice(t *testing.T) {
	out := render(t, reference(t))
	for _, want := range []string{
		"Example Software Consulting", "I N V O I C E", "CD/1195/26", "21 August 2026", "7 September 2026",
		"Acme Commercial Spares \\(Pty\\) Limited", "Attn: Jane", "SHIP TO", "Payment due before 7th of the next month",
		"Stock system usage for 14 concurrent users.", "192.94", "2,701.16", "550.00", "Subtotal", "3,251.16", "Total ZAR",
		"PAYMENT DETAILS", "Capitecbank", "Northgate", "Sort / branch code", "470010", "Savings account", "1234567890",
		"Disclaimer:", "Page 1 of",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("PDF is missing %q", want)
		}
	}
	for _, absent := range []string{"VAT", "DRAFT", "Discount", "DISC"} {
		if bytes.Contains(out, []byte(absent)) {
			t.Errorf("PDF must not contain %q", absent)
		}
	}
	save(t, "invoice.pdf", out)
}

func TestRenderVariants(t *testing.T) {
	draft := reference(t)
	inv := *draft.Invoice
	inv.Status, inv.Number, inv.Bank = domain.InvoiceDraft, "", domain.BankDetails{}
	draft.Invoice = &inv
	out := render(t, draft)
	save(t, "draft.pdf", out)
	for _, want := range []string{"DRAFT", "Not issued", "Payment details are added when the invoice is issued."} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("draft PDF missing %q", want)
		}
	}

	voided := reference(t)
	_ = voided.Invoice.Void("Duplicate", time.Now())
	if out := render(t, voided); !bytes.Contains(out, []byte("VOID")) || !bytes.Contains(out, []byte("Voided: Duplicate")) {
		t.Error("void")
	}
	paid := reference(t)
	_ = paid.Invoice.MarkPaid(time.Now())
	if out := render(t, paid); !bytes.Contains(out, []byte("PAID")) {
		t.Error("paid")
	}
	overdue := reference(t)
	_ = overdue.Invoice.MarkOverdue(time.Now())
	if out := render(t, overdue); !bytes.Contains(out, []byte("OVERDUE")) {
		t.Error("overdue pill")
	}

	// Many lines, VAT and discounts paginate; the euro survives cp1252; info strip fields.
	long := reference(t)
	inv2 := *long.Invoice
	inv2.Lines = nil
	for i := range 60 {
		inv2.Lines = append(inv2.Lines, domain.Line{Description: strings.Repeat("Consulting hours for project €", 1+i%3), Quantity: 1500, UnitPrice: 10000, Discount: 1000})
	}
	inv2.TaxRate = 1500
	inv2.Totals = domain.ComputeTotals(inv2.Lines, inv2.TaxRate)
	inv2.Header = domain.InvoiceHeader{SalesPerson: "Albie", Job: "J-42", DeliveryDate: domain.NewDate(2026, 8, 30)}
	inv2.Notes = "Thank you for your business."
	inv2.Bank.SwiftBIC, inv2.Bank.IBAN, inv2.Bank.AccountName, inv2.Bank.Country = "CABLZAJJ", "ZA00 0000", "Example", "South Africa"
	long.Invoice = &inv2
	long.Business.TaxNumber, long.Business.Email = "4000000000", "billing@example.com"
	out = render(t, long)
	if n := bytes.Count(out, []byte("/Type /Page\n")); n < 2 {
		t.Errorf("expected several pages, got %d", n)
	}
	for _, want := range []string{"Continued on next page", "\\(continued\\)", "VAT 15%", "10%", "DISC", "Discount", "SWIFT", "CABLZAJJ", "Country", "South Africa", "VAT No: 4000000000", "SALES PERSON", "J-42", "Thank you for your business."} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("long PDF missing %q", want)
		}
	}
	save(t, "invoice-long.pdf", out)
	compressed, err := Renderer{}.RenderPDF(context.Background(), reference(t))
	if err != nil || len(compressed) >= len(out) {
		t.Errorf("compressed output: %d bytes, %v", len(compressed), err)
	}
	if accountLabel("") != "Account number" {
		t.Error("account label fallback")
	}
}

func TestDisclaimerIsRenderedFromTheDocument(t *testing.T) {
	doc := reference(t)
	doc.Disclaimer = "Updated disclaimer: terms changed."
	out := render(t, doc)
	if !bytes.Contains(out, []byte("Updated disclaimer: terms changed.")) || bytes.Contains(out, []byte("PrimeSoft")) {
		t.Error("the disclaimer comes from the document, not the invoice")
	}
}

func TestRenderStatement(t *testing.T) {
	ref := reference(t)
	c := domain.Customer{ID: "c", Code: "CD", Name: "Acme Commercial Spares (Pty) Limited", BillingAddress: "P O BOX 2000", Currency: "ZAR"}
	mk := func(num string, due domain.Date, total int64) domain.Invoice {
		return domain.Invoice{ID: num, Number: num, CustomerID: "c", Status: domain.InvoiceSent, Currency: "ZAR", IssueDate: due.AddDays(-30), DueDate: due, Totals: domain.Totals{Total: total}}
	}
	st := domain.NewStatement(c, []domain.Invoice{
		mk("CD/1195/26", domain.NewDate(2026, 9, 7), 325116),
		mk("CD/1196/26", domain.NewDate(2026, 10, 7), 325116),
		mk("CD/1197/26", domain.NewDate(2026, 11, 7), 325116),
	}, domain.NewDate(2026, 10, 15))
	doc := ports.StatementDocument{Statement: st, Business: ref.Business, Currency: ref.Currency, Bank: ref.Invoice.Bank, Disclaimer: "Disclaimer: test"}
	out, err := Renderer{Uncompressed: true}.RenderStatement(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"S T A T E M E N T", "Statement date", "15 October 2026", "STATEMENT FOR", "CURRENT", "31-60 DAYS", "CD/1195/26", "38", "Balance due ZAR", "9,753.48", "PAYMENT DETAILS", "Disclaimer: test"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("statement missing %q", want)
		}
	}
	save(t, "statement.pdf", out)
	empty := doc
	empty.Statement = domain.NewStatement(c, nil, domain.NewDate(2026, 10, 15))
	if out, _ := (Renderer{Uncompressed: true}).RenderStatement(context.Background(), empty); !bytes.Contains(out, []byte("Nothing is outstanding")) {
		t.Error("empty statement")
	}
}

// TestPaymentCardNeverOverlapsTotals guards the draft-message overlap: every text run in
// the payment card must end left of the totals box.
func TestPaymentCardNeverOverlapsTotals(t *testing.T) {
	p := newPage(Renderer{Uncompressed: true}, "t", "a", "INVOICE", domain.BusinessProfile{Name: "B"}, domain.Currency{Code: "ZAR", MinorUnits: 2})
	p.pdf.AddPage()
	cardW := contentW - 90
	totalsX := pageW - margin - 80
	check := func(b domain.BankDetails) {
		p.font("I", 8.5)
		for _, l := range p.wrap("Payment details are added when the invoice is issued.", cardW-8) {
			if margin+4+p.pdf.GetStringWidth(l) > totalsX {
				t.Fatalf("message line %q overflows into the totals", l)
			}
		}
		p.font("B", 8.5)
		for _, l := range p.wrap(b.IBAN, cardW-8-32) {
			if margin+4+32+p.pdf.GetStringWidth(l) > totalsX {
				t.Fatalf("value line %q overflows", l)
			}
		}
		bottom := p.payment(100, margin, cardW, b, "REF")
		if bottom <= 100 {
			t.Fatal("card height")
		}
	}
	check(domain.BankDetails{})
	check(domain.BankDetails{AccountNumber: "1", BankName: strings.Repeat("Very Long Bank Name ", 4), IBAN: "GB29 NWBK 6016 1331 9268 19 EXTRA LONG IDENTIFIER VALUE"})
	if y := p.infoStrip(50, [][2]string{{"Payment terms", "Payment due before 7th of the next month"}, {"A", "x"}, {"B", "y"}, {"C", "z"}, {"D", "w"}, {"E", "v"}}); y < 50+9+2*3.9 {
		t.Fatal("info strip must grow for wrapped values")
	}
}
