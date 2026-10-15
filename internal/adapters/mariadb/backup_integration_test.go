//go:build integration

package mariadb_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/aesgcm"
	"github.com/albievan/cactusinv/internal/adapters/agecrypt"
	"github.com/albievan/cactusinv/internal/adapters/backupdir"
	"github.com/albievan/cactusinv/internal/adapters/backupformat"
	"github.com/albievan/cactusinv/internal/adapters/idgen"
	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

const operatorPassword = "correct horse battery"

// backupEnv is setup() with the real backup adapters: snapshotter, format, age, the
// backups directory and the staging database.
type backupEnv struct {
	*env
	sealer *agecrypt.Sealer
	files  *backupdir.Dir
	op     domain.User
	opCtx  context.Context
}

func setupBackups(t *testing.T) *backupEnv {
	t.Helper()
	return withBackups(t, setup(t))
}

func withBackups(t *testing.T, e *env) *backupEnv {
	t.Helper()
	key, err := agecrypt.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := agecrypt.New(key, agecrypt.WithWorkFactor(10))
	if err != nil {
		t.Fatal(err)
	}
	files, err := backupdir.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := aesgcm.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	b := &backupEnv{env: e, sealer: sealer, files: files}
	b.svc = app.New(app.Deps{
		Tx: e.store, Clock: e.clock, IDs: idgen.UUIDv7{}, Mailer: e.mailer, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Cipher: cipher,
		Backups: app.BackupDeps{
			Snapshotter: mariadb.NewSnapshotter(testDB), Codec: backupformat.New(backupformat.Limits{}), Sealer: sealer,
			Files: files, Stager: newStager(t), AppVersion: "test", Build: "1",
		},
	})
	return b
}

// operator creates the signed-in administrator who takes and restores backups.
func (b *backupEnv) operator(t *testing.T) {
	t.Helper()
	b.op = must(b.svc.CreateUser(b.ctx, "albie@example.com", operatorPassword))
	b.opCtx = app.WithActor(b.ctx, app.Actor{ID: b.op.ID, Name: b.op.Email})
}

// reinsertOperator puts the same account back into a wiped database without writing to
// the audit trail: it stands in for the operator of a fresh installation whose trail is
// still empty (the CLI restore of 14.17 needs no live account at all).
func (b *backupEnv) reinsertOperator(t *testing.T) {
	t.Helper()
	if err := b.store.Repos().Users.Create(b.ctx, b.op); err != nil {
		t.Fatal(err)
	}
}

// seedBusiness adds an issued and e-mailed invoice, one e-mail due later, a schedule and an
// SMTP password: every table has rows.
func (b *backupEnv) seedBusiness(t *testing.T) {
	t.Helper()
	ctx := b.opCtx
	inv := must(b.svc.CreateInvoice(ctx, b.reference()))
	must(b.svc.IssueInvoice(ctx, inv.ID, domain.Date{}))
	must(b.svc.SendInvoice(ctx, inv.ID, app.SendInput{IdempotencyKey: "now"}))
	if res := must(b.svc.DispatchEmails(ctx, 5)); res.Sent != 1 {
		t.Fatalf("dispatch: %+v", res)
	}
	must(b.svc.SendInvoice(ctx, inv.ID, app.SendInput{IdempotencyKey: "later", SendAt: b.clock.Now().Add(48 * time.Hour)}))
	must(b.svc.CreateSchedule(ctx, domain.Schedule{
		CustomerID: b.customer.ID, Name: "monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 9, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: b.product.ID, Quantity: 1000}},
	}))
	st := must(b.svc.GetSMTPSettings(ctx)).Settings
	st.Enabled, st.Username = true, "me@gmail.com"
	pw := "abcd efgh ijkl mnop"
	must(b.svc.UpdateSMTPSettings(ctx, st, &pw))
}

func (b *backupEnv) backup(t *testing.T) string {
	t.Helper()
	b.clock.Advance(time.Second) // names have one-second resolution
	return must(b.svc.CreateBackup(b.opCtx, domain.BackupManual)).Name
}

func (b *backupEnv) restore(t *testing.T, name string) (app.RestorePreview, app.RestoreResult) {
	t.Helper()
	p := must(b.svc.StageRestore(b.opCtx, name, ""))
	return p, must(b.svc.ApplyRestore(b.opCtx, p.ID, operatorPassword, domain.RestoreConfirmationWord))
}

