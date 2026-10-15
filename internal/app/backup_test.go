package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

const adminPassword = "correct horse battery"

// backupHarness is a seeded installation with every backup port faked and a signed-in
// administrator in ctx.
type backupHarness struct {
	*harness
	files  *fakes.Files
	snap   *fakes.Snapshotter
	stager *fakes.Stager
	admin  domain.User
}

func newBackupHarness(t *testing.T, files *fakes.Files, key bool, cipher ports.Cipher) *backupHarness {
	t.Helper()
	return newBackupHarnessAt(t, start, files, key, cipher)
}

// newBackupHarnessAt seeds at another time: a different installation's audit trail.
func newBackupHarnessAt(t *testing.T, at time.Time, files *fakes.Files, key bool, cipher ports.Cipher) *backupHarness {
	t.Helper()
	h := newHarness(t)
	h.clock.Set(at)
	if files == nil {
		files = &fakes.Files{Clock: h.clock}
	}
	b := &backupHarness{harness: h, files: files, snap: &fakes.Snapshotter{Store: h.store}, stager: &fakes.Stager{Store: h.store}}
	jhb, _ := time.LoadLocation("Africa/Johannesburg")
	times, _ := domain.ParseBackupTimes("02:00")
	h.svc = app.New(app.Deps{
		Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: cipher,
		Backups: app.BackupDeps{
			Snapshotter: b.snap, Codec: fakes.Codec{}, Sealer: fakes.Sealer{Key: key}, Files: files, Stager: b.stager,
			Times: times, Location: jhb, RetentionDays: 14, AppVersion: "1.5.0", Build: "77",
		},
	})
	h.seed()
	b.admin = must(h.svc.CreateUser(h.ctx, "albie@example.com", adminPassword))
	h.ctx = app.WithActor(h.ctx, app.Actor{ID: b.admin.ID, Name: b.admin.Email})
	return b
}

func (b *backupHarness) lastAudit() domain.AuditEntry {
	a := b.store.Audit()
	return a[len(a)-1]
}

func (b *backupHarness) actions() []string {
	var out []string
	for _, e := range b.store.Audit() {
		out = append(out, e.Action)
	}
	return out
}

func TestCreateAndListBackups(t *testing.T) {
	b := newBackupHarness(t, nil, true, fakes.Cipher{})
	created := must(b.svc.CreateBackup(b.ctx, domain.BackupManual))
	n, err := domain.ParseBackupName(created.Name)
	if err != nil || n.Kind != domain.BackupManual || n.Schema != 13 || !n.CreatedAt.Equal(start) || created.Size == 0 || len(created.SHA256) != 64 {
		t.Fatalf("created = %+v (%v)", created, err)
	}
	if !bytes.HasPrefix(b.files.Data(created.Name), []byte("fake-age key\n")) {
		t.Fatal("stored backups are sealed with the server key")
	}
	if a := b.lastAudit(); a.Action != "backup.created" || a.EntityID != created.Name || a.ActorName != "albie@example.com" || !strings.Contains(a.Summary, created.SHA256) {
		t.Fatalf("audit = %+v", a)
	}
	if created.Digest.AuditHead.Seq == 0 {
		t.Fatal("the digest names the audit head")
	}
	b.files.Put("cactusinv-20261001-020000.sql.gz", []byte("legacy"), start.Add(-48*time.Hour))
	b.files.Put("notes.txt", []byte("ignored"), start)
	ov := must(b.svc.ListBackups(b.ctx))
	if !ov.KeyConfigured || ov.TimeZone != "Africa/Johannesburg" || ov.RetentionDays != 14 || ov.RestoreUnavailable != "" || ov.LastRun != nil {
		t.Fatalf("overview = %+v", ov)
	}
	if want := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC); !ov.NextRun.Equal(want) {
		t.Fatalf("next run %v, want 02:00 SAST tomorrow", ov.NextRun)
	}
	if len(ov.Files) != 2 || ov.Files[0].Name != created.Name || !ov.Files[1].Legacy || ov.Files[0].Schema != 13 {
		t.Fatalf("files = %+v", ov.Files)
	}
	b.stager.NotReady = errors.New("access denied")
	if ov := must(b.svc.ListBackups(b.ctx)); !strings.Contains(ov.RestoreUnavailable, "staging database") {
		t.Fatalf("restore unavailable: %q", ov.RestoreUnavailable)
	}
}

