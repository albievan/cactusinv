package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ActorBackupScheduler takes the scheduled backups.
var ActorBackupScheduler = Actor{ID: "system:backup", Name: "Backup scheduler"}

// BackupDeps are the ports and settings for backups and restores (ADR-010). With no
// Snapshotter every backup use case reports domain.ErrNotConfigured.
type BackupDeps struct {
	Snapshotter ports.Snapshotter
	Codec       ports.BackupCodec
	Sealer      ports.BackupSealer
	Files       ports.BackupFiles
	Stager      ports.RestoreStager
	// Times are the daily scheduled backup times in Location (none: no scheduled backups).
	Times    []domain.TimeOfDay
	Location *time.Location
	// RetentionDays prunes older backups (0: keep everything).
	RetentionDays int
	// AppVersion and Build are written into every backup header.
	AppVersion string
	Build      string
	// ExportTimeout bounds a download (default DefaultExportTimeout): the export streams
	// to the client from inside one consistent snapshot, which a stalled client must not
	// keep open indefinitely.
	ExportTimeout time.Duration
}

// DefaultExportTimeout is how long a download may take.
const DefaultExportTimeout = 30 * time.Minute

// errExportTimeout ends a download that took longer than ExportTimeout.
var errExportTimeout = errors.New("the download took too long and was stopped")

// deadlineWriter refuses writes after its deadline.
type deadlineWriter struct {
	w        io.Writer
	deadline time.Time
}

func (d deadlineWriter) Write(p []byte) (int, error) {
	if time.Now().After(d.deadline) {
		return 0, errExportTimeout
	}
	return d.w.Write(p)
}

// BackupFileInfo is one file in the backups directory.
type BackupFileInfo struct {
	Name      string
	Size      int64
	ModTime   time.Time
	CreatedAt time.Time         // from the name (the modification time for legacy dumps)
	Schema    int64             // 0: not known (an upload or a legacy dump)
	Kind      domain.BackupKind // "" for legacy dumps
	Legacy    bool
}

// CreatedBackup describes a backup just written.
type CreatedBackup struct {
	Name   string
	Size   int64
	SHA256 string
	Schema int64
	Digest domain.BackupDigest
}

// BackupOverview is the backups page: schedule, key, last run and stored files.
type BackupOverview struct {
	KeyConfigured bool
	Times         []domain.TimeOfDay
	TimeZone      string
	NextRun       time.Time // zero without scheduled backups or without a key
	RetentionDays int
	LastRun       *domain.BackupRun
	Files         []BackupFileInfo
	// RestoreUnavailable says why restores cannot be staged ("" when they can).
	RestoreUnavailable string
}

// countingHash hashes and counts what is written through it.
type countingHash struct {
	h hash.Hash
	n int64
}

func newCountingHash() *countingHash { return &countingHash{h: sha256.New()} }

func (c *countingHash) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return c.h.Write(p)
}

func (c *countingHash) Sum() string { return hex.EncodeToString(c.h.Sum(nil)) }

// lockBackups takes the one-backup-operation-at-a-time slot (age's scrypt alone needs about
// 256 MiB); domain.ErrBusy when another backup or restore is running.
func (s *Service) lockBackups() (func(), error) {
	select {
	case s.backupSlot <- struct{}{}:
		return func() { <-s.backupSlot }, nil
	default:
		return nil, domain.ErrBusy
	}
}

func (s *Service) backupsConfigured() error {
	b := s.backups
	if b.Snapshotter == nil || b.Codec == nil || b.Sealer == nil {
		return fmt.Errorf("%w: backups", domain.ErrNotConfigured)
	}
	return nil
}

func (s *Service) backupHeader(ctx context.Context, src ports.BackupSource) domain.BackupHeader {
	return domain.BackupHeader{
		FormatVersion: domain.BackupFormatVersion, SchemaVersion: src.SchemaVersion(),
		AppVersion: s.backups.AppVersion, Build: s.backups.Build, CreatedAt: s.now(),
		CreatedBy: domain.TruncateUTF8(ActorFrom(ctx).Name, 254), Tables: src.Tables(), // the header limits bytes
	}
}

// CreateBackup writes a new backup to the backups directory, encrypted to the server
// backup key, and records it in the audit trail.
func (s *Service) CreateBackup(ctx context.Context, kind domain.BackupKind) (CreatedBackup, error) {
	unlock, err := s.lockBackups()
	if err != nil {
		return CreatedBackup{}, wrap("app.CreateBackup", err)
	}
	defer unlock()
	out, err := s.createBackup(ctx, kind)
	return out, wrap("app.CreateBackup", err)
}

