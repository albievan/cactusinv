package fakes

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// The backup fakes model a database of four tables taken from the fake Store:
// invoice_sequences, users, smtp_settings and audit_log. That is enough for the use cases'
// rules (numbers never rewind, sign-in after a restore, SMTP password, audit merge).

var fakeTables = []domain.BackupTable{
	{Name: "invoice_sequences", Columns: []domain.BackupColumn{{Name: "name", Kind: domain.ColumnText}, {Name: "next_value", Kind: domain.ColumnInt}}},
	{Name: "users", Columns: []domain.BackupColumn{{Name: "id", Kind: domain.ColumnText}, {Name: "email", Kind: domain.ColumnText}, {Name: "password_hash", Kind: domain.ColumnText}, {Name: "created_at", Kind: domain.ColumnDateTime}}},
	{Name: "smtp_settings", Columns: []domain.BackupColumn{{Name: "id", Kind: domain.ColumnInt}, {Name: "host", Kind: domain.ColumnText}, {Name: "version", Kind: domain.ColumnInt}, {Name: "password_enc", Kind: domain.ColumnBytes}}},
	{Name: "audit_log", Columns: []domain.BackupColumn{
		{Name: "seq", Kind: domain.ColumnInt},
		{Name: "at", Kind: domain.ColumnDateTime},
		{Name: "actor_id", Kind: domain.ColumnText},
		{Name: "actor_name", Kind: domain.ColumnText},
		{Name: "action", Kind: domain.ColumnText},
		{Name: "entity_type", Kind: domain.ColumnText},
		{Name: "entity_id", Kind: domain.ColumnText},
		{Name: "summary", Kind: domain.ColumnText},
		{Name: "prev_hash", Kind: domain.ColumnText},
		{Name: "hash", Kind: domain.ColumnText},
	}},
}

func auditRow(e domain.AuditEntry) []any {
	return []any{e.Seq, e.At.UTC(), e.ActorID, e.ActorName, e.Action, e.EntityType, e.EntityID, e.Summary, e.PrevHash, e.Hash}
}

func auditFromRow(r []any) domain.AuditEntry {
	s := func(i int) string { v, _ := r[i].(string); return v }
	seq, _ := r[0].(int64)
	at, _ := r[1].(time.Time)
	return domain.AuditEntry{Seq: seq, At: at, ActorID: s(2), ActorName: s(3), Action: s(4), EntityType: s(5), EntityID: s(6), Summary: s(7), PrevHash: s(8), Hash: s(9)}
}

// rowsOf exports the fake store's state as table rows.
func rowsOf(st *state) map[string][][]any {
	out := map[string][][]any{"invoice_sequences": {{"invoice", st.seq}}}
	ids := slices.Sorted(maps.Keys(st.users))
	for _, id := range ids {
		u := st.users[id]
		out["users"] = append(out["users"], []any{u.ID, u.Email, u.PasswordHash, u.CreatedAt.UTC()})
	}
	if st.smtp != nil {
		var pw any
		if st.smtpPass != nil {
			pw = slices.Clone(st.smtpPass)
		}
		out["smtp_settings"] = [][]any{{int64(1), st.smtp.Host, int64(st.smtp.Version), pw}}
	}
	for _, e := range st.audit {
		out["audit_log"] = append(out["audit_log"], auditRow(e))
	}
	return out
}

// Snapshotter exports the fake Store (ports.Snapshotter).
type Snapshotter struct {
	Store  *Store
	Schema int64 // default 13
	Err    error
}

type fakeSource struct {
	schema int64
	rows   map[string][][]any
}

