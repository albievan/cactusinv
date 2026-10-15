package app

import (
	"context"

	"github.com/albievan/cactusinve/internal/domain"
	"github.com/albievan/cactusinve/internal/ports"
)

// ListProducts is the product look-up: search by name/SKU, optionally only products
// priced in one currency.
func (s *Service) ListProducts(ctx context.Context, f ports.ProductFilter) ([]domain.Product, int, error) {
	f.Page = f.Page.Normalize()
	out, n, err := s.tx.Repos().Products.List(ctx, f)
	return out, n, wrap("app.ListProducts", err)
}

// GetProduct returns one product with its prices.
func (s *Service) GetProduct(ctx context.Context, id string) (domain.Product, error) {
	out, err := s.tx.Repos().Products.Get(ctx, id)
	return out, wrap("app.GetProduct", err)
}

// CreateProduct adds a product to the look-up.
func (s *Service) CreateProduct(ctx context.Context, p domain.Product) (domain.Product, error) {
	p.ID = s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := checkProduct(ctx, r, &p); err != nil {
			return err
		}
		return r.Products.Create(ctx, p)
	})
	if err != nil {
		return domain.Product{}, wrap("app.CreateProduct", err)
	}
	return s.GetProduct(ctx, p.ID)
}

// UpdateProduct edits a product and replaces its price list. Existing invoices keep the
// prices they were issued with.
func (s *Service) UpdateProduct(ctx context.Context, p domain.Product) (domain.Product, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if _, err := r.Products.Get(ctx, p.ID); err != nil {
			return err
		}
		if err := checkProduct(ctx, r, &p); err != nil {
			return err
		}
		return r.Products.Update(ctx, p)
	})
	if err != nil {
		return domain.Product{}, wrap("app.UpdateProduct", err)
	}
	return s.GetProduct(ctx, p.ID)
}

// DeleteProduct removes a product. Invoice lines keep their snapshot.
func (s *Service) DeleteProduct(ctx context.Context, id string) error {
	return wrap("app.DeleteProduct", s.tx.Repos().Products.Delete(ctx, id))
}

func checkProduct(ctx context.Context, r ports.Repos, p *domain.Product) error {
	if err := p.Validate(); err != nil {
		return err
	}
	for c := range p.Prices {
		if _, err := r.Currencies.Get(ctx, c); err != nil {
			return err
		}
	}
	return nil
}
