package ledger

import "testing"

func TestMoneyAndReconciliation(t *testing.T) {
	cents, err := Cents("123.456789")
	if err != nil || cents != "1.23456789" {
		t.Fatalf("cents: %s %v", cents, err)
	}
	for _, v := range []string{"NaN", "1e3", "0.0000000000001", "9999999999999999999", ""} {
		if _, e := Amount(v); e == nil {
			t.Fatal("accepted invalid amount", v)
		}
	}
	for _, tc := range []struct {
		a, b, status, d string
		comparable      bool
	}{{"0.1", "0.3", "mismatch", "0.2", true}, {"1", "1.01", "within_tolerance", "0.01", true}, {"1", "1", "matched", "0", true}, {"", "0", "pending_source", "", true}, {"1", "1", "not_comparable", "", false}} {
		s, d, e := Match(tc.a, tc.b, "0.01", tc.comparable)
		if e != nil || s != tc.status || d != tc.d {
			t.Fatalf("%+v got %s %s %v", tc, s, d, e)
		}
	}
}
func TestImportCorrectionKeysAndPrecision(t *testing.T) {
	csv := "period_start,period_end,model,amount,currency,charge_category,coverage\n2024-02-29,2024-03-01,unknown-model,0.123456789123,USD,usage,complete\n"
	p, e := ParseCSV([]byte(csv), "custom", "same-account-report", "America/New_York", "actual", "aggregate")
	if e != nil || p.Rejected != 0 || p.Totals["USD"] != "0.123456789123" {
		t.Fatalf("%+v %v", p, e)
	}
	first := p.Entries[0]
	key := first.Key
	first.Amount = "7.1"
	if NaturalKey(first) != key {
		t.Fatal("amount incorrectly participates in natural key")
	}
	if p.Entries[0].Start.Hour() != 5 {
		t.Fatal("source timezone not preserved")
	}
	duplicate := csv + "2024-02-29,2024-03-01,unknown-model,0.123456789123,USD,usage,complete\n"
	p, e = ParseCSV([]byte(duplicate), "custom", "scope", "UTC", "actual", "aggregate")
	if e != nil || p.Rejected != 1 {
		t.Fatal("duplicate aggregate not rejected")
	}
	_, e = ParseCSV([]byte("prompt,amount\nsecret,1\n"), "custom", "scope", "UTC", "actual", "aggregate")
	if e == nil {
		t.Fatal("prompt accepted")
	}
}
func TestCSVInjection(t *testing.T) {
	for _, v := range []string{"=SUM(A1)", "  @x", "\t-cmd"} {
		if SafeCell(v) != "'"+v {
			t.Fatal(v)
		}
	}
}
