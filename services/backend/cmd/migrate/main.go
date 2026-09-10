package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/ten2ten2/minoduck/services/backend/migrations"
	"log/slog"
	"os"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("migration failed", "error", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := os.Getenv("MIGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = migrations.Apply(ctx, db); e != nil {
		return e
	}
	m, e := rivermigrate.New(riverpgxv5.New(db), nil)
	if e != nil {
		return e
	}
	_, e = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return e
}
