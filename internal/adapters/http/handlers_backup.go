package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// Backup routes are not ordinary requests: an upload is large, an export streams for as
// long as the snapshot is being written, and a stage or apply may take minutes. They get
// their own body limit and deadlines (set through http.ResponseController); export and
// upload are exempt from Idempotency-Key (their bodies are never buffered or stored).
const (
	pathExport  = "/api/v1/backups/export"
	pathUploads = "/api/v1/backups/uploads"
)

// DefaultMaxUploadBytes is the upload limit when none is configured (256 MiB).
const DefaultMaxUploadBytes = 256 << 20

// routeLimits are one request's body limit and read/write deadlines (zero: the server's).
type routeLimits struct {
	body          int64
	read, written time.Duration
}

// long reports whether the route gets more than the defaults (only once authenticated).
func (l routeLimits) long() bool { return l.body != maxBody || l.read > 0 || l.written > 0 }

func limitsFor(r *http.Request, maxUpload int64) routeLimits {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == pathUploads:
		return routeLimits{body: maxUpload, read: 30 * time.Minute, written: 30 * time.Minute}
	case r.Method == http.MethodPost && r.URL.Path == pathExport:
		return routeLimits{body: maxBody, written: app.DefaultExportTimeout + time.Minute}
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/restores" || strings.HasPrefix(r.URL.Path, "/api/v1/restores/")) ||
		r.Method == http.MethodPost && r.URL.Path == "/api/v1/backups":
		return routeLimits{body: maxBody, written: 15 * time.Minute}
	}
	return routeLimits{body: maxBody}
}

// skipsIdempotency reports the routes whose bodies must not be buffered or fingerprinted:
// export and upload (large, never replayable), stage (a passphrase) and every route that
// carries a password (reauthPath: sign-in, password changes, user management, apply),
// which an unsalted hash would expose to guessing.
func skipsIdempotency(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	p := r.URL.Path
	return p == pathExport || p == pathUploads || p == "/api/v1/restores" || reauthPath(r)
}

// abortOnError breaks the connection when the streamed body fails after the response
// started: a clean end would look like a complete download (http.ErrAbortHandler makes the
// server drop the connection, so the client's read fails).
type abortOnError struct{ r io.ReadCloser }

func (a abortOnError) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		panic(http.ErrAbortHandler)
	}
	return n, err
}

func (a abortOnError) Close() error { return a.r.Close() }

// applyLimits sets the request's body limit and deadlines.
func applyLimits(w http.ResponseWriter, r *http.Request, l routeLimits) {
	r.Body = http.MaxBytesReader(w, r.Body, l.body)
	rc := http.NewResponseController(w)
	if l.read > 0 {
		_ = rc.SetReadDeadline(time.Now().Add(l.read))
	}
	if l.written > 0 {
		_ = rc.SetWriteDeadline(time.Now().Add(l.written))
	}
}

func toAPIBackupFile(f app.BackupFileInfo) BackupFile {
	kind := BackupFileKind(f.Kind)
	if f.Legacy {
		kind = Legacy
	}
	return BackupFile{Name: f.Name, Size: f.Size, CreatedAt: f.CreatedAt, Schema: f.Schema, Kind: kind, Legacy: f.Legacy}
}

func (h *handlers) ListBackups(ctx context.Context, _ ListBackupsRequestObject) (ListBackupsResponseObject, error) {
	ov, err := h.svc.ListBackups(ctx)
	if err != nil {
		return nil, err
	}
	out := ListBackups200JSONResponse{
		KeyConfigured: ov.KeyConfigured, TimeZone: ov.TimeZone, RetentionDays: ov.RetentionDays,
		RestoreUnavailable: ov.RestoreUnavailable, MaxUploadBytes: h.maxUpload, Times: []string{}, Files: []BackupFile{},
	}
	for _, t := range ov.Times {
		out.Times = append(out.Times, time.Date(2000, 1, 1, t.Hour, t.Minute, 0, 0, time.UTC).Format("15:04"))
	}
	if !ov.NextRun.IsZero() {
		next := ov.NextRun
		out.NextRun = &next
	}
	if r := ov.LastRun; r != nil {
		run := BackupRun{Slot: r.Slot, Name: r.Name, Result: BackupRunResult(r.Result), Error: r.Error, StartedAt: r.StartedAt}
		if !r.FinishedAt.IsZero() {
			f := r.FinishedAt
			run.FinishedAt = &f
		}
		out.LastRun = &run
	}
	for _, f := range ov.Files {
		out.Files = append(out.Files, toAPIBackupFile(f))
	}
	return out, nil
}

func (h *handlers) CreateBackup(ctx context.Context, _ CreateBackupRequestObject) (CreateBackupResponseObject, error) {
	b, err := h.svc.CreateBackup(ctx, domain.BackupManual)
	if err != nil {
		return nil, err
	}
	return CreateBackup201JSONResponse{Name: b.Name, Size: b.Size, Sha256: b.SHA256, Schema: b.Schema}, nil
}

