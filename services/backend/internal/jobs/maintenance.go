package jobs

import (
	"context"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"time"
)

func (w *Worker) maintenance(ctx context.Context, wid string) error {
	tx, e := platform.TenantTx(ctx, w.DB, wid)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var code, status string
	var grace, retentionGrace *time.Time
	e = tx.QueryRow(ctx, `SELECT s.plan_code,s.status,s.grace_period_until,s.retention_grace_until FROM subscriptions s JOIN workspaces w ON w.billing_account_id=s.billing_account_id WHERE w.id=$1`, wid).Scan(&code, &status, &grace, &retentionGrace)
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	p := subscriptions.Effective(code, status, grace, now)
	cutoff := now.Truncate(24*time.Hour).AddDate(0, 0, -p.RetentionDays)
	hold := retentionGrace != nil && retentionGrace.After(now)
	if status == "past_due" && grace != nil && grace.AddDate(0, 0, 30).After(now) {
		hold = true
	}
	if !hold {
		// Evidence lifetimes follow their underlying ledger period, not their run date.
		if _, e = tx.Exec(ctx, `DELETE FROM reconciliation_runs WHERE workspace_id=$1 AND (period_end<$2 OR invoice_id IN(SELECT id FROM invoices WHERE workspace_id=$1 AND period_end<$2))`, wid, cutoff); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM invoices WHERE workspace_id=$1 AND period_end<$2`, wid, cutoff); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM cost_entries WHERE workspace_id=$1 AND period_end<$2`, wid, cutoff); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM usage_buckets WHERE workspace_id=$1 AND period_end<$2`, wid, cutoff); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM insights WHERE workspace_id=$1 AND generated_at<$2`, wid, cutoff); e != nil {
			return e
		}
	}
	rows, e := tx.Query(ctx, `SELECT id,object_key FROM exports WHERE workspace_id=$1 AND expires_at<now() AND object_key IS NOT NULL`, wid)
	if e != nil {
		return e
	}
	type object struct{ ID, Key string }
	objects := []object{}
	for rows.Next() {
		var o object
		if e = rows.Scan(&o.ID, &o.Key); e != nil {
			rows.Close()
			return e
		}
		objects = append(objects, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, o := range objects {
		if e = w.Objects.Delete(ctx, o.Key); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE exports SET state='expired',object_key=NULL WHERE workspace_id=$1 AND id=$2`, wid, o.ID); e != nil {
			return e
		}
	}
	rows, e = tx.Query(ctx, `SELECT id,object_key FROM source_batches b WHERE workspace_id=$1 AND (
 (state<>'committed' AND created_at<now()-interval '7 days')
 OR ($3=false AND state='committed' AND coalesce(nullif(preview->>'period_end','')::timestamptz,created_at)<$2)
 ) AND NOT EXISTS(SELECT 1 FROM cost_entries c WHERE c.workspace_id=$1 AND c.source_batch_id=b.id) AND NOT EXISTS(SELECT 1 FROM usage_buckets u WHERE u.workspace_id=$1 AND u.source_batch_id=b.id)`, wid, cutoff, hold)
	if e != nil {
		return e
	}
	objects = []object{}
	for rows.Next() {
		var o object
		if e = rows.Scan(&o.ID, &o.Key); e != nil {
			rows.Close()
			return e
		}
		objects = append(objects, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, o := range objects {
		if e = w.Objects.Delete(ctx, o.Key); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM source_batches WHERE workspace_id=$1 AND id=$2`, wid, o.ID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM notification_deliveries WHERE workspace_id=$1 AND created_at<now()-interval '90 days';`, wid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM audit_events WHERE workspace_id=$1 AND created_at<now()-interval '2 years'`, wid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
