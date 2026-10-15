package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func validHeader() BackupHeader {
	return BackupHeader{
		FormatVersion: BackupFormatVersion, SchemaVersion: 13, AppVersion: "1.5.0", Build: "120",
		CreatedAt: time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC), CreatedBy: "Backup scheduler",
		Tables: []BackupTable{
			{Name: "currencies", Columns: []BackupColumn{{"code", ColumnText}, {"minor_units", ColumnInt}}},
			{Name: "audit_log", Columns: []BackupColumn{{"seq", ColumnInt}, {"at", ColumnDateTime}, {"hash", ColumnText}}},
		},
	}
}

func TestBackupHeaderValidate(t *testing.T) {
	cases := []struct {
		name string
		edit func(*BackupHeader)
		want error
	}{
		{"valid", func(*BackupHeader) {}, nil},
		{"format 2", func(h *BackupHeader) { h.FormatVersion = 2 }, ErrBackupUnsupported},
		{"no schema", func(h *BackupHeader) { h.SchemaVersion = 0 }, ErrBackupInvalid},
		{"no time", func(h *BackupHeader) { h.CreatedAt = time.Time{} }, ErrBackupInvalid},
		{"no tables", func(h *BackupHeader) { h.Tables = nil }, ErrBackupInvalid},
		{"long text", func(h *BackupHeader) { h.CreatedBy = strings.Repeat("x", 255) }, ErrBackupInvalid},
		{"bad utf8", func(h *BackupHeader) { h.Build = "\xff" }, ErrBackupInvalid},
		{"injected name", func(h *BackupHeader) { h.Tables[0].Name = "x`; DROP TABLE users; --" }, ErrBackupInvalid},
		{"upper case", func(h *BackupHeader) { h.Tables[0].Name = "Users" }, ErrBackupInvalid},
		{"duplicate table", func(h *BackupHeader) { h.Tables[1].Name = "currencies" }, ErrBackupInvalid},
		{"excluded table", func(h *BackupHeader) { h.Tables[0].Name = "sessions" }, ErrBackupInvalid},
		{"no columns", func(h *BackupHeader) { h.Tables[0].Columns = nil }, ErrBackupInvalid},
		{"duplicate column", func(h *BackupHeader) { h.Tables[0].Columns[1].Name = "code" }, ErrBackupInvalid},
		{"bad column", func(h *BackupHeader) { h.Tables[0].Columns[0].Name = "a.b" }, ErrBackupInvalid},
		{"bad kind", func(h *BackupHeader) { h.Tables[0].Columns[0].Kind = "float" }, ErrBackupInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := validHeader()
			c.edit(&h)
			if err := h.Validate(); !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
				t.Fatalf("Validate() = %v, want %v", err, c.want)
			}
		})
	}
}

