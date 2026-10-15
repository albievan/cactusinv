// Package fakes provides in-memory implementations of every port for unit tests.
package fakes

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
)

// Clock is a settable fake clock.
type Clock struct {
	mu sync.Mutex
	T  time.Time
}

// Now implements ports.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.T
}

// Set moves the clock.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.T = t
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

// IDs is a deterministic ID generator: id-1, id-2, …
type IDs struct {
	mu sync.Mutex
	n  int
}

// NewID implements ports.IDGenerator.
func (g *IDs) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("id-%04d", g.n)
}

// state is everything the fake database holds.
type state struct {
	business    *domain.BusinessProfile
	currencies  map[domain.CurrencyCode]domain.Currency
	banks       map[string]domain.BankAccount
	customers   map[string]domain.Customer
	products    map[string]domain.Product
	invoices    map[string]domain.Invoice
	events      []domain.InvoiceEvent
	seq         int64
	schedules   map[string]domain.Schedule
	runs        []domain.ScheduleRun
	emails      map[string]domain.EmailMessage
	users       map[string]domain.User
	sessions    map[string]domain.Session
	idempotency map[string]ports.IdempotencyRecord
	exported    map[string]int
}

func newState() *state {
	return &state{
		currencies: map[domain.CurrencyCode]domain.Currency{}, banks: map[string]domain.BankAccount{},
		customers: map[string]domain.Customer{}, products: map[string]domain.Product{},
		invoices: map[string]domain.Invoice{}, seq: 1, schedules: map[string]domain.Schedule{},
		emails: map[string]domain.EmailMessage{}, users: map[string]domain.User{},
		sessions: map[string]domain.Session{}, idempotency: map[string]ports.IdempotencyRecord{},
		exported: map[string]int{},
	}
}

func (s *state) clone() *state {
	c := *s
	if s.business != nil {
		b := *s.business
		c.business = &b
	}
	c.currencies = maps.Clone(s.currencies)
	c.banks = maps.Clone(s.banks)
	c.customers = maps.Clone(s.customers)
	c.products = maps.Clone(s.products)
	c.invoices = maps.Clone(s.invoices)
	c.events = slices.Clone(s.events)
	c.schedules = maps.Clone(s.schedules)
	c.runs = slices.Clone(s.runs)
	c.emails = maps.Clone(s.emails)
	c.users = maps.Clone(s.users)
	c.sessions = maps.Clone(s.sessions)
	c.idempotency = maps.Clone(s.idempotency)
	c.exported = maps.Clone(s.exported)
	return &c
}

// Store is an in-memory database implementing ports.TxManager. Transactions are
// serialised and rolled back on error. Set FailNext to inject a failure into the next
// repository call.
type Store struct {
	mu       sync.Mutex
	st       *state
	FailNext error
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{st: newState()} }

// WithinTx implements ports.TxManager.
func (s *Store) WithinTx(ctx context.Context, fn func(context.Context, ports.Repos) error) error {
	s.mu.Lock()
	snapshot := s.st.clone()
	s.mu.Unlock()
	if err := fn(ctx, s.Repos()); err != nil {
		s.mu.Lock()
		s.st = snapshot
		s.mu.Unlock()
		return err
	}
	return nil
}

// Repos implements ports.TxManager.
func (s *Store) Repos() ports.Repos {
	return ports.Repos{
		Business: business{s}, Currencies: currencies{s}, Banks: banks{s}, Customers: customers{s},
		Products: products{s}, Invoices: invoices{s}, Sequences: sequences{s}, Schedules: schedules{s},
		Emails: emails{s}, Users: users{s}, Sessions: sessions{s}, Idempotency: idempotency{s},
	}
}

// do runs f under the lock, honouring FailNext.
func (s *Store) do(f func(st *state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailNext != nil {
		err := s.FailNext
		s.FailNext = nil
		return err
	}
	return f(s.st)
}

// Emails returns a copy of every outbox message (test inspection).
func (s *Store) Emails() []domain.EmailMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Collect(maps.Values(s.st.emails))
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Events returns every invoice audit event (test inspection).
func (s *Store) Events() []domain.InvoiceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.st.events)
}

func page[T any](items []T, p ports.Page) ([]T, int) {
	p = p.Normalize()
	total := len(items)
	if p.Offset >= total {
		return []T{}, total
	}
	return items[p.Offset:min(total, p.Offset+p.Limit)], total
}

func contains(haystack, q string) bool {
	return q == "" || strings.Contains(strings.ToLower(haystack), strings.ToLower(q))
}

