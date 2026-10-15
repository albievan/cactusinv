package pdf

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
)

// reference rebuilds example/Inv_no_CD_1195_26.pdf.
func reference(t *testing.T) ports.InvoiceDocument {
	t.Helper()
	c := domain.Customer{
		ID: "c", Code: "CD", Name: "Acme Commercial Spares (Pty) Limited", Attention: "Jane",
		BillingAddress:  "P O BOX 2000,\nGERMISTON,\n1401",
		ShippingName:    "Acme Commercial Spares (Pty) Ltd",
		ShippingAddress: "No. 1 Example Road,\nIndustria Ext. 1,\nJohannesburg.\n2001",
		Currency:        "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsDayOfNextMonth, Value: 7}, Active: true,
		DefaultNotes: "Disclaimer:\nWe accept no responsibility for any 3rd Party software integrated with the stock system that has not been supplied by PrimeSoft. The Alchamex software is a third party package and as such we Primesoft do not accept responsibility for any problems or malfunction due to the Alchamex software being utilized.",
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
	return ports.InvoiceDocument{
		Invoice:  inv,
		Business: domain.BusinessProfile{Name: "Example Software Consulting", AddressLines: "P. O. Box 1000\nNorthgate\n1616", Phone: "011 555 0100"},
		Currency: domain.Currency{Code: "ZAR", Name: "South African Rand", Symbol: "R", MinorUnits: 2},
	}
}

func TestRenderReferenceInvoice(t *testing.T) {
	doc := reference(t)
	out, err := Renderer{Uncompressed: true}.RenderPDF(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
	for _, want := range []string{
		"Example Software Consulting", "Inv. No. CD/1195/26", "21 August 2026",
		"Acme Commercial Spares \\(Pty\\) Limited", "Att: Jane", "Ship To:",
		"Payment due before 7th", "7 September 2026",
		"Stock system usage for 14 concurrent users.", "192.94", "2701.16", "550.00",
		"Capitecbank", "Northgate", "Savings Acc:", "1234567890", "470010",
		"TOTAL DISCOUNT", "0.00", "SUBTOTAL", "3251.16", "TOTAL ZAR", "Disclaimer:",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("PDF is missing %q", want)
		}
	}
	if bytes.Contains(out, []byte("VAT")) {
		t.Error("a 0% invoice must not mention VAT")
	}
	if bytes.Contains(out, []byte("DRAFT")) {
		t.Error("issued invoice must not be watermarked")
	}
	if dir := os.Getenv("CACTUSINV_PDF_OUT"); dir != "" {
		if err := os.WriteFile(dir+"/Inv_no_CD_1195_26.pdf", out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderVariants(t *testing.T) {
	r := Renderer{Uncompressed: true}
	draft := reference(t)
	inv := *draft.Invoice
	inv.Status, inv.Number, inv.Bank = domain.InvoiceDraft, "", domain.BankDetails{}
	draft.Invoice = &inv
	out, err := r.RenderPDF(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DRAFT", "Draft Invoice", "\\(not issued\\)", "Bank details are added when the invoice is issued."} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("draft PDF missing %q", want)
		}
	}

	voided := reference(t)
	_ = voided.Invoice.Void("Duplicate", time.Now())
	out, _ = r.RenderPDF(context.Background(), voided)
	if !bytes.Contains(out, []byte("VOID")) || !bytes.Contains(out, []byte("Voided: Duplicate")) {
		t.Error("void watermark")
	}

	// Many lines, tax and discounts paginate and show the tax row; euro symbol survives cp1252.
	long := reference(t)
	inv2 := *long.Invoice
	inv2.Lines = nil
	for i := range 60 {
		inv2.Lines = append(inv2.Lines, domain.Line{Description: strings.Repeat("Consulting hours for project €", 1+i%3), Quantity: 1500, UnitPrice: 10000, Discount: 1000})
	}
	inv2.TaxRate = 1500
	inv2.Totals = domain.ComputeTotals(inv2.Lines, inv2.TaxRate)
	inv2.Bank.SwiftBIC, inv2.Bank.IBAN, inv2.Bank.AccountName = "CABLZAJJ", "ZA00 0000", "Example"
	long.Invoice = &inv2
	long.Business.TaxNumber, long.Business.Email = "4000000000", "billing@example.com"
	out, err = r.RenderPDF(context.Background(), long)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(out, []byte("/Type /Page\n")); n < 2 {
		t.Errorf("expected several pages, got %d", n)
	}
	for _, want := range []string{"Continued on next page", "VAT 15%", "10%", "SWIFT/BIC:", "CABLZAJJ", "VAT No: 4000000000"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("long PDF missing %q", want)
		}
	}
	compressed, err := Renderer{}.RenderPDF(context.Background(), reference(t))
	if err != nil || len(compressed) >= len(out) {
		t.Errorf("compressed output: %d bytes, %v", len(compressed), err)
	}
	if accountLabel("") != "Account:" {
		t.Error("account label fallback")
	}
}