func TestParseBackupHeader(t *testing.T) {
	data, err := json.Marshal(validHeader())
	if err != nil {
		t.Fatal(err)
	}
	h, err := ParseBackupHeader(data)
	if err != nil || h.SchemaVersion != 13 || len(h.Tables) != 2 || h.Tables[1].Columns[1].Kind != ColumnDateTime {
		t.Fatalf("round trip: %+v %v", h, err)
	}
	if tb, ok := h.Table("audit_log"); !ok || tb.ColumnIndex("hash") != 2 || tb.ColumnIndex("nope") != -1 {
		t.Fatal("Table / ColumnIndex")
	}
	if _, ok := h.Table("nope"); ok {
		t.Fatal("missing table")
	}
	for name, in := range map[string]string{
		"not json":      `{`,
		"unknown field": strings.Replace(string(data), `"build"`, `"evil":1,"build"`, 1),
		"trailing":      string(data) + `{}`,
	} {
		if _, err := ParseBackupHeader([]byte(in)); !errors.Is(err, ErrBackupInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseBackupHeader([]byte(`{"formatVersion":2,"whatever":true}`)); !errors.Is(err, ErrBackupUnsupported) {
		t.Fatalf("future format: %v", err)
	}
}

func TestCheckRestorable(t *testing.T) {
	cases := []struct {
		schema, current int64
		want            error
	}{
		{13, 13, nil},
		{12, 13, nil},
		{14, 13, ErrBackupTooNew},
		{11, 13, ErrBackupUnsupported},
	}
	for _, c := range cases {
		h := validHeader()
		h.SchemaVersion = c.schema
		if err := CheckRestorable(h, c.current); !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
			t.Errorf("schema %d into %d: %v, want %v", c.schema, c.current, err, c.want)
		}
	}
	h := validHeader()
	h.FormatVersion = 9
	if err := CheckRestorable(h, 13); !errors.Is(err, ErrBackupUnsupported) {
		t.Fatalf("format: %v", err)
	}
}

func TestColumnKindCheckValue(t *testing.T) {
	at := time.Date(2026, 10, 15, 2, 0, 0, 123456000, time.UTC)
	ok := []struct {
		k ColumnKind
		v any
	}{
		{ColumnInt, int64(-5)},
		{ColumnInt, nil},
		{ColumnDecimal, "-12.50"},
		{ColumnText, "Grüße 🌵"},
		{ColumnBytes, []byte{0, 1}},
		{ColumnDateTime, at},
		{ColumnDate, "2026-02-28"},
	}
	for _, c := range ok {
		if err := c.k.CheckValue(c.v); err != nil {
			t.Errorf("%s %v: %v", c.k, c.v, err)
		}
	}
	bad := []struct {
		k ColumnKind
		v any
	}{
		{ColumnInt, 5},
		{ColumnInt, "5"},
		{ColumnDecimal, "1e5"},
		{ColumnText, "\xff"},
		{ColumnText, 1},
		{ColumnBytes, "x"},
		{ColumnDateTime, at.In(time.FixedZone("x", 3600))},
		{ColumnDate, "2026-02-30"},
		{ColumnDate, "2026-2-3"},
		{ColumnDate, 3},
		{"float", 1.5},
	}
	for _, c := range bad {
		if err := c.k.CheckValue(c.v); !errors.Is(err, ErrBackupInvalid) {
			t.Errorf("%s %#v accepted", c.k, c.v)
		}
	}
	tb := validHeader().Tables[0]
	if err := tb.CheckRow([]any{"ZAR", int64(2)}); err != nil {
		t.Fatal(err)
	}
	if err := tb.CheckRow([]any{"ZAR"}); !errors.Is(err, ErrBackupInvalid) {
		t.Fatal("short row")
	}
	if err := tb.CheckRow([]any{"ZAR", "2"}); !errors.Is(err, ErrBackupInvalid) {
		t.Fatal("wrong type")
	}
}

func TestBackupDigest(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	d := BackupDigest{Tables: []TableDigest{{Name: "currencies", Rows: 2, SHA256: sum}}, AuditHead: AuditRef{Seq: 7, Hash: sum}}
	data, _ := json.Marshal(d)
	got, err := ParseBackupDigest(data)
	if err != nil || VerifyBackupDigest(d, got) != nil {
		t.Fatalf("round trip %v", err)
	}
	other := got
	other.Tables = []TableDigest{{Name: "currencies", Rows: 3, SHA256: sum}}
	if err := VerifyBackupDigest(d, other); !errors.Is(err, ErrBackupInvalid) {
		t.Fatal("row count mismatch")
	}
	other = got
	other.AuditHead.Seq = 8
	if err := VerifyBackupDigest(d, other); !errors.Is(err, ErrBackupInvalid) {
		t.Fatal("audit head mismatch")
	}
	for _, in := range []string{
		`{"tables":[{"name":"x","rows":1,"sha256":"zz"}],"auditHead":{"seq":0,"hash":""}}`,
		`{"tables":[{"name":"X","rows":1,"sha256":"` + sum + `"}],"auditHead":{"seq":0,"hash":""}}`,
		`{"tables":[{"name":"x","rows":-1,"sha256":"` + sum + `"}],"auditHead":{"seq":0,"hash":""}}`,
		`{"tables":[],"auditHead":{"seq":-1,"hash":""}}`,
		`{"tables":[],"auditHead":{"seq":1,"hash":"nothex"}}`,
		`{"tables":[],"extra":1}`,
	} {
		if _, err := ParseBackupDigest([]byte(in)); !errors.Is(err, ErrBackupInvalid) {
			t.Errorf("%s accepted", in)
		}
	}
	h := validHeader()
	audit, _ := h.Table("audit_log")
	if ref, ok := AuditHeadOf(audit, []any{int64(9), time.Now().UTC(), sum}); !ok || ref.Seq != 9 || ref.Hash != sum {
		t.Fatal("AuditHeadOf")
	}
	if _, ok := AuditHeadOf(h.Tables[0], []any{"ZAR", int64(2)}); ok {
		t.Fatal("not the audit table")
	}
	if _, ok := AuditHeadOf(audit, []any{"9", nil, sum}); ok {
		t.Fatal("bad types")
	}
	if _, ok := AuditHeadOf(BackupTable{Name: AuditTableName}, nil); ok {
		t.Fatal("no columns")
	}
}

func TestBackupNames(t *testing.T) {
	n := BackupName{CreatedAt: time.Date(2026, 10, 15, 4, 0, 0, 0, time.FixedZone("SAST", 7200)), Schema: 14, Kind: BackupScheduled}
	if n.String() != "cactusinv-20261015T020000Z-s14-scheduled.age" {
		t.Fatal(n.String())
	}
	got, err := ParseBackupName(n.String())
	if err != nil || !got.CreatedAt.Equal(n.CreatedAt) || got.Schema != 14 || got.Kind != BackupScheduled {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := ParseBackupName("cactusinv-20261015T020000Z-s0-upload.age"); err != nil || got.Schema != 0 || got.Kind != BackupUpload {
		t.Fatal("upload")
	}
	if got, err := ParseBackupName("cactusinv-20261015T020000Z-s14-prerestore.age"); err != nil || got.Kind != BackupPreRestore {
		t.Fatal("prerestore")
	}
	for _, s := range []string{
		"", "cactusinv-20261015T020000Z-s14-scheduled.age.part", "../cactusinv-20261015T020000Z-s14-manual.age",
		"cactusinv-20261015T020000Z-s14-evil.age", "cactusinv-20261015T020000Z-s014-manual.age",
		"cactusinv-20261315T020000Z-s14-manual.age", "cactusinv-20261015T020000Z-s14-manual.age\n",
		"x/cactusinv-20261015T020000Z-s14-manual.age", "cactusinv-20260230T020000Z-s1-manual.age",
	} {
		if _, err := ParseBackupName(s); !errors.Is(err, ErrValidation) {
			t.Errorf("%q accepted", s)
		}
	}
	if !IsLegacyBackupName("cactusinv-20261015-020000.sql.gz") || IsLegacyBackupName("../cactusinv-20261015-020000.sql.gz") {
		t.Fatal("legacy names")
	}
}

func TestBackupPassphraseAndConfirmation(t *testing.T) {
	cases := map[string]bool{
		"":                        false,
		"fifteen chars!!":         false,
		"sixteen chars!!!":        true,
		"ÄÖÜäöüßÄÖÜäöüßÄÖ":        true, // 16 runes
		strings.Repeat("x", 1025): false,
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff": false,
	}
	for p, ok := range cases {
		if err := ValidateBackupPassphrase(p); (err == nil) != ok {
			t.Errorf("%q: %v", p, err)
		}
	}
	if CheckRestoreConfirmation(" RESTORE ") != nil || CheckRestoreConfirmation("restore") == nil || CheckRestoreConfirmation("") == nil {
		t.Fatal("confirmation")
	}
}

func TestSystemState(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	s := SystemState{RestoreID: "r1", RestoreExpiresAt: now.Add(RestoreStageTTL), RestoreSource: "x", RestoreHeader: validHeader()}
	if !s.HasStagedRestore(now) || s.HasStagedRestore(now.Add(RestoreStageTTL)) {
		t.Fatal("expiry")
	}
	s.ClearRestore()
	if s.HasStagedRestore(now) || s.RestoreSource != "" || len(s.RestoreHeader.Tables) != 0 {
		t.Fatal("cleared")
	}
	s.Hold(now, "albie", "restore of x")
	if !s.AutomationHeld || s.HeldBy != "albie" {
		t.Fatal("hold")
	}
	s.Resume()
	if s.AutomationHeld || !s.HeldAt.IsZero() {
		t.Fatal("resume")
	}
}

// ---- audit merge ----

func randomEntry(r *rand.Rand, i int) AuditEntry {
	return AuditEntry{
		At: time.Date(2026, 1, 1, 0, 0, i, r.IntN(1e9), time.UTC), ActorID: fmt.Sprint("u", r.IntN(3)),
		ActorName: fmt.Sprint("user ", r.IntN(3)), Action: fmt.Sprint("invoice.a", r.IntN(5)),
		EntityType: "invoice", EntityID: fmt.Sprint("inv-", r.IntN(100)), Summary: fmt.Sprint("summary ", r.Int()),
	}
}

// sealOnto appends entries to chain the way the stores do: Seq = last + 1, sealed onto
// the head.
func sealOnto(chain []AuditEntry, entries ...AuditEntry) []AuditEntry {
	for _, e := range entries {
		prev := ""
		if n := len(chain); n > 0 {
			prev = chain[n-1].Hash
		}
		e.Seq = int64(len(chain) + 1)
		e.Seal(prev)
		chain = append(chain, e)
	}
	return chain
}

func content(e AuditEntry) string {
	return strings.Join([]string{e.At.Format(AuditTimeLayout), e.ActorID, e.ActorName, e.Action, e.EntityType, e.EntityID, e.Summary}, "|")
}

// applyMerge appends the plan's appendix to current like the restore use case does.
func applyMerge(t *testing.T, current []AuditEntry, p AuditMergePlan, backup []AuditEntry) []AuditEntry {
	t.Helper()
	out := slices.Clone(current)
	for _, e := range p.Appendix(backup, AuditMarker{At: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), ActorID: "u9", ActorName: "restorer"}) {
		if p.Mode == AuditAppendVerbatim {
			head := ""
			if n := len(out); n > 0 {
				head = out[n-1].Hash
			}
			if e.PrevHash != head || e.Seq != int64(len(out)+1) || e.ComputeHash(e.PrevHash) != e.Hash {
				t.Fatalf("verbatim entry #%d does not chain from the head", e.Seq)
			}
			out = append(out, e)
			continue
		}
		out = sealOnto(out, e)
	}
	return out
}

// TestPlanAuditMergeProperty: for random trails with a random fork point, the merged
// trail verifies, keeps every current entry unchanged and contains every backup entry's
// content.
func TestPlanAuditMergeProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	modes := map[AuditMergeMode]int{}
	for iter := range 2000 {
		shared := r.IntN(6)
		var base []AuditEntry
		for i := range shared {
			base = sealOnto(base, randomEntry(r, i))
		}
		current, backup := slices.Clone(base), slices.Clone(base)
		switch r.IntN(4) {
		case 0: // backup is behind current (restore an older backup of this installation)
			for i := range r.IntN(5) {
				current = sealOnto(current, randomEntry(r, 100+i))
			}
		case 1: // current is behind the backup (e.g. a fresh, empty trail)
			for i := range r.IntN(5) {
				backup = sealOnto(backup, randomEntry(r, 200+i))
			}
		default: // diverged
			for i := range r.IntN(5) {
				current = sealOnto(current, randomEntry(r, 300+i))
			}
			for i := range r.IntN(5) {
				backup = sealOnto(backup, randomEntry(r, 400+i))
			}
		}
		// A backup whose own chain was tampered with after the shared part. (A tampered copy
		// of a shared entry keeps its hash and is matched to the intact live entry.)
		if extra := len(backup) - len(base); extra > 0 && r.IntN(5) == 0 {
			backup[len(base)+r.IntN(extra)].Summary += " (altered)"
		}
		p := PlanAuditMerge(current, backup)
		modes[p.Mode]++
		merged := applyMerge(t, current, p, backup)
		if p.BackupBroken && p.Mode == AuditAppendVerbatim {
			t.Fatalf("iter %d: a broken backup chain was appended verbatim", iter)
		}
		currentOK := VerifyAuditChain(current).OK
		if v := VerifyAuditChain(merged); currentOK && !v.OK {
			t.Fatalf("iter %d (%s): merged chain broken at #%d: %s", iter, p.Mode, v.BrokenAt, v.Reason)
		}
		for i, e := range current {
			if merged[i] != e {
				t.Fatalf("iter %d: current entry #%d changed", iter, e.Seq)
			}
		}
		have := map[string]int{}
		for _, e := range merged {
			have[content(e)]++
		}
		for _, e := range backup {
			if have[content(e)] == 0 {
				t.Fatalf("iter %d (%s): backup entry #%d lost", iter, p.Mode, e.Seq)
			}
		}
		if p.Mode == AuditAppendResealed {
			begin, _, _, end := p.ResealSeqs()
			if merged[begin-1].Action != AuditResealBegin || merged[end-1].Action != AuditResealEnd || int(end) != len(merged) {
				t.Fatalf("iter %d: markers not at #%d and #%d", iter, begin, end)
			}
		}
		if p.Summary() == "" {
			t.Fatal("summary")
		}
	}
	for _, m := range []AuditMergeMode{AuditKeepCurrent, AuditAppendVerbatim, AuditAppendResealed} {
		if modes[m] == 0 {
			t.Fatalf("mode %s never exercised: %v", m, modes)
		}
	}
}

