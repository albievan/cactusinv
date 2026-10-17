package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// ---- settings import/export ----

const (
	settingsFileFormat  = SettingsFileFormat("cactusinv-settings")
	settingsFileVersion = SettingsFileFormatVersion(1)
)

func (h *handlers) ExportSettings(ctx context.Context, req ExportSettingsRequestObject) (ExportSettingsResponseObject, error) {
	pass := req.Body.Passphrase
	if pass != "" {
		// Refuse a short passphrase before anything is read or audited.
		if err := domain.ValidateBackupPassphrase(pass); err != nil {
			return nil, err
		}
	}
	f, err := h.svc.ExportSettings(ctx)
	if err != nil {
		return nil, err
	}
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	out := h.toAPISettingsFile(f, mu)
	if pass == "" {
		return ExportSettings200JSONResponse(out), nil
	}
	plain, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	sealed, err := h.svc.SealSettings(plain, pass)
	if err != nil {
		return nil, err
	}
	return ExportSettings200JSONResponse{
		Format: out.Format, FormatVersion: out.FormatVersion, ExportedAt: out.ExportedAt, AppVersion: out.AppVersion,
		Encrypted: sealed,
	}, nil
}

func (h *handlers) toAPISettingsFile(f app.SettingsFile, mu minorUnits) SettingsFile {
	out := SettingsFile{
		Format: settingsFileFormat, FormatVersion: settingsFileVersion,
		ExportedAt: h.clock.Now().UTC(), AppVersion: h.version,
		Currencies: make([]Currency, len(f.Currencies)), BankAccounts: make([]BankAccount, len(f.BankAccounts)),
		Products: make([]Product, len(f.Products)), Customers: make([]Customer, len(f.Customers)),
		Schedules: make([]SettingsSchedule, len(f.Schedules)),
	}
	if f.Business != nil {
		b := toAPIBusiness(*f.Business)
		out.Business = &b
	}
	if f.EmailServer != nil {
		// The file holds no password, so passwordSet is false.
		e := toAPIEmailSettings(app.SMTPView{Settings: *f.EmailServer, Warnings: domain.EmailSenderWarnings(*f.EmailServer)})
		out.EmailServer = &e
	}
	for i, c := range f.Currencies {
		out.Currencies[i] = toAPICurrency(c)
	}
	for i, b := range f.BankAccounts {
		out.BankAccounts[i] = toAPIBank(b)
	}
	for i, p := range f.Products {
		out.Products[i] = toAPIProduct(p, mu)
	}
	for i, c := range f.Customers {
		out.Customers[i] = toAPICustomer(c)
	}
	for i, s := range f.Schedules {
		sc := toAPISchedule(&s.Schedule, nil, s.Currency, mu)
		for j, l := range s.Schedule.Lines {
			sc.Lines[j].Description = l.Description
		}
		out.Schedules[i] = SettingsSchedule{CustomerCode: s.CustomerCode, Currency: string(s.Currency), ProductSkus: s.ProductSKUs, Schedule: sc}
	}
	return out
}

