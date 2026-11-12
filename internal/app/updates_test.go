package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// TestCheckForUpdate (M27): newer, same and older published versions; a development build,
// the check switched off, an unreachable or unreadable source — none of them an error.
func TestCheckForUpdate(t *testing.T) {
	svc := func(version string, rel ports.ReleaseSource) *app.Service {
		return app.New(app.Deps{Tx: fakes.NewStore(), Clock: &fakes.Clock{T: time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)}, IDs: &fakes.IDs{}, Version: version, Releases: rel})
	}
	ctx := context.Background()
	for _, c := range []struct {
		published string
		available bool
	}{{"1.13.0", true}, {"v1.12.2\n", true}, {"2.0.0", true}, {"1.12.1", false}, {"1.9.9", false}} {
		got := svc("1.12.1", &fakes.Releases{Version: c.published}).CheckForUpdate(ctx)
		if got.Available != c.available || got.Current != "1.12.1" || got.Latest != strings.TrimPrefix(strings.TrimSpace(c.published), "v") || got.Problem != "" {
			t.Errorf("published %q: %+v", c.published, got)
		}
	}

	rel := &fakes.Releases{Version: "1.13.0"}
	if got := svc("dev", rel).CheckForUpdate(ctx); got.Available || !strings.Contains(got.Problem, "development build") || rel.Calls != 0 {
		t.Errorf("dev build: %+v (%d calls)", got, rel.Calls)
	}
	var off ports.ReleaseSource
	if got := svc("1.12.1", off).CheckForUpdate(ctx); got.Available || !strings.Contains(got.Problem, "switched off") {
		t.Errorf("off: %+v", got)
	}
	if got := svc("1.12.1", &fakes.Releases{Err: errors.New("dial tcp: no route")}).CheckForUpdate(ctx); got.Available || !strings.Contains(got.Problem, "could not be read") || got.Latest != "" {
		t.Errorf("unreachable: %+v", got)
	}
	if got := svc("1.12.1", &fakes.Releases{Version: "<html>404</html>"}).CheckForUpdate(ctx); got.Available || !strings.Contains(got.Problem, "could not be understood") {
		t.Errorf("garbage: %+v", got)
	}
}

// TestInstallUpdate (M31): a standalone binary installs the newest release and restarts; a
// container, the newest version and a failed download are refused or reported, and the
// running version stays.
func TestInstallUpdate(t *testing.T) {
	store := fakes.NewStore()
	newSvc := func(up *fakes.Upgrader, published string) *app.Service {
		return app.New(app.Deps{
			Tx: store, Clock: &fakes.Clock{T: time.Date(2026, 11, 12, 6, 0, 0, 0, time.UTC)}, IDs: &fakes.IDs{},
			Version: "1.15.0", Releases: &fakes.Releases{Version: published}, Upgrader: up,
		})
	}
	ctx := app.WithActor(context.Background(), app.Actor{ID: "u1", Name: "admin"})
	waitFor := func(svc *app.Service, state string) app.UpgradeJob {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if j := svc.UpgradeStatus(); j.State == state {
				return j
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("state %q never reached: %+v", state, svc.UpgradeStatus())
		return app.UpgradeJob{}
	}

	up := &fakes.Upgrader{}
	svc := newSvc(up, "1.16.0")
	if c := svc.CheckForUpdate(ctx); !c.Available || !c.CanInstall {
		t.Fatalf("check = %+v", c)
	}
	restarted := make(chan struct{})
	svc.SetRestart(func() { close(restarted) })
	job, err := svc.InstallUpdate(ctx)
	if err != nil || job.From != "1.15.0" || job.To != "1.16.0" {
		t.Fatalf("start = %+v, %v", job, err)
	}
	<-restarted
	if j := waitFor(svc, app.UpgradeRestarting); j.Done != 10 || j.Total != 10 || j.Error != "" || len(up.Installed) != 1 || up.Installed[0] != "1.16.0" {
		t.Fatalf("after install: %+v, installed %v", j, up.Installed)
	}
	if _, err := svc.InstallUpdate(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a second install while restarting: %v", err)
	}
	if !auditHas(t, store, "system.update_installed", "installed 1.16.0 in place of 1.15.0") {
		t.Fatal("install not audited")
	}

	container := newSvc(&fakes.Upgrader{Problem: "This cactusInv runs in a container."}, "1.16.0")
	if c := container.CheckForUpdate(ctx); c.CanInstall || c.InstallProblem != "This cactusInv runs in a container." {
		t.Fatalf("container check = %+v", c)
	}
	if _, err := container.InstallUpdate(ctx); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "container") {
		t.Fatalf("container install: %v", err)
	}
	if _, err := newSvc(&fakes.Upgrader{}, "1.15.0").InstallUpdate(ctx); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "newest version") {
		t.Fatalf("already newest: %v", err)
	}

	broken := newSvc(&fakes.Upgrader{Err: errors.New("SHA-256 does not match")}, "1.16.0")
	if _, err := broken.InstallUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	if j := waitFor(broken, app.UpgradeFailed); !strings.Contains(j.Error, "SHA-256 does not match") {
		t.Fatalf("failed job = %+v", j)
	}
	if !auditHas(t, store, "system.update_failed", "1.15.0 keeps running") {
		t.Fatal("failure not audited")
	}
}

func auditHas(t *testing.T, store *fakes.Store, action, summary string) bool {
	t.Helper()
	var entries []domain.AuditEntry
	_ = store.WithinTx(context.Background(), func(ctx context.Context, r ports.Repos) error {
		var err error
		entries, _, err = r.Audit.List(ctx, ports.AuditFilter{Page: ports.Page{Limit: 100}})
		return err
	})
	for _, e := range entries {
		if e.Action == action && strings.Contains(e.Summary, summary) {
			return true
		}
	}
	return false
}
