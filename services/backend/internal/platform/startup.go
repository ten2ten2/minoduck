package platform

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, c Config) (*pgxpool.Pool, error) {
	config, e := pgxpool.ParseConfig(c.DatabaseURL)
	if e != nil {
		return nil, fmt.Errorf("INVALID_DATABASE_URL")
	}
	config.MaxConns = 12
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		return nil, e
	}
	if e = pool.Ping(ctx); e != nil {
		pool.Close()
		return nil, e
	}
	if c.Env == "production" {
		var privileged bool
		e = pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&privileged)
		if e != nil || privileged {
			pool.Close()
			return nil, fmt.Errorf("RUNTIME_ROLE_MUST_ENFORCE_RLS")
		}
	}
	return pool, nil
}