func TestBackupUseCaseErrors(t *testing.T) {
	cases := []struct {
		name string
		run  func(b *backupHarness) error
		want error
	}{
		{"no key", func(*backupHarness) error {
			nb := newBackupHarness(t, nil, false, nil)
			_, err := nb.svc.CreateBackup(nb.ctx, domain.BackupManual)
			return err
		}, domain.ErrNotConfigured},
		{"not configured", func(b *backupHarness) error {
			svc := app.New(app.Deps{Tx: b.store, Clock: b.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}})
			_, err := svc.CreateBackup(b.ctx, domain.BackupManual)
			return err
		}, domain.ErrNotConfigured},
		{"snapshot fails", func(b *backupHarness) error {
			b.snap.Err = errors.New("db down")
			_, err := b.svc.CreateBackup(b.ctx, domain.BackupManual)
			b.snap.Err = nil
			return err
		}, nil},
		{"commit fails", func(b *backupHarness) error {
			b.files.FailCommit = errors.New("disk full")
			_, err := b.svc.CreateBackup(b.ctx, domain.BackupManual)
			if fs := must(b.files.List(b.ctx)); len(fs) != 0 {
				t.Fatal("a failed backup leaves no file")
			}
			return err
		}, nil},
		{"busy", func(b *backupHarness) error {
			var inner error
			err := b.svc.ExportBackup(b.ctx, "a long enough passphrase", func(string) (io.Writer, error) {
				_, inner = b.svc.CreateBackup(b.ctx, domain.BackupManual)
				return io.Discard, nil
			})
			if err != nil {
				return err
			}
			return inner
		}, domain.ErrBusy},
		{"short passphrase", func(b *backupHarness) error {
			return b.svc.ExportBackup(b.ctx, "too short", func(string) (io.Writer, error) { return io.Discard, nil })
		}, domain.ErrValidation},
		{"export not configured", func(b *backupHarness) error {
			svc := app.New(app.Deps{Tx: b.store, Clock: b.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}})
			return svc.ExportBackup(b.ctx, "a long enough passphrase", nil)
		}, domain.ErrNotConfigured},
		{"upload not configured", func(b *backupHarness) error {
			svc := app.New(app.Deps{Tx: b.store, Clock: b.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}})
			_, err := svc.UploadBackup(b.ctx, strings.NewReader("x"))
			return err
		}, domain.ErrNotConfigured},
		{"upload read fails", func(b *backupHarness) error {
			_, err := b.svc.UploadBackup(b.ctx, io.MultiReader(strings.NewReader("x"), errReader{}))
			return err
		}, nil},
		{"open fails", func(b *backupHarness) error {
			return b.svc.ExportBackup(b.ctx, "a long enough passphrase", func(string) (io.Writer, error) { return nil, errors.New("client gone") })
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBackupHarness(t, nil, true, nil)
			err := c.run(b)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestExportBackupAuditsBeforeStreaming(t *testing.T) {
	b := newBackupHarness(t, nil, false, nil) // downloads work without the server key
	var buf bytes.Buffer
	var gotName string
	err := b.svc.ExportBackup(b.ctx, "a long enough passphrase", func(name string) (io.Writer, error) {
		gotName = name
		if a := b.lastAudit(); a.Action != "backup.exported" || a.EntityID != name {
			t.Fatalf("not audited before streaming: %+v", a)
		}
		return &buf, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.ParseBackupName(gotName); err != nil || !strings.HasPrefix(buf.String(), "fake-age scrypt ") {
		t.Fatalf("name %q, data %q", gotName, buf.String()[:20])
	}
	if fs := must(b.files.List(b.ctx)); len(fs) != 0 {
		t.Fatal("an export is not stored")
	}
	info := must(b.svc.UploadBackup(b.ctx, &buf))
	if info.Kind != domain.BackupUpload || info.Size == 0 || b.lastAudit().Action != "backup.uploaded" {
		t.Fatalf("upload = %+v", info)
	}
}

func TestRunDueBackupsOncePerSlot(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	// 08:00 SAST: today's 02:00 slot is due (00:00 UTC).
	if ran := must(b.svc.RunDueBackups(context.Background())); !ran {
		t.Fatal("first pass takes the due slot")
	}
	if ran := must(b.svc.RunDueBackups(context.Background())); ran {
		t.Fatal("a slot runs once")
	}
	ov := must(b.svc.ListBackups(b.ctx))
	if ov.LastRun == nil || ov.LastRun.Result != domain.BackupRunOK || !ov.LastRun.Slot.Equal(time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)) || len(ov.Files) != 1 || ov.Files[0].Kind != domain.BackupScheduled {
		t.Fatalf("overview = %+v", ov)
	}
	if a := b.lastAudit(); a.Action != "backup.created" || a.ActorName != "Backup scheduler" {
		t.Fatalf("audit = %+v", a)
	}
	b.clock.Advance(24 * time.Hour)
	b.files.FailCommit = errors.New("disk full")
	if ran, err := b.svc.RunDueBackups(context.Background()); !ran || err == nil {
		t.Fatalf("failed slot: %v %v", ran, err)
	}
	if ov := must(b.svc.ListBackups(b.ctx)); ov.LastRun.Result != domain.BackupRunFailed || ov.LastRun.Error != "disk" {
		t.Fatalf("failed run recorded: %+v", ov.LastRun)
	}
	if !slices.Contains(b.actions(), "backup.failed") {
		t.Fatal("failure audited")
	}
	if ran := must(b.svc.RunDueBackups(context.Background())); ran {
		t.Fatal("a failed slot is not retried; the next slot is")
	}
	nokey := newBackupHarness(t, nil, false, nil)
	if ran := must(nokey.svc.RunDueBackups(context.Background())); ran {
		t.Fatal("no key, no scheduled backups")
	}
}

func TestPruneBackups(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	old := domain.BackupName{CreatedAt: start.AddDate(0, 0, -20), Schema: 13, Kind: domain.BackupScheduled}.String()
	older := domain.BackupName{CreatedAt: start.AddDate(0, 0, -30), Schema: 13, Kind: domain.BackupManual}.String()
	b.files.Put(old, []byte("x"), start)
	b.files.Put(older, []byte("x"), start)
	b.files.Put("cactusinv-20260701-020000.sql.gz", []byte("x"), start.AddDate(0, 0, -40))
	fresh := must(b.svc.CreateBackup(b.ctx, domain.BackupManual)).Name
	removed := must(b.svc.PruneBackups(b.ctx))
	if len(removed) != 3 || slices.Contains(removed, fresh) {
		t.Fatalf("removed = %v", removed)
	}
	if a := b.lastAudit(); a.Action != "backup.pruned" || !strings.Contains(a.Summary, "removed 3 backups") {
		t.Fatalf("one audit entry per pass: %+v", a)
	}
	n := len(b.store.Audit())
	if removed := must(b.svc.PruneBackups(b.ctx)); len(removed) != 0 || len(b.store.Audit()) != n {
		t.Fatal("nothing to prune, nothing audited")
	}
}

// ---- restore ----

// backupNow takes a stored backup of b and returns its name.
func (b *backupHarness) backupNow() string {
	return must(b.svc.CreateBackup(b.ctx, domain.BackupManual)).Name
}

func (b *backupHarness) saveSMTPPassword() {
	st := must(b.svc.GetSMTPSettings(b.ctx)).Settings
	st.Enabled, st.Username = true, "me@gmail.com"
	pw := "abcd efgh ijkl mnop"
	must(b.svc.UpdateSMTPSettings(b.ctx, st, &pw))
}

func TestStageAndApplyRestore(t *testing.T) {
	b := newBackupHarness(t, nil, true, fakes.Cipher{})
	b.saveSMTPPassword()
	name := b.backupNow()
	seqAtBackup := must(b.svc.GetBusiness(b.ctx)).NextSequence

	// After the backup: two invoices issued (numbers 1195, 1196 used) and a user added.
	b.clock.Advance(time.Hour)
	for range 2 {
		inv := must(b.svc.CreateInvoice(b.ctx, b.referenceInput()))
		must(b.svc.IssueInvoice(b.ctx, inv.ID, domain.Date{}))
	}
	must(b.svc.CreateUser(b.ctx, "later@example.com", "another long password"))
	if _, _, err := b.svc.Login(b.ctx, "later@example.com", "another long password"); err != nil {
		t.Fatal(err)
	}

	p := must(b.svc.StageRestore(b.ctx, name, ""))
	if p.ID == "" || p.Source != name || len(p.SHA256) != 64 || p.SchemaFrom != 13 || p.SchemaTo != 13 || !p.ExpiresAt.Equal(b.clock.Now().Add(30*time.Minute)) {
		t.Fatalf("preview = %+v", p)
	}
	if p.InvoicesSince != 2 || !p.CanSignIn || p.SMTPPasswordCleared || p.Audit.Mode != domain.AuditKeepCurrent || !p.LiveAuditOK {
		t.Fatalf("preview facts = %+v", p)
	}
	if i := slices.IndexFunc(p.Tables, func(c app.RestoreTableCount) bool { return c.Name == "users" }); i < 0 || p.Tables[i].Backup != 1 || p.Tables[i].Live != 2 {
		t.Fatalf("counts = %+v", p.Tables)
	}
	if !strings.Contains(strings.Join(p.Warnings, "\n"), "2 invoices were created after") {
		t.Fatalf("warnings = %v", p.Warnings)
	}
	if again := must(b.svc.CurrentRestore(b.ctx)); again.ID != p.ID {
		t.Fatal("current restore")
	}

	refusals := []struct {
		name, password, confirm string
		want                    error
	}{
		{"wrong password", "nope", "RESTORE", domain.ErrValidation},
		{"wrong confirmation", adminPassword, "restore", domain.ErrValidation},
	}
	for _, r := range refusals {
		before := len(b.store.Audit())
		if _, err := b.svc.ApplyRestore(b.ctx, p.ID, r.password, r.confirm); !errors.Is(err, r.want) {
			t.Fatalf("%s: %v", r.name, err)
		}
		if a := b.store.Audit(); len(a) != before+1 || a[before].Action != "backup.restore_refused" {
			t.Fatalf("%s: not audited", r.name)
		}
	}
	if _, err := b.svc.ApplyRestore(b.ctx, "other-id", adminPassword, "RESTORE"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}

	// An e-mail being delivered right now blocks the swap.
	inv, _ := must3(b.svc.ListInvoices(b.ctx, ports.InvoiceFilter{}))
	m := must(b.svc.SendInvoice(b.ctx, inv[0].ID, app.SendInput{IdempotencyKey: "k1"}))
	b.store.SetSending(m.ID, b.clock.Now())
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
	if b.lastAudit().Action != "backup.restore_refused" || must(b.store.Repos().Users.Count(b.ctx)) != 2 {
		t.Fatal("busy refusal audited, live data unchanged")
	}
	b.store.SetSending(m.ID, b.clock.Now().Add(-time.Hour)) // a crashed dispatcher's stale row

	res := must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, " RESTORE "))
	if !res.SequenceKept || res.SMTPPasswordCleared || res.Audit.Mode != domain.AuditKeepCurrent || res.Source != name {
		t.Fatalf("result = %+v", res)
	}
	if next := must(b.svc.GetBusiness(b.ctx)).NextSequence; next != seqAtBackup+2 {
		t.Fatalf("sequence rewound: %d (backup %d)", next, seqAtBackup)
	}
	if users := must(b.store.Repos().Users.Count(b.ctx)); users != 1 {
		t.Fatalf("users replaced: %v", users)
	}
	if v := must(b.svc.GetSMTPSettings(b.ctx)); !v.PasswordSet {
		t.Fatal("this installation's SMTP password is kept")
	}
	if held := must(b.svc.AutomationHeld(b.ctx)); !held {
		t.Fatal("automation held")
	}
	if a := b.lastAudit(); a.Action != "backup.restored" || !strings.Contains(a.Summary, name) || !strings.Contains(a.Summary, "invoice sequence kept") {
		t.Fatalf("audit = %+v", a)
	}
	if v := must(b.svc.VerifyAudit(b.ctx)); !v.OK {
		t.Fatalf("chain = %+v", v)
	}
	if _, err := b.svc.CurrentRestore(b.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("the stage is consumed")
	}
	if res := must(b.svc.DispatchEmails(b.ctx, 5)); res.Sent != 0 {
		t.Fatal("nothing is sent while held")
	}
	if res := must(b.svc.RunDueSchedules(b.ctx, 5)); res.Issued != 0 {
		t.Fatal("no schedule runs while held")
	}
	must0(t, b.svc.ResumeAutomation(b.ctx))
	if held := must(b.svc.AutomationHeld(b.ctx)); held || b.lastAudit().Action != "automation.resumed" {
		t.Fatal("resumed and audited")
	}
	n := len(b.store.Audit())
	must0(t, b.svc.ResumeAutomation(b.ctx))
	if len(b.store.Audit()) != n {
		t.Fatal("resuming twice changes nothing")
	}
}

func must0(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSignsEveryoneOut(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	tok, _, err := b.svc.Login(b.ctx, "albie@example.com", adminPassword)
	must0(t, err)
	must0(t, b.svc.Idempotency().Reserve(b.ctx, ports.IdempotencyRecord{Key: "k", Fingerprint: "f", CreatedAt: b.clock.Now()}))
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
	if _, err := b.svc.Authenticate(b.ctx, tok); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("sessions revoked")
	}
	if _, err := b.svc.Idempotency().Get(b.ctx, "k"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("idempotency keys deleted")
	}
}

func TestRestoreRefusesBadBackups(t *testing.T) {
	cases := []struct {
		name       string
		prepare    func(b *backupHarness) (file, passphrase string)
		want       error
		wantAction string
	}{
		{"tampered", func(b *backupHarness) (string, string) {
			name := b.backupNow()
			data := b.files.Data(name)
			data[len(data)/2] ^= 0x01
			b.files.Put(name, data, b.clock.Now())
			return name, ""
		}, domain.ErrBackupInvalid, "backup.restore_refused"},
		{"truncated", func(b *backupHarness) (string, string) {
			name := b.backupNow()
			data := b.files.Data(name)
			b.files.Put(name, data[:len(data)-20], b.clock.Now())
			return name, ""
		}, domain.ErrBackupInvalid, "backup.restore_refused"},
		{"wrong passphrase", func(b *backupHarness) (string, string) {
			var buf bytes.Buffer
			must0(t, b.svc.ExportBackup(b.ctx, "the right passphrase!", func(string) (io.Writer, error) { return &buf, nil }))
			return must(b.svc.UploadBackup(b.ctx, &buf)).Name, "a wrong passphrase!!"
		}, domain.ErrBackupInvalid, "backup.restore_refused"},
		{"too new", func(b *backupHarness) (string, string) {
			b.snap.Schema = 14
			return b.backupNow(), ""
		}, domain.ErrBackupTooNew, "backup.restore_refused"},
		{"missing", func(*backupHarness) (string, string) {
			return "cactusinv-20260101T000000Z-s13-manual.age", ""
		}, domain.ErrNotFound, "backup.restore_refused"},
		{"not a name", func(*backupHarness) (string, string) { return "../../etc/passwd", "" }, domain.ErrValidation, ""},
		{"staging unavailable", func(b *backupHarness) (string, string) {
			name := b.backupNow()
			b.stager.NotReady = errors.New("access denied for database cactusinv_restore")
			return name, ""
		}, nil, "backup.restore_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBackupHarness(t, nil, true, nil)
			file, pass := c.prepare(b)
			_, err := b.svc.StageRestore(b.ctx, file, pass)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.wantAction != "" && b.lastAudit().Action != c.wantAction {
				t.Fatalf("audit = %+v", b.lastAudit())
			}
			if _, err := b.svc.CurrentRestore(b.ctx); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal("nothing staged")
			}
		})
	}
}

