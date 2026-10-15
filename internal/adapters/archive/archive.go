// Package archive implements ports.InvoiceArchive on a directory — the invoices volume.
package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/albievan/simpleinvoice/internal/ports"
)

// Dir stores files below Root.
type Dir struct {
	Root string
}

var _ ports.InvoiceArchive = Dir{}

// Save implements ports.InvoiceArchive. It writes to a temporary file and renames it, so
// readers never see a half-written PDF. Paths may not escape Root.
func (d Dir) Save(_ context.Context, path string, data []byte) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return errors.New("archive.Save: invalid path")
	}
	full := filepath.Join(d.Root, clean)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("archive.Save: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return fmt.Errorf("archive.Save: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("archive.Save: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("archive.Save: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("archive.Save: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return fmt.Errorf("archive.Save: %w", err)
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		return fmt.Errorf("archive.Save: %w", err)
	}
	return nil
}

// Ping checks the archive directory is writable (readiness).
func (d Dir) Ping(_ context.Context) error {
	f, err := os.CreateTemp(d.Root, ".ready-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
