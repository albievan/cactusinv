package mariadb

import (
	"context"
	"database/sql"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type invoiceRepo struct{ q *sqlcgen.Queries }

func toInvoice(r sqlcgen.Invoice) *domain.Invoice {
	return &domain.Invoice{
		ID: r.ID, Number: r.Number.String, Status: domain.InvoiceStatus(r.Status), CustomerID: r.CustomerID,
		Currency: domain.CurrencyCode(r.CurrencyCode), IssueDate: fromNullDate(r.IssueDate), DueDate: fromNullDate(r.DueDate),
		Terms:     domain.PaymentTerms{Kind: domain.TermsKind(r.TermsKind), Value: int(r.TermsValue)},
		TermsText: r.TermsText,
		Header: domain.InvoiceHeader{
			SalesPerson: r.SalesPerson, Job: r.Job, ShippingMethod: r.ShippingMethod, ShippingTerms: r.ShippingTerms,
			DeliveryDate: fromNullDate(r.DeliveryDate),
		},
		BillTo:  domain.Party{Name: r.BillToName, Address: r.BillToAddress, Attention: r.BillToAttention},
		ShipTo:  domain.Party{Name: r.ShipToName, Address: r.ShipToAddress},
		Notes:   r.Notes,
		TaxRate: domain.BasisPoints(r.TaxRateBp),
		Totals:  domain.Totals{Subtotal: r.SubtotalMinor, Discount: r.DiscountMinor, Tax: r.TaxMinor, Total: r.TotalMinor},
		Bank: domain.BankDetails{
			BankName: r.BankName, Branch: r.BankBranch, BranchCode: r.BankBranchCode, AccountName: r.BankAccountName,
			AccountNumber: r.BankAccountNumber, AccountType: r.BankAccountType, SwiftBIC: r.BankSwiftBic, IBAN: r.BankIban,
			Country: r.BankCountry,
		},
		ScheduleID: r.ScheduleID.String, IssuedAt: fromNullTime(r.IssuedAt), SentAt: fromNullTime(r.SentAt),
		PaidAt: fromNullTime(r.PaidAt), OverdueAt: fromNullTime(r.OverdueAt), VoidedAt: fromNullTime(r.VoidedAt), VoidReason: r.VoidReason,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), Version: int(r.Version),
	}
}