func (s *Service) createBackup(ctx context.Context, kind domain.BackupKind) (CreatedBackup, error) {
	if err := s.backupsConfigured(); err != nil {
		return CreatedBackup{}, err
	}
	if s.backups.Files == nil || !s.backups.Sealer.KeyConfigured() {
		return CreatedBackup{}, fmt.Errorf("%w: backup_key", domain.ErrNotConfigured)
	}
	var out CreatedBackup
	err := s.backups.Snapshotter.Snapshot(ctx, func(ctx context.Context, src ports.BackupSource) error {
		h := s.backupHeader(ctx, src)
		name, fw, err := s.createBackupFile(ctx, h, kind)
		if err != nil {
			return err
		}
		sum := newCountingHash()
		digest, err := s.sealAndEncode(ctx, io.MultiWriter(fw, sum), h, src, func(w io.Writer) (io.WriteCloser, error) {
			return s.backups.Sealer.SealWithKey(w)
		})
		if err != nil {
			_ = fw.Abort()
			var pe *fs.PathError
			if errors.As(err, &pe) {
				return backupFailure{"disk", err}
			}
			return backupFailure{"database", err}
		}
		if err := fw.Commit(); err != nil {
			_ = fw.Abort()
			return backupFailure{"disk", err}
		}
		out = CreatedBackup{Name: name, Size: sum.n, SHA256: sum.Sum(), Schema: h.SchemaVersion, Digest: digest}
		return nil
	})
	if err != nil {
		var bf backupFailure
		if !errors.As(err, &bf) && !errors.Is(err, domain.ErrConflict) {
			err = backupFailure{"database", err}
		}
		return CreatedBackup{}, err
	}
	if kind == domain.BackupPreRestore {
		// Recorded by the restore's own entry (backup.restored, or its refusal or failure),
		// so the live trail does not change between the preview and the swap.
		return out, nil
	}
	return out, s.auditNow(ctx, "backup.created", "backup", out.Name, fmt.Sprintf("%s backup, schema %d, %d bytes, sha256 %s", kind, out.Schema, out.Size, out.SHA256))
}

// backupFailure tags why a backup failed with a category people can act on (database,
// disk, key); the error itself is only logged.
type backupFailure struct {
	category string
	err      error
}

func (b backupFailure) Error() string { return b.category + ": " + b.err.Error() }
func (b backupFailure) Unwrap() error { return b.err }

// failureCategory is the category recorded for a failed scheduled backup.
func failureCategory(err error) string {
	var bf backupFailure
	switch {
	case errors.Is(err, domain.ErrNotConfigured):
		return "key"
	case errors.As(err, &bf):
		return bf.category
	case errors.Is(err, domain.ErrConflict):
		return "disk"
	}
	return "other"
}

// createBackupFile creates the stored file for a new backup. Names have one-second
// resolution: a manual or safety backup taken in a second already used takes the next
// free one (a scheduled backup's slot runs once anyway).
func (s *Service) createBackupFile(ctx context.Context, h domain.BackupHeader, kind domain.BackupKind) (string, ports.BackupFileWriter, error) {
	tries := maxUploadNameTries
	if kind == domain.BackupScheduled {
		tries = 1
	}
	var err error
	for i := range tries {
		name := domain.BackupName{CreatedAt: h.CreatedAt.Truncate(time.Second).Add(time.Duration(i) * time.Second), Schema: h.SchemaVersion, Kind: kind}.String()
		var fw ports.BackupFileWriter
		if fw, err = s.backups.Files.Create(ctx, name); err == nil {
			return name, fw, nil
		}
		if !errors.Is(err, domain.ErrConflict) {
			return "", nil, backupFailure{"disk", err}
		}
	}
	return "", nil, err
}

// sealAndEncode encodes src through an encrypting writer onto w.
func (s *Service) sealAndEncode(ctx context.Context, w io.Writer, h domain.BackupHeader, src ports.BackupSource, seal func(io.Writer) (io.WriteCloser, error)) (domain.BackupDigest, error) {
	sealed, err := seal(w)
	if err != nil {
		return domain.BackupDigest{}, err
	}
	digest, err := s.backups.Codec.Encode(ctx, sealed, h, src)
	if cerr := sealed.Close(); err == nil {
		err = cerr
	}
	return digest, err
}

