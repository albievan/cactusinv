//go:build integration

package mariadb_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// TestHeldAutomationClaimsNothing: while system_state holds automation (after a restore),
// neither the dispatcher nor the schedule runner claims anything; once resumed they do.
func TestHeldAutomationClaimsNothing(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.CreateInvoice(e.ctx, e.reference()))
	must(e.svc.IssueInvoice(e.ctx, inv.ID, domain.Date{}))
	must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{IdempotencyKey: "held"}))
	must(e.svc.CreateSchedule(e.ctx, domain.Schedule{
		CustomerID: e.customer.ID, Name: "monthly",
		Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, StartDate: domain.NewDate(2026, 8, 1)},
		Lines:      []domain.ScheduleLine{{ProductID: e.product.ID, Quantity: 1000}},
	}))
	e.clock.Set(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))

	hold := func(held bool) {
		t.Helper()
		if err := e.store.WithinTx(e.ctx, func(ctx context.Context, r ports.Repos) error {
			s, err := r.System.Lock(ctx)
			if err != nil {
				return err
			}
			if held {
				s.Hold(e.clock.Now(), "albie", "restore of x")
			} else {
				s.Resume()
			}
			return r.System.Save(ctx, s)
		}); err != nil {
			t.Fatal(err)
		}
	}
	hold(true)
	if s := must(e.store.Repos().System.Get(e.ctx)); !s.AutomationHeld || s.HeldBy != "albie" || s.HeldAt.IsZero() {
		t.Fatalf("state = %+v", s)
	}
	if res := must(e.svc.DispatchEmails(e.ctx, 5)); res.Sent != 0 || e.mailer.Count() != 0 {
		t.Fatalf("dispatched while held: %+v", res)
	}
	if res := must(e.svc.RunDueSchedules(e.ctx, 5)); res.Issued != 0 {
		t.Fatalf("ran schedules while held: %+v", res)
	}
	hold(false)
	if res := must(e.svc.DispatchEmails(e.ctx, 5)); res.Sent != 1 {
		t.Fatalf("after resume: %+v", res)
	}
	if res := must(e.svc.RunDueSchedules(e.ctx, 5)); res.Issued == 0 {
		t.Fatalf("schedules after resume: %+v", res)
	}
}

func TestSystemStateAndBackupRuns(t *testing.T) {
	e := setup(t)
	r := e.store.Repos()
	now := time.Date(2026, 10, 15, 2, 0, 0, 123456000, time.UTC)
	h := domain.BackupHeader{
		FormatVersion: 1, SchemaVersion: 13, CreatedAt: now, CreatedBy: "x",
		Tables: []domain.BackupTable{{Name: "currencies", Columns: []domain.BackupColumn{{Name: "code", Kind: domain.ColumnText}}}},
	}
	s := domain.SystemState{
		RestoreID: "11111111-2222-3333-4444-555555555555", RestoreExpiresAt: now.Add(30 * time.Minute),
		RestoreSource: "cactusinv-20261015T020000Z-s13-manual.age", RestoreSHA256: "ab", RestoreStagedBy: "u1", RestoreHeader: h,
	}
	if err := r.System.Save(e.ctx, s); err != nil {
		t.Fatal(err)
	}
	got := must(r.System.Get(e.ctx))
	if got.RestoreID != s.RestoreID || !got.RestoreExpiresAt.Equal(s.RestoreExpiresAt) || got.RestoreHeader.Tables[0].Name != "currencies" || !got.RestoreHeader.CreatedAt.Equal(now) {
		t.Fatalf("round trip: %+v", got)
	}
	got.ClearRestore()
	if err := r.System.Save(e.ctx, got); err != nil {
		t.Fatal(err)
	}
	if got := must(r.System.Get(e.ctx)); got.RestoreID != "" || len(got.RestoreHeader.Tables) != 0 {
		t.Fatalf("cleared: %+v", got)
	}

	if _, err := r.BackupRuns.Latest(e.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no runs yet: %v", err)
	}
	run := domain.BackupRun{Slot: now, Result: domain.BackupRunRunning, StartedAt: now}
	if err := r.BackupRuns.Start(e.ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := r.BackupRuns.Start(e.ctx, run); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a slot runs once: %v", err)
	}
	run.Result, run.Name, run.FinishedAt = domain.BackupRunOK, "cactusinv-20261015T020000Z-s13-scheduled.age", now.Add(time.Second)
	if err := r.BackupRuns.Finish(e.ctx, run); err != nil {
		t.Fatal(err)
	}
	if got := must(r.BackupRuns.Latest(e.ctx)); got.Result != domain.BackupRunOK || got.Name != run.Name || !got.Slot.Equal(now) {
		t.Fatalf("latest: %+v", got)
	}
	if err := r.BackupRuns.Finish(e.ctx, domain.BackupRun{Slot: now.Add(time.Hour)}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("finish unknown slot: %v", err)
	}
}

