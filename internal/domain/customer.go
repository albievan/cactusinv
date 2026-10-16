package domain

import (
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var invoiceCodeRe = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

// MaxCustomerCC is the most CC addresses a customer may have (the API's maxItems).
const MaxCustomerCC = 10

// Customer is someone the business invoices. A customer is billed in exactly one currency.
type Customer struct {
	ID              string
	Code            string // invoice-number code, e.g. "CD" in CD/1195/26
	Name            string
	Email           string
	CC              []string
	Attention       string
	BillingAddress  string
	ShippingName    string
	ShippingAddress string
	Phone           string
	TaxNumber       string
	Currency        CurrencyCode
	Terms           PaymentTerms
	BankAccountID   string // optional override of the currency default
	DefaultNotes    string
	Active          bool
	Version         int
}

// Validate checks a customer's invariants and normalises its fields.
func (c *Customer) Validate() error {
	for _, f := range []*string{
		&c.Code, &c.Name, &c.Email, &c.Attention, &c.BillingAddress, &c.ShippingName,
		&c.ShippingAddress, &c.Phone, &c.TaxNumber, &c.BankAccountID,
	} {
		*f = strings.TrimSpace(*f)
	}
	c.Code = strings.ToUpper(c.Code)
	if !invoiceCodeRe.MatchString(c.Code) {
		return invalid("code", "must be 1-10 letters or digits")
	}
	if c.Name == "" || len(c.Name) > 200 {
		return invalid("name", "is required (max 200 characters)")
	}
	if err := validEmail("email", c.Email); err != nil {
		return err
	}
	cc := c.CC[:0:0]
	for _, e := range c.CC {
		e = strings.TrimSpace(e)
		if e == "" || slices.ContainsFunc(cc, func(x string) bool { return strings.EqualFold(x, e) }) {
			continue
		}
		if err := validEmail("cc", e); err != nil {
			return err
		}
		cc = append(cc, e)
	}
	// Every e-mail goes to the customer plus these, and a message allows at most
	// MaxRecipients (the contract says 10 CCs).
	if len(cc) > MaxCustomerCC {
		return invalid("cc", "at most "+strconv.Itoa(MaxCustomerCC)+" addresses")
	}
	c.CC = cc
	if _, err := ParseCurrencyCode(string(c.Currency)); err != nil {
		return err
	}
	if _, err := NewPaymentTerms(c.Terms.Kind, c.Terms.Value); err != nil {
		return err
	}
	if len(c.BillingAddress) > 1000 || len(c.ShippingAddress) > 1000 {
		return invalid("address", "max 1000 characters")
	}
	if len(c.DefaultNotes) > 4000 {
		return invalid("defaultNotes", "max 4000 characters")
	}
	return nil
}

// ParseEmailList splits a comma/semicolon separated list and validates each address.
func ParseEmailList(field, s string) ([]string, error) {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if err := validEmail(field, p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func validEmail(field, s string) error {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 254 {
		return invalid(field, "must be a valid e-mail address")
	}
	return nil
}
