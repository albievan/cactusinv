package domain

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg" // pictures in notes are PNG or JPEG: DecodeConfig needs both decoders
	_ "image/png"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Invoice notes are plain text, as they always were, or formatted (M33): HTML written by
// the web app's notes editor. Formatted notes are read into a small model under an
// allowlist (ParseNotes) and stored only in the canonical form CleanNotes writes back, so
// what is stored, shown and printed is the same: paragraphs and list items of styled text,
// tables, pictures and web links. Scripts, event handlers, classes and outside images never
// survive; a picture is a PNG or JPEG data: URI, and a link goes to an http, https or
// mailto address (SafeLink) and is clickable in the PDF.

// Notes limits.
const (
	PlainNotesMax   = 4000    // characters of plain-text notes
	NotesMaxBytes   = 800_000 // formatted notes, pictures included (a request body is at most 1 MiB)
	NoteImageMax    = 500_000 // bytes of one picture
	noteTableMaxRow = 200
	noteTableMaxCol = 20
	noteMinSize     = 6.0
	noteMaxSize     = 48.0
)

// NoteFont is a notes font family: the PDF has one of each.
type NoteFont string

// Note fonts ("" is the default, sans).
const (
	NoteSans  NoteFont = "sans"
	NoteSerif NoteFont = "serif"
	NoteMono  NoteFont = "mono"
)

// RGB is a colour.
type RGB struct{ R, G, B uint8 }

// NoteStyle is how a run of text is set. Zero values mean the default.
type NoteStyle struct {
	Bold, Italic, Underline, Strike bool
	Color                           *RGB
	Size                            float64 // points; 0 = the default size
	Font                            NoteFont
	// Link is the address the text links to (http, https or mailto; see SafeLink); "" for
	// text that is not a link.
	Link string
}

// NoteRun is styled text, or a line break.
type NoteRun struct {
	Text  string
	Style NoteStyle
	Break bool
}

// NoteBlockKind is what a block of notes is.
type NoteBlockKind string

// Block kinds.
const (
	NoteParagraph NoteBlockKind = "p"
	NoteBullet    NoteBlockKind = "bullet"
	NoteNumbered  NoteBlockKind = "number"
	NoteTable     NoteBlockKind = "table"
	NoteImage     NoteBlockKind = "img"
)

// NoteCell is a table cell.
type NoteCell struct {
	Header bool
	Align  string
	Runs   []NoteRun
}

// NotePicture is a picture in the notes.
type NotePicture struct {
	Format        string // png or jpeg
	Data          []byte
	Width, Height int // pixels, as stored
	Shown         int // pixels wide it is shown at (≤ Width); 0 = Width
}

// NoteBlock is a paragraph, list item, table or picture.
type NoteBlock struct {
	Kind    NoteBlockKind
	Align   string // left (""), center or right
	Runs    []NoteRun
	Number  int // numbered list items: 1, 2, …
	Rows    [][]NoteCell
	Picture *NotePicture
}

// Notes is formatted notes.
type Notes struct{ Blocks []NoteBlock }

// IsRichNotes reports whether notes are formatted (HTML) rather than plain text.
func IsRichNotes(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "<") }

// CleanNotes checks notes and returns them as they are stored: plain text unchanged
// (at most PlainNotesMax characters), formatted notes in canonical form.
func CleanNotes(field, s string) (string, error) {
	if !IsRichNotes(s) {
		if len(s) > PlainNotesMax {
			return "", invalid(field, fmt.Sprintf("max %d characters", PlainNotesMax))
		}
		return s, nil
	}
	if len(s) > 2*NotesMaxBytes {
		return "", invalid(field, "the notes are too large (pictures included, at most 800 kB)")
	}
	n, err := ParseNotes(s)
	if err != nil {
		return "", invalid(field, err.Error())
	}
	out := n.HTML()
	if len(out) > NotesMaxBytes {
		return "", invalid(field, "the notes are too large (pictures included, at most 800 kB)")
	}
	if strings.TrimSpace(n.PlainText()) == "" && !n.hasPicture() && !n.hasTable() {
		return "", nil // an emptied editor: no notes
	}
	return out, nil
}