// ExportBackup takes a fresh backup encrypted with passphrase and streams it to the writer
// open returns; nothing is stored. The download is audited before the first byte is
// written; open receives the file name (for Content-Disposition).
func (s *Service) ExportBackup(ctx context.Context, passphrase string, open func(name string) (io.Writer, error)) error {
	if err := domain.ValidateBackupPassphrase(passphrase); err != nil {
		return wrap("app.ExportBackup", err)
	}
	if err := s.backupsConfigured(); err != nil {
		return wrap("app.ExportBackup", err)
	}
	unlock, err := s.lockBackups()
	if err != nil {
		return wrap("app.ExportBackup", err)
	}
	defer unlock()
	limit := s.backups.ExportTimeout
	if limit <= 0 {
		limit = DefaultExportTimeout
	}
	// Wall-clock bound: the snapshot (an open read view) ends when the download stalls.
	deadline := time.Now().Add(limit)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	err = s.backups.Snapshotter.Snapshot(ctx, func(ctx context.Context, src ports.BackupSource) error {
		h := s.backupHeader(ctx, src)
		name := domain.BackupName{CreatedAt: h.CreatedAt, Schema: h.SchemaVersion, Kind: domain.BackupManual}.String()
		if err := s.auditNow(ctx, "backup.exported", "backup", name, fmt.Sprintf("downloaded, schema %d, encrypted with a passphrase", h.SchemaVersion)); err != nil {
			return err
		}
		w, err := open(name)
		if err != nil {
			return err
		}
		_, err = s.sealAndEncode(ctx, deadlineWriter{w: w, deadline: deadline}, h, src, func(w io.Writer) (io.WriteCloser, error) {
			return s.backups.Sealer.SealWithPassphrase(w, passphrase)
		})
		return err
	})
	return wrap("app.ExportBackup", err)
}

// UploadBackup stores an uploaded backup file as it arrives (still encrypted); it can then
// be staged by name with its passphrase. The caller limits the size of r.
func (s *Service) UploadBackup(ctx context.Context, r io.Reader) (BackupFileInfo, error) {
	if s.backups.Files == nil {
		return BackupFileInfo{}, wrap("app.UploadBackup", fmt.Errorf("%w: backups directory", domain.ErrNotConfigured))
	}
	// Names have one-second resolution: a second upload in the same second takes the next
	// free second.
	var n domain.BackupName
	var name string
	var fw ports.BackupFileWriter
	var err error
	for i := range maxUploadNameTries {
		n = domain.BackupName{CreatedAt: s.now().Truncate(time.Second).Add(time.Duration(i) * time.Second), Kind: domain.BackupUpload}
		name = n.String()
		if fw, err = s.backups.Files.Create(ctx, name); !errors.Is(err, domain.ErrConflict) {
			break
		}
	}
	if err != nil {
		return BackupFileInfo{}, wrap("app.UploadBackup", err)
	}
	sum := newCountingHash()
	if _, err := io.Copy(io.MultiWriter(fw, sum), r); err != nil {
		_ = fw.Abort()
		return BackupFileInfo{}, wrap("app.UploadBackup", err)
	}
	if err := fw.Commit(); err != nil {
		_ = fw.Abort()
		return BackupFileInfo{}, wrap("app.UploadBackup", err)
	}
	info := BackupFileInfo{Name: name, Size: sum.n, ModTime: n.CreatedAt, CreatedAt: n.CreatedAt, Kind: domain.BackupUpload}
	return info, wrap("app.UploadBackup", s.auditNow(ctx, "backup.uploaded", "backup", name, fmt.Sprintf("%d bytes, sha256 %s", sum.n, sum.Sum())))
}

// ListBackups returns the schedule, the last scheduled run and the stored files (newest
// first).
func (s *Service) ListBackups(ctx context.Context) (BackupOverview, error) {
	b := s.backups
	out := BackupOverview{
		KeyConfigured: b.Sealer != nil && b.Sealer.KeyConfigured(), Times: b.Times,
		TimeZone: s.backupLocation().String(), RetentionDays: b.RetentionDays,
	}
	if out.KeyConfigured {
		out.NextRun, _ = domain.NextBackupSlot(b.Times, s.backupLocation(), s.now())
	}
	if run, err := s.tx.Repos().BackupRuns.Latest(ctx); err == nil {
		out.LastRun = &run
	} else if !errors.Is(err, domain.ErrNotFound) {
		return BackupOverview{}, wrap("app.ListBackups", err)
	}
	switch {
	case b.Stager == nil:
		out.RestoreUnavailable = "restores are not configured"
	default:
		if err := b.Stager.Ready(ctx); err != nil {
			s.log.WarnContext(ctx, "restore staging database unavailable", "err", err)
			out.RestoreUnavailable = "the staging database is not available; run deploy.sh --install to create and grant it"
		}
	}
	files, err := s.backupFiles(ctx)
	out.Files = files
	return out, wrap("app.ListBackups", err)
}

