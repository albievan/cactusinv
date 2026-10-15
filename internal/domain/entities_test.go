package domain

import (
	"errors"
	"testing"
)

func TestCustomerValidate(t *testing.T) {
	c := cityDeep()
	c.Code = " cd "
	c.CC = []string{" a@b.example ", ""}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Code != "CD" || len(c.CC) != 1 || c.CC[0] != "a@b.example" {
		t.Fatalf("not normalised: %q %v", c.Code, c.CC)
	}
	tests := []struct {
		name   string
		mutate func(*Customer)
	}{
		{"code", func(c *Customer) { c.Code = "C/D" }},
		{"name", func(c *Customer) { c.Name = "" }},
		{"email", func(c *Customer) { c.Email = "juanita" }},
		{"email display name", func(c *Customer) { c.Email = "Jane <j@x.example>" }},
		{"cc", func(c *Customer) { c.CC = []string{"bad"} }},
		{"currency", func(c *Customer) { c.Currency = "R" }},
		{"terms", func(c *Customer) { c.Terms = PaymentTerms{Kind: TermsDaysAfter, Value: 400} }},
		{"address", func(c *Customer) { c.BillingAddress = string(make([]byte, 1001)) }},
		{"notes", func(c *Customer) { c.DefaultNotes = string(make([]byte, 4001)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cityDeep()
			tt.mutate(&c)
			if err := c.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestParseEmailList(t *testing.T) {
	got, err := ParseEmailList("cc", "a@x.example; b@x.example, ,")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := ParseEmailList("cc", "a@x.example, nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBankAccount(t *testing.T) {
	b := BankAccount{Currency: "ZAR", Label: " Capitec ", BankName: "Capitecbank", AccountNumber: "1234567890", Active: true, IsDefault: true}
	if err := b.Validate(); err != nil || b.Label != "Capitec" {
		t.Fatalf("Validate: %v %q", err, b.Label)
	}
	if d := b.Details(); d.AccountNumber != "1234567890" || d.BankName != "Capitecbank" {
		t.Fatalf("Details = %+v", d)
	}
	bad := []func(*BankAccount){
		func(b *BankAccount) { b.Currency = "" },
		func(b *BankAccount) { b.Label = "" },
		func(b *BankAccount) { b.BankName = "" },
		func(b *BankAccount) { b.AccountNumber = "" },
		func(b *BankAccount) { b.Active = false },
	}
	for i, m := range bad {
		x := b
		m(&x)
		if err := x.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
}

func TestResolveBankAccount(t *testing.T) {
	zarDefault := BankAccount{ID: "zar1", Currency: "ZAR", IsDefault: true, Active: true}
	zarOther := BankAccount{ID: "zar2", Currency: "ZAR", Active: true}
	zarOff := BankAccount{ID: "zar3", Currency: "ZAR"}
	usd := BankAccount{ID: "usd1", Currency: "USD", IsDefault: true, Active: true}
	all := []BankAccount{zarDefault, zarOther, zarOff, usd}
	c := cityDeep()

	tests := []struct {
		name     string
		override string
		currency CurrencyCode
		accounts []BankAccount
		wantID   string
		wantErr  error
	}{
		{"currency default", "", "ZAR", all, "zar1", nil},
		{"override", "zar2", "ZAR", all, "zar2", nil},
		{"override other currency", "usd1", "ZAR", all, "", ErrCurrencyMismatch},
		{"override inactive", "zar3", "ZAR", all, "", ErrInactive},
		{"unknown override falls back", "gone", "ZAR", all, "zar1", nil},
		{"usd customer", "", "USD", all, "usd1", nil},
		{"no default", "", "EUR", all, "", ErrNoBankAccountForCurrency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.BankAccountID = tt.override
			c.Currency = tt.currency
			got, err := ResolveBankAccount(c, tt.accounts)
			if !errors.Is(err, tt.wantErr) || got.ID != tt.wantID {
				t.Fatalf("got %q, %v", got.ID, err)
			}
		})
	}
}

func TestProduct(t *testing.T) {
	p := Product{SKU: " SS-USER ", Name: "Stock system user", Prices: map[CurrencyCode]int64{"ZAR": 19294, "USD": 1100}}
	if err := p.Validate(); err != nil || p.SKU != "SS-USER" {
		t.Fatalf("Validate: %v", err)
	}
	m, err := p.PriceIn("ZAR")
	if err != nil || m.Amount != 19294 || m.Currency != "ZAR" {
		t.Fatalf("PriceIn = %+v %v", m, err)
	}
	if _, err := p.PriceIn("EUR"); !errors.Is(err, ErrNoPriceForCurrency) {
		t.Fatalf("EUR: %v", err)
	}
	if cs := p.Currencies(); len(cs) != 2 || cs[0] != "USD" || cs[1] != "ZAR" {
		t.Fatalf("Currencies = %v", cs)
	}
	if p.LineDescription() != "Stock system user" {
		t.Fatal("LineDescription falls back to name")
	}
	p.Description = "Stock system usage"
	if p.LineDescription() != "Stock system usage" {
		t.Fatal("LineDescription prefers description")
	}
	bad := []func(*Product){
		func(p *Product) { p.SKU = "" },
		func(p *Product) { p.Name = "" },
		func(p *Product) { p.Prices = map[CurrencyCode]int64{"ZAR": -1} },
		func(p *Product) { p.Prices = map[CurrencyCode]int64{"Z": 1} },
		func(p *Product) { p.Unit = string(make([]byte, 33)) },
		func(p *Product) { p.Description = string(make([]byte, 1001)) },
	}
	for i, m := range bad {
		x := p
		m(&x)
		if err := x.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestValidationErrorText(t *testing.T) {
	err := invalid("name", "is required")
	if err.Error() != "name: is required" {
		t.Fatal(err.Error())
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "name" {
		t.Fatal("errors.As")
	}
}