func TestPlanAuditMergeModes(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	var base []AuditEntry
	for i := range 3 {
		base = sealOnto(base, randomEntry(r, i))
	}
	longer := sealOnto(slices.Clone(base), randomEntry(r, 10), randomEntry(r, 11))
	fork := sealOnto(slices.Clone(base), randomEntry(r, 20))

	if p := PlanAuditMerge(longer, base); p.Mode != AuditKeepCurrent || p.CommonSeq != 3 || p.Count != 0 {
		t.Fatalf("keep: %+v", p)
	}
	if p := PlanAuditMerge(base, longer); p.Mode != AuditAppendVerbatim || p.FromSeq != 4 || p.Count != 2 || p.BackupHead.Seq != 5 {
		t.Fatalf("verbatim: %+v", p)
	}
	if p := PlanAuditMerge(nil, longer); p.Mode != AuditAppendVerbatim || p.FromSeq != 1 || p.Count != 5 {
		t.Fatalf("verbatim into empty: %+v", p)
	}
	p := PlanAuditMerge(fork, longer)
	if p.Mode != AuditAppendResealed || p.CommonSeq != 3 || p.FromSeq != 4 || p.Count != 2 {
		t.Fatalf("resealed: %+v", p)
	}
	app := p.Appendix(longer, AuditMarker{ActorName: "x"})
	if len(app) != 4 || app[0].Action != AuditResealBegin || app[3].Action != AuditResealEnd || app[1].Hash != "" || app[1].Summary != longer[3].Summary {
		t.Fatalf("appendix: %+v", app)
	}
	if !strings.Contains(app[0].Summary, longer[4].Hash) {
		t.Fatal("marker records the original head")
	}
	// fork has 4 entries: begin #5, re-sealed #6–#7, end #8.
	if b, f, l, e := p.ResealSeqs(); b != 5 || f != 6 || l != 7 || e != 8 ||
		!strings.Contains(app[0].Summary, "the next 2 entries (#6–#7)") || !strings.Contains(app[0].Summary, "ends at marker #8") ||
		!strings.Contains(app[3].Summary, "begun at marker #5") || !strings.Contains(p.Summary(), "between markers #5 and #8") {
		t.Fatalf("markers: %q / %q / %q", app[0].Summary, app[3].Summary, p.Summary())
	}
	if s := PlanAuditMerge(base, longer).Summary(); !strings.Contains(s, "(#4–#5)") {
		t.Fatalf("verbatim range: %q", s)
	}
	if p := PlanAuditMerge(nil, nil); p.Mode != AuditKeepCurrent || p.Appendix(nil, AuditMarker{}) != nil {
		t.Fatal("both empty")
	}
	broken := slices.Clone(longer)
	broken[4].Summary = "x"
	if p := PlanAuditMerge(base, broken); p.Mode != AuditAppendResealed || !p.BackupBroken || p.BackupBrokenAt != 5 || !strings.Contains(p.Summary(), "broken") {
		t.Fatalf("broken backup: %+v", p)
	}
	if !strings.Contains(PlanAuditMerge(base, longer).Summary(), "unchanged") || !strings.Contains(PlanAuditMerge(longer, base).Summary(), "keep") {
		t.Fatal("summaries")
	}
}

