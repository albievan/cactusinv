package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// SettingsFile is what Settings → Import/Export moves between installations: the business
// profile, the e-mail server (never its password), currencies, bank accounts, products,
// customers and schedules. On import a nil or empty section is left alone.
type SettingsFile struct {
	Business     *BusinessSettings
	EmailServer  *domain.SMTPSettings
	Currencies   []domain.Currency
	BankAccounts []domain.BankAccount
	Products     []domain.Product
	Customers    []domain.Customer
	Schedules    []ScheduleImport
}

// ScheduleImport is a schedule in a settings file. Ids differ between installations, so
// its customer is named by code and each line's product by SKU; Currency is the customer's
// currency at export, the currency of the lines' unit prices.
type ScheduleImport struct {
	CustomerCode string
	Currency     domain.CurrencyCode
	ProductSKUs  []string // one per line
	Schedule     domain.Schedule
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
	Products     ImportCounts
	Customers    ImportCounts
	Schedules    ImportCounts
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
		if f.Products, err = allProducts(ctx, r); err != nil {
			return err
		}
		if f.Customers, err = allCustomers(ctx, r); err != nil {
			return err
		}
		if f.Schedules, err = exportSchedules(ctx, r, f.Customers, f.Products); err != nil {
			return err
		}
		return s.audit(ctx, r, "settings.exported", "settings", "export", fmt.Sprintf(
			"%d currencies, %d bank accounts, %d products, %d customers, %d schedules",
			len(f.Currencies), len(f.BankAccounts), len(f.Products), len(f.Customers), len(f.Schedules)))
	})
	if err != nil {
		return SettingsFile{}, wrap("app.ExportSettings", err)
	}
	return f, nil
}

