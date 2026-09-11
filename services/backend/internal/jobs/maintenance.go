package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
)

type cleanupObject struct {
	ID, Key, Kind string
}

func (w *Worker) maintenance(ctx context.Context, wid string) error {
	objects, err := w.claimMaintenance(ctx, wid)
	if err != nil {
		return err
	}
	for _, object := range objects {
		if err = w.Objects.Delete(ctx, object.Key); err != nil {
			return err
		}
		if err = w.finishObjectDeletion(ctx, wid, object); err != nil {
			return err
		}
	}
	return nil
}

// claimMaintenance commits database state before any irreversible object
// operation. SKIP LOCKED prevents cleanup from racing an import commit.
func (w *Worker) claimMaintenance(ctx context.Context, wid string) ([]cleanupObject, error) {
	tx, err := platform.TenantTx(ctx, w.DB, wid)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var account, code, status string
	var grace, retentionGrace *time.Time
	if err = tx.QueryRow(ctx, `SELECT billing_account_id FROM workspaces WHERE id=$1`, wid).Scan(&account); err != nil {
		return nil, err
	}
	if err = subscriptions.LockAccount(ctx, tx, account); err != nil {
		return nil, err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1 AND billing_account_id=$2 AND deletion_requested_at IS NULL)`, wid, account).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	if err = tx.QueryRow(ctx, `SELECT plan_code,status,grace_period_until,retention_grace_until FROM subscriptions WHERE billing_account_id=$1 FOR UPDATE`, account).Scan(&code, &status, &grace, &retentionGrace); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	plan := subscriptions.Effective(code, status, grace, now)
	if err = subscriptions.ApplyEntitlements(ctx, tx, account, plan); err != nil {
		return nil, err
	}
	cutoff := now.Truncate(24*time.Hour).AddDate(0, 0, -plan.RetentionDays)
	hold := retentionGrace != nil && retentionGrace.After(now)
	if status == "past_due" && grace != nil && grace.AddDate(0, 0, 30).After(now) {
		hold = true
	}
	if !hold {
		for _, statement := range []string{
			`DELETE FROM reconciliation_runs WHERE workspace_id=$1 AND (period_end<$2 OR invoice_id IN(SELECT id FROM invoices WHERE workspace_id=$1 AND period_end<$2))`,
			`DELETE FROM invoices WHERE workspace_id=$1 AND period_end<$2`,
			`DELETE FROM cost_entries WHERE workspace_id=$1 AND period_end<$2`,
			`DELETE FROM usage_buckets WHERE workspace_id=$1 AND period_end<$2`,
			`DELETE FROM insights WHERE workspace_id=$1 AND generated_at<$2`,
		} {
			if _, err = tx.Exec(ctx, statement, wid, cutoff); err != nil {
				return nil, err
			}
		}
	}
	objects := []cleanupObject{}
	rows, err := tx.Query(ctx, `WITH picked AS (
 SELECT id FROM exports
 WHERE workspace_id=$1 AND object_key IS NOT NULL AND (state='deleting' OR expires_at<now())
 FOR UPDATE SKIP LOCKED
) UPDATE exports e SET state='deleting' FROM picked p
WHERE e.workspace_id=$1 AND e.id=p.id RETURNING e.id,e.object_key`, wid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		object := cleanupObject{Kind: "export"}
		if err = rows.Scan(&object.ID, &object.Key); err != nil {
			rows.Close()
			return nil, err
		}
		objects = append(objects, object)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx, `WITH picked AS (
 SELECT id FROM source_batches b
 WHERE workspace_id=$1 AND (
  state='deleting'
  OR (state<>'committed' AND created_at<now()-interval '7 days')
  OR ($3=false AND state='committed' AND coalesce(period_end,created_at)<$2)
 ) AND NOT EXISTS(SELECT 1 FROM cost_entries c WHERE c.workspace_id=$1 AND c.source_batch_id=b.id)
   AND NOT EXISTS(SELECT 1 FROM usage_buckets u WHERE u.workspace_id=$1 AND u.source_batch_id=b.id)
 FOR UPDATE SKIP LOCKED
) UPDATE source_batches b SET state='deleting' FROM picked p
WHERE b.workspace_id=$1 AND b.id=p.id RETURNING b.id,b.object_key`, wid, cutoff, hold)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		object := cleanupObject{Kind: "source"}
		if err = rows.Scan(&object.ID, &object.Key); err != nil {
			rows.Close()
			return nil, err
		}
		objects = append(objects, object)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Successful writes no longer need their intent. Unlinked intents become
	// recoverable orphan-cleanup work after in-flight requests have had an hour.
	if _, err = tx.Exec(ctx, `DELETE FROM object_intents i WHERE workspace_id=$1 AND (
 EXISTS(SELECT 1 FROM source_batches b WHERE b.workspace_id=$1 AND b.object_key=i.object_key)
 OR EXISTS(SELECT 1 FROM exports e WHERE e.workspace_id=$1 AND e.object_key=i.object_key)
)`, wid); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `UPDATE object_intents SET state='deleting'
WHERE workspace_id=$1 AND (state='deleting' OR created_at<now()-interval '1 hour')
RETURNING object_key`, wid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		object := cleanupObject{Kind: "intent"}
		if err = rows.Scan(&object.Key); err != nil {
			rows.Close()
			return nil, err
		}
		objects = append(objects, object)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, statement := range []string{
		`DELETE FROM notification_deliveries WHERE workspace_id=$1 AND created_at<now()-interval '90 days'`,
		`DELETE FROM audit_events WHERE workspace_id=$1 AND created_at<now()-interval '2 years'`,
	} {
		if _, err = tx.Exec(ctx, statement, wid); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_rate_events WHERE created_at<now()-interval '24 hours'`); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return objects, nil
}

func (w *Worker) finishObjectDeletion(ctx context.Context, wid string, object cleanupObject) error {
	tx, err := platform.TenantTx(ctx, w.DB, wid)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	switch object.Kind {
	case "export":
		_, err = tx.Exec(ctx, `UPDATE exports SET state='expired',object_key=NULL WHERE workspace_id=$1 AND id=$2 AND state='deleting'`, wid, object.ID)
	case "source":
		_, err = tx.Exec(ctx, `DELETE FROM source_batches b WHERE workspace_id=$1 AND id=$2 AND state='deleting'
 AND NOT EXISTS(SELECT 1 FROM cost_entries c WHERE c.workspace_id=$1 AND c.source_batch_id=b.id)
 AND NOT EXISTS(SELECT 1 FROM usage_buckets u WHERE u.workspace_id=$1 AND u.source_batch_id=b.id)`, wid, object.ID)
	case "intent":
		_, err = tx.Exec(ctx, `DELETE FROM object_intents WHERE workspace_id=$1 AND object_key=$2 AND state='deleting'`, wid, object.Key)
	default:
		return fmt.Errorf("UNKNOWN_CLEANUP_KIND")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
