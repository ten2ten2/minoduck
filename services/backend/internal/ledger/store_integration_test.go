package ledger

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ten2ten2/minoduck/services/backend/migrations"
)

func TestNativeCoverageUsesRetainedBatchPeriods(t *testing.T) {
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
	fixture, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback(ctx)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,email) VALUES($1,$2)`, []any{uid, "coverage-" + uuid.NewString() + "@example.test"}},
		{`INSERT INTO billing_accounts(id,owner_id) VALUES($1,$2)`, []any{billingID, uid}},
		{`INSERT INTO workspaces(id,billing_account_id,slug,name) VALUES($1,$2,$3,'Coverage test')`, []any{wid, billingID, "coverage-" + uuid.NewString()[:12]}},
		{`INSERT INTO provider_accounts(id,workspace_id,provider,name,external_account_ref,status,generation) VALUES($1,$2,'openai','Fixture','fixture','ready',1)`, []any{aid, wid}},
	} {
		if _, err = fixture.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = fixture.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -14)
	mid, end := start.AddDate(0, 0, 7), start.AddDate(0, 0, 14)
	insertBatch := func(from, to time.Time) string {
		t.Helper()
		id := uuid.NewString()
		if _, e := db.Exec(ctx, `INSERT INTO source_batches(id,workspace_id,account_id,object_key,content_hash,source_scope,cost_kind,source_timezone,granularity,state,period_start,period_end,committed_at) VALUES($1,$2,$3,$4,$5,'native-cost','actual','UTC','aggregate','committed',$6,$7,now())`, id, wid, aid, wid+"/"+id+".json", uuid.NewString(), from, to); e != nil {
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
	scopeComplete, scopeErr := CompleteScope(ctx, tx, wid, aid, "USD", "native-cost", start, end)
	_ = tx.Rollback(ctx)
	if err != nil || !complete {
		t.Fatalf("retained shards should cover the interval: complete=%v err=%v", complete, err)
	}
	if scopeErr != nil || !scopeComplete {
		t.Fatalf("retained batch periods should cover native costs without preview JSON: complete=%v err=%v", scopeComplete, scopeErr)
	}
	if _, err = db.Exec(ctx, `DELETE FROM source_batches WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	complete, err = CompleteUsage(ctx, tx, wid, aid, start, end)
	scopeComplete, scopeErr = CompleteScope(ctx, tx, wid, aid, "USD", "native-cost", start, end)
	_ = tx.Rollback(ctx)
	if err != nil || complete {
		t.Fatalf("pruned shard must make coverage incomplete despite sync_runs history: complete=%v err=%v", complete, err)
	}
	if scopeErr != nil || scopeComplete {
		t.Fatalf("pruned shard must also make cost coverage incomplete: complete=%v err=%v", scopeComplete, scopeErr)
	}
}
