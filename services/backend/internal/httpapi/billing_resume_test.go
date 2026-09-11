package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
)

func TestClearPendingCancellation(t *testing.T) {
	called := false
	client := subscriptions.NewStripe("sk_test_fixture", &http.Client{Transport: billingTransport(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Method != http.MethodPost || r.URL.Path != "/v1/subscriptions/sub_fixture" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("cancel_at_period_end"); got != "false" {
			t.Fatalf("cancel_at_period_end=%q", got)
		}
		if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "resume-") {
			t.Fatal("missing resume idempotency key")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"id":"sub_fixture","status":"active"}`))}, nil
	})})
	if err := clearPendingCancellation(context.Background(), client, "sub_fixture"); err != nil || !called {
		t.Fatalf("clear pending cancellation: %v, called=%v", err, called)
	}
}
