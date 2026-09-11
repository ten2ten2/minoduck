package platform

import "testing"

func baseConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("BFF_SERVICE_TOKEN", "local-test-token")
	for _, key := range []string{
		"PROVIDER_ENCRYPTION_MASTER_KEY",
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

func TestLoadRejectsPartialExternalConfiguration(t *testing.T) {
	t.Run("google", func(t *testing.T) {
		baseConfigEnv(t)
		t.Setenv("GOOGLE_CLIENT_ID", "client")
		if _, err := Load(); err == nil {
			t.Fatal("accepted partial Google configuration")
		}
	})
	t.Run("stripe", func(t *testing.T) {
		baseConfigEnv(t)
		t.Setenv("STRIPE_SECRET_KEY", "sk_test_example")
		if _, err := Load(); err == nil {
			t.Fatal("accepted partial Stripe configuration")
		}
	})
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