// ---- business ----

type business struct{ s *Store }

func (r business) Get(_ context.Context) (domain.BusinessProfile, error) {
	var out domain.BusinessProfile
	err := r.s.do(func(st *state) error {
		if st.business == nil {
			return domain.ErrNotFound
		}
		out = *st.business
		return nil
	})
	return out, err
}

func (r business) Save(_ context.Context, b domain.BusinessProfile) error {
	return r.s.do(func(st *state) error {
		cur := 0
		if st.business != nil {
			cur = st.business.Version
		}
		if cur != b.Version {
			return domain.ErrConflict
		}
		b.Version++
		st.business = &b
		return nil
	})
}

// ---- currencies ----

type currencies struct{ s *Store }

func (r currencies) List(_ context.Context) ([]domain.Currency, error) {
	var out []domain.Currency
	err := r.s.do(func(st *state) error {
		out = slices.Collect(maps.Values(st.currencies))
		sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
		return nil
	})
	return out, err
}

func (r currencies) Get(_ context.Context, code domain.CurrencyCode) (domain.Currency, error) {
	var out domain.Currency
	err := r.s.do(func(st *state) error {
		c, ok := st.currencies[code]
		if !ok {
			return domain.ErrNotFound
		}
		out = c
		return nil
	})
	return out, err
}

func (r currencies) Create(_ context.Context, c domain.Currency) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.currencies[c.Code]; ok {
			return domain.ErrConflict
		}
		st.currencies[c.Code] = c
		return nil
	})
}

func (r currencies) Update(_ context.Context, c domain.Currency) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.currencies[c.Code]; !ok {
			return domain.ErrNotFound
		}
		st.currencies[c.Code] = c
		return nil
	})
}

// ---- bank accounts ----

type banks struct{ s *Store }

func (r banks) List(_ context.Context, currency domain.CurrencyCode) ([]domain.BankAccount, error) {
	var out []domain.BankAccount
	err := r.s.do(func(st *state) error {
		for _, b := range st.banks {
			if currency == "" || b.Currency == currency {
				out = append(out, b)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	return out, err
}

func (r banks) Get(_ context.Context, id string) (domain.BankAccount, error) {
	var out domain.BankAccount
	err := r.s.do(func(st *state) error {
		b, ok := st.banks[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = b
		return nil
	})
	return out, err
}

func (r banks) Create(_ context.Context, b domain.BankAccount) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.currencies[b.Currency]; !ok {
			return domain.ErrNotFound
		}
		for _, o := range st.banks {
			if b.IsDefault && o.IsDefault && o.Currency == b.Currency {
				return domain.ErrConflict
			}
		}
		b.Version = 1
		st.banks[b.ID] = b
		return nil
	})
}

func (r banks) Update(_ context.Context, b domain.BankAccount) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.banks[b.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != b.Version {
			return domain.ErrConflict
		}
		for _, o := range st.banks {
			if o.ID != b.ID && b.IsDefault && o.IsDefault && o.Currency == b.Currency {
				return domain.ErrConflict
			}
		}
		b.Version++
		st.banks[b.ID] = b
		return nil
	})
}

func (r banks) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.banks[id]; !ok {
			return domain.ErrNotFound
		}
		for _, c := range st.customers {
			if c.BankAccountID == id {
				return domain.ErrConflict
			}
		}
		delete(st.banks, id)
		return nil
	})
}

func (r banks) ClearDefault(_ context.Context, currency domain.CurrencyCode, keepID string) error {
	return r.s.do(func(st *state) error {
		for id, b := range st.banks {
			if b.Currency == currency && id != keepID && b.IsDefault {
				b.IsDefault = false
				b.Version++
				st.banks[id] = b
			}
		}
		return nil
	})
}

// ---- customers ----

type customers struct{ s *Store }

