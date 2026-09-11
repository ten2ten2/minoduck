package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
)

func TestScheduleDurationFollowsDestinationInterval(t *testing.T) {
	for _, interval := range []string{"month", "year"} {
		t.Run(interval, func(t *testing.T) {
			current := &stripe.SubscriptionItem{ID: "si_fixture", CurrentPeriodStart: 1700000000, CurrentPeriodEnd: 1702592000, Price: &stripe.Price{ID: "price_current"}}
			called := false
			client := subscriptions.NewStripe("sk_test_fixture", &http.Client{Transport: billingTransport(func(r *http.Request) (*http.Response, error) {
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
				if r.Header.Get("Idempotency-Key") != "change-fixture" {
					t.Fatal("missing idempotency key")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"id":"sub_sched_fixture"}`))}, nil
			})})
			if _, err := client.V1SubscriptionSchedules.Update(context.Background(), "sub_sched_fixture", scheduleParams(current, "price_destination", interval, "change-fixture")); err != nil || !called {
				t.Fatalf("schedule request: %v, called=%v", err, called)
			}
		})
	}
}

func TestSubscriptionChangeMode(t *testing.T) {
	tests := []struct {
		name                                       string
		fromPlan, fromInterval, toPlan, toInterval string
		hasSchedule                                bool
		want                                       string
	}{
		{"same without schedule", "team", "month", "team", "month", false, "unchanged"},
		{"same cancels schedule", "team", "month", "team", "month", true, "cancel_scheduled"},
		{"downgrade is deferred", "team", "month", "starter", "month", false, "deferred"},
		{"existing deferred target can be replaced", "team", "month", "starter", "month", true, "deferred"},
		{"upgrade is immediate", "starter", "month", "team", "month", false, "immediate"},
		{"upgrade releases old schedule", "starter", "month", "team", "month", true, "release_then_immediate"},
		{"annual to monthly is deferred", "starter", "year", "starter", "month", true, "deferred"},
		{"monthly to annual replaces old schedule immediately", "starter", "month", "starter", "year", true, "release_then_immediate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subscriptionChangeMode(tt.fromPlan, tt.fromInterval, tt.toPlan, tt.toInterval, tt.hasSchedule)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestStripeEventSubject(t *testing.T) {
	tests := []struct {
		name         string
		kind         stripe.EventType
		object       string
		customer     string
		subscription string
	}{
		{"checkout", stripe.EventTypeCheckoutSessionCompleted, `{"customer":"cus_1","subscription":"sub_1"}`, "cus_1", "sub_1"},
		{"subscription", stripe.EventTypeCustomerSubscriptionUpdated, `{"id":"sub_1","customer":"cus_1"}`, "cus_1", "sub_1"},
		{"invoice", stripe.EventTypeInvoicePaid, `{"customer":"cus_1","parent":{"type":"subscription_details","subscription_details":{"subscription":"sub_1"}}}`, "cus_1", "sub_1"},
		{"expanded invoice", stripe.EventTypeInvoicePaymentFailed, `{"customer":{"id":"cus_1"},"parent":{"type":"subscription_details","subscription_details":{"subscription":{"id":"sub_1"}}}}`, "cus_1", "sub_1"},
		{"one-off invoice", stripe.EventTypeInvoicePaid, `{"customer":"cus_1","parent":null}`, "cus_1", ""},
		{"unhandled event", stripe.EventTypeCustomerCreated, `{"id":"cus_1"}`, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customer, subscription, err := stripeEventSubject(stripe.Event{Type: tt.kind, Data: &stripe.EventData{Raw: json.RawMessage(tt.object)}})
			if err != nil || customer != tt.customer || subscription != tt.subscription {
				t.Fatalf("subject: customer=%q subscription=%q error=%v", customer, subscription, err)
			}
		})
	}
}
