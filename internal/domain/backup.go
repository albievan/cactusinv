package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Backup format and restore rules (ADR-010).
const (
	// BackupFormatVersion is the only backup format this version writes and reads.
	BackupFormatVersion = 1
	// MinBackupSchema is the oldest database schema a backup may come from: v1.4.0's
	// schema, the last before backups existed.
	MinBackupSchema = 12
	// MinBackupPassphraseLength is the shortest accepted passphrase for a download.
	MinBackupPassphraseLength = 16
	// MaxBackupPassphraseLength bounds a passphrase (scrypt input).
	MaxBackupPassphraseLength = 1024
	// RestoreConfirmationWord must be typed to apply a restore.
	RestoreConfirmationWord = "RESTORE"
	// RestoreStageTTL is how long a staged restore can be applied.
	RestoreStageTTL = 30 * time.Minute
	// AuditTableName is the table holding the hash-chained audit trail (ADR-009).
	AuditTableName = "audit_log"

	maxBackupTables  = 256
	maxBackupColumns = 512
	maxBackupText    = 254
)

// backupExcluded are the tables never written to a backup: transient or local to one
// installation (ADR-010). Every other base table is exported.
var backupExcluded = []string{"sessions", "idempotency_keys", "goose_db_version", "audit_head", "system_state", "backup_runs"}

// BackupExcludedTables returns the tables a backup never contains.
func BackupExcludedTables() []string { return slices.Clone(backupExcluded) }

// IsBackupExcluded reports whether table is never written to a backup.
func IsBackupExcluded(table string) bool { return slices.Contains(backupExcluded, table) }

// ColumnKind is how a column's values are represented in a backup.
type ColumnKind string

// Column kinds. Values in a backup row are: nil (NULL); int64 (ColumnInt); string
// (ColumnDecimal, ColumnText; ColumnDate as YYYY-MM-DD); []byte (ColumnBytes); time.Time
// in UTC (ColumnDateTime).
const (
	ColumnInt      ColumnKind = "int"
	ColumnDecimal  ColumnKind = "decimal"
	ColumnText     ColumnKind = "text"
	ColumnBytes    ColumnKind = "bytes"
	ColumnDateTime ColumnKind = "datetime"
	ColumnDate     ColumnKind = "date"
)

// Valid reports whether k is a known kind.
func (k ColumnKind) Valid() bool {
	switch k {
	case ColumnInt, ColumnDecimal, ColumnText, ColumnBytes, ColumnDateTime, ColumnDate:
		return true
	}
	return false
}

var decimalRe = regexp.MustCompile(`^-?[0-9]{1,65}(\.[0-9]{1,30})?$`)

// CheckValue reports whether v is a valid value of kind k.
func (k ColumnKind) CheckValue(v any) error {
	if v == nil {
		return nil
	}
	ok := false
	switch k {
	case ColumnInt:
		_, ok = v.(int64)
	case ColumnDecimal:
		s, isStr := v.(string)
		ok = isStr && decimalRe.MatchString(s)
	case ColumnText:
		s, isStr := v.(string)
		ok = isStr && utf8.ValidString(s)
	case ColumnBytes:
		_, ok = v.([]byte)
	case ColumnDateTime:
		t, isTime := v.(time.Time)
		ok = isTime && t.Location() == time.UTC
	case ColumnDate:
		s, isStr := v.(string)
		if isStr {
			_, err := time.Parse(time.DateOnly, s)
			ok = err == nil && len(s) == len(time.DateOnly)
		}
	}
	if !ok {
		return fmt.Errorf("%w: %T is not a %s value", ErrBackupInvalid, v, k)
	}
	return nil
}

// BackupColumn is one exported column.
type BackupColumn struct {
	Name string     `json:"name"`
	Kind ColumnKind `json:"kind"`
}

// BackupTable is one exported table and its columns, in export order.
type BackupTable struct {
	Name    string         `json:"name"`
	Columns []BackupColumn `json:"columns"`
}

// ColumnIndex returns the position of the named column, or -1.
func (t BackupTable) ColumnIndex(name string) int {
	return slices.IndexFunc(t.Columns, func(c BackupColumn) bool { return c.Name == name })
}