func (r customers) List(_ context.Context, f ports.CustomerFilter) ([]domain.Customer, int, error) {
	var out []domain.Customer
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.Customer
		for _, c := range st.customers {
			if (c.Active || f.IncludeInactive) && (contains(c.Name, f.Query) || contains(c.Code, f.Query)) {
				all = append(all, c)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r customers) Get(_ context.Context, id string) (domain.Customer, error) {
	var out domain.Customer
	err := r.s.do(func(st *state) error {
		c, ok := st.customers[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = c
		return nil
	})
	return out, err
}

func (r customers) Create(_ context.Context, c domain.Customer) error {
	return r.s.do(func(st *state) error {
		for _, o := range st.customers {
			if o.Code == c.Code {
				return domain.ErrConflict
			}
		}
		if _, ok := st.currencies[c.Currency]; !ok {
			return domain.ErrNotFound
		}
		c.Version = 1
		st.customers[c.ID] = c
		return nil
	})
}

func (r customers) Update(_ context.Context, c domain.Customer) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.customers[c.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != c.Version {
			return domain.ErrConflict
		}
		for _, o := range st.customers {
			if o.ID != c.ID && o.Code == c.Code {
				return domain.ErrConflict
			}
		}
		c.Version++
		st.customers[c.ID] = c
		return nil
	})
}

func (r customers) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.customers[id]; !ok {
			return domain.ErrNotFound
		}
		for _, inv := range st.invoices {
			if inv.CustomerID == id {
				return domain.ErrConflict
			}
		}
		for _, sc := range st.schedules {
			if sc.CustomerID == id {
				return domain.ErrConflict
			}
		}
		delete(st.customers, id)
		return nil
	})
}

// ---- products ----

type products struct{ s *Store }

func cloneProduct(p domain.Product) domain.Product {
	p.Prices = maps.Clone(p.Prices)
	return p
}

func (r products) List(_ context.Context, f ports.ProductFilter) ([]domain.Product, int, error) {
	var out []domain.Product
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.Product
		for _, p := range st.products {
			if !p.Active && !f.IncludeInactive {
				continue
			}
			if !contains(p.Name, f.Query) && !contains(p.SKU, f.Query) {
				continue
			}
			if _, ok := p.Prices[f.Currency]; f.Currency != "" && !ok {
				continue
			}
			all = append(all, cloneProduct(p))
		}
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r products) Get(_ context.Context, id string) (domain.Product, error) {
	var out domain.Product
	err := r.s.do(func(st *state) error {
		p, ok := st.products[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = cloneProduct(p)
		return nil
	})
	return out, err
}

func (r products) GetMany(_ context.Context, ids []string) (map[string]domain.Product, error) {
	out := map[string]domain.Product{}
	err := r.s.do(func(st *state) error {
		for _, id := range ids {
			if p, ok := st.products[id]; ok {
				out[id] = cloneProduct(p)
			}
		}
		return nil
	})
	return out, err
}

func (r products) Create(_ context.Context, p domain.Product) error {
	return r.s.do(func(st *state) error {
		for _, o := range st.products {
			if o.SKU == p.SKU {
				return domain.ErrConflict
			}
		}
		for c := range p.Prices {
			if _, ok := st.currencies[c]; !ok {
				return domain.ErrNotFound
			}
		}
		p.Version = 1
		st.products[p.ID] = cloneProduct(p)
		return nil
	})
}

func (r products) Update(_ context.Context, p domain.Product) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.products[p.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != p.Version {
			return domain.ErrConflict
		}
		for _, o := range st.products {
			if o.ID != p.ID && o.SKU == p.SKU {
				return domain.ErrConflict
			}
		}
		p.Version++
		st.products[p.ID] = cloneProduct(p)
		return nil
	})
}

func (r products) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.products[id]; !ok {
			return domain.ErrNotFound
		}
		delete(st.products, id)
		return nil
	})
}

// ---- invoices ----

type invoices struct{ s *Store }

func cloneInvoice(inv domain.Invoice) *domain.Invoice {
	inv.Lines = slices.Clone(inv.Lines)
	return &inv
}

func (r invoices) List(_ context.Context, f ports.InvoiceFilter) ([]domain.Invoice, int, error) {
	var out []domain.Invoice
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.Invoice
		for _, inv := range st.invoices {
			if f.Status != "" && inv.Status != f.Status {
				continue
			}
			if f.CustomerID != "" && inv.CustomerID != f.CustomerID {
				continue
			}
			if !contains(inv.Number, f.Query) && !contains(inv.BillTo.Name, f.Query) {
				continue
			}
			if f.Overdue && !inv.IsOverdue(f.Today) {
				continue
			}
			inv.Lines = nil
			all = append(all, inv)
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].CreatedAt.After(all[j].CreatedAt) || (all[i].CreatedAt.Equal(all[j].CreatedAt) && all[i].ID > all[j].ID)
		})
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r invoices) Get(_ context.Context, id string) (*domain.Invoice, error) {
	var out *domain.Invoice
	err := r.s.do(func(st *state) error {
		inv, ok := st.invoices[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = cloneInvoice(inv)
		return nil
	})
	return out, err
}

func (r invoices) Create(_ context.Context, inv *domain.Invoice) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.customers[inv.CustomerID]; !ok {
			return domain.ErrNotFound
		}
		inv.Version = 1
		st.invoices[inv.ID] = *cloneInvoice(*inv)
		return nil
	})
}

