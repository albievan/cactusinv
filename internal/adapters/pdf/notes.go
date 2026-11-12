package pdf

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"

	"github.com/albievan/cactusinv/internal/domain"
)

// Formatted notes (M33) are laid out here from domain.Notes: paragraphs and list items of
// styled runs wrapped word by word, tables with borders, pictures. Sans is the embedded
// DejaVu; Serif and Mono are the PDF core fonts Times and Courier (cp1252, so text in other
// scripts belongs in Sans). Page breaks work as for plain notes.

const (
	noteSize   = 8.0 // the default size of notes, as plain notes
	noteLineH  = 3.6 // the line height at noteSize
	noteIndent = 5.0 // list items
	cellPad    = 1.5 // table cells
	pxToMM     = 25.4 / 96
	maxPicH    = 120.0
)

// noteSeg is a piece of a laid-out line in one style.
type noteSeg struct {
	text string
	st   domain.NoteStyle
	w    float64
}

type noteLine struct {
	segs []noteSeg
	w    float64
	size float64 // the largest size on the line
}

func (l noteLine) height() float64 { return max(noteLineH, l.size*0.45) }

func sizeOf(st domain.NoteStyle) float64 {
	if st.Size > 0 {
		return st.Size
	}
	return noteSize
}

// setNoteFont selects st's font and returns the text as that font needs it.
func (p *page) setNoteFont(st domain.NoteStyle) func(string) string {
	style := ""
	if st.Bold {
		style += "B"
	}
	if st.Italic {
		style += "I"
	}
	switch st.Font {
	case domain.NoteSerif, domain.NoteMono:
		fam := "times"
		if st.Font == domain.NoteMono {
			fam = "courier"
		}
		p.pdf.SetFont(fam, style, sizeOf(st))
		if p.cp1252 == nil {
			p.cp1252 = p.pdf.UnicodeTranslatorFromDescriptor("")
		}
		return p.cp1252
	default:
		p.pdf.SetFont(fontFamily, style, sizeOf(st))
		return func(s string) string { return s }
	}
}

// layout wraps runs into lines at most w wide.
func (p *page) layout(runs []domain.NoteRun, w float64) []noteLine {
	var lines []noteLine
	cur := noteLine{size: noteSize}
	push := func() {
		lines = append(lines, cur)
		cur = noteLine{size: noteSize}
	}
	add := func(text string, st domain.NoteStyle) {
		tr := p.setNoteFont(st)
		sw := p.pdf.GetStringWidth(tr(text))
		if n := len(cur.segs); n > 0 && cur.segs[n-1].st == st {
			cur.segs[n-1].text += text
			cur.segs[n-1].w += sw
		} else {
			cur.segs = append(cur.segs, noteSeg{text: text, st: st, w: sw})
		}
		cur.w += sw
		cur.size = max(cur.size, sizeOf(st))
	}
	width := func(text string, st domain.NoteStyle) float64 {
		tr := p.setNoteFont(st)
		return p.pdf.GetStringWidth(tr(text))
	}
	for _, r := range runs {
		if r.Break {
			push()
			continue
		}
		for _, word := range splitWords(r.Text) {
			ww := width(word, r.Style)
			if cur.w+ww <= w || (len(cur.segs) == 0 && ww <= w) {
				if len(cur.segs) == 0 && strings.TrimSpace(word) == "" {
					continue // no space at the start of a line
				}
				add(word, r.Style)
				continue
			}
			if strings.TrimSpace(word) == "" {
				push() // the space the line broke at
				continue
			}
			if len(cur.segs) > 0 {
				push()
			}
			// A word wider than the line is cut where it fills it (at least one whole
			// character a line). Widths add up character by character, so each character
			// is measured once: a very long word costs no more than its length.
			for ww > w {
				_, cut := utf8.DecodeRuneInString(word)
				cw := width(word[:cut], r.Style)
				for cut < len(word) {
					_, size := utf8.DecodeRuneInString(word[cut:])
					rw := width(word[cut:cut+size], r.Style)
					if cw+rw > w {
						break
					}
					cw += rw
					cut += size
				}
				add(word[:cut], r.Style)
				push()
				word = word[cut:]
				ww -= cw
			}
			if word != "" {
				add(word, r.Style)
			}
		}
	}
	if len(cur.segs) > 0 || len(lines) == 0 {
		push()
	}
	// Spaces at the end of a line are not measured into its alignment.
	for i := range lines {
		if n := len(lines[i].segs); n > 0 {
			last := &lines[i].segs[n-1]
			if t := strings.TrimRight(last.text, " "); t != last.text {
				tr := p.setNoteFont(last.st)
				nw := p.pdf.GetStringWidth(tr(t))
				lines[i].w -= last.w - nw
				last.text, last.w = t, nw
			}
		}
	}
	return lines
}

