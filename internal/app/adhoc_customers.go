package app

import (
	"context"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ListAdhocCustomers returns a page of ad-hoc customers, by name.
func (s *Service) ListAdhocCustomers(ctx context.Context, f ports.AdhocCustomerFilter) ([]domain.AdhocCustomer, int, error) {
	f.Page = f.Page.Normalize()
	out, n, err := s.tx.Repos().AdhocCustomers.List(ctx, f)
	return out, n, wrap("app.ListAdhocCustomers", err)
}

// GetAdhocCustomer returns one ad-hoc customer.
func (s *Service) GetAdhocCustomer(ctx context.Context, id string) (domain.AdhocCustomer, error) {
	out, err := s.tx.Repos().AdhocCustomers.Get(ctx, id)
	return out, wrap("app.GetAdhocCustomer", err)
}

// CreateAdhocCustomer adds an ad-hoc customer (a prospect to quote) in an active currency.
func (s *Service) CreateAdhocCustomer(ctx context.Context, a domain.AdhocCustomer) (domain.AdhocCustomer, error) {
	a.ID, a.CustomerID, a.ConvertedAt = s.ids.NewID(), "", time.Time{}
	a.CreatedAt, a.UpdatedAt = s.now(), s.now()
	if err := a.Validate(); err != nil {
		return domain.AdhocCustomer{}, wrap("app.CreateAdhocCustomer", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if _, err := activeCurrency(ctx, r, a.Currency); err != nil {
			return err
		}
		if err := r.AdhocCustomers.Create(ctx, a); err != nil {
			return err
		}
		return s.audit(ctx, r, "adhoc_customer.created", "adhoc_customer", a.ID, a.Name)
	})
	if err != nil {
		return domain.AdhocCustomer{}, wrap("app.CreateAdhocCustomer", err)
	}
	return s.GetAdhocCustomer(ctx, a.ID)
}

// UpdateAdhocCustomer edits an ad-hoc customer that has not been made a customer. A
// currency change needs it to have no quotes yet (they are priced in the old one).
func (s *Service) UpdateAdhocCustomer(ctx context.Context, a domain.AdhocCustomer) (domain.AdhocCustomer, error) {
	if err := a.Validate(); err != nil {
		return domain.AdhocCustomer{}, wrap("app.UpdateAdhocCustomer", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.AdhocCustomers.Get(ctx, a.ID)
		if err != nil {
			return err
		}
		if cur.Version != a.Version {
			return domain.ErrConflict
		}
		if cur.Converted() {
			return domain.ErrInvalidTransition
		}
		if a.Currency != cur.Currency {
			n, err := r.Quotes.CountForAdhoc(ctx, a.ID)
			if err != nil {
				return err
			}
			if n > 0 {
				return &domain.ValidationError{Field: "currency", Reason: "cannot change: this ad-hoc customer already has quotes in " + string(cur.Currency)}
			}
			if _, err := activeCurrency(ctx, r, a.Currency); err != nil {
				return err
			}
		}
		a.CustomerID, a.ConvertedAt, a.CreatedAt, a.UpdatedAt = "", time.Time{}, cur.CreatedAt, s.now()
		if err := r.AdhocCustomers.Update(ctx, a); err != nil {
			return err
		}
		return s.audit(ctx, r, "adhoc_customer.updated", "adhoc_customer", a.ID, a.Name)
	})
	if err != nil {
		return domain.AdhocCustomer{}, wrap("app.UpdateAdhocCustomer", err)
	}
	return s.GetAdhocCustomer(ctx, a.ID)
}

// DeleteAdhocCustomer removes an ad-hoc customer no quote was made out to.
func (s *Service) DeleteAdhocCustomer(ctx context.Context, id string) error {
	return wrap("app.DeleteAdhocCustomer", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		a, err := r.AdhocCustomers.Get(ctx, id)
		if err != nil {
			return err
		}
		n, err := r.Quotes.CountForAdhoc(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return &domain.ValidationError{Field: "id", Reason: "has quotes: it stays as the record of who they were made out to"}
		}
		if err := r.AdhocCustomers.Delete(ctx, id); err != nil {
			return err
		}
		return s.audit(ctx, r, "adhoc_customer.deleted", "adhoc_customer", id, a.Name)
	}))
}

// ConvertAdhocCustomer makes a real customer of an ad-hoc customer whose quote turned into
// a sale: a customer with their details plus what a customer needs (an invoice code, an
// e-mail address, payment terms). Every quote made out to them then belongs to the
// customer, so it can be converted into an invoice. Needs the right to add customers.
func (s *Service) ConvertAdhocCustomer(ctx context.Context, id string, d domain.CustomerDetails) (domain.Customer, error) {
	if !ActorFrom(ctx).Can(domain.PermCustomersCreate) {
		return domain.Customer{}, wrap("app.ConvertAdhocCustomer", domain.ErrNoAccess)
	}
	customerID := s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		a, err := r.AdhocCustomers.Get(ctx, id)
		if err != nil {
			return err
		}
		c, err := a.ToCustomer(customerID, d)
		if err != nil {
			return err
		}
		if err := checkCustomer(ctx, r, &c, nil); err != nil {
			return err
		}
		if err := r.Customers.Create(ctx, c); err != nil {
			return err
		}
		if err := s.audit(ctx, r, "customer.created", "customer", c.ID, c.Code+" "+c.Name+" (from ad-hoc customer)"); err != nil {
			return err
		}
		if err := a.MarkConverted(c.ID, s.now()); err != nil {
			return err
		}
		if err := r.AdhocCustomers.Update(ctx, a); err != nil {
			return err
		}
		if err := r.Quotes.SetCustomerForAdhoc(ctx, a.ID, c.ID, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, r, "adhoc_customer.converted", "adhoc_customer", a.ID, a.Name+" became customer "+c.Code)
	})
	if err != nil {
		return domain.Customer{}, wrap("app.ConvertAdhocCustomer", err)
	}
	return s.GetCustomer(ctx, customerID)
}
