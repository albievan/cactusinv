//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/migrations"
)

// TestGroupRepository exercises every access-group method (ADR-012) against MariaDB.
func TestGroupRepository(t *testing.T) {
	store := reset(t)
	ctx := context.Background()
	r := store.Repos()
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{"u-a", "u-b", "u-c"} {
		if err := r.Users.Create(ctx, domain.User{ID: id, Email: id + "@example.com", PasswordHash: "h", CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	gs := must(r.Groups.List(ctx))
	if len(gs) != 1 || !gs[0].IsAdministrators() || !gs[0].Builtin || gs[0].Name != "Administrators" || len(gs[0].MemberIDs) != 0 {
		t.Fatalf("seeded = %+v", gs)
	}
	g := domain.Group{ID: "g-1", Name: "Accounts", Description: "Bills", Permissions: []domain.Permission{domain.PermAuditView, domain.PermInvoicesSend}, MemberIDs: []string{"u-a", "u-b"}, CreatedAt: at, UpdatedAt: at}
	if err := r.Groups.Create(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := r.Groups.Create(ctx, domain.Group{ID: "g-2", Name: "ACCOUNTS", CreatedAt: at, UpdatedAt: at}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a name in another case is a conflict: %v", err)
	}
	if err := r.Groups.Create(ctx, domain.Group{ID: "g-3", Name: "Ghosts", MemberIDs: []string{"u-none"}, CreatedAt: at, UpdatedAt: at}); err == nil {
		t.Fatal("an unknown member is refused")
	}
	got := must(r.Groups.Lock(ctx, "g-1"))
	if got.Name != "Accounts" || !slices.Equal(got.Permissions, g.Permissions) || !slices.Equal(got.MemberIDs, g.MemberIDs) || !got.CreatedAt.Equal(at) {
		t.Fatalf("got = %+v", got)
	}

	g.Name, g.Permissions, g.MemberIDs, g.UpdatedAt = "Billing", []domain.Permission{domain.PermStatementsSend}, []string{"u-c"}, at.Add(time.Hour)
	if err := r.Groups.Update(ctx, g); err != nil {
		t.Fatal(err)
	}
	got = must(r.Groups.Get(ctx, "g-1"))
	if got.Name != "Billing" || !slices.Equal(got.Permissions, g.Permissions) || !slices.Equal(got.MemberIDs, []string{"u-c"}) || !got.UpdatedAt.Equal(g.UpdatedAt) {
		t.Fatalf("updated = %+v", got)
	}
	// The built-in group: only its members change.
	admins := domain.Group{ID: domain.AdministratorsGroupID, Builtin: true, Name: "Renamed", Permissions: []domain.Permission{domain.PermAuditView}, MemberIDs: []string{"u-a", "u-c"}, UpdatedAt: at}
	if err := r.Groups.Update(ctx, admins); err != nil {
		t.Fatal(err)
	}
	if got := must(r.Groups.Get(ctx, domain.AdministratorsGroupID)); got.Name != "Administrators" || len(got.Permissions) != 0 || !slices.Equal(got.MemberIDs, []string{"u-a", "u-c"}) {
		t.Fatalf("administrators = %+v", got)
	}

	// A user's groups, with permissions; Administrators has none stored.
	fu := must(r.Groups.ForUser(ctx, "u-c"))
	if len(fu) != 2 || fu[0].ID != domain.AdministratorsGroupID || !fu[0].Builtin || len(fu[0].Permissions) != 0 || fu[1].ID != "g-1" || !slices.Equal(fu[1].Permissions, g.Permissions) {
		t.Fatalf("for user = %+v", fu)
	}
	if err := r.Groups.SetUserGroups(ctx, "u-b", []string{"g-1", domain.AdministratorsGroupID}); err != nil {
		t.Fatal(err)
	}
	if err := r.Groups.SetUserGroups(ctx, "u-c", nil); err != nil {
		t.Fatal(err)
	}
	if fu := must(r.Groups.ForUser(ctx, "u-c")); len(fu) != 0 {
		t.Fatalf("u-c = %+v", fu)
	}
	if err := r.Groups.SetUserGroups(ctx, "u-c", []string{"g-none"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}

	// Active administrators: a disabled member is not one.
	if err := r.Users.SetDisabled(ctx, "u-b", at); err != nil {
		t.Fatal(err)
	}
	err := store.WithinTx(ctx, func(ctx context.Context, tx ports.Repos) error {
		ids, err := tx.Groups.LockActiveAdministrators(ctx)
		if err == nil && !slices.Equal(ids, []string{"u-a"}) {
			t.Errorf("active administrators = %v", ids)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Groups.Delete(ctx, domain.AdministratorsGroupID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the built-in group is never deleted: %v", err)
	}
	if err := r.Groups.Delete(ctx, "g-1"); err != nil {
		t.Fatal(err)
	}
	if fu := must(r.Groups.ForUser(ctx, "u-b")); len(fu) != 1 || fu[0].ID != domain.AdministratorsGroupID {
		t.Fatalf("memberships go with the group: %+v", fu)
	}
}

// TestAccessGroupsMigrationSeedsOneAdministrator checks 00018 on existing users: only the
// default administrator joins Administrators (Albie's decision), and an installation is
// never left without an active administrator.
func TestAccessGroupsMigrationSeedsOneAdministrator(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		users []struct {
			id, login string
			disabled  bool
		}
		want []string
	}{
		{"default admin only", []struct {
			id, login string
			disabled  bool
		}{{"u-1", "admin", false}, {"u-2", "albie@example.com", false}}, []string{"u-1"}},
		{"no default admin: the oldest active user", []struct {
			id, login string
			disabled  bool
		}{{"u-1", "gone@example.com", true}, {"u-2", "albie@example.com", false}, {"u-3", "carol@example.com", false}}, []string{"u-2"}},
		{"default admin disabled: also the oldest active user", []struct {
			id, login string
			disabled  bool
		}{{"u-1", "admin", true}, {"u-2", "albie@example.com", false}}, []string{"u-1", "u-2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reset(t)
			provider, err := goose.NewProvider(goose.DialectMySQL, testDB, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.DownTo(ctx, 17); err != nil {
				t.Fatal(err)
			}
			for i, u := range tc.users {
				var disabled any
				if u.disabled {
					disabled = at
				}
				if _, err := testDB.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at, disabled_at) VALUES (?, ?, 'h', ?, ?)`,
					u.id, u.login, at.Add(time.Duration(i)*time.Hour), disabled); err != nil {
					t.Fatal(err)
				}
			}
			if err := mariadb.MigrateTo(ctx, testDB, 18); err != nil {
				t.Fatal(err)
			}
			g := must(mariadb.NewStore(testDB).Repos().Groups.Get(ctx, domain.AdministratorsGroupID))
			if !slices.Equal(g.MemberIDs, tc.want) {
				t.Fatalf("administrators = %v, want %v", g.MemberIDs, tc.want)
			}
		})
	}
}