// maxUploadNameTries bounds the search for a free upload name.
const maxUploadNameTries = 60

func (s *Service) backupLocation() *time.Location {
	if s.backups.Location == nil {
		return time.UTC
	}
	return s.backups.Location
}

func (s *Service) backupFiles(ctx context.Context) ([]BackupFileInfo, error) {
	if s.backups.Files == nil {
		return nil, nil
	}
	list, err := s.backups.Files.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BackupFileInfo, 0, len(list))
	for _, f := range list {
		info := BackupFileInfo{Name: f.Name, Size: f.Size, ModTime: f.ModTime, CreatedAt: f.ModTime, Legacy: f.Legacy}
		if n, err := domain.ParseBackupName(f.Name); err == nil {
			info.CreatedAt, info.Schema, info.Kind = n.CreatedAt, n.Schema, n.Kind
		} else if !f.Legacy {
			continue
		}
		out = append(out, info)
	}
	sortNewestFirst(out)
	return out, nil
}

func sortNewestFirst(files []BackupFileInfo) {
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && files[j].CreatedAt.After(files[j-1].CreatedAt); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

// RunDueBackups takes the scheduled backup of the current slot if no replica has taken it
// yet (backup_runs.slot is unique), then prunes. It reports whether it took one.
func (s *Service) RunDueBackups(ctx context.Context) (bool, error) {
	b := s.backups
	if len(b.Times) == 0 || b.Sealer == nil || !b.Sealer.KeyConfigured() || s.backupsConfigured() != nil {
		return false, nil
	}
	slot, ok := domain.DueBackupSlot(b.Times, s.backupLocation(), s.now())
	if !ok {
		return false, nil
	}
	unlock, err := s.lockBackups()
	if err != nil {
		return false, nil // a manual backup or restore is running; try again next pass
	}
	defer unlock()
	ctx = WithActor(ctx, ActorBackupScheduler)
	run := domain.BackupRun{Slot: slot, Result: domain.BackupRunRunning, StartedAt: s.now()}
	if err := s.tx.Repos().BackupRuns.Start(ctx, run); errors.Is(err, domain.ErrConflict) {
		return false, nil
	} else if err != nil {
		return false, wrap("app.RunDueBackups", err)
	}
	created, berr := s.createBackup(ctx, domain.BackupScheduled)
	run.FinishedAt, run.Name, run.Result = s.now(), created.Name, domain.BackupRunOK
	if berr != nil {
		// Only the category is stored and shown (database, disk, key, other); the error
		// itself, which may quote internal details, goes to the log.
		run.Result, run.Error = domain.BackupRunFailed, failureCategory(berr)
		s.log.ErrorContext(ctx, "scheduled backup failed", "slot", slot, "category", run.Error, "err", berr)
		_ = s.auditNow(ctx, "backup.failed", "backup", "", "scheduled backup failed ("+run.Error+")")
	}
	if err := s.tx.Repos().BackupRuns.Finish(ctx, run); err != nil {
		return true, wrap("app.RunDueBackups", err)
	}
	if berr != nil {
		return true, wrap("app.RunDueBackups", berr)
	}
	_, err = s.pruneBackups(ctx)
	return true, wrap("app.RunDueBackups", err)
}

// PruneBackups removes backups (and legacy dumps) older than the retention period, never
// the newest, recording one audit entry per pass that removed anything.
func (s *Service) PruneBackups(ctx context.Context) ([]string, error) {
	out, err := s.pruneBackups(ctx)
	return out, wrap("app.PruneBackups", err)
}

func (s *Service) pruneBackups(ctx context.Context) ([]string, error) {
	if s.backups.RetentionDays <= 0 || s.backups.Files == nil {
		return nil, nil
	}
	files, err := s.backupFiles(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]domain.BackupFile, len(files))
	for i, f := range files {
		refs[i] = domain.BackupFile{Name: f.Name, Time: f.CreatedAt, Legacy: f.Legacy}
	}
	var removed []string
	var firstErr error
	for _, name := range domain.SelectPrunable(refs, s.now(), s.backups.RetentionDays) {
		if err := s.backups.Files.Remove(ctx, name); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, name)
	}
	if len(removed) > 0 {
		// Names sort by time: record the count and the range, not every name (the summary
		// is bounded).
		summary := fmt.Sprintf("removed %d backups older than %d days", len(removed), s.backups.RetentionDays)
		if len(removed) == 1 {
			summary += ": " + removed[0]
		} else {
			summary += fmt.Sprintf(", from %s to %s", removed[0], removed[len(removed)-1])
		}
		if err := s.auditNow(ctx, "backup.pruned", "backup", "", summary); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return removed, firstErr
}
