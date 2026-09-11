package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/shopspring/decimal"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Failure struct {
	Code       string
	RetryAfter time.Duration
	Permanent  bool
}

func (e Failure) Error() string { return e.Code }

type Scalar string

func (s *Scalar) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return fmt.Errorf("SOURCE_SCHEMA_CHANGED")
	}
	var raw string
	if len(b) > 0 && b[0] == '"' {
		if e := json.Unmarshal(b, &raw); e != nil {
			return e
		}
	} else {
		raw = string(b)
	}
	value, e := decimal.NewFromString(raw)
	if e != nil {
		return e
	}
	normalized := value.String()
	if _, e = ledger.Amount(normalized); e != nil {
		return e
	}
	*s = Scalar(normalized)
	return nil
}

type Usage struct {
	Key        string            `json:"key"`
	Start      time.Time         `json:"start"`
	End        time.Time         `json:"end"`
	Model      string            `json:"model"`
	Metrics    map[string]Scalar `json:"metrics"`
	Dimensions map[string]string `json:"dimensions"`
}
type Snapshot struct {
	Entries []ledger.Entry `json:"entries"`
	Usage   []Usage        `json:"usage"`
	Start   time.Time      `json:"start"`
	End     time.Time      `json:"end"`
	Version string         `json:"connector_version"`
}
type Client struct{ HTTP *http.Client }

