package mariadb

import (
	"context"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type quoteRepo struct{ q *sqlcgen.Queries }

func toQuote(r sqlcgen.Quote) *domain.Quote {
	return &domain.Quote{
		ID: r.ID, Number: r.Number, Status: domain.QuoteStatus(r.Status), CustomerID: r.CustomerID.String,
		AdhocCustomerID: r.AdhocCustomerID.String, Currency: domain.CurrencyCode(r.CurrencyCode),
		IssueDate: fromDate(r.IssueDate), ValidUntil: fromDate(r.ValidUntil), SalesPerson: r.SalesPerson, Reference: r.Reference,
		BillTo: domain.Party{Name: r.BillToName, Address: r.BillToAddress, Attention: r.BillToAttention},
		Notes:  r.Notes, Terms: r.Terms, TaxRate: domain.BasisPoints(r.TaxRateBp),
		Totals:    domain.Totals{Subtotal: r.SubtotalMinor, Discount: r.DiscountMinor, Tax: r.TaxMinor, Total: r.TotalMinor},
		InvoiceID: r.InvoiceID.String, SentAt: fromNullTime(r.SentAt), AcceptedAt: fromNullTime(r.AcceptedAt),
		DeclinedAt: fromNullTime(r.DeclinedAt), DeclineReason: r.DeclineReason, ConvertedAt: fromNullTime(r.ConvertedAt),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), Version: int(r.Version),
	}
}

// quoteToday is the date expired quotes are judged by; a zero date (no Expired filter)
// must still be a valid DATE for the driver.
func quoteToday(d domain.Date) time.Time {
	if d.IsZero() {
		d = domain.NewDate(1970, 1, 1)
	}
	return dateValue(d)
}

