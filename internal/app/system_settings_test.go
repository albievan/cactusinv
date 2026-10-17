package app_test

import (
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

// System Settings (M23, Albie 2026-10-17): the session timeouts and the host folders mapped
// to the container's backup and invoice folders.

func TestSessionTimeoutsApplyToEverySession(t *testing.T) {
	h := newHarness(t)
	must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
	tok := h.login("albie@example.com", "correct horse battery")
	if v := must(h.svc.GetSystemSettings(h.ctx)); v.Settings.SessionMax != 7*24*time.Hour || v.Settings.SessionIdle != 0 || v.Settings.Version != 0 {
		t.Fatalf("defaults: %+v", v.Settings)
	}
	// Before any idle timeout, a session survives a long pause.
	h.clock.Advance(3 * time.Hour)
	if !h.signedIn(tok) {
		t.Fatal("no idle timeout yet")
	}
	st := domain.SystemSettings{SessionIdle: 30 * time.Minute, SessionMax: 8 * time.Hour}
	must(h.svc.UpdateSystemSettings(h.ctx, st))
	// Active use keeps it alive; the touch is recorded at most once a minute.
	for range 10 {
		h.clock.Advance(20 * time.Minute)
		if !h.signedIn(tok) {
			t.Fatalf("active session signed out at %v", h.clock.Now())
		}
	}
	// The maximum (8 hours from the start, 3 h + 200 min ago) ends it even while active.
	h.clock.Advance(2 * time.Hour)
	if h.signedIn(tok) {
		t.Fatal("past the maximum")
	}
	if h.signedIn(tok) {
		t.Fatal("an ended session was deleted")
	}

	// A new sign-in lasts the new maximum, and ends after 30 idle minutes.
	tok = h.login("albie@example.com", "correct horse battery")
	_, exp, _ := h.svc.Login(h.ctx, "albie@example.com", "correct horse battery")
	if !exp.Equal(h.clock.Now().UTC().Truncate(time.Microsecond).Add(8 * time.Hour)) {
		t.Fatalf("expires %v", exp)
	}
	h.clock.Advance(31 * time.Minute)
	if h.signedIn(tok) {
		t.Fatal("idle session not signed out")
	}
	if _, n := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Action: "settings.system_updated"})); n != 1 {
		t.Fatalf("audited %d times", n)
	}
}

func TestSystemSettingsFolders(t *testing.T) {
	h := newHarness(t)
	h.svc = app.New(app.Deps{
		Tx: h.store, Clock: h.clock, IDs: h.ids, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{},
		Deployment: app.Deployment{
			Target: "dockerdesktop", ProjectDir: "/Users/albie/projects/cactusInv", BackupDir: "/data/backups", InvoicesDir: "/data/invoices",
			BackupHostDir: "/Users/albie/projects/cactusInv/data/backups", InvoicesHostDir: "/Users/albie/projects/cactusInv/data/invoices",
		},
	})
	v := must(h.svc.GetSystemSettings(h.ctx))
	if v.PendingFolders() || len(must(h.svc.FolderMappings(h.ctx))) != 0 {
		t.Fatalf("nothing saved: %+v", v)
	}
	// The folder already mounted, written relative to the project folder: nothing pending.
	st := v.Settings
	st.BackupHostDir = "./data/backups/"
	v = must(h.svc.UpdateSystemSettings(h.ctx, st))
	if v.Settings.BackupHostDir != "./data/backups" || v.PendingFolders() {
		t.Fatalf("same folder: %+v pending=%v", v.Settings, v.PendingFolders())
	}
	st = v.Settings
	st.InvoicesHostDir = "/Volumes/NAS/invoices"
	v = must(h.svc.UpdateSystemSettings(h.ctx, st))
	if !v.PendingFolders() {
		t.Fatal("a new folder waits for the next deploy")
	}
	if m := must(h.svc.FolderMappings(h.ctx)); len(m) != 2 || m["INVOICES_DIR"] != "/Volumes/NAS/invoices" || m["BACKUP_DIR"] != "./data/backups" {
		t.Fatalf("mappings %v", m)
	}
	// A stale version is refused; a bad folder names its field.
	if _, err := h.svc.UpdateSystemSettings(h.ctx, st); err == nil {
		t.Fatal("stale version accepted")
	}
	st = v.Settings
	st.BackupHostDir = "backups"
	if _, err := h.svc.UpdateSystemSettings(h.ctx, st); fieldOf(err) != "backupHostDir" {
		t.Fatalf("relative folder: %v", err)
	}
	// On Kubernetes nothing is mapped, so nothing is pending.
	k := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: h.ids, Hasher: fakes.Hasher{}, Deployment: app.Deployment{Target: "k3s"}})
	if must(k.GetSystemSettings(h.ctx)).PendingFolders() {
		t.Fatal("pending on k3s")
	}
}