// ParseNotes reads formatted notes. Anything outside the allowlist is dropped (its text
// kept); a picture that is not a PNG or JPEG of at most NoteImageMax bytes is an error.
func ParseNotes(s string) (Notes, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		return Notes{}, fmt.Errorf("unreadable notes")
	}
	p := &notesParser{}
	for _, n := range nodes {
		if err := p.node(n, NoteStyle{}, ""); err != nil {
			return Notes{}, err
		}
	}
	p.flush()
	return Notes{Blocks: p.blocks}, nil
}

type notesParser struct {
	blocks []NoteBlock
	cur    *NoteBlock // the paragraph or list item being filled
	list   []listCtx
}

type listCtx struct {
	numbered bool
	n        int
}

// dropped elements lose their content too.
var noteDropped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true, atom.Object: true, atom.Embed: true,
	atom.Svg: true, atom.Math: true, atom.Template: true, atom.Noscript: true, atom.Head: true,
	atom.Title: true, atom.Select: true, atom.Textarea: true, atom.Button: true, atom.Input: true,
	atom.Video: true, atom.Audio: true, atom.Canvas: true, atom.Form: true,
}

var noteBlockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Blockquote: true, atom.Pre: true, atom.Section: true,
	atom.Article: true, atom.Header: true, atom.Footer: true, atom.Hr: true,
}

func (p *notesParser) flush() {
	if p.cur == nil {
		return
	}
	b := *p.cur
	p.cur = nil
	b.Runs = trimRuns(b.Runs)
	p.blocks = append(p.blocks, b)
}

func (p *notesParser) open(kind NoteBlockKind, align string) {
	p.flush()
	p.cur = &NoteBlock{Kind: kind, Align: align}
}

func (p *notesParser) para(align string) *NoteBlock {
	if p.cur == nil {
		p.cur = &NoteBlock{Kind: NoteParagraph, Align: align}
	}
	return p.cur
}

func (p *notesParser) node(n *html.Node, st NoteStyle, align string) error {
	switch n.Type {
	case html.TextNode:
		if t := collapseSpace(n.Data); t != "" {
			b := p.para(align)
			b.Runs = append(b.Runs, NoteRun{Text: t, Style: st})
		}
		return nil
	case html.ElementNode:
	default:
		return nil
	}
	if noteDropped[n.DataAtom] {
		return nil
	}
	st, a := styleOf(n, st)
	if a != "" {
		align = a
	}
	switch n.DataAtom {
	case atom.Br:
		b := p.para(align)
		b.Runs = append(b.Runs, NoteRun{Break: true})
		return nil
	case atom.Img:
		pic, err := pictureOf(n)
		if err != nil || pic == nil {
			return err
		}
		p.flush()
		p.blocks = append(p.blocks, NoteBlock{Kind: NoteImage, Align: align, Picture: pic})
		return nil
	case atom.Table:
		p.flush()
		rows := tableRows(n, st)
		if len(rows) > 0 {
			p.blocks = append(p.blocks, NoteBlock{Kind: NoteTable, Rows: rows})
		}
		return nil
	case atom.Ul, atom.Ol:
		p.flush()
		p.list = append(p.list, listCtx{numbered: n.DataAtom == atom.Ol})
		err := p.children(n, st, align)
		p.flush()
		p.list = p.list[:len(p.list)-1]
		return err
	case atom.Li:
		kind, num := NoteBullet, 0
		if l := len(p.list); l > 0 {
			if p.list[l-1].numbered {
				p.list[l-1].n++
				kind, num = NoteNumbered, p.list[l-1].n
			}
		}
		p.open(kind, align)
		p.cur.Number = num
		err := p.children(n, st, align)
		p.flush()
		return err
	}
	if noteBlockTags[n.DataAtom] {
		if isHeading(n.DataAtom) {
			st.Bold = true
		}
		p.flush()
		err := p.children(n, st, align)
		p.flush()
		return err
	}
	return p.children(n, st, align)
}

