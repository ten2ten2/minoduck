package subscriptions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const APIVersion = "2025-06-30.basil"

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
		Data []struct {
			ID    string `json:"id"`
			Start int64  `json:"current_period_start"`
			End   int64  `json:"current_period_end"`
			Price struct {
				ID        string `json:"id"`
				Recurring struct {
					Interval string `json:"interval"`
				} `json:"recurring"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
	Schedule      *string         `json:"schedule"`
	LatestInvoice json.RawMessage `json:"latest_invoice"`
}

func (s Stripe) Request(ctx context.Context, method, path, key string, form url.Values, out any) error {
	if s.Key == "" {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	req, e := http.NewRequestWithContext(ctx, method, "https://api.stripe.com/v1/"+path, strings.NewReader(form.Encode()))
	if e != nil {
		return e
	}
	req.SetBasicAuth(s.Key, "")
	req.Header.Set("Stripe-Version", APIVersion)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	h := s.HTTP
	if h == nil {
		h = &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, e := h.Do(req)
	if e != nil {
		return errors.New("STRIPE_UNAVAILABLE")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("STRIPE_REQUEST_FAILED_%d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2*1024*1024)).Decode(out)
}
func (s Stripe) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	var v Subscription
	e := s.Request(ctx, "GET", "subscriptions/"+url.PathEscape(id), "", nil, &v)
	return v, e
}
func VerifySignature(body []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return errors.New("STRIPE_NOT_CONFIGURED")
	}
	timestamp := ""
	signatures := []string{}
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "t" {
			timestamp = kv[1]
		}
		if kv[0] == "v1" {
			signatures = append(signatures, kv[1])
		}
	}
	secs, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || now.Sub(time.Unix(secs, 0)).Abs() > 5*time.Minute {
		return errors.New("INVALID_STRIPE_SIGNATURE")
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(timestamp + "."))
	h.Write(body)
	expected := h.Sum(nil)
	for _, sig := range signatures {
		got, e := hex.DecodeString(sig)
		if e == nil && hmac.Equal(expected, got) {
			return nil
		}
	}
	return errors.New("INVALID_STRIPE_SIGNATURE")
}