func (r invoices) Update(_ context.Context, inv *domain.Invoice) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.invoices[inv.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != inv.Version {
			return domain.ErrConflict
		}
		if inv.Number != "" {
			for _, o := range st.invoices {
				if o.ID != inv.ID && o.Number == inv.Number {
					return domain.ErrConflict
				}
			}
		}
		inv.Version++
		st.invoices[inv.ID] = *cloneInvoice(*inv)
		return nil
	})
}

func (r invoices) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.invoices[id]; !ok {
			return domain.ErrNotFound
		}
		delete(st.invoices, id)
		return nil
	})
}

func (r invoices) AddEvent(_ context.Context, ev domain.InvoiceEvent) error {
	return r.s.do(func(st *state) error {
		st.events = append(st.events, ev)
		return nil
	})
}

func (r invoices) ListEvents(_ context.Context, invoiceID string) ([]domain.InvoiceEvent, error) {
	var out []domain.InvoiceEvent
	err := r.s.do(func(st *state) error {
		for _, e := range st.events {
			if e.InvoiceID == invoiceID {
				out = append(out, e)
			}
		}
		return nil
	})
	return out, err
}

func (r invoices) Totals(_ context.Context, today domain.Date) ([]ports.CurrencyTotals, error) {
	var out []ports.CurrencyTotals
	err := r.s.do(func(st *state) error {
		by := map[domain.CurrencyCode]*ports.CurrencyTotals{}
		for _, inv := range st.invoices {
			if inv.Status != domain.InvoiceIssued && inv.Status != domain.InvoiceSent {
				continue
			}
			t := by[inv.Currency]
			if t == nil {
				t = &ports.CurrencyTotals{Currency: inv.Currency}
				by[inv.Currency] = t
			}
			t.OpenCount++
			t.Outstanding += inv.Totals.Total
			if inv.IsOverdue(today) {
				t.OverdueCount++
				t.Overdue += inv.Totals.Total
			}
		}
		for _, t := range by {
			out = append(out, *t)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
		return nil
	})
	return out, err
}

func (r invoices) ListUnexported(_ context.Context, limit int) ([]ports.ExportRef, error) {
	var out []ports.ExportRef
	err := r.s.do(func(st *state) error {
		for id, inv := range st.invoices {
			if inv.Status != domain.InvoiceDraft && st.exported[id] != inv.Version && len(out) < limit {
				out = append(out, ports.ExportRef{ID: id, Version: inv.Version})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	return out, err
}

func (r invoices) MarkExported(_ context.Context, id string, version int) error {
	return r.s.do(func(st *state) error {
		st.exported[id] = version
		return nil
	})
}

func (r invoices) CountForCustomer(_ context.Context, customerID string) (int, error) {
	n := 0
	err := r.s.do(func(st *state) error {
		for _, inv := range st.invoices {
			if inv.CustomerID == customerID {
				n++
			}
		}
		return nil
	})
	return n, err
}

// ---- sequences ----

type sequences struct{ s *Store }

func (r sequences) Next(_ context.Context) (int64, error) {
	var v int64
	err := r.s.do(func(st *state) error {
		v = st.seq
		st.seq++
		return nil
	})
	return v, err
}

func (r sequences) Peek(_ context.Context) (int64, error) {
	var v int64
	err := r.s.do(func(st *state) error {
		v = st.seq
		return nil
	})
	return v, err
}

func (r sequences) Set(_ context.Context, next int64) error {
	return r.s.do(func(st *state) error {
		st.seq = next
		return nil
	})
}

// ---- schedules ----

type schedules struct{ s *Store }

func cloneSchedule(sc domain.Schedule) *domain.Schedule {
	sc.Lines = slices.Clone(sc.Lines)
	return &sc
}

func (r schedules) List(_ context.Context, f ports.ScheduleFilter) ([]domain.Schedule, int, error) {
	var out []domain.Schedule
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.Schedule
		for _, sc := range st.schedules {
			if (f.CustomerID == "" || sc.CustomerID == f.CustomerID) && (f.Status == "" || sc.Status == f.Status) {
				all = append(all, *cloneSchedule(sc))
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r schedules) Get(_ context.Context, id string) (*domain.Schedule, error) {
	var out *domain.Schedule
	err := r.s.do(func(st *state) error {
		sc, ok := st.schedules[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = cloneSchedule(sc)
		return nil
	})
	return out, err
}

func (r schedules) Create(_ context.Context, sc *domain.Schedule) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.customers[sc.CustomerID]; !ok {
			return domain.ErrNotFound
		}
		sc.Version = 1
		st.schedules[sc.ID] = *cloneSchedule(*sc)
		return nil
	})
}

func (r schedules) Update(_ context.Context, sc *domain.Schedule) error {
	return r.s.do(func(st *state) error {
		cur, ok := st.schedules[sc.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if cur.Version != sc.Version {
			return domain.ErrConflict
		}
		sc.Version++
		st.schedules[sc.ID] = *cloneSchedule(*sc)
		return nil
	})
}

func (r schedules) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.schedules[id]; !ok {
			return domain.ErrNotFound
		}
		delete(st.schedules, id)
		return nil
	})
}

func (r schedules) ClaimDue(_ context.Context, now time.Time, limit int) ([]*domain.Schedule, error) {
	var out []*domain.Schedule
	err := r.s.do(func(st *state) error {
		var all []domain.Schedule
		for _, sc := range st.schedules {
			if sc.IsDue(now) {
				all = append(all, sc)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].NextRunAt.Before(all[j].NextRunAt) })
		for i := 0; i < len(all) && i < limit; i++ {
			out = append(out, cloneSchedule(all[i]))
		}
		return nil
	})
	return out, err
}

func (r schedules) InsertRun(_ context.Context, run domain.ScheduleRun) error {
	return r.s.do(func(st *state) error {
		for _, x := range st.runs {
			if x.ScheduleID == run.ScheduleID && x.Period == run.Period {
				return domain.ErrConflict
			}
		}
		st.runs = append(st.runs, run)
		return nil
	})
}

func (r schedules) ListRuns(_ context.Context, scheduleID string, limit int) ([]domain.ScheduleRun, error) {
	var out []domain.ScheduleRun
	err := r.s.do(func(st *state) error {
		for i := len(st.runs) - 1; i >= 0 && len(out) < limit; i-- {
			if st.runs[i].ScheduleID == scheduleID {
				out = append(out, st.runs[i])
			}
		}
		return nil
	})
	return out, err
}

// ---- e-mail outbox ----

type emails struct{ s *Store }

func cloneEmail(m domain.EmailMessage) *domain.EmailMessage {
	m.To = slices.Clone(m.To)
	m.CC = slices.Clone(m.CC)
	return &m
}

func (r emails) Create(_ context.Context, m *domain.EmailMessage) error {
	return r.s.do(func(st *state) error {
		if m.IdempotencyKey != "" {
			for _, o := range st.emails {
				if o.IdempotencyKey == m.IdempotencyKey {
					return domain.ErrConflict
				}
			}
		}
		st.emails[m.ID] = *cloneEmail(*m)
		return nil
	})
}

func (r emails) Get(_ context.Context, id string) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := r.s.do(func(st *state) error {
		m, ok := st.emails[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = cloneEmail(m)
		return nil
	})
	return out, err
}

func (r emails) GetByIdempotencyKey(_ context.Context, key string) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := r.s.do(func(st *state) error {
		for _, m := range st.emails {
			if m.IdempotencyKey == key {
				out = cloneEmail(m)
				return nil
			}
		}
		return domain.ErrNotFound
	})
	return out, err
}

func (r emails) Update(_ context.Context, m *domain.EmailMessage) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.emails[m.ID]; !ok {
			return domain.ErrNotFound
		}
		st.emails[m.ID] = *cloneEmail(*m)
		return nil
	})
}

