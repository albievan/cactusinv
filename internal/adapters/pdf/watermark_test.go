package pdf

import (
	"context"
	"testing"

	"github.com/albievan/cactusinv/internal/domain"
)

// Every page of a voided invoice is marked VOID, also the one the totals and payment
// details run on to (the mark used to be drawn with the line table only).
func TestEveryPageIsWatermarked(t *testing.T) {
	doc := reference(t)
	inv := *doc.Invoice
	for len(inv.Lines) < 20 { // a page of lines: the totals move to a page of their own
		inv.Lines = append(inv.Lines, inv.Lines[0])
	}
	inv.Status, inv.VoidReason = domain.InvoiceVoid, "issued twice"
	doc.Invoice = &inv
	r, runs := tracer()
	if _, err := r.RenderPDF(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	pages, marked := map[int]bool{}, map[int]bool{}
	for _, x := range *runs {
		pages[x.page] = true
		if x.text == "VOID" {
			marked[x.page] = true
		}
	}
	if len(pages) < 2 || len(marked) != len(pages) {
		t.Fatalf("%d pages, %d marked VOID", len(pages), len(marked))
	}
}

// A sample of a scheduled invoice says SAMPLE - DO NOT EMAIL TO CUSTOMER on every page
// and is titled as a sample, never as an invoice with a number (M32).
func TestSampleIsWatermarked(t *testing.T) {
	doc := reference(t)
	inv := *doc.Invoice
	for len(inv.Lines) < 20 {
		inv.Lines = append(inv.Lines, inv.Lines[0])
	}
	inv.Number, inv.Sample = domain.SampleNumber, true
	doc.Invoice = &inv
	r, runs := tracer()
	if _, err := r.RenderPDF(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	pages, marked := map[int]bool{}, map[int]bool{}
	for _, x := range *runs {
		pages[x.page] = true
		if x.text == SampleMark {
			marked[x.page] = true
		}
	}
	if len(pages) < 2 || len(marked) != len(pages) {
		t.Fatalf("%d pages, %d marked as a sample", len(pages), len(marked))
	}
	if invoiceTitle(&inv) == "Invoice SAMPLE" {
		t.Fatal("a sample is not titled as an invoice")
	}
}
