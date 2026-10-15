// Package backupdir implements ports.BackupFiles on the backups volume (ADR-010). Only
// generated backup names (domain.ParseBackupName) are ever created or opened, and legacy
// sidecar dumps listed and removed; every access goes through an os.Root, so no name can
// reach outside the directory. Files are written as a hidden .part, fsynced, then linked
// into place, mode 0600.
package backupdir

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Dir is the backups directory.
type Dir struct {
	path string
}

// New checks that path is a directory and returns it.
func New(path string) (*Dir, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("backupdir.New: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("backupdir.New: %s is not a directory", path)
	}
	return &Dir{path: path}, nil
}

func checkName(name string, legacyOK bool) error {
	if _, err := domain.ParseBackupName(name); err == nil || (legacyOK && domain.IsLegacyBackupName(name)) {
		return nil
	}
	return &domain.ValidationError{Field: "backup", Reason: "is not a backup name"}
}

func (d *Dir) root() (*os.Root, error) {
	r, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, fmt.Errorf("backupdir: %w", err)
	}
	return r, nil
}

// writer writes one backup through a temporary file.
type writer struct {
	root                *os.Root
	f                   *os.File
	tmp, final, reserve string
	done                bool
}

// Create implements ports.BackupFiles.
func (d *Dir) Create(_ context.Context, name string) (ports.BackupFileWriter, error) {
	if err := checkName(name, false); err != nil {
		return nil, err
	}
	root, err := d.root()
	if err != nil {
		return nil, err
	}
	if _, err := root.Lstat(name); err == nil {
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Create: %w: %s exists", domain.ErrConflict, name)
	}
	// Reserve the name (O_EXCL), so a second writer of the same name fails here, not at
	// Commit — callers that pick names (uploads) can then take the next one.
	reserve := "." + name + reservedSuffix
	r, err := root.OpenFile(reserve, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = root.Close()
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("backupdir.Create: %w: %s is being written", domain.ErrConflict, name)
		}
		return nil, fmt.Errorf("backupdir.Create: %w", err)
	}
	_ = r.Close()
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		_ = root.Remove(reserve)
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Create: %w", err)
	}
	tmp := "." + name + "." + hex.EncodeToString(suffix) + ".part"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = root.Remove(reserve)
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Create: %w", err)
	}
	return &writer{root: root, f: f, tmp: tmp, final: name, reserve: reserve}, nil
}

// reservedSuffix marks the hidden file that reserves a name while it is written.
const reservedSuffix = ".reserved"

func (w *writer) Write(p []byte) (int, error) { return w.f.Write(p) }

// Commit fsyncs the file, links it into place (never replacing an existing backup) and
// fsyncs the directory.
func (w *writer) Commit() (err error) {
	if w.done {
		return errors.New("backupdir: file already finished")
	}
	defer func() {
		if err != nil {
			_ = w.Abort()
		}
	}()
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("backupdir.Commit: %w", err)
	}
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("backupdir.Commit: %w", err)
	}
	if err := w.root.Link(w.tmp, w.final); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("backupdir.Commit: %w: %s exists", domain.ErrConflict, w.final)
		}
		// File systems without hard links: rename, after checking nothing is there.
		if _, serr := w.root.Lstat(w.final); serr == nil {
			return fmt.Errorf("backupdir.Commit: %w: %s exists", domain.ErrConflict, w.final)
		}
		if err := w.root.Rename(w.tmp, w.final); err != nil {
			return fmt.Errorf("backupdir.Commit: %w", err)
		}
	} else if err := w.root.Remove(w.tmp); err != nil {
		return fmt.Errorf("backupdir.Commit: %w", err)
	}
	_ = w.root.Remove(w.reserve)
	w.done = true
	defer w.root.Close()
	return syncDir(w.root)
}

// Abort removes the temporary file. It does nothing after Commit.
func (w *writer) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	_ = w.f.Close()
	err := w.root.Remove(w.tmp)
	_ = w.root.Remove(w.reserve)
	_ = w.root.Close()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("backupdir.Abort: %w", err)
	}
	return nil
}

// syncDir makes a link, rename or removal in the directory durable.
func syncDir(root *os.Root) error {
	if runtime.GOOS == "windows" { // directories cannot be fsynced there
		return nil
	}
	d, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("backupdir: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("backupdir: sync directory: %w", err)
	}
	return nil
}

// rootedFile closes the root with the file.
type rootedFile struct {
	*os.File
	root *os.Root
}

func (f rootedFile) Close() error {
	err := f.File.Close()
	_ = f.root.Close()
	return err
}

// Open implements ports.BackupFiles.
func (d *Dir) Open(_ context.Context, name string) (io.ReadCloser, error) {
	if err := checkName(name, true); err != nil {
		return nil, err
	}
	root, err := d.root()
	if err != nil {
		return nil, err
	}
	st, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Open: %w", domain.ErrNotFound)
	}
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Open: %w", err)
	}
	if !st.Mode().IsRegular() {
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Open: %s is not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("backupdir.Open: %w", err)
	}
	return rootedFile{File: f, root: root}, nil
}

// StalePartAge is how old a temporary .part file must be before List removes it: it was
// left by a backup or upload that crashed (a running one writes to it continuously).
const StalePartAge = 24 * time.Hour

// List implements ports.BackupFiles. It also removes stale temporary files.
func (d *Dir) List(_ context.Context) ([]ports.BackupFileInfo, error) {
	entries, err := os.ReadDir(d.path)
	if err != nil {
		return nil, fmt.Errorf("backupdir.List: %w", err)
	}
	var out []ports.BackupFileInfo
	for _, e := range entries {
		if e.Type().IsRegular() && isPart(e.Name()) {
			d.removeIfStale(e)
			continue
		}
		if !e.Type().IsRegular() || checkName(e.Name(), true) != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		out = append(out, ports.BackupFileInfo{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime().UTC(), Legacy: domain.IsLegacyBackupName(e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// isPart reports whether name is one of our temporary files (".<backup name>.<hex>.part")
// or name reservations (".<backup name>.reserved").
func isPart(name string) bool {
	if strings.HasPrefix(name, ".") && strings.HasSuffix(name, reservedSuffix) {
		return checkName(strings.TrimSuffix(strings.TrimPrefix(name, "."), reservedSuffix), false) == nil
	}
	if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".part") {
		return false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(name, "."), ".part")
	i := strings.LastIndexByte(inner, '.')
	return i > 0 && checkName(inner[:i], false) == nil
}

func (d *Dir) removeIfStale(e fs.DirEntry) {
	info, err := e.Info()
	if err != nil || time.Since(info.ModTime()) < StalePartAge {
		return
	}
	root, err := d.root()
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.Remove(e.Name())
}

// Remove implements ports.BackupFiles.
func (d *Dir) Remove(_ context.Context, name string) error {
	if err := checkName(name, true); err != nil {
		return err
	}
	root, err := d.root()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("backupdir.Remove: %w", domain.ErrNotFound)
		}
		return fmt.Errorf("backupdir.Remove: %w", err)
	}
	return syncDir(root)
}
