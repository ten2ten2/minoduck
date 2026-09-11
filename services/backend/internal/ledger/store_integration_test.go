package ledger

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ten2ten2/minoduck/services/backend/migrations"
)

func TestCompleteUsageUsesRetainedSourceBatches(t *testing.T) {
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
	if _, err = db.Exec(ctx, `INSERT INTO users(id,email) VALUES($1,$2); INSERT INTO billing_accounts(id,owner_id) VALUES($3,$1); INSERT INTO workspaces(id,billing_account_id,slug,name) VALUES($4,$3,$5,'Coverage test'); INSERT INTO provider_accounts(id,workspace_id,provider,name,external_account_ref,status,generation) VALUES($6,$4,'openai','Fixture','fixture','ready',1)`, uid, "coverage-"+uuid.NewString()+"@example.test", billingID, wid, "coverage-"+uuid.NewString()[:12], aid); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -14)
	mid, end := start.AddDate(0, 0, 7), start.AddDate(0, 0, 14)
	insertBatch := func(from, to time.Time) string {
		t.Helper()
		id := uuid.NewString()
		preview, _ := json.Marshal(map[string]any{"period_start": from, "period_end": to})
		if _, e := db.Exec(ctx, `INSERT INTO source_batches(id,workspace_id,account_id,object_key,content_hash,source_scope,cost_kind,source_timezone,granularity,state,preview,committed_at) VALUES($1,$2,$3,$4,$5,'native-cost','actual','UTC','aggregate','committed',$6,now())`, id, wid, aid, wid+"/"+id+".json", uuid.NewString(), preview); e != nil {
			t.Fatal(e)
		}
		return id
	}
	insertBatch(start, mid)
	second := insertBatch(mid, end)
	// A successful historical run alone must not keep claiming coverage after
	// its persisted shard evidence has been pruned by retention.
	if _, err = db.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,account_id,generation,state,period_start,period_end) VALUES($1,$2,$3,1,'succeeded',$4,$5)`, uuid.NewString(), wid, aid, start, end); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := CompleteUsage(ctx, tx, wid, aid, start, end)
	_ = tx.Rollback(ctx)
	if err != nil || !complete {
		t.Fatalf("retained shards should cover the interval: complete=%v err=%v", complete, err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM source_batches WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	complete, err = CompleteUsage(ctx, tx, wid, aid, start, end)
	_ = tx.Rollback(ctx)
	if err != nil || complete {
		t.Fatalf("pruned shard must make coverage incomplete despite sync_runs history: complete=%v err=%v", complete, err)
	}
}
