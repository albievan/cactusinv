package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// RestoreTableCount compares one table's rows in the backup and in the live database.
type RestoreTableCount struct {
	Name   string
	Backup int64
	Live   int64
}

// RestorePreview is what a staged restore would do, shown before it is applied.
type RestorePreview struct {
	ID         string
	ExpiresAt  time.Time
	Source     string
	SHA256     string
	Header     domain.BackupHeader
	SchemaFrom int64 // the backup's schema
	SchemaTo   int64 // this version's schema
	Tables     []RestoreTableCount
	Audit      domain.AuditMergePlan
	// LiveAuditOK is false when the current audit trail does not verify; a restore leaves
	// that visible.
	LiveAuditOK bool
	// InvoicesSince counts live invoices created after the backup: a restore removes them.
	InvoicesSince int
	// CanSignIn says whether the account staging the restore is an active user in the backup.
	CanSignIn bool
	// SMTPPasswordCleared says the backup's e-mail server password cannot be decrypted here.
	SMTPPasswordCleared bool
	// SafetyBackup says the live data will be backed up just before the restore.
	SafetyBackup bool
	Warnings     []string
}

// RestoreResult is what an applied restore did.
type RestoreResult struct {
	Source              string
	SafetyBackup        string // the backup of the data just before the restore ("" when none)
	Audit               domain.AuditMergePlan
	SequenceKept        bool // the live invoice sequence was ahead and was kept
	SMTPPasswordCleared bool
}

// stageSink refuses a backup this version cannot restore before any row is loaded.
type stageSink struct {
	ports.BackupSink
	current int64
}

func (k stageSink) Header(ctx context.Context, h domain.BackupHeader) error {
	if err := domain.CheckRestorable(h, k.current); err != nil {
		return err
	}
	return k.BackupSink.Header(ctx, h)
}

