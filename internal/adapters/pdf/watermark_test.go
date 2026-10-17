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