func isHeading(a atom.Atom) bool {
	switch a {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return true
	}
	return false
}

func (p *notesParser) children(n *html.Node, st NoteStyle, align string) error {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := p.node(c, st, align); err != nil {
			return err
		}
	}
	return nil
}

// tableRows reads a table's rows (thead, tbody, tfoot or directly), each cell's text with
// its styles; anything else in a cell becomes text with line breaks.
func tableRows(t *html.Node, st NoteStyle) [][]NoteCell {
	var rows [][]NoteCell
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Thead, atom.Tbody, atom.Tfoot:
				walk(c)
			case atom.Tr:
				if len(rows) >= noteTableMaxRow {
					return
				}
				var row []NoteCell
				for d := c.FirstChild; d != nil; d = d.NextSibling {
					if d.Type != html.ElementNode || (d.DataAtom != atom.Td && d.DataAtom != atom.Th) || len(row) >= noteTableMaxCol {
						continue
					}
					cs, align := styleOf(d, st)
					if d.DataAtom == atom.Th {
						cs.Bold = true
					}
					cell := NoteCell{Header: d.DataAtom == atom.Th, Align: align}
					cp := &notesParser{}
					_ = cp.children(d, cs, "")
					cp.flush()
					for i, b := range cp.blocks {
						if b.Kind == NoteTable || b.Kind == NoteImage {
							continue
						}
						if i > 0 && len(cell.Runs) > 0 {
							cell.Runs = append(cell.Runs, NoteRun{Break: true})
						}
						cell.Runs = append(cell.Runs, b.Runs...)
					}
					row = append(row, cell)
				}
				if len(row) > 0 {
					rows = append(rows, row)
				}
			}
		}
	}
	walk(t)
	return rows
}

var dataURI = regexp.MustCompile(`^data:image/(png|jpeg|jpg);base64,([A-Za-z0-9+/=\s]+)$`)

// A picture's size in pixels is bounded as well as its bytes: a small, highly compressed
// file can unpack to gigabytes when it is drawn.
const (
	NoteImageMaxSide   = 5000
	NoteImageMaxPixels = 16_000_000
)

// NotePictureFits reports whether a picture of w×h pixels is within the bounds.
func NotePictureFits(w, h int) bool {
	return w > 0 && h > 0 && w <= NoteImageMaxSide && h <= NoteImageMaxSide && w*h <= NoteImageMaxPixels
}

// pictureOf reads an img: nil for one that is not a data: URI (dropped), an error for a
// data: URI that is not a PNG or JPEG within NoteImageMax.
func pictureOf(n *html.Node) (*NotePicture, error) {
	src := strings.TrimSpace(attr(n, "src"))
	if !strings.HasPrefix(src, "data:") {
		return nil, nil
	}
	m := dataURI.FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("a picture must be a PNG or JPEG")
	}
	raw := strings.Join(strings.Fields(m[2]), "")
	if base64.StdEncoding.DecodedLen(len(raw)) > NoteImageMax+3 {
		return nil, fmt.Errorf("a picture may be at most 500 kB")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("a picture could not be read")
	}
	if len(data) > NoteImageMax {
		return nil, fmt.Errorf("a picture may be at most 500 kB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("a picture must be a PNG or JPEG")
	}
	if !NotePictureFits(cfg.Width, cfg.Height) {
		return nil, fmt.Errorf("a picture may be at most %d pixels a side and %d megapixels", NoteImageMaxSide, NoteImageMaxPixels/1_000_000)
	}
	pic := &NotePicture{Format: format, Data: data, Width: cfg.Width, Height: cfg.Height}
	shown := atoiPx(attr(n, "width"))
	if w := cssDecl(attr(n, "style"))["width"]; w != "" {
		shown = atoiPx(w)
	}
	if shown > 0 && shown < cfg.Width {
		pic.Shown = shown
	}
	return pic, nil
}

