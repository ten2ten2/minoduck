package ledger

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/shopspring/decimal"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxFileBytes = 20 * 1024 * 1024
const MaxRows = 100000

type Entry struct {
	Key        string            `json:"key"`
	Start      time.Time         `json:"period_start"`
	End        time.Time         `json:"period_end"`
	Timezone   string            `json:"source_timezone"`
	Provider   string            `json:"billing_provider"`
	Vendor     string            `json:"model_vendor"`
	Model      string            `json:"model"`
	Category   string            `json:"charge_category"`
	Kind       string            `json:"cost_kind"`
	Currency   string            `json:"currency"`
	Amount     string            `json:"amount"`
	Project    string            `json:"project"`
	Scope      string            `json:"source_scope"`
	SourceID   string            `json:"source_event_id"`
	SourceRef  string            `json:"-"`
	Coverage   string            `json:"coverage"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}
type RowError struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
}
type Preview struct {
	Entries  []Entry           `json:"-"`
	Rows     []Entry           `json:"rows"`
	Errors   []RowError        `json:"errors"`
	Count    int               `json:"count"`
	Rejected int               `json:"rejected"`
	Totals   map[string]string `json:"totals"`
	Hash     string            `json:"hash"`
	Start    *time.Time        `json:"period_start"`
	End      *time.Time        `json:"period_end"`
}

type periodSpan struct {
	Start time.Time
	End   time.Time
}

func Hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

// NaturalKey excludes mutable values. Event reports use the source's stable ID;
// aggregate reports use their full declared dimensions and time window.
func NaturalKey(e Entry) string {
	var parts []any
	if e.SourceID != "" {
		parts = []any{e.Scope, e.Kind, e.Provider, e.SourceID}
	} else {
		parts = []any{e.Scope, e.Kind, e.Start.UTC().Format(time.RFC3339Nano), e.End.UTC().Format(time.RFC3339Nano), e.Provider, e.Vendor, e.Model, e.Category, e.Currency, e.Project, e.Dimensions}
	}
	b, _ := json.Marshal(parts)
	return Hash(b)
}

// AggregateDimensionKey identifies the dimensions that may be summed together.
// Time and amount are intentionally excluded so overlapping aggregate windows can
// be detected before they silently double-count the same dimensional series.
func AggregateDimensionKey(e Entry) string {
	b, _ := json.Marshal([]any{e.Scope, e.Kind, e.Provider, e.Vendor, e.Model, e.Category, e.Currency, e.Project, e.Dimensions})
	return Hash(b)
}

func PeriodsOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && aEnd.After(bStart)
}

func ParseCSV(data []byte, provider, scope, timezone, kind, granularity string) (Preview, error) {
	p := Preview{Rows: []Entry{}, Entries: []Entry{}, Errors: []RowError{}, Totals: map[string]string{}, Hash: Hash(data)}
	if len(data) > MaxFileBytes {
		return p, fmt.Errorf("FILE_TOO_LARGE")
	}
	if !utf8.Valid(data) {
		return p, fmt.Errorf("INVALID_ENCODING")
	}
	if scope == "" || len(scope) > 120 || provider == "" || (granularity != "aggregate" && granularity != "event") {
		return p, fmt.Errorf("INVALID_SOURCE_SCOPE")
	}
	if kind != "actual" && kind != "billed" && kind != "calculated" && kind != "estimated" {
		return p, fmt.Errorf("INVALID_COST_KIND")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return p, fmt.Errorf("INVALID_TIMEZONE")
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	headers, err := r.Read()
	if err != nil {
		return p, fmt.Errorf("INVALID_CSV")
	}
	columns := map[string]int{}
	allowed := map[string]bool{"period_start": true, "period_end": true, "model": true, "model_vendor": true, "amount": true, "currency": true, "charge_category": true, "project": true, "source_event_id": true, "coverage": true}
	for i, h := range headers {
		h = strings.TrimSpace(strings.ToLower(h))
		if !allowed[h] {
			return p, fmt.Errorf("FORBIDDEN_OR_UNKNOWN_COLUMN")
		}
		if _, ok := columns[h]; ok {
			return p, fmt.Errorf("DUPLICATE_COLUMN")
		}
		columns[h] = i
	}
	for _, h := range []string{"period_start", "period_end", "amount", "currency", "charge_category", "coverage"} {
		if _, ok := columns[h]; !ok {
			return p, fmt.Errorf("MISSING_COLUMN")
		}
	}
	seen := map[string]bool{}
	aggregateSpans := map[string][]periodSpan{}
	for row := 2; ; row++ {
		fields, e := r.Read()
		if e == io.EOF {
			break
		}
		if row-1 > MaxRows {
			return p, fmt.Errorf("TOO_MANY_ROWS")
		}
		p.Count++
		reject := func(code string) {
			p.Rejected++
			if len(p.Errors) < 1000 {
				p.Errors = append(p.Errors, RowError{row, code})
			}
		}
		if e != nil {
			reject("INVALID_CSV_ROW")
			continue
		}
		get := func(k string) string {
			if i, ok := columns[k]; ok {
				return strings.TrimSpace(fields[i])
			}
			return ""
		}
		parseTime := func(v string) (time.Time, error) {
			t, e := time.Parse(time.RFC3339, v)
			if e == nil {
				return t, nil
			}
			return time.ParseInLocation("2006-01-02", v, loc)
		}
		start, e1 := parseTime(get("period_start"))
		end, e2 := parseTime(get("period_end"))
		if e1 != nil || e2 != nil || !end.After(start) || end.After(start.AddDate(1, 0, 0)) || end.After(time.Now().UTC().AddDate(0, 0, 45)) {
			reject("INVALID_PERIOD")
			continue
		}
		a, e := Amount(get("amount"))
		if e != nil {
			reject("INVALID_AMOUNT")
			continue
		}
		if !ValidCurrency(get("currency")) {
			reject("INVALID_CURRENCY")
			continue
		}
		category := get("charge_category")
		if category != "usage" && category != "fee" && category != "tax" && category != "credit" && category != "refund" {
			reject("INVALID_CHARGE_CATEGORY")
			continue
		}
		if ((category == "credit" || category == "refund") && a.IsPositive()) || ((category == "usage" || category == "fee" || category == "tax") && a.IsNegative()) {
			reject("INVALID_AMOUNT_SIGN")
			continue
		}
		if get("coverage") != "complete" && get("coverage") != "partial" {
			reject("INVALID_COVERAGE")
			continue
		}
		if granularity == "event" && get("source_event_id") == "" {
			reject("MISSING_EVENT_ID")
			continue
		}
		entry := Entry{Start: start.UTC(), End: end.UTC(), Timezone: timezone, Provider: provider, Vendor: get("model_vendor"), Model: get("model"), Amount: a.String(), Currency: get("currency"), Category: category, Kind: kind, Project: get("project"), Scope: scope, SourceRef: fmt.Sprintf("csv:row:%d", row), Coverage: get("coverage")}
		if granularity == "event" {
			entry.SourceID = get("source_event_id")
		}
		entry.Key = NaturalKey(entry)
		if seen[entry.Key] {
			reject("DUPLICATE_NATURAL_KEY")
			continue
		}
		if granularity == "aggregate" {
			dimension := AggregateDimensionKey(entry)
			overlap := false
			for _, span := range aggregateSpans[dimension] {
				if PeriodsOverlap(entry.Start, entry.End, span.Start, span.End) {
					overlap = true
					break
				}
			}
			if overlap {
				reject("OVERLAPPING_AGGREGATE_PERIOD")
				continue
			}
			aggregateSpans[dimension] = append(aggregateSpans[dimension], periodSpan{Start: entry.Start, End: entry.End})
		}
		seen[entry.Key] = true
		p.Entries = append(p.Entries, entry)
		if len(p.Rows) < 20 {
			p.Rows = append(p.Rows, entry)
		}
		total := decimal.Zero
		if old, ok := p.Totals[entry.Currency]; ok {
			total, _ = decimal.NewFromString(old)
		}
		p.Totals[entry.Currency] = total.Add(a).String()
		if p.Start == nil || entry.Start.Before(*p.Start) {
			v := entry.Start
			p.Start = &v
		}
		if p.End == nil || entry.End.After(*p.End) {
			v := entry.End
			p.End = &v
		}
	}
	if p.Count == 0 {
		return p, fmt.Errorf("EMPTY_FILE")
	}
	if p.Start != nil && p.End != nil && p.End.After(p.Start.AddDate(1, 0, 0)) {
		return p, fmt.Errorf("PERIOD_SPAN_TOO_LARGE")
	}
	return p, nil
}