// wipe empties the live database the hard way: every migration down and up again.
func (b *backupEnv) wipe(t *testing.T) {
	t.Helper()
	b.store = reset(t)
}

func withoutAudit(rows map[string][][]any) map[string][][]any {
	out := map[string][][]any{}
	for k, v := range rows {
		if k != domain.AuditTableName {
			out[k] = v
		}
	}
	return out
}

func auditActions(t *testing.T, ctx context.Context, store *mariadb.Store) []string {
	t.Helper()
	var out []string
	for _, e := range must(store.Repos().Audit.Chain(ctx, 0, 100000)) {
		out = append(out, e.Action)
	}
	return out
}

// TestBackupWipeRestore: seed → back up → wipe → restore → every table as it was, the
// audit trail brought back verbatim and verifying, automation held until resumed.
func TestBackupWipeRestore(t *testing.T) {
	b := setupBackups(t)
	b.operator(t)
	b.seedBusiness(t)
	_, _, before := snapshotRows(t, b.ctx)
	name := b.backup(t)

	b.wipe(t)
	b.reinsertOperator(t)
	p, res := b.restore(t, name)
	if p.SchemaFrom != headSchema || p.SchemaTo != headSchema || p.Audit.Mode != domain.AuditAppendVerbatim || p.InvoicesSince != 0 || p.SMTPPasswordCleared || !p.CanSignIn {
		t.Fatalf("preview = %+v", p)
	}
	if res.Audit.Mode != domain.AuditAppendVerbatim || res.SequenceKept || res.SMTPPasswordCleared {
		t.Fatalf("result = %+v", res)
	}
	_, _, after := snapshotRows(t, b.ctx)
	for table, rows := range withoutAudit(before) {
		if !reflect.DeepEqual(after[table], rows) {
			t.Errorf("table %s differs after the restore", table)
		}
	}
	auditBefore, auditAfter := before[domain.AuditTableName], after[domain.AuditTableName]
	if len(auditAfter) != len(auditBefore)+1 || !reflect.DeepEqual(auditAfter[:len(auditBefore)], auditBefore) {
		t.Fatalf("audit: %d entries before, %d after", len(auditBefore), len(auditAfter))
	}
	if acts := auditActions(t, b.ctx, b.store); acts[len(acts)-1] != "backup.restored" {
		t.Fatalf("last action %s", acts[len(acts)-1])
	}
	if v := must(b.svc.VerifyAudit(b.ctx)); !v.OK {
		t.Fatalf("chain = %+v", v)
	}
	if _, _, ok, _ := b.svc.SMTPServer(b.ctx); !ok {
		t.Fatal("the SMTP password decrypts on the same installation")
	}

	// Held: the e-mail due later is not sent, the schedule does not run.
	b.clock.Advance(30 * 24 * time.Hour)
	if res := must(b.svc.DispatchEmails(b.ctx, 5)); res.Sent != 0 {
		t.Fatalf("sent while held: %+v", res)
	}
	if res := must(b.svc.RunDueSchedules(b.ctx, 5)); res.Issued != 0 {
		t.Fatalf("ran while held: %+v", res)
	}
	if err := b.svc.ResumeAutomation(b.opCtx); err != nil {
		t.Fatal(err)
	}
	if res := must(b.svc.DispatchEmails(b.ctx, 5)); res.Sent != 1 {
		t.Fatalf("after resume: %+v", res)
	}
}

// rewrite decrypts a stored backup, lets edit change its tar entries, and stores the
// result validly encrypted under a new name: what someone holding the key could do.
func (b *backupEnv) rewrite(t *testing.T, name string, edit func(name string, data []byte) []byte) string {
	t.Helper()
	f := must(b.files.Open(b.ctx, name))
	defer f.Close()
	plain := must(b.sealer.Open(f, ""))
	gz := must(gzip.NewReader(plain))
	tr := tar.NewReader(gz)
	var out bytes.Buffer
	ngz := gzip.NewWriter(&out)
	tw := tar.NewWriter(ngz)
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data := edit(hd.Name, must(io.ReadAll(tr)))
		hd.Size = int64(len(data))
		if err := tw.WriteHeader(hd); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	_ = ngz.Close()
	b.clock.Advance(time.Second)
	newName := domain.BackupName{CreatedAt: b.clock.Now(), Kind: domain.BackupUpload}.String()
	w := must(b.files.Create(b.ctx, newName))
	sealed := must(b.sealer.SealWithKey(w))
	_, _ = sealed.Write(out.Bytes())
	if err := sealed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	return newName
}