func (h *handlers) ImportSettings(ctx context.Context, req ImportSettingsRequestObject) (ImportSettingsResponseObject, error) {
	b, err := h.openSettingsFile(req.Body.File, req.Body.Passphrase)
	if err != nil {
		return nil, err
	}
	want := func(s SettingsSection) bool {
		return len(req.Body.Sections) == 0 || slices.Contains(req.Body.Sections, s)
	}
	// Amounts are read in each currency's minor units: this installation's, and the file's
	// for currencies it adds (a changed one is refused by the import).
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range b.Currencies {
		code := domain.CurrencyCode(strings.ToUpper(c.Code))
		if _, ok := mu[code]; !ok {
			mu[code] = c.MinorUnits
		}
	}
	var f app.SettingsFile
	if b.Business != nil && want(SettingsSectionBusiness) {
		vat, err := percent("business.vatPercent", b.Business.VatPercent)
		if err != nil {
			return nil, err
		}
		x := b.Business
		f.Business = &app.BusinessSettings{Profile: domain.BusinessProfile{
			Name: x.Name, AddressLines: x.AddressLines, Phone: x.Phone, Email: x.Email, TaxNumber: x.TaxNumber,
			NumberFormat: x.NumberFormat, TimeZone: x.TimeZone, DefaultNotes: x.DefaultNotes,
			EmailSubject: x.EmailSubject, EmailBody: x.EmailBody, VATRate: vat,
		}, NextSequence: x.NextSequence}
	}
	if e := b.EmailServer; e != nil && want(SettingsSectionEmailServer) { // a password in the file is ignored
		f.EmailServer = &domain.SMTPSettings{Enabled: e.Enabled, Host: e.Host, Port: e.Port, Username: e.Username, From: e.From, TLS: string(e.Tls)}
	}
	if want(SettingsSectionCurrencies) {
		for _, c := range b.Currencies {
			f.Currencies = append(f.Currencies, domain.Currency{Code: domain.CurrencyCode(c.Code), Name: c.Name, Symbol: c.Symbol, MinorUnits: c.MinorUnits, Active: c.Active})
		}
	}
	if want(SettingsSectionBankAccounts) {
		for _, a := range b.BankAccounts {
			f.BankAccounts = append(f.BankAccounts, fromAPIBank(a.Id, a))
		}
	}
	if want(SettingsSectionProducts) {
		for i, p := range b.Products {
			dp, err := fromAPIProduct(p.Id, p, mu)
			if err != nil {
				return nil, inFileItem("products", p.Sku, i, err)
			}
			f.Products = append(f.Products, dp)
		}
	}
	if want(SettingsSectionCustomers) {
		for _, c := range b.Customers {
			f.Customers = append(f.Customers, fromAPICustomer(c.Id, c))
		}
	}
	if want(SettingsSectionSchedules) {
		for i, s := range b.Schedules {
			cur := domain.CurrencyCode(strings.ToUpper(s.Currency))
			sc, err := fromAPISchedule("", s.Schedule, cur, mu)
			if err != nil {
				return nil, inFileItem("schedules", s.CustomerCode+" "+s.Schedule.Name, i, err)
			}
			for j := range sc.Lines {
				sc.Lines[j].Description = s.Schedule.Lines[j].Description
			}
			f.Schedules = append(f.Schedules, app.ScheduleImport{CustomerCode: s.CustomerCode, Currency: cur, ProductSKUs: s.ProductSkus, Schedule: sc})
		}
	}
	res, err := h.svc.ImportSettings(ctx, f, req.Params.Apply)
	if err != nil {
		return nil, err
	}
	counts := func(c app.ImportCounts) ImportCounts {
		return ImportCounts{Created: c.Created, Updated: c.Updated, Unchanged: c.Unchanged}
	}
	return ImportSettings200JSONResponse{
		Applied: res.Applied, Business: counts(res.Business), EmailServer: counts(res.EmailServer),
		Currencies: counts(res.Currencies), BankAccounts: counts(res.BankAccounts), Products: counts(res.Products),
		Customers: counts(res.Customers), Schedules: counts(res.Schedules), Warnings: res.Warnings,
	}, nil
}

// openSettingsFile checks the file's format and, when it is protected, decrypts it.
func (h *handlers) openSettingsFile(f SettingsFile, passphrase string) (SettingsFile, error) {
	if err := checkSettingsFormat(f); err != nil {
		return SettingsFile{}, err
	}
	if len(f.Encrypted) == 0 {
		return f, nil
	}
	plain, err := h.svc.OpenSettings(f.Encrypted, passphrase)
	if err != nil {
		return SettingsFile{}, err
	}
	var inner SettingsFile
	if err := json.Unmarshal(plain, &inner); err != nil {
		return SettingsFile{}, &domain.ValidationError{Field: "file", Reason: "the protected file does not hold a settings file"}
	}
	if err := checkSettingsFormat(inner); err != nil {
		return SettingsFile{}, err
	}
	if len(inner.Encrypted) > 0 {
		return SettingsFile{}, &domain.ValidationError{Field: "file", Reason: "is protected twice"}
	}
	return inner, nil
}

func checkSettingsFormat(f SettingsFile) error {
	if f.Format != settingsFileFormat {
		return &domain.ValidationError{Field: "format", Reason: "this is not a cactusInv settings file"}
	}
	if f.FormatVersion != settingsFileVersion {
		return &domain.ValidationError{Field: "formatVersion", Reason: "the file was made by a newer cactusInv; update this one first"}
	}
	return nil
}

// inFileItem names the file item a conversion error is about, e.g. products[SS-USERS].prices.
func inFileItem(section, key string, i int, err error) error {
	if key = strings.TrimSpace(key); key == "" {
		key = "#" + strconv.Itoa(i+1)
	}
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return &domain.ValidationError{Field: section + "[" + key + "]." + ve.Field, Reason: ve.Reason}
	}
	return err
}
