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
		"LEGAL_RELEASE_APPROVED",
		"LEGAL_ENTITY_NAME",
		"LEGAL_CONTACT_EMAIL",
		"LEGAL_TERMS_EFFECTIVE_DATE",
		"LEGAL_PRIVACY_EFFECTIVE_DATE",
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

func TestLoadRejectsInvalidR2Endpoint(t *testing.T) {
	for _, endpoint := range []string{"http://example.r2.cloudflarestorage.com", "https://example.r2.cloudflarestorage.com/unexpected"} {
		t.Run(endpoint, func(t *testing.T) {
			baseConfigEnv(t)
			t.Setenv("R2_ENDPOINT", endpoint)
			t.Setenv("R2_BUCKET", "bucket")
			t.Setenv("R2_ACCESS_KEY_ID", "access")
			t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
			if _, err := Load(); err == nil {
				t.Fatal("accepted invalid R2 endpoint")
			}
		})
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

func TestProductionLiveStripeRequiresApprovedLegalRelease(t *testing.T) {
	baseConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_URL", "https://app.example.com")
	t.Setenv("BFF_SERVICE_TOKEN", "a-production-token-with-at-least-32-characters")
	t.Setenv("PROVIDER_ENCRYPTION_MASTER_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("R2_BUCKET", "private")
	t.Setenv("R2_ACCESS_KEY_ID", "access")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
	t.Setenv("RESEND_API_KEY", "re_example")
	t.Setenv("MAIL_FROM", "support@example.com")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_example")
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
	if _, err := Load(); err == nil {
		t.Fatal("accepted live Stripe without an approved legal release")
	}
	t.Setenv("LEGAL_RELEASE_APPROVED", "true")
	t.Setenv("LEGAL_ENTITY_NAME", "Example, Inc.")
	t.Setenv("LEGAL_CONTACT_EMAIL", "legal@example.com")
	t.Setenv("LEGAL_TERMS_EFFECTIVE_DATE", "2026-09-11")
	t.Setenv("LEGAL_PRIVACY_EFFECTIVE_DATE", "2026-09-11")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
