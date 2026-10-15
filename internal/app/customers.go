package app

import (
	"context"

	"github.com/albievan/cactusinve/internal/domain"
	"github.com/albievan/cactusinve/internal/ports"
)

// ListCustomers returns a page of customers and the total count.
func (s *Service) ListCustomers(ctx context.Context, f ports.CustomerFilter) ([]domain.Customer, int, error) {
	f.Page = f.Page.Normalize()
	out, n, err := s.tx.Repos().Customers.List(ctx, f)
	return out, n, wrap("app.ListCustomers", err)
}

// GetCustomer returns one customer.
func (s *Service) GetCustomer(ctx context.Context, id string) (domain.Customer, error) {
	out, err := s.tx.Repos().Customers.Get(ctx, id)
	return out, wrap("app.GetCustomer", err)
}

// CreateCustomer adds a customer billed in an existing, active currency.
func (s *Service) CreateCustomer(ctx context.Context, c domain.Customer) (domain.Customer, error) {
	c.ID = s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := checkCustomer(ctx, r, &c); err != nil {
			return err
		}
		return r.Customers.Create(ctx, c)
	})
	if err != nil {
		return domain.Customer{}, wrap("app.CreateCustomer", err)
	}
	return s.GetCustomer(ctx, c.ID)
}

// UpdateCustomer edits a customer. A currency change affects future invoices only.
func (s *Service) UpdateCustomer(ctx context.Context, c domain.Customer) (domain.Customer, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if _, err := r.Customers.Get(ctx, c.ID); err != nil {
			return err
		}
		if err := checkCustomer(ctx, r, &c); err != nil {
			return err
		}
		return r.Customers.Update(ctx, c)
	})
	if err != nil {
		return domain.Customer{}, wrap("app.UpdateCustomer", err)
	}
	return s.GetCustomer(ctx, c.ID)
}

// DeleteCustomer removes a customer with no invoices or schedules; otherwise deactivate.
func (s *Service) DeleteCustomer(ctx context.Context, id string) error {
	return wrap("app.DeleteCustomer", s.tx.Repos().Customers.Delete(ctx, id))
}

func checkCustomer(ctx context.Context, r ports.Repos, c *domain.Customer) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
		return err
	}
	if c.BankAccountID == "" {
		return nil
	}
	b, err := r.Banks.Get(ctx, c.BankAccountID)
	if err != nil {
		return err
	}
	if b.Currency != c.Currency {
		return &domain.ValidationError{Field: "bankAccountId", Reason: "bank account must be in the customer's currency"}
	}
	if !b.Active {
		return domain.ErrInactive
	}
	return nil
}
