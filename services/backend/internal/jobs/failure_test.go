package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ten2ten2/minoduck/services/backend/migrations"
)

func TestFailSyncRunFencesGeneration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}

	uid, billingID, wid, aid := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	email := "jobs-" + uuid.NewString() + "@example.test"
	if _, err = db.Exec(ctx, `INSERT INTO users(id,email) VALUES($1,$2); INSERT INTO billing_accounts(id,owner_id) VALUES($3,$1); INSERT INTO workspaces(id,billing_account_id,slug,name) VALUES($4,$3,$5,'Jobs test'); INSERT INTO provider_accounts(id,workspace_id,provider,name,external_account_ref,status,generation,next_sync_at) VALUES($6,$4,'openai','Fixture','fixture','ready',2,now()+interval '1 hour')`, uid, email, billingID, wid, "jobs-"+uuid.NewString()[:12], aid); err != nil {
		t.Fatal(err)
	}
	worker := Worker{DB: db}
	start := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1)

	staleRun := uuid.NewString()
	if _, err = db.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,account_id,generation,period_start,period_end) VALUES($1,$2,$3,1,$4,$5)`, staleRun, wid, aid, start, end); err != nil {
		t.Fatal(err)
	}
	if err = worker.failSyncRun(ctx, wid, staleRun, "SYNC_FAILED", false); err != nil {
		t.Fatal(err)
	}
	var runState, connectionState string
	if err = db.QueryRow(ctx, `SELECT state FROM sync_runs WHERE id=$1`, staleRun).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT status FROM provider_accounts WHERE id=$1`, aid).Scan(&connectionState); err != nil {
		t.Fatal(err)
	}
	if runState != "failed" || connectionState != "ready" {
		t.Fatalf("stale generation changed connection: run=%s connection=%s", runState, connectionState)
	}

	currentRun := uuid.NewString()
	if _, err = db.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,account_id,generation,period_start,period_end) VALUES($1,$2,$3,2,$4,$5)`, currentRun, wid, aid, start, end); err != nil {
		t.Fatal(err)
	}
	if err = worker.failSyncRun(ctx, wid, currentRun, "CREDENTIAL_DECRYPTION_FAILED", true); err != nil {
		t.Fatal(err)
	}
	var code string
	var next *time.Time
	if err = db.QueryRow(ctx, `SELECT status,error_code,next_sync_at FROM provider_accounts WHERE id=$1`, aid).Scan(&connectionState, &code, &next); err != nil {
		t.Fatal(err)
	}
	if connectionState != "error" || code != "CREDENTIAL_DECRYPTION_FAILED" || next != nil {
		t.Fatalf("current generation terminal state: status=%s code=%s next=%v", connectionState, code, next)
	}
}
