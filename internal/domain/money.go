package domain

import (
	"math/big"
	"strconv"
	"strings"
)

// CurrencyCode is an upper-case ISO 4217 alphabetic code such as "ZAR".
type CurrencyCode string

// ParseCurrencyCode validates and normalises a currency code.
func ParseCurrencyCode(s string) (CurrencyCode, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 3 {
		return "", invalid("currency", "must be a 3-letter ISO 4217 code")
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return "", invalid("currency", "must be a 3-letter ISO 4217 code")
		}
	}
	return CurrencyCode(s), nil
}

// Currency is a currency the business invoices in.
type Currency struct {
	Code       CurrencyCode
	Name       string
	Symbol     string
	MinorUnits int
	Active     bool
}

// NewCurrency validates a currency definition.
func NewCurrency(code, name, symbol string, minorUnits int, active bool) (Currency, error) {
	c, err := ParseCurrencyCode(code)
	if err != nil {
		return Currency{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return Currency{}, invalid("name", "is required (max 64 characters)")
	}
	symbol = strings.TrimSpace(symbol)
	if len(symbol) > 8 {
		return Currency{}, invalid("symbol", "max 8 characters")
	}
	if minorUnits < 0 || minorUnits > 3 {
		return Currency{}, invalid("minorUnits", "must be between 0 and 3")
	}
	return Currency{Code: c, Name: name, Symbol: symbol, MinorUnits: minorUnits, Active: active}, nil
}

// Money is an exact amount in a currency's minor unit (ADR-005).
type Money struct {
	Amount   int64
	Currency CurrencyCode
}

// Add returns m+o, refusing to mix currencies.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{Amount: m.Amount + o.Amount, Currency: m.Currency}, nil
}

// maxAmountDigits bounds parsed amounts well inside int64.
const maxAmountDigits = 15

// ParseAmount parses a decimal string such as "2701.16" into minor units for a currency
// with the given number of minor units. More decimals than the currency allows is an error,
// never a silent rounding.
func ParseAmount(s string, minorUnits int) (int64, error) {
	return parseFixed(s, minorUnits, "amount")
}

// FormatAmount renders minor units as a plain decimal string, e.g. 270116 → "2701.16".
func FormatAmount(amount int64, minorUnits int) string {
	return formatFixed(amount, minorUnits, true)
}

// FormatGrouped renders minor units with thousands separators, e.g. "12 701.16".
func FormatGrouped(amount int64, minorUnits int, sep string) string {
	s := FormatAmount(amount, minorUnits)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(r)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// Quantity is a fixed-point quantity in thousandths (milli-units): 1.5 hours = 1500.
type Quantity int64

// QuantityScale is the number of milli-units in one unit.
const QuantityScale = 1000

// ParseQuantity parses "14" or "1.5" into a Quantity.
func ParseQuantity(s string) (Quantity, error) {
	v, err := parseFixed(s, 3, "quantity")
	return Quantity(v), err
}

// String renders a quantity without trailing zeros: 14000 → "14", 1500 → "1.5".
func (q Quantity) String() string { return formatFixed(int64(q), 3, false) }

// BasisPoints is a rate in hundredths of a percent: 1500 = 15 %.
type BasisPoints int

// ParsePercent parses "15" or "12.5" (percent) into basis points.
func ParsePercent(s string) (BasisPoints, error) {
	v, err := parseFixed(s, 2, "percent")
	if err != nil {
		return 0, err
	}
	if v < 0 || v > 10000 {
		return 0, invalid("percent", "must be between 0 and 100")
	}
	return BasisPoints(v), nil
}

// String renders basis points as a percent without trailing zeros: 1500 → "15".
func (b BasisPoints) String() string { return formatFixed(int64(b), 2, false) }

// MulDivRound returns round(a*b/d) with half-away-from-zero rounding, computed without
// overflow. d must be positive.
func MulDivRound(a, b, d int64) int64 {
	n := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	neg := n.Sign() < 0
	n.Abs(n)
	q, r := new(big.Int).QuoRem(n, big.NewInt(d), new(big.Int))
	if new(big.Int).Mul(r, big.NewInt(2)).Cmp(big.NewInt(d)) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q.Int64()
}

func parseFixed(s string, scale int, field string) (int64, error) {
	s = normalizeDecimalComma(strings.TrimSpace(s))
	if s == "" {
		return 0, invalid(field, "is required")
	}
	neg := false
	if s[0] == '-' || s[0] == '+' {
		neg = s[0] == '-'
		s = s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" && frac == "" { // "-", "+", "." or "-." are not numbers
		return 0, invalid(field, "is not a valid number")
	}
	if intPart == "" {
		intPart = "0"
	}
	if len(frac) > scale {
		return 0, invalid(field, "has too many decimal places (max "+strconv.Itoa(scale)+")")
	}
	if len(intPart)+scale > maxAmountDigits+3 || !allDigits(intPart) || !allDigits(frac) {
		return 0, invalid(field, "is not a valid number")
	}
	frac += strings.Repeat("0", scale-len(frac))
	v, err := strconv.ParseInt(intPart+frac, 10, 64)
	if err != nil {
		return 0, invalid(field, "is not a valid number")
	}
	if neg {
		v = -v
	}
	return v, nil
}

func formatFixed(v int64, scale int, keepZeros bool) string {
	neg := v < 0
	u := uint64(v)
	if neg {
		u = uint64(-v)
	}
	digits := strconv.FormatUint(u, 10)
	if scale > 0 {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		intPart, frac := digits[:len(digits)-scale], digits[len(digits)-scale:]
		if !keepZeros {
			frac = strings.TrimRight(frac, "0")
		}
		digits = intPart
		if frac != "" {
			digits += "." + frac
		}
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeDecimalComma reads a decimal comma as a point ("192,94" → "192.94"), as phones in
// comma regions type it, when it cannot be a thousands separator: exactly one comma, no
// point, and not exactly three digits after it ("1,500" is left as is and refused as
// ambiguous). The web app applies the same rule (normalizeDecimal).
func normalizeDecimalComma(s string) string {
	i := strings.IndexByte(s, ',')
	if i < 0 || strings.Count(s, ",") != 1 || strings.Contains(s, ".") || len(s)-i-1 == 3 {
		return s
	}
	return s[:i] + "." + s[i+1:]
}
