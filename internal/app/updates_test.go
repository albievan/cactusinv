package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
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