func TestRestoreClearsAForeignSMTPPassword(t *testing.T) {
	src := newBackupHarness(t, nil, true, fakes.Cipher{})
	src.saveSMTPPassword()
	name := src.backupNow()
	// Another installation (its app_key cannot decrypt the password) restores the file.
	dst := newBackupHarnessAt(t, start.Add(time.Hour), src.files, true, otherCipher{})
	p := must(dst.svc.StageRestore(dst.ctx, name, ""))
	if !p.SMTPPasswordCleared || !strings.Contains(strings.Join(p.Warnings, "\n"), "password in this backup cannot be decrypted") {
		t.Fatalf("preview = %+v", p)
	}
	res := must(dst.svc.ApplyRestore(dst.ctx, p.ID, adminPassword, "RESTORE"))
	if !res.SMTPPasswordCleared || !strings.Contains(dst.lastAudit().Summary, "e-mail server password cleared") {
		t.Fatalf("result = %+v", res)
	}
	if v := must(dst.svc.GetSMTPSettings(dst.ctx)); v.PasswordSet {
		t.Fatal("password cleared")
	}
}

type otherCipher struct{}

func (otherCipher) Encrypt(p []byte) ([]byte, error) { return append([]byte("other:"), p...), nil }
func (otherCipher) Decrypt([]byte) ([]byte, error) {
	return nil, errors.New("cipher: message authentication failed")
}

