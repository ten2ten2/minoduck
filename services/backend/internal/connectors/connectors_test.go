package connectors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(t *testing.T, handler func(*http.Request) (int, string, string)) Client {
	t.Helper()
	return Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		code, body, retry := handler(r)
		h := http.Header{}
		h.Set("Retry-After", retry)
		return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
}
func TestNativeCostPrecisionAndBYOK(t *testing.T) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)
	t.Run("anthropic cents and cache classes", func(t *testing.T) {
		c := fixtureClient(t, func(r *http.Request) (int, string, string) {
			if r.Host != "api.anthropic.com" || r.Header.Get("x-api-key") != "synthetic-key" {
				t.Fatal("wrong destination or authentication")
			}
			if strings.Contains(r.URL.Path, "cost_report") {
				return 200, fmt.Sprintf(`{"data":[{"starting_at":%q,"ending_at":%q,"results":[{"amount":"12.3456789123","currency":"USD","description":"text","workspace_id":null}]}],"has_more":false}`, start.Format(time.RFC3339), end.Format(time.RFC3339)), ""
			}
			return 200, fmt.Sprintf(`{"data":[{"starting_at":%q,"ending_at":%q,"results":[{"model":"synthetic-model","uncached_input_tokens":100,"cache_read_input_tokens":200,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":40},"output_tokens":50}]}],"has_more":false}`, start.Format(time.RFC3339), end.Format(time.RFC3339)), ""
		})
		s, e := c.Fetch(context.Background(), "anthropic", "synthetic-key", start, end)
		if e != nil {
			t.Fatal(e)
		}
		if len(s.Entries) != 1 || s.Entries[0].Amount != "0.123456789123" {
			t.Fatalf("cent conversion lost precision: %+v", s)
		}
		if s.Usage[0].Metrics["cache_write_1h"] != "40" || s.Usage[0].Metrics["input_uncached"] != "100" {
			t.Fatal("cache classes lost")
		}
	})
	t.Run("openrouter excludes BYOK mirror", func(t *testing.T) {
		c := fixtureClient(t, func(r *http.Request) (int, string, string) {
			if r.URL.Query().Get("date") != start.Format("2006-01-02") {
				t.Fatal("wrong UTC day")
			}
			return 200, fmt.Sprintf(`{"data":[{"date":%q,"model":"synthetic/model","model_permaslug":"synthetic-v1","endpoint_id":"endpoint-1","provider_name":"synthetic","usage":0.123456789123,"byok_usage_inference":99,"prompt_tokens":100,"completion_tokens":10,"requests":1}]}`, start.Format("2006-01-02")), ""
		})
		s, e := c.Fetch(context.Background(), "openrouter", "synthetic-key", start, end)
		if e != nil {
			t.Fatal(e)
		}
		if s.Entries[0].Amount != "0.123456789123" || s.Usage[0].Metrics["byok_mirror_estimate_excluded"] != "99" {
			t.Fatal("BYOK mirror was added to actual spend", s)
		}
	})
}
func TestProviderFailuresNeverPublishPartialSnapshot(t *testing.T) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)
	for _, tc := range []struct {
		status    int
		code      string
		permanent bool
	}{{401, "PROVIDER_INVALID_CREDENTIAL", true}, {403, "PROVIDER_PERMISSION_DENIED", true}, {429, "PROVIDER_RATE_LIMITED", false}, {503, "PROVIDER_UNAVAILABLE", false}} {
		t.Run(tc.code, func(t *testing.T) {
			c := fixtureClient(t, func(*http.Request) (int, string, string) { return tc.status, `{"sensitive":"never exposed"}`, "120" })
			s, e := c.Fetch(context.Background(), "openai", "synthetic-key", start, end)
			var f Failure
			if !errors.As(e, &f) || f.Code != tc.code || f.Permanent != tc.permanent || len(s.Entries) != 0 {
				t.Fatalf("wrong failure: %+v %+v", s, e)
			}
			if tc.status == 429 && f.RetryAfter != 2*time.Minute {
				t.Fatal("Retry-After ignored")
			}
		})
	}
	c := fixtureClient(t, func(r *http.Request) (int, string, string) {
		return 200, fmt.Sprintf(`{"data":[{"start_time":%d,"end_time":%d,"results":[{"amount":{"value":0.1,"currency":"USD"}}]}],"has_more":true,"next_page":null}`, start.Unix(), end.Unix()), ""
	})
	s, e := c.Fetch(context.Background(), "openai", "synthetic-key", start, end)
	if e == nil || len(s.Entries) != 0 {
		t.Fatal("partial page treated as complete")
	}
}
func TestOpenAISeparatesUsageAndCorrectionIdentity(t *testing.T) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)
	amount := "0.1"
	c := fixtureClient(t, func(r *http.Request) (int, string, string) {
		if strings.Contains(r.URL.Path, "costs") {
			return 200, fmt.Sprintf(`{"data":[{"start_time":%d,"end_time":%d,"results":[{"amount":{"value":%s,"currency":"usd"},"line_item":"text","project_id":"synthetic"}]}],"has_more":false}`, start.Unix(), end.Unix(), amount), ""
		}
		return 200, fmt.Sprintf(`{"data":[{"start_time":%d,"end_time":%d,"results":[{"model":"synthetic-model","project_id":"synthetic","input_tokens":100,"input_cached_tokens":20,"output_tokens":10,"num_model_requests":1}]}],"has_more":false}`, start.Unix(), end.Unix()), ""
	})
	a, e := c.Fetch(context.Background(), "openai", "synthetic-key", start, end)
	if e != nil {
		t.Fatal(e)
	}
	amount = "0.2"
	b, e := c.Fetch(context.Background(), "openai", "synthetic-key", start, end)
	if e != nil {
		t.Fatal(e)
	}
	if len(a.Entries) != 1 || len(a.Usage) != 1 || a.Entries[0].Model != "" || a.Entries[0].Key != b.Entries[0].Key {
		t.Fatal("cost identity includes amount or fabricated model")
	}
}
