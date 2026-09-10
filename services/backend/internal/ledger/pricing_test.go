package ledger

import "testing"

func TestCacheBucketsAreExclusive(t *testing.T) {
	r := RateCard{"2", "8", "0.5", "2.5", "4"}
	amount, e := CalculateTextCost(map[string]string{"input_total": "1000000", "input_cached_subset": "250000", "output": "100000"}, r)
	if e != nil || amount != "2.425" {
		t.Fatalf("%s %v", amount, e)
	}
	amount, e = CalculateTextCost(map[string]string{"input_uncached": "750000", "cache_read": "250000", "output": "100000", "cache_write_5m": "10000", "cache_write_1h": "0"}, r)
	if e != nil || amount != "2.45" {
		t.Fatalf("%s %v", amount, e)
	}
	if _, e = CalculateTextCost(map[string]string{"input_total": "1", "input_cached_subset": "2", "output": "0"}, r); e == nil {
		t.Fatal("overlapping cached tokens accepted")
	}
}