// TestRestoreAuditMergeModes restores the same backup into three live trails.
func TestRestoreAuditMergeModes(t *testing.T) {
	src := newBackupHarness(t, nil, true, nil)
	backupTrail := src.store.Audit() // what the backup contains (not its own backup.created)
	name := src.backupNow()

	// keep_current: the live trail already holds every backup entry (shown above too).
	if p := must(src.svc.StageRestore(src.ctx, name, "")); p.Audit.Mode != domain.AuditKeepCurrent {
		t.Fatalf("keep: %+v", p.Audit)
	}

	// append_verbatim: an empty live trail (a wiped database).
	empty := fakes.NewStore()
	stager := &fakes.Stager{Store: empty}
	clock := &fakes.Clock{T: start.Add(time.Hour)}
	svc := app.New(app.Deps{Tx: empty, Clock: clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}, Backups: app.BackupDeps{
		Snapshotter: &fakes.Snapshotter{Store: empty}, Codec: fakes.Codec{}, Sealer: fakes.Sealer{Key: true}, Files: src.files, Stager: stager,
	}})
	// The operator is signed in with an account that exists in the backup.
	ctx := app.WithActor(context.Background(), app.Actor{ID: src.admin.ID, Name: src.admin.Email})
	p := must(svc.StageRestore(ctx, name, ""))
	if p.Audit.Mode != domain.AuditAppendVerbatim || p.Audit.FromSeq != 1 || p.Audit.Count != len(backupTrail) || !p.CanSignIn {
		t.Fatalf("verbatim preview: %+v", p.Audit)
	}
	// The wiped database has no users yet: re-authentication is against the restored rows
	// only after the restore, so the operator must exist live. Seed the same account.
	must0(t, empty.Repos().Users.Create(ctx, domain.User{ID: src.admin.ID, Email: src.admin.Email, PasswordHash: "hash:" + adminPassword}))
	res := must(svc.ApplyRestore(ctx, p.ID, adminPassword, "RESTORE"))
	trail := empty.Audit()
	if res.Audit.Mode != domain.AuditAppendVerbatim || len(trail) != len(backupTrail)+1 {
		t.Fatalf("verbatim: %d entries, want %d + backup.restored", len(trail), len(backupTrail))
	}
	for i, e := range backupTrail {
		if trail[i] != e {
			t.Fatalf("entry #%d not verbatim", e.Seq)
		}
	}
	if v := must(svc.VerifyAudit(ctx)); !v.OK {
		t.Fatalf("verbatim chain: %+v", v)
	}

	// append_resealed: a trail that diverged (another installation with its own history).
	other := newBackupHarnessAt(t, start.Add(time.Hour), src.files, true, nil)
	before := other.store.Audit()
	other.ctx = app.WithActor(other.ctx, app.Actor{ID: other.admin.ID, Name: other.admin.Email})
	p = must(other.svc.StageRestore(other.ctx, name, ""))
	if p.Audit.Mode != domain.AuditAppendResealed || p.Audit.Count != len(backupTrail) || p.Audit.CommonSeq != 0 {
		t.Fatalf("resealed preview: %+v", p.Audit)
	}
	must(other.svc.ApplyRestore(other.ctx, p.ID, adminPassword, "RESTORE"))
	after := other.store.Audit()
	for i, e := range before {
		if after[i] != e {
			t.Fatalf("live entry #%d changed", e.Seq)
		}
	}
	tail := after[len(before):]
	if tail[0].Action != domain.AuditResealBegin || tail[len(backupTrail)+1].Action != domain.AuditResealEnd || tail[len(tail)-1].Action != "backup.restored" {
		t.Fatalf("markers: %s … %s", tail[0].Action, tail[len(tail)-1].Action)
	}
	for i, e := range backupTrail {
		got := tail[i+1]
		if got.Summary != e.Summary || got.Action != e.Action || !got.At.Equal(e.At) || got.Hash == e.Hash {
			t.Fatalf("re-sealed entry %d: %+v vs %+v", i, got, e)
		}
	}
	if v := must(other.svc.VerifyAudit(other.ctx)); !v.OK {
		t.Fatalf("resealed chain: %+v", v)
	}
}

