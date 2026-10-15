//go:build integration

package mariadb_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

const stagingName = "cactusinv_restore"

func newStager(t *testing.T) *mariadb.Stager {
	t.Helper()
	cfg := testCfg
	cfg.Name = stagingName
	staging, err := mariadb.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("staging database (granted by testdata/10-restore-db.sql): %v", err)
	}
	t.Cleanup(func() { _ = staging.Close() })
	s, err := mariadb.NewStager(context.Background(), testDB, staging, testCfg.Name, stagingName)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// stageLive stages a snapshot of the live database (no file, no encryption).
func stageLive(t *testing.T, ctx context.Context, s *mariadb.Stager) ports.StagedRestore {
	t.Helper()
	staged, err := s.Stage(ctx, func(ctx context.Context, sink ports.BackupSink) error {
		return mariadb.NewSnapshotter(testDB).Snapshot(ctx, func(ctx context.Context, src ports.BackupSource) error {
			h := domain.BackupHeader{FormatVersion: 1, SchemaVersion: src.SchemaVersion(), CreatedAt: time.Now().UTC(), CreatedBy: "test", Tables: src.Tables()}
			if err := sink.Header(ctx, h); err != nil {
				return err
			}
			for _, tb := range h.Tables {
				if err := src.ScanTable(ctx, tb.Name, func(r []any) error { return sink.Row(ctx, tb.Name, r) }); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return staged
}

func TestStagerLoadsAndDescribes(t *testing.T) {
	e := setup(t)
	s := newStager(t)
	must(e.svc.CreateUser(e.ctx, "a@example.com", "long enough password"))
	staged := stageLive(t, e.ctx, s)
	if staged.SchemaVersion != headSchema || staged.Counts["customers"] != 1 || staged.Counts["users"] != 1 || len(staged.UserIDs) != 1 {
		t.Fatalf("staged = %+v", staged)
	}
	for name, n := range staged.Counts {
		if staged.LiveCounts[name] != n {
			t.Errorf("%s: staged %d, live %d", name, n, staged.LiveCounts[name])
		}
	}
	if _, ok := staged.Counts["sessions"]; ok {
		t.Fatal("excluded tables are not staged")
	}
	live := must(e.store.Repos().Audit.Chain(e.ctx, 0, 10000))
	if got := must(s.StagedAudit(e.ctx)); !reflect.DeepEqual(got, live) {
		t.Fatalf("staged audit differs: %d vs %d entries", len(got), len(live))
	}
	if err := s.Discard(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Staged(e.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("after discard: %v", err)
	}
	if err := s.Apply(e.ctx, func(context.Context, ports.RestoreTx) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("apply without a stage: %v", err)
	}
	for _, c := range []struct{ name, staging string }{{"cactusinv", "cactusinv"}, {"cactusinv", "scratch"}, {"cactusinv", "_restore"}} {
		if _, err := mariadb.NewStager(e.ctx, testDB, testDB, c.name, c.staging); err == nil {
			t.Fatalf("staging %q accepted", c.staging)
		}
	}
	// A staging pool that is really connected to the live database is refused.
	if _, err := mariadb.NewStager(e.ctx, testDB, testDB, "cactusinv", stagingName); err == nil || !strings.Contains(err.Error(), "connected to") {
		t.Fatalf("pool on the live database: %v", err)
	}
}

// TestApplyFailureLeavesLiveDataUnchanged injects a failure after the tables were replaced
// and an audit entry appended: everything rolls back, byte for byte.
func TestApplyFailureLeavesLiveDataUnchanged(t *testing.T) {
	e := setup(t)
	s := newStager(t)
	stageLive(t, e.ctx, s)
	// Live moves on after the stage.
	must(e.svc.CreateCurrency(e.ctx, "EUR", "Euro", "€", 2, true))
	inv := must(e.svc.CreateInvoice(e.ctx, e.reference()))
	must(e.svc.IssueInvoice(e.ctx, inv.ID, domain.Date{}))
	_, _, before := snapshotRows(t, e.ctx)

	injected := errors.New("injected failure")
	err := s.Apply(e.ctx, func(ctx context.Context, tx ports.RestoreTx) error {
		if err := tx.ReplaceTables(ctx); err != nil {
			return err
		}
		r := tx.Repos()
		if n := must(r.Invoices.CountCreatedSince(ctx, time.Time{})); n != 0 {
			t.Errorf("replaced tables hold the staged invoices: %d", n)
		}
		if err := r.Audit.Append(ctx, &domain.AuditEntry{At: e.clock.Now(), ActorName: "x", Action: "x.y", EntityType: "x"}); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("err = %v", err)
	}
	_, _, after := snapshotRows(t, e.ctx)
	if !reflect.DeepEqual(before, after) {
		for name := range before {
			if !reflect.DeepEqual(before[name], after[name]) {
				t.Errorf("table %s changed", name)
			}
		}
		t.Fatal("live data changed")
	}
	// The pool is healthy: no connection is left holding table locks.
	for range 25 {
		must(e.svc.GetBusiness(e.ctx))
	}
	must(e.svc.CreateCurrency(e.ctx, "GBP", "Pound", "£", 2, true))
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK {
		t.Fatalf("chain after rollback: %+v", v)
	}
}

// TestApplyMakesConcurrentWritersWait: a writer arriving during the swap waits for it and
// then succeeds against the restored data; the audit trail still verifies, and the
// append-only triggers still hold.
func TestApplyMakesConcurrentWritersWait(t *testing.T) {
	e := setup(t)
	s := newStager(t)
	stageLive(t, e.ctx, s)
	must(e.svc.CreateCurrency(e.ctx, "EUR", "Euro", "€", 2, true)) // not in the stage

	locked := make(chan struct{})
	var applyDone, writerDone time.Time
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-locked
		if _, err := e.svc.CreateCurrency(e.ctx, "CHF", "Swiss Franc", "Fr", 2, true); err != nil {
			t.Error(err)
		}
		writerDone = time.Now()
	}()
	err := s.Apply(e.ctx, func(ctx context.Context, tx ports.RestoreTx) error {
		if err := tx.ReplaceTables(ctx); err != nil {
			return err
		}
		close(locked)
		time.Sleep(1500 * time.Millisecond)
		applyDone = time.Now()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if !writerDone.After(applyDone) {
		t.Fatal("the writer did not wait for the swap")
	}
	codes := map[domain.CurrencyCode]bool{}
	for _, c := range must(e.svc.ListCurrencies(e.ctx)) {
		codes[c.Code] = true
	}
	if !codes["ZAR"] || !codes["CHF"] || codes["EUR"] {
		t.Fatalf("currencies after the swap and the late writer: %v", codes)
	}
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK {
		t.Fatalf("chain: %+v", v)
	}
	if _, err := testDB.ExecContext(e.ctx, "UPDATE audit_log SET summary = 'x'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update after a restore: %v", err)
	}
	if _, err := testDB.ExecContext(e.ctx, "DELETE FROM audit_log"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete after a restore: %v", err)
	}
}

// TestApplyPanicLeavesNothingLocked: a panic inside the swap rolls it back and unlocks;
// the connection never returns to the pool holding locks or a half-applied swap.
func TestApplyPanicLeavesNothingLocked(t *testing.T) {
	e := setup(t)
	s := newStager(t)
	stageLive(t, e.ctx, s)
	must(e.svc.CreateCurrency(e.ctx, "EUR", "Euro", "€", 2, true))
	_, _, before := snapshotRows(t, e.ctx)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panic must propagate")
			}
		}()
		_ = s.Apply(e.ctx, func(ctx context.Context, tx ports.RestoreTx) error {
			if err := tx.ReplaceTables(ctx); err != nil {
				return err
			}
			panic("bug in a restore step")
		})
	}()
	ctx, cancel := context.WithTimeout(e.ctx, 20*time.Second)
	defer cancel()
	// Use every pooled connection: none may hold table locks or an open swap.
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.CreateProduct(ctx, domain.Product{SKU: fmt.Sprintf("P%02d", i), Name: "p", Prices: map[domain.CurrencyCode]int64{"ZAR": 1}, Active: true}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		t.Fatal("writers hung: a connection kept the table locks")
	}
	_, _, after := snapshotRows(t, e.ctx)
	for _, table := range []string{"currencies", "customers", "invoices", "users", "bank_accounts"} {
		if !reflect.DeepEqual(before[table], after[table]) {
			t.Errorf("table %s changed", table)
		}
	}
	if v := must(e.svc.VerifyAudit(e.ctx)); !v.OK {
		t.Fatalf("chain = %+v", v)
	}
}

// TestBackupLockAcrossConnections: the GET_LOCK is exclusive across sessions (processes).
func TestBackupLockAcrossConnections(t *testing.T) {
	e := setup(t)
	a, b := newStager(t), newStager(t)
	unlock, err := a.Lock(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Lock(e.ctx); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("second holder: %v", err)
	}
	unlock()
	unlock2, err := b.Lock(e.ctx)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	unlock2()
}

// TestClaimConcurrentWithApply: a claim transaction holding the system state in share mode
// and a swap that needs it exclusively both finish — one waits for the other, nothing
// hangs, and a claim after a held restore claims nothing.
func TestClaimConcurrentWithApply(t *testing.T) {
	e := setup(t)
	s := newStager(t)
	inv := must(e.svc.CreateInvoice(e.ctx, e.reference()))
	must(e.svc.IssueInvoice(e.ctx, inv.ID, domain.Date{}))
	must(e.svc.SendInvoice(e.ctx, inv.ID, app.SendInput{IdempotencyKey: "c1"}))
	stageLive(t, e.ctx, s)
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()

	// 1. The claim holds its locks while the swap starts.
	claimed := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := e.store.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
			due, err := r.Emails.ClaimDue(ctx, e.clock.Now(), 1)
			if err != nil {
				return err
			}
			select {
			case claimed <- struct{}{}:
			default:
			}
			time.Sleep(time.Second)
			if len(due) > 0 {
				_ = due[0].Claim(e.clock.Now())
				return r.Emails.Update(ctx, due[0])
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	}()
	<-claimed
	err := s.Apply(ctx, func(ctx context.Context, tx ports.RestoreTx) error {
		if err := tx.ReplaceTables(ctx); err != nil {
			return err
		}
		st, err := tx.Repos().System.Lock(ctx)
		if err != nil {
			return err
		}
		st.Hold(e.clock.Now(), "test", "restore of x")
		return tx.Repos().System.Save(ctx, st)
	})
	wg.Wait()
	if err != nil {
		t.Fatalf("apply after a concurrent claim: %v", err)
	}

	// 2. A dispatcher arriving during the next swap waits, then claims nothing (held).
	stageLive(t, e.ctx, s)
	inSwap := make(chan struct{})
	done := make(chan app.DispatchResult, 1)
	go func() {
		<-inSwap
		res, derr := e.svc.DispatchEmails(ctx, 5)
		if derr != nil {
			t.Error(derr)
		}
		done <- res
	}()
	if err := s.Apply(ctx, func(ctx context.Context, tx ports.RestoreTx) error {
		close(inSwap)
		time.Sleep(500 * time.Millisecond)
		return tx.ReplaceTables(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-done:
		if res.Sent != 0 {
			t.Fatalf("sent while held: %+v", res)
		}
	case <-ctx.Done():
		t.Fatal("dispatcher hung")
	}
}

// TestSwapConnectionHoldsTheBackupLock: during Apply the backup lock belongs to the swap's
// own session, so another service cannot stage; killing that connection aborts the swap
// and frees the lock and the tables together.
func TestSwapConnectionHoldsTheBackupLock(t *testing.T) {
	e := setup(t)
	s, other := newStager(t), newStager(t)
	stageLive(t, e.ctx, s)
	must(e.svc.CreateCurrency(e.ctx, "EUR", "Euro", "€", 2, true))
	_, _, before := snapshotRows(t, e.ctx)
	ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
	defer cancel()

	err := s.Apply(ctx, func(ctx context.Context, tx ports.RestoreTx) error {
		if err := tx.ReplaceTables(ctx); err != nil {
			return err
		}
		if _, err := other.Lock(ctx); !errors.Is(err, domain.ErrBusy) {
			t.Errorf("another service took the lock during a swap: %v", err)
		}
		var holder sql.NullInt64
		if err := testDB.QueryRowContext(ctx, "SELECT IS_USED_LOCK('cactusinv_backup:cactusinv')").Scan(&holder); err != nil {
			return fmt.Errorf("lock holder: %w", err)
		}
		if !holder.Valid {
			return errors.New("the swap connection does not hold the backup lock")
		}
		if _, err := testDB.ExecContext(ctx, fmt.Sprintf("KILL %d", holder.Int64)); err != nil {
			return err
		}
		// The swap's session is gone: its next statement fails.
		_, err := tx.Repos().Sequences.Peek(ctx)
		return err
	})
	if err == nil {
		t.Fatal("the swap survived its connection")
	}
	unlock, err := other.Lock(ctx)
	if err != nil {
		t.Fatalf("lock not freed with the swap connection: %v", err)
	}
	unlock()
	must(e.svc.CreateCurrency(ctx, "GBP", "Pound", "£", 2, true)) // tables are unlocked
	_, _, after := snapshotRows(t, e.ctx)
	for _, table := range []string{"customers", "invoices", "users", "bank_accounts"} {
		if !reflect.DeepEqual(before[table], after[table]) {
			t.Errorf("table %s changed", table)
		}
	}
}
