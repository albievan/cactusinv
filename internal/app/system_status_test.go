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

// Test doubles for the status probes (M57).

type fakeDB struct{ err error }

func (f fakeDB) Inspect(context.Context) (ports.DatabaseStatus, error) {
	return ports.DatabaseStatus{
		Version: "11.4.2-MariaDB", Uptime: 3 * time.Hour, Ping: 2 * time.Millisecond, Connections: 3, MaxConnections: 151,
		Variables: []ports.Setting{{Name: "max_connections", Value: "151"}}, Pool: ports.DBPool{Open: 2, InUse: 1},
	}, f.err
}

type fakeHost struct {
	start   time.Time
	down    map[string]bool
	free    uint64
	reached []string
}

func (f *fakeHost) Runtime() ports.RuntimeStats {
	return ports.RuntimeStats{Goroutines: 12, HeapBytes: 8 << 20, Hostname: "app-1", StartedAt: f.start}
}

func (f *fakeHost) Disk(string) (ports.DiskUsage, error) {
	return ports.DiskUsage{FreeBytes: f.free, TotalBytes: 100 << 30}, nil
}

func (f *fakeHost) Reach(_ context.Context, host string, _ int) (time.Duration, error) {
	f.reached = append(f.reached, host)
	if f.down[host] {
		return 0, errors.New("refused")
	}
	return time.Millisecond, nil
}

type fakeRequests struct{}

func (fakeRequests) RequestStats() ports.RequestStats {
	return ports.RequestStats{Total: 40, ServerErrors: 1, Average: 5 * time.Millisecond, P95: 20 * time.Millisecond}
}

type fakeSecrets map[string]string

func (f fakeSecrets) Get(_ context.Context, key string) (string, error) {
	if v, ok := f[key]; ok {
		return v, nil
	}
	return "", domain.ErrNotFound
}

func systemHarness(t *testing.T, db ports.DatabaseInspector, host *fakeHost) *harness {
	h := newHarness(t)
	h.svc = app.New(app.Deps{
		Tx: h.store, Clock: h.clock, IDs: h.ids, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Version: "1.34.0",
		Deployment: app.Deployment{Target: "k3s", BackupDir: "/data/backups", InvoicesDir: "/data/invoices"},
		System: app.SystemDeps{
			Database: db, Host: host, Requests: fakeRequests{}, Build: "61", Container: true,
			Secrets:    fakeSecrets{"db_password": "s3cret-db", "backup_key": "AGE-SECRET-KEY-1X"},
			SecretKeys: []app.SecretInfo{{Key: "db_password", Description: "database"}, {Key: "backup_key", Description: "backups"}, {Key: "smtp_password", Description: "mail"}},
			DBHost:     "mariadb", DBPort: 3306, DBName: "cactusinv", DBUser: "cactusinv",
			MailHost: "smtp.example.com", MailPort: 587, MailpitHost: "mailpit", MailpitPort: 1025,
			Configuration: []ports.Setting{{Name: "CACTUSINV_HTTP_ADDR", Value: ":8080"}},
		},
	})
	return h
}

func TestSystemStatus(t *testing.T) {
	host := &fakeHost{start: start.Add(-90 * time.Minute), down: map[string]bool{"mailpit": true}, free: 50 << 30}
	h := systemHarness(t, fakeDB{}, host)
	st := must(h.svc.SystemStatus(h.ctx))
	if st.Target != "k3s" || st.Build != "61" || st.Uptime != 90*time.Minute || st.Database.Version != "11.4.2-MariaDB" || st.DatabaseError != "" || st.DBHost != "mariadb" {
		t.Fatalf("status = %+v", st)
	}
	names := map[string]app.ServiceStatus{}
	for _, s := range st.Services {
		names[s.Name] = s
	}
	if names["cactusInv"].State != app.StateRunning || !strings.Contains(names["cactusInv"].Detail, "app-1") ||
		names["MariaDB"].State != app.StateRunning || names["E-mail server"].State != app.StateRunning ||
		names["E-mail server"].Detail != "smtp.example.com:587 (set by the deployment): answers" || names["Mailpit"].State != app.StateUnreachable {
		t.Fatalf("services = %+v", st.Services)
	}
	if len(st.Secrets) != 3 || !st.Secrets[0].Present || st.Secrets[2].Present || len(st.Configuration) != 1 {
		t.Fatalf("secrets %+v", st.Secrets)
	}
	// A database that does not answer is reported, not an error.
	h = systemHarness(t, fakeDB{err: errors.New("dial tcp: refused")}, host)
	st = must(h.svc.SystemStatus(h.ctx))
	if st.DatabaseError == "" || st.Services[1].State != app.StateUnreachable || strings.Contains(st.Services[1].Detail, "refused") {
		t.Fatalf("down = %+v", st.Services[1])
	}
}

func TestRevealSecret(t *testing.T) {
	h := systemHarness(t, fakeDB{}, &fakeHost{})
	ctx, _, _ := h.users()
	if _, err := h.svc.RevealSecret(ctx, "db_password", "wrong password"); fieldOf(err) != "currentPassword" || h.auditCount("user.reauth_failed") != 1 {
		t.Fatalf("wrong password: %v", err)
	}
	v := must(h.svc.RevealSecret(ctx, "db_password", adminPW))
	if v != "s3cret-db" {
		t.Fatalf("value %q", v)
	}
	if a := h.audited("secret.viewed"); a.EntityID != "db_password" || strings.Contains(a.Summary, "s3cret") {
		t.Fatalf("audit %+v", a)
	}
	if _, err := h.svc.RevealSecret(ctx, "../etc/passwd", adminPW); fieldOf(err) != "key" {
		t.Fatalf("unknown key: %v", err)
	}
	if _, err := h.svc.RevealSecret(ctx, "smtp_password", adminPW); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing secret: %v", err)
	}
}

func TestSystemHealth(t *testing.T) {
	host := &fakeHost{start: start.Add(-time.Hour), free: 50 << 30}
	h := systemHarness(t, fakeDB{}, host)
	hl := must(h.svc.SystemHealth(h.ctx))
	checks := map[string]app.HealthCheck{}
	for _, c := range hl.Checks {
		checks[c.Name] = c
	}
	if checks["Database"].State != app.HealthOK || checks["E-mail server"].State != app.HealthOK || checks["Outbox"].State != app.HealthOK ||
		checks["Automatic sending"].State != app.HealthOK || checks["Disk: backups"].State != app.HealthOK {
		t.Fatalf("checks = %+v", hl.Checks)
	}
	// No backups configured here: a warning, so the whole is a warning.
	if checks["Backups"].State != app.HealthWarning || hl.State != app.HealthWarning {
		t.Fatalf("backups %+v, state %s", checks["Backups"], hl.State)
	}
	if hl.Requests.Total != 40 || hl.Uptime != time.Hour || hl.DBPing != 2*time.Millisecond || hl.Runtime.Goroutines != 12 {
		t.Fatalf("performance = %+v", hl)
	}
	// The database down fails the whole; low disk warns.
	host.free = 1 << 30
	h = systemHarness(t, fakeDB{err: errors.New("down")}, host)
	hl = must(h.svc.SystemHealth(h.ctx))
	if hl.State != app.HealthFailed || hl.Checks[0].State != app.HealthFailed {
		t.Fatalf("down = %+v", hl)
	}
	for _, c := range hl.Checks {
		if c.Name == "Disk: invoice PDFs" && c.State != app.HealthWarning {
			t.Fatalf("low disk %+v", c)
		}
	}
}
