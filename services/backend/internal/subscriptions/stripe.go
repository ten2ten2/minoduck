package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

const APIVersion = stripe.APIVersion

type Stripe struct {
	Key  string
	HTTP *http.Client
}
type Subscription struct {
	ID       string            `json:"id"`
	Customer string            `json:"customer"`
	Status   string            `json:"status"`
	Cancel   bool              `json:"cancel_at_period_end"`
	Metadata map[string]string `json:"metadata"`
	Items    struct {
		Data []SubscriptionItem `json:"data"`
	} `json:"items"`
	Schedule      *string         `json:"schedule"`
	LatestInvoice json.RawMessage `json:"latest_invoice"`
}

type SubscriptionItem struct {
	ID    string `json:"id"`
	Start int64  `json:"current_period_start"`
	End   int64  `json:"current_period_end"`
	Price struct {
		ID        string `json:"id"`
		Recurring struct {
			Interval string `json:"interval"`
		} `json:"recurring"`
	} `json:"price"`
}

// Configure an isolated SDK backend: no global API key or default SDK logging
// (Stripe error messages can contain customer details).
func (s Stripe) backend() stripe.Backend {
	h := http.Client{Timeout: 25 * time.Second}
	if s.HTTP != nil {
		h = *s.HTTP
	}
	h.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	h.Transport = limitedTransport{base: h.Transport}
	return stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		HTTPClient:        &h,
		MaxNetworkRetries: stripe.Int64(2),
		LeveledLogger:     &stripe.LeveledLogger{Level: stripe.LevelNull},
	})
}

type limitedTransport struct{ base http.RoundTripper }
type limitedBody struct {
	io.Reader
	closer io.Closer
	once   sync.Once
	err    error
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err != nil {
		_ = b.Close()
	}
	return n, err
}

func (b *limitedBody) Close() error {
	b.once.Do(func() { b.err = b.closer.Close() })
	return b.err
}

func (t limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(req)
	if err == nil {
		res.Body = &limitedBody{Reader: io.LimitReader(res.Body, 2*1024*1024), closer: res.Body}
	}
	return res, err
}

func requestError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) {
		return fmt.Errorf("STRIPE_REQUEST_FAILED_%d", stripeErr.HTTPStatusCode)
	}
	return errors.New("STRIPE_UNAVAILABLE")
}

// Request keeps the application's form contract while delegating authentication,
// API versioning, retry classification and idempotent retries to the SDK.
func (s Stripe) Request(ctx context.Context, method, path, key string, form url.Values, out any) error {
	if s.Key == "" {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	params := &stripe.RawParams{Params: stripe.Params{Context: ctx}}
	if key != "" {
		params.SetIdempotencyKey(key)
	}
	backend, ok := s.backend().(stripe.RawRequestBackend)
	if !ok {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	res, err := backend.RawRequest(method, "/v1/"+path, s.Key, form.Encode(), params)
	if err != nil {
		return requestError(ctx, err)
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("STRIPE_REQUEST_FAILED_%d", res.StatusCode)
	}
	if err = json.Unmarshal(res.RawJSON, out); err != nil {
		return errors.New("STRIPE_INVALID_RESPONSE")
	}
	return nil
}
func (s Stripe) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	var v Subscription
	if s.Key == "" {
		return v, errors.New("STRIPE_NOT_CONFIGURED")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client := stripe.NewClient(s.Key, stripe.WithBackends(&stripe.Backends{API: s.backend()}))
	live, err := client.V1Subscriptions.Retrieve(ctx, id, nil)
	if err != nil {
		return v, requestError(ctx, err)
	}
	if live.LastResponse.StatusCode >= 300 {
		return v, fmt.Errorf("STRIPE_REQUEST_FAILED_%d", live.LastResponse.StatusCode)
	}
	v.ID, v.Status, v.Cancel, v.Metadata = live.ID, string(live.Status), live.CancelAtPeriodEnd, live.Metadata
	// SDK expandable fields accept either an ID or an expanded object.
	if live.Customer != nil {
		v.Customer = live.Customer.ID
	}
	if live.Schedule != nil {
		v.Schedule = &live.Schedule.ID
	}
	if live.LatestInvoice != nil {
		v.LatestInvoice, _ = json.Marshal(live.LatestInvoice.ID)
	}
	if live.Items != nil {
		for _, item := range live.Items.Data {
			if item == nil || item.Price == nil || item.Price.Recurring == nil {
				return Subscription{}, errors.New("STRIPE_INVALID_RESPONSE")
			}
			entry := SubscriptionItem{ID: item.ID, Start: item.CurrentPeriodStart, End: item.CurrentPeriodEnd}
			entry.Price.ID = item.Price.ID
			entry.Price.Recurring.Interval = string(item.Price.Recurring.Interval)
			v.Items.Data = append(v.Items.Data, entry)
		}
	}
	return v, nil
}

// SetSchedulePhases uses the typed current API. Clover removed `iterations`;
// phase duration must explicitly follow the destination price's billing period.
func (s Stripe) SetSchedulePhases(ctx context.Context, id, key string, current SubscriptionItem, price, interval string) error {
	if s.Key == "" {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client := stripe.NewClient(s.Key, stripe.WithBackends(&stripe.Backends{API: s.backend()}))
	params := &stripe.SubscriptionScheduleUpdateParams{
		EndBehavior:       stripe.String("release"),
		ProrationBehavior: stripe.String("none"),
		Phases: []*stripe.SubscriptionScheduleUpdatePhaseParams{
			{
				StartDate: stripe.Int64(current.Start), EndDate: stripe.Int64(current.End),
				Items: []*stripe.SubscriptionScheduleUpdatePhaseItemParams{
					{Price: stripe.String(current.Price.ID), Quantity: stripe.Int64(1)},
				},
				ProrationBehavior: stripe.String("none"),
			},
			{
				Duration: &stripe.SubscriptionScheduleUpdatePhaseDurationParams{
					Interval: stripe.String(interval), IntervalCount: stripe.Int64(1),
				},
				Items: []*stripe.SubscriptionScheduleUpdatePhaseItemParams{
					{Price: stripe.String(price), Quantity: stripe.Int64(1)},
				},
				ProrationBehavior: stripe.String("none"),
			},
		},
	}
	params.SetIdempotencyKey(key)
	live, err := client.V1SubscriptionSchedules.Update(ctx, id, params)
	if err != nil {
		return requestError(ctx, err)
	}
	if live.LastResponse.StatusCode >= 300 {
		return fmt.Errorf("STRIPE_REQUEST_FAILED_%d", live.LastResponse.StatusCode)
	}
	return nil
}

func VerifySignature(body []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	timestamp := ""
	parts := strings.Split(header, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		kv := strings.SplitN(parts[i], "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "t" {
			timestamp = kv[1]
		}
	}
	secs, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || now.Sub(time.Unix(secs, 0)).Abs() > 5*time.Minute {
		return errors.New("INVALID_STRIPE_SIGNATURE")
	}
	// The symmetric five-minute window above uses our injectable clock and also
	// rejects future timestamps. Only skip the SDK's separate wall-clock check;
	// signature verification and secret rotation remain handled by the SDK.
	if e = stripe.ValidatePayload(body, strings.Join(parts, ","), secret, stripe.WithIgnoreTolerance()); e != nil {
		return errors.New("INVALID_STRIPE_SIGNATURE")
	}
	return nil
}
