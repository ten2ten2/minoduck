package platform

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

type Config struct {
	Env, Port, DatabaseURL, AppURL, BFFToken, MasterKey, KeyID string
	R2Endpoint, R2Bucket, R2AccessKey, R2SecretKey, StorageDir string
	ResendKey, MailFrom, GoogleID, GoogleSecret                string
	StripeKey, StripeWebhookSecret, StripePortalConfig         string
	Prices                                                     map[string]string
}

func validMasterKey(value string) bool {
	if value == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func Load() (Config, error) {
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	c := Config{Env: get("APP_ENV", "development"), Port: get("PORT", "8080"), DatabaseURL: os.Getenv("DATABASE_URL"), AppURL: strings.TrimRight(get("APP_URL", "http://localhost:3001"), "/"), BFFToken: os.Getenv("BFF_SERVICE_TOKEN"), MasterKey: os.Getenv("PROVIDER_ENCRYPTION_MASTER_KEY"), KeyID: get("PROVIDER_KEY_ID", "v1"), R2Endpoint: os.Getenv("R2_ENDPOINT"), R2Bucket: os.Getenv("R2_BUCKET"), R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY"), StorageDir: get("STORAGE_DIR", "./tmp/objects"), ResendKey: os.Getenv("RESEND_API_KEY"), MailFrom: os.Getenv("MAIL_FROM"), GoogleID: os.Getenv("GOOGLE_CLIENT_ID"), GoogleSecret: os.Getenv("GOOGLE_CLIENT_SECRET"), StripeKey: os.Getenv("STRIPE_SECRET_KEY"), StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"), StripePortalConfig: os.Getenv("STRIPE_PORTAL_CONFIGURATION"), Prices: map[string]string{}}
	for _, p := range []string{"starter", "team"} {
		for _, v := range []struct{ k, env string }{{"month", "MONTHLY"}, {"year", "YEARLY"}} {
			c.Prices[p+":"+v.k] = os.Getenv("STRIPE_PRICE_" + strings.ToUpper(p) + "_" + v.env)
		}
	}
	if c.DatabaseURL == "" || c.BFFToken == "" {
		return c, errors.New("DATABASE_URL and BFF_SERVICE_TOKEN are required")
	}
	if c.MasterKey != "" && !validMasterKey(c.MasterKey) {
		return c, errors.New("PROVIDER_ENCRYPTION_MASTER_KEY must be base64-encoded 32 bytes")
	}
	if c.Env == "production" && (!strings.HasPrefix(c.AppURL, "https://") || len(c.BFFToken) < 32 || !validMasterKey(c.MasterKey) || c.R2Endpoint == "" || c.R2Bucket == "" || c.R2AccessKey == "" || c.R2SecretKey == "" || c.ResendKey == "" || c.MailFrom == "") {
		return c, errors.New("production secrets, HTTPS, R2 and email configuration required")
	}
	return c, nil
}
