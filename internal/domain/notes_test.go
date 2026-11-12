package domain

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func pngURI(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{200, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// Plain notes stay plain; formatted notes keep what the editor makes and nothing else
// (M33).
func TestCleanNotes(t *testing.T) {
	if got, err := CleanNotes("notes", "Hours for {period}.\nThanks"); err != nil || got != "Hours for {period}.\nThanks" {
		t.Fatalf("plain: %q %v", got, err)
	}
	if _, err := CleanNotes("notes", strings.Repeat("x", PlainNotesMax+1)); !errors.Is(err, ErrValidation) {
		t.Fatalf("plain too long: %v", err)
	}

	in := `<div style="text-align: center"><span style="font-family: &quot;Times New Roman&quot;; font-size: x-large; color: rgb(200, 0, 0);">Big <b>red</b></span> <u>under</u> <s>gone</s></div>` +
		`<div>line one<br>line two</div><ul><li>first</li><li>second</li></ul><ol><li>one</li></ol>` +
		`<table><tbody><tr><th>Item</th><th style="text-align:right">Hours</th></tr><tr><td>Support</td><td>12</td></tr></tbody></table>`
	got, err := CleanNotes("notes", in)
	if err != nil {
		t.Fatal(err)
	}
	want := `<p style="text-align:center"><span style="color:#c80000;font-size:18pt;font-family:serif">Big </span><span style="color:#c80000;font-size:18pt;font-family:serif"><b>red</b></span> <u>under</u> <s>gone</s></p>` +
		`<p>line one<br>line two</p><ul><li>first</li><li>second</li></ul><ol><li>one</li></ol>` +
		`<table><tbody><tr><th><b>Item</b></th><th style="text-align:right"><b>Hours</b></th></tr><tr><td>Support</td><td>12</td></tr></tbody></table>`
	if got != want {
		t.Fatalf("clean:\n got %s\nwant %s", got, want)
	}
	// Canonical form is stable.
	if again, _ := CleanNotes("notes", got); again != got {
		t.Fatalf("not stable:\n%s\n%s", got, again)
	}
	n, _ := ParseNotes(got)
	if n.Blocks[2].Kind != NoteBullet || n.Blocks[4].Kind != NoteNumbered || n.Blocks[4].Number != 1 || n.Blocks[5].Kind != NoteTable {
		t.Fatalf("blocks = %+v", n.Blocks)
	}
	if pt := n.PlainText(); !strings.Contains(pt, "line one\nline two") || !strings.Contains(pt, "Support\t12") {
		t.Fatalf("plain text = %q", pt)
	}
}

func TestCleanNotesRemovesWhatIsNotAllowed(t *testing.T) {
	in := `<div onclick="alert(1)" class="x">Hi <script>alert(2)</script><a href="javascript:alert(3)">link</a>` +
		`<img src="https://evil.example/t.png"><img src="x" onerror="alert(4)"><iframe src="https://evil.example"></iframe>` +
		`<span style="background:url(javascript:alert(5)); color: expression(alert(6))">styled</span><style>p{}</style></div>`
	got, err := CleanNotes("notes", in)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<p>Hi linkstyled</p>" {
		t.Fatalf("got %s", got)
	}
	for _, bad := range []string{"script", "alert", "onclick", "href", "evil", "iframe", "class", "url("} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q survived: %s", bad, got)
		}
	}
	if got, _ := CleanNotes("notes", "<p>   </p><div><br></div>"); got != "" {
		t.Fatalf("an emptied editor is no notes: %q", got)
	}
	if got, _ := CleanNotes("notes", "<p>a &lt;b&gt; &amp; c</p>"); got != "<p>a &lt;b&gt; &amp; c</p>" {
		t.Fatalf("escaping: %s", got)
	}
}

func TestNotesPictures(t *testing.T) {
	uri := pngURI(t, 40, 20)
	got, err := CleanNotes("notes", `<div style="text-align:right"><img src="`+uri+`" width="20" onload="x()"></div>`)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := ParseNotes(got)
	if len(n.Blocks) != 1 || n.Blocks[0].Kind != NoteImage || n.Blocks[0].Align != "right" {
		t.Fatalf("blocks = %+v", n.Blocks)
	}
	if pic := n.Blocks[0].Picture; pic.Format != "png" || pic.Width != 40 || pic.Height != 20 || pic.Shown != 20 {
		t.Fatalf("picture = %+v", pic)
	}
	if strings.Contains(got, "onload") {
		t.Fatal(got)
	}
	bad := map[string]string{
		"not an image":  "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("hello")),
		"svg":           "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",
		"too big":       "data:image/png;base64," + strings.Repeat("A", NoteImageMax*4/3+100),
		"broken base64": "data:image/png;base64,@@@",
	}
	for name, src := range bad {
		if _, err := CleanNotes("notes", `<p><img src="`+src+`"></p>`); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A schedule and an invoice clean their notes when saved.
	s := cityDeepSchedule()
	s.Notes = `<p onclick="x()">Hours for {period}</p>`
	if err := s.Validate(); err != nil || s.Notes != "<p>Hours for {period}</p>" {
		t.Fatalf("schedule notes: %q %v", s.Notes, err)
	}
	inv := Invoice{Status: InvoiceIssued}
	if err := inv.AmendNotes(`<p>ok<script>x</script></p>`, s.CreatedAt); err != nil || inv.Notes != "<p>ok</p>" {
		t.Fatalf("amended: %q %v", inv.Notes, err)
	}
}

func TestNotesStyleParsing(t *testing.T) {
	cases := map[string]NoteStyle{
		`<font face="Courier New" size="5" color="#00f">x</font>`:                       {Font: NoteMono, Size: 18, Color: &RGB{0, 0, 255}},
		`<span style="font-size:20px;font-weight:700;font-style:italic">x</span>`:       {Size: 15, Bold: true, Italic: true},
		`<span style="font-size:200pt;text-decoration:underline line-through">x</span>`: {Size: noteMaxSize, Underline: true, Strike: true},
		`<span style="font-family:Georgia;color:navy">x</span>`:                         {Font: NoteSerif, Color: &RGB{0, 0, 128}},
	}
	for in, want := range cases {
		n, err := ParseNotes(in)
		if err != nil || len(n.Blocks) != 1 || len(n.Blocks[0].Runs) != 1 {
			t.Fatalf("%s: %+v %v", in, n, err)
		}
		got := n.Blocks[0].Runs[0].Style
		if (got.Color == nil) != (want.Color == nil) || (got.Color != nil && *got.Color != *want.Color) {
			t.Errorf("%s: colour %v, want %v", in, got.Color, want.Color)
		}
		got.Color, want.Color = nil, nil
		if got != want {
			t.Errorf("%s: %+v, want %+v", in, got, want)
		}
	}
}
