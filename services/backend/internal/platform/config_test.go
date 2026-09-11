package platform

import "testing"

func baseConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_URL", "http://localhost:3001")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("BFF_SERVICE_TOKEN", "local-test-token")
	for _, key := range []string{
		"PROVIDER_ENCRYPTION_MASTER_KEY",
		"R2_ENDPOINT",
		"R2_BUCKET",
		"R2_ACCESS_KEY_ID",
		"R2_SECRET_ACCESS_KEY",
		"RESEND_API_KEY",
		"MAIL_FROM",
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"STRIPE_SECRET_KEY",
		"STRIPE_WEBHOOK_SECRET",
		"STRIPE_PORTAL_CONFIGURATION",
		"STRIPE_PRICE_STARTER_MONTHLY",
		"STRIPE_PRICE_STARTER_YEARLY",
		"STRIPE_PRICE_TEAM_MONTHLY",
		"STRIPE_PRICE_TEAM_YEARLY",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadRejectsUnknownEnvironment(t *testing.T) {
	baseConfigEnv(t)
	t.Setenv("APP_ENV", "prod")
	if _, err := Load(); err == nil {
		t.Fatal("accepted unknown APP_ENV")
	}
}

func TestLoadRejectsPartialExternalConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"google", "GOOGLE_CLIENT_ID", "client"},
		{"stripe", "STRIPE_SECRET_KEY", "sk_test_example"},
		{"r2", "R2_ENDPOINT", "https://example.r2.cloudflarestorage.com"},
		{"email", "RESEND_API_KEY", "re_example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseConfigEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatalf("accepted partial %s configuration", tc.name)
			}
		})
	}
}

func TestLoadRejectsUnsafeAppURL(t *testing.T) {
	baseConfigEnv(t)
	t.Setenv("APP_URL", "http://example.com")
	if _, err := Load(); err == nil {
		t.Fatal("accepted remote plaintext APP_URL")
	}
}

func TestLoadAcceptsCompleteStripeConfiguration(t *testing.T) {
	baseConfigEnv(t)
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_example")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_example")
	t.Setenv("STRIPE_PORTAL_CONFIGURATION", "bpc_example")
	for _, key := range []string{
		"STRIPE_PRICE_STARTER_MONTHLY",
		"STRIPE_PRICE_STARTER_YEARLY",
		"STRIPE_PRICE_TEAM_MONTHLY",
		"STRIPE_PRICE_TEAM_YEARLY",
	} {
		t.Setenv(key, "price_example")
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
