// Package pdf renders invoices and customer statements as PDF (ports.InvoiceRenderer) in
// a clean, modern layout: an accent bar, a large document title with a status pill, label/
// value address blocks, a lightly ruled line table, a highlighted total, a payment-details
// card and the disclaimer — which is supplied fresh on every render.
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Renderer implements ports.InvoiceRenderer.
type Renderer struct {
	// Uncompressed leaves content streams readable (tests grep them).
	Uncompressed bool
}

var _ ports.InvoiceRenderer = Renderer{}

type rgb struct{ r, g, b int }

// Palette: cactus green accent on neutral greys.
var (
	accent     = rgb{47, 93, 70}
	accentSoft = rgb{232, 240, 235}
	ink        = rgb{28, 33, 31}
	muted      = rgb{110, 118, 114}
	hairline   = rgb{221, 226, 223}
	cardFill   = rgb{246, 248, 247}
	warn       = rgb{180, 83, 9}
	danger     = rgb{185, 28, 28}
	good       = rgb{21, 128, 61}
)

// Page geometry (A4, millimetres).
const (
	pageW, pageH = 210.0, 297.0
	margin       = 16.0
	contentW     = pageW - 2*margin
	footerY      = pageH - 12
	bodyBottom   = pageH - 22
)

type page struct {
	pdf  *fpdf.Fpdf
	tr   func(string) string
	cur  domain.Currency
	biz  domain.BusinessProfile
	kind string // "INVOICE" | "STATEMENT"
}

func newPage(r Renderer, title, author, kind string, biz domain.BusinessProfile, cur domain.Currency) *page {
	f := fpdf.New("P", "mm", "A4", "")
	p := &page{pdf: f, tr: f.UnicodeTranslatorFromDescriptor(""), cur: cur, biz: biz, kind: kind}
	f.SetCompression(!r.Uncompressed)
	f.SetMargins(margin, margin, margin)
	f.SetAutoPageBreak(false, 0)
	f.SetTitle(p.tr(title), false)
	f.SetAuthor(p.tr(author), false)
	f.SetCreator("cactusInv", false)
	f.AliasNbPages("{nb}")
	f.SetFooterFunc(p.footer)
	return p
}

