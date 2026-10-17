package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// SettingsFile is what Settings → Import/Export moves between installations: the business
// profile, the e-mail server (never its password), currencies, bank accounts and customers.
// On import a nil or empty section is left alone.
type SettingsFile struct {
	Business     *BusinessSettings
	EmailServer  *domain.SMTPSettings
	Currencies   []domain.Currency
	BankAccounts []domain.BankAccount
	Customers    []domain.Customer
}

// ImportCounts says what an import did to one section.
type ImportCounts struct {
	Created, Updated, Unchanged int
}

// ImportResult is what ImportSettings did, or would do on a dry run.
type ImportResult struct {
	Applied      bool // false: a dry run, nothing was saved
	Business     ImportCounts
	EmailServer  ImportCounts
	Currencies   ImportCounts
	BankAccounts ImportCounts
	Customers    ImportCounts
	Warnings     []string // never nil
}

// errDryRun rolls back the transaction of a dry-run import.
var errDryRun = errors.New("dry run")

// ExportSettings reads the settings an import can restore, from one consistent snapshot,
// and records the export in the audit log (the file holds customer contact details and
// bank account numbers).
func (s *Service) ExportSettings(ctx context.Context) (SettingsFile, error) {
	var f SettingsFile
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		next, err := r.Sequences.Peek(ctx)
		if err != nil {
			return err
		}
		f.Business = &BusinessSettings{Profile: b, NextSequence: next}
		st, _, err := r.SMTP.Get(ctx)
		switch {
		case err == nil:
			f.EmailServer = &st
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}
		if f.Currencies, err = r.Currencies.List(ctx); err != nil {
			return err
		}
		if f.BankAccounts, err = r.Banks.List(ctx, ""); err != nil {
			return err
		}
		if f.Customers, err = allCustomers(ctx, r); err != nil {
			return err
		}
		return s.audit(ctx, r, "settings.exported", "settings", "export", fmt.Sprintf(
			"%d currencies, %d bank accounts, %d customers", len(f.Currencies), len(f.BankAccounts), len(f.Customers)))
	})
	if err != nil {
		return SettingsFile{}, wrap("app.ExportSettings", err)
	}
	return f, nil
}

