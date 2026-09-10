package platform

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TenantTx(ctx context.Context, pool *pgxpool.Pool, wid string) (pgx.Tx, error) {
	tx, e := pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "SELECT set_config('app.workspace_id',$1,true)", wid); e != nil {
		tx.Rollback(ctx)
		return nil, e
	}
	return tx, nil
}
func Audit(ctx context.Context, tx pgx.Tx, wid, actor, action, subject string, details any) error {
	b, e := json.Marshal(details)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,subject_id,details) VALUES($1,nullif($2,'')::uuid,$3,$4,$5)`, wid, actor, action, subject, b)
	return e
}
func JSONRows(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]json.RawMessage, error) {
	rows, e := tx.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