func (p *page) output() ([]byte, error) {
	if err := p.pdf.Error(); err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	var buf bytes.Buffer
	if err := p.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func (p *page) color(c rgb) { p.pdf.SetTextColor(c.r, c.g, c.b) }
func (p *page) fill(c rgb)  { p.pdf.SetFillColor(c.r, c.g, c.b) }
func (p *page) draw(c rgb)  { p.pdf.SetDrawColor(c.r, c.g, c.b) }
func (p *page) font(style string, size float64) {
	p.pdf.SetFont("Helvetica", style, size)
}

func (p *page) text(x, y float64, s string) { p.pdf.Text(x, y, p.tr(s)) }

// textRight draws s right-aligned ending at x.
func (p *page) textRight(x, y float64, s string) {
	t := p.tr(s)
	p.pdf.Text(x-p.pdf.GetStringWidth(t), y, t)
}

func (p *page) money(amount int64) string {
	return domain.FormatGrouped(amount, p.cur.MinorUnits, ",")
}

func (p *page) hline(y float64) {
	p.draw(hairline)
	p.pdf.SetLineWidth(0.2)
	p.pdf.Line(margin, y, pageW-margin, y)
}

// wrap word-wraps UTF-8 text to width w in the current font, returning lines translated to
// the PDF's cp1252 encoding (fpdf's SplitText panics on characters such as "€").
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
			for _, r := range word {
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

// pill draws a rounded status label ending at xRight.
func (p *page) pill(xRight, y float64, label string, c rgb) {
	p.font("B", 7.5)
	w := p.pdf.GetStringWidth(label) + 6
	p.draw(c)
	p.pdf.SetLineWidth(0.35)
	p.pdf.RoundedRect(xRight-w, y-4, w, 6, 3, "1234", "D")
	p.color(c)
	p.pdf.Text(xRight-w+3, y, label)
}

// header draws the accent bar, the business block, the title and the key facts (label/
// value pairs, right-aligned under the title). It returns the y below.
func (p *page) header(facts [][2]string, status string, statusColor rgb) float64 {
	p.fill(accent)
	p.pdf.Rect(0, 0, pageW, 3.5, "F")

	p.color(accent)
	p.font("B", 15)
	y := 18.0
	for _, l := range p.wrap(p.biz.Name, 100) {
		p.pdf.Text(margin, y, l)
		y += 6.2
	}
	p.font("", 8)
	p.color(muted)
	y--
	for _, l := range nonEmpty(append(splitLines(p.biz.AddressLines), phoneLine(p.biz.Phone), p.biz.Email, vatLine(p.biz.TaxNumber))...) {
		p.text(margin, y, l)
		y += 3.8
	}
	leftBottom := y

	right := pageW - margin
	p.font("B", 22)
	p.color(ink)
	title := spaced(p.kind)
	p.textRight(right, 20, title)
	if status != "" {
		p.font("B", 22)
		p.pill(right-p.pdf.GetStringWidth(title)-4, 19, status, statusColor)
	}
	fy := 29.0
	for _, f := range facts {
		if f[1] == "" {
			continue
		}
		p.font("", 8.5)
		p.color(muted)
		p.textRight(right-48, fy, f[0])
		p.font("B", 9)
		p.color(ink)
		p.textRight(right, fy, f[1])
		fy += 5
	}
	return max(leftBottom, fy) + 4
}

type partyBlock struct {
	label string
	party domain.Party
}

func (b partyBlock) empty() bool { return b.party.Name == "" && b.party.Address == "" }

// parties draws up to two label/value blocks side by side and returns the y below them.
func (p *page) parties(top float64, blocks ...partyBlock) float64 {
	colW := contentW / 2
	bottom := top
	for i, b := range blocks {
		if b.empty() {
			continue
		}
		x := margin + float64(i)*colW
		p.font("B", 7.5)
		p.color(muted)
		p.text(x, top+4, strings.ToUpper(b.label))
		y := top + 9.5
		p.font("B", 10)
		p.color(ink)
		for _, l := range p.wrap(b.party.Name, colW-8) {
			p.pdf.Text(x, y, l)
			y += 4.6
		}
		p.font("", 9)
		for _, a := range splitLines(b.party.Address) {
			for _, l := range p.wrap(a, colW-8) {
				p.pdf.Text(x, y, l)
				y += 4.2
			}
		}
		if b.party.Attention != "" {
			p.color(muted)
			p.text(x, y, "Attn: "+b.party.Attention)
			p.color(ink)
			y += 4.2
		}
		bottom = max(bottom, y)
	}
	return bottom + 3
}

// infoStrip prints the non-empty label/value pairs in a light card.
func (p *page) infoStrip(top float64, pairs [][2]string) float64 {
	var items [][2]string
	for _, kv := range pairs {
		if strings.TrimSpace(kv[1]) != "" {
			items = append(items, kv)
		}
	}
	if len(items) == 0 {
		return top
	}
	h := 13.0
	p.fill(cardFill)
	p.pdf.RoundedRect(margin, top, contentW, h, 2, "1234", "F")
	w := contentW / float64(len(items))
	for i, kv := range items {
		x := margin + 4 + float64(i)*w
		p.font("B", 7)
		p.color(muted)
		p.text(x, top+5, strings.ToUpper(kv[0]))
		p.font("", 8.5)
		p.color(ink)
		if lines := p.wrap(kv[1], w-6); len(lines) > 0 {
			p.pdf.Text(x, top+10, lines[0])
		}
	}
	return top + h + 5
}

// column is a table column; align is "L" or "R".
type column struct {
	title string
	w     float64
	align string
}

func (p *page) tableHeader(top float64, cols []column) float64 {
	p.fill(accentSoft)
	p.pdf.Rect(margin, top, contentW, 7, "F")
	p.font("B", 7.5)
	p.color(accent)
	x := margin
	for _, c := range cols {
		if c.align == "R" {
			p.textRight(x+c.w-2, top+4.7, strings.ToUpper(c.title))
		} else {
			p.text(x+2, top+4.7, strings.ToUpper(c.title))
		}
		x += c.w
	}
	return top + 7
}

// row draws one table row from already-wrapped cells and returns its height.
func (p *page) row(top float64, cols []column, cells [][]string) float64 {
	h := float64(maxLines(cells))*4.3 + 3.6
	x := margin
	p.font("", 9)
	p.color(ink)
	for i, c := range cols {
		for j, l := range cells[i] {
			y := top + 5 + float64(j)*4.3
			if c.align == "R" {
				p.pdf.Text(x+c.w-2-p.pdf.GetStringWidth(l), y, l)
			} else {
				p.pdf.Text(x+2, y, l)
			}
		}
		x += c.w
	}
	p.hline(top + h)
	return h
}

// totals prints label/value rows right-aligned and the highlighted grand total.
func (p *page) totals(top float64, rows [][2]string, grandLabel, grandValue string) float64 {
	x0 := pageW - margin - 80
	y := top + 5
	for _, r := range rows {
		p.font("", 9)
		p.color(muted)
		p.text(x0+3, y, r[0])
		p.color(ink)
		p.textRight(pageW-margin-3, y, r[1])
		y += 5.5
	}
	p.fill(accent)
	p.pdf.RoundedRect(x0, y-2, 80, 10, 2, "1234", "F")
	p.font("B", 10.5)
	p.pdf.SetTextColor(255, 255, 255)
	p.text(x0+3, y+4.6, grandLabel)
	p.textRight(pageW-margin-3, y+4.6, grandValue)
	return y + 12
}

// payment draws the bank-details card and returns its bottom.
func (p *page) payment(top, x, w float64, b domain.BankDetails, reference string) float64 {
	rows := [][2]string{
		{"Bank", b.BankName},
		{"Branch name", b.Branch},
		{"Sort / branch code", b.BranchCode},
		{"Account name", b.AccountName},
		{accountLabel(b.AccountType), b.AccountNumber},
		{"SWIFT", b.SwiftBIC},
		{"IBAN", b.IBAN},
		{"Country", b.Country},
		{"Reference", reference},
	}
	var shown [][2]string
	for _, r := range rows {
		if strings.TrimSpace(r[1]) != "" {
			shown = append(shown, r)
		}
	}
	if b.AccountNumber == "" {
		shown = [][2]string{{"", "Payment details are added when the invoice is issued."}}
	}
	h := 9 + float64(len(shown))*4.4
	p.fill(cardFill)
	p.pdf.RoundedRect(x, top, w, h, 2, "1234", "F")
	p.font("B", 7.5)
	p.color(accent)
	p.text(x+4, top+5.5, "PAYMENT DETAILS")
	y := top + 10.5
	for _, r := range shown {
		p.font("", 8.5)
		p.color(muted)
		p.text(x+4, y, r[0])
		p.font("B", 8.5)
		p.color(ink)
		p.text(x+36, y, r[1])
		y += 4.4
	}
	return top + h
}

// notes prints optional notes and then the disclaimer (its first line as a heading),
// starting a new page when there is no room. It returns the bottom.
func (p *page) notes(top float64, notes, disclaimer string) float64 {
	type noteLine struct{ text, style string }
	var lines []noteLine
	p.font("", 8)
	for _, l := range p.wrap(notes, contentW) {
		lines = append(lines, noteLine{l, ""})
	}
	if disclaimer != "" {
		if len(lines) > 0 {
			lines = append(lines, noteLine{"", ""})
		}
		p.font("I", 7.8)
		for i, l := range p.wrap(disclaimer, contentW) {
			style := "I"
			if i == 0 {
				style = "BI"
			}
			lines = append(lines, noteLine{l, style})
		}
	}
	if len(lines) == 0 {
		return top
	}
	if top+float64(len(lines))*3.6+6 > bodyBottom {
		p.newPage()
		top = 20
	}
	p.hline(top)
	y := top + 6
	for _, l := range lines {
		p.font(l.style, 7.8)
		p.color(muted)
		p.pdf.Text(margin, y, l.text)
		y += 3.6
	}
	return y
}

func (p *page) newPage() {
	p.pdf.AddPage()
	p.fill(accent)
	p.pdf.Rect(0, 0, pageW, 3.5, "F")
}

func (p *page) footer() {
	p.draw(hairline)
	p.pdf.SetLineWidth(0.2)
	p.pdf.Line(margin, footerY-4, pageW-margin, footerY-4)
	p.font("", 7)
	p.color(muted)
	p.text(margin, footerY, p.biz.Name)
	p.textRight(pageW-margin, footerY, "Page "+strconv.Itoa(p.pdf.PageNo())+" of {nb}")
}

// watermark marks drafts, voided and paid documents diagonally.
func (p *page) watermark(mark string, c rgb) {
	if mark == "" {
		return
	}
	p.pdf.SetAlpha(0.08, "Normal")
	p.font("B", 110)
	p.color(c)
	p.pdf.TransformBegin()
	p.pdf.TransformRotate(35, pageW/2, pageH/2)
	w := p.pdf.GetStringWidth(mark)
	p.pdf.Text(pageW/2-w/2, pageH/2+15, mark)
	p.pdf.TransformEnd()
	p.pdf.SetAlpha(1, "Normal")
}

// ---- invoice ----

// RenderPDF implements ports.InvoiceRenderer.
func (r Renderer) RenderPDF(_ context.Context, doc ports.InvoiceDocument) ([]byte, error) {
	inv := doc.Invoice
	p := newPage(r, invoiceTitle(inv), doc.Business.Name, "INVOICE", doc.Business, doc.Currency)
	status, statusColor, mark := invoiceStatus(inv)

	number := inv.Number
	if number == "" {
		number = "Not issued"
	}
	due := ""
	if !inv.DueDate.IsZero() {
		due = inv.DueDate.Long()
	}
	facts := [][2]string{
		{"Invoice no.", number},
		{"Date", inv.IssueDate.Long()},
		{"Due", due},
		{"Amount due", string(inv.Currency) + " " + p.money(inv.Totals.Total)},
	}
	terms := inv.TermsText
	if terms == "" {
		terms = inv.Terms.Text()
	}
	delivery := ""
	if !inv.Header.DeliveryDate.IsZero() {
		delivery = inv.Header.DeliveryDate.Long()
	}
	hasDisc := false
	for _, l := range inv.Lines {
		hasDisc = hasDisc || l.Discount > 0
	}
	cols := invoiceColumns(hasDisc)

	lines := inv.Lines
	for first := true; first || len(lines) > 0; first = false {
		var y float64
		if first {
			p.pdf.AddPage()
			y = p.header(facts, status, statusColor)
			y = p.parties(y, partyBlock{"Bill to", inv.BillTo}, partyBlock{"Ship to", inv.ShipTo})
			y = p.infoStrip(y, [][2]string{
				{"Payment terms", terms},
				{"Sales person", inv.Header.SalesPerson},
				{"Job", inv.Header.Job},
				{"Shipping method", inv.Header.ShippingMethod},
				{"Shipping terms", inv.Header.ShippingTerms},
				{"Delivery date", delivery},
			})
		} else {
			p.newPage()
			p.font("B", 9)
			p.color(muted)
			p.text(margin, 14, "Invoice "+number+" (continued)")
			y = 20
		}
		y = p.tableHeader(y, cols)
		i := 0
		for ; i < len(lines); i++ {
			cells := p.invoiceCells(lines[i], cols, hasDisc)
			if y+float64(maxLines(cells))*4.3+3.6 > bodyBottom-4 && i > 0 {
				break
			}
			y += p.row(y, cols, cells)
		}
		lines = lines[i:]
		p.watermark(mark, statusColor)
		if len(lines) > 0 {
			p.font("I", 8)
			p.color(muted)
			p.textRight(pageW-margin, bodyBottom, "Continued on next page")
			continue
		}
		t := inv.Totals
		rows := [][2]string{{"Subtotal", p.money(t.Subtotal)}}
		if t.Discount != 0 {
			rows = append(rows, [2]string{"Discount", "-" + p.money(t.Discount)})
		}
		// VAT appears only when charged: a 0 % invoice shows nothing about VAT.
		if inv.TaxRate > 0 {
			rows = append(rows, [2]string{"VAT " + inv.TaxRate.String() + "%", p.money(t.Tax)})
		}
		if y+float64(len(rows))*5.5+66 > bodyBottom {
			p.newPage()
			y = 20
		}
		totalsBottom := p.totals(y+2, rows, "Total "+string(inv.Currency), p.money(t.Total))
		payBottom := p.payment(y+4, margin, contentW-90, inv.Bank, inv.Number)
		p.notes(max(totalsBottom, payBottom)+5, inv.Notes, doc.Disclaimer)
		if inv.Status == domain.InvoiceVoid && inv.VoidReason != "" {
			p.font("I", 8)
			p.color(danger)
			p.text(margin, bodyBottom+3, "Voided: "+inv.VoidReason)
		}
	}
	return p.output()
}

func invoiceColumns(hasDisc bool) []column {
	if hasDisc {
		return []column{{"Description", 86, "L"}, {"Qty", 16, "R"}, {"Unit price", 28, "R"}, {"Disc", 16, "R"}, {"Amount", contentW - 146, "R"}}
	}
	return []column{{"Description", 102, "L"}, {"Qty", 16, "R"}, {"Unit price", 28, "R"}, {"Amount", contentW - 146, "R"}}
}

func (p *page) invoiceCells(l domain.Line, cols []column, hasDisc bool) [][]string {
	p.font("", 9)
	cells := [][]string{p.wrap(l.Description, cols[0].w-4), {l.Quantity.String()}, {p.money(l.UnitPrice)}}
	if hasDisc {
		d := ""
		if l.Discount > 0 {
			d = l.Discount.String() + "%"
		}
		cells = append(cells, []string{d})
	}
	return append(cells, []string{p.money(l.Amount())})
}

func maxLines(cells [][]string) int {
	n := 1
	for _, c := range cells {
		n = max(n, len(c))
	}
	return n
}

func invoiceTitle(inv *domain.Invoice) string {
	if inv.Number == "" {
		return "Draft invoice"
	}
	return "Invoice " + inv.Number
}

func invoiceStatus(inv *domain.Invoice) (label string, c rgb, watermark string) {
	switch inv.Status {
	case domain.InvoiceDraft:
		return "DRAFT", muted, "DRAFT"
	case domain.InvoicePaid:
		return "PAID", good, "PAID"
	case domain.InvoiceVoid:
		return "VOID", danger, "VOID"
	case domain.InvoiceOverdue:
		return "OVERDUE", warn, ""
	default:
		return "", accent, ""
	}
}

// ---- statement ----

// RenderStatement implements ports.InvoiceRenderer.
func (r Renderer) RenderStatement(_ context.Context, doc ports.StatementDocument) ([]byte, error) {
	st := doc.Statement
	p := newPage(r, "Statement "+st.Customer.Name, doc.Business.Name, "STATEMENT", doc.Business, doc.Currency)
	facts := [][2]string{
		{"Statement date", st.AsOf.Long()},
		{"Account", st.Customer.Code},
		{"Balance due", string(st.Currency) + " " + p.money(st.Total)},
	}
	cols := []column{{"Invoice", 40, "L"}, {"Date", 34, "L"}, {"Due", 34, "L"}, {"Days overdue", 26, "R"}, {"Amount", contentW - 134, "R"}}
	lines := st.Lines
	for first := true; first || len(lines) > 0; first = false {
		var y float64
		if first {
			p.pdf.AddPage()
			y = p.header(facts, "", accent)
			c := st.Customer
			y = p.parties(y, partyBlock{"Statement for", domain.Party{Name: c.Name, Address: c.BillingAddress, Attention: c.Attention}})
			y = p.aging(y, st.Aging)
		} else {
			p.newPage()
			y = 20
		}
		y = p.tableHeader(y, cols)
		if len(lines) == 0 {
			p.font("I", 9)
			p.color(muted)
			p.text(margin+2, y+6, "Nothing is outstanding. Thank you!")
			y += 10
		}
		i := 0
		for ; i < len(lines); i++ {
			if y+8 > bodyBottom-4 && i > 0 {
				break
			}
			l := lines[i]
			days, due := "", ""
			if l.DaysOverdue > 0 {
				days = strconv.Itoa(l.DaysOverdue)
			}
			if !l.DueDate.IsZero() {
				due = l.DueDate.Long()
			}
			y += p.row(y, cols, [][]string{{p.tr(l.Number)}, {l.IssueDate.Long()}, {due}, {days}, {p.money(l.Amount)}})
		}
		lines = lines[i:]
		if len(lines) > 0 {
			continue
		}
		if y+70 > bodyBottom {
			p.newPage()
			y = 20
		}
		totalsBottom := p.totals(y+2, nil, "Balance due "+string(st.Currency), p.money(st.Total))
		payBottom := p.payment(y+4, margin, contentW-90, doc.Bank, st.Customer.Code)
		p.notes(max(totalsBottom, payBottom)+5, "", doc.Disclaimer)
	}
	return p.output()
}

// aging draws the five aging buckets as small cards.
func (p *page) aging(top float64, a domain.Aging) float64 {
	buckets := []struct {
		label string
		amt   int64
	}{{"Current", a.Current}, {"1-30 days", a.Days1to30}, {"31-60 days", a.Days31to60}, {"61-90 days", a.Days61to90}, {"90+ days", a.Over90}}
	gap := 3.0
	w := (contentW - gap*4) / 5
	for i, b := range buckets {
		x := margin + float64(i)*(w+gap)
		p.fill(cardFill)
		p.pdf.RoundedRect(x, top, w, 14, 2, "1234", "F")
		p.font("B", 7)
		p.color(muted)
		p.text(x+3, top+5, strings.ToUpper(b.label))
		p.font("B", 10)
		p.color(ink)
		if i >= 2 && b.amt > 0 {
			p.color(warn)
		}
		p.text(x+3, top+11, p.money(b.amt))
	}
	return top + 19
}

// ---- helpers ----

func spaced(s string) string { return strings.Join(strings.Split(s, ""), " ") }

func accountLabel(accountType string) string {
	if accountType == "" {
		return "Account number"
	}
	return accountType + " account"
}

func vatLine(n string) string {
	if n == "" {
		return ""
	}
	return "VAT No: " + n
}

func phoneLine(phone string) string {
	if phone == "" {
		return ""
	}
	return "Tel: " + phone
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
