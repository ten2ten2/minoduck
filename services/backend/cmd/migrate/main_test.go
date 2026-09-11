package main

import "testing"

func TestMigrationRequiresItsOwnDatabaseURL(t *testing.T) {
	t.Setenv("MIGRATION_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "postgres://runtime:password@localhost/minoduck")
	if err := run(); err == nil || err.Error() != "MIGRATION_DATABASE_URL is required" {
		t.Fatalf("must reject missing migration credentials before connecting: %v", err)
	}
}
