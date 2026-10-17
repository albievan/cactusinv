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

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
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
	audit       []domain.AuditEntry
	seq         int64
	schedules   map[string]domain.Schedule
	runs        []domain.ScheduleRun
	emails      map[string]domain.EmailMessage
	users       map[string]domain.User
	groups      map[string]domain.Group
	sessions    map[string]domain.Session
	idempotency map[string]ports.IdempotencyRecord
	exported    map[string]int
	smtp        *domain.SMTPSettings
	smtpPass    []byte
	system      domain.SystemState
	backupRuns  []domain.BackupRun
}

func newState() *state {
	return &state{
		currencies: map[domain.CurrencyCode]domain.Currency{}, banks: map[string]domain.BankAccount{},
		customers: map[string]domain.Customer{}, products: map[string]domain.Product{},
		invoices: map[string]domain.Invoice{}, seq: 1, schedules: map[string]domain.Schedule{},
		emails: map[string]domain.EmailMessage{}, users: map[string]domain.User{},
		groups:   map[string]domain.Group{domain.AdministratorsGroupID: {ID: domain.AdministratorsGroupID, Name: "Administrators", Builtin: true, Permissions: []domain.Permission{}, MemberIDs: []string{}}},
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
	c.audit = slices.Clone(s.audit)
	c.schedules = maps.Clone(s.schedules)
	c.runs = slices.Clone(s.runs)
	c.emails = maps.Clone(s.emails)
	c.users = maps.Clone(s.users)
	c.groups = maps.Clone(s.groups) // groups' slices are replaced, never changed in place
	c.sessions = maps.Clone(s.sessions)
	c.idempotency = maps.Clone(s.idempotency)
	c.exported = maps.Clone(s.exported)
	c.smtpPass = slices.Clone(s.smtpPass)
	c.backupRuns = slices.Clone(s.backupRuns)
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
		Emails: emails{s}, Users: users{s}, Groups: groups{s}, Sessions: sessions{s}, Idempotency: idempotency{s}, SMTP: smtpStore{s}, Audit: auditStore{s},
		System: systemStore{s}, BackupRuns: backupRuns{s},
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

// Audit returns the whole audit trail, oldest first (test inspection).
func (s *Store) Audit() []domain.AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.st.audit)
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

func (r customers) GetMany(_ context.Context, ids []string) (map[string]domain.Customer, error) {
	out := map[string]domain.Customer{}
	err := r.s.do(func(st *state) error {
		for _, id := range ids {
			if c, ok := st.customers[id]; ok {
				out[id] = c
			}
		}
		return nil
	})
	return out, err
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

func (r invoices) Totals(_ context.Context, today domain.Date) ([]ports.CurrencyTotals, error) {
	var out []ports.CurrencyTotals
	err := r.s.do(func(st *state) error {
		by := map[domain.CurrencyCode]*ports.CurrencyTotals{}
		for _, inv := range st.invoices {
			if !inv.IsOpen() {
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
		var all []domain.Invoice
		for id, inv := range st.invoices {
			if inv.Status != domain.InvoiceDraft && st.exported[id] != inv.Version {
				all = append(all, inv)
			}
		}
		// Like the SQL (ORDER BY updated_at LIMIT ?): order first, then limit. The ID breaks
		// ties so the result never depends on map iteration order.
		sort.Slice(all, func(i, j int) bool {
			if !all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
				return all[i].UpdatedAt.Before(all[j].UpdatedAt)
			}
			return all[i].ID < all[j].ID
		})
		for _, inv := range all[:min(len(all), max(limit, 0))] {
			out = append(out, ports.ExportRef{ID: inv.ID, Version: inv.Version})
		}
		return nil
	})
	return out, err
}

func (r invoices) ResetExported(_ context.Context) error {
	return r.s.do(func(st *state) error {
		st.exported = map[string]int{}
		return nil
	})
}

func (r invoices) CountCreatedSince(_ context.Context, t time.Time) (int, error) {
	n := 0
	err := r.s.do(func(st *state) error {
		for _, inv := range st.invoices {
			if inv.CreatedAt.After(t) {
				n++
			}
		}
		return nil
	})
	return n, err
}

func (r invoices) MarkExported(_ context.Context, id string, version int) error {
	return r.s.do(func(st *state) error {
		st.exported[id] = version
		return nil
	})
}

func (r invoices) Monthly(_ context.Context, since domain.Date, loc *time.Location) ([]ports.MonthTotal, []ports.MonthTotal, error) {
	var inv, paid []ports.MonthTotal
	err := r.s.do(func(st *state) error {
		add := func(list *[]ports.MonthTotal, c domain.CurrencyCode, month string, amt int64) {
			for i := range *list {
				if (*list)[i].Currency == c && (*list)[i].Month == month {
					(*list)[i].Total += amt
					(*list)[i].Count++
					return
				}
			}
			*list = append(*list, ports.MonthTotal{Currency: c, Month: month, Total: amt, Count: 1})
		}
		for _, x := range st.invoices {
			if x.Status != domain.InvoiceDraft && x.Status != domain.InvoiceVoid && !x.IssueDate.Before(since) {
				add(&inv, x.Currency, x.IssueDate.Time(time.UTC).Format("2006-01"), x.Totals.Total)
			}
			if x.Status == domain.InvoicePaid && !x.PaidAt.Before(since.Time(loc)) {
				add(&paid, x.Currency, x.PaidAt.In(loc).Format("2006-01"), x.Totals.Total)
			}
		}
		less := func(l []ports.MonthTotal) {
			sort.Slice(l, func(i, j int) bool { return l[i].Month+string(l[i].Currency) < l[j].Month+string(l[j].Currency) })
		}
		less(inv)
		less(paid)
		return nil
	})
	return inv, paid, err
}

func (r invoices) TopDebtors(_ context.Context, limit int) ([]ports.Debtor, error) {
	var out []ports.Debtor
	err := r.s.do(func(st *state) error {
		by := map[string]*ports.Debtor{}
		for _, x := range st.invoices {
			if !x.IsOpen() {
				continue
			}
			k := x.CustomerID + string(x.Currency)
			d := by[k]
			if d == nil {
				d = &ports.Debtor{CustomerID: x.CustomerID, Name: st.customers[x.CustomerID].Name, Currency: x.Currency}
				by[k] = d
			}
			d.Outstanding += x.Totals.Total
			d.OpenCount++
		}
		for _, d := range by {
			out = append(out, *d)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Currency != out[j].Currency {
				return out[i].Currency < out[j].Currency
			}
			return out[i].Outstanding > out[j].Outstanding
		})
		kept := out[:0]
		n := map[domain.CurrencyCode]int{}
		for _, d := range out {
			if n[d.Currency] < limit {
				n[d.Currency]++
				kept = append(kept, d)
			}
		}
		out = kept
		return nil
	})
	return out, err
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

// Lock does nothing in the fake (transactions are serialised).
func (r sequences) Lock(context.Context) error { return nil }

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
		if st.system.AutomationHeld {
			return nil
		}
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
			if x.ScheduleID == run.ScheduleID && x.Overlaps(run) {
				return domain.ErrConflict
			}
		}
		st.runs = append(st.runs, run)
		return nil
	})
}