// TestRestoreRefusesBadBackups: a tampered file, a validly re-encrypted archive with one
// changed row, and a backup from a newer schema are refused, audited, and change nothing.
func TestRestoreRefusesBadBackups(t *testing.T) {
	b := setupBackups(t)
	b.operator(t)
	b.seedBusiness(t)
	name := b.backup(t)

	f := must(b.files.Open(b.ctx, name))
	raw := must(io.ReadAll(f))
	_ = f.Close()
	raw[len(raw)/2] ^= 0x01
	b.clock.Advance(time.Second)
	tampered := must(b.svc.UploadBackup(b.opCtx, bytes.NewReader(raw))).Name

	changedRow := b.rewrite(t, name, func(n string, d []byte) []byte {
		if strings.HasPrefix(n, "tables/currencies/") {
			return bytes.Replace(d, []byte("South African Rand"), []byte("South African Rant"), 1)
		}
		return d
	})
	newer := b.rewrite(t, name, func(n string, d []byte) []byte {
		if n == "header.json" {
			return bytes.Replace(d, fmt.Appendf(nil, `"schemaVersion":%d`, headSchema), fmt.Appendf(nil, `"schemaVersion":%d`, headSchema+1), 1)
		}
		return d
	})
	_, _, before := snapshotRows(t, b.ctx)
	for _, c := range []struct {
		file string
		want error
	}{
		{tampered, domain.ErrBackupInvalid},
		{changedRow, domain.ErrBackupInvalid},
		{newer, domain.ErrBackupTooNew},
	} {
		if _, err := b.svc.StageRestore(b.opCtx, c.file, ""); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.file, err, c.want)
		}
		if acts := auditActions(t, b.ctx, b.store); acts[len(acts)-1] != "backup.restore_refused" {
			t.Fatalf("%s: not audited (%s)", c.file, acts[len(acts)-1])
		}
	}
	_, _, after := snapshotRows(t, b.ctx)
	if !reflect.DeepEqual(withoutAudit(before), withoutAudit(after)) {
		t.Fatal("a refused restore changed live data")
	}
	if _, err := b.svc.CurrentRestore(b.opCtx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("nothing staged")
	}
	// The untouched backup still restores (the trail grew since: keep_current).
	if _, res := b.restore(t, name); res.Audit.Mode != domain.AuditKeepCurrent {
		t.Fatalf("mode %s", res.Audit.Mode)
	}
}

// TestOlderSchemaBackupRestoresIntoHead: a backup taken at schema 12 (v1.4.0) restores into
// the head schema through the real migrations.
func TestOlderSchemaBackupRestoresIntoHead(t *testing.T) {
	ctx := context.Background()
	if err := mariadb.Migrate(ctx, testDB, true); err != nil {
		t.Fatal(err)
	}
	if err := mariadb.MigrateTo(ctx, testDB, 12); err != nil {
		t.Fatal(err)
	}
	e := &env{ctx: ctx, store: mariadb.NewStore(testDB), clock: &fakes.Clock{T: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}, mailer: &fakes.Mailer{}}
	b := withBackups(t, e)
	must(b.svc.UpdateBusiness(ctx, domain.BusinessProfile{Name: "Old Co", TimeZone: "Africa/Johannesburg"}, 500))
	must(b.svc.CreateCurrency(ctx, "ZAR", "South African Rand", "R", 2, true))
	b.operator(t)
	// At schema 12 the snapshot has no system_state or backup_runs (excluded anyway).
	name := b.backup(t)
	if n, _ := domain.ParseBackupName(name); n.Schema != 12 {
		t.Fatalf("backup schema %d", n.Schema)
	}
	_, _, before := snapshotRows(t, ctx)
	if err := mariadb.Migrate(ctx, testDB, false); err != nil { // upgrade to head
		t.Fatal(err)
	}
	b.clock.Advance(time.Minute)
	must(b.svc.CreateCurrency(b.opCtx, "USD", "US Dollar", "$", 2, true)) // after the backup
	p, res := b.restore(t, name)
	if p.SchemaFrom != 12 || p.SchemaTo != headSchema || !strings.Contains(strings.Join(p.Warnings, "\n"), "older version") || res.Audit.Mode != domain.AuditKeepCurrent {
		t.Fatalf("preview %+v / result %+v", p, res)
	}
	_, _, after := snapshotRows(t, ctx)
	for table, rows := range withoutAudit(before) {
		if !reflect.DeepEqual(after[table], rows) {
			t.Errorf("table %s differs", table)
		}
	}
	if v := must(b.svc.VerifyAudit(ctx)); !v.OK {
		t.Fatalf("chain = %+v", v)
	}
}

