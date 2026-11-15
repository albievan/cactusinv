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
		if err := s.audit(ctx, r, "adhoc_customer.updated", "adhoc_customer", a.ID, a.Name); err != nil {
			return err
		}
		return s.refreshAdhocQuotes(ctx, r, a)
	})
	if err != nil {
		return domain.AdhocCustomer{}, wrap("app.UpdateAdhocCustomer", err)
	}
	return s.GetAdhocCustomer(ctx, a.ID)
}

// refreshAdhocQuotes prints the edited ad-hoc customer's details on its quotes that can still
// change (draft and sent, not expired; M50): fixing a name or address fixes the open quotes.
func (s *Service) refreshAdhocQuotes(ctx context.Context, r ports.Repos, a domain.AdhocCustomer) error {
	b, err := s.business(ctx, r)
	if err != nil {
		return err
	}
	today, party := s.today(b), domain.Party{Name: a.Name, Address: a.Address, Attention: a.Attention}
	for _, status := range []domain.QuoteStatus{domain.QuoteDraft, domain.QuoteSent} {
		var ids []string
		for page := (ports.Page{Limit: 200}); ; page.Offset += page.Limit {
			qs, total, err := r.Quotes.List(ctx, ports.QuoteFilter{AdhocCustomerID: a.ID, Status: status, Page: page})
			if err != nil {
				return err
			}
			for _, q := range qs {
				ids = append(ids, q.ID)
			}
			if len(qs) == 0 || page.Offset+len(qs) >= total {
				break
			}
		}
		for _, id := range ids {
			q, err := r.Quotes.Get(ctx, id) // with its lines
			if err != nil {
				return err
			}
			changed, err := q.PartyChanged(party, today, s.now())
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if err := r.Quotes.Update(ctx, q); err != nil {
				return err
			}
			if err := s.quoteEvent(ctx, r, q.ID, "updated", q.Number+": the ad-hoc customer's details changed"); err != nil {
				return err
			}
		}
	}
	return nil
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
		if c.Currency != a.Currency {
			// Their quotes are priced in the ad-hoc customer's currency and become the
			// customer's: an invoice made from one must be in the same currency.
			_, n, err := r.Quotes.List(ctx, ports.QuoteFilter{AdhocCustomerID: a.ID, Page: ports.Page{Limit: 1}})
			if err != nil {
				return err
			}
			if n > 0 {
				return &domain.ValidationError{Field: "currency", Reason: "must stay " + string(a.Currency) + ": their quotes are in " + string(a.Currency)}
			}
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