// CheckRow reports whether row has one valid value per column.
func (t BackupTable) CheckRow(row []any) error {
	if len(row) != len(t.Columns) {
		return fmt.Errorf("%w: %s row has %d values, want %d", ErrBackupInvalid, t.Name, len(row), len(t.Columns))
	}
	for i, c := range t.Columns {
		if err := c.Kind.CheckValue(row[i]); err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name, c.Name, err)
		}
	}
	return nil
}

// BackupHeader is the first entry of a backup: what it is and what it contains.
type BackupHeader struct {
	FormatVersion int           `json:"formatVersion"`
	SchemaVersion int64         `json:"schemaVersion"`
	AppVersion    string        `json:"appVersion"`
	Build         string        `json:"build"`
	CreatedAt     time.Time     `json:"createdAt"`
	CreatedBy     string        `json:"createdBy"`
	Tables        []BackupTable `json:"tables"`
}

var identRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Validate checks a header read from a backup. Table and column names must be plain
// identifiers (they are later quoted into SQL), unique, and never an excluded table.
func (h BackupHeader) Validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: header: %s", ErrBackupInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case h.FormatVersion != BackupFormatVersion:
		return fmt.Errorf("%w: format version %d", ErrBackupUnsupported, h.FormatVersion)
	case h.SchemaVersion <= 0:
		return bad("schema version %d", h.SchemaVersion)
	case h.CreatedAt.IsZero():
		return bad("no creation time")
	case !utf8.ValidString(h.AppVersion + h.Build + h.CreatedBy):
		return bad("text is not UTF-8")
	case len(h.AppVersion) > 64 || len(h.Build) > 64 || len(h.CreatedBy) > maxBackupText:
		return bad("text too long")
	case len(h.Tables) == 0 || len(h.Tables) > maxBackupTables:
		return bad("%d tables", len(h.Tables))
	}
	seen := map[string]bool{}
	for _, t := range h.Tables {
		if !identRe.MatchString(t.Name) || seen[t.Name] {
			return bad("table name %q", t.Name)
		}
		if IsBackupExcluded(t.Name) {
			return bad("table %q is never backed up", t.Name)
		}
		seen[t.Name] = true
		if len(t.Columns) == 0 || len(t.Columns) > maxBackupColumns {
			return bad("table %q has %d columns", t.Name, len(t.Columns))
		}
		cols := map[string]bool{}
		for _, c := range t.Columns {
			if !identRe.MatchString(c.Name) || cols[c.Name] || !c.Kind.Valid() {
				return bad("column %q.%q (%s)", t.Name, c.Name, c.Kind)
			}
			cols[c.Name] = true
		}
	}
	return nil
}

// Table returns the named table of the header.
func (h BackupHeader) Table(name string) (BackupTable, bool) {
	i := slices.IndexFunc(h.Tables, func(t BackupTable) bool { return t.Name == name })
	if i < 0 {
		return BackupTable{}, false
	}
	return h.Tables[i], true
}

// strictJSON decodes exactly one JSON value into v, refusing unknown fields.
func strictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