// TestRestoreBusyWhileAnEmailIsSending: a message in flight blocks the swap.
func TestRestoreBusyWhileAnEmailIsSending(t *testing.T) {
	b := setupBackups(t)
	b.operator(t)
	inv := must(b.svc.CreateInvoice(b.opCtx, b.reference()))
	must(b.svc.IssueInvoice(b.opCtx, inv.ID, domain.Date{}))
	name := b.backup(t)
	must(b.svc.SendInvoice(b.opCtx, inv.ID, app.SendInput{IdempotencyKey: "k"}))
	if err := b.store.WithinTx(b.ctx, func(ctx context.Context, r ports.Repos) error {
		due := must(r.Emails.ClaimDue(ctx, b.clock.Now(), 1))
		if err := due[0].Claim(b.clock.Now()); err != nil {
			return err
		}
		return r.Emails.Update(ctx, due[0])
	}); err != nil {
		t.Fatal(err)
	}
	p := must(b.svc.StageRestore(b.opCtx, name, ""))
	_, _, before := snapshotRows(t, b.ctx)
	if _, err := b.svc.ApplyRestore(b.opCtx, p.ID, operatorPassword, "RESTORE"); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	_, _, after := snapshotRows(t, b.ctx)
	if !reflect.DeepEqual(withoutAudit(before), withoutAudit(after)) {
		t.Fatal("live data changed")
	}
	if acts := auditActions(t, b.ctx, b.store); acts[len(acts)-1] != "backup.restore_refused" {
		t.Fatal("busy refusal audited")
	}
	if held := must(b.svc.AutomationHeld(b.ctx)); held {
		t.Fatal("not held")
	}
}

// TestRestoreAuditMergeModes: keep_current (restoring into the installation that took the
// backup) and append_resealed (into a trail that diverged); append_verbatim is
// TestBackupWipeRestore.
func TestRestoreAuditMergeModes(t *testing.T) {
	b := setupBackups(t)
	b.operator(t)
	b.seedBusiness(t)
	name := b.backup(t)
	backupTrail := must(b.store.Repos().Audit.Chain(b.ctx, 0, 100000))
	backupTrail = backupTrail[:len(backupTrail)-1] // the backup.created entry is not in it

	if _, res := b.restore(t, name); res.Audit.Mode != domain.AuditKeepCurrent {
		t.Fatalf("keep: %s", res.Audit.Mode)
	}
	if v := must(b.svc.VerifyAudit(b.ctx)); !v.OK {
		t.Fatalf("keep chain = %+v", v)
	}

	b.wipe(t)
	b.operator(t) // the fresh installation's own history diverges from the backup's
	must(b.svc.CreateCurrency(b.opCtx, "EUR", "Euro", "€", 2, true))
	liveBefore := must(b.store.Repos().Audit.Chain(b.ctx, 0, 100000))
	p, res := b.restore(t, name)
	if p.Audit.Mode != domain.AuditAppendResealed || res.Audit.Mode != domain.AuditAppendResealed || res.Audit.Count != len(backupTrail) {
		t.Fatalf("resealed: %+v", res.Audit)
	}
	trail := must(b.store.Repos().Audit.Chain(b.ctx, 0, 100000))
	if !reflect.DeepEqual(trail[:len(liveBefore)], liveBefore) {
		t.Fatal("live entries changed")
	}
	tail := trail[len(liveBefore):]
	if tail[0].Action != domain.AuditResealBegin || tail[len(backupTrail)+1].Action != domain.AuditResealEnd || tail[len(tail)-1].Action != "backup.restored" {
		t.Fatalf("markers: %v … %v", tail[0].Action, tail[len(tail)-1].Action)
	}
	// The sequence numbers the markers and backup.restored announce are the real ones.
	begin, first, last, end := res.Audit.ResealSeqs()
	endMarker := tail[len(backupTrail)+1]
	if tail[0].Seq != begin || tail[1].Seq != first || tail[len(backupTrail)].Seq != last || endMarker.Seq != end ||
		!strings.Contains(tail[0].Summary, fmt.Sprintf("(#%d–#%d)", first, last)) || !strings.Contains(tail[0].Summary, fmt.Sprintf("ends at marker #%d", end)) ||
		!strings.Contains(endMarker.Summary, fmt.Sprintf("begun at marker #%d", begin)) ||
		!strings.Contains(tail[len(tail)-1].Summary, fmt.Sprintf("between markers #%d and #%d", begin, end)) {
		t.Fatalf("announced #%d/#%d–#%d/#%d; got begin #%d %q, end #%d %q, restored %q",
			begin, first, last, end, tail[0].Seq, tail[0].Summary, endMarker.Seq, endMarker.Summary, tail[len(tail)-1].Summary)
	}
	for i, e := range backupTrail {
		got := tail[i+1]
		if got.Summary != e.Summary || got.Action != e.Action || got.ActorName != e.ActorName || !got.At.Equal(e.At) {
			t.Fatalf("re-sealed entry %d differs", i)
		}
	}
	if v := must(b.svc.VerifyAudit(b.ctx)); !v.OK {
		t.Fatalf("resealed chain = %+v", v)
	}
	if !slices.ContainsFunc(must(b.svc.ListCurrencies(b.ctx)), func(c domain.Currency) bool { return c.Code == "USD" }) {
		t.Fatal("restored currencies")
	}
}

