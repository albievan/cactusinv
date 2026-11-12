package pdf

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

// Regression tests for the v1.19.0 code review (M36): notes and figures that do not fit.

// One table cell taller than a page carries on over the next pages instead of running off
// the bottom, and a very long unbroken word in a narrow cell is cut quickly and on whole
// characters.
func TestNotesThatDoNotFit(t *testing.T) {
	tall := strings.Repeat("line of a very tall cell<br>", 200)
	word := strings.Repeat("€", 20_000)
	notes, err := domain.CleanNotes("notes", `<table><tbody><tr><td>`+tall+`</td><td>side</td></tr></tbody></table>`+
		`<table><tbody><tr>`+strings.Repeat(`<td>x</td>`, 19)+`<td><span style="font-size:48pt">`+word+`</span></td></tr></tbody></table>`)
	if err != nil {
		t.Fatal(err)
	}
	doc := reference(t)
	inv := *doc.Invoice
	inv.Notes = notes
	doc.Invoice = &inv
	r, runs := tracer()
	start := time.Now()
	if _, err := r.RenderPDF(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("rendering took %v", d)
	}
	lines, pages := 0, map[int]bool{}
	for _, x := range *runs {
		pages[x.page] = true
		if x.text == "line of a very tall cell" {
			lines++
		}
		if x.y > bodyBottom+0.01 && x.y != footerY {
			t.Fatalf("%q printed below the body on page %d (y %.1f)", x.text, x.page, x.y)
		}
		if strings.ContainsRune(x.text, '�') {
			t.Fatalf("a character was cut in two: %q", x.text)
		}
	}
	if lines != 200 || len(pages) < 3 {
		t.Fatalf("%d of 200 lines on %d pages", lines, len(pages))
	}
}

// A picture stored before pictures were bounded in pixels is left out, not decoded.
func TestHugePictureIsNotDecoded(t *testing.T) {
	p := newPage(Renderer{}, "t", "b", "INVOICE", domain.BusinessProfile{}, domain.Currency{Code: "ZAR", MinorUnits: 2})
	// 36 megapixels of one grey: a small file the old code decoded (and copied) whole.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 6000, 6000))); err != nil {
		t.Fatal(err)
	}
	pic := &domain.NotePicture{Format: "png", Data: buf.Bytes(), Width: 6000, Height: 6000}
	p.notePicture(20, domain.NoteBlock{Picture: pic})
	if p.pics != 0 {
		t.Fatal("a 36-megapixel picture was decoded")
	}
	// The header alone says it is too big: refused before anything is decoded.
	if domain.NotePictureFits(60000, 60000) || !domain.NotePictureFits(4000, 4000) {
		t.Fatal("NotePictureFits")
	}
}