// ParseBackupHeader decodes and validates header.json. An unknown format version is
// ErrBackupUnsupported; anything else wrong is ErrBackupInvalid.
func ParseBackupHeader(data []byte) (BackupHeader, error) {
	var probe struct {
		FormatVersion int `json:"formatVersion"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return BackupHeader{}, fmt.Errorf("%w: header: %w", ErrBackupInvalid, err)
	}
	if probe.FormatVersion != BackupFormatVersion {
		return BackupHeader{}, fmt.Errorf("%w: format version %d", ErrBackupUnsupported, probe.FormatVersion)
	}
	var h BackupHeader
	if err := strictJSON(data, &h); err != nil {
		return BackupHeader{}, fmt.Errorf("%w: header: %w", ErrBackupInvalid, err)
	}
	h.CreatedAt = h.CreatedAt.UTC()
	return h, h.Validate()
}

// CheckRestorable reports whether a backup with header h can be restored into a database
// at schema currentSchema: never from a newer schema, never an unknown format.
func CheckRestorable(h BackupHeader, currentSchema int64) error {
	if err := h.Validate(); err != nil {
		return err
	}
	switch {
	case h.SchemaVersion > currentSchema:
		return fmt.Errorf("%w: schema %d, this version has %d", ErrBackupTooNew, h.SchemaVersion, currentSchema)
	case h.SchemaVersion < MinBackupSchema:
		return fmt.Errorf("%w: schema %d is older than %d", ErrBackupUnsupported, h.SchemaVersion, MinBackupSchema)
	}
	return nil
}

// AuditRef identifies an audit entry by sequence number and hash.
type AuditRef struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

// TableDigest is one table's row count and the SHA-256 of its exported lines.
type TableDigest struct {
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}

// BackupDigest is the last entry of a backup: what the decoder must find.
type BackupDigest struct {
	Tables    []TableDigest `json:"tables"`
	AuditHead AuditRef      `json:"auditHead"`
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseBackupDigest decodes digest.json.
func ParseBackupDigest(data []byte) (BackupDigest, error) {
	var d BackupDigest
	if err := strictJSON(data, &d); err != nil {
		return BackupDigest{}, fmt.Errorf("%w: digest: %w", ErrBackupInvalid, err)
	}
	if len(d.Tables) > maxBackupTables {
		return BackupDigest{}, fmt.Errorf("%w: digest: %d tables", ErrBackupInvalid, len(d.Tables))
	}
	for _, t := range d.Tables {
		if !identRe.MatchString(t.Name) || t.Rows < 0 || !sha256Re.MatchString(t.SHA256) {
			return BackupDigest{}, fmt.Errorf("%w: digest: table %q", ErrBackupInvalid, t.Name)
		}
	}
	if d.AuditHead.Seq < 0 || (d.AuditHead.Hash != "" && !sha256Re.MatchString(d.AuditHead.Hash)) {
		return BackupDigest{}, fmt.Errorf("%w: digest: audit head", ErrBackupInvalid)
	}
	return d, nil
}

// VerifyBackupDigest compares the digest a backup declares with the one computed while
// reading it.
func VerifyBackupDigest(declared, computed BackupDigest) error {
	if declared.AuditHead != computed.AuditHead || !slices.Equal(declared.Tables, computed.Tables) {
		return fmt.Errorf("%w: contents do not match the digest", ErrBackupInvalid)
	}
	return nil
}

// AuditHeadOf returns the audit reference a row of table t carries, when t is the audit
// table.
func AuditHeadOf(t BackupTable, row []any) (AuditRef, bool) {
	if t.Name != AuditTableName {
		return AuditRef{}, false
	}
	si, hi := t.ColumnIndex("seq"), t.ColumnIndex("hash")
	if si < 0 || hi < 0 || si >= len(row) || hi >= len(row) {
		return AuditRef{}, false
	}
	seq, ok1 := row[si].(int64)
	hash, ok2 := row[hi].(string)
	return AuditRef{Seq: seq, Hash: hash}, ok1 && ok2
}

// BackupKind says how a stored backup came to be.
type BackupKind string

// Backup kinds.
const (
	BackupScheduled BackupKind = "scheduled"
	BackupManual    BackupKind = "manual"
	BackupUpload    BackupKind = "upload"
)

// BackupName is the parsed name of a stored backup file. Names are always generated by
// String and checked by ParseBackupName before any file is touched (ADR-010).
type BackupName struct {
	CreatedAt time.Time
	Schema    int64 // 0: not known yet (an upload)
	Kind      BackupKind
}

const backupNameTime = "20060102T150405Z"

var backupNameRe = regexp.MustCompile(`^cactusinv-([0-9]{8}T[0-9]{6}Z)-s(0|[1-9][0-9]{0,5})-(scheduled|manual|upload)\.age$`)

// String is the file name, e.g. cactusinv-20261015T020000Z-s13-scheduled.age.
func (n BackupName) String() string {
	return fmt.Sprintf("cactusinv-%s-s%d-%s.age", n.CreatedAt.UTC().Format(backupNameTime), n.Schema, n.Kind)
}

// ParseBackupName parses a stored backup name; anything else is ErrValidation.
func ParseBackupName(s string) (BackupName, error) {
	m := backupNameRe.FindStringSubmatch(s)
	if m == nil {
		return BackupName{}, invalid("backup", "is not a backup name")
	}
	t, err := time.Parse(backupNameTime, m[1])
	if err != nil {
		return BackupName{}, invalid("backup", "is not a backup name")
	}
	schema, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return BackupName{}, invalid("backup", "is not a backup name")
	}
	return BackupName{CreatedAt: t.UTC(), Schema: schema, Kind: BackupKind(m[3])}, nil
}

var legacyBackupRe = regexp.MustCompile(`^cactusinv-[0-9]{8}-[0-9]{6}\.sql\.gz$`)

// IsLegacyBackupName reports whether s names a plaintext mariadb-dump backup written by
// the retired backup sidecar (ADR-007).
func IsLegacyBackupName(s string) bool { return legacyBackupRe.MatchString(s) }

// ValidateBackupPassphrase checks a passphrase for an exported backup.
func ValidateBackupPassphrase(p string) error {
	n := utf8.RuneCountInString(p)
	switch {
	case !utf8.ValidString(p):
		return invalid("passphrase", "must be text")
	case n < MinBackupPassphraseLength:
		return invalid("passphrase", fmt.Sprintf("must be at least %d characters", MinBackupPassphraseLength))
	case len(p) > MaxBackupPassphraseLength:
		return invalid("passphrase", fmt.Sprintf("must be at most %d bytes", MaxBackupPassphraseLength))
	}
	return nil
}

// CheckRestoreConfirmation requires the confirmation word, typed exactly.
func CheckRestoreConfirmation(s string) error {
	if strings.TrimSpace(s) != RestoreConfirmationWord {
		return invalid("confirmation", "type "+RestoreConfirmationWord+" to confirm")
	}
	return nil
}

// SystemState is the installation's single state row: whether automation is held after a
// restore, and the restore currently staged (if any).
type SystemState struct {
	AutomationHeld bool
	HeldAt         time.Time
	HeldBy         string
	HeldReason     string
	// HeldRestoreID is the restore that held automation, written inside its swap: proof
	// that the swap committed.
	HeldRestoreID string
	// A staged restore (RestoreID empty when none).
	RestoreID        string
	RestoreExpiresAt time.Time
	RestoreSource    string // stored backup name, or "upload"
	RestoreSHA256    string
	RestoreStagedBy  string // user id
	RestoreHeader    BackupHeader
}

// HasStagedRestore reports whether a restore is staged and not expired at now.
func (s SystemState) HasStagedRestore(now time.Time) bool {
	return s.RestoreID != "" && now.Before(s.RestoreExpiresAt)
}

// ClearRestore forgets the staged restore.
func (s *SystemState) ClearRestore() {
	s.RestoreID, s.RestoreSource, s.RestoreSHA256, s.RestoreStagedBy = "", "", "", ""
	s.RestoreExpiresAt = time.Time{}
	s.RestoreHeader = BackupHeader{}
}

// Hold pauses automation after a restore.
func (s *SystemState) Hold(at time.Time, by, reason string) {
	s.AutomationHeld, s.HeldAt, s.HeldBy, s.HeldReason = true, at, truncate(by, maxBackupText), truncate(reason, 500)
}

// Resume lets the dispatcher and schedule runner claim work again.
func (s *SystemState) Resume() {
	s.AutomationHeld, s.HeldAt, s.HeldBy, s.HeldReason, s.HeldRestoreID = false, time.Time{}, "", "", ""
}

// BackupRunResult is the outcome of one scheduled backup slot.
type BackupRunResult string

// Backup run results.
const (
	BackupRunRunning BackupRunResult = "running"
	BackupRunOK      BackupRunResult = "ok"
	BackupRunFailed  BackupRunResult = "failed"
)

// BackupRun records one scheduled backup slot; the slot is unique, so a slot runs once
// across replicas.
type BackupRun struct {
	Slot       time.Time
	Name       string
	Result     BackupRunResult
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}
