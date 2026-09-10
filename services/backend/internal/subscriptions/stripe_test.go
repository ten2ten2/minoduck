package subscriptions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWebhookSignature(t *testing.T) {
	now := time.Unix(1700000000, 0)
	body := []byte(`{"id":"evt_1"}`)
	stamp := strconv.FormatInt(now.Unix(), 10)
	h := hmac.New(sha256.New, []byte("secret"))
	h.Write([]byte(stamp + "."))
	h.Write(body)
	header := "t=" + stamp + ",v1=bad,v1=" + hex.EncodeToString(h.Sum(nil))
	if e := VerifySignature(body, header, "secret", now); e != nil {
		t.Fatal(e)
	}
	if VerifySignature(body, header, "secret", now.Add(6*time.Minute)) == nil {
		t.Fatal("stale signature")
	}
	if VerifySignature([]byte("tampered"), header, "secret", now) == nil {
		t.Fatal("tampered payload")
	}
	if VerifySignature(body, header, "secret", now.Add(-6*time.Minute)) == nil {
		t.Fatal("future signature")
	}
	if VerifySignature(body, header, "", now) == nil {
		t.Fatal("empty signing secret")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestSDKRetriesKeepIdempotencyAndReleaseResponses(t *testing.T) {
	attempts := 0
	var bodies []*trackedBody
	client := Stripe{Key: "sk_test_fixture", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if r.URL.String() != "https://api.stripe.com/v1/checkout/sessions" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Stripe-Version") != APIVersion || r.Header.Get("Idempotency-Key") != "checkout-fixture" {
			t.Fatal("lost API version or stable idempotency key")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "mode=subscription" {
			t.Fatalf("retry changed request body: %q %v", body, err)
		}
		status, payload := 200, `{"id":"cs_fixture","url":"https://checkout.stripe.com/c/pay/fixture"}`
		if attempts == 1 {
			status, payload = 429, `{"error":{"type":"invalid_request_error","code":"lock_timeout","message":"synthetic transient lock"}}`
		}
		response := &trackedBody{Reader: strings.NewReader(payload)}
		bodies = append(bodies, response)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: response, Request: r}, nil
	})}}
	var out struct{ ID string }
	err := client.Request(context.Background(), http.MethodPost, "checkout/sessions", "checkout-fixture", url.Values{"mode": {"subscription"}}, &out)
	if err != nil || out.ID != "cs_fixture" || attempts != 2 {
		t.Fatalf("retry result: id=%q attempts=%d error=%v", out.ID, attempts, err)
	}
	for _, body := range bodies {
		if !body.closed {
			t.Fatal("SDK response body was not closed")
		}
	}
}

func TestSDKSubscriptionExpandableFields(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		t.Run(strconv.FormatBool(expanded), func(t *testing.T) {
			field := func(id string) any {
				if expanded {
					return map[string]any{"id": id}
				}
				return id
			}
			payload, _ := json.Marshal(map[string]any{
				"id": "sub_fixture", "status": "active", "customer": field("cus_fixture"),
				"schedule": field("sub_sched_fixture"), "latest_invoice": field("in_fixture"),
				"cancel_at_period_end": true, "metadata": map[string]string{"billing_account_id": "account_fixture"},
				"items": map[string]any{"data": []any{map[string]any{
					"id": "si_fixture", "current_period_start": 1700000000, "current_period_end": 1702592000,
					"price": map[string]any{"id": "price_fixture", "recurring": map[string]string{"interval": "month"}},
				}}},
			})
			client := Stripe{Key: "sk_test_fixture", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/subscriptions/sub_fixture" {
					t.Fatalf("unexpected typed request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(payload))), Request: r}, nil
			})}}
			live, err := client.GetSubscription(context.Background(), "sub_fixture")
			if err != nil || live.Customer != "cus_fixture" || live.Schedule == nil || *live.Schedule != "sub_sched_fixture" || string(live.LatestInvoice) != `"in_fixture"` || !live.Cancel {
				t.Fatalf("expandable subscription: %+v, %v", live, err)
			}
			if len(live.Items.Data) != 1 || live.Items.Data[0].Start != 1700000000 || live.Items.Data[0].End != 1702592000 || live.Items.Data[0].Price.Recurring.Interval != "month" {
				t.Fatalf("billing period/interval lost: %+v", live.Items)
			}
		})
	}
}

func TestSDKDoesNotRetryValidationErrorsOrExposeProviderDetails(t *testing.T) {
	attempts := 0
	client := Stripe{Key: "sk_test_fixture", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 400, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"customer@example.test private details"}}`))}, nil
	})}}
	err := client.Request(context.Background(), http.MethodPost, "customers", "customer-fixture", nil, &map[string]any{})
	if err == nil || err.Error() != "STRIPE_REQUEST_FAILED_400" || attempts != 1 {
		t.Fatalf("validation error handling: %v, attempts=%d", err, attempts)
	}
}

func TestSDKRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := Stripe{Key: "sk_test_fixture", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		return nil, r.Context().Err()
	})}}
	err := client.Request(ctx, http.MethodPost, "customers", "customer-fixture", nil, &map[string]any{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was swallowed: %v", err)
	}
}

func TestScheduleDurationFollowsDestinationInterval(t *testing.T) {
	for _, interval := range []string{"month", "year"} {
		t.Run(interval, func(t *testing.T) {
			current := SubscriptionItem{ID: "si_fixture", Start: 1700000000, End: 1702592000}
			current.Price.ID = "price_current"
			called := false
			client := Stripe{Key: "sk_test_fixture", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Method != http.MethodPost || r.URL.Path != "/v1/subscription_schedules/sub_sched_fixture" {
					t.Fatalf("unexpected schedule request: %s %s", r.Method, r.URL)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				want := map[string]string{
					"phases[0][start_date]": "1700000000", "phases[0][end_date]": "1702592000",
					"phases[0][items][0][price]": "price_current", "phases[1][items][0][price]": "price_destination",
					"phases[1][duration][interval]": interval, "phases[1][duration][interval_count]": "1",
					"phases[0][proration_behavior]": "none", "phases[1][proration_behavior]": "none",
					"proration_behavior": "none", "end_behavior": "release",
				}
				for key, value := range want {
					if r.Form.Get(key) != value {
						t.Errorf("%s: got %q want %q", key, r.Form.Get(key), value)
					}
				}
				if r.Form.Has("phases[1][iterations]") || r.Header.Get("Idempotency-Key") != "change-fixture" {
					t.Fatal("deprecated iterations or missing idempotency key")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"id":"sub_sched_fixture"}`))}, nil
			})}}
			if err := client.SetSchedulePhases(context.Background(), "sub_sched_fixture", "change-fixture", current, "price_destination", interval); err != nil || !called {
				t.Fatalf("schedule request: %v, called=%v", err, called)
			}
		})
	}
}
