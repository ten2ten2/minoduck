package subscriptions

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
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
}
