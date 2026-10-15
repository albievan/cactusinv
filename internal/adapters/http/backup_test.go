package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

const password = "correct horse battery"

func newBackupServer(t *testing.T, maxUpload int64) (*client, *fakes.Store) {
	t.Helper()
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}
	files := &fakes.Files{Clock: clock}
	svc := app.New(app.Deps{
		Tx: store, Clock: clock, IDs: &fakes.IDs{}, Mailer: &fakes.Mailer{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{},
		Backups: app.BackupDeps{
			Snapshotter: &fakes.Snapshotter{Store: store}, Codec: fakes.Codec{}, Sealer: fakes.Sealer{Key: true},
			Files: files, Stager: &fakes.Stager{Store: store},
		},
	})
	if _, err := svc.CreateUser(context.Background(), "albie@example.com", password); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{Service: svc, Clock: clock, MaxUploadBytes: maxUpload}))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, srv: srv, http: &http.Client{Jar: jar}}
	c.login()
	return c, store
}

// raw sends body as application/octet-stream and returns the response.
func (c *client) raw(method, path, contentType string, body io.Reader, headers ...string) *http.Response {
	c.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), method, c.srv.URL+path, body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Requested-With", "cactusinv")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp
}

func TestBackupRoutes(t *testing.T) {
	c, store := newBackupServer(t, 4<<20)
	// Make the backup larger than the 1 MiB body/capture limit of ordinary routes.
	for i := range 1500 {
		if err := store.Repos().Audit.Append(context.Background(), &domain.AuditEntry{
			At: time.Date(2026, 8, 21, 0, 0, i%60, 0, time.UTC), ActorName: "seed", Action: "x.y", EntityType: "x", Summary: strings.Repeat("s", 900),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var ov httpapi.BackupOverview
	c.ok(c.do("GET", "/api/v1/backups", nil, &ov), 200, "list")
	if !ov.KeyConfigured || ov.MaxUploadBytes != 4<<20 || ov.NextRun != nil || len(ov.Files) != 0 {
		t.Fatalf("overview = %+v", ov)
	}
	var created httpapi.CreatedBackup
	c.ok(c.do("POST", "/api/v1/backups", nil, &created, "Idempotency-Key", "b1"), 201, "create")
	if _, err := domain.ParseBackupName(created.Name); err != nil || len(created.Sha256) != 64 {
		t.Fatalf("created = %+v", created)
	}

	var p httpapi.Problem
	c.ok(c.do("POST", "/api/v1/backups/export", map[string]string{"passphrase": "short"}, &p), 422, "short passphrase")
	for range 2 { // the same Idempotency-Key twice: never replayed, never buffered
		resp := c.raw("POST", "/api/v1/backups/export", "application/json", strings.NewReader(`{"passphrase":"a long enough passphrase"}`), "Idempotency-Key", "e1")
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Idempotent-Replayed") != "" || resp.Header.Get("Content-Type") != "application/octet-stream" ||
			!strings.HasPrefix(resp.Header.Get("Content-Disposition"), `attachment; filename="cactusinv-`) {
			t.Fatalf("export: %d %v", resp.StatusCode, resp.Header)
		}
		if len(body) <= 1<<20 || !bytes.HasPrefix(body, []byte("fake-age scrypt ")) || bytes.Contains(body, []byte("long enough passphrase")) {
			t.Fatalf("export body: %d bytes", len(body))
		}
	}

	resp := c.raw("POST", "/api/v1/backups/uploads", "application/octet-stream", bytes.NewReader(make([]byte, 5<<20)))
	_ = resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("oversize upload: %d", resp.StatusCode)
	}
	resp = c.raw("POST", "/api/v1/backups/uploads", "application/octet-stream", strings.NewReader("not a backup"))
	_ = resp.Body.Close()
	c.ok(resp.StatusCode, 201, "upload")
	c.ok(c.do("GET", "/api/v1/backups", nil, &ov), 200, "list")
	var upload string
	for _, f := range ov.Files {
		if f.Kind == httpapi.Upload {
			upload = f.Name
		}
	}
	c.ok(c.do("POST", "/api/v1/restores", map[string]string{"backup": upload, "passphrase": "a long enough passphrase"}, &p), 422, "invalid upload")
	if p.Type != "https://cactusinv/problems/backup-invalid" {
		t.Fatalf("problem = %+v", p)
	}

	var prev httpapi.RestorePreview
	c.ok(c.do("POST", "/api/v1/restores", map[string]string{"backup": created.Name}, &prev), 200, "stage")
	if prev.Id == "" || prev.Source != created.Name || len(prev.Tables) == 0 || len(prev.Warnings) == 0 {
		t.Fatalf("preview = %+v", prev)
	}
	c.ok(c.do("GET", "/api/v1/restores/current", nil, &prev), 200, "current")
	c.ok(c.do("POST", "/api/v1/restores/"+prev.Id+"/apply", map[string]string{"currentPassword": "nope", "confirmation": "RESTORE"}, &p), 422, "wrong password")

	resp = c.raw("POST", "/api/v1/restores/"+prev.Id+"/apply", "application/json", strings.NewReader(`{"currentPassword":"`+password+`","confirmation":"RESTORE"}`))
	_ = resp.Body.Close()
	c.ok(resp.StatusCode, 200, "apply")
	cleared := false
	for _, ck := range resp.Cookies() {
		if ck.Name == httpapi.SessionCookie && ck.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("apply clears the session cookie")
	}
	c.ok(c.do("GET", "/api/v1/auth/me", nil, nil), 401, "signed out")

	c.login()
	var me httpapi.Me
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	if !me.AutomationPaused {
		t.Fatal("automation paused after a restore")
	}
	c.ok(c.do("POST", "/api/v1/automation/resume", nil, nil), 204, "resume")
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	if me.AutomationPaused {
		t.Fatal("resumed")
	}
	c.ok(c.do("POST", "/api/v1/restores", map[string]string{"backup": created.Name}, &prev), 200, "stage again")
	c.ok(c.do("DELETE", "/api/v1/restores/"+prev.Id, nil, nil), 204, "discard")
	c.ok(c.do("GET", "/api/v1/restores/current", nil, &p), 404, "nothing staged")
}

func TestBackupRoutesNotConfigured(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login()
	var p httpapi.Problem
	c.ok(c.do("POST", "/api/v1/backups", nil, &p), 503, "no backup key")
	if p.Type != "https://cactusinv/problems/not-configured" {
		t.Fatalf("problem = %+v", p)
	}
	var ov httpapi.BackupOverview
	c.ok(c.do("GET", "/api/v1/backups", nil, &ov), 200, "list")
	if ov.KeyConfigured || ov.RestoreUnavailable == "" || ov.MaxUploadBytes != httpapi.DefaultMaxUploadBytes {
		t.Fatalf("overview = %+v", ov)
	}
}

// failingSource sends more than 1 MiB of rows, then fails (a database connection lost
// mid-export).
type failingSource struct{}

func (failingSource) SchemaVersion() int64 { return 14 }
func (failingSource) Tables() []domain.BackupTable {
	return []domain.BackupTable{{Name: "big", Columns: []domain.BackupColumn{{Name: "x", Kind: domain.ColumnText}}}}
}

func (failingSource) ScanTable(_ context.Context, _ string, fn func([]any) error) error {
	row := strings.Repeat("x", 1024)
	for range 3000 {
		if err := fn([]any{row}); err != nil {
			return err
		}
	}
	return errors.New("connection lost")
}

type failingSnapshotter struct{}

func (failingSnapshotter) Snapshot(ctx context.Context, fn func(context.Context, ports.BackupSource) error) error {
	return fn(ctx, failingSource{})
}

// TestExportFailingMidStreamBreaksTheDownload: once the 200 is sent, a failure must not end
// as a clean response — the connection is dropped and the client's read fails.
func TestExportFailingMidStreamBreaksTheDownload(t *testing.T) {
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}
	svc := app.New(app.Deps{Tx: store, Clock: clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}, Backups: app.BackupDeps{
		Snapshotter: failingSnapshotter{}, Codec: fakes.Codec{}, Sealer: fakes.Sealer{},
	}})
	if _, err := svc.CreateUser(context.Background(), "albie@example.com", password); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{Service: svc, Clock: clock}))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, srv: srv, http: &http.Client{Jar: jar}}
	c.login()
	resp := c.raw("POST", "/api/v1/backups/export", "application/json", strings.NewReader(`{"passphrase":"a long enough passphrase"}`))
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatalf("a broken export read as complete (%d bytes)", len(body))
	}
	if len(body) < 1<<20 {
		t.Fatalf("the failure came before the stream: %d bytes", len(body))
	}
	if bytes.Contains(body, []byte("problem")) || bytes.Contains(body, []byte(`"status"`)) {
		t.Fatal("no problem JSON may be appended to the stream")
	}
}

// TestLongLimitsOnlyAfterSignIn: an unauthenticated upload is refused before its body is
// read, whatever its size.
func TestLongLimitsOnlyAfterSignIn(t *testing.T) {
	c, _ := newBackupServer(t, 64<<20)
	anon := &client{t: t, srv: c.srv, http: &http.Client{}}
	resp := anon.raw("POST", "/api/v1/backups/uploads", "application/octet-stream", bytes.NewReader(make([]byte, 2<<20)))
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous upload: %d", resp.StatusCode)
	}
}
