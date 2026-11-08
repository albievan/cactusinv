//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/ports"
)

// TestSchemaGuardAndDownTo (M27): a database migrated by a newer version is refused (goose
// alone would start on it), and down-to undoes an upgrade's migrations, keeping the data.
func TestSchemaGuardAndDownTo(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if cur, head, err := mariadb.SchemaVersions(ctx, testDB); err != nil || cur != headSchema || head != headSchema {
		t.Fatalf("versions %d %d %v", cur, head, err)
	}

	if err := mariadb.MigrateDownTo(ctx, testDB, headSchema-1); err != nil {
		t.Fatal(err)
	}
	if cur, _, _ := mariadb.SchemaVersions(ctx, testDB); cur != headSchema-1 {
		t.Fatalf("after down-to: schema %d", cur)
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	if cur, _, _ := mariadb.SchemaVersions(ctx, testDB); cur != headSchema {
		t.Fatalf("after up: schema %d", cur)
	}
	if got, _, err := e.svc.ListCustomers(e.ctx, ports.CustomerFilter{}); err != nil || len(got) != 1 {
		t.Fatalf("customers after down and up: %d %v", len(got), err)
	}
	if err := mariadb.MigrateDownTo(ctx, testDB, 0); err == nil {
		t.Fatal("down-to 0 was accepted")
	}

	// A newer version's migration.
	if _, err := testDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, headSchema+5); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM goose_db_version WHERE version_id = ?`, headSchema+5)
	})
	var newer *mariadb.SchemaNewerError
	if err := mariadb.Migrate(ctx, testDB, false); !errors.As(err, &newer) || newer.Database != headSchema+5 || newer.Code != headSchema {
		t.Fatalf("newer schema: %v", err)
	}
	if err := mariadb.MigrateDownTo(ctx, testDB, headSchema-1); !errors.As(err, &newer) {
		t.Fatalf("down-to on a newer schema: %v", err)
	}
	if cur, _, _ := mariadb.SchemaVersions(ctx, testDB); cur != headSchema+5 {
		t.Fatalf("the refusal changed the schema: %d", cur)
	}
}

// TestMigrateFinishesAfterCancel: a shutdown (SIGTERM cancels the context) during the
// migrations does not stop them halfway; MariaDB cannot roll back DDL, so a half-applied
// migration made every later start fail ("Duplicate column name", k3s 2026-11-08).
func TestMigrateFinishesAfterCancel(t *testing.T) {
	setup(t)
	bg := context.Background()
	if err := mariadb.MigrateDownTo(bg, testDB, headSchema-3); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	defer mariadb.SetAfterMigrationLock(cancel)()
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatalf("migrate cancelled after the lock: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("the context was not cancelled")
	}
	if cur, _, _ := mariadb.SchemaVersions(bg, testDB); cur != headSchema {
		t.Fatalf("schema %d, want %d", cur, headSchema)
	}
}
