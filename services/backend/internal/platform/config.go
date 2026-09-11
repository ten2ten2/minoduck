package platform

import (
	"encoding/base64"
	"errors"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env, Port, DatabaseURL, AppURL, BFFToken, MasterKey, KeyID string
	R2Endpoint, R2Bucket, R2AccessKey, R2SecretKey, StorageDir string
	ResendKey, MailFrom, GoogleID, GoogleSecret                string
	StripeKey, StripeWebhookSecret, StripePortalConfig         string
	LegalReleaseApproved, LegalEntityName, LegalContactEmail   string
	LegalTermsEffectiveDate, LegalPrivacyEffectiveDate         string
	Prices                                                     map[string]string
}

func validMasterKey(value string) bool {
	if value == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validAppURL(value string, production bool) bool {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if production {
		return u.Scheme == "https"
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
}

func validR2Endpoint(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}

func complete(values ...string) (hasAny, hasAll bool) {
	hasAll = true
	for _, value := range values {
		if value != "" {
			hasAny = true
		} else {
			hasAll = false
		}
	}
	return hasAny, hasAll
}

func validLegalRelease(c Config) bool {
	if c.LegalReleaseApproved != "true" || strings.TrimSpace(c.LegalEntityName) == "" {
		return false
	}
	address, err := mail.ParseAddress(c.LegalContactEmail)
	if err != nil || address.Address != c.LegalContactEmail {
		return false
	}
	for _, value := range []string{c.LegalTermsEffectiveDate, c.LegalPrivacyEffectiveDate} {
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return false
		}
	}
	return true
}

func (c Config) stripeConfigured() bool {
	if c.StripeKey == "" || c.StripeWebhookSecret == "" || c.StripePortalConfig == "" {
		return false
	}
	for _, price := range c.Prices {
		if price == "" {
			return false
		}
	}
	return len(c.Prices) == 4
}

func (c Config) hasStripeConfig() bool {
	if c.StripeKey != "" || c.StripeWebhookSecret != "" || c.StripePortalConfig != "" {
		return true
	}
	for _, price := range c.Prices {
		if price != "" {
			return true
		}
	}
	return false
}

func Load() (Config, error) {
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	c := Config{Env: get("APP_ENV", "development"), Port: get("PORT", "8080"), DatabaseURL: os.Getenv("DATABASE_URL"), AppURL: strings.TrimRight(get("APP_URL", "http://localhost:3001"), "/"), BFFToken: os.Getenv("BFF_SERVICE_TOKEN"), MasterKey: os.Getenv("PROVIDER_ENCRYPTION_MASTER_KEY"), KeyID: get("PROVIDER_KEY_ID", "v1"), R2Endpoint: os.Getenv("R2_ENDPOINT"), R2Bucket: os.Getenv("R2_BUCKET"), R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY"), StorageDir: get("STORAGE_DIR", "./tmp/objects"), ResendKey: os.Getenv("RESEND_API_KEY"), MailFrom: os.Getenv("MAIL_FROM"), GoogleID: os.Getenv("GOOGLE_CLIENT_ID"), GoogleSecret: os.Getenv("GOOGLE_CLIENT_SECRET"), StripeKey: os.Getenv("STRIPE_SECRET_KEY"), StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"), StripePortalConfig: os.Getenv("STRIPE_PORTAL_CONFIGURATION"), LegalReleaseApproved: os.Getenv("LEGAL_RELEASE_APPROVED"), LegalEntityName: os.Getenv("LEGAL_ENTITY_NAME"), LegalContactEmail: os.Getenv("LEGAL_CONTACT_EMAIL"), LegalTermsEffectiveDate: os.Getenv("LEGAL_TERMS_EFFECTIVE_DATE"), LegalPrivacyEffectiveDate: os.Getenv("LEGAL_PRIVACY_EFFECTIVE_DATE"), Prices: map[string]string{}}
	for _, p := range []string{"starter", "team"} {
		for _, v := range []struct{ k, env string }{{"month", "MONTHLY"}, {"year", "YEARLY"}} {
			c.Prices[p+":"+v.k] = os.Getenv("STRIPE_PRICE_" + strings.ToUpper(p) + "_" + v.env)
		}
	}
	if c.Env != "development" && c.Env != "production" {
		return c, errors.New("APP_ENV must be development or production")
	}
	if c.DatabaseURL == "" || c.BFFToken == "" {
		return c, errors.New("DATABASE_URL and BFF_SERVICE_TOKEN are required")
	}
	if !validAppURL(c.AppURL, c.Env == "production") {
		return c, errors.New("APP_URL must be HTTPS, or localhost HTTP in development")
	}
	if c.MasterKey != "" && !validMasterKey(c.MasterKey) {
		return c, errors.New("PROVIDER_ENCRYPTION_MASTER_KEY must be base64-encoded 32 bytes")
	}
	if has, all := complete(c.GoogleID, c.GoogleSecret); has && !all {
		return c, errors.New("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be configured together")
	}
	if has, all := complete(c.ResendKey, c.MailFrom); has && !all {
		return c, errors.New("RESEND_API_KEY and MAIL_FROM must be configured together")
	}
	if has, all := complete(c.R2Endpoint, c.R2Bucket, c.R2AccessKey, c.R2SecretKey); has && !all {
		return c, errors.New("R2 endpoint, bucket, access key and secret key must be configured together")
	}
	if c.R2Endpoint != "" && !validR2Endpoint(c.R2Endpoint) {
		return c, errors.New("R2_ENDPOINT must be an HTTPS origin without path, query or credentials")
	}
	if c.hasStripeConfig() && !c.stripeConfigured() {
		return c, errors.New("stripe key, webhook secret, portal configuration and all four price IDs must be configured together")
	}
	if c.Env == "production" && (strings.HasPrefix(c.StripeKey, "sk_live_") || strings.HasPrefix(c.StripeKey, "rk_live_")) && !validLegalRelease(c) {
		return c, errors.New("live Stripe requires approved legal entity, contact and Terms/Privacy effective dates")
	}
	if c.Env == "production" && (len(c.BFFToken) < 32 || !validMasterKey(c.MasterKey) || c.R2Endpoint == "" || c.ResendKey == "") {
		return c, errors.New("production secrets, HTTPS, R2 and email configuration required")
	}
	return c, nil
}