func atoiPx(s string) int {
	s = strings.TrimSuffix(strings.TrimSpace(s), "px")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 || f > 10000 {
		return 0
	}
	return int(f)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// styleOf applies an element's own formatting (its tag, a font element's attributes and
// its style attribute) to st, and returns its text alignment if it sets one.
func styleOf(n *html.Node, st NoteStyle) (NoteStyle, string) {
	switch n.DataAtom {
	case atom.B, atom.Strong:
		st.Bold = true
	case atom.I, atom.Em:
		st.Italic = true
	case atom.U, atom.Ins:
		st.Underline = true
	case atom.S, atom.Strike, atom.Del:
		st.Strike = true
	case atom.A:
		// A link to anything but a web or e-mail address keeps its text, not the link.
		if u, ok := SafeLink(attr(n, "href")); ok {
			st.Link = u
		}
	case atom.Font:
		if c, ok := parseColor(attr(n, "color")); ok {
			st.Color = &c
		}
		if f := attr(n, "face"); f != "" {
			st.Font = fontOf(f)
		}
		if sz, err := strconv.Atoi(strings.TrimSpace(attr(n, "size"))); err == nil && sz >= 1 && sz <= 7 {
			st.Size = []float64{7.5, 10, 12, 13.5, 18, 24, 36}[sz-1]
		}
	}
	align := ""
	if a := strings.ToLower(strings.TrimSpace(attr(n, "align"))); a == "center" || a == "right" || a == "left" {
		align = a
	}
	for k, v := range cssDecl(attr(n, "style")) {
		switch k {
		case "color":
			if c, ok := parseColor(v); ok {
				st.Color = &c
			}
		case "font-size":
			if sz := parseSize(v); sz > 0 {
				st.Size = sz
			}
		case "font-family":
			st.Font = fontOf(v)
		case "font-weight":
			st.Bold = v == "bold" || v == "bolder" || (len(v) == 3 && v >= "600")
			if v == "normal" {
				st.Bold = false
			}
		case "font-style":
			st.Italic = v == "italic" || v == "oblique"
		case "text-decoration", "text-decoration-line":
			if strings.Contains(v, "underline") {
				st.Underline = true
			}
			if strings.Contains(v, "line-through") {
				st.Strike = true
			}
			if v == "none" {
				st.Underline, st.Strike = false, false
			}
		case "text-align":
			if v == "center" || v == "right" || v == "left" {
				align = v
			}
		}
	}
	if align == "left" {
		align = ""
	}
	return st, align
}

func cssDecl(style string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(style, ";") {
		k, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important")))
		out[k] = strings.TrimSpace(v)
	}
	return out
}

var namedColors = map[string]RGB{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "red": {255, 0, 0}, "green": {0, 128, 0},
	"blue": {0, 0, 255}, "gray": {128, 128, 128}, "grey": {128, 128, 128}, "orange": {255, 165, 0},
	"purple": {128, 0, 128}, "navy": {0, 0, 128}, "maroon": {128, 0, 0}, "teal": {0, 128, 128},
}

var rgbFunc = regexp.MustCompile(`^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(?:,\s*[\d.]+\s*)?\)$`)

func parseColor(v string) (RGB, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if c, ok := namedColors[v]; ok {
		return c, true
	}
	if m := rgbFunc.FindStringSubmatch(v); m != nil {
		return RGB{channel(m[1], 10), channel(m[2], 10), channel(m[3], 10)}, true
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if _, err := strconv.ParseUint(h, 16, 32); err == nil && len(h) == 6 {
			return RGB{channel(h[0:2], 16), channel(h[2:4], 16), channel(h[4:6], 16)}, true
		}
	}
	return RGB{}, false
}

// channel reads one colour channel; a decimal one above 255 is 255.
func channel(s string, base int) uint8 {
	v, err := strconv.ParseUint(s, base, 8)
	if err != nil {
		return 255
	}
	return uint8(v)
}

var sizeKeywords = map[string]float64{
	"xx-small": 7, "x-small": 7.5, "small": 10, "medium": 12, "large": 13.5, "x-large": 18, "xx-large": 24, "xxx-large": 36,
}

func parseSize(v string) float64 {
	if s, ok := sizeKeywords[v]; ok {
		return s
	}
	unit := 1.0
	switch {
	case strings.HasSuffix(v, "pt"):
		v = strings.TrimSuffix(v, "pt")
	case strings.HasSuffix(v, "px"):
		v, unit = strings.TrimSuffix(v, "px"), 0.75
	default:
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) {
		return 0
	}
	return math.Round(min(max(f*unit, noteMinSize), noteMaxSize)*2) / 2
}

func fontOf(family string) NoteFont {
	f := strings.ToLower(family)
	switch {
	case strings.Contains(f, "mono") || strings.Contains(f, "courier") || strings.Contains(f, "consolas"):
		return NoteMono
	case strings.Contains(f, "times") || strings.Contains(f, "georgia") || (strings.Contains(f, "serif") && !strings.Contains(f, "sans")):
		return NoteSerif
	default:
		return NoteSans
	}
}

var spaces = regexp.MustCompile(`[ \t\r\n\f]+`)

func collapseSpace(s string) string { return spaces.ReplaceAllString(s, " ") }

// trimRuns drops spaces at the start and end of a block and around line breaks.
func trimRuns(rs []NoteRun) []NoteRun {
	out := rs[:0:0]
	for _, r := range rs {
		if !r.Break && r.Text == "" {
			continue
		}
		out = append(out, r)
	}
	for i := range out {
		if out[i].Break {
			continue
		}
		if i == 0 || out[i-1].Break || strings.HasSuffix(out[i-1].Text, " ") {
			out[i].Text = strings.TrimLeft(out[i].Text, " ")
		}
		if i == len(out)-1 || out[i+1].Break {
			out[i].Text = strings.TrimRight(out[i].Text, " ")
		}
	}
	final := out[:0]
	for _, r := range out {
		if r.Break || r.Text != "" {
			final = append(final, r)
		}
	}
	// A trailing break only marks an empty line the editor keeps: not printed.
	for len(final) > 0 && final[len(final)-1].Break && len(final) > 1 {
		final = final[:len(final)-1]
	}
	return final
}

// PlainText is the notes' text without formatting, a line per paragraph.
func (n Notes) PlainText() string {
	var b strings.Builder
	runs := func(rs []NoteRun) {
		for _, r := range rs {
			if r.Break {
				b.WriteByte('\n')
			} else {
				b.WriteString(r.Text)
			}
		}
	}
	for i, bl := range n.Blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch bl.Kind {
		case NoteTable:
			for _, row := range bl.Rows {
				for j, c := range row {
					if j > 0 {
						b.WriteByte('\t')
					}
					runs(c.Runs)
				}
				b.WriteByte('\n')
			}
		case NoteImage:
		default:
			runs(bl.Runs)
		}
	}
	return b.String()
}