func (r invoiceRepo) List(ctx context.Context, f ports.InvoiceFilter) ([]domain.Invoice, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListInvoices(ctx, sqlcgen.ListInvoicesParams{
		Status: string(f.Status), CustomerID: f.CustomerID, Query: f.Query, Overdue: f.Overdue,
		Today: nullDate(f.Today), Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountInvoices(ctx, sqlcgen.CountInvoicesParams{
		Status: string(f.Status), CustomerID: f.CustomerID, Query: f.Query, Overdue: f.Overdue, Today: nullDate(f.Today),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.Invoice, len(rows))
	for i, row := range rows {
		out[i] = *toInvoice(row)
	}
	return out, int(total), nil
}

func (r invoiceRepo) Get(ctx context.Context, id string) (*domain.Invoice, error) {
	row, err := r.q.GetInvoice(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	inv := toInvoice(row)
	lines, err := r.q.ListInvoiceLines(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	inv.Lines = make([]domain.Line, len(lines))
	for i, l := range lines {
		inv.Lines[i] = domain.Line{
			ProductID: l.ProductID.String, Description: l.Description, Quantity: domain.Quantity(l.QuantityMilli),
			UnitPrice: l.UnitPriceMinor, Discount: domain.BasisPoints(l.DiscountBp),
		}
	}
	return inv, nil
}

func (r invoiceRepo) Create(ctx context.Context, inv *domain.Invoice) error {
	err := r.q.InsertInvoice(ctx, sqlcgen.InsertInvoiceParams{
		ID: inv.ID, Number: nullString(inv.Number), Status: string(inv.Status), CustomerID: inv.CustomerID,
		CurrencyCode: string(inv.Currency), IssueDate: nullDate(inv.IssueDate), DueDate: nullDate(inv.DueDate),
		TermsKind: string(inv.Terms.Kind), TermsValue: i32(inv.Terms.Value), TermsText: inv.TermsText,
		SalesPerson: inv.Header.SalesPerson, Job: inv.Header.Job, ShippingMethod: inv.Header.ShippingMethod,
		ShippingTerms: inv.Header.ShippingTerms, DeliveryDate: nullDate(inv.Header.DeliveryDate),
		BillToName: inv.BillTo.Name, BillToAddress: inv.BillTo.Address, BillToAttention: inv.BillTo.Attention,
		ShipToName: inv.ShipTo.Name, ShipToAddress: inv.ShipTo.Address, Notes: inv.Notes, TaxRateBp: i32(int(inv.TaxRate)),
		SubtotalMinor: inv.Totals.Subtotal, DiscountMinor: inv.Totals.Discount, TaxMinor: inv.Totals.Tax,
		TotalMinor: inv.Totals.Total, BankName: inv.Bank.BankName, BankBranch: inv.Bank.Branch,
		BankBranchCode: inv.Bank.BranchCode, BankAccountName: inv.Bank.AccountName,
		BankAccountNumber: inv.Bank.AccountNumber, BankAccountType: inv.Bank.AccountType,
		BankSwiftBic: inv.Bank.SwiftBIC, BankIban: inv.Bank.IBAN, BankCountry: inv.Bank.Country,
		ScheduleID: nullString(inv.ScheduleID),
		IssuedAt:   nullTime(inv.IssuedAt), SentAt: nullTime(inv.SentAt), PaidAt: nullTime(inv.PaidAt), OverdueAt: nullTime(inv.OverdueAt),
		VoidedAt: nullTime(inv.VoidedAt), VoidReason: inv.VoidReason, CreatedAt: inv.CreatedAt.UTC(),
		UpdatedAt: inv.UpdatedAt.UTC(),
	})
	if err != nil {
		return mapErr(err)
	}
	inv.Version = 1
	return r.insertLines(ctx, inv)
}

func (r invoiceRepo) Update(ctx context.Context, inv *domain.Invoice) error {
	n, err := r.q.UpdateInvoice(ctx, sqlcgen.UpdateInvoiceParams{
		Number: nullString(inv.Number), Status: string(inv.Status), IssueDate: nullDate(inv.IssueDate),
		DueDate: nullDate(inv.DueDate), TermsKind: string(inv.Terms.Kind), TermsValue: i32(inv.Terms.Value),
		TermsText: inv.TermsText, SalesPerson: inv.Header.SalesPerson, Job: inv.Header.Job,
		ShippingMethod: inv.Header.ShippingMethod, ShippingTerms: inv.Header.ShippingTerms,
		DeliveryDate: nullDate(inv.Header.DeliveryDate), BillToName: inv.BillTo.Name, BillToAddress: inv.BillTo.Address,
		BillToAttention: inv.BillTo.Attention, ShipToName: inv.ShipTo.Name, ShipToAddress: inv.ShipTo.Address,
		Notes: inv.Notes, TaxRateBp: i32(int(inv.TaxRate)), SubtotalMinor: inv.Totals.Subtotal,
		DiscountMinor: inv.Totals.Discount, TaxMinor: inv.Totals.Tax, TotalMinor: inv.Totals.Total,
		BankName: inv.Bank.BankName, BankBranch: inv.Bank.Branch, BankBranchCode: inv.Bank.BranchCode,
		BankAccountName: inv.Bank.AccountName, BankAccountNumber: inv.Bank.AccountNumber,
		BankAccountType: inv.Bank.AccountType, BankSwiftBic: inv.Bank.SwiftBIC, BankIban: inv.Bank.IBAN,
		BankCountry: inv.Bank.Country, IssuedAt: nullTime(inv.IssuedAt), SentAt: nullTime(inv.SentAt), PaidAt: nullTime(inv.PaidAt), OverdueAt: nullTime(inv.OverdueAt),
		VoidedAt: nullTime(inv.VoidedAt), VoidReason: inv.VoidReason, UpdatedAt: inv.UpdatedAt.UTC(),
		ID: inv.ID, Version: i32(inv.Version),
	})
	if err := affected(n, err, domain.ErrConflict); err != nil {
		return err
	}
	inv.Version++
	if inv.Status != domain.InvoiceDraft && inv.Status != domain.InvoiceIssued {
		return nil // lines are frozen after issue; nothing to rewrite
	}
	if err := r.q.DeleteInvoiceLines(ctx, inv.ID); err != nil {
		return mapErr(err)
	}
	return r.insertLines(ctx, inv)
}

func (r invoiceRepo) insertLines(ctx context.Context, inv *domain.Invoice) error {
	for i, l := range inv.Lines {
		if err := r.q.InsertInvoiceLine(ctx, sqlcgen.InsertInvoiceLineParams{
			InvoiceID: inv.ID, Position: i32(i), ProductID: nullString(l.ProductID), Description: l.Description,
			QuantityMilli: int64(l.Quantity), UnitPriceMinor: l.UnitPrice, DiscountBp: i32(int(l.Discount)),
			AmountMinor: l.Amount(),
		}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r invoiceRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteInvoice(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

func (r invoiceRepo) AddEvent(ctx context.Context, ev domain.InvoiceEvent) error {
	return mapErr(r.q.InsertInvoiceEvent(ctx, sqlcgen.InsertInvoiceEventParams{
		ID: ev.ID, InvoiceID: ev.InvoiceID, Type: ev.Type, Detail: truncate(ev.Detail, 1000), At: ev.At.UTC(),
	}))
}

func (r invoiceRepo) ListEvents(ctx context.Context, invoiceID string) ([]domain.InvoiceEvent, error) {
	rows, err := r.q.ListInvoiceEvents(ctx, invoiceID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.InvoiceEvent, len(rows))
	for i, e := range rows {
		out[i] = domain.InvoiceEvent{ID: e.ID, InvoiceID: e.InvoiceID, Type: e.Type, Detail: e.Detail, At: e.At.UTC()}
	}
	return out, nil
}

func (r invoiceRepo) Totals(ctx context.Context, today domain.Date) ([]ports.CurrencyTotals, error) {
	rows, err := r.q.InvoiceTotals(ctx, sqlcgen.InvoiceTotalsParams{Today: nullDate(today)})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]ports.CurrencyTotals, len(rows))
	for i, t := range rows {
		out[i] = ports.CurrencyTotals{
			Currency: domain.CurrencyCode(t.CurrencyCode), OpenCount: int(t.OpenCount), Outstanding: t.Outstanding,
			OverdueCount: int(t.OverdueCount), Overdue: t.Overdue,
		}
	}
	return out, nil
}

func (r invoiceRepo) CountForCustomer(ctx context.Context, customerID string) (int, error) {
	n, err := r.q.CountInvoicesForCustomer(ctx, customerID)
	return int(n), mapErr(err)
}

func (r invoiceRepo) ListUnexported(ctx context.Context, limit int) ([]ports.ExportRef, error) {
	rows, err := r.q.ListUnexportedInvoices(ctx, i32(limit))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]ports.ExportRef, len(rows))
	for i, row := range rows {
		out[i] = ports.ExportRef{ID: row.ID, Version: int(row.Version)}
	}
	return out, nil
}

func (r invoiceRepo) MarkExported(ctx context.Context, id string, version int) error {
	return mapErr(r.q.MarkInvoiceExported(ctx, sqlcgen.MarkInvoiceExportedParams{
		ExportedVersion: sql.NullInt32{Int32: i32(version), Valid: true}, ID: id,
	}))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