// ---- schedule ----

func TestParseBackupTimes(t *testing.T) {
	got, err := ParseBackupTimes(" 14:30, 02:00,,14:30 ")
	if err != nil || !slices.Equal(got, []TimeOfDay{{2, 0}, {14, 30}}) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := ParseBackupTimes(""); err != nil || len(got) != 0 {
		t.Fatal("empty")
	}
	for _, s := range []string{"2:00", "24:00", "02:60", "noon", "02:00;03:00"} {
		if _, err := ParseBackupTimes(s); !errors.Is(err, ErrValidation) {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestBackupSlots(t *testing.T) {
	jhb, _ := time.LoadLocation("Africa/Johannesburg")
	london, _ := time.LoadLocation("Europe/London")
	times, _ := ParseBackupTimes("02:00,14:00")
	at := func(loc *time.Location, y int, m time.Month, d, h, mi int) time.Time {
		return time.Date(y, m, d, h, mi, 0, 0, loc)
	}
	cases := []struct {
		name      string
		times     []TimeOfDay
		loc       *time.Location
		now       time.Time
		due, next time.Time
	}{
		{"between", times, jhb, at(jhb, 2026, 10, 15, 9, 0), at(jhb, 2026, 10, 15, 2, 0), at(jhb, 2026, 10, 15, 14, 0)},
		{"exactly at a slot", times, jhb, at(jhb, 2026, 10, 15, 14, 0), at(jhb, 2026, 10, 15, 14, 0), at(jhb, 2026, 10, 16, 2, 0)},
		{"before midnight", times, jhb, at(jhb, 2026, 12, 31, 23, 59), at(jhb, 2026, 12, 31, 14, 0), at(jhb, 2027, 1, 1, 2, 0)},
		{"just after midnight", times, jhb, at(jhb, 2027, 1, 1, 0, 1), at(jhb, 2026, 12, 31, 14, 0), at(jhb, 2027, 1, 1, 2, 0)},
		{"midnight slot", []TimeOfDay{{0, 0}}, jhb, at(jhb, 2026, 3, 1, 0, 0), at(jhb, 2026, 3, 1, 0, 0), at(jhb, 2026, 3, 2, 0, 0)},
		// 29 March 2026: London clocks go 01:00 → 02:00; 01:30 does not exist that day.
		{
			"spring forward",
			[]TimeOfDay{{1, 30}},
			london, time.Date(2026, 3, 29, 3, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 29, 1, 30, 0, 0, london), time.Date(2026, 3, 30, 1, 30, 0, 0, london),
		},
		// 25 October 2026: 01:30 happens twice; exactly one slot that day.
		{
			"fall back",
			[]TimeOfDay{{1, 30}},
			london, time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 25, 1, 30, 0, 0, london), time.Date(2026, 10, 26, 1, 30, 0, 0, london),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			due, ok := DueBackupSlot(c.times, c.loc, c.now)
			if !ok || !due.Equal(c.due) {
				t.Errorf("due = %v, want %v", due, c.due)
			}
			next, ok := NextBackupSlot(c.times, c.loc, c.now)
			if !ok || !next.Equal(c.next) {
				t.Errorf("next = %v, want %v", next, c.next)
			}
			again, _ := DueBackupSlot(c.times, c.loc, c.now.Add(time.Minute))
			if c.now.Add(time.Minute).Before(c.next) && !again.Equal(due) {
				t.Error("the due slot is stable until the next one")
			}
		})
	}
	// Each local day has exactly one 01:30 slot across both DST changes.
	for _, day := range []time.Time{time.Date(2026, 3, 29, 12, 0, 0, 0, london), time.Date(2026, 10, 25, 12, 0, 0, 0, london)} {
		n := 0
		for _, s := range backupSlots([]TimeOfDay{{1, 30}}, london, day) {
			if l := s.In(london); l.Day() == day.Day() && l.Month() == day.Month() {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%v: %d slots", day, n)
		}
	}
	if _, ok := DueBackupSlot(nil, jhb, time.Now()); ok {
		t.Fatal("no times, no slot")
	}
	if _, ok := NextBackupSlot(nil, jhb, time.Now()); ok {
		t.Fatal("no times, no next slot")
	}
}

func TestSelectPrunable(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	files := []BackupFile{
		{Name: "a.age", Time: day(30)},
		{Name: "b.age", Time: day(20)},
		{Name: "c.age", Time: day(1)},
		{Name: "old.sql.gz", Time: day(40), Legacy: true},
		{Name: "new.sql.gz", Time: day(2), Legacy: true},
	}
	if got := SelectPrunable(files, now, 14); !slices.Equal(got, []string{"a.age", "b.age", "old.sql.gz"}) {
		t.Fatalf("%v", got)
	}
	// Everything is old: the newest file and the newest encrypted backup survive.
	old := []BackupFile{{Name: "x.age", Time: day(50)}, {Name: "y.age", Time: day(40)}, {Name: "z.sql.gz", Time: day(30), Legacy: true}}
	if got := SelectPrunable(old, now, 14); !slices.Equal(got, []string{"x.age"}) {
		t.Fatalf("never the newest: %v", got)
	}
	if SelectPrunable(files, now, 0) != nil || SelectPrunable(nil, now, 14) != nil {
		t.Fatal("disabled / empty")
	}
}

// ---- fuzz ----

func FuzzParseBackupName(f *testing.F) {
	f.Add("cactusinv-20261015T020000Z-s14-scheduled.age")
	f.Add("cactusinv-20261015T020000Z-s0-upload.age")
	f.Add("../../etc/passwd")
	f.Fuzz(func(t *testing.T, s string) {
		n, err := ParseBackupName(s)
		if err != nil {
			return
		}
		if n.String() != s {
			t.Fatalf("%q parsed but prints %q", s, n.String())
		}
		if strings.ContainsAny(s, `/\`) || strings.Contains(s, "..") {
			t.Fatalf("%q is a path", s)
		}
	})
}

func FuzzParseBackupHeader(f *testing.F) {
	data, _ := json.Marshal(validHeader())
	f.Add(data)
	f.Add([]byte(`{"formatVersion":1}`))
	f.Add([]byte(`{"formatVersion":1,"tables":[{"name":"x","columns":[{"name":"y","kind":"int"}]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		h, err := ParseBackupHeader(data)
		if err != nil {
			if !errors.Is(err, ErrBackupInvalid) && !errors.Is(err, ErrBackupUnsupported) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			return
		}
		for _, tb := range h.Tables {
			if !identRe.MatchString(tb.Name) || IsBackupExcluded(tb.Name) {
				t.Fatalf("accepted table %q", tb.Name)
			}
			for _, c := range tb.Columns {
				if !identRe.MatchString(c.Name) || !c.Kind.Valid() {
					t.Fatalf("accepted column %q", c.Name)
				}
			}
		}
	})
}

func FuzzParseBackupDigest(f *testing.F) {
	f.Add([]byte(`{"tables":[{"name":"x","rows":1,"sha256":"` + strings.Repeat("0", 64) + `"}],"auditHead":{"seq":1,"hash":""}}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := ParseBackupDigest(data)
		if err != nil {
			if !errors.Is(err, ErrBackupInvalid) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			return
		}
		if VerifyBackupDigest(d, d) != nil {
			t.Fatal("a digest equals itself")
		}
	})
}

// TestForgedResealMarker: a backup carrying its own "end of re-seal" entry cannot pass it
// off as the real one: the real markers name the exact range, and the forged entry sits
// inside it.
func TestForgedResealMarker(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	var live []AuditEntry
	for i := range 3 {
		live = sealOnto(live, randomEntry(r, i))
	}
	var backup []AuditEntry
	backup = sealOnto(backup, randomEntry(r, 10))
	backup = sealOnto(backup, AuditEntry{
		At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), ActorName: "restorer", Action: AuditResealEnd, EntityType: "backup",
		Summary: "end of the block begun at marker #4: 0 re-sealed entries",
	})
	backup = sealOnto(backup, randomEntry(r, 11)) // what the forger wants to look authentic
	p := PlanAuditMerge(live, backup)
	merged := applyMerge(t, live, p, backup)
	begin, first, last, end := p.ResealSeqs()
	if merged[begin-1].Action != AuditResealBegin || merged[end-1].Action != AuditResealEnd || int(end) != len(merged) {
		t.Fatalf("markers at #%d and #%d", begin, end)
	}
	if !strings.Contains(merged[end-1].Summary, fmt.Sprintf("begun at marker #%d", begin)) {
		t.Fatalf("real end marker: %q", merged[end-1].Summary)
	}
	forged := slices.IndexFunc(merged, func(e AuditEntry) bool {
		return e.Action == AuditResealEnd && e.Seq != end
	})
	if forged < 0 || merged[forged].Seq < first || merged[forged].Seq > last {
		t.Fatalf("the forged marker must sit inside the announced range #%d–#%d", first, last)
	}
	if !strings.Contains(merged[begin-1].Summary, fmt.Sprintf("ends at marker #%d", end)) {
		t.Fatal("begin marker names its end")
	}
}

func TestTruncateUTF8(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"ab€", 4, "ab"}, // € is 3 bytes: never split
		{"ab€c", 5, "ab€"},
		{"ab€", 6, "ab€"},
		{"🌵🌵", 7, "🌵"}, // 4-byte rune
		{"🌵", 3, ""},
		{"Grüße", 3, "Gr"},
		{"x", 0, ""},
		{"x", -1, ""},
	}
	for _, c := range cases {
		got := TruncateUTF8(c.in, c.max)
		if got != c.want || !utf8.ValidString(got) || len(got) > max(c.max, 0) {
			t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
	// Every truncating domain path stays valid UTF-8 at its limit.
	long := strings.Repeat("é", 600) // 1200 bytes
	e := AuditEntry{At: time.Now(), Summary: long}
	e.Seal("")
	m := EmailMessage{}
	m.Failed(long, time.Now())
	sc := Schedule{}
	sc.RecordFailure(long, time.Now())
	var st SystemState
	st.Hold(time.Now(), strings.Repeat("ü", 200), long)
	for name, v := range map[string]string{"audit summary": e.Summary, "email error": m.LastError, "schedule error": sc.LastError, "held by": st.HeldBy, "held reason": st.HeldReason} {
		if !utf8.ValidString(v) || v == "" {
			t.Errorf("%s: invalid UTF-8 after truncation", name)
		}
	}
	if len(e.Summary) != 1000 || len(st.HeldBy) != 254 || len(st.HeldReason) != 500 {
		t.Errorf("lengths %d %d %d", len(e.Summary), len(st.HeldBy), len(st.HeldReason))
	}
}

// TestForgedEndMarkerInTheRealFormat: a forger who predicts the live head writes an end
// marker in the exact real format with the right ranges. It still lands inside the
// announced range [first, last], and it cannot name the real original head hash, which
// depends on the forged entry itself.
func TestForgedEndMarkerInTheRealFormat(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	var live []AuditEntry
	for i := range 3 {
		live = sealOnto(live, randomEntry(r, i))
	}
	// The forger builds a placeholder of the same length to predict seqs and the head.
	placeholder := sealOnto(nil, randomEntry(r, 20), randomEntry(r, 21), randomEntry(r, 22))
	pp := PlanAuditMerge(live, placeholder)
	begin, first, last, end := pp.ResealSeqs()
	forgedSummary := fmt.Sprintf("end of the block begun at marker #%d: %d re-sealed entries #%d–#%d (original head #%d %s)",
		begin, pp.Count, first, last, pp.BackupHead.Seq, pp.BackupHead.Hash)
	backup := sealOnto(nil, placeholder[0],
		AuditEntry{At: placeholder[1].At, ActorName: "restorer", Action: AuditResealEnd, EntityType: "backup", Summary: forgedSummary},
		placeholder[2])
	p := PlanAuditMerge(live, backup)
	if b2, f2, l2, e2 := p.ResealSeqs(); b2 != begin || f2 != first || l2 != last || e2 != end {
		t.Fatal("the forger predicted the ranges correctly (the premise of this test)")
	}
	merged := applyMerge(t, live, p, backup)
	var forged, real AuditEntry
	for _, e := range merged {
		if e.Action == AuditResealEnd {
			if e.Seq == end {
				real = e
			} else {
				forged = e
			}
		}
	}
	if forged.Seq < first || forged.Seq > last {
		t.Fatalf("forged marker #%d outside the announced range #%d–#%d", forged.Seq, first, last)
	}
	if real.Seq != end || real.ActorID != "u9" || !strings.Contains(real.Summary, p.BackupHead.Hash) {
		t.Fatalf("real end marker: %+v", real)
	}
	if strings.Contains(forged.Summary, p.BackupHead.Hash) {
		t.Fatal("the forged marker names the real original head")
	}
}