func (r schedules) BilledUntil(_ context.Context, scheduleID string) (domain.Date, error) {
	var until domain.Date
	err := r.s.do(func(st *state) error {
		for _, x := range st.runs {
			if x.ScheduleID == scheduleID && x.PeriodEnd.After(until) {
				until = x.PeriodEnd
			}
		}
		return nil
	})
	return until, err
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

// Lock reads the message (the fake serialises transactions).
func (r emails) Lock(ctx context.Context, id string) (*domain.EmailMessage, error) {
	return r.Get(ctx, id)
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
			if (f.Status == "" || m.Status == f.Status) && (f.InvoiceID == "" || m.InvoiceID == f.InvoiceID) &&
				(f.CustomerID == "" || m.CustomerID == f.CustomerID) {
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
		if st.system.AutomationHeld {
			return nil
		}
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

func (r emails) CountSending(_ context.Context, now time.Time) (int, error) {
	n := 0
	err := r.s.do(func(st *state) error {
		for _, m := range st.emails {
			if m.Status == domain.EmailSending && m.UpdatedAt.After(now.Add(-domain.StaleSendingAfter)) {
				n++
			}
		}
		return nil
	})
	return n, err
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

// Lock reads the user (the fake serialises nothing; the use cases' re-checks still run).
func (r users) Lock(ctx context.Context, id string) (domain.User, error) { return r.Get(ctx, id) }

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

func (r users) UpdatePassword(_ context.Context, id, hash string, mustChange bool) error {
	return r.update(id, func(u *domain.User) { u.PasswordHash, u.MustChangePassword = hash, mustChange })
}

func (r users) SetDisabled(_ context.Context, id string, at time.Time) error {
	return r.update(id, func(u *domain.User) { u.DisabledAt = at })
}

func (r users) TouchLogin(_ context.Context, id string, at time.Time) error {
	return r.update(id, func(u *domain.User) { u.LastLoginAt = at })
}

func (r users) update(id string, f func(*domain.User)) error {
	return r.s.do(func(st *state) error {
		u, ok := st.users[id]
		if !ok {
			return domain.ErrNotFound
		}
		f(&u)
		st.users[id] = u
		return nil
	})
}

func (r users) List(_ context.Context) ([]domain.User, error) {
	var out []domain.User
	err := r.s.do(func(st *state) error {
		for _, u := range st.users {
			out = append(out, u)
		}
		slices.SortFunc(out, func(a, b domain.User) int { return strings.Compare(strings.ToLower(a.Email), strings.ToLower(b.Email)) })
		return nil
	})
	return out, err
}

// LockActive returns the active users' ids. The fake serialises transactions, so there
// is nothing to lock.
func (r users) LockActive(_ context.Context) ([]string, error) {
	var out []string
	err := r.s.do(func(st *state) error {
		for _, u := range st.users {
			if u.Active() {
				out = append(out, u.ID)
			}
		}
		slices.Sort(out)
		return nil
	})
	return out, err
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

func (r sessions) DeleteAll(_ context.Context) error {
	return r.s.do(func(st *state) error {
		st.sessions = map[string]domain.Session{}
		return nil
	})
}

func (r sessions) DeleteForUser(_ context.Context, userID, exceptHash string) error {
	return r.s.do(func(st *state) error {
		for k, s := range st.sessions {
			if s.UserID == userID && k != exceptHash {
				delete(st.sessions, k)
			}
		}
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

func (r idempotency) DeleteAll(_ context.Context) error {
	return r.s.do(func(st *state) error {
		st.idempotency = map[string]ports.IdempotencyRecord{}
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

// ---- SMTP settings ----

type smtpStore struct{ s *Store }

func (r smtpStore) Get(_ context.Context) (domain.SMTPSettings, []byte, error) {
	var out domain.SMTPSettings
	var pw []byte
	err := r.s.do(func(st *state) error {
		if st.smtp == nil {
			return domain.ErrNotFound
		}
		out, pw = *st.smtp, slices.Clone(st.smtpPass)
		return nil
	})
	return out, pw, err
}

func (r smtpStore) Save(_ context.Context, s domain.SMTPSettings, passwordEnc []byte) error {
	return r.s.do(func(st *state) error {
		cur := 0
		if st.smtp != nil {
			cur = st.smtp.Version
		}
		if cur != s.Version {
			return domain.ErrConflict
		}
		s.Version++
		st.smtp, st.smtpPass = &s, slices.Clone(passwordEnc)
		return nil
	})
}

// ---- audit ----

type auditStore struct{ s *Store }

func (r auditStore) Append(_ context.Context, e *domain.AuditEntry) error {
	return r.s.do(func(st *state) error {
		prev := ""
		if n := len(st.audit); n > 0 {
			prev = st.audit[n-1].Hash
		}
		e.Seq = int64(len(st.audit) + 1)
		e.Seal(prev)
		st.audit = append(st.audit, *e)
		return nil
	})
}

func (r auditStore) AppendVerbatim(_ context.Context, e domain.AuditEntry) error {
	return r.s.do(func(st *state) error {
		prev := ""
		if n := len(st.audit); n > 0 {
			prev = st.audit[n-1].Hash
		}
		if e.Seq != int64(len(st.audit)+1) || e.PrevHash != prev || e.ComputeHash(prev) != e.Hash {
			return fmt.Errorf("%w: audit entry #%d does not chain from the head", domain.ErrConflict, e.Seq)
		}
		st.audit = append(st.audit, e)
		return nil
	})
}

func (r auditStore) List(_ context.Context, f ports.AuditFilter) ([]domain.AuditEntry, int, error) {
	var out []domain.AuditEntry
	var total int
	err := r.s.do(func(st *state) error {
		var all []domain.AuditEntry
		for i := len(st.audit) - 1; i >= 0; i-- {
			e := st.audit[i]
			if (f.EntityType == "" || e.EntityType == f.EntityType) && (f.EntityID == "" || e.EntityID == f.EntityID) &&
				contains(e.ActorName, f.Actor) && strings.HasPrefix(e.Action, f.Action) {
				all = append(all, e)
			}
		}
		out, total = page(all, f.Page)
		return nil
	})
	return out, total, err
}

func (r auditStore) ForEntity(_ context.Context, entityType, entityID string) ([]domain.AuditEntry, error) {
	var out []domain.AuditEntry
	err := r.s.do(func(st *state) error {
		for _, e := range st.audit {
			if e.EntityType == entityType && e.EntityID == entityID {
				out = append(out, e)
			}
		}
		return nil
	})
	return out, err
}

func (r auditStore) Chain(_ context.Context, after int64, limit int) ([]domain.AuditEntry, error) {
	var out []domain.AuditEntry
	err := r.s.do(func(st *state) error {
		for _, e := range st.audit {
			if e.Seq > after && len(out) < limit {
				out = append(out, e)
			}
		}
		return nil
	})
	return out, err
}

// TamperAudit rewrites an entry's summary in place (tests only: proves verification).
func (s *Store) TamperAudit(seq int64, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.audit[seq-1].Summary = summary
}

// ---- system state & backup runs ----

type systemStore struct{ s *Store }

func (r systemStore) Get(_ context.Context) (domain.SystemState, error) {
	var out domain.SystemState
	err := r.s.do(func(st *state) error {
		out = st.system
		return nil
	})
	return out, err
}

func (r systemStore) Lock(ctx context.Context) (domain.SystemState, error) { return r.Get(ctx) }

func (r systemStore) Save(_ context.Context, sys domain.SystemState) error {
	return r.s.do(func(st *state) error {
		st.system = sys
		return nil
	})
}

type backupRuns struct{ s *Store }

func (r backupRuns) Start(_ context.Context, run domain.BackupRun) error {
	return r.s.do(func(st *state) error {
		for _, x := range st.backupRuns {
			if x.Slot.Equal(run.Slot) {
				return domain.ErrConflict
			}
		}
		st.backupRuns = append(st.backupRuns, run)
		return nil
	})
}

func (r backupRuns) Finish(_ context.Context, run domain.BackupRun) error {
	return r.s.do(func(st *state) error {
		for i, x := range st.backupRuns {
			if x.Slot.Equal(run.Slot) {
				st.backupRuns[i] = run
				return nil
			}
		}
		return domain.ErrNotFound
	})
}

func (r backupRuns) Latest(_ context.Context) (domain.BackupRun, error) {
	var out domain.BackupRun
	err := r.s.do(func(st *state) error {
		for _, x := range st.backupRuns {
			if x.Slot.After(out.Slot) {
				out = x
			}
		}
		if out.Slot.IsZero() {
			return domain.ErrNotFound
		}
		return nil
	})
	return out, err
}

// SetSending marks an outbox message as being delivered right now (tests).
func (s *Store) SetSending(id string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.st.emails[id]
	m.Status, m.UpdatedAt = domain.EmailSending, at
	s.st.emails[id] = m
}

// groups is the fake GroupRepository. Every write stores fresh slices, so a cloned state
// never shares one that changes.
type groups struct{ s *Store }

func copyGroup(g domain.Group) domain.Group {
	g.Permissions = append([]domain.Permission{}, g.Permissions...)
	g.MemberIDs = append([]string{}, g.MemberIDs...)
	return g
}

func (r groups) List(_ context.Context) ([]domain.Group, error) {
	var out []domain.Group
	err := r.s.do(func(st *state) error {
		for _, g := range st.groups {
			out = append(out, copyGroup(g))
		}
		slices.SortFunc(out, func(a, b domain.Group) int {
			if a.Builtin != b.Builtin {
				if a.Builtin {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		return nil
	})
	return out, err
}

func (r groups) Get(_ context.Context, id string) (domain.Group, error) {
	var out domain.Group
	err := r.s.do(func(st *state) error {
		g, ok := st.groups[id]
		if !ok {
			return domain.ErrNotFound
		}
		out = copyGroup(g)
		return nil
	})
	return out, err
}

// Lock reads the group (the fake serialises transactions).
func (r groups) Lock(ctx context.Context, id string) (domain.Group, error) { return r.Get(ctx, id) }

func (r groups) Create(_ context.Context, g domain.Group) error {
	return r.s.do(func(st *state) error {
		for _, o := range st.groups {
			if strings.EqualFold(o.Name, g.Name) {
				return domain.ErrConflict
			}
		}
		if err := checkMembers(st, g.MemberIDs); err != nil {
			return err
		}
		st.groups[g.ID] = copyGroup(g)
		return nil
	})
}

func (r groups) Update(_ context.Context, g domain.Group) error {
	return r.s.do(func(st *state) error {
		old, ok := st.groups[g.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if err := checkMembers(st, g.MemberIDs); err != nil {
			return err
		}
		if old.Builtin {
			old.MemberIDs = g.MemberIDs
			st.groups[g.ID] = copyGroup(old)
			return nil
		}
		for _, o := range st.groups {
			if o.ID != g.ID && strings.EqualFold(o.Name, g.Name) {
				return domain.ErrConflict
			}
		}
		g.Builtin, g.CreatedAt = false, old.CreatedAt
		st.groups[g.ID] = copyGroup(g)
		return nil
	})
}

func checkMembers(st *state, ids []string) error {
	for _, id := range ids {
		if _, ok := st.users[id]; !ok {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (r groups) Delete(_ context.Context, id string) error {
	return r.s.do(func(st *state) error {
		g, ok := st.groups[id]
		if !ok || g.Builtin {
			return domain.ErrNotFound
		}
		delete(st.groups, id)
		return nil
	})
}

func (r groups) ForUser(_ context.Context, userID string) ([]domain.Group, error) {
	var out []domain.Group
	err := r.s.do(func(st *state) error {
		for _, g := range st.groups {
			if g.HasMember(userID) {
				g = copyGroup(g)
				g.MemberIDs = nil
				out = append(out, g)
			}
		}
		slices.SortFunc(out, func(a, b domain.Group) int { return strings.Compare(a.ID, b.ID) })
		return nil
	})
	return out, err
}

func (r groups) SetUserGroups(_ context.Context, userID string, groupIDs []string) error {
	return r.s.do(func(st *state) error {
		if _, ok := st.users[userID]; !ok {
			return domain.ErrNotFound
		}
		for _, id := range groupIDs {
			if _, ok := st.groups[id]; !ok {
				return domain.ErrNotFound
			}
		}
		for id, g := range st.groups {
			g = copyGroup(g)
			g.MemberIDs = slices.DeleteFunc(g.MemberIDs, func(m string) bool { return m == userID })
			if slices.Contains(groupIDs, id) {
				g.MemberIDs = append(g.MemberIDs, userID)
				slices.Sort(g.MemberIDs)
			}
			st.groups[id] = g
		}
		return nil
	})
}

// LockActiveAdministrators returns the active administrators' ids (the fake serialises
// transactions, so there is nothing to lock).
func (r groups) LockActiveAdministrators(_ context.Context) ([]string, error) {
	var out []string
	err := r.s.do(func(st *state) error {
		for _, id := range st.groups[domain.AdministratorsGroupID].MemberIDs {
			if u, ok := st.users[id]; ok && u.Active() {
				out = append(out, id)
			}
		}
		return nil
	})
	return out, err
}