// splitWords splits text into words and the spaces between them.
func splitWords(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if i > start {
				out = append(out, s[start:i])
			}
			out = append(out, " ")
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// drawLine draws a laid-out line with its baseline at y, starting at x.
func (p *page) drawLine(l noteLine, x, y float64) {
	for _, s := range l.segs {
		tr := p.setNoteFont(s.st)
		c := muted
		if s.st.Link != "" {
			c = linkBlue
		}
		if s.st.Color != nil {
			c = rgb{int(s.st.Color.R), int(s.st.Color.G), int(s.st.Color.B)}
		}
		p.color(c)
		p.put(x, y, tr(s.text))
		if s.st.Link != "" {
			// Clickable in a PDF reader: the area over the text opens the address.
			h := sizeOf(s.st) * 0.353
			p.pdf.LinkString(x, y-h, s.w, h*1.25, s.st.Link)
		}
		if s.st.Underline || s.st.Strike || s.st.Link != "" {
			p.draw(c)
			p.pdf.SetLineWidth(max(0.15, sizeOf(s.st)*0.025))
			if s.st.Underline || s.st.Link != "" {
				p.pdf.Line(x, y+0.7, x+s.w, y+0.7)
			}
			if s.st.Strike {
				mid := y - sizeOf(s.st)*0.353*0.3
				p.pdf.Line(x, mid, x+s.w, mid)
			}
		}
		x += s.w
	}
}

func alignX(align string, left, avail, w float64) float64 {
	switch align {
	case "center":
		return left + (avail-w)/2
	case "right":
		return left + avail - w
	}
	return left
}

// richNotes draws formatted notes from y (the previous baseline) and returns the last
// baseline.
func (p *page) richNotes(y float64, n domain.Notes) float64 {
	for _, b := range n.Blocks {
		switch b.Kind {
		case domain.NoteTable:
			y = p.noteTable(y, b.Rows)
		case domain.NoteImage:
			y = p.notePicture(y, b)
		default:
			left, avail := float64(margin), contentW
			marker := ""
			if b.Kind == domain.NoteBullet || b.Kind == domain.NoteNumbered {
				left, avail = margin+noteIndent, contentW-noteIndent
				marker = "•"
				if b.Kind == domain.NoteNumbered {
					marker = strconv.Itoa(b.Number) + "."
				}
			}
			for i, l := range p.layout(b.Runs, avail) {
				h := l.height()
				if y+h > bodyBottom {
					p.newPage()
					y = 20 - noteLineH
				}
				y += h
				if i == 0 && marker != "" {
					p.setNoteFont(domain.NoteStyle{})
					p.color(muted)
					p.put(margin+1, y, marker)
				}
				p.drawLine(l, alignX(b.Align, left, avail, l.w), y)
			}
		}
		y += 1.2
	}
	return y
}

// noteTable draws a table, columns of equal width, rows moving to a new page whole.
func (p *page) noteTable(y float64, rows [][]domain.NoteCell) float64 {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return y
	}
	colW := contentW / float64(cols)
	top := y + 1.5
	for _, r := range rows {
		cells := make([][]noteLine, cols)
		rowH := 0.0
		for i := range cols {
			if i < len(r) {
				cells[i] = p.layout(r[i].Runs, colW-2*cellPad)
			}
			h := 0.0
			for _, l := range cells[i] {
				h += l.height()
			}
			rowH = max(rowH, h)
		}
		rowH += 2 * cellPad
		if top+rowH > bodyBottom {
			p.newPage()
			top = 20
		}
		// A row taller than a whole page carries on over the next pages, a slice a page.
		for {
			sliceH, rest := 0.0, false
			take := make([]int, cols)
			for i := range cols {
				h := 0.0
				for take[i] < len(cells[i]) {
					lh := cells[i][take[i]].height()
					if take[i] > 0 && top+2*cellPad+h+lh > bodyBottom {
						break
					}
					h += lh
					take[i]++
				}
				sliceH = max(sliceH, h)
				rest = rest || take[i] < len(cells[i])
			}
			sliceH += 2 * cellPad
			p.draw(hairline)
			p.pdf.SetLineWidth(0.2)
			for i := range cols {
				x := margin + float64(i)*colW
				style := "D"
				if i < len(r) && r[i].Header {
					p.fill(cardFill)
					style = "FD"
				}
				p.pdf.Rect(x, top, colW, sliceH, style)
				ly := top + cellPad
				align := ""
				if i < len(r) {
					align = r[i].Align
				}
				for _, l := range cells[i][:take[i]] {
					ly += l.height()
					p.drawLine(l, alignX(align, x+cellPad, colW-2*cellPad, l.w), ly-0.9)
				}
				cells[i] = cells[i][take[i]:]
			}
			top += sliceH
			if !rest {
				break
			}
			p.newPage()
			top = 20
		}
	}
	return top + noteLineH - 1.2
}

