//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/migrations"
)

// TestSystemSettingsStore (M23): migration 00021, the single System Settings row with its
// version check, a session's last use, the table left out of backups, and down/up.
func TestSystemSettingsStore(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	repo := e.store.Repos().SystemSettings
	if _, err := repo.Get(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("before first save: %v", err)
	}
	st := domain.SystemSettings{SessionIdle: 30 * time.Minute, SessionMax: 12 * time.Hour, BackupHostDir: "/Volumes/NAS/backups"}
	if err := repo.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	got := must(repo.Get(ctx))
	if got.SessionIdle != st.SessionIdle || got.SessionMax != st.SessionMax || got.BackupHostDir != st.BackupHostDir || got.Version != 1 {
		t.Fatalf("saved %+v", got)
	}
	if err := repo.Save(ctx, st); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	got.InvoicesHostDir = "./data/archive"
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}

	must(e.svc.CreateUser(e.ctx, "albie@example.com", "correct horse battery"))
	tok, _, err := e.svc.Login(e.ctx, "albie@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(10 * time.Minute)
	if _, err := e.svc.Authenticate(e.ctx, tok); err != nil {
		t.Fatal(err)
	}
	var seen time.Time
	if err := testDB.QueryRowContext(ctx, `SELECT last_seen_at FROM sessions`).Scan(&seen); err != nil || !seen.Equal(e.clock.Now().UTC().Truncate(time.Microsecond)) {
		t.Fatalf("last seen %v (%v)", seen, err)
	}
	e.clock.Advance(31 * time.Minute)
	if _, err := e.svc.Authenticate(e.ctx, tok); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("idle session: %v", err)
	}

	if err := mariadb.NewSnapshotter(testDB).Snapshot(ctx, func(_ context.Context, src ports.BackupSource) error {
		for _, tb := range src.Tables() {
			if tb.Name == "system_settings" {
				t.Error("system_settings is backed up")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectMySQL, testDB, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
}
