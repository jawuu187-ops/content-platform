package money

import "testing"

func TestNewRejectsNegativeAndUnknownCurrency(t *testing.T) {
	if _, err := New(-1, USD); err == nil {
		t.Fatal("expected negative amount to be rejected")
	}
	if _, err := New(1, Currency("XYZ")); err == nil {
		t.Fatal("expected unknown currency to be rejected")
	}
}

func TestAddSubCurrencyMismatch(t *testing.T) {
	a := MustNew(100, USD)
	b := MustNew(50, EUR)
	if _, err := a.Add(b); err == nil {
		t.Fatal("expected currency mismatch on Add")
	}
	if _, err := a.Sub(b); err == nil {
		t.Fatal("expected currency mismatch on Sub")
	}
}

func TestSubRefusesNegative(t *testing.T) {
	a := MustNew(50, USD)
	b := MustNew(100, USD)
	if _, err := a.Sub(b); err == nil {
		t.Fatal("expected negative result to be refused")
	}
}

func TestPercentSplitsWithoutLoss(t *testing.T) {
	// 20% platform commission on 33.33 USD: 6.666 USD -> 666 cents + 60 remainder
	gross := MustNew(3333, USD)
	part, remainder := gross.Percent(2000)
	if part.Units() != 666 || remainder.Units() != 60 {
		t.Fatalf("unexpected split: part=%d remainder=%d", part.Units(), remainder.Units())
	}
	if part.Units()*10000+remainder.Units() != gross.Units()*2000 {
		t.Fatal("split lost value")
	}
}

func TestParseAndRoundTrip(t *testing.T) {
	cases := map[string]int64{
		"0":       0,
		"1":       100,
		"1.5":     150,
		"12.34":   1234,
		"1000.00": 100000,
		".99":     99,
	}
	for in, want := range cases {
		got, err := Parse(in, USD)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if got.Units() != want {
			t.Fatalf("Parse(%q) = %d, want %d", in, got.Units(), want)
		}
	}
	for _, bad := range []string{"1.234", "-5", "", "abc"} {
		if _, err := Parse(bad, USD); err == nil {
			t.Fatalf("Parse(%q) should have failed", bad)
		}
	}
}

func TestDecimalFormatting(t *testing.T) {
	if got := MustNew(5, USD).Decimal(); got != "0.05" {
		t.Fatalf("Decimal = %q, want 0.05", got)
	}
	if got := MustNew(123456, USD).Decimal(); got != "1234.56" {
		t.Fatalf("Decimal = %q, want 1234.56", got)
	}
}
