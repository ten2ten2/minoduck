package ledger

import (
	"fmt"
	"github.com/shopspring/decimal"
)

type RateCard struct{ Input, Output, Read, Write5m, Write1h string }

// CalculateTextCost uses mutually exclusive buckets and explicit historical rates.
func CalculateTextCost(metrics map[string]string, r RateCard) (string, error) {
	value := func(key string, required bool) (decimal.Decimal, error) {
		s, ok := metrics[key]
		if !ok || s == "" {
			if required {
				return decimal.Zero, fmt.Errorf("INCOMPLETE_USAGE")
			}
			return decimal.Zero, nil
		}
		v, e := Amount(s)
		if e != nil || v.IsNegative() {
			return decimal.Zero, fmt.Errorf("INVALID_USAGE")
		}
		return v, nil
	}
	var input, read decimal.Decimal
	var e error
	if _, ok := metrics["input_uncached"]; ok {
		input, e = value("input_uncached", true)
		if e != nil {
			return "", e
		}
		read, e = value("cache_read", true)
	} else {
		input, e = value("input_total", true)
		if e != nil {
			return "", e
		}
		read, e = value("input_cached_subset", true)
		input = input.Sub(read)
		if input.IsNegative() {
			return "", fmt.Errorf("INVALID_CACHE_OVERLAP")
		}
	}
	if e != nil {
		return "", e
	}
	output, e := value("output", true)
	if e != nil {
		return "", e
	}
	write5, e := value("cache_write_5m", false)
	if e != nil {
		return "", e
	}
	write1, e := value("cache_write_1h", false)
	if e != nil {
		return "", e
	}
	total := decimal.Zero
	for _, v := range []struct {
		count decimal.Decimal
		rate  string
	}{{input, r.Input}, {output, r.Output}, {read, r.Read}, {write5, r.Write5m}, {write1, r.Write1h}} {
		price, e := Amount(v.rate)
		if e != nil || price.IsNegative() {
			return "", fmt.Errorf("INVALID_RATE")
		}
		total = total.Add(v.count.Mul(price).Div(decimal.NewFromInt(1000000)))
	}
	if total.Exponent() < -12 && !total.Equal(total.Truncate(12)) {
		return "", fmt.Errorf("AMOUNT_PRECISION_EXCEEDED")
	}
	return total.String(), nil
}
