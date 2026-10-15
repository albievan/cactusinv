package domain

import "strings"

// BankAccount is where customers pay. Each belongs to exactly one currency; each currency
// has at most one default account (ADR-003).
type BankAccount struct {
	ID            string
	Currency      CurrencyCode
	Label         string
	BankName      string
	Branch        string
	BranchCode    string
	AccountName   string
	AccountNumber string
	AccountType   string
	SwiftBIC      string
	IBAN          string
	Country       string
	IsDefault     bool
	Active        bool
	Version       int
}

// Validate checks a bank account's invariants and trims its fields.
func (b *BankAccount) Validate() error {
	for _, f := range []*string{
		&b.Label, &b.BankName, &b.Branch, &b.BranchCode, &b.AccountName,
		&b.AccountNumber, &b.AccountType, &b.SwiftBIC, &b.IBAN, &b.Country,
	} {
		*f = strings.TrimSpace(*f)
	}
	if _, err := ParseCurrencyCode(string(b.Currency)); err != nil {
		return err
	}
	if b.Label == "" || len(b.Label) > 80 {
		return invalid("label", "is required (max 80 characters)")
	}
	if b.BankName == "" || len(b.BankName) > 120 {
		return invalid("bankName", "is required (max 120 characters)")
	}
	if b.AccountNumber == "" || len(b.AccountNumber) > 64 {
		return invalid("accountNumber", "is required (max 64 characters)")
	}
	if len(b.Country) > 80 {
		return invalid("country", "max 80 characters")
	}
	if b.IsDefault && !b.Active {
		return invalid("isDefault", "an inactive account cannot be the default")
	}
	return nil
}

// Details snapshots the fields printed on an invoice.
func (b BankAccount) Details() BankDetails {
	return BankDetails{
		BankName: b.BankName, Branch: b.Branch, BranchCode: b.BranchCode,
		AccountName: b.AccountName, AccountNumber: b.AccountNumber, AccountType: b.AccountType,
		SwiftBIC: b.SwiftBIC, IBAN: b.IBAN, Country: b.Country,
	}
}

// BankDetails is the frozen copy of a bank account printed on an issued invoice.
type BankDetails struct {
	BankName      string
	Branch        string
	BranchCode    string
	AccountName   string
	AccountNumber string
	AccountType   string
	SwiftBIC      string
	IBAN          string
	Country       string
}

// ResolveBankAccount picks the account an invoice for customer c is paid into: the
// customer's override when set, otherwise the currency's default (ADR-003).
func ResolveBankAccount(c Customer, accounts []BankAccount) (BankAccount, error) {
	if c.BankAccountID != "" {
		for _, a := range accounts {
			if a.ID == c.BankAccountID {
				if a.Currency != c.Currency {
					return BankAccount{}, ErrCurrencyMismatch
				}
				if !a.Active {
					return BankAccount{}, ErrInactive
				}
				return a, nil
			}
		}
	}
	for _, a := range accounts {
		if a.Currency == c.Currency && a.IsDefault && a.Active {
			return a, nil
		}
	}
	return BankAccount{}, ErrNoBankAccountForCurrency
}
