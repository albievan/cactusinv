# ADR-008 — Every issued invoice is exported to the invoices volume

**Status:** accepted · **Date:** 2026-10-15

## Decision
A worker (`app.ExportInvoices`, every 30 s) renders each non-draft invoice whose current
`version` differs from `exported_version` and writes it atomically (temp file + rename) to
`<invoices volume>/<year>/Inv_no_<number>.pdf` through `ports.InvoiceArchive`. Every state
change bumps the version, so the archived copy is refreshed — a voided invoice is re-exported
with its VOID watermark, a paid one with its final state.

## Consequences
The invoices volume always holds the latest PDF of every invoice, readable without the
application. Export is eventually consistent (seconds) and retried until it succeeds.