// restoreRefusal reports whether err refuses a restore (the backup or the request is the
// problem) rather than a failure of the system.
func restoreRefusal(err error) bool {
	for _, target := range []error{
		domain.ErrBackupInvalid, domain.ErrBackupTooNew, domain.ErrBackupUnsupported, domain.ErrBusy,
		domain.ErrValidation, domain.ErrNotFound, domain.ErrUnauthenticated,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// refusalReason is the audited reason: the error class only, never row data or secrets.
func refusalReason(err error) string {
	switch {
	case errors.Is(err, domain.ErrBackupInvalid):
		return "the backup is invalid, damaged, or the passphrase or key is wrong"
	case errors.Is(err, domain.ErrBackupTooNew):
		return "the backup is from a newer version"
	case errors.Is(err, domain.ErrBackupUnsupported):
		return "the backup format is not supported"
	case errors.Is(err, domain.ErrBusy):
		return "busy: an e-mail is being sent or another backup operation is running"
	case errors.Is(err, domain.ErrNotFound):
		return "no staged restore with that id (or it expired)"
	}
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Field + " " + ve.Reason
	}
	return "refused"
}

// auditRestoreOutcome records a refused or failed restore in its own transaction.
func (s *Service) auditRestoreOutcome(ctx context.Context, source string, err error, notes ...string) {
	action, summary := "backup.restore_failed", "restore failed"
	if errors.Is(err, errSafetyBackup) {
		summary = "restore failed: " + errSafetyBackup.Error() + "; nothing was changed"
	}
	if restoreRefusal(err) {
		action, summary = "backup.restore_refused", "restore refused: "+refusalReason(err)
	}
	for _, n := range notes {
		summary += "; " + n
	}
	if aerr := s.auditNow(ctx, action, "backup", source, summary); aerr != nil {
		s.log.ErrorContext(ctx, "could not audit a restore outcome", "action", action, "err", aerr)
	}
}

func (s *Service) restoresConfigured() error {
	if err := s.backupsConfigured(); err != nil {
		return err
	}
	if s.backups.Stager == nil || s.backups.Files == nil {
		return fmt.Errorf("%w: restores", domain.ErrNotConfigured)
	}
	return nil
}

// StageRestore decrypts a stored backup (with passphrase, or the server key when empty),
// loads it into the staging database, migrates it to this version and returns the
// preview. The live data is not touched.
func (s *Service) StageRestore(ctx context.Context, name, passphrase string) (RestorePreview, error) {
	if err := s.restoresConfigured(); err != nil {
		return RestorePreview{}, wrap("app.StageRestore", err)
	}
	if _, err := domain.ParseBackupName(name); err != nil {
		return RestorePreview{}, wrap("app.StageRestore", err)
	}
	unlock, err := s.lockRestore(ctx)
	if err != nil {
		s.auditRestoreOutcome(ctx, name, err)
		return RestorePreview{}, wrap("app.StageRestore", err)
	}
	defer unlock()
	// The previous stage stops being applicable before the staging database is touched,
	// so nothing can ever apply a half-loaded staging database.
	if _, err := s.clearStage(ctx, "", false); err != nil {
		return RestorePreview{}, wrap("app.StageRestore", err)
	}
	p, err := s.stage(ctx, name, passphrase)
	if err != nil {
		s.auditRestoreOutcome(ctx, name, err)
		if derr := s.backups.Stager.Discard(ctx); derr != nil {
			s.log.WarnContext(ctx, "could not empty the staging database after a failed stage", "err", derr)
		}
	}
	return p, wrap("app.StageRestore", err)
}

// lockRestore takes the in-process backup slot and the installation-wide backup lock
// (every replica); domain.ErrBusy when either is taken.
func (s *Service) lockRestore(ctx context.Context) (func(), error) {
	unlockLocal, err := s.lockBackups()
	if err != nil {
		return nil, err
	}
	unlockShared, err := s.backups.Stager.Lock(ctx)
	if err != nil {
		unlockLocal()
		return nil, err
	}
	return func() {
		unlockShared()
		unlockLocal()
	}, nil
}

// clearStage forgets the staged restore in its own transaction: any one (onlyID ""), or
// only the one with id onlyID, and with onlyExpired only once it expired. It reports
// whether it cleared one. Call with the restore lock held.
func (s *Service) clearStage(ctx context.Context, onlyID string, onlyExpired bool) (bool, error) {
	cleared := false
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		st, err := r.System.Lock(ctx)
		if err != nil || st.RestoreID == "" || (onlyID != "" && st.RestoreID != onlyID) || (onlyExpired && st.HasStagedRestore(s.now())) {
			return err
		}
		st.ClearRestore()
		cleared = true
		return r.System.Save(ctx, st)
	})
	return cleared, err
}

func (s *Service) stage(ctx context.Context, name, passphrase string) (RestorePreview, error) {
	b := s.backups
	if err := b.Stager.Ready(ctx); err != nil {
		return RestorePreview{}, err
	}
	current, err := b.Stager.SchemaVersion(ctx)
	if err != nil {
		return RestorePreview{}, err
	}
	f, err := b.Files.Open(ctx, name)
	if err != nil {
		return RestorePreview{}, err
	}
	defer f.Close()
	sum := newCountingHash()
	cipher := io.TeeReader(f, sum)
	plain, err := b.Sealer.Open(cipher, passphrase)
	if err != nil {
		return RestorePreview{}, err
	}
	var header domain.BackupHeader
	if _, err := b.Stager.Stage(ctx, func(ctx context.Context, sink ports.BackupSink) error {
		h, _, err := b.Codec.Decode(ctx, plain, stageSink{BackupSink: sink, current: current})
		if err != nil {
			return err
		}
		header = h
		// Read the plaintext to its end: age authenticates the last chunk there, and
		// nothing may follow the backup.
		if n, err := io.Copy(io.Discard, plain); err != nil || n > 0 {
			return fmt.Errorf("%w: data after the backup", domain.ErrBackupInvalid)
		}
		return nil
	}); err != nil {
		return RestorePreview{}, err
	}
	if _, err := io.Copy(io.Discard, cipher); err != nil { // hash the whole file
		return RestorePreview{}, err
	}
	id := s.ids.NewID()
	actor := ActorFrom(ctx)
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		st, err := r.System.Lock(ctx)
		if err != nil {
			return err
		}
		st.RestoreID, st.RestoreExpiresAt, st.RestoreSource = id, s.now().Add(domain.RestoreStageTTL), name
		st.RestoreSHA256, st.RestoreStagedBy, st.RestoreHeader = sum.Sum(), actor.ID, header
		return r.System.Save(ctx, st)
	})
	if err != nil {
		return RestorePreview{}, err
	}
	return s.preview(ctx)
}

