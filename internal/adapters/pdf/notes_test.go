package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/domain"
)

// Formatted notes print their text, list markers, table cells and pictures, and a long
// table runs on to the next pages under the watermark (M33).
func TestRichNotes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 120))
	for x := range 300 {
		img.Set(x, x%120, color.RGBA{0, 90, 50, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	rows := `<tr><th>Item</th><th style="text-align:right">Hours</th></tr>`
	for i := range 60 {
		rows += `<tr><td>Support call with a long description that wraps inside its cell ` + strings.Repeat("x", i%5) + `</td><td style="text-align:right">2</td></tr>`
	}
	notes, err := domain.CleanNotes("notes", `<p style="text-align:center"><span style="font-size:14pt;color:#c80000"><b>Thank you</b></span> for your <u>business</u>, <s>old</s> <span style="font-family:serif">serif</span> <span style="font-family:monospace">mono</span> ünïcödé</p>`+
		`<ul><li>first point</li></ul><ol><li>step one</li><li>step two</li></ol>`+
		`<p><img src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(buf.Bytes())+`" width="150"></p>`+
		`<table><tbody>`+rows+`</tbody></table>`)
	if err != nil {
		t.Fatal(err)
	}
	doc := reference(t)
	inv := *doc.Invoice
	inv.Notes, inv.Status = notes, domain.InvoiceVoid
	doc.Invoice = &inv
	r, runs := tracer()
	pdf, err := r.RenderPDF(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	text := textOf(*runs)
	for _, want := range []string{"Thank you", "business", "•", "1.", "2.", "step two", "Item", "Hours", "Support call", "mono", "ünïcödé"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q not printed", want)
		}
	}
	if strings.Contains(text, "could not be printed") || !bytes.Contains(pdf, []byte("/Subtype /Image")) {
		t.Error("the picture is missing")
	}
	pages, marked := map[int]bool{}, map[int]bool{}
	for _, x := range *runs {
		pages[x.page] = true
		if x.text == "VOID" {
			marked[x.page] = true
		}
		if x.y > bodyBottom+0.01 && x.y != footerY {
			t.Errorf("%q printed below the body on page %d (y %.1f)", x.text, x.page, x.y)
		}
	}
	if len(pages) < 2 || len(marked) != len(pages) {
		t.Fatalf("%d pages, %d watermarked", len(pages), len(marked))
	}
}
