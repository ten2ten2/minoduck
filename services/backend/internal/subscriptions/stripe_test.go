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
	"strconv"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

func TestWebhookSignature(t *testing.T) {
	now := time.Now()
	body := []byte(`{"id":"evt_1","object":"event","api_version":"` + stripe.APIVersion + `","type":"customer.subscription.updated","data":{"object":{"id":"sub_fixture"}}}`)
	sign := func(at time.Time, payload []byte) string {
		stamp := strconv.FormatInt(at.Unix(), 10)
		h := hmac.New(sha256.New, []byte("secret"))
		h.Write([]byte(stamp + "."))
		h.Write(payload)
		return "t=" + stamp + ",v1=bad,v1=" + hex.EncodeToString(h.Sum(nil))
	}
	header := sign(now, body)
	if _, e := ParseStripeEvent(body, header, "secret"); e != nil {
		t.Fatal(e)
	}
	if _, e := ParseStripeEvent(body, sign(now.Add(-6*time.Minute), body), "secret"); e == nil {
		t.Fatal("stale signature")
	}
	if _, e := ParseStripeEvent([]byte("tampered"), header, "secret"); e == nil {
		t.Fatal("tampered payload")
	}
	if _, e := ParseStripeEvent(body, sign(now.Add(6*time.Minute), body), "secret"); e == nil {
		t.Fatal("future signature")
	}
	if _, e := ParseStripeEvent(body, header, ""); e == nil {
		t.Fatal("empty signing secret")
	}
	unsupported := []byte(strings.ReplaceAll(string(body), stripe.APIVersion, "unsupported"))
	if _, e := ParseStripeEvent(unsupported, sign(now, unsupported), "secret"); e == nil {
		t.Fatal("unsupported API version")
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
	client := NewStripe("sk_test_fixture", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if r.URL.String() != "https://api.stripe.com/v1/checkout/sessions" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Stripe-Version") != stripe.APIVersion || r.Header.Get("Idempotency-Key") != "checkout-fixture" {
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
	})})
	out, err := client.V1CheckoutSessions.Create(context.Background(), &stripe.CheckoutSessionCreateParams{
		IdempotencyKey: stripe.String("checkout-fixture"), Mode: stripe.String("subscription"),
	})
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
			client := NewStripe("sk_test_fixture", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/subscriptions/sub_fixture" {
					t.Fatalf("unexpected typed request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(payload))), Request: r}, nil
			})})
			live, err := client.V1Subscriptions.Retrieve(context.Background(), "sub_fixture", nil)
			if err != nil || live.Customer == nil || live.Customer.ID != "cus_fixture" || live.Schedule == nil || live.Schedule.ID != "sub_sched_fixture" || live.LatestInvoice == nil || live.LatestInvoice.ID != "in_fixture" || !live.CancelAtPeriodEnd {
				t.Fatalf("expandable subscription: %+v, %v", live, err)
			}
			if live.Items == nil || len(live.Items.Data) != 1 || live.Items.Data[0].CurrentPeriodStart != 1700000000 || live.Items.Data[0].CurrentPeriodEnd != 1702592000 || live.Items.Data[0].Price.Recurring.Interval != "month" {
				t.Fatalf("billing period/interval lost: %+v", live.Items)
			}
		})
	}
}

func TestSDKDoesNotRetryValidationErrorsOrExposeProviderDetails(t *testing.T) {
	attempts := 0
	client := NewStripe("sk_test_fixture", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 400, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"customer@example.test private details"}}`))}, nil
	})})
	_, err := client.V1Customers.Create(context.Background(), &stripe.CustomerCreateParams{IdempotencyKey: stripe.String("customer-fixture")})
	if err == nil || err.Error() != "STRIPE_REQUEST_FAILED_400" || attempts != 1 {
		t.Fatalf("validation error handling: %v, attempts=%d", err, attempts)
	}
}

func TestSDKRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewStripe("sk_test_fixture", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		return nil, r.Context().Err()
	})})
	_, err := client.V1Customers.Create(ctx, &stripe.CustomerCreateParams{IdempotencyKey: stripe.String("customer-fixture")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was swallowed: %v", err)
	}
}
