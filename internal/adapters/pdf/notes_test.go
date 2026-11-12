package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"slices"
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

// uriTarget is a link annotation's address in an uncompressed, unencrypted PDF.
var uriTarget = regexp.MustCompile(`/URI \(([^)]*)\)`)

// Links in formatted notes, and web addresses typed in plain notes and the disclaimer, are
// clickable in the PDF; anything SafeLink refuses is not.
func TestNotesLinksAreClickable(t *testing.T) {
	notes, err := domain.CleanNotes("notes", `<p>Read <a href="https://example.com/terms">our terms</a> or <a href="mailto:ann@example.com">mail Ann</a>.</p>`+
		`<table><tr><td><a href="https://cell.example">cell link</a></td></tr></table>`)
	if err != nil {
		t.Fatal(err)
	}
	doc := reference(t)
	inv := *doc.Invoice
	inv.Notes = notes
	doc.Invoice = &inv
	doc.Disclaimer = "Support: see www.cactus.example/help. Never javascript:alert(1)"
	out, txt := render(t, doc)
	save(t, "links.pdf", out)
	var got []string
	for _, m := range uriTarget.FindAllSubmatch(out, -1) {
		got = append(got, string(m[1]))
	}
	want := []string{"https://example.com/terms", "mailto:ann@example.com", "https://cell.example", "https://www.cactus.example/help"}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("no clickable link to %s (links: %v)", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("links = %v, want %v", got, want)
	}
	if !strings.Contains(txt, "our terms") || !strings.Contains(txt, "cell link") {
		t.Fatal("the link text is not printed")
	}

	// Plain notes: a typed address is clickable without the editor.
	plain := reference(t)
	pi := *plain.Invoice
	pi.Notes = "Track the delivery at https://track.example/abc123."
	plain.Invoice = &pi
	out, _ = render(t, plain)
	if m := uriTarget.FindAllSubmatch(out, -1); len(m) < 1 || string(m[0][1]) != "https://track.example/abc123" {
		t.Fatalf("plain notes link = %q", m)
	}
}
