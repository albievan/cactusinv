//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/idgen"
	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// TestUserRepository exercises every user and session method added for user management
// (ADR-011) against MariaDB.
func TestUserRepository(t *testing.T) {
	store := reset(t)
	ctx := context.Background()
	r := store.Repos()
	at := time.Date(2026, 10, 15, 9, 30, 0, 123456000, time.UTC)
	a := domain.User{ID: "u-a", Email: "Albie@Example.com", PasswordHash: "h1", CreatedAt: at, MustChangePassword: true}
	b := domain.User{ID: "u-b", Email: "admin", PasswordHash: "h2", CreatedAt: at}
	for _, u := range []domain.User{a, b} {
		if err := r.Users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Users.Create(ctx, domain.User{ID: "u-c", Email: "albie@example.COM", PasswordHash: "h", CreatedAt: at}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a login in another case is a conflict: %v", err)
	}
	got := must(r.Users.GetByEmail(ctx, "albie@example.com"))
	if got.ID != "u-a" || !got.MustChangePassword || !got.Active() || !got.LastLoginAt.IsZero() || !got.CreatedAt.Equal(at) {
		t.Fatalf("user = %+v", got)
	}
	list := must(r.Users.List(ctx))
	if len(list) != 2 || list[0].Email != "admin" || list[1].Email != "Albie@Example.com" {
		t.Fatalf("list = %+v", list)
	}
	if ids := must(r.Users.LockActive(ctx)); !slices.Equal(ids, []string{"u-a", "u-b"}) {
		t.Fatalf("active = %v", ids)
	}
	if err := r.Users.SetDisabled(ctx, "u-b", at); err != nil {
		t.Fatal(err)
	}
	if got := must(r.Users.Get(ctx, "u-b")); !got.DisabledAt.Equal(at) || got.Active() {
		t.Fatalf("disabled = %+v", got)
	}
	if ids := must(r.Users.LockActive(ctx)); !slices.Equal(ids, []string{"u-a"}) {
		t.Fatalf("active after disable = %v", ids)
	}
	if err := r.Users.SetDisabled(ctx, "u-b", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !must(r.Users.Get(ctx, "u-b")).Active() {
		t.Fatal("enabled again")
	}
	if err := r.Users.TouchLogin(ctx, "u-a", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := must(r.Users.Get(ctx, "u-a")); !got.LastLoginAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("last login = %v", got.LastLoginAt)
	}
	if err := r.Users.UpdatePassword(ctx, "u-a", "h3", false); err != nil {
		t.Fatal(err)
	}
	if got := must(r.Users.Get(ctx, "u-a")); got.PasswordHash != "h3" || got.MustChangePassword {
		t.Fatalf("password = %+v", got)
	}
	if err := r.Users.UpdatePassword(ctx, "u-a", "h3", true); err != nil { // same hash: still found
		t.Fatal(err)
	}
	if !must(r.Users.Get(ctx, "u-a")).MustChangePassword {
		t.Fatal("must-change set")
	}
	for name, err := range map[string]error{
		"password": r.Users.UpdatePassword(ctx, "nobody", "h", false),
		"disable":  r.Users.SetDisabled(ctx, "nobody", at),
		"touch":    r.Users.TouchLogin(ctx, "nobody", at),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s of a missing user: %v", name, err)
		}
	}
	for _, s := range []domain.Session{
		{TokenHash: "t-a1", UserID: "u-a", CreatedAt: at, ExpiresAt: at.Add(time.Hour)},
		{TokenHash: "t-a2", UserID: "u-a", CreatedAt: at, ExpiresAt: at.Add(time.Hour)},
		{TokenHash: "t-a3", UserID: "u-a", CreatedAt: at, ExpiresAt: at.Add(time.Hour)},
		{TokenHash: "t-b1", UserID: "u-b", CreatedAt: at, ExpiresAt: at.Add(time.Hour)},
	} {
		if err := r.Sessions.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Sessions.DeleteForUser(ctx, "u-a", "t-a2"); err != nil {
		t.Fatal(err)
	}
	alive := func(h string) bool { _, err := r.Sessions.Get(ctx, h); return err == nil }
	if alive("t-a1") || !alive("t-a2") || alive("t-a3") || !alive("t-b1") {
		t.Fatal("DeleteForUser keeps the excepted session and other users' sessions")
	}
	if err := r.Sessions.DeleteForUser(ctx, "u-a", ""); err != nil {
		t.Fatal(err)
	}
	if alive("t-a2") || !alive("t-b1") {
		t.Fatal("DeleteForUser without an exception ends every session of the user")
	}
}

func userEnv(t *testing.T) (*mariadb.Store, *app.Service, domain.User, domain.User) {
	t.Helper()
	store := reset(t)
	svc := app.New(app.Deps{Tx: store, Clock: &fakes.Clock{T: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}, IDs: idgen.UUIDv7{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	ctx := context.Background()
	a := must(svc.CreateUser(ctx, "a@example.com", "password of a long"))
	b := must(svc.CreateUser(ctx, "b@example.com", "password of b long"))
	return store, svc, a, b
}

func as(u domain.User) context.Context {
	return app.WithActor(context.Background(), app.Actor{ID: u.ID, Name: u.Email})
}

// TestDisablingWaitsForTheActiveUsersLock holds the active-users lock in one transaction
// while the other administrator disables the first: the second request waits, sees the
// committed result, and is refused — the last active user is never disabled.
func TestDisablingWaitsForTheActiveUsersLock(t *testing.T) {
	store, svc, a, b := userEnv(t)
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.WithinTx(as(a), func(ctx context.Context, r ports.Repos) error {
			if _, err := r.Users.LockActive(ctx); err != nil {
				return err
			}
			close(locked)
			<-release
			return r.Users.SetDisabled(ctx, b.ID, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
		})
	}()
	<-locked
	second := make(chan error, 1)
	go func() {
		_, err := svc.DisableUser(as(b), a.ID, "password of b long")
		second <- err
	}()
	select {
	case err := <-second:
		t.Fatalf("the second disable did not wait for the lock: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-second; !errors.Is(err, domain.ErrLastActiveUser) {
		t.Fatalf("second disable = %v, want ErrLastActiveUser", err)
	}
	if !must(store.Repos().Users.Get(context.Background(), a.ID)).Active() {
		t.Fatal("a stays active")
	}
}

// TestConcurrentDisablesOfEachOther races two administrators disabling each other:
// exactly one succeeds, every time.
func TestConcurrentDisablesOfEachOther(t *testing.T) {
	for i := range 5 {
		store, svc, a, b := userEnv(t)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		for j, pair := range [][2]domain.User{{a, b}, {b, a}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				pw := map[string]string{a.ID: "password of a long", b.ID: "password of b long"}[pair[0].ID]
				_, errs[j] = svc.DisableUser(as(pair[0]), pair[1].ID, pw)
			}()
		}
		close(start)
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if !errors.Is(err, domain.ErrLastActiveUser) && !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("round %d: unexpected error %v", i, err)
			}
		}
		active := must(store.Repos().Users.LockActive(context.Background()))
		if ok != 1 || len(active) != 1 {
			t.Fatalf("round %d: %d succeeded, %d active (%v)", i, ok, len(active), errs)
		}
	}
}

// TestUpgradeFlagsTheDefaultAdministrator migrates a v1.5.0 database (schema 14) to head:
// the default administrator — a login that is not an e-mail address, or the user the audit
// trail records as the default administrator — must change its password; a CLI-created
// user need not.
func TestUpgradeFlagsTheDefaultAdministrator(t *testing.T) {
	ctx := context.Background()
	if err := mariadb.Migrate(ctx, testDB, true); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.MigrateTo(ctx, testDB, 14); err != nil {
		t.Fatal(err)
	}
	for _, u := range [][2]string{{"u-admin", "admin"}, {"u-albie", "albie@example.com"}, {"u-boss", "boss@example.com"}} {
		if _, err := testDB.ExecContext(ctx, "INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, 'hash:x', ?)", u[0], u[1], time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	// boss@example.com was the default administrator (CACTUSINV_DEFAULT_ADMIN_USER), as the
	// audit trail records; albie@example.com was made with `cactusinv admin create`.
	for i, a := range [][3]string{{"u-boss", "default administrator boss@example.com", "h1"}, {"u-albie", "albie@example.com", "h2"}} {
		if _, err := testDB.ExecContext(ctx, `INSERT INTO audit_log (seq, at, actor_id, actor_name, action, entity_type, entity_id, summary, prev_hash, hash)
			VALUES (?, ?, 'system', 'System', 'user.created', 'user', ?, ?, '', ?)`, i+1, time.Now().UTC(), a[0], a[1], a[2]); err != nil {
			t.Fatal(err)
		}
	}
	if err := mariadb.Migrate(ctx, testDB, false); err != nil {
		t.Fatal(err)
	}
	r := mariadb.NewStore(testDB).Repos()
	if u := must(r.Users.Get(ctx, "u-admin")); !u.MustChangePassword || !u.Active() {
		t.Fatalf("default admin = %+v", u)
	}
	if u := must(r.Users.Get(ctx, "u-albie")); u.MustChangePassword || !u.Active() {
		t.Fatalf("e-mail login = %+v", u)
	}
	if u := must(r.Users.Get(ctx, "u-boss")); !u.MustChangePassword {
		t.Fatalf("default administrator with an e-mail login = %+v", u)
	}
}
