package mariadb

import (
	"context"
	"time"

	"github.com/albievan/simpleinvoice/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/simpleinvoice/internal/domain"
)

type businessRepo struct{ q *sqlcgen.Queries }

func (r businessRepo) Get(ctx context.Context) (domain.BusinessProfile, error) {
	b, err := r.q.GetBusiness(ctx)
	if err != nil {
		return domain.BusinessProfile{}, mapErr(err)
	}
	return domain.BusinessProfile{
		Name: b.Name, AddressLines: b.AddressLines, Phone: b.Phone, Email: b.Email, TaxNumber: b.TaxNumber,
		NumberFormat: b.NumberFormat, TimeZone: b.TimeZone, DefaultNotes: b.DefaultNotes,
		EmailSubject: b.EmailSubject, EmailBody: b.EmailBody, VATRate: domain.BasisPoints(b.VatRateBp),
		Version: int(b.Version),
	}, nil
}

func (r businessRepo) Save(ctx context.Context, b domain.BusinessProfile) error {
	now := time.Now().UTC()
	if b.Version == 0 {
		return mapErr(r.q.InsertBusiness(ctx, sqlcgen.InsertBusinessParams{
			Name: b.Name, AddressLines: b.AddressLines, Phone: b.Phone, Email: b.Email, TaxNumber: b.TaxNumber,
			NumberFormat: b.NumberFormat, TimeZone: b.TimeZone, DefaultNotes: b.DefaultNotes,
			EmailSubject: b.EmailSubject, EmailBody: b.EmailBody, VatRateBp: i32(int(b.VATRate)), UpdatedAt: now,
		}))
	}
	n, err := r.q.UpdateBusiness(ctx, sqlcgen.UpdateBusinessParams{
		Name: b.Name, AddressLines: b.AddressLines, Phone: b.Phone, Email: b.Email, TaxNumber: b.TaxNumber,
		NumberFormat: b.NumberFormat, TimeZone: b.TimeZone, DefaultNotes: b.DefaultNotes,
		EmailSubject: b.EmailSubject, EmailBody: b.EmailBody, VatRateBp: i32(int(b.VATRate)), UpdatedAt: now,
		Version: i32(b.Version),
	})
	return affected(n, err, domain.ErrConflict)
}

type sequenceRepo struct{ q *sqlcgen.Queries }

func (r sequenceRepo) Next(ctx context.Context) (int64, error) {
	v, err := r.q.LockSequence(ctx)
	if err != nil {
		return 0, mapErr(err)
	}
	return v, mapErr(r.q.SetSequence(ctx, v+1))
}

func (r sequenceRepo) Peek(ctx context.Context) (int64, error) {
	v, err := r.q.PeekSequence(ctx)
	return v, mapErr(err)
}

func (r sequenceRepo) Set(ctx context.Context, next int64) error {
	return mapErr(r.q.SetSequence(ctx, next))
}

type currencyRepo struct{ q *sqlcgen.Queries }

func toCurrency(c sqlcgen.Currency) domain.Currency {
	return domain.Currency{Code: domain.CurrencyCode(c.Code), Name: c.Name, Symbol: c.Symbol, MinorUnits: int(c.MinorUnits), Active: c.Active}
}

func (r currencyRepo) List(ctx context.Context) ([]domain.Currency, error) {
	rows, err := r.q.ListCurrencies(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.Currency, len(rows))
	for i, c := range rows {
		out[i] = toCurrency(c)
	}
	return out, nil
}

func (r currencyRepo) Get(ctx context.Context, code domain.CurrencyCode) (domain.Currency, error) {
	c, err := r.q.GetCurrency(ctx, string(code))
	if err != nil {
		return domain.Currency{}, mapErr(err)
	}
	return toCurrency(c), nil
}

func (r currencyRepo) Create(ctx context.Context, c domain.Currency) error {
	return mapErr(r.q.InsertCurrency(ctx, sqlcgen.InsertCurrencyParams{
		Code: string(c.Code), Name: c.Name, Symbol: c.Symbol, MinorUnits: minorUnits(c.MinorUnits), Active: c.Active,
	}))
}

func (r currencyRepo) Update(ctx context.Context, c domain.Currency) error {
	n, err := r.q.UpdateCurrency(ctx, sqlcgen.UpdateCurrencyParams{Name: c.Name, Symbol: c.Symbol, Active: c.Active, Code: string(c.Code)})
	return affected(n, err, domain.ErrNotFound)
}

type bankRepo struct{ q *sqlcgen.Queries }

func toBank(b sqlcgen.BankAccount) domain.BankAccount {
	return domain.BankAccount{
		ID: b.ID, Currency: domain.CurrencyCode(b.CurrencyCode), Label: b.Label, BankName: b.BankName, Branch: b.Branch,
		BranchCode: b.BranchCode, AccountName: b.AccountName, AccountNumber: b.AccountNumber, AccountType: b.AccountType,
		SwiftBIC: b.SwiftBic, IBAN: b.Iban, IsDefault: b.IsDefault, Active: b.Active, Version: int(b.Version),
	}
}

func (r bankRepo) List(ctx context.Context, currency domain.CurrencyCode) ([]domain.BankAccount, error) {
	rows, err := r.q.ListBankAccounts(ctx, sqlcgen.ListBankAccountsParams{Currency: string(currency)})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.BankAccount, len(rows))
	for i, b := range rows {
		out[i] = toBank(b)
	}
	return out, nil
}

func (r bankRepo) Get(ctx context.Context, id string) (domain.BankAccount, error) {
	b, err := r.q.GetBankAccount(ctx, id)
	if err != nil {
		return domain.BankAccount{}, mapErr(err)
	}
	return toBank(b), nil
}

func (r bankRepo) Create(ctx context.Context, b domain.BankAccount) error {
	return mapErr(r.q.InsertBankAccount(ctx, sqlcgen.InsertBankAccountParams{
		ID: b.ID, CurrencyCode: string(b.Currency), Label: b.Label, BankName: b.BankName, Branch: b.Branch,
		BranchCode: b.BranchCode, AccountName: b.AccountName, AccountNumber: b.AccountNumber, AccountType: b.AccountType,
		SwiftBic: b.SwiftBIC, Iban: b.IBAN, IsDefault: b.IsDefault, Active: b.Active,
	}))
}

func (r bankRepo) Update(ctx context.Context, b domain.BankAccount) error {
	n, err := r.q.UpdateBankAccount(ctx, sqlcgen.UpdateBankAccountParams{
		Label: b.Label, BankName: b.BankName, Branch: b.Branch, BranchCode: b.BranchCode, AccountName: b.AccountName,
		AccountNumber: b.AccountNumber, AccountType: b.AccountType, SwiftBic: b.SwiftBIC, Iban: b.IBAN,
		IsDefault: b.IsDefault, Active: b.Active, ID: b.ID, Version: i32(b.Version),
	})
	return affected(n, err, domain.ErrConflict)
}

func (r bankRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteBankAccount(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

func (r bankRepo) ClearDefault(ctx context.Context, currency domain.CurrencyCode, keepID string) error {
	return mapErr(r.q.ClearDefaultBankAccount(ctx, sqlcgen.ClearDefaultBankAccountParams{CurrencyCode: string(currency), ID: keepID}))
}

// minorUnits converts a validated 0–3 minor-unit count for the TINYINT column.
func minorUnits(n int) int8 {
	switch {
	case n <= 0:
		return 0
	case n >= 3:
		return 3
	default:
		return int8(n)
	}
}
