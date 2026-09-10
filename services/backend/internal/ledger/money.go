// Package ledger owns exact monetary semantics. Floats never enter the ledger.
package ledger

import (
	"errors"
	"github.com/shopspring/decimal"
	"regexp"
)

var decimalPattern = regexp.MustCompile(`^-?\d{1,18}(\.\d{1,12})?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func Amount(s string) (decimal.Decimal, error) {
	if !decimalPattern.MatchString(s) {
		return decimal.Zero, errors.New("INVALID_AMOUNT")
	}
	return decimal.NewFromString(s)
}

func ValidCurrency(s string) bool { return currencyPattern.MatchString(s) }

func Cents(s string) (string, error) {
	v, err := Amount(s)
	if err != nil {
		return "", err
	}
	return v.Div(decimal.NewFromInt(100)).String(), nil
}

func Match(expected, billed, tolerance string, comparable bool) (string, string, error) {
	if !comparable {
		return "not_comparable", "", nil
	}
	if expected == "" || billed == "" {
		return "pending_source", "", nil
	}
	a, e := Amount(expected)
	if e != nil {
		return "", "", e
	}
	b, e := Amount(billed)
	if e != nil {
		return "", "", e
	}
	t, e := Amount(tolerance)
	if e != nil || t.IsNegative() {
		return "", "", errors.New("INVALID_TOLERANCE")
	}
	d := b.Sub(a)
	status := "mismatch"
	if d.IsZero() {
		status = "matched"
	} else if d.Abs().LessThanOrEqual(t) {
		status = "within_tolerance"
	}
	return status, d.String(), nil
}

// SafeCell defeats CSV formula injection, including a leading whitespace prefix.
func SafeCell(s string) string {
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		if c == '=' || c == '+' || c == '-' || c == '@' {
			return "'" + s
		}
		break
	}
	return s
}
