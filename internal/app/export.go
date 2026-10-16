package app

import (
	"context"
	"fmt"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

// exportRetryAfter is how long an invoice whose export failed is left out of the passes.
const exportRetryAfter = 15 * time.Minute

// ExportResult counts what one export pass did.
type ExportResult struct {
	Exported int
	Failed   int
}

// ExportInvoices writes the PDF of every issued invoice whose current version is not yet
// archived to the invoice archive (the invoices volume). A status change (sent, paid,
// void) bumps the version, so the archived copy is refreshed — a voided invoice is
// re-exported with its VOID watermark.
func (s *Service) ExportInvoices(ctx context.Context, limit int) (ExportResult, error) {
	var res ExportResult
	if s.archive == nil {
		return res, nil
	}
	now := s.now()
	s.exportMu.Lock()
	if s.exportRetry == nil {
		s.exportRetry = map[string]time.Time{}
	}
	waiting := 0
	for id, until := range s.exportRetry {
		if now.After(until) {
			delete(s.exportRetry, id)
		} else {
			waiting++
		}
	}
	s.exportMu.Unlock()
	// Ask for enough to fill the pass past the invoices that are waiting out a failure.
	refs, err := s.tx.Repos().Invoices.ListUnexported(ctx, limit+min(waiting, 1000))
	if err != nil {
		return res, wrap("app.ExportInvoices", err)
	}
	done := 0
	for _, ref := range refs {
		if stopping(ctx) || done >= limit {
			return res, nil
		}
		s.exportMu.Lock()
		_, skip := s.exportRetry[ref.ID]
		s.exportMu.Unlock()
		if skip {
			continue
		}
		done++
		if err := s.exportOne(ctx, ref.ID, ref.Version); err != nil {
			res.Failed++
			s.log.WarnContext(ctx, "invoice export failed", "invoice_id", ref.ID, "err", err)
			s.exportMu.Lock()
			s.exportRetry[ref.ID] = now.Add(exportRetryAfter)
			s.exportMu.Unlock()
			continue
		}
		res.Exported++
	}
	return res, nil
}

func (s *Service) exportOne(ctx context.Context, id string, version int) error {
	r := s.tx.Repos()
	doc, err := s.document(ctx, r, id)
	if err != nil {
		return err
	}
	if doc.Invoice.Version != version {
		return nil // changed meanwhile; the next pass exports the newer version
	}
	pdf, err := s.renderer.RenderPDF(ctx, doc)
	if err != nil {
		return err
	}
	if err := s.archive.Save(ctx, ArchivePath(doc.Invoice), pdf); err != nil {
		return err
	}
	return r.Invoices.MarkExported(ctx, id, version)
}

// ArchivePath is where an invoice is stored in the archive: <year>/<file name>, e.g.
// 2026/Inv_no_CD_1195_26.pdf.
func ArchivePath(inv *domain.Invoice) string {
	return fmt.Sprintf("%04d/%s", inv.IssueDate.Year, PDFFilename(inv))
}
