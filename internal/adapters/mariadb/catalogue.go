package mariadb

import (
	"context"

	"github.com/albievan/simpleinvoice/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
)

type customerRepo struct{ q *sqlcgen.Queries }

func toCustomer(c sqlcgen.Customer) domain.Customer {
	return domain.Customer{
		ID: c.ID, Code: c.Code, Name: c.Name, Email: c.Email, CC: splitList(c.Cc), Attention: c.Attention,
		BillingAddress: c.BillingAddress, ShippingName: c.ShippingName, ShippingAddress: c.ShippingAddress,
		Phone: c.Phone, TaxNumber: c.TaxNumber, Currency: domain.CurrencyCode(c.CurrencyCode),
		Terms:         domain.PaymentTerms{Kind: domain.TermsKind(c.TermsKind), Value: int(c.TermsValue)},
		BankAccountID: c.BankAccountID.String, DefaultNotes: c.DefaultNotes, Active: c.Active, Version: int(c.Version),
	}
}

func (r customerRepo) List(ctx context.Context, f ports.CustomerFilter) ([]domain.Customer, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListCustomers(ctx, sqlcgen.ListCustomersParams{
		IncludeInactive: f.IncludeInactive, Query: f.Query, Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountCustomers(ctx, sqlcgen.CountCustomersParams{IncludeInactive: f.IncludeInactive, Query: f.Query})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.Customer, len(rows))
	for i, c := range rows {
		out[i] = toCustomer(c)
	}
	return out, int(total), nil
}

func (r customerRepo) Get(ctx context.Context, id string) (domain.Customer, error) {
	c, err := r.q.GetCustomer(ctx, id)
	if err != nil {
		return domain.Customer{}, mapErr(err)
	}
	return toCustomer(c), nil
}

func (r customerRepo) Create(ctx context.Context, c domain.Customer) error {
	return mapErr(r.q.InsertCustomer(ctx, sqlcgen.InsertCustomerParams{
		ID: c.ID, Code: c.Code, Name: c.Name, Email: c.Email, Cc: joinList(c.CC), Attention: c.Attention,
		BillingAddress: c.BillingAddress, ShippingName: c.ShippingName, ShippingAddress: c.ShippingAddress,
		Phone: c.Phone, TaxNumber: c.TaxNumber, CurrencyCode: string(c.Currency), TermsKind: string(c.Terms.Kind),
		TermsValue: i32(c.Terms.Value), BankAccountID: nullString(c.BankAccountID), DefaultNotes: c.DefaultNotes,
		Active: c.Active,
	}))
}

func (r customerRepo) Update(ctx context.Context, c domain.Customer) error {
	n, err := r.q.UpdateCustomer(ctx, sqlcgen.UpdateCustomerParams{
		Code: c.Code, Name: c.Name, Email: c.Email, Cc: joinList(c.CC), Attention: c.Attention,
		BillingAddress: c.BillingAddress, ShippingName: c.ShippingName, ShippingAddress: c.ShippingAddress,
		Phone: c.Phone, TaxNumber: c.TaxNumber, CurrencyCode: string(c.Currency), TermsKind: string(c.Terms.Kind),
		TermsValue: i32(c.Terms.Value), BankAccountID: nullString(c.BankAccountID), DefaultNotes: c.DefaultNotes,
		Active: c.Active, ID: c.ID, Version: i32(c.Version),
	})
	return affected(n, err, domain.ErrConflict)
}

func (r customerRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteCustomer(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

type productRepo struct{ q *sqlcgen.Queries }

// withPrices loads the price lists of rows.
func (r productRepo) withPrices(ctx context.Context, rows []sqlcgen.Product) ([]domain.Product, error) {
	out := make([]domain.Product, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, len(rows))
	idx := make(map[string]int, len(rows))
	for i, p := range rows {
		ids[i] = p.ID
		idx[p.ID] = i
		out[i] = domain.Product{
			ID: p.ID, SKU: p.Sku, Name: p.Name, Description: p.Description, Unit: p.Unit, Active: p.Active,
			Version: int(p.Version), Prices: map[domain.CurrencyCode]int64{},
		}
	}
	prices, err := r.q.ListPricesByProductIDs(ctx, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	for _, pp := range prices {
		out[idx[pp.ProductID]].Prices[domain.CurrencyCode(pp.CurrencyCode)] = pp.UnitPriceMinor
	}
	return out, nil
}

func (r productRepo) List(ctx context.Context, f ports.ProductFilter) ([]domain.Product, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListProducts(ctx, sqlcgen.ListProductsParams{
		IncludeInactive: f.IncludeInactive, Query: f.Query, Currency: string(f.Currency),
		Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountProducts(ctx, sqlcgen.CountProductsParams{IncludeInactive: f.IncludeInactive, Query: f.Query, Currency: string(f.Currency)})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out, err := r.withPrices(ctx, rows)
	return out, int(total), err
}

func (r productRepo) Get(ctx context.Context, id string) (domain.Product, error) {
	row, err := r.q.GetProduct(ctx, id)
	if err != nil {
		return domain.Product{}, mapErr(err)
	}
	out, err := r.withPrices(ctx, []sqlcgen.Product{row})
	if err != nil {
		return domain.Product{}, err
	}
	return out[0], nil
}

func (r productRepo) GetMany(ctx context.Context, ids []string) (map[string]domain.Product, error) {
	out := map[string]domain.Product{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.q.ListProductsByIDs(ctx, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	ps, err := r.withPrices(ctx, rows)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out[p.ID] = p
	}
	return out, nil
}

func (r productRepo) Create(ctx context.Context, p domain.Product) error {
	if err := r.q.InsertProduct(ctx, sqlcgen.InsertProductParams{
		ID: p.ID, Sku: p.SKU, Name: p.Name, Description: p.Description, Unit: p.Unit, Active: p.Active,
	}); err != nil {
		return mapErr(err)
	}
	return r.insertPrices(ctx, p)
}

func (r productRepo) Update(ctx context.Context, p domain.Product) error {
	n, err := r.q.UpdateProduct(ctx, sqlcgen.UpdateProductParams{
		Sku: p.SKU, Name: p.Name, Description: p.Description, Unit: p.Unit, Active: p.Active, ID: p.ID, Version: i32(p.Version),
	})
	if err := affected(n, err, domain.ErrConflict); err != nil {
		return err
	}
	if err := r.q.DeleteProductPrices(ctx, p.ID); err != nil {
		return mapErr(err)
	}
	return r.insertPrices(ctx, p)
}

func (r productRepo) insertPrices(ctx context.Context, p domain.Product) error {
	for _, c := range p.Currencies() {
		if err := r.q.InsertProductPrice(ctx, sqlcgen.InsertProductPriceParams{
			ProductID: p.ID, CurrencyCode: string(c), UnitPriceMinor: p.Prices[c],
		}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r productRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteProduct(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}
