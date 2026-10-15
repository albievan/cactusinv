// Package pdf renders invoices as PDF (ports.InvoiceRenderer). The layout mirrors the
// reference invoice example/Inv_no_CD_1195_26.pdf: a blue title band, From/To/Ship-To
// blocks, a row of header boxes, a ruled line-item table with the notes at its foot, the
// bank details in red labels and a totals box.
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
)

// Renderer implements ports.InvoiceRenderer.
type Renderer struct {
	// Uncompressed leaves content streams readable (tests grep them).
	Uncompressed bool
}

var _ ports.InvoiceRenderer = Renderer{}

type rgb struct{ r, g, b int }

var (
	bandBlue  = rgb{157, 195, 230}
	titleBlue = rgb{46, 117, 182}
	labelRed  = rgb{192, 0, 0}
	black     = rgb{0, 0, 0}
	grey      = rgb{110, 110, 110}
)

// Page geometry (A4, millimetres).
const (
	pageW, pageH = 210.0, 297.0
	margin       = 6.0
	contentW     = pageW - 2*margin
	tableBottom  = 247.0
	lineH        = 4.6
)

// Line table columns: Quantity, Description, Unit Price, Disc, Amount.
var colW = [5]float64{26, 103, 30, 20, contentW - 26 - 103 - 30 - 20}

// RenderPDF implements ports.InvoiceRenderer.
func (r Renderer) RenderPDF(_ context.Context, doc ports.InvoiceDocument) ([]byte, error) {
	p := &page{pdf: fpdf.New("P", "mm", "A4", ""), doc: doc}
	p.tr = p.pdf.UnicodeTranslatorFromDescriptor("")
	p.pdf.SetCompression(!r.Uncompressed)
	p.pdf.SetMargins(margin, margin, margin)
	p.pdf.SetAutoPageBreak(false, 0)
	p.pdf.SetTitle(p.tr(title(doc.Invoice)), false)
	p.pdf.SetAuthor(p.tr(doc.Business.Name), false)
	p.pdf.SetCreator("cactusInv", false)
	p.render()
	if err := p.pdf.Error(); err != nil {
		return nil, fmt.Errorf("pdf.RenderPDF: %w", err)
	}
	var buf bytes.Buffer
	if err := p.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf.RenderPDF: %w", err)
	}
	return buf.Bytes(), nil
}

func title(inv *domain.Invoice) string {
	if inv.Number == "" {
		return "Draft invoice"
	}
	return "Invoice " + inv.Number
}

type page struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
	doc ports.InvoiceDocument
}

func (p *page) color(c rgb) { p.pdf.SetTextColor(c.r, c.g, c.b) }
func (p *page) fill(c rgb)  { p.pdf.SetFillColor(c.r, c.g, c.b) }
func (p *page) font(style string, size float64) {
	p.pdf.SetFont("Helvetica", style, size)
}

func (p *page) text(x, y float64, s string) { p.pdf.Text(x, y, p.tr(s)) }

// wrap word-wraps UTF-8 text to width w in the current font and returns lines already
// translated to the PDF's cp1252 encoding. (fpdf's SplitText indexes its width table by
// rune and panics on characters such as "€".)
func (p *page) wrap(s string, w float64) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if p.pdf.GetStringWidth(p.tr(candidate)) <= w {
				line = candidate
				continue
			}
			if line != "" {
				out = append(out, p.tr(line))
			}
			line = ""
			for _, r := range word { // break words longer than a line
				if p.pdf.GetStringWidth(p.tr(line+string(r))) > w && line != "" {
					out = append(out, p.tr(line))
					line = ""
				}
				line += string(r)
			}
		}
		out = append(out, p.tr(line))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func (p *page) money(amount int64) string {
	return domain.FormatAmount(amount, p.doc.Currency.MinorUnits)
}