func (c Client) get(ctx context.Context, provider, key, path string, query url.Values, out any) error {
	hosts := map[string]string{"openai": "https://api.openai.com", "anthropic": "https://api.anthropic.com", "openrouter": "https://openrouter.ai"}
	host, ok := hosts[provider]
	if !ok {
		return Failure{Code: "UNSUPPORTED_PROVIDER", Permanent: true}
	}
	req, e := http.NewRequestWithContext(ctx, "GET", host+path+"?"+query.Encode(), nil)
	if e != nil {
		return e
	}
	if provider == "anthropic" {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		if path == "/v1/organizations/usage_report/messages" {
			req.Header.Set("anthropic-beta", "fast-mode-2026-02-01")
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, e := h.Do(req)
	if e != nil {
		return Failure{Code: "PROVIDER_UNAVAILABLE"}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		fail := Failure{Code: "PROVIDER_UNAVAILABLE"}
		switch res.StatusCode {
		case 401:
			fail.Code = "PROVIDER_INVALID_CREDENTIAL"
			fail.Permanent = true
		case 403:
			fail.Code = "PROVIDER_PERMISSION_DENIED"
			fail.Permanent = true
		case 429:
			fail.Code = "PROVIDER_RATE_LIMITED"
			if secs, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil {
				fail.RetryAfter = time.Duration(secs) * time.Second
			} else if at, e := http.ParseTime(res.Header.Get("Retry-After")); e == nil {
				fail.RetryAfter = time.Until(at)
			}
		default:
			if res.StatusCode < 500 {
				fail.Code = "SOURCE_SCHEMA_CHANGED"
				fail.Permanent = true
			}
		}
		return fail
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, 20*1024*1024+1))
	if e != nil {
		return e
	}
	if len(data) > 20*1024*1024 {
		return Failure{Code: "SOURCE_TOO_LARGE", Permanent: true}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if e = d.Decode(out); e != nil {
		return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
	}
	return nil
}
func (c Client) Fetch(ctx context.Context, provider, key string, start, end time.Time) (Snapshot, error) {
	s := Snapshot{Entries: []ledger.Entry{}, Usage: []Usage{}, Start: start, End: end, Version: "2026-09-11.1"}
	if !end.After(start) || end.After(time.Now().UTC().Truncate(24*time.Hour)) {
		return s, Failure{Code: "INVALID_SYNC_WINDOW", Permanent: true}
	}
	var e error
	switch provider {
	case "openai":
		e = c.openAI(ctx, key, &s)
	case "anthropic":
		e = c.anthropic(ctx, key, &s)
	case "openrouter":
		e = c.openRouter(ctx, key, &s)
	default:
		e = Failure{Code: "UNSUPPORTED_PROVIDER", Permanent: true}
	}
	if e != nil {
		return Snapshot{}, e
	}
	seen := map[string]bool{}
	for i := range s.Entries {
		v := &s.Entries[i]
		if _, e = ledger.Amount(v.Amount); e != nil {
			return Snapshot{}, Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		if !ledger.ValidCurrency(v.Currency) || v.Start.Before(start) || v.End.After(end) || !v.End.After(v.Start) {
			return Snapshot{}, Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		v.Key = ledger.NaturalKey(*v)
		if seen[v.Key] {
			return Snapshot{}, Failure{Code: "DUPLICATE_SOURCE_KEY", Permanent: true}
		}
		seen[v.Key] = true
	}
	usageSeen := map[string]bool{}
	for i := range s.Usage {
		u := &s.Usage[i]
		if u.Start.Before(start) || u.End.After(end) || !u.End.After(u.Start) || len(u.Metrics) == 0 {
			return Snapshot{}, Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		hasMetric := false
		for _, raw := range u.Metrics {
			if raw == "" {
				continue
			}
			value, parseErr := decimal.NewFromString(string(raw))
			if parseErr != nil || value.IsNegative() {
				return Snapshot{}, Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
			}
			hasMetric = true
		}
		if !hasMetric {
			return Snapshot{}, Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		setUsageKey(u)
		if usageSeen[u.Key] {
			return Snapshot{}, Failure{Code: "DUPLICATE_SOURCE_KEY", Permanent: true}
		}
		usageSeen[u.Key] = true
	}
	return s, nil
}
func base(provider, model, amount, currency string, start, end time.Time, d map[string]string) ledger.Entry {
	return ledger.Entry{Start: start, End: end, Timezone: "UTC", Provider: provider, Model: model, Amount: amount, Currency: strings.ToUpper(currency), Kind: "actual", Category: "usage", Scope: "native-cost", Coverage: "complete", Dimensions: d}
}
func (c Client) openAI(ctx context.Context, key string, s *Snapshot) error {
	q := url.Values{"start_time": {strconv.FormatInt(s.Start.Unix(), 10)}, "end_time": {strconv.FormatInt(s.End.Unix(), 10)}, "bucket_width": {"1d"}, "limit": {"31"}, "group_by": {"line_item", "project_id"}}
	seen := map[string]bool{}
	for {
		var page struct {
			Data []struct {
				Start   int64 `json:"start_time"`
				End     int64 `json:"end_time"`
				Results []struct {
					Amount struct {
						Value    *Scalar `json:"value"`
						Currency string  `json:"currency"`
					} `json:"amount"`
					Line    *string `json:"line_item"`
					Project *string `json:"project_id"`
				} `json:"results"`
			} `json:"data"`
			More bool    `json:"has_more"`
			Next *string `json:"next_page"`
		}
		if e := c.get(ctx, "openai", key, "/v1/organization/costs", q, &page); e != nil {
			return e
		}
		if page.Data == nil {
			return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		for _, b := range page.Data {
			for _, r := range b.Results {
				if r.Amount.Value == nil {
					return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
				}
				d := map[string]string{"line_item": nullable(r.Line), "project_id": nullable(r.Project)}
				v := base("openai", "", string(*r.Amount.Value), r.Amount.Currency, time.Unix(b.Start, 0).UTC(), time.Unix(b.End, 0).UTC(), d)
				v.Project = nullable(r.Project)
				s.Entries = append(s.Entries, v)
			}
		}
		if !page.More {
			break
		}
		if page.Next == nil || *page.Next == "" || seen[*page.Next] {
			return Failure{Code: "SOURCE_PAGINATION_INCOMPLETE", Permanent: true}
		}
		seen[*page.Next] = true
		q.Set("page", *page.Next)
	}
	q.Del("page")
	q["group_by"] = []string{"model", "project_id", "service_tier"}
	seen = map[string]bool{}
	for {
		var page struct {
			Data []struct {
				Start   int64 `json:"start_time"`
				End     int64 `json:"end_time"`
				Results []struct {
					Model    *string `json:"model"`
					Project  *string `json:"project_id"`
					Tier     *string `json:"service_tier"`
					Input    Scalar  `json:"input_tokens"`
					Cached   Scalar  `json:"input_cached_tokens"`
					Output   Scalar  `json:"output_tokens"`
					Requests Scalar  `json:"num_model_requests"`
				} `json:"results"`
			} `json:"data"`
			More bool    `json:"has_more"`
			Next *string `json:"next_page"`
		}
		if e := c.get(ctx, "openai", key, "/v1/organization/usage/completions", q, &page); e != nil {
			return e
		}
		if page.Data == nil {
			return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		for _, b := range page.Data {
			for _, r := range b.Results {
				d := map[string]string{"project_id": nullable(r.Project), "service_tier": nullable(r.Tier)}
				u := Usage{Start: time.Unix(b.Start, 0).UTC(), End: time.Unix(b.End, 0).UTC(), Model: nullable(r.Model), Dimensions: d, Metrics: map[string]Scalar{"input_total": r.Input, "input_cached_subset": r.Cached, "output": r.Output, "requests": r.Requests}}
				setUsageKey(&u)
				s.Usage = append(s.Usage, u)
			}
		}
		if !page.More {
			break
		}
		if page.Next == nil || *page.Next == "" || seen[*page.Next] {
			return Failure{Code: "SOURCE_PAGINATION_INCOMPLETE", Permanent: true}
		}
		seen[*page.Next] = true
		q.Set("page", *page.Next)
	}
	return nil
}
func nullable(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func setUsageKey(u *Usage) {
	b, _ := json.Marshal([]any{u.Start, u.End, u.Model, u.Dimensions})
	u.Key = ledger.Hash(b)
}
func (c Client) anthropic(ctx context.Context, key string, s *Snapshot) error {
	q := url.Values{"starting_at": {s.Start.Format(time.RFC3339)}, "ending_at": {s.End.Format(time.RFC3339)}, "bucket_width": {"1d"}, "limit": {"31"}, "group_by[]": {"workspace_id", "description"}}
	seen := map[string]bool{}
	for {
		var page struct {
			Data []struct {
				Start   time.Time `json:"starting_at"`
				End     time.Time `json:"ending_at"`
				Results []struct {
					Amount        *Scalar `json:"amount"`
					Currency      string  `json:"currency"`
					Description   string  `json:"description"`
					Model         *string `json:"model"`
					Workspace     *string `json:"workspace_id"`
					Geo           string  `json:"inference_geo"`
					Tier          string  `json:"service_tier"`
					ContextWindow string  `json:"context_window"`
					CostType      string  `json:"cost_type"`
					TokenType     string  `json:"token_type"`
				} `json:"results"`
			} `json:"data"`
			More bool    `json:"has_more"`
			Next *string `json:"next_page"`
		}
		if e := c.get(ctx, "anthropic", key, "/v1/organizations/cost_report", q, &page); e != nil {
			return e
		}
		if page.Data == nil {
			return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		for _, b := range page.Data {
			for _, r := range b.Results {
				if r.Amount == nil {
					return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
				}
				amount, e := ledger.Cents(string(*r.Amount))
				if e != nil {
					return e
				}
				d := map[string]string{
					"description": r.Description, "workspace_id": nullable(r.Workspace),
					"inference_geo": r.Geo, "service_tier": r.Tier, "context_window": r.ContextWindow,
					"cost_type": r.CostType, "token_type": r.TokenType, "known_exclusion": "priority_tier",
				}
				v := base("anthropic", nullable(r.Model), amount, r.Currency, b.Start, b.End, d)
				v.Vendor = "anthropic"
				v.Project = nullable(r.Workspace)
				s.Entries = append(s.Entries, v)
			}
		}
		if !page.More {
			break
		}
		if page.Next == nil || *page.Next == "" || seen[*page.Next] {
			return Failure{Code: "SOURCE_PAGINATION_INCOMPLETE", Permanent: true}
		}
		seen[*page.Next] = true
		q.Set("page", *page.Next)
	}
	q.Del("page")
	q["group_by[]"] = []string{"model", "workspace_id", "service_tier", "inference_geo", "speed"}
	seen = map[string]bool{}
	for {
		var page struct {
			Data []struct {
				Start   time.Time `json:"starting_at"`
				End     time.Time `json:"ending_at"`
				Results []struct {
					Model     string  `json:"model"`
					Workspace *string `json:"workspace_id"`
					Tier      string  `json:"service_tier"`
					Geo       string  `json:"inference_geo"`
					Speed     string  `json:"speed"`
					Input     Scalar  `json:"uncached_input_tokens"`
					Read      Scalar  `json:"cache_read_input_tokens"`
					Output    Scalar  `json:"output_tokens"`
					Creation  struct {
						Five Scalar `json:"ephemeral_5m_input_tokens"`
						Hour Scalar `json:"ephemeral_1h_input_tokens"`
					} `json:"cache_creation"`
				} `json:"results"`
			} `json:"data"`
			More bool    `json:"has_more"`
			Next *string `json:"next_page"`
		}
		if e := c.get(ctx, "anthropic", key, "/v1/organizations/usage_report/messages", q, &page); e != nil {
			return e
		}
		if page.Data == nil {
			return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		for _, b := range page.Data {
			for _, r := range b.Results {
				d := map[string]string{"workspace_id": nullable(r.Workspace), "service_tier": r.Tier, "region": r.Geo, "inference_geo": r.Geo, "speed": r.Speed}
				u := Usage{Start: b.Start, End: b.End, Model: r.Model, Dimensions: d, Metrics: map[string]Scalar{"input_uncached": r.Input, "cache_read": r.Read, "cache_write_5m": r.Creation.Five, "cache_write_1h": r.Creation.Hour, "output": r.Output}}
				setUsageKey(&u)
				s.Usage = append(s.Usage, u)
			}
		}
		if !page.More {
			break
		}
		if page.Next == nil || *page.Next == "" || seen[*page.Next] {
			return Failure{Code: "SOURCE_PAGINATION_INCOMPLETE", Permanent: true}
		}
		seen[*page.Next] = true
		q.Set("page", *page.Next)
	}
	return nil
}
func (c Client) openRouter(ctx context.Context, key string, s *Snapshot) error {
	if s.Start.Before(time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -30)) {
		return Failure{Code: "PROVIDER_HISTORY_LIMIT", Permanent: true}
	}
	for day := s.Start; day.Before(s.End); day = day.AddDate(0, 0, 1) {
		var page struct {
			Data []struct {
				Date     string  `json:"date"`
				Endpoint string  `json:"endpoint_id"`
				Model    string  `json:"model"`
				Version  string  `json:"model_permaslug"`
				Provider string  `json:"provider_name"`
				Usage    *Scalar `json:"usage"`
				BYOK     Scalar  `json:"byok_usage_inference"`
				Input    Scalar  `json:"prompt_tokens"`
				Output   Scalar  `json:"completion_tokens"`
				Requests Scalar  `json:"requests"`
			} `json:"data"`
		}
		if e := c.get(ctx, "openrouter", key, "/api/v1/activity", url.Values{"date": {day.Format("2006-01-02")}}, &page); e != nil {
			return e
		}
		if page.Data == nil {
			return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		for _, r := range page.Data {
			date, e := time.Parse("2006-01-02", r.Date)
			if e != nil || !date.Equal(day) || r.Usage == nil {
				return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
			}
			d := map[string]string{"endpoint_id": r.Endpoint, "serving_provider": r.Provider, "model_version": r.Version}
			v := base("openrouter", r.Model, string(*r.Usage), "USD", date, date.AddDate(0, 0, 1), d)
			v.Vendor = strings.SplitN(r.Model, "/", 2)[0]
			s.Entries = append(s.Entries, v)
			u := Usage{Start: date, End: date.AddDate(0, 0, 1), Model: r.Model, Dimensions: d, Metrics: map[string]Scalar{"input_total": r.Input, "output": r.Output, "requests": r.Requests, "byok_mirror_estimate_excluded": r.BYOK}}
			setUsageKey(&u)
			s.Usage = append(s.Usage, u)
		}
	}
	return nil
}
