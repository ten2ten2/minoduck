package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func SendMail(ctx context.Context, c Config, to, subject, body, key string) error {
	if c.ResendKey == "" {
		return fmt.Errorf("EMAIL_NOT_CONFIGURED")
	}
	data, _ := json.Marshal(map[string]any{"from": c.MailFrom, "to": []string{to}, "subject": subject, "text": body})
	req, e := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.ResendKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, e := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if e != nil {
		return fmt.Errorf("EMAIL_DELIVERY_FAILED")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("EMAIL_DELIVERY_FAILED")
	}
	return nil
}
