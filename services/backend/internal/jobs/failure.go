package jobs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
)

func (w *Worker) failSyncRun(ctx context.Context, wid, rid, code string, disableRetry bool) error {
	tx, e := w.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var aid string
	var generation int
	e = tx.QueryRow(ctx, `UPDATE sync_runs SET state='failed',finished_at=now(),error_code=$3 WHERE workspace_id=$1 AND id=$2 AND state IN ('pending','running') RETURNING account_id,generation`, wid, rid, code).Scan(&aid, &generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_accounts SET status='error',error_code=$1,next_sync_at=CASE WHEN $2 THEN NULL ELSE next_sync_at END WHERE workspace_id=$3 AND id=$4 AND generation=$5 AND status<>'disconnected'`, code, disableRetry, wid, aid, generation); e != nil {
		return e
	}
	if w.Queue != nil {
		if _, e = w.Queue.InsertTx(ctx, tx, tasks.Args{Task: "insights", WorkspaceID: wid, ResourceID: wid}, nil); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