func (p *page) render() {
	inv := p.doc.Invoice
	lines := inv.Lines
	for first := true; first || len(lines) > 0; first = false {
		p.pdf.AddPage()
		p.header()
		y := p.addresses()
		y = p.infoBoxes(y)
		lines = p.lineTable(y, lines)
		if len(lines) > 0 {
			p.font("I", 8)
			p.color(grey)
			p.text(margin, tableBottom+6, "Continued on next page")
		}
		p.watermark()
	}
	p.footer()
}

// header draws the blue band with the business name and the number/date block.
func (p *page) header() {
	b, inv := p.doc.Business, p.doc.Invoice
	p.fill(bandBlue)
	p.pdf.Rect(0, 0, pageW, 19, "F")
	p.font("B", 17)
	p.color(titleBlue)
	p.text(margin+2, 12, b.Name)
	p.font("B", 11)
	label := "Invoice No"
	if inv.Status == domain.InvoiceDraft {
		label = "Draft Invoice"
	}
	p.text(156, 12, label)

	p.font("B", 6.5)
	p.color(black)
	y := 23.0
	for _, l := range nonEmpty(append(splitLines(b.AddressLines), phoneLine(b.Phone), b.Email, taxLine(b.TaxNumber))...) {
		p.text(margin, y, l)
		y += 2.7
	}
	p.font("B", 11)
	num := inv.Number
	if num == "" {
		num = "(not issued)"
	}
	p.text(140, 27, "Inv. No. "+num)
	p.text(140, 32.5, "DATE")
	p.text(152, 32.5, inv.IssueDate.Long())
	p.font("", 8)
	p.color(grey)
	p.text(140, 36.5, "Currency: "+string(inv.Currency)+" "+p.doc.Currency.Name)
}

// addresses draws the To / Ship To blocks and returns the y below them.
func (p *page) addresses() float64 {
	inv := p.doc.Invoice
	top := 38.5
	p.pdf.SetDrawColor(0, 0, 0)
	p.pdf.SetLineWidth(0.2)
	p.pdf.Line(0, top, pageW, top)
	p.color(black)
	block := func(x, labelW float64, label string, party domain.Party, showAtt bool) float64 {
		p.font("", 9.5)
		p.text(x, top+5, label)
		p.font("B", 9.5)
		y := top + 5
		rows := append([]string{party.Name}, splitLines(party.Address)...)
		for _, l := range nonEmpty(rows...) {
			for _, w := range p.wrap(l, 93-labelW) {
				p.pdf.Text(x+labelW, y, w)
				y += 4.2
			}
		}
		if showAtt && party.Attention != "" {
			y += 2
			p.text(x, y, "Att: "+party.Attention)
			y += 4.2
		}
		return y
	}
	leftY := block(margin, 7, "To:", inv.BillTo, true)
	rightY := top + 5
	if inv.ShipTo.Name != "" || inv.ShipTo.Address != "" {
		rightY = block(107, 14, "Ship To:", inv.ShipTo, false)
	}
	bottom := max(leftY, rightY, top+24) + 1
	p.pdf.Line(104, top, 104, bottom)
	p.pdf.Line(0, bottom, pageW, bottom)
	return bottom
}

