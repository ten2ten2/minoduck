package main

import (
	"context"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"log/slog"
	"os"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("scheduler failed", "error", e)
		os.Exit(1)
	}
}
func run() error {
	c, e := platform.Load()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, e := platform.Open(ctx, c)
	if e != nil {
		return e
	}
	defer db.Close()
	q, e := river.NewClient(riverpgxv5.New(db), &river.Config{})
	if e != nil {
		return e
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT a.workspace_id,a.id FROM provider_accounts a JOIN workspaces w ON w.id=a.workspace_id WHERE a.status<>'disconnected' AND a.provider<>'csv' AND a.next_sync_at<=now() AND w.deletion_requested_at IS NULL ORDER BY a.next_sync_at LIMIT 100 FOR UPDATE OF a SKIP LOCKED`)
	if e != nil {
		return e
	}
	type item struct{ W, A string }
	items := []item{}
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.W, &i.A); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, i := range items {
		// sync_runs is FORCE RLS. Scheduled cross-workspace discovery happens on
		// non-tenant tables, then each enqueue is scoped before touching it.
		if _, e = tx.Exec(ctx, `SELECT set_config('app.workspace_id',$1,true)`, i.W); e != nil {
			return e
		}
		if _, e = tasks.EnqueueSync(ctx, tx, q, i.W, i.A); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at<now(); DELETE FROM login_tokens WHERE expires_at<now()-interval '1 day'; DELETE FROM oauth_states WHERE expires_at<now(); DELETE FROM invitations WHERE expires_at<now()-interval '7 days'`); e != nil {
		return e
	}
	maintenanceRows, e := tx.Query(ctx, `SELECT id FROM workspaces WHERE deletion_requested_at IS NULL AND next_maintenance_at<=now() LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if e != nil {
		return e
	}
	maintenanceIDs := []string{}
	for maintenanceRows.Next() {
		var id string
		if e = maintenanceRows.Scan(&id); e != nil {
			maintenanceRows.Close()
			return e
		}
		maintenanceIDs = append(maintenanceIDs, id)
	}
	e = maintenanceRows.Err()
	maintenanceRows.Close()
	if e != nil {
		return e
	}
	for _, id := range maintenanceIDs {
		if _, e = q.InsertTx(ctx, tx, tasks.Args{Task: "maintenance", WorkspaceID: id, ResourceID: id}, nil); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE workspaces SET next_maintenance_at=now()+interval '1 day' WHERE id=$1`, id); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
