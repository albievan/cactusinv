package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ListCurrencies returns every currency.
func (s *Service) ListCurrencies(ctx context.Context) ([]domain.Currency, error) {
	out, err := s.tx.Repos().Currencies.List(ctx)
	return out, wrap("app.ListCurrencies", err)
}

// CreateCurrency adds a currency.
func (s *Service) CreateCurrency(ctx context.Context, code, name, symbol string, minorUnits int, active bool) (domain.Currency, error) {
	c, err := domain.NewCurrency(code, name, symbol, minorUnits, active)
	if err != nil {
		return domain.Currency{}, wrap("app.CreateCurrency", err)
	}
	return c, wrap("app.CreateCurrency", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Currencies.Create(ctx, c); err != nil {
			return err
		}
		return s.audit(ctx, r, "currency.created", "currency", string(c.Code), c.Name)
	}))
}

// UpdateCurrency changes a currency's name, symbol and active flag. Minor units are
// immutable because stored amounts depend on them.
func (s *Service) UpdateCurrency(ctx context.Context, code domain.CurrencyCode, name, symbol string, active bool) (domain.Currency, error) {
	var out domain.Currency
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.Currencies.Get(ctx, code)
		if err != nil {
			return err
		}
		c, err := domain.NewCurrency(string(code), name, symbol, cur.MinorUnits, active)
		if err != nil {
			return err
		}
		out = c
		if err := r.Currencies.Update(ctx, c); err != nil {
			return err
		}
		return s.audit(ctx, r, "currency.updated", "currency", string(c.Code), activeText(c.Active))
	})
	return out, wrap("app.UpdateCurrency", err)
}

// activeCurrency loads a currency and requires it to be active.
func activeCurrency(ctx context.Context, r ports.Repos, code domain.CurrencyCode) (domain.Currency, error) {
	c, err := r.Currencies.Get(ctx, code)
	if err != nil {
		return c, err
	}
	if !c.Active {
		return c, domain.ErrInactive
	}
	return c, nil
}