// infoBoxes draws the Sales Person … Due Date row and returns the y below it.
func (p *page) infoBoxes(top float64) float64 {
	inv := p.doc.Invoice
	labels := []string{"Sales Person", "Job", "Shipping Method", "Shipping Terms", "Delivery Date", "Payment Terms", "Due Date"}
	widths := []float64{40, 25, 25, 25, 25, 34, contentW - 40 - 25*4 - 34}
	due := ""
	if !inv.DueDate.IsZero() {
		due = inv.DueDate.Long()
	}
	delivery := ""
	if !inv.Header.DeliveryDate.IsZero() {
		delivery = inv.Header.DeliveryDate.Long()
	}
	terms := inv.TermsText
	if terms == "" {
		terms = inv.Terms.Text()
	}
	values := []string{inv.Header.SalesPerson, inv.Header.Job, inv.Header.ShippingMethod, inv.Header.ShippingTerms, delivery, terms, due}

	p.fill(bandBlue)
	p.pdf.Rect(0, top, pageW, 9, "F")
	p.font("B", 8.5)
	p.color(titleBlue)
	x := margin
	for i, l := range labels {
		p.pdf.SetXY(x, top+0.5)
		p.pdf.MultiCell(widths[i], 4, p.tr(l), "", "C", false)
		x += widths[i]
	}
	valTop := top + 9
	p.font("B", 7)
	p.color(black)
	h := 13.0
	x = margin
	for i, v := range values {
		p.pdf.Rect(x, valTop, widths[i], h, "D")
		p.pdf.SetXY(x+0.5, valTop+0.8)
		p.pdf.MultiCell(widths[i]-1, 3, p.tr(v), "", "C", false)
		x += widths[i]
	}
	return valTop + h
}

// lineTable draws the ruled table, fills as many lines as fit and returns the rest.
func (p *page) lineTable(top float64, lines []domain.Line) []domain.Line {
	inv := p.doc.Invoice
	headers := []string{"Quantity", "Description", "Unit Price", "Disc", "Amount"}
	p.fill(bandBlue)
	p.pdf.Rect(0, top, pageW, 5, "F")
	p.font("B", 9)
	p.color(titleBlue)
	x := margin
	for i, h := range headers {
		p.pdf.SetXY(x, top)
		p.pdf.CellFormat(colW[i], 5, h, "", 0, "C", false, 0, "")
		x += colW[i]
	}
	// Column rules down to the table bottom, like the example.
	p.pdf.SetDrawColor(0, 0, 0)
	x = margin
	p.pdf.Rect(margin, top+5, contentW, tableBottom-top-5, "D")
	for i := range len(colW) - 1 {
		x += colW[i]
		p.pdf.Line(x, top+5, x, tableBottom)
	}

	notesH := 0.0
	var notes []string
	if inv.Notes != "" {
		p.font("I", 8.5)
		notes = p.wrap(inv.Notes, colW[1]-4)
		notesH = float64(len(notes))*3.6 + 8
	}
	limit := tableBottom - 2 - notesH
	y := top + 9
	p.color(black)
	i := 0
	for ; i < len(lines); i++ {
		l := lines[i]
		p.font("", 9.5)
		desc := p.wrap(l.Description, colW[1]-4)
		rowH := float64(len(desc))*lineH + 6
		if y+rowH > limit && i > 0 {
			break
		}
		x := margin
		p.pdf.SetXY(x+2, y-3.4)
		p.pdf.CellFormat(colW[0]-4, lineH, l.Quantity.String(), "", 0, "L", false, 0, "")
		x += colW[0]
		for j, d := range desc {
			p.pdf.Text(x+2, y+float64(j)*lineH, d)
		}
		x += colW[1]
		p.pdf.SetXY(x, y-3.4)
		p.pdf.CellFormat(colW[2]-2, lineH, p.money(l.UnitPrice), "", 0, "R", false, 0, "")
		x += colW[2]
		disc := ""
		if l.Discount > 0 {
			disc = l.Discount.String() + "%"
		}
		p.pdf.SetXY(x, y-3.4)
		p.pdf.CellFormat(colW[3]-2, lineH, disc, "", 0, "R", false, 0, "")
		x += colW[3]
		p.pdf.SetXY(x, y-3.4)
		p.pdf.CellFormat(colW[4]-2, lineH, p.money(l.Amount()), "", 0, "R", false, 0, "")
		y += rowH
	}
	rest := lines[i:]
	if len(rest) == 0 && len(notes) > 0 {
		// Like the example's "Disclaimer:", the first line of the notes is the heading.
		ny := tableBottom - notesH + 8
		for j, n := range notes {
			if j == 0 {
				p.font("BI", 9.5)
			} else {
				p.font("I", 8.5)
			}
			p.pdf.Text(margin+colW[0]+2, ny+float64(j)*3.6, n)
		}
	}
	return rest
}

