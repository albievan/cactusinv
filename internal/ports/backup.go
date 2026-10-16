package ports

import (
	"context"
	"io"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

// Backup and restore ports (ADR-010). Rows cross these interfaces as []any holding the
// value types documented on domain.ColumnKind, one value per column of their table.

// BackupSource is one consistent, read-only view of every backed-up table.
type BackupSource interface {
	// SchemaVersion is the database schema (goose) version of the view.
	SchemaVersion() int64
	// Tables lists the exported tables and columns, parents before children.
	Tables() []domain.BackupTable
	// ScanTable calls fn with every row of table in primary-key order. fn must not keep
	// row after it returns.
	ScanTable(ctx context.Context, table string, fn func(row []any) error) error
}

// Snapshotter opens a consistent snapshot of the database.
type Snapshotter interface {
	// Snapshot calls fn with a source that reads one consistent snapshot; the snapshot
	// ends when fn returns.
	Snapshot(ctx context.Context, fn func(ctx context.Context, src BackupSource) error) error
}

// BackupSink receives a decoded backup: the header first, then every row.
type BackupSink interface {
	Header(ctx context.Context, h domain.BackupHeader) error
	Row(ctx context.Context, table string, row []any) error
}

// BackupCodec reads and writes the backup format (header, table rows, digest).
type BackupCodec interface {
	// Encode writes h (with h.Tables), every row of src, and the digest to w, and returns
	// the digest it wrote.
	Encode(ctx context.Context, w io.Writer, h domain.BackupHeader, src BackupSource) (domain.BackupDigest, error)
	// Decode reads a backup from r into sink and verifies it against its digest. Damaged
	// or inconsistent input is domain.ErrBackupInvalid; sink's own errors are returned as
	// they are. On error the sink may have received part of the backup.
	Decode(ctx context.Context, r io.Reader, sink BackupSink) (domain.BackupHeader, domain.BackupDigest, error)
}

// BackupSealer encrypts and decrypts backup files.
type BackupSealer interface {
	// KeyConfigured reports whether the server backup key is available (stored backups).
	KeyConfigured() bool
	// SealWithKey returns a writer encrypting to the server backup key; Close finishes it.
	// domain.ErrNotConfigured without a key.
	SealWithKey(w io.Writer) (io.WriteCloser, error)
	// SealWithPassphrase returns a writer encrypting with a passphrase.
	SealWithPassphrase(w io.Writer, passphrase string) (io.WriteCloser, error)
	// Open decrypts r with the passphrase when one is given, else with the server key.
	// Wrong keys, damage and truncation are domain.ErrBackupInvalid, possibly only when
	// the returned reader is read.
	Open(r io.Reader, passphrase string) (io.Reader, error)
}

// BackupFileInfo describes a file in the backups directory.
type BackupFileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
	Legacy  bool // a plaintext .sql.gz of the retired backup sidecar
}

// BackupFileWriter writes one new backup file. Commit makes it visible under its name;
// Abort (or a failed Commit) removes it. Calling Abort after Commit does nothing.
type BackupFileWriter interface {
	io.Writer
	Commit() error
	Abort() error
}

// BackupFiles is the backups directory. Names are domain.BackupName strings (legacy dumps
// may only be listed and removed); anything else is refused.
type BackupFiles interface {
	// Create starts a new file; ErrConflict when the name exists.
	Create(ctx context.Context, name string) (BackupFileWriter, error)
	// Open reads a stored file; ErrNotFound when missing.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	// List returns backups and legacy dumps; other files are ignored.
	List(ctx context.Context) ([]BackupFileInfo, error)
	// Remove deletes a stored file.
	Remove(ctx context.Context, name string) error
}

// StagedRestore describes the backup loaded into the staging database.
type StagedRestore struct {
	// SchemaVersion is the staging database's schema after migrating it to head.
	SchemaVersion int64
	// Counts are the staged rows per included table; LiveCounts the live ones.
	Counts     map[string]int64
	LiveCounts map[string]int64
	// SMTPPasswordEnc is the staged SMTP password ciphertext (nil when none).
	SMTPPasswordEnc []byte
	// UserIDs are the ids of the staged users who are active (can sign in).
	UserIDs []string
}

// RestoreTx is the live database during an applied restore: every live table locked for
// writing on one connection, inside one transaction.
type RestoreTx interface {
	// Repos are the live repositories on the locked connection.
	Repos() Repos
	// ReplaceTables replaces every included table except the audit trail with the staged
	// rows.
	ReplaceTables(ctx context.Context) error
	// StagedAudit returns the staged audit trail, oldest first.
	StagedAudit(ctx context.Context) ([]domain.AuditEntry, error)
}

// RestoreStager loads a backup into the staging database and swaps it into the live one.
type RestoreStager interface {
	// Ready reports why a restore cannot be staged (no staging database or no access).
	Ready(ctx context.Context) error
	// Lock takes the installation-wide backup lock, held across every process until the
	// returned func is called; domain.ErrBusy when another process holds it. Stage and
	// Discard run only under it. Apply takes the same lock itself, on the swap's own
	// connection: do not hold Lock while calling Apply.
	Lock(ctx context.Context) (func(), error)
	// SchemaVersion is the live database's (head) schema version.
	SchemaVersion(ctx context.Context) (int64, error)
	// Stage empties the staging database, calls load with a sink that writes into it, then
	// migrates it to head and describes it.
	Stage(ctx context.Context, load func(ctx context.Context, sink BackupSink) error) (StagedRestore, error)
	// Staged describes what the staging database holds now.
	Staged(ctx context.Context) (StagedRestore, error)
	// StagedAudit returns the staged audit trail, oldest first.
	StagedAudit(ctx context.Context) ([]domain.AuditEntry, error)
	// Apply takes the backup lock (domain.ErrBusy when held elsewhere) and runs fn inside
	// one exclusive transaction over the live and staging tables; fn's error rolls
	// everything back.
	Apply(ctx context.Context, fn func(ctx context.Context, tx RestoreTx) error) error
	// Discard empties the staging database.
	Discard(ctx context.Context) error
}
