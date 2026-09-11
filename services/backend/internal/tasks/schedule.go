package tasks

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"time"
)

func EnqueueSync(ctx context.Context, tx pgx.Tx, q *river.Client[pgx.Tx], wid, aid string) (string, error) {
	now := time.Now().UTC()
	var account, plan, statusPlan string
	var grace *time.Time
	e := tx.QueryRow(ctx, `SELECT billing_account_id FROM workspaces WHERE id=$1`, wid).Scan(&account)
	if e != nil {
		return "", e
	}
	if e = subscriptions.LockAccount(ctx, tx, account); e != nil {
		return "", e
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1 AND billing_account_id=$2 AND deletion_requested_at IS NULL)`, wid, account).Scan(&active); e != nil {
		return "", e
	}
	if !active {
		return "", nil
	}
	e = tx.QueryRow(ctx, `SELECT plan_code,status,grace_period_until FROM subscriptions WHERE billing_account_id=$1 FOR UPDATE`, account).Scan(&plan, &statusPlan, &grace)
	if e != nil {
		return "", e
	}
	effective := subscriptions.Effective(plan, statusPlan, grace, now)
	if e = subscriptions.ApplyEntitlements(ctx, tx, account, effective); e != nil {
		return "", e
	}
	var provider, status string
	var generation int
	var backfill *time.Time
	var suspended bool
	e = tx.QueryRow(ctx, `SELECT a.provider,a.status,a.generation,a.backfill_cursor,a.billing_suspended OR w.billing_suspended FROM provider_accounts a JOIN workspaces w ON w.id=a.workspace_id WHERE a.workspace_id=$1 AND a.id=$2 FOR UPDATE OF a`, wid, aid).Scan(&provider, &status, &generation, &backfill, &suspended)
	if e != nil {
		return "", e
	}
	if status == "disconnected" || provider == "csv" || suspended {
		return "", nil
	}
	var existing string
	e = tx.QueryRow(ctx, `SELECT id FROM sync_runs WHERE account_id=$1 AND state IN ('pending','running')`, aid).Scan(&existing)
	if e == nil {
		return existing, nil
	}
	if e != pgx.ErrNoRows {
		return "", e
	}
	latest := now.Truncate(24 * time.Hour)
	paid := statusPlan == "active" || (statusPlan == "past_due" && grace != nil && grace.After(now))
	limit := 90
	if !paid || plan == "free" || provider == "openrouter" {
		limit = 30
	}
	floor := latest.AddDate(0, 0, -limit)
	end := latest
	start := end.AddDate(0, 0, -7)
	// Walk backward in bounded seven-day slices until the advertised history is
	// populated. Once caught up, refresh only the most recent fourteen days so
	// provider corrections are observed without rewriting two calendar months on
	// every scheduled run.
	if backfill != nil && backfill.After(floor) {
		end = backfill.UTC().Truncate(24 * time.Hour)
		start = end.AddDate(0, 0, -7)
	} else if backfill != nil {
		start = latest.AddDate(0, 0, -14)
	}
	if start.Before(floor) {
		start = floor
	}
	id := uuid.NewString()
	_, e = tx.Exec(ctx, `INSERT INTO sync_runs(id,workspace_id,account_id,generation,period_start,period_end) VALUES($1,$2,$3,$4,$5,$6)`, id, wid, aid, generation, start, end)
	if e != nil {
		return "", e
	}
	if _, e = q.InsertTx(ctx, tx, Args{Task: "sync", WorkspaceID: wid, ResourceID: id}, nil); e != nil {
		return "", e
	}
	hours := 24
	if paid && plan != "free" && provider != "openrouter" {
		hours = 2
	}
	_, e = tx.Exec(ctx, `UPDATE provider_accounts SET next_sync_at=$1 WHERE id=$2 AND workspace_id=$3`, now.Add(time.Duration(hours)*time.Hour), aid, wid)
	return id, e
}
