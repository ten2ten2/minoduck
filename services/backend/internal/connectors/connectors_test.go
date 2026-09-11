package connectors

import (
	"context"
	"encoding/json"
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

func TestProviderScalarNormalizesScientificNotation(t *testing.T) {
	var value Scalar
	if err := json.Unmarshal([]byte(`1e-7`), &value); err != nil || value != "0.0000001" {
		t.Fatalf("scientific provider decimal not normalized: %q %v", value, err)
	}
	if err := json.Unmarshal([]byte(`1e20`), &value); err == nil {
		t.Fatal("provider amount outside ledger precision was accepted")
	}
}

func TestNativeCostPrecisionAndBYOK(t *testing.T) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)
	t.Run("anthropic cents and pricing dimensions", func(t *testing.T) {
		c := fixtureClient(t, func(r *http.Request) (int, string, string) {
			if r.Host != "api.anthropic.com" || r.Header.Get("x-api-key") != "synthetic-key" {
				t.Fatal("wrong destination or authentication")
			}
			if strings.Contains(r.URL.Path, "cost_report") {
				return 200, fmt.Sprintf(`{"data":[{"starting_at":%q,"ending_at":%q,"results":[{"amount":"12.3456789123","currency":"USD","description":"text","workspace_id":null,"inference_geo":"us","service_tier":"standard","cost_type":"tokens","token_type":"uncached_input_tokens"}]}],"has_more":false}`, start.Format(time.RFC3339), end.Format(time.RFC3339)), ""
			}
			if r.Header.Get("anthropic-beta") != "fast-mode-2026-02-01" {
				t.Fatal("fast-mode dimension requested without beta header")
			}
			groups := strings.Join(r.URL.Query()["group_by[]"], ",")
			if !strings.Contains(groups, "inference_geo") || !strings.Contains(groups, "speed") {
				t.Fatal("pricing dimensions missing from usage grouping", groups)
			}
			return 200, fmt.Sprintf(`{"data":[{"starting_at":%q,"ending_at":%q,"results":[{"model":"synthetic-model","workspace_id":null,"service_tier":"standard","inference_geo":"us","speed":"standard","uncached_input_tokens":100,"cache_read_input_tokens":200,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":40},"output_tokens":50}]}],"has_more":false}`, start.Format(time.RFC3339), end.Format(time.RFC3339)), ""
		})
		s, e := c.Fetch(context.Background(), "anthropic", "synthetic-key", "organization-1", start, end)
		if e != nil {
			t.Fatal(e)
		}
		if len(s.Entries) != 1 || s.Entries[0].Amount != "0.123456789123" || s.Entries[0].Dimensions["inference_geo"] != "us" {
			t.Fatalf("Anthropic cost dimensions or precision lost: %+v", s)
		}
		if s.Usage[0].Metrics["cache_write_1h"] != "40" || s.Usage[0].Metrics["input_uncached"] != "100" || s.Usage[0].Dimensions["region"] != "us" || s.Usage[0].Dimensions["speed"] != "standard" {
			t.Fatal("Anthropic usage pricing dimensions lost", s.Usage[0])
		}
	})
	t.Run("openrouter excludes BYOK mirror", func(t *testing.T) {
		c := fixtureClient(t, func(r *http.Request) (int, string, string) {
			if r.URL.Query().Get("date") != start.Format("2006-01-02") {
				t.Fatal("wrong UTC day")
			}
			if r.URL.Query().Get("workspace_id") != "workspace-1" {
				t.Fatal("OpenRouter activity was not scoped to the verified workspace")
			}
			return 200, fmt.Sprintf(`{"data":[{"date":%q,"model":"synthetic/model","model_permaslug":"synthetic-v1","endpoint_id":"endpoint-1","provider_name":"synthetic","usage":0.123456789123,"byok_usage_inference":99,"prompt_tokens":100,"completion_tokens":10,"requests":1}]}`, start.Format("2006-01-02")), ""
		})
		s, e := c.Fetch(context.Background(), "openrouter", "synthetic-key", "workspace-1", start, end)
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
	}{{401, "PROVIDER_INVALID_CREDENTIAL", true}, {403, "PROVIDER_PERMISSION_DENIED", true}, {408, "PROVIDER_UNAVAILABLE", false}, {409, "PROVIDER_UNAVAILABLE", false}, {425, "PROVIDER_UNAVAILABLE", false}, {429, "PROVIDER_RATE_LIMITED", false}, {503, "PROVIDER_UNAVAILABLE", false}} {
		t.Run(tc.code, func(t *testing.T) {
			c := fixtureClient(t, func(*http.Request) (int, string, string) { return tc.status, `{"sensitive":"never exposed"}`, "120" })
			s, e := c.Fetch(context.Background(), "openai", "synthetic-key", "", start, end)
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
	s, e := c.Fetch(context.Background(), "openai", "synthetic-key", "", start, end)
	if e == nil || len(s.Entries) != 0 {
		t.Fatal("partial page treated as complete")
	}
}
func TestProviderIdentityValidation(t *testing.T) {
	t.Run("anthropic organization", func(t *testing.T) {
		client := fixtureClient(t, func(r *http.Request) (int, string, string) {
			if r.URL.Path != "/v1/organizations/me" || r.Header.Get("x-api-key") != "synthetic-key" {
				t.Fatalf("unexpected identity request: %s", r.URL)
			}
			return 200, `{"id":"org_verified","name":"Verified org"}`, ""
		})
		identity, err := client.Identity(context.Background(), "anthropic", "synthetic-key", "label-only")
		if err != nil || !identity.Verified || identity.ID != "org_verified" || identity.Scope != "organization" {
			t.Fatalf("identity=%+v err=%v", identity, err)
		}
	})
	t.Run("openrouter workspace slug", func(t *testing.T) {
		client := fixtureClient(t, func(r *http.Request) (int, string, string) {
			if r.URL.Path != "/api/v1/workspaces" || r.URL.Query().Get("limit") != "100" {
				t.Fatalf("unexpected identity request: %s", r.URL)
			}
			return 200, `{"data":[{"id":"ws_verified","name":"Verified workspace","slug":"finance"}],"total_count":1}`, ""
		})
		identity, err := client.Identity(context.Background(), "openrouter", "synthetic-key", "finance")
		if err != nil || !identity.Verified || identity.ID != "ws_verified" || identity.Scope != "workspace" {
			t.Fatalf("identity=%+v err=%v", identity, err)
		}
	})
	t.Run("openrouter offset pagination", func(t *testing.T) {
		calls := 0
		client := fixtureClient(t, func(r *http.Request) (int, string, string) {
			calls++
			if calls == 1 && r.URL.Query().Get("offset") == "0" {
				return 200, `{"data":[{"id":"ws_first","slug":"first"}],"total_count":2}`, ""
			}
			if calls == 2 && r.URL.Query().Get("offset") == "1" {
				return 200, `{"data":[{"id":"ws_second","slug":"second"}],"total_count":2}`, ""
			}
			t.Fatalf("unexpected page request: %s", r.URL)
			return 500, `{}`, ""
		})
		identity, err := client.Identity(context.Background(), "openrouter", "synthetic-key", "second")
		if err != nil || identity.ID != "ws_second" || calls != 2 {
			t.Fatalf("identity=%+v calls=%d err=%v", identity, calls, err)
		}
	})
	t.Run("trailing response is rejected", func(t *testing.T) {
		client := fixtureClient(t, func(*http.Request) (int, string, string) {
			return 200, `{"id":"org_verified"}{"unexpected":true}`, ""
		})
		_, err := client.Identity(context.Background(), "anthropic", "synthetic-key", "")
		var failure Failure
		if !errors.As(err, &failure) || failure.Code != "SOURCE_SCHEMA_CHANGED" || !failure.Permanent {
			t.Fatalf("trailing response accepted: %v", err)
		}
	})
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
	a, e := c.Fetch(context.Background(), "openai", "synthetic-key", "", start, end)
	if e != nil {
		t.Fatal(e)
	}
	amount = "0.2"
	b, e := c.Fetch(context.Background(), "openai", "synthetic-key", "", start, end)
	if e != nil {
		t.Fatal(e)
	}
	if len(a.Entries) != 1 || len(a.Usage) != 1 || a.Entries[0].Model != "" || a.Entries[0].Key != b.Entries[0].Key {
		t.Fatal("cost identity includes amount or fabricated model")
	}
}
func TestUsageValidationRejectsCorruptSnapshots(t *testing.T) {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)
	cost := fmt.Sprintf(`{"data":[{"start_time":%d,"end_time":%d,"results":[{"amount":{"value":0.1,"currency":"USD"},"line_item":"text","project_id":"synthetic"}]}],"has_more":false}`, start.Unix(), end.Unix())
	usageResult := func(bucketStart, bucketEnd int64, results string) string {
		return fmt.Sprintf(`{"data":[{"start_time":%d,"end_time":%d,"results":[%s]}],"has_more":false}`, bucketStart, bucketEnd, results)
	}
	valid := `{"model":"synthetic-model","project_id":"synthetic","service_tier":"default","input_tokens":100,"input_cached_tokens":20,"output_tokens":10,"num_model_requests":1}`
	for _, tc := range []struct {
		name, body, code string
	}{
		{"outside requested window", usageResult(start.AddDate(0, 0, -1).Unix(), start.Unix(), valid), "SOURCE_SCHEMA_CHANGED"},
		{"duplicate usage identity", usageResult(start.Unix(), end.Unix(), valid+","+valid), "DUPLICATE_SOURCE_KEY"},
		{"negative usage metric", usageResult(start.Unix(), end.Unix(), `{"model":"synthetic-model","project_id":"synthetic","service_tier":"default","input_tokens":-1,"input_cached_tokens":0,"output_tokens":10,"num_model_requests":1}`), "SOURCE_SCHEMA_CHANGED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(r *http.Request) (int, string, string) {
				if strings.Contains(r.URL.Path, "costs") {
					return 200, cost, ""
				}
				return 200, tc.body, ""
			})
			snapshot, err := c.Fetch(context.Background(), "openai", "synthetic-key", "", start, end)
			var failure Failure
			if !errors.As(err, &failure) || failure.Code != tc.code || !failure.Permanent || len(snapshot.Entries) != 0 || len(snapshot.Usage) != 0 {
				t.Fatalf("corrupt usage was not rejected atomically: snapshot=%+v error=%+v", snapshot, err)
			}
		})
	}
}
