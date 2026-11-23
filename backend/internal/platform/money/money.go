// Package money implements exact monetary amounts in integer minor units.
//
// Floating point is never used for money anywhere in this codebase. An Amount
// carries its own Currency and, unlike a naive int64 wrapper, refuses to be
// combined with an amount in a different currency.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Currency is an ISO-4217 alphabetic code. Only codes configured in
// AllowedCurrencies may be used by the platform.
type Currency string

const (
	USD Currency = "USD"
	EUR Currency = "EUR"
	GBP Currency = "GBP"
)

// AllowedCurrencies is the set of settlement currencies the platform supports.
// Presentment currencies are converted at the processor boundary; the ledger
// stores the settlement currency only.
var AllowedCurrencies = map[Currency]struct{}{
	USD: {}, EUR: {}, GBP: {},
}

// MinorUnits holds the number of decimal places for a currency (e.g. USD = 2).
// Zero-decimal currencies are intentionally absent until they are supported.
var minorUnits = map[Currency]int{
	USD: 2, EUR: 2, GBP: 2,
}

var (
	// ErrCurrencyMismatch is returned when amounts in different currencies are combined.
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
	// ErrInvalidCurrency is returned for a currency outside AllowedCurrencies.
	ErrInvalidCurrency = errors.New("money: unsupported currency")
	// ErrNegativeAmount is returned when a negative amount is constructed.
	ErrNegativeAmount = errors.New("money: negative amount")
	// ErrFractionalAmount is returned when an amount has sub-minor-unit precision.
	ErrFractionalAmount = errors.New("money: fractional minor units")
)

// Amount is a non-negative quantity of money in minor units of Currency.
type Amount struct {
	units    int64
	currency Currency
}

// New constructs an Amount, validating the currency and the sign.
func New(units int64, c Currency) (Amount, error) {
	if _, ok := AllowedCurrencies[c]; !ok {
		return Amount{}, fmt.Errorf("%w: %q", ErrInvalidCurrency, c)
	}
	if units < 0 {
		return Amount{}, fmt.Errorf("%w: %d", ErrNegativeAmount, units)
	}
	return Amount{units: units, currency: c}, nil
}

// MustNew is New for compile-time-known constants; it panics on invalid input
// and is therefore only appropriate for tests and package-level constants.
func MustNew(units int64, c Currency) Amount {
	a, err := New(units, c)
	if err != nil {
		panic(err)
	}
	return a
}

// Zero returns the zero amount in the given currency.
func Zero(c Currency) Amount { return Amount{units: 0, currency: c} }

// Units returns the amount in minor units (e.g. cents).
func (a Amount) Units() int64 { return a.units }

// Currency returns the amount's currency.
func (a Amount) Currency() Currency { return a.currency }

// IsZero reports whether the amount is zero.
func (a Amount) IsZero() bool { return a.units == 0 }

// Add returns a+b, requiring identical currencies.
func (a Amount) Add(b Amount) (Amount, error) {
	if a.currency != b.currency {
		return Amount{}, fmt.Errorf("%w: %s + %s", ErrCurrencyMismatch, a.currency, b.currency)
	}
	return Amount{units: a.units + b.units, currency: a.currency}, nil
}

// Sub returns a-b. It fails rather than producing a negative amount; callers
// that may legitimately go negative (for example a fan wallet overdraft check)
// must compare first.
func (a Amount) Sub(b Amount) (Amount, error) {
	if a.currency != b.currency {
		return Amount{}, fmt.Errorf("%w: %s - %s", ErrCurrencyMismatch, a.currency, b.currency)
	}
	if b.units > a.units {
		return Amount{}, fmt.Errorf("%w: %d - %d", ErrNegativeAmount, a.units, b.units)
	}
	return Amount{units: a.units - b.units, currency: a.currency}, nil
}

// Cmp compares two amounts: -1 if a<b, 0 if equal, +1 if a>b.
func (a Amount) Cmp(b Amount) int {
	switch {
	case a.units < b.units:
		return -1
	case a.units > b.units:
		return 1
	default:
		return 0
	}
}

// Percent returns floor(amount * bps / 10000) and the leftover fraction that
// could not be expressed in whole minor units, measured in 1/10000 of a minor
// unit. The invariant always holds:
//
//	part.Units()*10000 + fraction == a.Units()*bps
//
// Callers decide which side of the split absorbs the fraction, so commission and
// payout maths never silently lose or invent money through rounding. It panics
// on programmer errors only (bps outside 0..10000, or an overflow).
func (a Amount) Percent(bps int64) (part Amount, fraction int64) {
	if bps < 0 || bps > 10000 {
		panic(fmt.Sprintf("money: basis points out of range: %d", bps))
	}
	if bps != 0 && a.units > math.MaxInt64/bps {
		panic(fmt.Sprintf("money: percent overflows int64: %d bps on %d minor units", bps, a.units))
	}
	total := a.units * bps
	return Amount{units: total / 10000, currency: a.currency}, total % 10000
}

// String renders the amount for logs and display, e.g. "12.34 USD".
func (a Amount) String() string {
	return fmt.Sprintf("%s %s", a.Decimal(), a.currency)
}

// Decimal renders the amount as a fixed-precision decimal string.
func (a Amount) Decimal() string {
	scale, ok := minorUnits[a.currency]
	if !ok {
		return strconv.FormatInt(a.units, 10)
	}
	divisor := int64(1)
	for i := 0; i < scale; i++ {
		divisor *= 10
	}
	whole := a.units / divisor
	frac := a.units % divisor
	return fmt.Sprintf("%d.%0*d", whole, scale, frac)
}

// Parse converts a decimal string such as "12.34" into an Amount. It rejects
// sub-minor-unit precision instead of silently rounding.
func Parse(s string, c Currency) (Amount, error) {
	scale, ok := minorUnits[c]
	if !ok {
		return Amount{}, fmt.Errorf("%w: %q", ErrInvalidCurrency, c)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return Amount{}, fmt.Errorf("money: empty amount")
	}
	if strings.HasPrefix(s, "-") {
		return Amount{}, fmt.Errorf("%w: %q", ErrNegativeAmount, s)
	}
	wholePart, fracPart, hasFrac := strings.Cut(s, ".")
	if wholePart == "" {
		wholePart = "0"
	}
	whole, err := strconv.ParseInt(wholePart, 10, 64)
	if err != nil {
		return Amount{}, fmt.Errorf("money: invalid whole part %q: %w", wholePart, err)
	}
	if hasFrac && len(fracPart) > scale {
		return Amount{}, fmt.Errorf("%w: %q has more than %d decimals", ErrFractionalAmount, s, scale)
	}
	for len(fracPart) < scale {
		fracPart += "0"
	}
	frac := int64(0)
	if fracPart != "" {
		frac, err = strconv.ParseInt(fracPart, 10, 64)
		if err != nil {
			return Amount{}, fmt.Errorf("money: invalid fraction %q: %w", fracPart, err)
		}
	}
	divisor := int64(1)
	for i := 0; i < scale; i++ {
		divisor *= 10
	}
	return New(whole*divisor+frac, c)
}
