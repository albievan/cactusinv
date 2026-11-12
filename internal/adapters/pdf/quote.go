package pdf

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// RenderQuote implements ports.InvoiceRenderer: the quote, with the validity date and terms
// where an invoice has its due date and payment details. The PDF is encrypted so that a
// reader lets it be printed, and nothing else: no changes, form filling, annotations or
// copying (encryptPrintOnly).
func (r Renderer) RenderQuote(_ context.Context, doc ports.QuoteDocument) ([]byte, error) {
	q := doc.Quote
	p := newPage(r, "Quotation "+q.Number, doc.Business.Name, "QUOTATION", doc.Business, doc.Currency)
	status, statusColor, mark := quoteStatus(q, doc.Expired)
	p.mark, p.markColor = mark, statusColor
	facts := [][2]string{
		{"Quotation no.", q.Number},
		{"Date", q.IssueDate.Long()},
		{"Valid until", q.ValidUntil.Long()},
		{"Total", string(q.Currency) + " " + p.money(q.Totals.Total)},
	}
	hasDisc := false
	for _, l := range q.Lines {
		hasDisc = hasDisc || l.Discount > 0
	}
	cols := invoiceColumns(hasDisc)
	y := p.lineTable(q.Lines, cols, hasDisc, "Quotation "+q.Number+" (continued)", func() float64 {
		y := p.header(facts, status, statusColor)
		y = p.parties(y, partyBlock{"Quotation for", q.BillTo})
		return p.infoStrip(y, [][2]string{
			{"Sales person", q.SalesPerson},
			{"Reference", q.Reference},
			{"Verification code", q.Fingerprint()},
		})
	})
	t := q.Totals
	rows := [][2]string{{"Subtotal", p.money(t.Subtotal)}}
	if t.Discount != 0 {
		rows = append(rows, [2]string{"Discount", "-" + p.money(t.Discount)})
	}
	if q.TaxRate > 0 {
		rows = append(rows, [2]string{"VAT " + q.TaxRate.String() + "%", p.money(t.Tax)})
	}
	if y+float64(len(rows))*5.5+30 > bodyBottom {
		p.newPage()
		y = 20
	}
	totalsBottom := p.totals(y+2, rows, "Total "+string(q.Currency), p.money(t.Total))
	p.validity(y+4, q)
	terms := q.Terms
	if terms != "" {
		terms = "Terms: " + terms
	}
	p.notes(totalsBottom+5, q.Notes, terms)
	out, err := p.output()
	if err != nil {
		return nil, err
	}
	return encryptPrintOnly(out)
}

// validity is the box beside the totals saying until when the quote holds.
func (p *page) validity(top float64, q *domain.Quote) {
	p.font("B", 8)
	p.color(accent)
	p.text(margin, top+3, "VALIDITY")
	p.font("", 8.5)
	p.color(ink)
	y := top + 8.5
	for _, l := range p.wrap("This quotation is valid until "+q.ValidUntil.Long()+". Prices are in "+string(q.Currency)+".", contentW-96) {
		p.put(margin, y, l)
		y += 4
	}
	p.font("", 7.5)
	p.color(muted)
	for _, l := range p.wrap("Please quote "+q.Number+" when you accept. Verification code "+q.Fingerprint()+".", contentW-96) {
		p.put(margin, y+1, l)
		y += 3.6
	}
}

func quoteStatus(q *domain.Quote, expired bool) (label string, c rgb, watermark string) {
	switch {
	case q.Status == domain.QuoteDraft && expired, q.Status == domain.QuoteSent && expired:
		return "EXPIRED", warn, "EXPIRED"
	case q.Status == domain.QuoteAccepted:
		return "ACCEPTED", good, ""
	case q.Status == domain.QuoteDeclined:
		return "DECLINED", danger, "DECLINED"
	case q.Status == domain.QuoteConverted:
		return "INVOICED", good, ""
	default:
		return "", accent, ""
	}
}