// TestApplyRacingAStage: two processes (separate services, stagers and in-process locks)
// apply one staged backup while staging another. The live data always ends as one whole
// state — unchanged or the applied backup — never a copy of a half-loaded staging database.
func TestApplyRacingAStage(t *testing.T) {
	b := setupBackups(t)
	b.operator(t)
	b.seedBusiness(t)
	_, _, state1 := snapshotRows(t, b.ctx)
	name1 := b.backup(t)
	must(b.svc.CreateCustomer(b.opCtx, domain.Customer{Code: "C2", Name: "Second", Email: "s@x.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))
	name2 := b.backup(t)
	must(b.svc.CreateCustomer(b.opCtx, domain.Customer{Code: "C3", Name: "Third", Email: "t@x.example", Currency: "ZAR", Terms: domain.PaymentTerms{Kind: domain.TermsOnReceipt}, Active: true}))

	other := app.New(app.Deps{
		Tx: b.store, Clock: b.clock, IDs: idgen.UUIDv7{}, Hasher: fakes.Hasher{},
		Backups: app.BackupDeps{
			Snapshotter: mariadb.NewSnapshotter(testDB), Codec: backupformat.New(backupformat.Limits{}), Sealer: b.sealer,
			Files: b.files, Stager: newStager(t),
		},
	})
	for round := range 3 {
		_, _, before := snapshotRows(t, b.ctx)
		p := must(b.svc.StageRestore(b.opCtx, name1, ""))
		stageErr := make(chan error, 1)
		go func() {
			_, err := other.StageRestore(b.opCtx, name2, "")
			stageErr <- err
		}()
		_, applyErr := b.svc.ApplyRestore(b.opCtx, p.ID, operatorPassword, "RESTORE")
		serr := <-stageErr
		if applyErr != nil && !errors.Is(applyErr, domain.ErrBusy) && !errors.Is(applyErr, domain.ErrNotFound) {
			t.Fatalf("round %d: apply: %v", round, applyErr)
		}
		if serr != nil && !errors.Is(serr, domain.ErrBusy) {
			t.Fatalf("round %d: stage: %v", round, serr)
		}
		_, _, after := snapshotRows(t, b.ctx)
		restored := reflect.DeepEqual(after["customers"], state1["customers"]) &&
			reflect.DeepEqual(after["invoices"], state1["invoices"]) && reflect.DeepEqual(after["users"], state1["users"])
		unchanged := reflect.DeepEqual(withoutAudit(after), withoutAudit(before))
		if (applyErr == nil && !restored) || (applyErr != nil && !unchanged) {
			t.Fatalf("round %d (apply err %v): live data is neither the old state nor the backup (%d customers)", round, applyErr, len(after["customers"]))
		}
		if v := must(b.svc.VerifyAudit(b.ctx)); !v.OK {
			t.Fatalf("round %d: chain = %+v", round, v)
		}
	}
}
