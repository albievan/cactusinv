package domain

import (
	"slices"
	"strings"
)

// Product is an item in the look-up list. It has an explicit price per currency; there is
// no exchange-rate conversion (ADR-003).
type Product struct {
	ID          string
	SKU         string
	Name        string
	Description string // default invoice line description
	Unit        string // e.g. "user", "hour", "month"
	Prices      map[CurrencyCode]int64
	Active      bool
	Version     int
}

// Validate checks a product's invariants.
func (p *Product) Validate() error {
	p.SKU = strings.TrimSpace(p.SKU)
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	p.Unit = strings.TrimSpace(p.Unit)
	if p.SKU == "" || len(p.SKU) > 64 {
		return invalid("sku", "is required (max 64 characters)")
	}
	if p.Name == "" || len(p.Name) > 200 {
		return invalid("name", "is required (max 200 characters)")
	}
	if len(p.Description) > 1000 {
		return invalid("description", "max 1000 characters")
	}
	if len(p.Unit) > 32 {
		return invalid("unit", "max 32 characters")
	}
	if len(p.Prices) == 0 {
		return invalid("prices", "add the unit cost in at least one currency")
	}
	for c, amt := range p.Prices {
		if _, err := ParseCurrencyCode(string(c)); err != nil {
			return err
		}
		if amt < 0 {
			return invalid("prices", "must not be negative")
		}
	}
	return nil
}

// PriceIn returns the product's unit price in currency c.
func (p Product) PriceIn(c CurrencyCode) (Money, error) {
	amt, ok := p.Prices[c]
	if !ok {
		return Money{}, ErrNoPriceForCurrency
	}
	return Money{Amount: amt, Currency: c}, nil
}

// LineDescription is the text used when the product is added to an invoice.
func (p Product) LineDescription() string {
	if p.Description != "" {
		return p.Description
	}
	return p.Name
}

// Currencies lists the currencies the product is priced in, sorted.
func (p Product) Currencies() []CurrencyCode {
	out := make([]CurrencyCode, 0, len(p.Prices))
	for c := range p.Prices {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}
