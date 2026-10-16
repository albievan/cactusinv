//go:build integration

package mariadb_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/migrations"
)

// 00019 splits each per-area edit permission into one per action without changing what a
// group can do, and its down migration folds them back.
func TestGranularPermissionsMigration(t *testing.T) {
	ctx := context.Background()
	setup(t)
	provider, err := goose.NewProvider(goose.DialectMySQL, testDB, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 18); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mariadb.Migrate(ctx, testDB, false) })
	if _, err := testDB.ExecContext(ctx, `INSERT INTO access_groups (id, name, created_at, updated_at) VALUES ('g1', 'Clerks', NOW(), NOW())`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"invoices.view", "invoices.edit", "invoices.status", "customers.edit", "emails.manage", "settings.edit"} {
		if _, err := testDB.ExecContext(ctx, `INSERT INTO access_group_permissions (group_id, permission) VALUES ('g1', ?)`, p); err != nil {
			t.Fatal(err)
		}
	}
	perms := func() string {
		rows, err := testDB.QueryContext(ctx, `SELECT permission FROM access_group_permissions WHERE group_id = 'g1'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		slices.Sort(out)
		return strings.Join(out, " ")
	}
	if err := mariadb.MigrateTo(ctx, testDB, 19); err != nil {
		t.Fatal(err)
	}
	want := "customers.create customers.delete customers.edit emails.cancel emails.retry invoices.create invoices.delete invoices.edit invoices.issue invoices.overdue invoices.pay invoices.view invoices.void settings.banks settings.business settings.currencies settings.email"
	if got := perms(); got != want {
		t.Fatalf("up:\n got %s\nwant %s", got, want)
	}
	if _, err := provider.DownTo(ctx, 18); err != nil {
		t.Fatal(err)
	}
	if got, want := perms(), "customers.edit emails.manage invoices.edit invoices.status invoices.view settings.edit"; got != want {
		t.Fatalf("down:\n got %s\nwant %s", got, want)
	}
}
