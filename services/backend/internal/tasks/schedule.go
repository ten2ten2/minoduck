package tasks

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"time"
)

func EnqueueSync(ctx context.Context, tx pgx.Tx, q *river.Client[pgx.Tx], wid, aid string) (string, error) {
	var provider, status string
	var generation int
	var through *time.Time
	e := tx.QueryRow(ctx, `SELECT provider,status,generation,data_through FROM provider_accounts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, wid, aid).Scan(&provider, &status, &generation, &through)
	if e != nil {
		return "", e
	}
	if status == "disconnected" || provider == "csv" {
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
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -7)
	// First value is seven complete days; subsequent runs include the current and
	// previous calendar month, capped to provider history and current entitlement.
	if through != nil {
		start = time.Date(end.Year(), end.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	}
	var plan, statusPlan string
	var grace *time.Time
	e = tx.QueryRow(ctx, `SELECT s.plan_code,s.status,s.grace_period_until FROM subscriptions s JOIN workspaces w ON w.billing_account_id=s.billing_account_id WHERE w.id=$1`, wid).Scan(&plan, &statusPlan, &grace)
	if e != nil {
		return "", e
	}
	paid := statusPlan == "active" || (statusPlan == "past_due" && grace != nil && grace.After(time.Now()))
	limit := 90
	if !paid || plan == "free" || provider == "openrouter" {
		limit = 30
	}
	floor := end.AddDate(0, 0, -limit)
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
	_, e = tx.Exec(ctx, `UPDATE provider_accounts SET next_sync_at=$1 WHERE id=$2 AND workspace_id=$3`, time.Now().Add(time.Duration(hours)*time.Hour), aid, wid)
	return id, e
}