// ImportSettings merges a settings file into this installation, all or nothing, in one
// transaction. Nothing is deleted. Currencies are matched by code, bank accounts by id
// (the same installation) or by currency and account number, products by SKU, customers by
// code, schedules by customer and name; a match is updated, anything else created. A new
// schedule is created paused, so nothing is invoiced before someone resumes it; a matched
// one keeps its run state and billed periods. The next invoice number only moves forward,
// the e-mail server keeps its stored password (a file never holds one), and a currency's
// minor units never change. With apply false nothing is saved: the result says what would
// happen.
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
		if err := s.importProducts(ctx, r, f.Products, &res); err != nil {
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
		if err := s.importSchedules(ctx, r, f.Schedules, &res); err != nil {
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
	parts := make([]string, 0, 7)
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

func (s *Service) importProducts(ctx context.Context, r ports.Repos, in []domain.Product, res *ImportResult) error {
	if len(in) == 0 {
		return nil
	}
	list, err := allProducts(ctx, r)
	if err != nil {
		return err
	}
	bySKU := map[string]domain.Product{}
	for _, p := range list {
		bySKU[p.SKU] = p
	}
	seen := map[string]bool{}
	for i, p := range in {
		if err := checkProduct(ctx, r, &p); err != nil {
			return inItem("products", itemKey(strings.TrimSpace(p.SKU), i), err)
		}
		if seen[p.SKU] {
			return inItem("products", p.SKU, &domain.ValidationError{Field: "sku", Reason: "appears more than once in the file"})
		}
		seen[p.SKU] = true
		cur, ok := bySKU[p.SKU]
		if ok {
			p.ID, p.Version = cur.ID, cur.Version
			if reflect.DeepEqual(p, cur) {
				res.Products.Unchanged++
				continue
			}
			if err := r.Products.Update(ctx, p); err != nil {
				return err
			}
			res.Products.Updated++
			err = s.audit(ctx, r, "product.updated", "product", p.ID, p.SKU+" "+p.Name)
		} else {
			p.ID, p.Version = s.ids.NewID(), 0
			if err := r.Products.Create(ctx, p); err != nil {
				return err
			}
			res.Products.Created++
			err = s.audit(ctx, r, "product.created", "product", p.ID, p.SKU+" "+p.Name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) importSchedules(ctx context.Context, r ports.Repos, in []ScheduleImport, res *ImportResult) error {
	if len(in) == 0 {
		return nil
	}
	customers, err := allCustomers(ctx, r) // including those just imported
	if err != nil {
		return err
	}
	products, err := allProducts(ctx, r)
	if err != nil {
		return err
	}
	byCode := map[string]domain.Customer{}
	for _, c := range customers {
		byCode[c.Code] = c
	}
	bySKU := map[string]string{}
	for _, p := range products {
		bySKU[p.SKU] = p.ID
	}
	list, err := allSchedules(ctx, r)
	if err != nil {
		return err
	}
	byName := map[string]domain.Schedule{}
	for _, sc := range list {
		byName[sc.CustomerID+"\x00"+sc.Name] = sc
	}
	seen := map[string]bool{}
	paused := 0
	for i, in := range in {
		sc := in.Schedule
		code := strings.ToUpper(strings.TrimSpace(in.CustomerCode))
		sc.Name = strings.TrimSpace(sc.Name)
		key := itemKey(strings.TrimSpace(code+" "+sc.Name), i)
		c, ok := byCode[code]
		if !ok {
			return inItem("schedules", key, &domain.ValidationError{Field: "customerCode", Reason: "no customer " + code + " in the file or on this server"})
		}
		sc.CustomerID = c.ID
		if len(in.ProductSKUs) != len(sc.Lines) {
			return inItem("schedules", key, &domain.ValidationError{Field: "productSkus", Reason: "must name one product per line"})
		}
		sc.Lines = slices.Clone(sc.Lines)
		for j, sku := range in.ProductSKUs {
			id, ok := bySKU[strings.TrimSpace(sku)]
			if !ok {
				return inItem("schedules", key, &domain.ValidationError{Field: "lines.product", Reason: "no product " + sku + " in the file or on this server"})
			}
			sc.Lines[j].ProductID = id
			if sc.Lines[j].UnitPrice != nil && in.Currency != c.Currency {
				return inItem("schedules", key, &domain.ValidationError{
					Field: "lines.unitPrice", Reason: "is in " + string(in.Currency) + ", but customer " + code + " is billed in " + string(c.Currency),
				})
			}
		}
		if seen[c.ID+"\x00"+sc.Name] {
			return inItem("schedules", key, &domain.ValidationError{Field: "name", Reason: "appears more than once in the file for this customer"})
		}
		seen[c.ID+"\x00"+sc.Name] = true
		if err := checkScheduleAccess(ctx, sc); err != nil {
			return err
		}
		cur, ok := byName[c.ID+"\x00"+sc.Name]
		if !ok {
			sc.ID, sc.CreatedAt, sc.Version = s.ids.NewID(), s.now(), 0
			sc.Recurrence.DefaultWeekendRule()
			if err := s.createSchedule(ctx, r, &sc, true); err != nil {
				return inItem("schedules", key, err)
			}
			res.Schedules.Created++
			paused++
			continue
		}
		if err := checkScheduleAccess(ctx, cur); err != nil {
			return err
		}
		sc.ID, sc.Version = cur.ID, cur.Version
		if sc.Recurrence.Weekend == "" {
			sc.Recurrence.Weekend = cur.Recurrence.Weekend
		}
		if err := s.checkSchedule(ctx, r, &sc); err != nil {
			return inItem("schedules", key, err)
		}
		if sameDefinition(sc, cur) {
			res.Schedules.Unchanged++
			continue
		}
		if err := s.editSchedule(ctx, r, &cur, &sc); err != nil {
			return inItem("schedules", key, err)
		}
		res.Schedules.Updated++
	}
	if paused > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d new schedule(s) are imported paused, so nothing is invoiced or e-mailed yet: resume each on Schedules when it should start.", paused))
	}
	return nil
}

// sameDefinition compares what a settings file can change about a schedule (not its run
// state, id or version).
func sameDefinition(a, b domain.Schedule) bool {
	def := func(s domain.Schedule) domain.Schedule {
		if len(s.Lines) == 0 {
			s.Lines = nil
		}
		return domain.Schedule{
			CustomerID: s.CustomerID, Name: s.Name, Kind: s.Kind, EmailScope: s.EmailScope, Recurrence: s.Recurrence,
			Lines: s.Lines, Header: s.Header, Notes: s.Notes, TaxRate: s.TaxRate, AutoSend: s.AutoSend,
			EmailDelayDays: s.EmailDelayDays, EmailSubject: s.EmailSubject, EmailBody: s.EmailBody,
		}
	}
	return reflect.DeepEqual(def(a), def(b))
}

// exportSchedules names each schedule's customer by code and its lines' products by SKU.
func exportSchedules(ctx context.Context, r ports.Repos, customers []domain.Customer, products []domain.Product) ([]ScheduleImport, error) {
	list, err := allSchedules(ctx, r)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.Customer{}
	for _, c := range customers {
		byID[c.ID] = c
	}
	sku := map[string]string{}
	for _, p := range products {
		sku[p.ID] = p.SKU
	}
	out := make([]ScheduleImport, 0, len(list))
	for _, sc := range list {
		c := byID[sc.CustomerID]
		si := ScheduleImport{CustomerCode: c.Code, Currency: c.Currency, Schedule: sc, ProductSKUs: make([]string, len(sc.Lines))}
		for i, l := range sc.Lines {
			si.ProductSKUs[i] = sku[l.ProductID]
		}
		out = append(out, si)
	}
	return out, nil
}

// allProducts reads every product, active or not, a page at a time.
func allProducts(ctx context.Context, r ports.Repos) ([]domain.Product, error) {
	var out []domain.Product
	page := ports.Page{Limit: 200}.Normalize()
	for {
		ps, total, err := r.Products.List(ctx, ports.ProductFilter{IncludeInactive: true, Page: page})
		if err != nil {
			return nil, err
		}
		out = append(out, ps...)
		page.Offset += len(ps)
		if len(ps) == 0 || page.Offset >= total {
			return out, nil
		}
	}
}

// allSchedules reads every schedule, a page at a time.
func allSchedules(ctx context.Context, r ports.Repos) ([]domain.Schedule, error) {
	var out []domain.Schedule
	page := ports.Page{Limit: 200}.Normalize()
	for {
		ss, total, err := r.Schedules.List(ctx, ports.ScheduleFilter{Page: page})
		if err != nil {
			return nil, err
		}
		out = append(out, ss...)
		page.Offset += len(ss)
		if len(ss) == 0 || page.Offset >= total {
			return out, nil
		}
	}
}

// maxSettingsFile bounds a decrypted settings file (as the HTTP body limit bounds a plain one).
const maxSettingsFile = 16 << 20

// SealSettings encrypts an exported settings file with a passphrase (age with scrypt, as a
// downloaded backup is), so it can be opened only with that passphrase.
func (s *Service) SealSettings(plain []byte, passphrase string) ([]byte, error) {
	if err := domain.ValidateBackupPassphrase(passphrase); err != nil {
		return nil, wrap("app.SealSettings", err)
	}
	sealer := s.backups.Sealer
	if sealer == nil {
		return nil, wrap("app.SealSettings", fmt.Errorf("%w: encryption", domain.ErrNotConfigured))
	}
	unlock, err := s.lockBackups() // scrypt needs memory: one at a time, with backups
	if err != nil {
		return nil, wrap("app.SealSettings", err)
	}
	defer unlock()
	var buf bytes.Buffer
	w, err := sealer.SealWithPassphrase(&buf, passphrase)
	if err == nil {
		_, err = w.Write(plain)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return nil, wrap("app.SealSettings", err)
	}
	return buf.Bytes(), nil
}

// OpenSettings decrypts a settings file sealed by SealSettings. A wrong passphrase or a
// damaged file is a validation error on the passphrase.
func (s *Service) OpenSettings(sealed []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, wrap("app.OpenSettings", &domain.ValidationError{Field: "passphrase", Reason: "the file is protected: enter its passphrase"})
	}
	sealer := s.backups.Sealer
	if sealer == nil {
		return nil, wrap("app.OpenSettings", fmt.Errorf("%w: encryption", domain.ErrNotConfigured))
	}
	unlock, err := s.lockBackups()
	if err != nil {
		return nil, wrap("app.OpenSettings", err)
	}
	defer unlock()
	rd, err := sealer.Open(bytes.NewReader(sealed), passphrase)
	var plain []byte
	if err == nil {
		plain, err = io.ReadAll(io.LimitReader(rd, maxSettingsFile+1))
	}
	switch {
	case errors.Is(err, domain.ErrBackupInvalid):
		return nil, wrap("app.OpenSettings", &domain.ValidationError{Field: "passphrase", Reason: "is wrong, or the file is damaged"})
	case err != nil:
		return nil, wrap("app.OpenSettings", err)
	case len(plain) > maxSettingsFile:
		return nil, wrap("app.OpenSettings", &domain.ValidationError{Field: "file", Reason: "is too large"})
	}
	return plain, nil
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
