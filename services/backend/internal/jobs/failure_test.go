package jobs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"github.com/ten2ten2/minoduck/services/backend/migrations"
)

func TestSyncFailureStateWorksWithRuntimeRLS(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err = migrations.Apply(ctx, admin); err != nil {
		t.Fatal(err)
	}
	role := "md_jobs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, `CREATE ROLE `+role+` NOSUPERUSER NOBYPASSRLS; GRANT USAGE ON SCHEMA public TO `+role+`; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO `+role+`; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO `+role); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 2
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SET ROLE `+role+`; SET TIME ZONE 'UTC'`)
		return err
	}
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	uid, billingID, wid, aid := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	email := "jobs-" + uuid.NewString() + "@example.test"
	if _, err = admin.Exec(ctx, `INSERT INTO users(id,email) VALUES($1,$2); INSERT INTO billing_accounts(id,owner_id) VALUES($3,$1); INSERT INTO workspaces(id,billing_account_id,slug,name) VALUES($4,$3,$5,'Jobs test'); INSERT INTO provider_accounts(id,workspace_id,provider,name,external_account_ref,status,credential_cipher,credential_key_id,generation,next_sync_at) VALUES($6,$4,'openai','Fixture','fixture','ready',$7,'old-key',2,now()+interval '1 hour')`, uid, email, billingID, wid, "jobs-"+uuid.NewString()[:12], aid, []byte{1}); err != nil {
		t.Fatal(err)
	}
	worker := Worker{DB: db, Config: platform.Config{KeyID: "new-key"}}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)

	staleRun := uuid.NewString()
	if _, err = admin.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,account_id,generation,period_start,period_end) VALUES($1,$2,$3,1,$4,$5)`, staleRun, wid, aid, start, end); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM sync_runs WHERE id=$1`, staleRun).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("runtime role unexpectedly bypassed RLS: count=%d err=%v", visible, err)
	}
	if err = worker.failSyncRun(ctx, wid, staleRun, "SYNC_FAILED", false); err != nil {
		t.Fatal(err)
	}
	var runState, connectionState string
	if err = admin.QueryRow(ctx, `SELECT state FROM sync_runs WHERE id=$1`, staleRun).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT status FROM provider_accounts WHERE id=$1`, aid).Scan(&connectionState); err != nil {
		t.Fatal(err)
	}
	if runState != "failed" || connectionState != "ready" {
		t.Fatalf("stale generation changed connection: run=%s connection=%s", runState, connectionState)
	}

	currentRun := uuid.NewString()
	if _, err = admin.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,account_id,generation,period_start,period_end) VALUES($1,$2,$3,2,$4,$5)`, currentRun, wid, aid, start, end); err != nil {
		t.Fatal(err)
	}
	job := &river.Job[tasks.Args]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3}, Args: tasks.Args{Task: "sync", WorkspaceID: wid, ResourceID: currentRun}}
	if err = worker.sync(ctx, job); err == nil {
		t.Fatal("key-version mismatch unexpectedly succeeded")
	}
	var code string
	var next *time.Time
	if err = admin.QueryRow(ctx, `SELECT state,error_code FROM sync_runs WHERE id=$1`, currentRun).Scan(&runState, &code); err != nil {
		t.Fatal(err)
	}
	if runState != "failed" || code != "ENCRYPTION_KEY_VERSION_UNAVAILABLE" {
		t.Fatalf("run terminal state: state=%s code=%s", runState, code)
	}
	if err = admin.QueryRow(ctx, `SELECT status,error_code,next_sync_at FROM provider_accounts WHERE id=$1`, aid).Scan(&connectionState, &code, &next); err != nil {
		t.Fatal(err)
	}
	if connectionState != "error" || code != "ENCRYPTION_KEY_VERSION_UNAVAILABLE" || next != nil {
		t.Fatalf("connection terminal state: status=%s code=%s next=%v", connectionState, code, next)
	}
}