func (n Notes) hasPicture() bool {
	for _, b := range n.Blocks {
		if b.Kind == NoteImage {
			return true
		}
	}
	return false
}

func (n Notes) hasTable() bool {
	for _, b := range n.Blocks {
		if b.Kind == NoteTable {
			return true
		}
	}
	return false
}

// HTML writes the notes in canonical form.
func (n Notes) HTML() string {
	var b strings.Builder
	list := NoteBlockKind("")
	closeList := func() {
		switch list {
		case NoteBullet:
			b.WriteString("</ul>")
		case NoteNumbered:
			b.WriteString("</ol>")
		}
		list = ""
	}
	for _, bl := range n.Blocks {
		if bl.Kind != list {
			closeList()
		}
		switch bl.Kind {
		case NoteBullet, NoteNumbered:
			if list == "" {
				if bl.Kind == NoteBullet {
					b.WriteString("<ul>")
				} else {
					b.WriteString("<ol>")
				}
				list = bl.Kind
			}
			b.WriteString("<li" + alignAttr(bl.Align) + ">")
			writeRuns(&b, bl.Runs)
			b.WriteString("</li>")
		case NoteTable:
			b.WriteString("<table><tbody>")
			for _, row := range bl.Rows {
				b.WriteString("<tr>")
				for _, c := range row {
					tag := "td"
					if c.Header {
						tag = "th"
					}
					b.WriteString("<" + tag + alignAttr(c.Align) + ">")
					writeRuns(&b, c.Runs)
					b.WriteString("</" + tag + ">")
				}
				b.WriteString("</tr>")
			}
			b.WriteString("</tbody></table>")
		case NoteImage:
			pic := bl.Picture
			b.WriteString("<p" + alignAttr(bl.Align) + `><img src="data:image/` + pic.Format + ";base64," + base64.StdEncoding.EncodeToString(pic.Data) + `"`)
			if pic.Shown > 0 {
				b.WriteString(` width="` + strconv.Itoa(pic.Shown) + `"`)
			}
			b.WriteString("></p>")
		default:
			b.WriteString("<p" + alignAttr(bl.Align) + ">")
			if len(bl.Runs) == 0 {
				b.WriteString("<br>")
			}
			writeRuns(&b, bl.Runs)
			b.WriteString("</p>")
		}
	}
	closeList()
	return b.String()
}

