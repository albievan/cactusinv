package domain

import (
	"strings"
	"time"
)

// AdhocCustomer is someone a quote is for who is not (yet) a customer: a prospect with
// just enough detail to address a quote. If the quote turns into a sale, ToCustomer makes
// a real customer of them; their quotes then belong to that customer.
type AdhocCustomer struct {
	ID        string
	Name      string
	Email     string
	Attention string
	Address   string
	Phone     string
	TaxNumber string
	Currency  CurrencyCode
	// CustomerID is the customer made from this one (empty until converted). A converted
	// ad-hoc customer is kept, unchanged, as the record of who the quotes were made out to.
	CustomerID  string
	ConvertedAt time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

// Validate checks and normalises an ad-hoc customer. Only the name and currency are
// required: a prospect may be quoted on paper before their e-mail address is known.
func (a *AdhocCustomer) Validate() error {
	for _, f := range []*string{&a.Name, &a.Email, &a.Attention, &a.Address, &a.Phone, &a.TaxNumber} {
		*f = strings.TrimSpace(*f)
	}
	if a.Name == "" || len(a.Name) > 200 {
		return invalid("name", "is required (max 200 characters)")
	}
	if a.Email != "" {
		if err := validEmail("email", a.Email); err != nil {
			return err
		}
	}
	if len(a.Attention) > 200 || len(a.Phone) > 100 || len(a.TaxNumber) > 64 {
		return invalid("attention", "attention max 200, phone max 100, tax number max 64 characters")
	}
	if len(a.Address) > 1000 {
		return invalid("address", "max 1000 characters")
	}
	if _, err := ParseCurrencyCode(string(a.Currency)); err != nil {
		return err
	}
	return nil
}

// Converted reports whether a customer has been made from this ad-hoc customer.
func (a *AdhocCustomer) Converted() bool { return a.CustomerID != "" }

// CustomerDetails is what converting an ad-hoc customer adds: what a customer needs and a
// prospect does not have. Empty fields keep the ad-hoc customer's own.
type CustomerDetails struct {
	Code     string
	Email    string
	Terms    PaymentTerms
	Currency CurrencyCode
}

// ToCustomer builds the customer an ad-hoc customer becomes (not yet stored; Validate runs
// on it). The customer is billed in the ad-hoc customer's currency unless d says otherwise.
func (a *AdhocCustomer) ToCustomer(id string, d CustomerDetails) (Customer, error) {
	if a.Converted() {
		return Customer{}, ErrInvalidTransition
	}
	c := Customer{
		ID: id, Code: d.Code, Name: a.Name, Email: a.Email, Attention: a.Attention,
		BillingAddress: a.Address, Phone: a.Phone, TaxNumber: a.TaxNumber, Currency: a.Currency,
		Terms: d.Terms, Active: true,
	}
	if strings.TrimSpace(d.Email) != "" {
		c.Email = d.Email
	}
	if d.Currency != "" {
		c.Currency = d.Currency
	}
	if c.Terms.Kind == "" {
		c.Terms = PaymentTerms{Kind: TermsOnReceipt}
	}
	if err := c.Validate(); err != nil {
		return Customer{}, err
	}
	return c, nil
}

// MarkConverted records the customer made from this ad-hoc customer.
func (a *AdhocCustomer) MarkConverted(customerID string, now time.Time) error {
	if a.Converted() {
		return ErrInvalidTransition
	}
	a.CustomerID = customerID
	a.ConvertedAt = now
	a.UpdatedAt = now
	return nil
}
