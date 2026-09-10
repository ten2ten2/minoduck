package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"github.com/ten2ten2/minoduck/services/backend/internal/jobs"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"github.com/ten2ten2/minoduck/services/backend/migrations"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMVPIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	if e = migrations.Apply(ctx, admin); e != nil {
		t.Fatal(e)
	}
	m, e := rivermigrate.New(riverpgxv5.New(admin), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Migrate(ctx, rivermigrate.DirectionUp, nil); e != nil {
		t.Fatal(e)
	}
	role := "md_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, `CREATE ROLE `+role+` NOSUPERUSER NOBYPASSRLS`); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `GRANT USAGE ON SCHEMA public TO `+role+`; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO `+role+`; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO `+role); e != nil {
		t.Fatal(e)
	}
	admin.Close()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.MaxConns = 2
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, e := c.Exec(ctx, `SET ROLE `+role+`;SET TIME ZONE 'UTC'`)
		return e
	}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	q, e := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if e != nil {
		t.Fatal(e)
	}
	config := platform.Config{Env: "development", AppURL: "http://localhost:3001", BFFToken: platform.RandomToken(), StorageDir: t.TempDir(), KeyID: "v1"}
	app := Server{DB: pool, Queue: q, Config: config, Objects: platform.Objects{Config: config}}
	server := httptest.NewServer(app.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := ""
	request := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		var data io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			data = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, server.URL+"/api/v1"+path, data)
		req.Header.Set("X-MinoDuck-Service", config.BFFToken)
		req.Header.Set("Origin", config.AppURL)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, res.StatusCode, want, b)
		}
		var result map[string]any
		_ = json.Unmarshal(b, &result)
		return result
	}
	email := "integration-" + uuid.NewString() + "@example.test"
	out := request("POST", "/auth/email/start", map[string]any{"email": email, "locale": "en"}, 202)
	u, e := url.Parse(out["development_link"].(string))
	if e != nil {
		t.Fatal(e)
	}
	request("POST", "/auth/email/verify", map[string]any{"token": u.Query().Get("token")}, 200)
	user := request("GET", "/me", nil, 200)
	uid := user["id"].(string)
	csrf = user["csrf_token"].(string)
	oldCSRF := csrf
	csrf = "wrong"
	request("POST", "/workspaces", map[string]any{"name": "Bad CSRF", "slug": "bad-csrf"}, 403)
	csrf = oldCSRF
	work := request("POST", "/workspaces", map[string]any{"name": "Integration workspace", "slug": "test-" + uuid.NewString()[:12]}, 201)
	wid := work["id"].(string)
	base := "/workspaces/" + wid
	source := request("POST", base+"/connections", map[string]any{"provider": "csv", "name": "Synthetic report", "account_ref": "synthetic-" + uuid.NewString(), "credential": ""}, 201)
	aid := source["id"].(string)
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)
	header := "period_start,period_end,model,amount,currency,charge_category,coverage\n"
	makeCSV := func(amount string) string {
		return header + fmt.Sprintf("%s,%s,unmapped-model,%s,USD,usage,complete\n", start.Format("2006-01-02"), end.Format("2006-01-02"), amount)
	}
	upload := func(content string) string {
		t.Helper()
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		f, _ := mw.CreateFormFile("file", "synthetic.csv")
		_, _ = io.WriteString(f, content)
		for k, v := range map[string]string{"account_id": aid, "source_scope": "test-report", "cost_kind": "actual", "timezone": "UTC", "granularity": "aggregate"} {
			_ = mw.WriteField(k, v)
		}
		_ = mw.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api/v1"+base+"/imports", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-MinoDuck-Service", config.BFFToken)
		req.Header.Set("Origin", config.AppURL)
		req.Header.Set("X-CSRF-Token", csrf)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 201 {
			t.Fatalf("upload %d %s", res.StatusCode, body)
		}
		var v map[string]any
		if e = json.Unmarshal(body, &v); e != nil {
			t.Fatal(e)
		}
		return v["id"].(string)
	}
	for i := 0; i < 3; i++ {
		iid := upload(makeCSV("0.123456789123"))
		request("POST", base+"/imports/"+iid+"/commit", map[string]any{"confirm_corrections": true}, 200)
	}
	params := "?start=" + start.Format("2006-01-02") + "&end=" + end.Format("2006-01-02")
	costs := request("GET", base+"/costs"+params, nil, 200)
	items := costs["items"].([]any)
	if len(items) != 1 {
		t.Fatal("repeated import duplicated costs", costs)
	}
	entry := items[0].(map[string]any)
	if entry["amount"] != "0.123456789123" {
		t.Fatal("precision lost", entry)
	}
	iid := upload(makeCSV("0.2"))
	request("POST", base+"/imports/"+iid+"/commit", map[string]any{"confirm_corrections": true}, 200)
	costs = request("GET", base+"/costs"+params, nil, 200)
	entry = costs["items"].([]any)[0].(map[string]any)
	eid := entry["id"].(string)
	if entry["revision"] != float64(2) {
		t.Fatal("correction not versioned", entry)
	}
	detail := request("GET", base+"/costs/"+eid, nil, 200)
	if len(detail["history"].([]any)) != 2 {
		t.Fatal("correction history missing")
	}
	invoice := request("POST", base+"/invoices", map[string]any{"account_id": aid, "reference": "synthetic-invoice", "currency": "USD", "period_start": start.Format("2006-01-02"), "period_end": end.Format("2006-01-02"), "amount": "0.21", "adjustment": "0", "source_scope": "test-report", "coverage_confirmed": true, "evidence_note": "Synthetic fixture, not a real bill"}, 201)
	run := request("POST", base+"/reconciliation-runs", map[string]any{"invoice_id": invoice["id"]}, 201)
	if run["match_status"] != "within_tolerance" || run["difference"] != "0.01" {
		t.Fatal("wrong reconciliation", run)
	}
	request("GET", base+"/reconciliation-runs/"+run["id"].(string), nil, 402)
	request("POST", base+"/exports", nil, 402)
	// A direct SQL query without tenant context cannot read financial records.
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM cost_entries`).Scan(&count); e != nil || count != 0 {
		t.Fatalf("RLS missing-context failure: count=%d err=%v", count, e)
	}
	other := uuid.NewString()
	tx, e := platform.TenantTx(ctx, pool, other)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM cost_entries WHERE id=$1`, eid).Scan(&count); e != nil || count != 0 {
		t.Fatalf("RLS cross-tenant failure: count=%d err=%v", count, e)
	}
	tx.Rollback(ctx)
	request("GET", "/workspaces/"+other+"/costs", nil, 404)
	// CompleteScope must reject a missing source day even when the user confirms coverage.
	tx, e = platform.TenantTx(ctx, pool, wid)
	if e != nil {
		t.Fatal(e)
	}
	complete, e := ledger.CompleteScope(ctx, tx, wid, aid, "USD", "test-report", start.AddDate(0, 0, -1), end)
	tx.Rollback(ctx)
	if e != nil || complete {
		t.Fatalf("missing day accepted: %v %v", complete, e)
	}
	// Paid entitlements are set only by server-side state. Fixtures simulate confirmed Stripe state.
	_, e = pool.Exec(ctx, `UPDATE subscriptions SET plan_code='starter',status='active',billing_interval='month' WHERE billing_account_id=(SELECT billing_account_id FROM workspaces WHERE id=$1)`, wid)
	if e != nil {
		t.Fatal(e)
	}
	request("GET", base+"/reconciliation-runs/"+run["id"].(string), nil, 200)
	request("PATCH", base+"/reconciliation-items/"+run["id"].(string), map[string]any{"handling_status": "explained", "note": "Synthetic rounding difference"}, 200)
	exported := request("POST", base+"/exports"+params, nil, 202)
	worker := jobs.Worker{DB: pool, Queue: q, Config: config, Objects: app.Objects}
	// A shutdown on the final attempt must leave the export retryable. River
	// refunds that attempt; a restarted worker must still be able to finish it.
	interrupted, cancelJob := context.WithCancel(ctx)
	cancelJob()
	exportJob := &river.Job[tasks.Args]{JobRow: &rivertype.JobRow{Attempt: 3, MaxAttempts: 3}, Args: tasks.Args{Task: "export", WorkspaceID: wid, ResourceID: exported["id"].(string)}}
	if e = worker.Work(interrupted, exportJob); !errors.Is(e, context.Canceled) {
		t.Fatalf("interrupted worker: %v", e)
	}
	tx, e = platform.TenantTx(ctx, pool, wid)
	if e != nil {
		t.Fatal(e)
	}
	var exportState string
	e = tx.QueryRow(ctx, `SELECT state FROM exports WHERE workspace_id=$1 AND id=$2`, wid, exported["id"].(string)).Scan(&exportState)
	tx.Rollback(ctx)
	if e != nil || exportState != "pending" {
		t.Fatalf("shutdown made export non-retryable: state=%q error=%v", exportState, e)
	}
	if e = worker.Work(ctx, exportJob); e != nil {
		t.Fatal("export worker:", e)
	}
	request("GET", base+"/exports/"+exported["id"].(string)+"/download", nil, 200)
	request("GET", "/workspaces/"+other+"/exports/"+exported["id"].(string)+"/download", nil, 404)
	request("GET", base+"/subscription", nil, 200)
	request("POST", base+"/alert-rules", map[string]any{"name": "Test budget", "kind": "budget", "currency": "USD", "amount": "10", "enabled": true}, 201)
	// Viewer role may read costs but cannot import, change sources, or administer billing.
	_, e = pool.Exec(ctx, `UPDATE workspace_members SET role='viewer' WHERE workspace_id=$1 AND user_id=$2`, wid, uid)
	if e != nil {
		t.Fatal(e)
	}
	request("GET", base+"/costs"+params, nil, 200)
	request("POST", base+"/connections", map[string]any{}, 403)
	request("POST", base+"/subscription/checkout", map[string]any{"plan": "team", "billing_interval": "year"}, 403)
	_, e = pool.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, wid, uid)
	if e != nil {
		t.Fatal(e)
	}
	request("DELETE", base+"/connections/"+aid, nil, 200)
	request("POST", base+"/connections/"+aid+"/sync", nil, 400)
	request("GET", base+"/costs"+params, nil, 200)
	if files, e := filepath.Glob(filepath.Join(config.StorageDir, wid, "*.csv")); e != nil || len(files) < 3 {
		t.Fatal("source evidence not retained")
	}
	testStripeReplay(t, pool, &app, server.URL, wid)
	request("POST", "/auth/logout", nil, 200)
	request("GET", "/me", nil, 401)
}