func alignAttr(a string) string {
	if a == "" {
		return ""
	}
	return ` style="text-align:` + a + `"`
}

func writeRuns(b *strings.Builder, rs []NoteRun) {
	for _, r := range rs {
		if r.Break {
			b.WriteString("<br>")
			continue
		}
		var css []string
		st := r.Style
		if st.Color != nil {
			css = append(css, fmt.Sprintf("color:#%02x%02x%02x", st.Color.R, st.Color.G, st.Color.B))
		}
		if st.Size > 0 {
			css = append(css, "font-size:"+strconv.FormatFloat(st.Size, 'f', -1, 64)+"pt")
		}
		if st.Font != "" && st.Font != NoteSans {
			fam := map[NoteFont]string{NoteSerif: "serif", NoteMono: "monospace"}[st.Font]
			css = append(css, "font-family:"+fam)
		}
		var open, closing string
		if len(css) > 0 {
			open, closing = `<span style="`+strings.Join(css, ";")+`">`, "</span>"
		}
		for _, t := range []struct {
			on  bool
			tag string
		}{{st.Bold, "b"}, {st.Italic, "i"}, {st.Underline, "u"}, {st.Strike, "s"}} {
			if t.on {
				open += "<" + t.tag + ">"
				closing = "</" + t.tag + ">" + closing
			}
		}
		if st.Link != "" {
			open, closing = `<a href="`+html.EscapeString(st.Link)+`">`+open, closing+"</a>"
		}
		b.WriteString(open + html.EscapeString(r.Text) + closing)
	}
}

// MaxLinkLength is the longest link address kept in notes.
const MaxLinkLength = 2000

// SafeLink checks a link address for notes and returns it in canonical form: an absolute
// http or https address with a host, or a mailto address; "www.example.com" means https.
// Anything else (javascript:, data:, file:, relative paths, addresses with spaces or
// control characters) is refused, so a link in the notes, the app or the PDF can only open
// a web page or a new e-mail.
func SafeLink(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > MaxLinkLength || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(s), "www.") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" || u.User != nil || strings.HasPrefix(u.Host, ".") {
			return "", false
		}
	case "mailto":
		if u.Opaque == "" || strings.ContainsAny(u.Opaque, "/\\") {
			return "", false
		}
	default:
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	return u.String(), true
}