// CurrentRestore returns the preview of the staged restore, or ErrNotFound.
func (s *Service) CurrentRestore(ctx context.Context) (RestorePreview, error) {
	if err := s.restoresConfigured(); err != nil {
		return RestorePreview{}, wrap("app.CurrentRestore", err)
	}
	p, err := s.preview(ctx)
	return p, wrap("app.CurrentRestore", err)
}

func (s *Service) preview(ctx context.Context) (RestorePreview, error) {
	r := s.tx.Repos()
	st, err := r.System.Get(ctx)
	if err != nil {
		return RestorePreview{}, err
	}
	if !st.HasStagedRestore(s.now()) {
		return RestorePreview{}, domain.ErrNotFound
	}
	staged, err := s.backups.Stager.Staged(ctx)
	if err != nil {
		return RestorePreview{}, err
	}
	backupAudit, err := s.backups.Stager.StagedAudit(ctx)
	if err != nil {
		return RestorePreview{}, err
	}
	liveAudit, err := s.auditChain(ctx, r)
	if err != nil {
		return RestorePreview{}, err
	}
	since, err := r.Invoices.CountCreatedSince(ctx, st.RestoreHeader.CreatedAt)
	if err != nil {
		return RestorePreview{}, err
	}
	p := RestorePreview{
		ID: st.RestoreID, ExpiresAt: st.RestoreExpiresAt, Source: st.RestoreSource, SHA256: st.RestoreSHA256,
		Header: st.RestoreHeader, SchemaFrom: st.RestoreHeader.SchemaVersion, SchemaTo: staged.SchemaVersion,
		Audit: domain.PlanAuditMerge(liveAudit, backupAudit), LiveAuditOK: domain.VerifyAuditChain(liveAudit).OK,
		InvoicesSince: since, CanSignIn: canSignIn(ActorFrom(ctx), staged.UserIDs),
		SMTPPasswordCleared: staged.SMTPPasswordEnc != nil && !s.canDecrypt(staged.SMTPPasswordEnc),
		SafetyBackup:        s.safetyBackupPossible(),
	}
	for _, name := range sortedKeys(staged.Counts, staged.LiveCounts) {
		p.Tables = append(p.Tables, RestoreTableCount{Name: name, Backup: staged.Counts[name], Live: staged.LiveCounts[name]})
	}
	p.Warnings = restoreWarnings(p)
	return p, nil
}

// canSignIn reports whether the actor staging a restore can sign in afterwards: a user
// whose account is active in the backup, or a system actor (the CLI operator), which has no
// account to lose.
func canSignIn(a Actor, userIDs []string) bool {
	return strings.HasPrefix(a.ID, "system") || slices.Contains(userIDs, a.ID)
}