func TestRestoreRollsBackOnFailure(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	must(b.svc.CreateUser(b.ctx, "later@example.com", "another long password"))
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	b.stager.FailReplace = errors.New("lock wait timeout")
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); err == nil {
		t.Fatal("must fail")
	}
	if users := must(b.store.Repos().Users.Count(b.ctx)); users != 2 {
		t.Fatal("live data unchanged after a failed swap")
	}
	if held := must(b.svc.AutomationHeld(b.ctx)); held {
		t.Fatal("not held after a failed swap")
	}
	if b.lastAudit().Action != "backup.restore_failed" {
		t.Fatalf("audit = %+v", b.lastAudit())
	}
	b.stager.FailReplace = nil
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); err != nil {
		t.Fatalf("the stage survives a failed attempt: %v", err)
	}
}

func TestStagedRestoreExpiresAndDiscards(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	if err := b.svc.DiscardRestore(b.ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("wrong id: %v", err)
	}
	must0(t, b.svc.DiscardRestore(b.ctx, p.ID))
	if b.lastAudit().Action != "backup.restore_discarded" {
		t.Fatal("discard audited")
	}
	if _, err := b.stager.Staged(b.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("staging emptied")
	}

	p = must(b.svc.StageRestore(b.ctx, name, ""))
	b.clock.Advance(domain.RestoreStageTTL + time.Second)
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
	must0(t, b.svc.Housekeeping(b.ctx))
	if _, err := b.stager.Staged(b.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("housekeeping discards an expired stage")
	}
	if st := must(b.store.Repos().System.Get(b.ctx)); st.RestoreID != "" {
		t.Fatal("state cleared")
	}
}

func TestRestorePreconditions(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	// A system actor cannot re-authenticate.
	if _, err := b.svc.ApplyRestore(context.Background(), p.ID, adminPassword, "RESTORE"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("no user: %v", err)
	}
	plain := app.New(app.Deps{Tx: b.store, Clock: b.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}})
	for name, err := range map[string]error{
		"stage":   func() error { _, err := plain.StageRestore(b.ctx, name, ""); return err }(),
		"current": func() error { _, err := plain.CurrentRestore(b.ctx); return err }(),
		"apply":   func() error { _, err := plain.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); return err }(),
		"discard": plain.DiscardRestore(b.ctx, p.ID),
	} {
		if !errors.Is(err, domain.ErrNotConfigured) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if ov := must(plain.ListBackups(b.ctx)); ov.RestoreUnavailable == "" || ov.KeyConfigured || len(ov.Files) != 0 {
		t.Fatalf("unconfigured overview: %+v", ov)
	}
	must0(t, plain.Housekeeping(b.ctx))
}

