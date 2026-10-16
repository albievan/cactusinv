package pdf

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/domain"
)

// Nothing is ever drawn below the page or into the footer, however long a line's
// description or the notes are, and none of their text is lost.
func TestLongContentStaysOnThePage(t *testing.T) {
	doc := reference(t)
	inv := *doc.Invoice
	var desc, notes []string
	for i := range 70 {
		desc = append(desc, "Description line "+strconv.Itoa(i))
	}
	for i := range 90 {
		notes = append(notes, "Note line "+strconv.Itoa(i))
	}
	inv.Lines = []domain.Line{{Description: strings.Join(desc, "\n"), Quantity: 1000, UnitPrice: 100}, {Description: "Second", Quantity: 1000, UnitPrice: 100}}
	inv.Totals = domain.ComputeTotals(inv.Lines, inv.TaxRate)
	inv.Notes = strings.Join(notes, "\n")
	doc.Invoice = &inv
	out, txt := render(t, doc)
	r, runs := tracer()
	if _, err := r.RenderPDF(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	for _, x := range *runs {
		if x.y < 5 || x.y > pageH-5 {
			t.Fatalf("%q drawn off the page at y=%.1f (page %d)", x.text, x.y, x.page)
		}
		if strings.HasPrefix(x.text, "Description line") || strings.HasPrefix(x.text, "Note line") {
			if x.y > bodyBottom+0.01 {
				t.Fatalf("%q drawn into the footer at y=%.1f", x.text, x.y)
			}
		}
	}
	for _, want := range []string{"Description line 0", "Description line 69", "Second", "Note line 0", "Note line 89"} {
		if !strings.Contains(txt, want+"\n") {
			t.Errorf("missing %q", want)
		}
	}
	if n := bytes.Count(out, []byte("/Type /Page\n")); n < 3 {
		t.Errorf("expected the content to run over several pages, got %d", n)
	}
}

// Names in other scripts print as typed (an embedded UTF-8 font), not as dots.
func TestNonLatinTextIsKept(t *testing.T) {
	doc := reference(t)
	inv := *doc.Invoice
	inv.BillTo.Name = "Zażółć Łódź Москва Ελλάδα"
	doc.Invoice = &inv
	out, txt := render(t, doc)
	if !strings.Contains(txt, "Zażółć Łódź Москва Ελλάδα") {
		t.Fatal("the name was changed")
	}
	if !bytes.Contains(out, []byte("/BaseFont /utf8dejavu")) || !bytes.Contains(out, []byte("/FontFile2")) {
		t.Fatal("the UTF-8 font is not embedded")
	}
}
