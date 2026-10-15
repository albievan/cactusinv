package domain

import (
	"errors"
	"testing"
)

func TestParseCurrencyCode(t *testing.T) {
	tests := []struct {
		in      string
		want    CurrencyCode
		wantErr bool
	}{
		{"zar", "ZAR", false},
		{" USD ", "USD", false},
		{"US", "", true},
		{"U5D", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := ParseCurrencyCode(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseCurrencyCode(%q) = %q, %v", tt.in, got, err)
		}
		if err != nil && !errors.Is(err, ErrValidation) {
			t.Errorf("error %v is not ErrValidation", err)
		}
	}
}

func TestNewCurrency(t *testing.T) {
	if _, err := NewCurrency("ZAR", "South African Rand", "R", 2, true); err != nil {
		t.Fatal(err)
	}
	bad := []struct {
		code, name, sym string
		minor           int
	}{
		{"ZA", "x", "R", 2},
		{"ZAR", "", "R", 2},
		{"ZAR", "Rand", "RRRRRRRRR", 2},
		{"ZAR", "Rand", "R", 4},
		{"ZAR", "Rand", "R", -1},
	}
	for _, b := range bad {
		if _, err := NewCurrency(b.code, b.name, b.sym, b.minor, true); err == nil {
			t.Errorf("NewCurrency(%+v) expected error", b)
		}
	}
}

func TestParseAndFormatAmount(t *testing.T) {
	tests := []struct {
		in      string
		minor   int
		want    int64
		out     string
		wantErr bool
	}{
		{"2701.16", 2, 270116, "2701.16", false},
		{"550", 2, 55000, "550.00", false},
		{"0.5", 2, 50, "0.50", false},
		{".5", 2, 50, "0.50", false},
		{"-12.34", 2, -1234, "-12.34", false},
		{"+1", 2, 100, "1.00", false},
		{"1000", 0, 1000, "1000", false},
		{"1.234", 3, 1234, "1.234", false},
		{"1.234", 2, 0, "", true},
		{"abc", 2, 0, "", true},
		{"1.2.3", 2, 0, "", true},
		{"", 2, 0, "", true},
		{"99999999999999999999", 2, 0, "", true},
	}
	for _, tt := range tests {
		got, err := ParseAmount(tt.in, tt.minor)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ParseAmount(%q) err = %v", tt.in, err)
		}
		if err != nil {
			continue
		}
		if got != tt.want {
			t.Errorf("ParseAmount(%q) = %d, want %d", tt.in, got, tt.want)
		}
		if s := FormatAmount(got, tt.minor); s != tt.out {
			t.Errorf("FormatAmount(%d) = %q, want %q", got, s, tt.out)
		}
	}
}

func TestFormatGrouped(t *testing.T) {
	tests := []struct {
		amt   int64
		minor int
		want  string
	}{
		{325116, 2, "3,251.16"},
		{100, 2, "1.00"},
		{123456789, 2, "1,234,567.89"},
		{-123456789, 2, "-1,234,567.89"},
		{1000, 0, "1,000"},
		{5, 2, "0.05"},
	}
	for _, tt := range tests {
		if got := FormatGrouped(tt.amt, tt.minor, ","); got != tt.want {
			t.Errorf("FormatGrouped(%d) = %q, want %q", tt.amt, got, tt.want)
		}
	}
}

func TestMoneyAdd(t *testing.T) {
	a := Money{Amount: 100, Currency: "ZAR"}
	got, err := a.Add(Money{Amount: 250, Currency: "ZAR"})
	if err != nil || got.Amount != 350 {
		t.Fatalf("Add = %v, %v", got, err)
	}
	if _, err := a.Add(Money{Amount: 1, Currency: "USD"}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("mixed currencies: err = %v", err)
	}
}

func TestQuantityAndPercent(t *testing.T) {
	q, err := ParseQuantity("1.5")
	if err != nil || q != 1500 || q.String() != "1.5" {
		t.Fatalf("ParseQuantity(1.5) = %d %q %v", q, q.String(), err)
	}
	q, _ = ParseQuantity("14")
	if q.String() != "14" {
		t.Fatalf("Quantity(14).String() = %q", q.String())
	}
	if _, err := ParseQuantity("1.2345"); err == nil {
		t.Fatal("expected error for 4 decimals")
	}
	p, err := ParsePercent("12.5")
	if err != nil || p != 1250 || p.String() != "12.5" {
		t.Fatalf("ParsePercent = %d %v", p, err)
	}
	for _, bad := range []string{"101", "-1", "x"} {
		if _, err := ParsePercent(bad); err == nil {
			t.Errorf("ParsePercent(%q) expected error", bad)
		}
	}
}

func TestMulDivRound(t *testing.T) {
	tests := []struct{ a, b, d, want int64 }{
		{14000, 19294, 1000, 270116},
		{1500, 333, 1000, 500}, // 499.5 → 500
		{1, 5, 10, 1},          // 0.5 → 1
		{1, 4, 10, 0},          // 0.4 → 0
		{-1, 5, 10, -1},        // -0.5 → -1 (away from zero)
		{-1, 4, 10, 0},         // -0.4 → 0
		{1_000_000_000, 1_000_000_000_000, 1000, 1_000_000_000_000_000_000},
	}
	for _, tt := range tests {
		if got := MulDivRound(tt.a, tt.b, tt.d); got != tt.want {
			t.Errorf("MulDivRound(%d,%d,%d) = %d, want %d", tt.a, tt.b, tt.d, got, tt.want)
		}
	}
}