func TestPruneSummaryIsBounded(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	for i := range 50 {
		b.files.Put(domain.BackupName{CreatedAt: start.AddDate(0, 0, -100+i), Schema: 13, Kind: domain.BackupScheduled}.String(), []byte("x"), start)
	}
	b.backupNow()
	removed := must(b.svc.PruneBackups(b.ctx))
	if len(removed) != 50 {
		t.Fatalf("removed %d", len(removed))
	}
	a := b.lastAudit()
	if a.Action != "backup.pruned" || len(a.Summary) > 300 || !strings.Contains(a.Summary, "removed 50 backups") ||
		!strings.Contains(a.Summary, removed[0]) || !strings.Contains(a.Summary, removed[49]) {
		t.Fatalf("summary (%d bytes): %q", len(a.Summary), a.Summary)
	}
}

func TestUploadNamesNeverCollide(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	first := must(b.svc.UploadBackup(b.ctx, strings.NewReader("a")))
	second := must(b.svc.UploadBackup(b.ctx, strings.NewReader("b"))) // same clock second
	if first.Name == second.Name || !second.CreatedAt.Equal(first.CreatedAt.Add(time.Second)) {
		t.Fatalf("%s / %s", first.Name, second.Name)
	}
}

// slowWriter stalls on its first write, like a client that stopped reading.
type slowWriter struct{ n int }

func (w *slowWriter) Write(p []byte) (int, error) {
	if w.n == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	w.n++
	return len(p), nil
}

func TestExportIsTimeBounded(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	svc := app.New(app.Deps{Tx: b.store, Clock: b.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}, Backups: app.BackupDeps{
		Snapshotter: b.snap, Codec: fakes.Codec{}, Sealer: fakes.Sealer{}, ExportTimeout: 10 * time.Millisecond,
	}})
	err := svc.ExportBackup(b.ctx, "a long enough passphrase", func(string) (io.Writer, error) { return &slowWriter{}, nil })
	if err == nil || !strings.Contains(err.Error(), "took too long") {
		t.Fatalf("err = %v", err)
	}
}

func TestStageRefusesTrailingData(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	data := append(b.files.Data(name), []byte(`{"table":"users","row":[]}`+"\n")...)
	up := must(b.svc.UploadBackup(b.ctx, bytes.NewReader(data)))
	// Re-seal check: an upload keeps the server-key prefix, so the key opens it.
	if _, err := b.svc.StageRestore(b.ctx, up.Name, ""); !errors.Is(err, domain.ErrBackupInvalid) {
		t.Fatalf("err = %v", err)
	}
}

// TestUploadedBackupIsNeverVerbatim: the upload is audited (with the file's SHA-256)
// before it can be staged, so the live head is an entry no uploaded file can contain and
// the file's entries can only be re-sealed, never passed off as the live trail's own.
func TestUploadedBackupIsNeverVerbatim(t *testing.T) {
	src := newBackupHarness(t, nil, true, nil)
	name := src.backupNow()
	data := src.files.Data(name)
	// A fresh installation whose trail is still empty receives the file.
	empty := fakes.NewStore()
	files := &fakes.Files{Clock: src.clock}
	svc := app.New(app.Deps{Tx: empty, Clock: src.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}, Backups: app.BackupDeps{
		Snapshotter: &fakes.Snapshotter{Store: empty}, Codec: fakes.Codec{}, Sealer: fakes.Sealer{Key: true}, Files: files, Stager: &fakes.Stager{Store: empty},
	}})
	ctx := app.WithActor(context.Background(), app.Actor{ID: src.admin.ID, Name: src.admin.Email})
	up := must(svc.UploadBackup(ctx, bytes.NewReader(data)))
	if a := empty.Audit(); len(a) != 1 || a[0].Action != "backup.uploaded" {
		t.Fatalf("upload audited first: %+v", a)
	}
	p := must(svc.StageRestore(ctx, up.Name, ""))
	if p.Audit.Mode != domain.AuditAppendResealed {
		t.Fatalf("an uploaded backup was planned %s", p.Audit.Mode)
	}
}

// TestBackupLockIsSharedAcrossProcesses: while another process holds the backup lock,
// stage, apply, discard are refused as busy and housekeeping leaves the stage alone.
func TestBackupLockIsSharedAcrossProcesses(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	release := b.stager.HoldLock()
	if _, err := b.svc.StageRestore(b.ctx, name, ""); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("stage: %v", err)
	}
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("apply: %v", err)
	}
	if err := b.svc.DiscardRestore(b.ctx, p.ID); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("discard: %v", err)
	}
	b.clock.Advance(domain.RestoreStageTTL + time.Second)
	must0(t, b.svc.Housekeeping(b.ctx))
	if st := must(b.store.Repos().System.Get(b.ctx)); st.RestoreID != p.ID {
		t.Fatal("housekeeping must skip while another process works")
	}
	release()
	must0(t, b.svc.Housekeeping(b.ctx))
	if st := must(b.store.Repos().System.Get(b.ctx)); st.RestoreID != "" {
		t.Fatal("expired stage cleared once the lock is free")
	}
}

// TestNewStageClearsTheOldOneFirst: a failed stage leaves no applicable restore behind.
func TestNewStageClearsTheOldOneFirst(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	if _, err := b.svc.StageRestore(b.ctx, "cactusinv-20200101T000000Z-s13-manual.age", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the old stage must be gone: %v", err)
	}
}