func (f fakeSource) SchemaVersion() int64         { return f.schema }
func (f fakeSource) Tables() []domain.BackupTable { return slices.Clone(fakeTables) }
func (f fakeSource) ScanTable(_ context.Context, table string, fn func([]any) error) error {
	for _, r := range f.rows[table] {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot implements ports.Snapshotter.
func (s *Snapshotter) Snapshot(ctx context.Context, fn func(context.Context, ports.BackupSource) error) error {
	if s.Err != nil {
		return s.Err
	}
	s.Store.mu.Lock()
	src := fakeSource{schema: s.schema(), rows: rowsOf(s.Store.st)}
	s.Store.mu.Unlock()
	return fn(ctx, src)
}

func (s *Snapshotter) schema() int64 {
	if s.Schema == 0 {
		return 13
	}
	return s.Schema
}

// ---- codec ----

// Codec writes a JSON-lines stand-in for the backup format (ports.BackupCodec).
type Codec struct{}

type codecValue struct {
	K string    `json:"k"`
	I int64     `json:"i,omitempty"`
	S string    `json:"s,omitempty"`
	B []byte    `json:"b,omitempty"`
	T time.Time `json:"t,omitzero"`
}

type codecLine struct {
	Header *domain.BackupHeader `json:"header,omitempty"`
	Table  string               `json:"table,omitempty"`
	Row    []codecValue         `json:"row,omitempty"`
	Counts map[string]int64     `json:"counts,omitempty"`
}

func encodeValue(v any) codecValue {
	switch x := v.(type) {
	case int64:
		return codecValue{K: "i", I: x}
	case string:
		return codecValue{K: "s", S: x}
	case []byte:
		return codecValue{K: "b", B: x}
	case time.Time:
		return codecValue{K: "t", T: x}
	}
	return codecValue{K: "n"}
}

func decodeValue(c codecValue) any {
	switch c.K {
	case "i":
		return c.I
	case "s":
		return c.S
	case "b":
		if c.B == nil {
			return []byte{}
		}
		return c.B
	case "t":
		return c.T.UTC()
	}
	return nil
}

// Encode implements ports.BackupCodec.
func (Codec) Encode(ctx context.Context, w io.Writer, h domain.BackupHeader, src ports.BackupSource) (domain.BackupDigest, error) {
	enc := json.NewEncoder(w)
	if err := enc.Encode(codecLine{Header: &h}); err != nil {
		return domain.BackupDigest{}, err
	}
	counts := map[string]int64{}
	var d domain.BackupDigest
	for _, t := range h.Tables {
		err := src.ScanTable(ctx, t.Name, func(row []any) error {
			vals := make([]codecValue, len(row))
			for i, v := range row {
				vals[i] = encodeValue(v)
			}
			counts[t.Name]++
			if ref, ok := domain.AuditHeadOf(t, row); ok {
				d.AuditHead = ref
			}
			return enc.Encode(codecLine{Table: t.Name, Row: vals})
		})
		if err != nil {
			return domain.BackupDigest{}, err
		}
		d.Tables = append(d.Tables, domain.TableDigest{Name: t.Name, Rows: counts[t.Name], SHA256: strings.Repeat("0", 64)})
	}
	return d, enc.Encode(codecLine{Counts: counts})
}

// Decode implements ports.BackupCodec.
func (Codec) Decode(ctx context.Context, r io.Reader, sink ports.BackupSink) (domain.BackupHeader, domain.BackupDigest, error) {
	invalid := func(why string) error { return fmt.Errorf("%w: fake codec: %s", domain.ErrBackupInvalid, why) }
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	var h domain.BackupHeader
	counts := map[string]int64{}
	first := true
	for sc.Scan() {
		var l codecLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return h, domain.BackupDigest{}, invalid("not JSON")
		}
		switch {
		case first:
			if l.Header == nil {
				return h, domain.BackupDigest{}, invalid("no header")
			}
			h = *l.Header
			if err := h.Validate(); err != nil {
				return h, domain.BackupDigest{}, err
			}
			if err := sink.Header(ctx, h); err != nil {
				return h, domain.BackupDigest{}, err
			}
			first = false
		case l.Counts != nil:
			if !maps.Equal(l.Counts, counts) {
				return h, domain.BackupDigest{}, invalid("digest mismatch")
			}
			if sc.Scan() {
				return h, domain.BackupDigest{}, invalid("data after the digest")
			}
			var d domain.BackupDigest
			for _, t := range h.Tables {
				d.Tables = append(d.Tables, domain.TableDigest{Name: t.Name, Rows: counts[t.Name], SHA256: strings.Repeat("0", 64)})
			}
			return h, d, nil
		default:
			row := make([]any, len(l.Row))
			for i, v := range l.Row {
				row[i] = decodeValue(v)
			}
			counts[l.Table]++
			if err := sink.Row(ctx, l.Table, row); err != nil {
				return h, domain.BackupDigest{}, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return h, domain.BackupDigest{}, err
	}
	return h, domain.BackupDigest{}, invalid("truncated")
}

// ---- sealer ----

// Sealer marks plaintext with a key or passphrase line instead of encrypting
// (ports.BackupSealer).
type Sealer struct {
	Key bool // whether the server backup key is configured
}

const sealKeyLine = "fake-age key\n"

func passLine(p string) string {
	sum := sha256.Sum256([]byte(p))
	return "fake-age scrypt " + hex.EncodeToString(sum[:]) + "\n"
}

type prefixWriter struct {
	w      io.Writer
	prefix string
	done   bool
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	if !p.done {
		if _, err := io.WriteString(p.w, p.prefix); err != nil {
			return 0, err
		}
		p.done = true
	}
	return p.w.Write(b)
}

func (p *prefixWriter) Close() error {
	_, err := p.Write(nil)
	return err
}

// KeyConfigured implements ports.BackupSealer.
func (s Sealer) KeyConfigured() bool { return s.Key }

// SealWithKey implements ports.BackupSealer.
func (s Sealer) SealWithKey(w io.Writer) (io.WriteCloser, error) {
	if !s.Key {
		return nil, domain.ErrNotConfigured
	}
	return &prefixWriter{w: w, prefix: sealKeyLine}, nil
}

// SealWithPassphrase implements ports.BackupSealer.
func (s Sealer) SealWithPassphrase(w io.Writer, passphrase string) (io.WriteCloser, error) {
	return &prefixWriter{w: w, prefix: passLine(passphrase)}, nil
}

// Open implements ports.BackupSealer.
func (s Sealer) Open(r io.Reader, passphrase string) (io.Reader, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, domain.ErrBackupInvalid
	}
	switch {
	case passphrase != "" && line == passLine(passphrase):
	case passphrase == "" && s.Key && line == sealKeyLine:
	default:
		return nil, domain.ErrBackupInvalid
	}
	return br, nil
}

// ---- files ----

// Files is an in-memory backups directory (ports.BackupFiles).
type Files struct {
	mu       sync.Mutex
	Clock    *Clock
	files    map[string]fakeFile
	reserved map[string]bool
	// FailCommit makes the next Commit fail.
	FailCommit error
}

type fakeFile struct {
	data []byte
	mod  time.Time
}

func checkName(name string, legacyOK bool) error {
	if _, err := domain.ParseBackupName(name); err == nil || (legacyOK && domain.IsLegacyBackupName(name)) {
		return nil
	}
	return &domain.ValidationError{Field: "backup", Reason: "is not a backup name"}
}

type fileWriter struct {
	f    *Files
	name string
	buf  bytes.Buffer
	done bool
}

func (w *fileWriter) Write(b []byte) (int, error) { return w.buf.Write(b) }

func (w *fileWriter) Commit() error {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	if w.done {
		return errors.New("fake files: already finished")
	}
	w.done = true
	delete(w.f.reserved, w.name)
	if err := w.f.FailCommit; err != nil {
		w.f.FailCommit = nil
		return err
	}
	w.f.put(w.name, w.buf.Bytes())
	return nil
}

func (w *fileWriter) Abort() error {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	if !w.done {
		delete(w.f.reserved, w.name)
	}
	w.done = true
	return nil
}

func (f *Files) put(name string, data []byte) {
	if f.files == nil {
		f.files = map[string]fakeFile{}
	}
	mod := time.Time{}
	if f.Clock != nil {
		mod = f.Clock.Now()
	}
	f.files[name] = fakeFile{data: slices.Clone(data), mod: mod}
}

// Put stores a file directly (tests), e.g. a legacy dump with a given age.
func (f *Files) Put(name string, data []byte, mod time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.put(name, data)
	ff := f.files[name]
	ff.mod = mod
	f.files[name] = ff
}

// Data returns a stored file's bytes (tests).
func (f *Files) Data(name string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.files[name].data)
}

// Create implements ports.BackupFiles.
func (f *Files) Create(_ context.Context, name string) (ports.BackupFileWriter, error) {
	if err := checkName(name, false); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[name]; ok || f.reserved[name] {
		return nil, domain.ErrConflict
	}
	if f.reserved == nil {
		f.reserved = map[string]bool{}
	}
	f.reserved[name] = true
	return &fileWriter{f: f, name: name}, nil
}

// Open implements ports.BackupFiles.
func (f *Files) Open(_ context.Context, name string) (io.ReadCloser, error) {
	if err := checkName(name, true); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ff, ok := f.files[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(slices.Clone(ff.data))), nil
}

// List implements ports.BackupFiles.
func (f *Files) List(_ context.Context) ([]ports.BackupFileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ports.BackupFileInfo
	for name, ff := range f.files {
		out = append(out, ports.BackupFileInfo{Name: name, Size: int64(len(ff.data)), ModTime: ff.mod, Legacy: domain.IsLegacyBackupName(name)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Remove implements ports.BackupFiles.
func (f *Files) Remove(_ context.Context, name string) error {
	if err := checkName(name, true); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[name]; !ok {
		return domain.ErrNotFound
	}
	delete(f.files, name)
	return nil
}

// ---- stager ----

// Stager stages a backup in memory and applies it to the fake Store
// (ports.RestoreStager).
type Stager struct {
	mu       sync.Mutex
	Store    *Store
	Schema   int64 // the live schema; default 13
	NotReady error
	// FailReplace makes ReplaceTables fail (after changing data, to prove the rollback).
	FailReplace error
	// CommitErrAfterCommit makes Apply return this error although the swap committed
	// (a COMMIT whose acknowledgement was lost).
	CommitErrAfterCommit error
	// CommitErrWithoutCommit makes Apply roll the swap back and return this error.
	CommitErrWithoutCommit error
	// AfterCommitError runs after either commit error, outside the swap (e.g. to change
	// the system state as another replica would).
	AfterCommitError func()
	rows             map[string][][]any
	staged           bool
	lock             sync.Mutex
}

type stagerSink struct{ s *Stager }

func (k stagerSink) Header(_ context.Context, h domain.BackupHeader) error {
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	if h.SchemaVersion > k.s.schema() {
		return errors.New("fake stager: no migration for that schema")
	}
	return nil
}

func (k stagerSink) Row(_ context.Context, table string, row []any) error {
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	k.s.rows[table] = append(k.s.rows[table], row)
	return nil
}

func (s *Stager) schema() int64 {
	if s.Schema == 0 {
		return 13
	}
	return s.Schema
}

// Ready implements ports.RestoreStager.
func (s *Stager) Ready(context.Context) error { return s.NotReady }

// Lock implements ports.RestoreStager with an in-memory try-lock (another "process" can
// hold it with HoldLock).
func (s *Stager) Lock(context.Context) (func(), error) {
	if !s.lock.TryLock() {
		return nil, domain.ErrBusy
	}
	return s.lock.Unlock, nil
}

// HoldLock takes the backup lock as another process would; call the result to release.
func (s *Stager) HoldLock() func() {
	s.lock.Lock()
	return s.lock.Unlock
}

// SchemaVersion implements ports.RestoreStager.
func (s *Stager) SchemaVersion(context.Context) (int64, error) { return s.schema(), nil }

// Stage implements ports.RestoreStager.
func (s *Stager) Stage(ctx context.Context, load func(context.Context, ports.BackupSink) error) (ports.StagedRestore, error) {
	if s.NotReady != nil {
		return ports.StagedRestore{}, s.NotReady
	}
	s.mu.Lock()
	s.rows, s.staged = map[string][][]any{}, false
	s.mu.Unlock()
	if err := load(ctx, stagerSink{s}); err != nil {
		return ports.StagedRestore{}, err
	}
	s.mu.Lock()
	s.staged = true
	s.mu.Unlock()
	return s.Staged(ctx)
}

// Staged implements ports.RestoreStager.
func (s *Stager) Staged(context.Context) (ports.StagedRestore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.staged {
		return ports.StagedRestore{}, domain.ErrNotFound
	}
	out := ports.StagedRestore{SchemaVersion: s.schema(), Counts: map[string]int64{}, LiveCounts: map[string]int64{}}
	for _, t := range fakeTables {
		out.Counts[t.Name] = int64(len(s.rows[t.Name]))
	}
	s.Store.mu.Lock()
	for name, rows := range rowsOf(s.Store.st) {
		out.LiveCounts[name] = int64(len(rows))
	}
	s.Store.mu.Unlock()
	for _, r := range s.rows["smtp_settings"] {
		if b, ok := r[3].([]byte); ok {
			out.SMTPPasswordEnc = slices.Clone(b)
		}
	}
	for _, r := range s.rows["users"] {
		id, _ := r[0].(string)
		out.UserIDs = append(out.UserIDs, id)
	}
	return out, nil
}

// StagedAudit implements ports.RestoreStager.
func (s *Stager) StagedAudit(context.Context) ([]domain.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.AuditEntry
	for _, r := range s.rows["audit_log"] {
		out = append(out, auditFromRow(r))
	}
	return out, nil
}

// Discard implements ports.RestoreStager.
func (s *Stager) Discard(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows, s.staged = nil, false
	return nil
}

type restoreTx struct {
	s *Stager
	r ports.Repos
}

func (t restoreTx) Repos() ports.Repos { return t.r }

func (t restoreTx) StagedAudit(ctx context.Context) ([]domain.AuditEntry, error) {
	return t.s.StagedAudit(ctx)
}

func (t restoreTx) ReplaceTables(context.Context) error {
	t.s.mu.Lock()
	rows := t.s.rows
	failure := t.s.FailReplace
	t.s.mu.Unlock()
	return t.s.Store.do(func(st *state) error {
		st.seq = 1
		for _, r := range rows["invoice_sequences"] {
			st.seq, _ = r[1].(int64)
		}
		st.users = map[string]domain.User{}
		for _, r := range rows["users"] {
			u := domain.User{}
			u.ID, _ = r[0].(string)
			u.Email, _ = r[1].(string)
			u.PasswordHash, _ = r[2].(string)
			u.CreatedAt, _ = r[3].(time.Time)
			st.users[u.ID] = u
		}
		st.smtp, st.smtpPass = nil, nil
		for _, r := range rows["smtp_settings"] {
			host, _ := r[1].(string)
			v, _ := r[2].(int64)
			st.smtp = &domain.SMTPSettings{Host: host, Version: int(v)}
			if b, ok := r[3].([]byte); ok {
				st.smtpPass = slices.Clone(b)
			}
		}
		return failure
	})
}

// Apply implements ports.RestoreStager: fn runs in one fake-Store transaction.
func (s *Stager) Apply(ctx context.Context, fn func(context.Context, ports.RestoreTx) error) error {
	s.mu.Lock()
	staged := s.staged
	s.mu.Unlock()
	if !staged {
		return domain.ErrNotFound
	}
	if !s.lock.TryLock() {
		return domain.ErrBusy
	}
	defer s.lock.Unlock()
	err := s.Store.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := fn(ctx, restoreTx{s: s, r: r}); err != nil {
			return err
		}
		return s.CommitErrWithoutCommit // non-nil: rolled back
	})
	if err == nil && s.CommitErrAfterCommit != nil {
		err = s.CommitErrAfterCommit
	}
	if err != nil && (errors.Is(err, s.CommitErrAfterCommit) || errors.Is(err, s.CommitErrWithoutCommit)) && s.AfterCommitError != nil {
		s.AfterCommitError()
	}
	return err
}
