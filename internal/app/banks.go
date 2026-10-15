package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ListBankAccounts returns bank accounts, optionally only those of one currency.
func (s *Service) ListBankAccounts(ctx context.Context, currency domain.CurrencyCode) ([]domain.BankAccount, error) {
	out, err := s.tx.Repos().Banks.List(ctx, currency)
	return out, wrap("app.ListBankAccounts", err)
}

// GetBankAccount returns one bank account.
func (s *Service) GetBankAccount(ctx context.Context, id string) (domain.BankAccount, error) {
	out, err := s.tx.Repos().Banks.Get(ctx, id)
	return out, wrap("app.GetBankAccount", err)
}

// CreateBankAccount attaches a new bank account to a currency. The first active account
// of a currency becomes its default automatically.
func (s *Service) CreateBankAccount(ctx context.Context, b domain.BankAccount) (domain.BankAccount, error) {
	b.ID = s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := b.Validate(); err != nil {
			return err
		}
		if _, err := r.Currencies.Get(ctx, b.Currency); err != nil {
			return err
		}
		existing, err := r.Banks.List(ctx, b.Currency)
		if err != nil {
			return err
		}
		if !hasDefault(existing) && b.Active {
			b.IsDefault = true
		}
		if b.IsDefault {
			if err := r.Banks.ClearDefault(ctx, b.Currency, b.ID); err != nil {
				return err
			}
		}
		return r.Banks.Create(ctx, b)
	})
	if err != nil {
		return domain.BankAccount{}, wrap("app.CreateBankAccount", err)
	}
	return s.GetBankAccount(ctx, b.ID)
}

// UpdateBankAccount edits a bank account. Its currency cannot change, because customers
// and invoices rely on it.
func (s *Service) UpdateBankAccount(ctx context.Context, b domain.BankAccount) (domain.BankAccount, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.Banks.Get(ctx, b.ID)
		if err != nil {
			return err
		}
		if b.Currency != cur.Currency {
			return &domain.ValidationError{Field: "currency", Reason: "cannot be changed; create a new account"}
		}
		if err := b.Validate(); err != nil {
			return err
		}
		if b.IsDefault {
			if err := r.Banks.ClearDefault(ctx, b.Currency, b.ID); err != nil {
				return err
			}
		}
		return r.Banks.Update(ctx, b)
	})
	if err != nil {
		return domain.BankAccount{}, wrap("app.UpdateBankAccount", err)
	}
	return s.GetBankAccount(ctx, b.ID)
}

// DeleteBankAccount removes an account no customer overrides to. Issued invoices keep
// their snapshot of the details.
func (s *Service) DeleteBankAccount(ctx context.Context, id string) error {
	return wrap("app.DeleteBankAccount", s.tx.Repos().Banks.Delete(ctx, id))
}

func hasDefault(accounts []domain.BankAccount) bool {
	for _, a := range accounts {
		if a.IsDefault {
			return true
		}
	}
	return false
}