// TestBackupByALongMultibyteName: the header's 254-byte limit is met without splitting a
// character, so the backup is not refused.
func TestBackupByALongMultibyteName(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	ctx := app.WithActor(b.ctx, app.Actor{ID: b.admin.ID, Name: strings.Repeat("ü", 200)})
	name := must(b.svc.CreateBackup(ctx, domain.BackupManual)).Name
	if p := must(b.svc.StageRestore(b.ctx, name, "")); len(p.Header.CreatedBy) != 254 {
		t.Fatalf("created by %d bytes", len(p.Header.CreatedBy))
	}
}

// TestRestoreCommitOutcome: when the swap reports a COMMIT error, its own id in the system
// state decides — never the backup's name, which an earlier restore may share.
func TestRestoreCommitOutcome(t *testing.T) {
	commitErr := errors.New("invalid connection")
	cases := []struct {
		name string
		// earlier restores the same backup first and leaves automation held (not resumed).
		earlier     bool
		afterCommit bool // the COMMIT went through
		meddle      bool // another replica clears the stage before the check
		wantErr     error
		wantAction  string
		wantApplied bool
	}{
		{name: "committed, ack lost", afterCommit: true, wantAction: "backup.restored", wantApplied: true},
		{name: "not committed", wantErr: commitErr, wantAction: "backup.restore_failed"},
		{name: "not committed after an earlier unresumed restore of the same backup", earlier: true, wantErr: commitErr, wantAction: "backup.restore_failed"},
		{name: "cannot tell", meddle: true, wantErr: domain.ErrOutcomeUnknown, wantAction: "backup.restore_outcome_unknown"},
		{name: "cannot tell after an earlier restore", earlier: true, meddle: true, wantErr: domain.ErrOutcomeUnknown, wantAction: "backup.restore_outcome_unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBackupHarness(t, nil, true, nil)
			name := b.backupNow()
			if c.earlier {
				p := must(b.svc.StageRestore(b.ctx, name, ""))
				must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
			}
			must(b.svc.CreateUser(b.ctx, "later@example.com", "another long password"))
			p := must(b.svc.StageRestore(b.ctx, name, ""))
			if c.afterCommit {
				b.stager.CommitErrAfterCommit = commitErr
			} else {
				b.stager.CommitErrWithoutCommit = commitErr
			}
			if c.meddle {
				b.stager.AfterCommitError = func() {
					must0(t, b.store.WithinTx(b.ctx, func(ctx context.Context, r ports.Repos) error {
						st := must(r.System.Lock(ctx))
						st.ClearRestore()
						return r.System.Save(ctx, st)
					}))
				}
			}
			_, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE")
			if (c.wantErr == nil) != (err == nil) || (c.wantErr != nil && !errors.Is(err, c.wantErr)) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if a := b.lastAudit(); a.Action != c.wantAction {
				t.Fatalf("audit = %s, want %s", a.Action, c.wantAction)
			}
			if n := must(b.store.Repos().Users.Count(b.ctx)); (n == 1) != c.wantApplied {
				t.Fatalf("users = %d, applied = %v", n, c.wantApplied)
			}
		})
	}
}

// TestStagingCopyNeverLingers: housekeeping empties a staging database no stage is
// recorded for; the post-apply discard leaves another replica's fresh stage alone.
func TestStagingCopyNeverLingers(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	must(b.svc.StageRestore(b.ctx, name, ""))
	// The record went (e.g. a cancelled request) but the plaintext copy stayed.
	must0(t, b.store.WithinTx(b.ctx, func(ctx context.Context, r ports.Repos) error {
		st := must(r.System.Lock(ctx))
		st.ClearRestore()
		return r.System.Save(ctx, st)
	}))
	must0(t, b.svc.Housekeeping(b.ctx))
	if _, err := b.stager.Staged(b.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("orphaned staging copy not emptied")
	}

	p := must(b.svc.StageRestore(b.ctx, name, ""))
	b.stager.CommitErrAfterCommit = errors.New("invalid connection")
	b.stager.AfterCommitError = func() { // another replica records a fresh stage
		must0(t, b.store.WithinTx(b.ctx, func(ctx context.Context, r ports.Repos) error {
			st := must(r.System.Lock(ctx))
			st.RestoreID, st.RestoreExpiresAt = "other-stage", b.clock.Now().Add(time.Hour)
			return r.System.Save(ctx, st)
		}))
	}
	must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
	if _, err := b.stager.Staged(b.ctx); err != nil {
		t.Fatal("another replica's recorded stage must not be discarded")
	}
}

// barrierReader blocks its first read until every reader has started.
type barrierReader struct {
	wg   *sync.WaitGroup
	once sync.Once
	r    io.Reader
}

func (b *barrierReader) Read(p []byte) (int, error) {
	b.once.Do(func() { b.wg.Done(); b.wg.Wait() })
	return b.r.Read(p)
}

func TestConcurrentUploadsInOneSecond(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	var started, done sync.WaitGroup
	started.Add(2)
	names := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		done.Add(1)
		go func() {
			defer done.Done()
			info, err := b.svc.UploadBackup(b.ctx, &barrierReader{wg: &started, r: strings.NewReader(fmt.Sprint("upload ", i))})
			names[i], errs[i] = info.Name, err
		}()
	}
	done.Wait()
	if errs[0] != nil || errs[1] != nil || names[0] == names[1] {
		t.Fatalf("names %v, errors %v", names, errs)
	}
}