func sortedKeys(maps ...map[string]int64) []string {
	var out []string
	for _, m := range maps {
		for k := range m {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	slices.Sort(out)
	return out
}

func restoreWarnings(p RestorePreview) []string {
	var w []string
	if p.InvoicesSince > 0 {
		w = append(w, fmt.Sprintf("%d invoices were created after this backup was taken; restoring removes them (their numbers are not reused)", p.InvoicesSince))
	}
	if !p.CanSignIn {
		w = append(w, "your account is not an active user in this backup: after the restore you must sign in with an account that is")
	}
	if p.SMTPPasswordCleared {
		w = append(w, "the e-mail server password in this backup cannot be decrypted by this installation; it will be cleared and must be entered again")
	}
	if p.SchemaFrom < p.SchemaTo {
		w = append(w, fmt.Sprintf("the backup comes from an older version (schema %d); it was upgraded to schema %d", p.SchemaFrom, p.SchemaTo))
	}
	if p.Audit.BackupBroken {
		w = append(w, fmt.Sprintf("the backup's audit trail does not verify (broken at #%d); its entries will be re-sealed and the break recorded", p.Audit.BackupBrokenAt))
	} else if p.Audit.Mode == domain.AuditAppendResealed {
		w = append(w, fmt.Sprintf("the audit trails have diverged; %d entries from the backup will be added re-sealed between two markers", p.Audit.Count))
	}
	if !p.SafetyBackup {
		w = append(w, "no safety backup of the current data will be taken (the server backup key, backup_key, is not set up): download a backup first if you may want it back")
	}
	if !p.LiveAuditOK {
		w = append(w, "the current audit trail does not verify; the restore leaves it as it is")
	}
	w = append(w, "automatic e-mails and schedules will be paused after the restore until you resume them", "everyone will be signed out")
	return w
}

func (s *Service) canDecrypt(enc []byte) bool {
	if s.cipher == nil {
		return false
	}
	_, err := s.cipher.Decrypt(enc)
	return err == nil
}

// auditChain reads a whole audit trail, oldest first.
func (s *Service) auditChain(ctx context.Context, r ports.Repos) ([]domain.AuditEntry, error) {
	var all []domain.AuditEntry
	var after int64
	for {
		batch, err := r.Audit.Chain(ctx, after, 5000)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return all, nil
		}
		all = append(all, batch...)
		after = batch[len(batch)-1].Seq
	}
}

// reauthenticate checks the signed-in actor's current password. A disabled actor is
// unauthenticated.
func (s *Service) reauthenticate(ctx context.Context, password string) error {
	a := ActorFrom(ctx)
	u, err := s.tx.Repos().Users.Get(ctx, a.ID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !u.Active()) {
		return domain.ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	if ok, err := s.hasher.Verify(u.PasswordHash, password); err != nil || !ok {
		return &domain.ValidationError{Field: "currentPassword", Reason: "is not correct"}
	}
	return nil
}

// ApplyRestore swaps the staged backup into the live database in one exclusive
// transaction (ADR-010). It needs the actor's current password and the confirmation word.
// Afterwards everyone is signed out and automation is held until ResumeAutomation.
func (s *Service) ApplyRestore(ctx context.Context, id, currentPassword, confirmation string) (RestoreResult, error) {
	if err := s.restoresConfigured(); err != nil {
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	if err := s.reauthenticate(ctx, currentPassword); err != nil {
		s.auditRestoreOutcome(ctx, id, err)
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	if err := domain.CheckRestoreConfirmation(confirmation); err != nil {
		s.auditRestoreOutcome(ctx, id, err)
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	return s.applyRestore(ctx, id)
}

// ActorOperator is someone with a shell on the server running the cactusinv CLI.
var ActorOperator = Actor{ID: "system:operator", Name: "Operator (CLI)"}

// ApplyRestoreAsOperator applies a staged restore from the command line. Shell access to
// the server stands in for the password; the CLI asks for explicit confirmation (--yes).
func (s *Service) ApplyRestoreAsOperator(ctx context.Context, id string) (RestoreResult, error) {
	if err := s.restoresConfigured(); err != nil {
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	return s.applyRestore(WithActor(ctx, ActorOperator), id)
}

// NoteLegacyRestore records in the audit trail that a plaintext mariadb-dump backup was
// restored outside the application (deploy.sh, retired sidecar format): such a restore
// replaces audit_log wholesale, so this entry marks the break (ADR-009 amendment).
func (s *Service) NoteLegacyRestore(ctx context.Context, name string, r io.Reader) error {
	sum := newCountingHash()
	if _, err := io.Copy(sum, r); err != nil {
		return wrap("app.NoteLegacyRestore", err)
	}
	return wrap("app.NoteLegacyRestore", s.auditNow(WithActor(ctx, ActorOperator), "backup.legacy_restored", "backup", domain.TruncateUTF8(name, 64),
		fmt.Sprintf("a plaintext mariadb-dump backup (%d bytes, sha256 %s) was restored with deploy.sh; the audit trail before this entry came from that dump", sum.n, sum.Sum())))
}

func (s *Service) applyRestore(ctx context.Context, id string) (RestoreResult, error) {
	// The in-process slot only: the stager takes the installation-wide lock on the swap's
	// own connection (ADR-010).
	unlock, err := s.lockBackups()
	if err != nil {
		s.auditRestoreOutcome(ctx, id, err)
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	defer unlock()
	// A safety backup of the live data comes first, so a wrong restore can be undone
	// (ADR-010). Only for a stage that can be applied now.
	if st, err := s.tx.Repos().System.Get(ctx); err != nil || st.RestoreID != id || !st.HasStagedRestore(s.now()) {
		if err == nil {
			err = domain.ErrNotFound
		}
		s.auditRestoreOutcome(ctx, id, err)
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	safety := ""
	if s.safetyBackupPossible() {
		b, err := s.createBackup(ctx, domain.BackupPreRestore)
		if err != nil {
			s.log.ErrorContext(ctx, "safety backup before a restore failed", "err", err)
			err = fmt.Errorf("%w: %w", errSafetyBackup, err)
			s.auditRestoreOutcome(ctx, id, err)
			return RestoreResult{}, wrap("app.ApplyRestore", err)
		}
		safety = b.Name
	}
	res, err := s.apply(ctx, id, safety)
	var notes []string
	if safety != "" {
		notes = append(notes, "safety backup "+safety+" was taken first")
	}
	if err != nil && !restoreRefusal(err) {
		// The swap failed — or only its COMMIT reported an error. The swap writes its own
		// id into system_state, so its outcome is read from there.
		switch s.restoreOutcome(ctx, id) {
		case outcomeCommitted:
			s.log.WarnContext(ctx, "restore commit reported an error but the restore is in place", "source", res.Source)
			err = nil
		case outcomeUnknown:
			s.auditOutcomeUnknown(ctx, id, notes...)
			return RestoreResult{}, wrap("app.ApplyRestore", fmt.Errorf("%w: check the audit log and the data before retrying", domain.ErrOutcomeUnknown))
		}
	}
	if err != nil {
		s.auditRestoreOutcome(ctx, id, err, notes...)
		return RestoreResult{}, wrap("app.ApplyRestore", err)
	}
	// The staged plaintext copy goes even if the request was cancelled meanwhile.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	s.discardStagingIfIdle(cleanup)
	s.log.InfoContext(ctx, "restore applied", "source", res.Source, "audit_mode", string(res.Audit.Mode))
	return res, nil
}

// cleanupTimeout bounds work that runs detached from the request.
const cleanupTimeout = 5 * time.Minute

type restoreOutcome int

const (
	outcomeUnknown restoreOutcome = iota
	outcomeCommitted
	outcomeNotCommitted
)

// restoreOutcome reads whether the swap of restore id committed: only that swap writes id
// into HeldRestoreID (and consumes the stage); a stage still recorded under id means it
// did not. Anything else cannot be told apart.
func (s *Service) restoreOutcome(ctx context.Context, id string) restoreOutcome {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	st, err := s.tx.Repos().System.Get(ctx)
	switch {
	case err != nil:
		return outcomeUnknown
	case st.HeldRestoreID == id:
		return outcomeCommitted
	case st.RestoreID == id:
		return outcomeNotCommitted
	}
	return outcomeUnknown
}

// auditOutcomeUnknown records a restore whose result could not be determined.
func (s *Service) auditOutcomeUnknown(ctx context.Context, id string, notes ...string) {
	summary := "the restore's COMMIT failed and whether it took effect could not be determined; check the data and the audit log"
	for _, n := range notes {
		summary += "; " + n
	}
	if err := s.auditNow(context.WithoutCancel(ctx), "backup.restore_outcome_unknown", "backup", id, summary); err != nil {
		s.log.ErrorContext(ctx, "could not audit an unknown restore outcome", "err", err)
	}
}

// discardStagingIfIdle empties the staging database under the shared lock, unless another
// process holds the lock or a stage is recorded (another replica's fresh stage).
func (s *Service) discardStagingIfIdle(ctx context.Context) {
	release, err := s.backups.Stager.Lock(ctx)
	if err != nil {
		return
	}
	defer release()
	st, err := s.tx.Repos().System.Get(ctx)
	if err != nil || st.RestoreID != "" {
		return
	}
	if err := s.backups.Stager.Discard(ctx); err != nil {
		s.log.WarnContext(ctx, "could not empty the staging database", "err", err)
	}
}

// errSafetyBackup marks a restore stopped because the safety backup failed.
var errSafetyBackup = errors.New("the safety backup of the current data could not be taken")

// safetyBackupPossible reports whether a restore can first back up the live data (needs
// the server backup key and the backups directory).
func (s *Service) safetyBackupPossible() bool {
	b := s.backups
	return b.Sealer != nil && b.Sealer.KeyConfigured() && b.Files != nil && b.Snapshotter != nil
}

func (s *Service) apply(ctx context.Context, id, safety string) (RestoreResult, error) {
	res := RestoreResult{SafetyBackup: safety}
	err := s.backups.Stager.Apply(ctx, func(ctx context.Context, tx ports.RestoreTx) error {
		r := tx.Repos()
		st, err := r.System.Lock(ctx)
		if err != nil {
			return err
		}
		if st.RestoreID != id || !st.HasStagedRestore(s.now()) {
			return domain.ErrNotFound
		}
		res.Source = st.RestoreSource
		if n, err := r.Emails.CountSending(ctx, s.now()); err != nil {
			return err
		} else if n > 0 {
			return domain.ErrBusy
		}
		liveSeq, err := r.Sequences.Peek(ctx)
		if err != nil {
			return err
		}
		liveAudit, err := s.auditChain(ctx, r)
		if err != nil {
			return err
		}
		if err := tx.ReplaceTables(ctx); err != nil {
			return err
		}
		// Invoice numbers never rewind: numbers issued after the backup stay used.
		if backupSeq, err := r.Sequences.Peek(ctx); err != nil {
			return err
		} else if liveSeq > backupSeq {
			res.SequenceKept = true
			if err := r.Sequences.Set(ctx, liveSeq); err != nil {
				return err
			}
		}
		if err := r.Invoices.ResetExported(ctx); err != nil {
			return err
		}
		if res.SMTPPasswordCleared, err = s.clearForeignSMTPPassword(ctx, r); err != nil {
			return err
		}
		if err := r.Sessions.DeleteAll(ctx); err != nil {
			return err
		}
		if err := r.Idempotency.DeleteAll(ctx); err != nil {
			return err
		}
		backupAudit, err := tx.StagedAudit(ctx)
		if err != nil {
			return err
		}
		res.Audit = domain.PlanAuditMerge(liveAudit, backupAudit)
		if err := s.mergeAudit(ctx, r, res.Audit, backupAudit); err != nil {
			return err
		}
		actor := ActorFrom(ctx)
		st.Hold(s.now(), actor.Name, "restore of "+st.RestoreSource)
		st.HeldRestoreID = id
		header := st.RestoreHeader
		sha := st.RestoreSHA256
		st.ClearRestore()
		if err := r.System.Save(ctx, st); err != nil {
			return err
		}
		return s.audit(ctx, r, "backup.restored", "backup", res.Source, restoredSummary(res, header, sha, liveSeq))
	})
	return res, err
}

func restoredSummary(res RestoreResult, h domain.BackupHeader, sha string, liveSeq int64) string {
	parts := []string{
		fmt.Sprintf("restored %s (sha256 %s), taken %s by %s, schema %d", res.Source, sha, h.CreatedAt.Format(time.RFC3339), h.CreatedBy, h.SchemaVersion),
		"audit: " + res.Audit.Summary(),
	}
	if res.SequenceKept {
		parts = append(parts, fmt.Sprintf("invoice sequence kept at %d", liveSeq))
	}
	if res.SMTPPasswordCleared {
		parts = append(parts, "e-mail server password cleared")
	}
	if res.SafetyBackup != "" {
		parts = append(parts, "safety backup of the data before: "+res.SafetyBackup)
	} else {
		parts = append(parts, "no safety backup (no backup_key)")
	}
	parts = append(parts, "automation held, everyone signed out")
	return strings.Join(parts, "; ")
}

// clearForeignSMTPPassword clears a restored SMTP password this installation's app_key
// cannot decrypt.
func (s *Service) clearForeignSMTPPassword(ctx context.Context, r ports.Repos) (bool, error) {
	settings, enc, err := r.SMTP.Get(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil || enc == nil || s.canDecrypt(enc) {
		return false, err
	}
	return true, r.SMTP.Save(ctx, settings, nil)
}

// mergeAudit appends the plan's entries: verbatim ones unchanged, the others sealed onto
// the head.
func (s *Service) mergeAudit(ctx context.Context, r ports.Repos, p domain.AuditMergePlan, backup []domain.AuditEntry) error {
	a := ActorFrom(ctx)
	for _, e := range p.Appendix(backup, domain.AuditMarker{At: s.now(), ActorID: a.ID, ActorName: a.Name}) {
		var err error
		if p.Mode == domain.AuditAppendVerbatim {
			err = r.Audit.AppendVerbatim(ctx, e)
		} else {
			err = r.Audit.Append(ctx, &e)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// DiscardRestore forgets the staged restore and empties the staging database.
func (s *Service) DiscardRestore(ctx context.Context, id string) error {
	if err := s.restoresConfigured(); err != nil {
		return wrap("app.DiscardRestore", err)
	}
	unlock, err := s.lockRestore(ctx)
	if err != nil {
		return wrap("app.DiscardRestore", err)
	}
	defer unlock()
	var source string
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		st, err := r.System.Lock(ctx)
		if err != nil {
			return err
		}
		if st.RestoreID == "" || st.RestoreID != id {
			return domain.ErrNotFound
		}
		source = st.RestoreSource
		st.ClearRestore()
		if err := r.System.Save(ctx, st); err != nil {
			return err
		}
		return s.audit(ctx, r, "backup.restore_discarded", "backup", source, "staged restore discarded")
	})
	if err != nil {
		return wrap("app.DiscardRestore", err)
	}
	return wrap("app.DiscardRestore", s.backups.Stager.Discard(ctx))
}

// ResumeAutomation lets the dispatcher and the schedule runner work again after a restore.
func (s *Service) ResumeAutomation(ctx context.Context) error {
	return wrap("app.ResumeAutomation", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		st, err := r.System.Lock(ctx)
		if err != nil || !st.AutomationHeld {
			return err
		}
		reason := st.HeldReason
		st.Resume()
		if err := r.System.Save(ctx, st); err != nil {
			return err
		}
		return s.audit(ctx, r, "automation.resumed", "settings", "automation", "automatic e-mails and schedules resumed (held after "+reason+")")
	}))
}

// AutomationHeld reports whether automation is paused after a restore.
func (s *Service) AutomationHeld(ctx context.Context) (bool, error) {
	st, err := s.tx.Repos().System.Get(ctx)
	return st.AutomationHeld, wrap("app.AutomationHeld", err)
}

// discardExpiredRestore empties the staging database once a staged restore expired, and
// whenever no stage is recorded (a copy left behind, e.g. by a cancelled request or a
// crash). It skips the pass when a backup operation is running anywhere.
func (s *Service) discardExpiredRestore(ctx context.Context) error {
	if s.backups.Stager == nil {
		return nil
	}
	st, err := s.tx.Repos().System.Get(ctx)
	if err != nil || st.HasStagedRestore(s.now()) {
		return err
	}
	unlock, err := s.lockRestore(ctx)
	if errors.Is(err, domain.ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	if st.RestoreID != "" {
		// Re-check under the lock: the same stage, still expired.
		cleared, err := s.clearStage(ctx, st.RestoreID, true)
		if err != nil || !cleared {
			return err
		}
	} else if cur, err := s.tx.Repos().System.Get(ctx); err != nil || cur.RestoreID != "" {
		return err // staged meanwhile
	}
	return s.backups.Stager.Discard(ctx)
}
