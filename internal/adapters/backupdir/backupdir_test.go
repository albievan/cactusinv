package backupdir

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

var ctx = context.Background()

func name(kind domain.BackupKind, sec int) string {
	return domain.BackupName{CreatedAt: time.Date(2026, 10, 15, 2, 0, sec, 0, time.UTC), Schema: 13, Kind: kind}.String()
}

func newDir(t *testing.T) (*Dir, string) {
	t.Helper()
	path := t.TempDir()
	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	return d, path
}

func TestCreateOpenListRemove(t *testing.T) {
	d, path := newDir(t)
	n := name(domain.BackupManual, 0)
	w, err := d.Create(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "ciphertext"); err != nil {
		t.Fatal(err)
	}
	if list, _ := d.List(ctx); len(list) != 0 {
		t.Fatal("invisible until committed")
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := w.Abort(); err != nil {
		t.Fatal("abort after commit does nothing")
	}
	st, err := os.Stat(filepath.Join(path, n))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	r, err := d.Open(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "ciphertext" {
		t.Fatal(string(b))
	}
	// Unknown files, leftovers and directories are ignored; legacy dumps are listed.
	for _, f := range []string{"notes.txt", "." + n + ".abc.part", "cactusinv-20261001-020000.sql.gz"} {
		if err := os.WriteFile(filepath.Join(path, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Mkdir(filepath.Join(path, name(domain.BackupScheduled, 9)), 0o700)
	list, err := d.List(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "cactusinv-20261001-020000.sql.gz" || !list[0].Legacy || list[1].Name != n || list[1].Size != 10 {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := d.Remove(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove(ctx, n); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	if err := d.Remove(ctx, "cactusinv-20261001-020000.sql.gz"); err != nil {
		t.Fatal("legacy dumps can be pruned")
	}
	if _, err := d.Open(ctx, n); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("open removed: %v", err)
	}
}

func TestCommitNeverReplaces(t *testing.T) {
	d, path := newDir(t)
	n := name(domain.BackupScheduled, 1)
	w1, _ := d.Create(ctx, n)
	if _, err := d.Create(ctx, n); !errors.Is(err, domain.ErrConflict) { // the name is reserved
		t.Fatalf("second writer of a reserved name: %v", err)
	}
	_, _ = io.WriteString(w1, "first")
	if err := w1.Commit(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(path, n)); string(b) != "first" {
		t.Fatal("replaced")
	}
	if _, err := d.Create(ctx, n); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("create existing: %v", err)
	}
	entries, _ := os.ReadDir(path)
	if len(entries) != 1 {
		t.Fatalf("leftover temporary files: %v", entries)
	}
	if err := w1.Commit(); err == nil {
		t.Fatal("commit twice")
	}
}

func TestAbortLeavesNothing(t *testing.T) {
	d, path := newDir(t)
	w, _ := d.Create(ctx, name(domain.BackupManual, 2))
	_, _ = io.WriteString(w, "partial")
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(path); len(entries) != 0 {
		t.Fatalf("left %v", entries)
	}
}

func TestNamesAreNeverPaths(t *testing.T) {
	d, path := newDir(t)
	outside := filepath.Join(filepath.Dir(path), "outside.age")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	t.Cleanup(func() { _ = os.Remove(outside) })
	for _, bad := range []string{"../outside.age", "/etc/passwd", "..", "", "x/" + name(domain.BackupManual, 3), name(domain.BackupManual, 3) + "/..", "notes.txt", "cactusinv-20261001-020000.sql.gz/../x"} {
		if _, err := d.Create(ctx, bad); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("create %q: %v", bad, err)
		}
		if _, err := d.Open(ctx, bad); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("open %q: %v", bad, err)
		}
		if err := d.Remove(ctx, bad); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("remove %q: %v", bad, err)
		}
	}
	if _, err := d.Create(ctx, "cactusinv-20261001-020000.sql.gz"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("legacy dumps are never created")
	}
	// A valid name that is a symlink pointing outside is not followed.
	if runtime.GOOS != "windows" {
		link := name(domain.BackupUpload, 4)
		if err := os.Symlink(outside, filepath.Join(path, link)); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Open(ctx, link); err == nil {
			t.Fatal("followed a symlink out of the directory")
		}
		if list, _ := d.List(ctx); len(list) != 0 {
			t.Fatalf("symlink listed: %+v", list)
		}
	}
}

func TestNew(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory")
	}
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, nil, 0o600)
	if _, err := New(f); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file: %v", err)
	}
	d, path := newDir(t)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := d.List(ctx); err == nil {
		t.Fatal("list of a vanished directory")
	}
	if _, err := d.Create(ctx, name(domain.BackupManual, 5)); err == nil {
		t.Fatal("create in a vanished directory")
	}
}

func TestListRemovesStaleParts(t *testing.T) {
	d, path := newDir(t)
	n := name(domain.BackupManual, 6)
	stale := filepath.Join(path, "."+n+".0011223344556677.part")
	fresh := filepath.Join(path, "."+n+".8899aabbccddeeff.part")
	other := filepath.Join(path, ".notours.part")
	for _, f := range []string{stale, fresh, other} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-StalePartAge - time.Hour)
	for _, f := range []string{stale, other} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale .part removed")
	}
	for _, f := range []string{fresh, other} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s must stay: %v", f, err)
		}
	}
}

func TestReservationsAreReleased(t *testing.T) {
	d, path := newDir(t)
	n := name(domain.BackupUpload, 7)
	w, _ := d.Create(ctx, n)
	_ = w.Abort()
	w, err := d.Create(ctx, n) // free again after Abort
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Commit()
	entries, _ := os.ReadDir(path)
	if len(entries) != 1 || entries[0].Name() != n {
		t.Fatalf("left %v", entries)
	}
	// A reservation left by a crash goes with the stale parts.
	stale := filepath.Join(path, "."+name(domain.BackupUpload, 8)+".reserved")
	_ = os.WriteFile(stale, nil, 0o600)
	old := time.Now().Add(-StalePartAge - time.Hour)
	_ = os.Chtimes(stale, old, old)
	_, _ = d.List(ctx)
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale reservation kept")
	}
}