// notePicture draws a picture at its shown size (96 dpi), at most the page's width and
// maxPicH high. A picture the PDF cannot take is left out with a note.
func (p *page) notePicture(y float64, b domain.NoteBlock) float64 {
	pic := b.Picture
	wpx := pic.Width
	if pic.Shown > 0 {
		wpx = pic.Shown
	}
	w := float64(wpx) * pxToMM
	h := w * float64(pic.Height) / float64(pic.Width)
	if w > contentW {
		w, h = contentW, contentW*float64(pic.Height)/float64(pic.Width)
	}
	if h > maxPicH {
		w, h = w*maxPicH/h, maxPicH
	}
	top := y + 1.5
	if top+h > bodyBottom {
		p.newPage()
		top = 20
	}
	data, typ := pic.Data, "JPG"
	// Notes stored before pictures were bounded in pixels are checked again here, before
	// anything is decoded.
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(pic.Data)); err != nil || !domain.NotePictureFits(cfg.Width, cfg.Height) {
		return p.missingPicture(top)
	}
	if pic.Format == "png" {
		// Re-encoded as plain 8-bit PNG: the PDF cannot take interlaced or 16-bit ones.
		img, err := png.Decode(bytes.NewReader(pic.Data))
		if err != nil {
			return p.missingPicture(top)
		}
		rgba := image.NewNRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
		var buf bytes.Buffer
		if png.Encode(&buf, rgba) != nil {
			return p.missingPicture(top)
		}
		data, typ = buf.Bytes(), "PNG"
	}
	p.pics++
	name := "note-picture-" + strconv.Itoa(p.pics)
	opt := fpdf.ImageOptions{ImageType: typ}
	p.pdf.RegisterImageOptionsReader(name, opt, bytes.NewReader(data))
	if p.pdf.Err() {
		p.pdf.ClearError()
		return p.missingPicture(top)
	}
	p.pdf.ImageOptions(name, alignX(b.Align, margin, contentW, w), top, w, h, false, opt, 0, "")
	return top + h + noteLineH - 1.2
}

func (p *page) missingPicture(top float64) float64 {
	p.setNoteFont(domain.NoteStyle{Italic: true})
	p.color(muted)
	p.put(margin, top+noteLineH, "[a picture that could not be printed]")
	return top + noteLineH
}
