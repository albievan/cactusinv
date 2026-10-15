//go:build integration

package mariadb_test

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/backupformat"
	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// backedUpTables is every table a backup contains. A new table must be added here or to
// domain's exclude list: TestSnapshotCoversEveryTable fails until someone decides.
// headSchema is the newest migration.
const headSchema = 14

var backedUpTables = []string{
	"audit_log", "bank_accounts", "business_profile", "currencies", "customers", "email_outbox", "invoice_lines",
	"invoice_sequences", "invoices", "product_prices", "products", "schedule_lines", "schedule_runs", "schedules",
	"smtp_settings", "users",
}

// snapshotRows reads a whole snapshot into memory (tests only).
func snapshotRows(t *testing.T, ctx context.Context) (int64, []domain.BackupTable, map[string][][]any) {
	t.Helper()
	var version int64
	var tables []domain.BackupTable
	rows := map[string][][]any{}
	err := mariadb.NewSnapshotter(testDB).Snapshot(ctx, func(ctx context.Context, src ports.BackupSource) error {
		version, tables = src.SchemaVersion(), src.Tables()
		for _, tb := range tables {
			if err := src.ScanTable(ctx, tb.Name, func(r []any) error {
				rows[tb.Name] = append(rows[tb.Name], slices.Clone(r))
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return version, tables, rows
}

func baseTableNames(t *testing.T, ctx context.Context) []string {
	t.Helper()
	rows, err := testDB.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestSnapshotCoversEveryTable(t *testing.T) {
	e := setup(t)
	version, tables, rows := snapshotRows(t, e.ctx)
	if version != headSchema {
		t.Fatalf("schema version %d", version)
	}
	var names []string
	for _, tb := range tables {
		names = append(names, tb.Name)
	}
	exported := slices.Sorted(slices.Values(names))
	if !slices.Equal(exported, backedUpTables) {
		t.Fatalf("exported %v\nwant     %v", exported, backedUpTables)
	}
	for _, n := range baseTableNames(t, e.ctx) {
		if !slices.Contains(backedUpTables, n) && !domain.IsBackupExcluded(n) {
			t.Errorf("table %s is neither backed up nor excluded: add it to backedUpTables or to domain's exclude list", n)
		}
	}
	pos := func(n string) int { return slices.Index(names, n) }
	for _, edge := range [][2]string{{"currencies", "bank_accounts"}, {"bank_accounts", "customers"}, {"customers", "invoices"}, {"invoices", "invoice_lines"}, {"schedules", "invoices"}, {"products", "product_prices"}} {
		if pos(edge[0]) > pos(edge[1]) {
			t.Errorf("%s must come before %s: %v", edge[0], edge[1], names)
		}
	}
	banks := tables[pos("bank_accounts")]
	if banks.ColumnIndex("default_marker") >= 0 || banks.ColumnIndex("is_default") < 0 {
		t.Fatal("generated columns are not exported")
	}
	if len(rows["customers"]) != 1 || len(rows["currencies"]) != 2 || len(rows["audit_log"]) == 0 {
		t.Fatalf("rows = %d customers, %d currencies", len(rows["customers"]), len(rows["currencies"]))
	}
}

// TestSnapshotValuesRoundTrip: every column kind comes out of MariaDB exactly as stored,
// and survives the backup format.
func TestSnapshotValuesRoundTrip(t *testing.T) {
	e := setup(t)
	if _, err := testDB.ExecContext(e.ctx, `CREATE TABLE backup_probe (
		id BIGINT NOT NULL PRIMARY KEY, at DATETIME(6) NULL, d DATE NULL, n BIGINT NULL, b MEDIUMBLOB NULL,
		t VARCHAR(100) NULL, amount DECIMAL(14,4) NULL, flag BOOLEAN NULL, code CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NULL,
		twice BIGINT AS (id * 2) STORED
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testDB.ExecContext(context.Background(), "DROP TABLE backup_probe") })
	at := time.Date(2026, 10, 15, 2, 3, 4, 123456000, time.UTC)
	blob := append([]byte{0, 255, '\n', '"'}, bytes.Repeat([]byte{7}, 70000)...)
	if _, err := testDB.ExecContext(e.ctx, "INSERT INTO backup_probe (id, at, d, n, b, t, amount, flag, code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?), (2, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)",
		int64(1), at, "2026-02-28", int64(-9223372036854775808), blob, "Grüße 🌵 \"x\"\n", "-1234567890.1234", true, "ZAR"); err != nil {
		t.Fatal(err)
	}
	want := [][]any{
		{int64(1), at, "2026-02-28", int64(-9223372036854775808), blob, "Grüße 🌵 \"x\"\n", "-1234567890.1234", int64(1), "ZAR"},
		{int64(2), nil, nil, nil, nil, nil, nil, nil, nil},
	}
	_, tables, rows := snapshotRows(t, e.ctx)
	i := slices.IndexFunc(tables, func(tb domain.BackupTable) bool { return tb.Name == "backup_probe" })
	if i < 0 {
		t.Fatal("a new table is exported automatically")
	}
	probe := tables[i]
	if probe.ColumnIndex("twice") >= 0 {
		t.Fatal("generated column exported")
	}
	if !reflect.DeepEqual(rows["backup_probe"], want) {
		t.Fatalf("rows:\n got %#v\nwant %#v", rows["backup_probe"], want)
	}
	// Through the backup format and back.
	h := domain.BackupHeader{FormatVersion: 1, SchemaVersion: 13, CreatedAt: at, CreatedBy: "test", Tables: []domain.BackupTable{probe}}
	var buf bytes.Buffer
	codec := backupformat.New(backupformat.Limits{})
	if _, err := codec.Encode(e.ctx, &buf, h, memSource{h: h, rows: rows}); err != nil {
		t.Fatal(err)
	}
	got := &collectSink{rows: map[string][][]any{}}
	if _, _, err := codec.Decode(e.ctx, &buf, got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.rows["backup_probe"], want) {
		t.Fatalf("after the format:\n got %#v\nwant %#v", got.rows["backup_probe"], want)
	}
}

// TestSnapshotIsConsistent: rows written after the snapshot started are not in it.
func TestSnapshotIsConsistent(t *testing.T) {
	e := setup(t)
	var before, during int
	err := mariadb.NewSnapshotter(testDB).Snapshot(e.ctx, func(ctx context.Context, src ports.BackupSource) error {
		count := func() int {
			n := 0
			_ = src.ScanTable(ctx, "currencies", func([]any) error { n++; return nil })
			return n
		}
		before = count()
		if _, err := e.svc.CreateCurrency(e.ctx, "EUR", "Euro", "€", 2, true); err != nil {
			return err
		}
		during = count()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if before != 2 || during != 2 {
		t.Fatalf("snapshot saw %d then %d currencies", before, during)
	}
}

type memSource struct {
	h    domain.BackupHeader
	rows map[string][][]any
}

func (m memSource) SchemaVersion() int64         { return m.h.SchemaVersion }
func (m memSource) Tables() []domain.BackupTable { return m.h.Tables }
func (m memSource) ScanTable(_ context.Context, table string, fn func([]any) error) error {
	for _, r := range m.rows[table] {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

type collectSink struct {
	header domain.BackupHeader
	rows   map[string][][]any
}

func (c *collectSink) Header(_ context.Context, h domain.BackupHeader) error {
	c.header = h
	return nil
}

func (c *collectSink) Row(_ context.Context, table string, row []any) error {
	c.rows[table] = append(c.rows[table], row)
	return nil
}