func (r emails) List(_ context.Context, f ports.EmailFilter) ([]domain.EmailMessage, int, error) {
	var out []domain.EmailMessage
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.EmailMessage
		for _, m := range st.emails {
			if (f.Status == "" || m.Status == f.Status) && (f.InvoiceID == "" || m.InvoiceID == f.InvoiceID) {
				all = append(all, *cloneEmail(m))
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r emails) ClaimDue(_ context.Context, now time.Time, limit int) ([]*domain.EmailMessage, error) {
	var out []*domain.EmailMessage
	err := r.s.do(func(st *state) error {
		var all []domain.EmailMessage
		for _, m := range st.emails {
			if m.Claimable(now) {
				all = append(all, m)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].NextAttemptAt.Before(all[j].NextAttemptAt) })
		for i := 0; i < len(all) && i < limit; i++ {
			out = append(out, cloneEmail(all[i]))
		}
		return nil
	})
	return out, err
}

func (r emails) Stats(_ context.Context) (ports.OutboxStats, error) {
	var out ports.OutboxStats
	err := r.s.do(func(st *state) error {
		for _, m := range st.emails {
			if m.Status == domain.EmailPending {
				out.Pending++
				if out.OldestPending.IsZero() || m.NextAttemptAt.Before(out.OldestPending) {
					out.OldestPending = m.NextAttemptAt
				}
			}
		}
		return nil
	})
	return out, err
}

// ---- users & sessions ----

type users struct{ s *Store }

func (r users) GetByEmail(_ context.Context, email string) (domain.User, error) {
	var out domain.User
	err := r.s.do(func(st *state) error {
		for _, u := range st.users {
			if strings.EqualFold(u.Email, email) {
				out = u
				return nil
			}
		}
		return domain.ErrNotFound
	})
	return out, err
}

func (r users) Get(_ context.Context, id string) (domain.User, error) {
	var out domain.User
	err := r.s.do(func(st *state) error {
		u, ok := st.users[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = u
		return nil
	})
	return out, err
}

func (r users) Create(_ context.Context, u domain.User) error {
	return r.s.do(func(st *state) error {
		for _, o := range st.users {
			if strings.EqualFold(o.Email, u.Email) {
				return domain.ErrConflict
			}
		}
		st.users[u.ID] = u
		return nil
	})
}

func (r users) UpdatePassword(_ context.Context, id, hash string) error {
	return r.s.do(func(st *state) error {
		u, ok := st.users[id]
		if !ok {
			return domain.ErrNotFound
		}
		u.PasswordHash = hash
		st.users[id] = u
		return nil
	})
}

func (r users) Count(_ context.Context) (int, error) {
	n := 0
	err := r.s.do(func(st *state) error {
		n = len(st.users)
		return nil
	})
	return n, err
}

type sessions struct{ s *Store }

func (r sessions) Create(_ context.Context, s domain.Session) error {
	return r.s.do(func(st *state) error {
		st.sessions[s.TokenHash] = s
		return nil
	})
}

func (r sessions) Get(_ context.Context, tokenHash string) (domain.Session, error) {
	var out domain.Session
	err := r.s.do(func(st *state) error {
		s, ok := st.sessions[tokenHash]
		if !ok {
			return domain.ErrNotFound
		}
		out = s
		return nil
	})
	return out, err
}

func (r sessions) Delete(_ context.Context, tokenHash string) error {
	return r.s.do(func(st *state) error {
		delete(st.sessions, tokenHash)
		return nil
	})
}

func (r sessions) DeleteExpired(_ context.Context, now time.Time) error {
	return r.s.do(func(st *state) error {
		for k, s := range st.sessions {
			if !s.ExpiresAt.After(now) {
				delete(st.sessions, k)
			}
		}
		return nil
	})
}

// ---- idempotency ----

type idempotency struct{ s *Store }

func (r idempotency) Reserve(_ context.Context, rec ports.IdempotencyRecord) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.idempotency[rec.Key]; ok {
			return domain.ErrConflict
		}
		st.idempotency[rec.Key] = rec
		return nil
	})
}

func (r idempotency) Get(_ context.Context, key string) (ports.IdempotencyRecord, error) {
	var out ports.IdempotencyRecord
	err := r.s.do(func(st *state) error {
		rec, ok := st.idempotency[key]
		if !ok {
			return domain.ErrNotFound
		}
		out = rec
		return nil
	})
	return out, err
}

func (r idempotency) Complete(_ context.Context, key string, status int, body []byte) error {
	return r.s.do(func(st *state) error {
		rec, ok := st.idempotency[key]
		if !ok {
			return domain.ErrNotFound
		}
		rec.Status = status
		rec.Body = slices.Clone(body)
		st.idempotency[key] = rec
		return nil
	})
}

func (r idempotency) Release(_ context.Context, key string) error {
	return r.s.do(func(st *state) error {
		delete(st.idempotency, key)
		return nil
	})
}

func (r idempotency) DeleteBefore(_ context.Context, t time.Time) error {
	return r.s.do(func(st *state) error {
		for k, rec := range st.idempotency {
			if rec.CreatedAt.Before(t) {
				delete(st.idempotency, k)
			}
		}
		return nil
	})
}
