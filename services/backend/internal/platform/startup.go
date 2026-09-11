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
		var privileged, canCreateSchema bool
		e = pool.QueryRow(ctx, `SELECT r.rolsuper OR r.rolbypassrls, has_schema_privilege(current_user,'public','CREATE') FROM pg_roles r WHERE r.rolname=current_user`).Scan(&privileged, &canCreateSchema)
		if e != nil || privileged || canCreateSchema {
			pool.Close()
			return nil, fmt.Errorf("RUNTIME_ROLE_MUST_BE_UNPRIVILEGED")
		}
	}
	return pool, nil
}