// The transport returns synthetic Stripe snapshots. No payment is attempted.
type billingTransport func(*http.Request) (*http.Response, error)

func (f billingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testStripeReplay(t *testing.T, pool *pgxpool.Pool, app *Server, baseURL, wid string) {
	t.Helper()
	ctx := context.Background()
	var account string
	if e := pool.QueryRow(ctx, `SELECT billing_account_id FROM workspaces WHERE id=$1`, wid).Scan(&account); e != nil {
		t.Fatal(e)
	}
	customer := "cus_fixture_" + uuid.NewString()
	sid := "sub_fixture_" + uuid.NewString()
	if _, e := pool.Exec(ctx, `UPDATE subscriptions SET provider_customer_id=$1,provider_subscription_id=$2 WHERE billing_account_id=$3`, customer, sid, account); e != nil {
		t.Fatal(e)
	}
	app.Config.StripeKey = "fixture-only"
	app.Config.StripeWebhookSecret = "fixture-signing-secret"
	app.Config.Prices = map[string]string{"starter:month": "price_fixture_starter"}
	now := time.Now()
	var mu sync.Mutex
	liveStatus := "active"
	calls := 0
	app.StripeHTTP = &http.Client{Transport: billingTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		var data any
		if strings.Contains(r.URL.Path, "/invoices/") {
			data = map[string]any{"created": now.Add(-48 * time.Hour).Unix()}
		} else {
			data = map[string]any{"id": sid, "customer": customer, "status": liveStatus, "metadata": map[string]string{"billing_account_id": account}, "items": map[string]any{"data": []any{map[string]any{"id": "si_fixture", "current_period_start": now.Add(-24 * time.Hour).Unix(), "current_period_end": now.Add(29 * 24 * time.Hour).Unix(), "price": map[string]any{"id": "price_fixture_starter", "recurring": map[string]string{"interval": "month"}}}}}, "latest_invoice": "in_fixture"}
		}
		b, _ := json.Marshal(data)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(b)), Request: r}, nil
	})}
	send := func(id, kind string, want int) {
		t.Helper()
		object := map[string]string{"id": sid, "customer": customer, "subscription": sid}
		b, _ := json.Marshal(map[string]any{"id": id, "type": kind, "data": map[string]any{"object": object}})
		stamp := fmt.Sprint(time.Now().Unix())
		mac := hmac.New(sha256.New, []byte(app.Config.StripeWebhookSecret))
		mac.Write([]byte(stamp + "."))
		mac.Write(b)
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/webhooks/stripe", bytes.NewReader(b))
		req.Header.Set("Stripe-Signature", "t="+stamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("webhook %s: %d %s", kind, res.StatusCode, body)
		}
	}
	eventID := "evt_fixture_" + uuid.NewString()
	send(eventID, "invoice.payment_failed", 200)
	var plan, status string
	var grace *time.Time
	read := func() {
		t.Helper()
		if e := pool.QueryRow(ctx, `SELECT plan_code,status,grace_period_until FROM subscriptions WHERE billing_account_id=$1`, account).Scan(&plan, &status, &grace); e != nil {
			t.Fatal(e)
		}
	}
	read()
	if plan != "starter" || status != "active" {
		t.Fatal("delayed failure event overrode current paid state")
	}
	mu.Lock()
	before := calls
	mu.Unlock()
	send(eventID, "invoice.payment_failed", 200)
	mu.Lock()
	after := calls
	liveStatus = "past_due"
	mu.Unlock()
	if before != after {
		t.Fatal("duplicate webhook repeated fulfillment")
	}
	send("evt_"+uuid.NewString(), "customer.subscription.updated", 200)
	read()
	if grace == nil || grace.Sub(now.Add(5*24*time.Hour)).Abs() > time.Second {
		t.Fatal("grace period is based on arrival instead of invoice")
	}
	firstGrace := *grace
	send("evt_"+uuid.NewString(), "invoice.payment_failed", 200)
	read()
	if grace == nil || !grace.Equal(firstGrace) {
		t.Fatal("repeated failures extended grace")
	}
	mu.Lock()
	liveStatus = "canceled"
	mu.Unlock()
	send("evt_"+uuid.NewString(), "customer.subscription.deleted", 200)
	read()
	if plan != "free" {
		t.Fatal("canceled subscription retained entitlement")
	}
}