func TestOperatorRestoreAndLegacyNote(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	p := must(b.svc.StageRestore(app.WithActor(context.Background(), app.ActorOperator), name, ""))
	if !p.CanSignIn || strings.Contains(strings.Join(p.Warnings, "\n"), "your account") {
		t.Fatalf("the CLI operator has no account to lose: %+v", p.Warnings)
	}
	res := must(b.svc.ApplyRestoreAsOperator(context.Background(), p.ID))
	if a := b.lastAudit(); a.Action != "backup.restored" || a.ActorName != "Operator (CLI)" || res.Source != name {
		t.Fatalf("audit = %+v", a)
	}
	must0(t, b.svc.NoteLegacyRestore(context.Background(), "cactusinv-20261001-020000.sql.gz", strings.NewReader("-- dump")))
	if a := b.lastAudit(); a.Action != "backup.legacy_restored" || !strings.Contains(a.Summary, "7 bytes") || a.ActorName != "Operator (CLI)" {
		t.Fatalf("legacy note = %+v", a)
	}
	plain := app.New(app.Deps{Tx: b.store, Clock: b.clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}})
	if _, err := plain.ApplyRestoreAsOperator(b.ctx, "x"); !errors.Is(err, domain.ErrNotConfigured) {
		t.Fatalf("not configured: %v", err)
	}
	if err := b.svc.NoteLegacyRestore(b.ctx, "x", errReader{}); err == nil {
		t.Fatal("read error")
	}
}

func TestBackupsInOneSecondTakeTheNextName(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	first := must(b.svc.CreateBackup(b.ctx, domain.BackupManual)).Name
	second := must(b.svc.CreateBackup(b.ctx, domain.BackupManual)).Name
	n1, _ := domain.ParseBackupName(first)
	n2, _ := domain.ParseBackupName(second)
	if first == second || !n2.CreatedAt.Equal(n1.CreatedAt.Add(time.Second)) {
		t.Fatalf("%s / %s", first, second)
	}
}

func TestBackupFailureCategories(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	b.snap.Err = errors.New("Error 2013: Lost connection to server at 'mariadb:3306'")
	b.clock.Set(time.Date(2026, 8, 21, 3, 0, 0, 0, time.UTC))
	if _, err := b.svc.RunDueBackups(context.Background()); err == nil {
		t.Fatal("must fail")
	}
	ov := must(b.svc.ListBackups(b.ctx))
	if ov.LastRun.Error != "database" || strings.Contains(b.lastAudit().Summary, "mariadb:3306") {
		t.Fatalf("category %q; audit %q", ov.LastRun.Error, b.lastAudit().Summary)
	}
}

// TestSafetyBackupBeforeRestore: with the server key a restore first backs up the live
// data (named in the result and in backup.restored), and restoring that undoes it.
func TestSafetyBackupBeforeRestore(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	must(b.svc.CreateUser(b.ctx, "later@example.com", "another long password"))
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	if !p.SafetyBackup || strings.Contains(strings.Join(p.Warnings, "\n"), "no safety backup") {
		t.Fatalf("preview: %+v", p)
	}
	res := must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
	if n, err := domain.ParseBackupName(res.SafetyBackup); err != nil || n.Kind != domain.BackupPreRestore {
		t.Fatalf("safety backup %q: %v", res.SafetyBackup, err)
	}
	if a := b.lastAudit(); a.Action != "backup.restored" || !strings.Contains(a.Summary, res.SafetyBackup) {
		t.Fatalf("audit = %+v", a)
	}
	if must(b.store.Repos().Users.Count(b.ctx)) != 1 {
		t.Fatal("restored")
	}
	// Undo: restore the safety backup.
	p = must(b.svc.StageRestore(b.ctx, res.SafetyBackup, ""))
	must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
	if must(b.store.Repos().Users.Count(b.ctx)) != 2 {
		t.Fatal("the safety backup brings the replaced data back")
	}
}

func TestNoSafetyBackupWithoutKey(t *testing.T) {
	b := newBackupHarness(t, nil, false, nil)
	var buf bytes.Buffer
	must0(t, b.svc.ExportBackup(b.ctx, "a long enough passphrase", func(string) (io.Writer, error) { return &buf, nil }))
	up := must(b.svc.UploadBackup(b.ctx, &buf))
	p := must(b.svc.StageRestore(b.ctx, up.Name, "a long enough passphrase"))
	if p.SafetyBackup || !strings.Contains(strings.Join(p.Warnings, "\n"), "no safety backup of the current data will be taken") {
		t.Fatalf("preview: %+v", p)
	}
	res := must(b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"))
	if res.SafetyBackup != "" || !strings.Contains(b.lastAudit().Summary, "no safety backup") {
		t.Fatalf("result %+v; audit %q", res, b.lastAudit().Summary)
	}
}

func TestRestoreStopsWhenTheSafetyBackupFails(t *testing.T) {
	b := newBackupHarness(t, nil, true, nil)
	name := b.backupNow()
	must(b.svc.CreateUser(b.ctx, "later@example.com", "another long password"))
	p := must(b.svc.StageRestore(b.ctx, name, ""))
	b.files.FailCommit = errors.New("disk full")
	if _, err := b.svc.ApplyRestore(b.ctx, p.ID, adminPassword, "RESTORE"); err == nil {
		t.Fatal("must stop")
	}
	if must(b.store.Repos().Users.Count(b.ctx)) != 2 || must(b.svc.AutomationHeld(b.ctx)) {
		t.Fatal("nothing changed")
	}
	if a := b.lastAudit(); a.Action != "backup.restore_failed" || !strings.Contains(a.Summary, "safety backup") {
		t.Fatalf("audit = %+v", a)
	}
	if _, err := b.svc.ApplyRestore(b.ctx, "nope", adminPassword, "RESTORE"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown id is refused before any backup: %v", err)
	}
}