// footer draws the bank details and the totals box on the last page.
func (p *page) footer() {
	inv := p.doc.Invoice
	top := tableBottom
	p.pdf.Line(0, top, pageW, top)

	b := inv.Bank
	rows := [][2]string{
		{"Bank:", b.BankName},
		{"Branch:", b.Branch},
		{accountLabel(b.AccountType), b.AccountNumber},
		{"Bank Code:", b.BranchCode},
		{"Account Name:", b.AccountName},
		{"SWIFT/BIC:", b.SwiftBIC},
		{"IBAN:", b.IBAN},
	}
	y := top + 9
	if b.AccountNumber == "" {
		p.font("I", 8.5)
		p.color(grey)
		p.text(margin, y, "Bank details are added when the invoice is issued.")
	}
	for _, r := range rows {
		if r[1] == "" || b.AccountNumber == "" {
			continue
		}
		p.font("B", 9.5)
		p.color(labelRed)
		p.text(margin, y, r[0])
		p.color(black)
		p.text(margin+28, y, r[1])
		y += 4
	}

	t := inv.Totals
	cur := string(inv.Currency)
	totals := [][2]string{{"TOTAL DISCOUNT", p.money(t.Discount)}, {"SUBTOTAL", p.money(t.Subtotal)}}
	// VAT appears only when charged: a 0 % invoice shows nothing about VAT.
	if inv.TaxRate > 0 {
		totals = append(totals, [2]string{"VAT " + inv.TaxRate.String() + "%", p.money(t.Tax)})
	}
	totals = append(totals, [2]string{"TOTAL " + cur, p.money(t.Total)})
	x := margin + colW[0] + colW[1]
	labelW := colW[2] + colW[3]
	rowH := 8.0
	for i, r := range totals {
		ry := top + float64(i)*rowH
		p.pdf.Rect(x, ry, labelW, rowH, "D")
		p.pdf.Rect(x+labelW, ry, colW[4], rowH, "D")
		p.font("B", 9.5)
		p.color(black)
		p.pdf.SetXY(x+1.5, ry)
		p.pdf.CellFormat(labelW-3, rowH, r[0], "", 0, "L", false, 0, "")
		p.font("", 9.5)
		if i == len(totals)-1 {
			p.font("B", 10)
		}
		p.pdf.SetXY(x+labelW, ry)
		p.pdf.CellFormat(colW[4]-2, rowH, r[1], "", 0, "R", false, 0, "")
	}
	if inv.Status == domain.InvoiceVoid && inv.VoidReason != "" {
		p.font("I", 8)
		p.color(labelRed)
		p.text(margin, pageH-12, "Voided: "+inv.VoidReason)
	}
}

// watermark marks drafts and voided invoices diagonally.
func (p *page) watermark() {
	var mark string
	switch p.doc.Invoice.Status {
	case domain.InvoiceDraft:
		mark = "DRAFT"
	case domain.InvoiceVoid:
		mark = "VOID"
	default:
		return
	}
	p.pdf.SetAlpha(0.12, "Normal")
	p.font("B", 110)
	p.color(labelRed)
	p.pdf.TransformBegin()
	p.pdf.TransformRotate(35, pageW/2, pageH/2)
	w := p.pdf.GetStringWidth(mark)
	p.pdf.Text(pageW/2-w/2, pageH/2+15, mark)
	p.pdf.TransformEnd()
	p.pdf.SetAlpha(1, "Normal")
}

func accountLabel(accountType string) string {
	if accountType == "" {
		return "Account:"
	}
	return accountType + " Acc:"
}

func taxLine(n string) string {
	if n == "" {
		return ""
	}
	return "VAT No: " + n
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}

func phoneLine(phone string) string {
	if phone == "" {
		return ""
	}
	return "Tel No: " + phone
}
