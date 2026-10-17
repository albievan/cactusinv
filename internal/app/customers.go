package app

import (
	"context"
	"fmt"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
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
		if err := checkCustomer(ctx, r, &c, nil); err != nil {
			return err
		}
		if err := r.Customers.Create(ctx, c); err != nil {
			return err
		}
		return s.audit(ctx, r, "customer.created", "customer", c.ID, c.Code+" "+c.Name)
	})
	if err != nil {
		return domain.Customer{}, wrap("app.CreateCustomer", err)
	}
	return s.GetCustomer(ctx, c.ID)
}

// UpdateCustomer edits a customer. A currency change affects future invoices only.
func (s *Service) UpdateCustomer(ctx context.Context, c domain.Customer) (domain.Customer, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.Customers.Get(ctx, c.ID)
		if err != nil {
			return err
		}
		if err := checkCustomer(ctx, r, &c, &cur); err != nil {
			return err
		}
		if c.Currency != cur.Currency {
			if err := noOpenInvoices(ctx, r, cur); err != nil {
				return err
			}
		}
		if err := r.Customers.Update(ctx, c); err != nil {
			return err
		}
		return s.audit(ctx, r, "customer.updated", "customer", c.ID, c.Code+" "+c.Name)
	})
	if err != nil {
		return domain.Customer{}, wrap("app.UpdateCustomer", err)
	}
	return s.GetCustomer(ctx, c.ID)
}

// DeleteCustomer removes a customer with no invoices or schedules; otherwise deactivate.
func (s *Service) DeleteCustomer(ctx context.Context, id string) error {
	return wrap("app.DeleteCustomer", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		c, err := r.Customers.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := r.Customers.Delete(ctx, id); err != nil {
			return err
		}
		return s.audit(ctx, r, "customer.deleted", "customer", id, c.Code+" "+c.Name)
	}))
}

// checkCustomer validates c; cur is the stored customer on an update (nil on create). A
// currency or bank account that was deactivated after it was chosen does not stop the
// customer being edited: only a newly chosen one must be active.
func checkCustomer(ctx context.Context, r ports.Repos, c *domain.Customer, cur *domain.Customer) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if cur == nil || c.Currency != cur.Currency {
		if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
			return err
		}
	} else if _, err := r.Currencies.Get(ctx, c.Currency); err != nil {
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
	if !b.Active && (cur == nil || cur.BankAccountID != c.BankAccountID) {
		return domain.ErrInactive
	}
	return nil
}

// noOpenInvoices refuses a currency change while the customer owes invoices in the old
// currency: a statement is in one currency, so they would drop off it.
func noOpenInvoices(ctx context.Context, r ports.Repos, c domain.Customer) error {
	open := 0
	for _, st := range []domain.InvoiceStatus{domain.InvoiceIssued, domain.InvoiceSent, domain.InvoiceOverdue} {
		_, n, err := r.Invoices.List(ctx, ports.InvoiceFilter{CustomerID: c.ID, Status: st, Page: ports.Page{Limit: 1}})
		if err != nil {
			return err
		}
		open += n
	}
	if open > 0 {
		return &domain.ValidationError{Field: "currency", Reason: fmt.Sprintf(
			"cannot change while %d invoice(s) in %s are unpaid: mark them paid or void them first", open, c.Currency)}
	}
	return nil
}