// TestAppendVerbatimRefusesBrokenLinks: only an entry that chains from the head goes in.
func TestAppendVerbatimRefusesBrokenLinks(t *testing.T) {
	e := setup(t)
	chain := must(e.store.Repos().Audit.Chain(e.ctx, 0, 1000))
	head := chain[len(chain)-1]
	next := domain.AuditEntry{Seq: head.Seq + 1, At: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ActorName: "restored", Action: "invoice.created", EntityType: "invoice", EntityID: "x"}
	next.Seal(head.Hash)
	appendV := func(e2 domain.AuditEntry) error {
		return e.store.WithinTx(e.ctx, func(ctx context.Context, r ports.Repos) error { return r.Audit.AppendVerbatim(ctx, e2) })
	}
	wrongSeq, wrongPrev, altered := next, next, next
	wrongSeq.Seq++
	wrongPrev.Seal("deadbeef")
	altered.Summary = "changed after sealing"
	for name, bad := range map[string]domain.AuditEntry{"seq": wrongSeq, "prev": wrongPrev, "content": altered} {
		if err := appendV(bad); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := appendV(next); err != nil {
		t.Fatal(err)
	}
	// A rolled-back append leaves no gap: seq is assigned explicitly, not by AUTO_INCREMENT.
	_ = e.store.WithinTx(e.ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Audit.Append(ctx, &domain.AuditEntry{At: e.clock.Now(), ActorName: "x", Action: "x.y", EntityType: "x"}); err != nil {
			return err
		}
		return errors.New("roll back")
	})
	must(e.svc.CreateCurrency(e.ctx, "EUR", "Euro", "€", 2, true))
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK {
		t.Fatalf("chain after verbatim append and a rollback: %+v", v)
	}
}

// TestNewRepositoryMethods covers the small methods restores need.
func TestNewRepositoryMethods(t *testing.T) {
	e := setup(t)
	r := e.store.Repos()
	inv := must(e.svc.CreateInvoice(e.ctx, e.reference()))
	must(e.svc.IssueInvoice(e.ctx, inv.ID, domain.Date{}))
	if n := must(r.Invoices.CountCreatedSince(e.ctx, e.clock.Now().Add(-time.Hour))); n != 1 {
		t.Fatalf("created since = %d", n)
	}
	if n := must(r.Invoices.CountCreatedSince(e.ctx, e.clock.Now())); n != 0 {
		t.Fatalf("created since now = %d", n)
	}
	for _, ref := range must(r.Invoices.ListUnexported(e.ctx, 10)) {
		if err := r.Invoices.MarkExported(e.ctx, ref.ID, ref.Version); err != nil {
			t.Fatal(err)
		}
	}
	if refs := must(r.Invoices.ListUnexported(e.ctx, 10)); len(refs) != 0 {
		t.Fatal("exported")
	}
	if err := r.Invoices.ResetExported(e.ctx); err != nil {
		t.Fatal(err)
	}
	if refs := must(r.Invoices.ListUnexported(e.ctx, 10)); len(refs) != 1 {
		t.Fatal("reset re-exports")
	}
	m := must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{IdempotencyKey: "k"}))
	if n := must(r.Emails.CountSending(e.ctx, e.clock.Now())); n != 0 {
		t.Fatal("nothing sending yet")
	}
	claimed := must(r.Emails.ClaimDue(e.ctx, e.clock.Now(), 1))
	if len(claimed) != 1 || claimed[0].ID != m.ID {
		t.Fatal("claim")
	}
	_ = claimed[0].Claim(e.clock.Now())
	if err := r.Emails.Update(e.ctx, claimed[0]); err != nil {
		t.Fatal(err)
	}
	if n := must(r.Emails.CountSending(e.ctx, e.clock.Now())); n != 1 {
		t.Fatalf("sending = %d", n)
	}
	if n := must(r.Emails.CountSending(e.ctx, e.clock.Now().Add(domain.StaleSendingAfter+time.Second))); n != 0 {
		t.Fatal("a stale sending row is not in flight")
	}
	must(e.svc.CreateUser(e.ctx, "a@example.com", "long enough password"))
	if _, _, err := e.svc.Login(e.ctx, "a@example.com", "long enough password"); err != nil {
		t.Fatal(err)
	}
	if err := r.Idempotency.Reserve(e.ctx, ports.IdempotencyRecord{Key: "k1", Fingerprint: "f", CreatedAt: e.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := r.Sessions.DeleteAll(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Idempotency.DeleteAll(e.ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := testDB.QueryRowContext(e.ctx, "SELECT (SELECT COUNT(*) FROM sessions) + (SELECT COUNT(*) FROM idempotency_keys)").Scan(&n); err != nil || n != 0 {
		t.Fatalf("left %d (%v)", n, err)
	}
}