func (r quoteRepo) List(ctx context.Context, f ports.QuoteFilter) ([]domain.Quote, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListQuotes(ctx, sqlcgen.ListQuotesParams{
		Status: string(f.Status), CustomerID: filter(f.CustomerID), AdhocCustomerID: filter(f.AdhocCustomerID),
		Query: likeEscape(f.Query), Expired: f.Expired, Today: quoteToday(f.Today), Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountQuotes(ctx, sqlcgen.CountQuotesParams{
		Status: string(f.Status), CustomerID: filter(f.CustomerID), AdhocCustomerID: filter(f.AdhocCustomerID),
		Query: likeEscape(f.Query), Expired: f.Expired, Today: quoteToday(f.Today),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.Quote, len(rows))
	for i, row := range rows {
		out[i] = *toQuote(row)
	}
	return out, int(total), nil
}

func (r quoteRepo) Get(ctx context.Context, id string) (*domain.Quote, error) {
	row, err := r.q.GetQuote(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return r.withLines(ctx, toQuote(row))
}

func (r quoteRepo) GetByInvoice(ctx context.Context, invoiceID string) (*domain.Quote, error) {
	row, err := r.q.GetQuoteByInvoice(ctx, nullString(invoiceID))
	if err != nil {
		return nil, mapErr(err)
	}
	return r.withLines(ctx, toQuote(row))
}

func (r quoteRepo) withLines(ctx context.Context, q *domain.Quote) (*domain.Quote, error) {
	lines, err := r.q.ListQuoteLines(ctx, q.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	q.Lines = make([]domain.Line, len(lines))
	for i, l := range lines {
		q.Lines[i] = domain.Line{
			ProductID: l.ProductID.String, Description: l.Description, Quantity: domain.Quantity(l.QuantityMilli),
			UnitPrice: l.UnitPriceMinor, Discount: domain.BasisPoints(l.DiscountBp),
		}
	}
	return q, nil
}

func (r quoteRepo) Create(ctx context.Context, q *domain.Quote) error {
	err := r.q.InsertQuote(ctx, sqlcgen.InsertQuoteParams{
		ID: q.ID, Number: q.Number, Status: string(q.Status), CustomerID: nullString(q.CustomerID),
		AdhocCustomerID: nullString(q.AdhocCustomerID), CurrencyCode: string(q.Currency),
		IssueDate: dateValue(q.IssueDate), ValidUntil: dateValue(q.ValidUntil), SalesPerson: q.SalesPerson, Reference: q.Reference,
		BillToName: q.BillTo.Name, BillToAddress: q.BillTo.Address, BillToAttention: q.BillTo.Attention,
		Notes: q.Notes, Terms: q.Terms, TaxRateBp: i32(int(q.TaxRate)), SubtotalMinor: q.Totals.Subtotal,
		DiscountMinor: q.Totals.Discount, TaxMinor: q.Totals.Tax, TotalMinor: q.Totals.Total,
		InvoiceID: nullString(q.InvoiceID), SentAt: nullTime(q.SentAt), AcceptedAt: nullTime(q.AcceptedAt),
		DeclinedAt: nullTime(q.DeclinedAt), DeclineReason: q.DeclineReason, ConvertedAt: nullTime(q.ConvertedAt),
		CreatedAt: q.CreatedAt.UTC(), UpdatedAt: q.UpdatedAt.UTC(),
	})
	if err != nil {
		return mapErr(err)
	}
	q.Version = 1
	return r.insertLines(ctx, q)
}

func (r quoteRepo) Update(ctx context.Context, q *domain.Quote) error {
	n, err := r.q.UpdateQuote(ctx, sqlcgen.UpdateQuoteParams{
		Status: string(q.Status), CustomerID: nullString(q.CustomerID), IssueDate: dateValue(q.IssueDate),
		ValidUntil: dateValue(q.ValidUntil), SalesPerson: q.SalesPerson, Reference: q.Reference,
		BillToName: q.BillTo.Name, BillToAddress: q.BillTo.Address, BillToAttention: q.BillTo.Attention,
		Notes: q.Notes, Terms: q.Terms, TaxRateBp: i32(int(q.TaxRate)), SubtotalMinor: q.Totals.Subtotal,
		DiscountMinor: q.Totals.Discount, TaxMinor: q.Totals.Tax, TotalMinor: q.Totals.Total,
		InvoiceID: nullString(q.InvoiceID), SentAt: nullTime(q.SentAt), AcceptedAt: nullTime(q.AcceptedAt),
		DeclinedAt: nullTime(q.DeclinedAt), DeclineReason: q.DeclineReason, ConvertedAt: nullTime(q.ConvertedAt),
		UpdatedAt: q.UpdatedAt.UTC(), ID: q.ID, Version: i32(q.Version),
	})
	if err := affected(n, err, domain.ErrConflict); err != nil {
		return err
	}
	q.Version++
	if !q.Editable() {
		return nil // the lines of an accepted, declined or converted quote no longer change
	}
	if err := r.q.DeleteQuoteLines(ctx, q.ID); err != nil {
		return mapErr(err)
	}
	return r.insertLines(ctx, q)
}

func (r quoteRepo) insertLines(ctx context.Context, q *domain.Quote) error {
	for i, l := range q.Lines {
		if err := r.q.InsertQuoteLine(ctx, sqlcgen.InsertQuoteLineParams{
			QuoteID: q.ID, Position: i32(i), ProductID: nullString(l.ProductID), Description: l.Description,
			QuantityMilli: int64(l.Quantity), UnitPriceMinor: l.UnitPrice, DiscountBp: i32(int(l.Discount)),
			AmountMinor: l.Amount(),
		}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r quoteRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteQuote(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

func (r quoteRepo) SetCustomerForAdhoc(ctx context.Context, adhocID, customerID string, now time.Time) error {
	return mapErr(r.q.SetQuotesCustomer(ctx, sqlcgen.SetQuotesCustomerParams{
		CustomerID: nullString(customerID), UpdatedAt: now.UTC(), AdhocCustomerID: nullString(adhocID),
	}))
}

func (r quoteRepo) CountForAdhoc(ctx context.Context, adhocID string) (int, error) {
	n, err := r.q.CountQuotesForAdhoc(ctx, nullString(adhocID))
	return int(n), mapErr(err)
}

type adhocCustomerRepo struct{ q *sqlcgen.Queries }

func toAdhocCustomer(r sqlcgen.AdhocCustomer) domain.AdhocCustomer {
	return domain.AdhocCustomer{
		ID: r.ID, Name: r.Name, Email: r.Email, Attention: r.Attention, Address: r.Address, Phone: r.Phone,
		TaxNumber: r.TaxNumber, Currency: domain.CurrencyCode(r.CurrencyCode), CustomerID: r.CustomerID.String,
		ConvertedAt: fromNullTime(r.ConvertedAt), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), Version: int(r.Version),
	}
}

func (r adhocCustomerRepo) List(ctx context.Context, f ports.AdhocCustomerFilter) ([]domain.AdhocCustomer, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListAdhocCustomers(ctx, sqlcgen.ListAdhocCustomersParams{
		Query: likeEscape(f.Query), IncludeConverted: f.IncludeConverted, Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountAdhocCustomers(ctx, sqlcgen.CountAdhocCustomersParams{Query: likeEscape(f.Query), IncludeConverted: f.IncludeConverted})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.AdhocCustomer, len(rows))
	for i, row := range rows {
		out[i] = toAdhocCustomer(row)
	}
	return out, int(total), nil
}

func (r adhocCustomerRepo) Get(ctx context.Context, id string) (domain.AdhocCustomer, error) {
	row, err := r.q.GetAdhocCustomer(ctx, id)
	if err != nil {
		return domain.AdhocCustomer{}, mapErr(err)
	}
	return toAdhocCustomer(row), nil
}

func (r adhocCustomerRepo) Create(ctx context.Context, a domain.AdhocCustomer) error {
	return mapErr(r.q.InsertAdhocCustomer(ctx, sqlcgen.InsertAdhocCustomerParams{
		ID: a.ID, Name: a.Name, Email: a.Email, Attention: a.Attention, Address: a.Address, Phone: a.Phone,
		TaxNumber: a.TaxNumber, CurrencyCode: string(a.Currency), CustomerID: nullString(a.CustomerID),
		ConvertedAt: nullTime(a.ConvertedAt), CreatedAt: a.CreatedAt.UTC(), UpdatedAt: a.UpdatedAt.UTC(),
	}))
}

func (r adhocCustomerRepo) Update(ctx context.Context, a domain.AdhocCustomer) error {
	n, err := r.q.UpdateAdhocCustomer(ctx, sqlcgen.UpdateAdhocCustomerParams{
		Name: a.Name, Email: a.Email, Attention: a.Attention, Address: a.Address, Phone: a.Phone,
		TaxNumber: a.TaxNumber, CurrencyCode: string(a.Currency), CustomerID: nullString(a.CustomerID),
		ConvertedAt: nullTime(a.ConvertedAt), UpdatedAt: a.UpdatedAt.UTC(), ID: a.ID, Version: i32(a.Version),
	})
	return affected(n, err, domain.ErrConflict)
}

func (r adhocCustomerRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteAdhocCustomer(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

type quoteSettingsRepo struct{ q *sqlcgen.Queries }

func (r quoteSettingsRepo) Get(ctx context.Context) (domain.QuoteSettings, error) {
	row, err := r.q.GetQuoteSettings(ctx)
	if err != nil {
		return domain.QuoteSettings{}, mapErr(err)
	}
	return domain.QuoteSettings{
		ValidDays: int(row.ValidDays), NumberFormat: row.NumberFormat, EmailSubject: row.EmailSubject,
		EmailBody: row.EmailBody, Terms: row.Terms, Version: int(row.Version),
	}, nil
}

func (r quoteSettingsRepo) Save(ctx context.Context, s domain.QuoteSettings) error {
	now := time.Now().UTC()
	if s.Version == 0 {
		return mapErr(r.q.InsertQuoteSettings(ctx, sqlcgen.InsertQuoteSettingsParams{
			ValidDays: i32(s.ValidDays), NumberFormat: s.NumberFormat, EmailSubject: s.EmailSubject,
			EmailBody: s.EmailBody, Terms: s.Terms, UpdatedAt: now,
		}))
	}
	n, err := r.q.UpdateQuoteSettings(ctx, sqlcgen.UpdateQuoteSettingsParams{
		ValidDays: i32(s.ValidDays), NumberFormat: s.NumberFormat, EmailSubject: s.EmailSubject,
		EmailBody: s.EmailBody, Terms: s.Terms, UpdatedAt: now, Version: i32(s.Version),
	})
	return affected(n, err, domain.ErrConflict)
}

type quoteSequenceRepo struct{ q *sqlcgen.Queries }

func (r quoteSequenceRepo) Next(ctx context.Context) (int64, error) {
	v, err := r.q.LockQuoteSequence(ctx)
	if err != nil {
		return 0, mapErr(err)
	}
	return v, mapErr(r.q.SetQuoteSequence(ctx, v+1))
}

func (r quoteSequenceRepo) Lock(ctx context.Context) error {
	_, err := r.q.LockQuoteSequence(ctx)
	return mapErr(err)
}

func (r quoteSequenceRepo) Peek(ctx context.Context) (int64, error) {
	v, err := r.q.PeekQuoteSequence(ctx)
	return v, mapErr(err)
}

func (r quoteSequenceRepo) Set(ctx context.Context, next int64) error {
	return mapErr(r.q.SetQuoteSequence(ctx, next))
}
