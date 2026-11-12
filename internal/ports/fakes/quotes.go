package fakes

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ---- quotes ----

type quotes struct{ s *Store }

func cloneQuote(q domain.Quote) *domain.Quote {
	q.Lines = slices.Clone(q.Lines)
	return &q
}

func (r quotes) List(_ context.Context, f ports.QuoteFilter) ([]domain.Quote, int, error) {
	var out []domain.Quote
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.Quote
		for _, q := range st.quotes {
			if (f.Status != "" && q.Status != f.Status) || (f.CustomerID != "" && q.CustomerID != f.CustomerID) ||
				(f.AdhocCustomerID != "" && q.AdhocCustomerID != f.AdhocCustomerID) ||
				(!contains(q.Number, f.Query) && !contains(q.BillTo.Name, f.Query)) || (f.Expired && !q.Expired(f.Today)) {
				continue
			}
			q.Lines = nil
			all = append(all, q)
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].CreatedAt.After(all[j].CreatedAt) || (all[i].CreatedAt.Equal(all[j].CreatedAt) && all[i].ID > all[j].ID)
		})
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r quotes) Get(_ context.Context, id string) (*domain.Quote, error) {
	var out *domain.Quote
	err := r.s.do(func(st *state) error {
		q, ok := st.quotes[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = cloneQuote(q)
		return nil
	})
	return out, err
}

func (r quotes) GetByInvoice(_ context.Context, invoiceID string) (*domain.Quote, error) {
	var out *domain.Quote
	err := r.s.do(func(st *state) error {
		for _, q := range st.quotes {
			if invoiceID != "" && q.InvoiceID == invoiceID {
				out = cloneQuote(q)
				return nil
			}
		}
		return domain.ErrNotFound
	})
	return out, err
}

func (r quotes) Create(_ context.Context, q *domain.Quote) error {
	return r.s.do(func(st *state) error {
		if q.CustomerID != "" {
			if _, ok := st.customers[q.CustomerID]; !ok {
				return domain.ErrNotFound
			}
		}
		if q.AdhocCustomerID != "" {
			if _, ok := st.adhoc[q.AdhocCustomerID]; !ok {
				return domain.ErrNotFound
			}
		}
		for _, o := range st.quotes {
			if o.Number == q.Number {
				return domain.ErrConflict
			}
		}
		q.Version = 1
		st.quotes[q.ID] = *cloneQuote(*q)
		return nil
	})
}

func (r quotes) Update(_ context.Context, q *domain.Quote) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.quotes[q.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != q.Version {
			return domain.ErrConflict
		}
		q.Version++
		st.quotes[q.ID] = *cloneQuote(*q)
		return nil
	})
}

func (r quotes) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.quotes[id]; !ok {
			return domain.ErrNotFound
		}
		delete(st.quotes, id)
		for mid, m := range st.emails { // email_outbox.quote_id ON DELETE CASCADE
			if m.QuoteID == id {
				delete(st.emails, mid)
			}
		}
		return nil
	})
}

func (r quotes) SetCustomerForAdhoc(_ context.Context, adhocID, customerID string, now time.Time) error {
	return r.s.do(func(st *state) error {
		for id, q := range st.quotes {
			if q.AdhocCustomerID == adhocID && q.CustomerID == "" {
				q.CustomerBecame(customerID, now)
				q.Version++
				st.quotes[id] = q
			}
		}
		return nil
	})
}

func (r quotes) CountForAdhoc(_ context.Context, adhocID string) (int, error) {
	n := 0
	err := r.s.do(func(st *state) error {
		for _, q := range st.quotes {
			if q.AdhocCustomerID == adhocID {
				n++
			}
		}
		return nil
	})
	return n, err
}

// ---- ad-hoc customers ----

type adhocCustomers struct{ s *Store }

func (r adhocCustomers) List(_ context.Context, f ports.AdhocCustomerFilter) ([]domain.AdhocCustomer, int, error) {
	var out []domain.AdhocCustomer
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.AdhocCustomer
		for _, a := range st.adhoc {
			if (!f.IncludeConverted && a.Converted()) || (!contains(a.Name, f.Query) && !contains(a.Email, f.Query)) {
				continue
			}
			all = append(all, a)
		}
		sort.Slice(all, func(i, j int) bool {
			if !strings.EqualFold(all[i].Name, all[j].Name) {
				return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
			}
			return all[i].ID < all[j].ID
		})
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r adhocCustomers) Get(_ context.Context, id string) (domain.AdhocCustomer, error) {
	var out domain.AdhocCustomer
	err := r.s.do(func(st *state) error {
		a, ok := st.adhoc[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = a
		return nil
	})
	return out, err
}

func (r adhocCustomers) Create(_ context.Context, a domain.AdhocCustomer) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.currencies[a.Currency]; !ok {
			return domain.ErrNotFound
		}
		a.Version = 1
		st.adhoc[a.ID] = a
		return nil
	})
}

func (r adhocCustomers) Update(_ context.Context, a domain.AdhocCustomer) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.adhoc[a.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != a.Version {
			return domain.ErrConflict
		}
		a.Version++
		st.adhoc[a.ID] = a
		return nil
	})
}

func (r adhocCustomers) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.adhoc[id]; !ok {
			return domain.ErrNotFound
		}
		for _, q := range st.quotes {
			if q.AdhocCustomerID == id {
				return domain.ErrConflict
			}
		}
		delete(st.adhoc, id)
		return nil
	})
}

// ---- quote settings and numbers ----

type quoteSettings struct{ s *Store }

func (r quoteSettings) Get(_ context.Context) (domain.QuoteSettings, error) {
	var out domain.QuoteSettings
	err := r.s.do(func(st *state) error {
		if st.quoteSet == nil {
			return domain.ErrNotFound
		}
		out = *st.quoteSet
		return nil
	})
	return out, err
}

func (r quoteSettings) Save(_ context.Context, q domain.QuoteSettings) error {
	return r.s.do(func(st *state) error {
		cur := 0
		if st.quoteSet != nil {
			cur = st.quoteSet.Version
		}
		if q.Version != cur {
			return domain.ErrConflict
		}
		q.Version++
		st.quoteSet = &q
		return nil
	})
}

type quoteSequences struct{ s *Store }

func (r quoteSequences) Next(_ context.Context) (int64, error) {
	var v int64
	err := r.s.do(func(st *state) error {
		v = st.quoteSeq
		st.quoteSeq++
		return nil
	})
	return v, err
}

// Lock does nothing in the fake (transactions are serialised).
func (r quoteSequences) Lock(context.Context) error { return nil }

func (r quoteSequences) Peek(_ context.Context) (int64, error) {
	var v int64
	err := r.s.do(func(st *state) error {
		v = st.quoteSeq
		return nil
	})
	return v, err
}

func (r quoteSequences) Set(_ context.Context, next int64) error {
	return r.s.do(func(st *state) error {
		st.quoteSeq = next
		return nil
	})
}
