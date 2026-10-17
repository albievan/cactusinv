package httpapi

import (
	"context"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// ---- settings import/export ----

const (
	settingsFileFormat  = SettingsFileFormat("cactusinv-settings")
	settingsFileVersion = SettingsFileFormatVersion(1)
)

func (h *handlers) ExportSettings(ctx context.Context, _ ExportSettingsRequestObject) (ExportSettingsResponseObject, error) {
	f, err := h.svc.ExportSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := SettingsFile{
		Format: settingsFileFormat, FormatVersion: settingsFileVersion,
		ExportedAt: h.clock.Now().UTC(), AppVersion: h.version,
		Currencies: make([]Currency, len(f.Currencies)), BankAccounts: make([]BankAccount, len(f.BankAccounts)),
		Customers: make([]Customer, len(f.Customers)),
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
	for i, c := range f.Customers {
		out.Customers[i] = toAPICustomer(c)
	}
	return ExportSettings200JSONResponse(out), nil
}

func (h *handlers) ImportSettings(ctx context.Context, req ImportSettingsRequestObject) (ImportSettingsResponseObject, error) {
	b := req.Body
	if b.Format != settingsFileFormat {
		return nil, &domain.ValidationError{Field: "format", Reason: "this is not a cactusInv settings file"}
	}
	if b.FormatVersion != settingsFileVersion {
		return nil, &domain.ValidationError{Field: "formatVersion", Reason: "the file was made by a newer cactusInv; update this one first"}
	}
	var f app.SettingsFile
	if b.Business != nil {
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
	if e := b.EmailServer; e != nil { // a password in the file is ignored
		f.EmailServer = &domain.SMTPSettings{Enabled: e.Enabled, Host: e.Host, Port: e.Port, Username: e.Username, From: e.From, TLS: string(e.Tls)}
	}
	for _, c := range b.Currencies {
		f.Currencies = append(f.Currencies, domain.Currency{Code: domain.CurrencyCode(c.Code), Name: c.Name, Symbol: c.Symbol, MinorUnits: c.MinorUnits, Active: c.Active})
	}
	for _, a := range b.BankAccounts {
		f.BankAccounts = append(f.BankAccounts, fromAPIBank(a.Id, a))
	}
	for _, c := range b.Customers {
		f.Customers = append(f.Customers, fromAPICustomer(c.Id, c))
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
		Currencies: counts(res.Currencies), BankAccounts: counts(res.BankAccounts), Customers: counts(res.Customers),
		Warnings: res.Warnings,
	}, nil
}
