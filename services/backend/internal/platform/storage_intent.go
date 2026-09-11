package platform

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterObjectIntent commits a durable cleanup record before an object write.
// A later database rollback can therefore never make an uploaded object invisible
// to maintenance.
func RegisterObjectIntent(ctx context.Context, db *pgxpool.Pool, wid, key, purpose string) error {
	tx, err := TenantTx(ctx, db, wid)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO object_intents(workspace_id,object_key,purpose) VALUES($1,$2,$3) ON CONFLICT(workspace_id,object_key) DO UPDATE SET state='pending',created_at=now()`, wid, key, purpose); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ClearObjectIntent(ctx context.Context, db *pgxpool.Pool, wid, key string) error {
	tx, err := TenantTx(ctx, db, wid)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM object_intents WHERE workspace_id=$1 AND object_key=$2`, wid, key); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CleanupObject retains the durable intent whenever deletion is uncertain. If
// deletion succeeds but clearing the row fails, maintenance safely repeats the
// idempotent delete later.
func CleanupObject(ctx context.Context, db *pgxpool.Pool, objects Objects, wid, key string) error {
	cleanup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := objects.Delete(cleanup, key); err != nil {
		return err
	}
	return ClearObjectIntent(cleanup, db, wid, key)
}