// ExportBackup streams the backup as it is written: ExportBackup runs in a goroutine
// writing into a pipe, and the response starts once it has audited the download and named
// the file. A failure mid-stream breaks the download (an age file that does not open), and
// a client that goes away closes the pipe, which ends the export.
func (h *handlers) ExportBackup(ctx context.Context, req ExportBackupRequestObject) (ExportBackupResponseObject, error) {
	pr, pw := io.Pipe()
	named := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil { // never take the process down; break the download
				err := fmt.Errorf("export: panic: %v", p)
				_ = pw.CloseWithError(err)
				failed <- err
			}
		}()
		err := h.svc.ExportBackup(ctx, req.Body.Passphrase, func(name string) (io.Writer, error) {
			named <- name
			return pw, nil
		})
		_ = pw.CloseWithError(err) // nil: the reader sees EOF
		failed <- err
	}()
	select {
	case name := <-named:
		return ExportBackup200ApplicationoctetStreamResponse{
			Body: abortOnError{pr}, Headers: ExportBackup200ResponseHeaders{ContentDisposition: `attachment; filename="` + name + `"`},
		}, nil
	case err := <-failed:
		_ = pr.Close()
		return nil, err
	}
}

func (h *handlers) UploadBackup(ctx context.Context, req UploadBackupRequestObject) (UploadBackupResponseObject, error) {
	f, err := h.svc.UploadBackup(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	return UploadBackup201JSONResponse(toAPIBackupFile(f)), nil
}

func toAPIAuditMerge(p domain.AuditMergePlan) AuditMerge {
	return AuditMerge{Mode: AuditMergeMode(p.Mode), Count: p.Count, Summary: p.Summary(), BackupBroken: p.BackupBroken}
}

func toAPIPreview(p app.RestorePreview) RestorePreview {
	out := RestorePreview{
		Id: p.ID, ExpiresAt: p.ExpiresAt, Source: p.Source, Sha256: p.SHA256, CreatedAt: p.Header.CreatedAt,
		CreatedBy: p.Header.CreatedBy, AppVersion: p.Header.AppVersion, SchemaFrom: p.SchemaFrom, SchemaTo: p.SchemaTo,
		Audit: toAPIAuditMerge(p.Audit), LiveAuditOk: p.LiveAuditOK, InvoicesSince: p.InvoicesSince, CanSignIn: p.CanSignIn,
		SmtpPasswordCleared: p.SMTPPasswordCleared, SafetyBackup: p.SafetyBackup, Warnings: p.Warnings, Tables: []RestoreTableCount{},
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	for _, t := range p.Tables {
		out.Tables = append(out.Tables, RestoreTableCount{Name: t.Name, Backup: t.Backup, Live: t.Live})
	}
	return out
}

func (h *handlers) StageRestore(ctx context.Context, req StageRestoreRequestObject) (StageRestoreResponseObject, error) {
	p, err := h.svc.StageRestore(ctx, req.Body.Backup, req.Body.Passphrase)
	if err != nil {
		return nil, err
	}
	return StageRestore200JSONResponse(toAPIPreview(p)), nil
}

func (h *handlers) GetCurrentRestore(ctx context.Context, _ GetCurrentRestoreRequestObject) (GetCurrentRestoreResponseObject, error) {
	p, err := h.svc.CurrentRestore(ctx)
	if err != nil {
		return nil, err
	}
	return GetCurrentRestore200JSONResponse(toAPIPreview(p)), nil
}

func (h *handlers) DiscardRestore(ctx context.Context, req DiscardRestoreRequestObject) (DiscardRestoreResponseObject, error) {
	if err := h.svc.DiscardRestore(ctx, req.Id); err != nil {
		return nil, err
	}
	return DiscardRestore204Response{}, nil
}

// ApplyRestore swaps the data in and clears the session cookie: every session was deleted.
func (h *handlers) ApplyRestore(ctx context.Context, req ApplyRestoreRequestObject) (ApplyRestoreResponseObject, error) {
	res, err := h.svc.ApplyRestore(ctx, req.Id, req.Body.CurrentPassword, req.Body.Confirmation)
	if err != nil {
		return nil, err
	}
	return ApplyRestore200JSONResponse{
		Body: RestoreResult{
			Source: res.Source, Audit: toAPIAuditMerge(res.Audit), SequenceKept: res.SequenceKept, SmtpPasswordCleared: res.SMTPPasswordCleared,
			SafetyBackup: res.SafetyBackup,
		},
		Headers: ApplyRestore200ResponseHeaders{SetCookie: h.cookies.cleared().String()},
	}, nil
}

func (h *handlers) ResumeAutomation(ctx context.Context, _ ResumeAutomationRequestObject) (ResumeAutomationResponseObject, error) {
	if err := h.svc.ResumeAutomation(ctx); err != nil {
		return nil, err
	}
	return ResumeAutomation204Response{}, nil
}
