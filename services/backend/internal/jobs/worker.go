package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/ten2ten2/minoduck/services/backend/internal/connectors"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"time"
)

type Worker struct {
	river.WorkerDefaults[tasks.Args]
	DB      *pgxpool.Pool
	Config  platform.Config
	Objects platform.Objects
	Queue   *river.Client[pgx.Tx]
}

func (w *Worker) Timeout(*river.Job[tasks.Args]) time.Duration { return 12 * time.Minute }
func (w *Worker) Work(ctx context.Context, job *river.Job[tasks.Args]) (err error) {
	defer func() {
		// River 0.44+ refunds an interrupted attempt during shutdown. Preserve
		// application state so the retried job can finish after the worker restarts.
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) && job.Attempt >= job.MaxAttempts {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if job.Args.Task == "sync" {
				_ = w.failSyncRun(cleanup, job.Args.WorkspaceID, job.Args.ResourceID, "SYNC_FAILED", false)
			}
			if job.Args.Task == "export" {
				tx, e := platform.TenantTx(cleanup, w.DB, job.Args.WorkspaceID)
				if e == nil {
					defer tx.Rollback(cleanup)
					_, e = tx.Exec(cleanup, `UPDATE exports SET state='failed' WHERE workspace_id=$1 AND id=$2 AND state='pending'`, job.Args.WorkspaceID, job.Args.ResourceID)
					if e == nil {
						_ = tx.Commit(cleanup)
					}
				}
			}
			if job.Args.Task == "notification" {
				_, _ = w.DB.Exec(cleanup, `UPDATE notification_deliveries SET state='failed' WHERE workspace_id=$1 AND id=$2 AND state='pending'`, job.Args.WorkspaceID, job.Args.ResourceID)
			}
		}
	}()
	a := job.Args
	if _, e := uuid.Parse(a.WorkspaceID); e != nil {
		return river.JobCancel(fmt.Errorf("INVALID_WORKSPACE"))
	}
	if a.Task == "delete_workspace" {
		return w.deleteWorkspace(ctx, a.WorkspaceID)
	}
	var exists bool
	e := w.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1 AND deletion_requested_at IS NULL)`, a.WorkspaceID).Scan(&exists)
	if e != nil {
		return e
	}
	if !exists {
		return river.JobCancel(fmt.Errorf("WORKSPACE_DELETED"))
	}
	switch a.Task {
	case "maintenance":
		return w.maintenance(ctx, a.WorkspaceID)
	case "sync":
		return w.sync(ctx, job)
	case "insights":
		return w.buildInsights(ctx, a.WorkspaceID)
	case "export":
		return w.export(ctx, a)
	case "notification":
		return w.notify(ctx, a)
	default:
		return river.JobCancel(fmt.Errorf("UNKNOWN_TASK"))
	}
}
func (w *Worker) sync(ctx context.Context, job *river.Job[tasks.Args]) error {
	a := job.Args
	var aid, provider, status, keyID string
	var generation, currentGeneration int
	var encrypted []byte
	var start, end time.Time
	e := w.DB.QueryRow(ctx, `SELECT r.account_id,r.generation,r.period_start,r.period_end,p.provider,p.status,p.credential_cipher,p.credential_key_id,p.generation FROM sync_runs r JOIN provider_accounts p ON p.id=r.account_id AND p.workspace_id=r.workspace_id WHERE r.id=$1 AND r.workspace_id=$2`, a.ResourceID, a.WorkspaceID).Scan(&aid, &generation, &start, &end, &provider, &status, &encrypted, &keyID, &currentGeneration)
	if e != nil {
		return e
	}
	if generation != currentGeneration || status == "disconnected" {
		return river.JobCancel(fmt.Errorf("CONNECTION_DISCONNECTED"))
	}
	if keyID != w.Config.KeyID {
		if e = w.failSyncRun(ctx, a.WorkspaceID, a.ResourceID, "ENCRYPTION_KEY_VERSION_UNAVAILABLE", true); e != nil {
			return e
		}
		return river.JobCancel(fmt.Errorf("ENCRYPTION_KEY_VERSION_UNAVAILABLE"))
	}
	key, e := platform.Decrypt(w.Config.MasterKey, encrypted, a.WorkspaceID+":"+aid)
	if e != nil {
		if e = w.failSyncRun(ctx, a.WorkspaceID, a.ResourceID, "CREDENTIAL_DECRYPTION_FAILED", true); e != nil {
			return e
		}
		return river.JobCancel(fmt.Errorf("CREDENTIAL_DECRYPTION_FAILED"))
	}
	// A session advisory lock covers network work; no transaction is held while fetching.
	lease, e := w.DB.Acquire(ctx)
	if e != nil {
		return e
	}
	defer lease.Release()
	var locked bool
	if e = lease.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, aid).Scan(&locked); e != nil {
		return e
	}
	if !locked {
		return river.JobSnooze(30 * time.Second)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = lease.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, aid)
	}()
	started, e := w.DB.Exec(ctx, `UPDATE sync_runs SET state='running',attempts=attempts+1,started_at=coalesce(started_at,now()) WHERE workspace_id=$1 AND id=$2 AND state<>'canceled'`, a.WorkspaceID, a.ResourceID)
	if e != nil {
		return e
	}
	if started.RowsAffected() == 0 {
		return river.JobCancel(fmt.Errorf("SYNC_CANCELED"))
	}
	defer func() {
		if errors.Is(ctx.Err(), context.Canceled) {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Match River's refunded attempt without reviving disconnected runs.
			_, _ = w.DB.Exec(cleanup, `UPDATE sync_runs SET state='pending',attempts=greatest(attempts-1,0) WHERE workspace_id=$1 AND id=$2 AND state='running'`, a.WorkspaceID, a.ResourceID)
		}
	}()
	for from := start; from.Before(end); from = from.AddDate(0, 0, 7) {
		to := from.AddDate(0, 0, 7)
		if to.After(end) {
			to = end
		}
		snapshot, e := (connectors.Client{}).Fetch(ctx, provider, key, from, to)
		if e != nil {
			return w.syncFailure(ctx, job, aid, generation, e)
		}
		batch := uuid.NewString()
		object := a.WorkspaceID + "/" + batch + ".json"
		data, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		if len(data) > 64*1024*1024 {
			return w.syncFailure(ctx, job, aid, generation, connectors.Failure{Code: "SOURCE_TOO_LARGE", Permanent: true})
		}
		if e = w.Objects.Put(ctx, object, data); e != nil {
			return e
		}
		tx, e := platform.TenantTx(ctx, w.DB, a.WorkspaceID)
		if e != nil {
			_ = w.Objects.Delete(ctx, object)
			return e
		}
		e = w.publishShard(ctx, tx, a, aid, generation, batch, object, data, snapshot)
		if e != nil {
			tx.Rollback(ctx)
			_ = w.Objects.Delete(ctx, object)
			var failure connectors.Failure
			if errors.As(e, &failure) {
				return w.syncFailure(ctx, job, aid, generation, e)
			}
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			_ = w.Objects.Delete(ctx, object)
			return e
		}
	}
	tx, e := platform.TenantTx(ctx, w.DB, a.WorkspaceID)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var g int
	e = tx.QueryRow(ctx, `SELECT generation FROM provider_accounts WHERE workspace_id=$1 AND id=$2 AND status<>'disconnected' FOR UPDATE`, a.WorkspaceID, aid).Scan(&g)
	if e != nil || g != generation {
		return river.JobCancel(fmt.Errorf("CONNECTION_DISCONNECTED"))
	}
	if _, e = tx.Exec(ctx, `UPDATE sync_runs SET state='succeeded',finished_at=now(),error_code=NULL WHERE workspace_id=$1 AND id=$2`, a.WorkspaceID, a.ResourceID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_accounts SET status='ready',last_sync_at=now(),data_through=$1,error_code=NULL WHERE workspace_id=$2 AND id=$3`, end, a.WorkspaceID, aid); e != nil {
		return e
	}
	if _, e = w.Queue.InsertTx(ctx, tx, tasks.Args{Task: "insights", WorkspaceID: a.WorkspaceID, ResourceID: a.WorkspaceID}, nil); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (w *Worker) publishShard(ctx context.Context, tx pgx.Tx, a tasks.Args, aid string, generation int, batch, object string, data []byte, snapshot connectors.Snapshot) error {
	var g int
	var state string
	e := tx.QueryRow(ctx, `SELECT generation,status FROM provider_accounts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, a.WorkspaceID, aid).Scan(&g, &state)
	if e != nil {
		return e
	}
	if g != generation || state == "disconnected" {
		return river.JobCancel(fmt.Errorf("CONNECTION_DISCONNECTED"))
	}
	var deleted bool
	e = tx.QueryRow(ctx, `SELECT deletion_requested_at IS NOT NULL FROM workspaces WHERE id=$1 FOR SHARE`, a.WorkspaceID).Scan(&deleted)
	if e != nil {
		return e
	}
	if deleted {
		return river.JobCancel(fmt.Errorf("WORKSPACE_DELETED"))
	}
	var overlap bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND source_scope<>'native-cost' AND period_start<$4 AND period_end>$3)`, a.WorkspaceID, aid, snapshot.Start, snapshot.End).Scan(&overlap); e != nil {
		return e
	}
	if overlap {
		return connectors.Failure{Code: "OVERLAPPING_SOURCE_SCOPE", Permanent: true}
	}
	preview, _ := json.Marshal(map[string]any{"period_start": snapshot.Start, "period_end": snapshot.End, "row_count": len(snapshot.Entries), "connector_version": snapshot.Version})
	_, e = tx.Exec(ctx, `INSERT INTO source_batches(id,workspace_id,account_id,object_key,content_hash,source_scope,cost_kind,source_timezone,granularity,state,preview,committed_at) VALUES($1,$2,$3,$4,$5,'native-cost','actual','UTC','aggregate','committed',$6,now())`, batch, a.WorkspaceID, aid, object, ledger.Hash(data), preview)
	if e != nil {
		return e
	}
	if _, e = ledger.Publish(ctx, tx, a.WorkspaceID, aid, batch, snapshot.Entries); e != nil {
		return e
	}
	// Only a fully fetched shard may withdraw records omitted by a provider correction.
	keys := []string{}
	for _, v := range snapshot.Entries {
		keys = append(keys, v.Key)
	}
	if _, e = tx.Exec(ctx, `UPDATE cost_entries SET is_current=false WHERE workspace_id=$1 AND account_id=$2 AND source_scope='native-cost' AND cost_kind='actual' AND is_current AND period_start>=$3 AND period_end<=$4 AND NOT(source_record_key=ANY($5))`, a.WorkspaceID, aid, snapshot.Start, snapshot.End, keys); e != nil {
		return e
	}
	usageKeys := []string{}
	for _, u := range snapshot.Usage {
		usageKeys = append(usageKeys, u.Key)
		metrics, _ := json.Marshal(u.Metrics)
		dims, _ := json.Marshal(u.Dimensions)
		_, e = tx.Exec(ctx, `INSERT INTO usage_buckets(workspace_id,account_id,source_batch_id,source_record_key,period_start,period_end,model,metrics,dimensions) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(workspace_id,account_id,source_record_key) DO UPDATE SET source_batch_id=excluded.source_batch_id,metrics=excluded.metrics,dimensions=excluded.dimensions`, a.WorkspaceID, aid, batch, u.Key, u.Start, u.End, u.Model, metrics, dims)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM usage_buckets WHERE workspace_id=$1 AND account_id=$2 AND period_start>=$3 AND period_end<=$4 AND NOT(source_record_key=ANY($5))`, a.WorkspaceID, aid, snapshot.Start, snapshot.End, usageKeys); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO sync_checkpoints(workspace_id,account_id,resource_kind,data_through) VALUES($1,$2,'cost_and_usage',$3) ON CONFLICT(account_id,resource_kind) DO UPDATE SET data_through=greatest(sync_checkpoints.data_through,excluded.data_through)`, a.WorkspaceID, aid, snapshot.End)
	return e
}
func (w *Worker) syncFailure(ctx context.Context, job *river.Job[tasks.Args], aid string, generation int, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var f connectors.Failure
	if !errors.As(err, &f) {
		f = connectors.Failure{Code: "PROVIDER_UNAVAILABLE"}
	}
	var attempts int
	if e := w.DB.QueryRow(ctx, `SELECT attempts FROM sync_runs WHERE workspace_id=$1 AND id=$2`, job.Args.WorkspaceID, job.Args.ResourceID).Scan(&attempts); e != nil {
		return e
	}
	terminal := f.Permanent || job.Attempt >= job.MaxAttempts || attempts >= job.MaxAttempts
	state := "pending"
	if terminal {
		state = "failed"
	}
	tx, e := w.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE sync_runs SET state=$1,error_code=$2,finished_at=CASE WHEN $1='failed' THEN now() ELSE NULL END WHERE workspace_id=$3 AND id=$4 AND state<>'canceled'`, state, f.Code, job.Args.WorkspaceID, job.Args.ResourceID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_accounts SET status='error',error_code=$1,next_sync_at=CASE WHEN $2 THEN NULL ELSE next_sync_at END WHERE workspace_id=$3 AND id=$4 AND generation=$5 AND status<>'disconnected'`, f.Code, f.Permanent, job.Args.WorkspaceID, aid, generation); e != nil {
		return e
	}
	if state == "failed" {
		if _, e = w.Queue.InsertTx(ctx, tx, tasks.Args{Task: "insights", WorkspaceID: job.Args.WorkspaceID, ResourceID: job.Args.WorkspaceID}, nil); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if terminal {
		return river.JobCancel(f)
	}
	if f.RetryAfter > 0 && job.Attempt < job.MaxAttempts {
		if f.RetryAfter > time.Hour {
			f.RetryAfter = time.Hour
		}
		return river.JobSnooze(f.RetryAfter)
	}
	return f
}
func (w *Worker) deleteWorkspace(ctx context.Context, wid string) error {
	var deleted bool
	e := w.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deletion_tombstones WHERE workspace_id=$1)`, wid).Scan(&deleted)
	if e != nil {
		return e
	}
	if !deleted {
		return river.JobCancel(fmt.Errorf("DELETION_NOT_REQUESTED"))
	}
	// Tombstone and revoked generation prevent an in-flight sync from publishing.
	if e = w.Objects.DeleteWorkspace(ctx, wid); e != nil {
		return e
	}
	tx, e := platform.TenantTx(ctx, w.DB, wid)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE river_job SET state='cancelled',finalized_at=now() WHERE args->>'workspace_id'=$1 AND args->>'task'<>'delete_workspace' AND state IN ('available','scheduled','retryable')`, wid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, wid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE deletion_tombstones SET completed_at=now() WHERE workspace_id=$1`, wid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