// ImportSettings merges a settings file into this installation, all or nothing, in one
// transaction. Nothing is deleted. Currencies are matched by code, bank accounts by id
// (the same installation) or by currency and account number, customers by code; a match is
// updated, anything else created. The next invoice number only moves forward, the e-mail
// server keeps its stored password (a file never holds one), and a currency's minor units
// never change. With apply false nothing is saved: the result says what would happen.
func (s *Service) ImportSettings(ctx context.Context, f SettingsFile, apply bool) (ImportResult, error) {
	res := ImportResult{Applied: apply, Warnings: []string{}}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := s.importCurrencies(ctx, r, f.Currencies, &res); err != nil {
			return err
		}
		bankIDs, err := s.importBanks(ctx, r, f.BankAccounts, &res)
		if err != nil {
			return err
		}
		if err := s.importCustomers(ctx, r, f.Customers, bankIDs, &res); err != nil {
			return err
		}
		if err := s.importBusiness(ctx, r, f.Business, &res); err != nil {
			return err
		}
		if err := s.importEmailServer(ctx, r, f.EmailServer, &res); err != nil {
			return err
		}
		if err := s.audit(ctx, r, "settings.imported", "settings", "import", res.summary()); err != nil {
			return err
		}
		if !apply {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	if err != nil {
		return ImportResult{}, wrap("app.ImportSettings", err)
	}
	return res, nil
}

func (r ImportResult) summary() string {
	parts := make([]string, 0, 5)
	for _, c := range []struct {
		name string
		n    ImportCounts
	}{
		{"currencies", r.Currencies},
		{"bank accounts", r.BankAccounts},
		{"customers", r.Customers},
		{"business", r.Business},
		{"e-mail server", r.EmailServer},
	} {
		if c.n != (ImportCounts{}) {
			parts = append(parts, fmt.Sprintf("%s %d new, %d changed, %d unchanged", c.name, c.n.Created, c.n.Updated, c.n.Unchanged))
		}
	}
	if len(parts) == 0 {
		return "nothing to import"
	}
	return strings.Join(parts, "; ")
}

func (s *Service) importCurrencies(ctx context.Context, r ports.Repos, in []domain.Currency, res *ImportResult) error {
	list, err := r.Currencies.List(ctx)
	if err != nil {
		return err
	}
	have := map[domain.CurrencyCode]domain.Currency{}
	for _, c := range list {
		have[c.Code] = c
	}
	seen := map[domain.CurrencyCode]bool{}
	for i, in := range in {
		c, err := domain.NewCurrency(string(in.Code), in.Name, in.Symbol, in.MinorUnits, in.Active)
		if err != nil {
			return inItem("currencies", itemKey(string(in.Code), i), err)
		}
		if seen[c.Code] {
			return inItem("currencies", string(c.Code), &domain.ValidationError{Field: "code", Reason: "appears more than once in the file"})
		}
		seen[c.Code] = true
		cur, ok := have[c.Code]
		switch {
		case !ok:
			if err := r.Currencies.Create(ctx, c); err != nil {
				return err
			}
			res.Currencies.Created++
			err = s.audit(ctx, r, "currency.created", "currency", string(c.Code), c.Name)
		case cur.MinorUnits != c.MinorUnits:
			return inItem("currencies", string(c.Code), &domain.ValidationError{
				Field: "minorUnits", Reason: "is " + strconv.Itoa(cur.MinorUnits) + " here and cannot change (stored amounts depend on it)",
			})
		case cur == c:
			res.Currencies.Unchanged++
		default:
			if err := r.Currencies.Update(ctx, c); err != nil {
				return err
			}
			res.Currencies.Updated++
			err = s.audit(ctx, r, "currency.updated", "currency", string(c.Code), activeText(c.Active))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// importBanks returns the id each file account has here, for the customers' overrides.
func (s *Service) importBanks(ctx context.Context, r ports.Repos, in []domain.BankAccount, res *ImportResult) (map[string]string, error) {
	list, err := r.Banks.List(ctx, "")
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.BankAccount{}
	byNumber := map[string]domain.BankAccount{}
	number := func(b domain.BankAccount) string { return string(b.Currency) + " " + b.AccountNumber }
	for _, b := range list {
		byID[b.ID] = b
		byNumber[number(b)] = b
	}
	ids := map[string]string{}
	seen := map[string]bool{}
	used := map[string]bool{} // accounts here already matched by an entry
	for i, b := range in {
		fileID := strings.TrimSpace(b.ID)
		b.Currency = domain.CurrencyCode(strings.ToUpper(strings.TrimSpace(string(b.Currency))))
		if err := b.Validate(); err != nil {
			return nil, inItem("bankAccounts", itemKey(b.Label, i), err)
		}
		key := itemKey(b.Label, i)
		if seen[number(b)] {
			return nil, inItem("bankAccounts", key, &domain.ValidationError{Field: "accountNumber", Reason: "appears more than once in the file for " + string(b.Currency)})
		}
		seen[number(b)] = true
		if _, err := r.Currencies.Get(ctx, b.Currency); err != nil {
			return nil, inItem("bankAccounts", key, err)
		}
		cur, ok := byID[fileID]
		if !ok {
			cur, ok = byNumber[number(b)]
		}
		if ok && cur.Currency != b.Currency {
			return nil, inItem("bankAccounts", key, &domain.ValidationError{Field: "currency", Reason: "is " + string(cur.Currency) + " here and cannot change"})
		}
		if ok && used[cur.ID] {
			return nil, inItem("bankAccounts", key, &domain.ValidationError{Field: "id", Reason: "is the same account as another entry in the file"})
		}
		if ok {
			used[cur.ID] = true
			// Fresh, because clearing another account's default may have changed it.
			if cur, err = r.Banks.Get(ctx, cur.ID); err != nil {
				return nil, err
			}
			b.ID, b.Version = cur.ID, cur.Version
		} else {
			b.ID = s.ids.NewID()
			existing, err := r.Banks.List(ctx, b.Currency)
			if err != nil {
				return nil, err
			}
			if !hasDefault(existing) && b.Active {
				b.IsDefault = true // as CreateBankAccount: the first active account is the default
			}
		}
		if fileID != "" {
			ids[fileID] = b.ID
		}
		if ok && b == cur {
			res.BankAccounts.Unchanged++
			continue
		}
		if b.IsDefault {
			if err := r.Banks.ClearDefault(ctx, b.Currency, b.ID); err != nil {
				return nil, err
			}
		}
		action := "bank_account.updated"
		if ok {
			err = r.Banks.Update(ctx, b)
			res.BankAccounts.Updated++
		} else {
			err = r.Banks.Create(ctx, b)
			res.BankAccounts.Created++
			action = "bank_account.created"
		}
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, r, action, "bank_account", b.ID, b.Label+" ("+string(b.Currency)+")"); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (s *Service) importCustomers(ctx context.Context, r ports.Repos, in []domain.Customer, bankIDs map[string]string, res *ImportResult) error {
	if len(in) == 0 {
		return nil
	}
	list, err := allCustomers(ctx, r)
	if err != nil {
		return err
	}
	byCode := map[string]domain.Customer{}
	for _, c := range list {
		byCode[c.Code] = c
	}
	seen := map[string]bool{}
	for i, c := range in {
		c.Currency = domain.CurrencyCode(strings.ToUpper(strings.TrimSpace(string(c.Currency))))
		if id, ok := bankIDs[strings.TrimSpace(c.BankAccountID)]; ok {
			c.BankAccountID = id
		}
		if err := checkCustomer(ctx, r, &c); err != nil {
			return inItem("customers", itemKey(strings.ToUpper(strings.TrimSpace(c.Code)), i), err)
		}
		if seen[c.Code] {
			return inItem("customers", c.Code, &domain.ValidationError{Field: "code", Reason: "appears more than once in the file"})
		}
		seen[c.Code] = true
		cur, ok := byCode[c.Code]
		switch {
		case !ok:
			c.ID, c.Version = s.ids.NewID(), 0
			if err := r.Customers.Create(ctx, c); err != nil {
				return err
			}
			res.Customers.Created++
			err = s.audit(ctx, r, "customer.created", "customer", c.ID, c.Code+" "+c.Name)
		case sameCustomer(cur, c):
			res.Customers.Unchanged++
		default:
			c.ID, c.Version = cur.ID, cur.Version
			if err := r.Customers.Update(ctx, c); err != nil {
				return err
			}
			res.Customers.Updated++
			err = s.audit(ctx, r, "customer.updated", "customer", c.ID, c.Code+" "+c.Name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// sameCustomer compares everything an import can change (not the id or version).
func sameCustomer(a, b domain.Customer) bool {
	if !slices.Equal(a.CC, b.CC) {
		return false
	}
	a.CC, b.CC = nil, nil
	a.ID, a.Version = b.ID, b.Version
	return reflect.DeepEqual(a, b)
}

func (s *Service) importBusiness(ctx context.Context, r ports.Repos, in *BusinessSettings, res *ImportResult) error {
	if in == nil {
		return nil
	}
	p := in.Profile
	if err := p.Validate(); err != nil {
		return inItem("business", "", err)
	}
	cur, err := s.business(ctx, r)
	if err != nil {
		return err
	}
	p.Version = cur.Version
	changed := p != cur
	if changed {
		if err := r.Business.Save(ctx, p); err != nil {
			return err
		}
		if err := s.audit(ctx, r, "settings.business_updated", "settings", "business", p.Name); err != nil {
			return err
		}
	}
	next, err := r.Sequences.Peek(ctx)
	if err != nil {
		return err
	}
	switch {
	case in.NextSequence > next:
		if err := r.Sequences.Set(ctx, in.NextSequence); err != nil {
			return err
		}
		changed = true
		if err := s.audit(ctx, r, "settings.next_number_set", "settings", "business", fmt.Sprintf("next invoice sequence %d", in.NextSequence)); err != nil {
			return err
		}
	case in.NextSequence > 0 && in.NextSequence < next:
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"The next invoice sequence stays %d: the file has %d, and numbers already used are never given again.", next, in.NextSequence))
	}
	if changed {
		res.Business.Updated++
	} else {
		res.Business.Unchanged++
	}
	return nil
}

func (s *Service) importEmailServer(ctx context.Context, r ports.Repos, in *domain.SMTPSettings, res *ImportResult) error {
	if in == nil {
		return nil
	}
	st := *in
	if err := st.Validate(); err != nil {
		return inItem("emailServer", "", err)
	}
	cur, enc, err := r.SMTP.Get(ctx)
	found := err == nil
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if st.Enabled && st.Username != "" && len(enc) == 0 {
		st.Enabled = false
		res.Warnings = append(res.Warnings,
			"The e-mail server is imported switched off: a settings file never holds the password, so enter it under Settings → E-mail server and switch the server on there.")
	}
	st.Version = cur.Version
	if found && st == cur {
		res.EmailServer.Unchanged++
		return nil
	}
	if err := r.SMTP.Save(ctx, st, enc); err != nil {
		return err
	}
	if found {
		res.EmailServer.Updated++
	} else {
		res.EmailServer.Created++
	}
	return s.audit(ctx, r, "settings.email_server_updated", "settings", "email", st.Host+" as "+st.Username+", "+activeText(st.Enabled)+", imported")
}

// allCustomers reads every customer, active or not, a page at a time.
func allCustomers(ctx context.Context, r ports.Repos) ([]domain.Customer, error) {
	var out []domain.Customer
	page := ports.Page{Limit: 200}.Normalize()
	for {
		cs, total, err := r.Customers.List(ctx, ports.CustomerFilter{IncludeInactive: true, Page: page})
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
		page.Offset += len(cs)
		if len(cs) == 0 || page.Offset >= total {
			return out, nil
		}
	}
}

// itemKey names an item in an error: its code or label, else its position in the file.
func itemKey(key string, i int) string {
	if key = strings.TrimSpace(key); key != "" {
		return key
	}
	return "#" + strconv.Itoa(i+1)
}

// inItem says which item of the file an error is about, e.g. customers[CD].email.
func inItem(section, key string, err error) error {
	field := section
	if key != "" {
		field += "[" + key + "]"
	}
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		return &domain.ValidationError{Field: field + "." + ve.Field, Reason: ve.Reason}
	case errors.Is(err, domain.ErrInactive):
		return &domain.ValidationError{Field: field, Reason: "uses an inactive currency or bank account"}
	case errors.Is(err, domain.ErrNotFound):
		return &domain.ValidationError{Field: field, Reason: "uses a currency or bank account that is neither in the file nor on this server"}
	}
	return err
}
