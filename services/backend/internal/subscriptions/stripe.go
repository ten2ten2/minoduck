package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

// NewStripe configures the SDK once for all billing calls. API models and
// request encoding belong to the SDK; this layer only enforces our boundaries.
func NewStripe(key string, client *http.Client) *stripe.Client {
	h := http.Client{Timeout: 25 * time.Second}
	if client != nil {
		h = *client
	}
	h.Transport = stripeTransport{base: h.Transport}
	backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		HTTPClient:        &h,
		MaxNetworkRetries: stripe.Int64(2),
		LeveledLogger:     &stripe.LeveledLogger{Level: stripe.LevelNull},
	})
	return stripe.NewClient(key, stripe.WithBackends(&stripe.Backends{API: stripeBackend{Backend: backend}}))
}

type stripeBackend struct{ stripe.Backend }

func (b stripeBackend) Call(method, path, key string, params stripe.ParamsContainer, out stripe.LastResponseSetter) error {
	if key == "" {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	p := params.GetParams()
	ctx, cancel := context.WithTimeout(p.Context, 25*time.Second)
	defer cancel()
	p.Context = ctx
	if err := b.Backend.Call(method, path, key, params, out); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if stripeErr, ok := errors.AsType[*stripe.Error](err); ok {
			return fmt.Errorf("STRIPE_REQUEST_FAILED_%d", stripeErr.HTTPStatusCode)
		}
		return errors.New("STRIPE_UNAVAILABLE")
	}
	return nil
}

type stripeTransport struct{ base http.RoundTripper }
type stripeBody struct {
	io.Reader
	io.Closer
}

func (t stripeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(req)
	if err != nil {
		return res, err
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		res.Body.Close()
		return nil, errors.New("STRIPE_REDIRECT_REFUSED")
	}
	res.Body = stripeBody{Reader: io.LimitReader(res.Body, 2*1024*1024), Closer: res.Body}
	return res, nil
}

// ParseStripeEvent uses the SDK's signature, recency and API-version checks.
// Also reject timestamps more than five minutes in the future.
func ParseStripeEvent(body []byte, header, secret string) (stripe.Event, error) {
	for part := range strings.SplitSeq(header, ",") {
		name, value, _ := strings.Cut(part, "=")
		if name == "t" {
			seconds, err := strconv.ParseInt(value, 10, 64)
			if err != nil || time.Unix(seconds, 0).After(time.Now().Add(5*time.Minute)) {
				return stripe.Event{}, stripe.ErrWebhookInvalidHeader
			}
		}
	}
	return stripe.ConstructEvent(body, header, secret)
}
