package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// queryer is a *sql.DB, *sql.Conn or *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// tableInfo is one backed-up table: its exported (non-generated) columns and primary key.
type tableInfo struct {
	domain.BackupTable
	PK []string
}

// columnKind maps a MariaDB column type to its backup kind (ADR-010).
func columnKind(dataType, columnType string) (domain.ColumnKind, error) {
	switch strings.ToLower(dataType) {
	case "tinyint", "smallint", "mediumint", "int", "integer", "year":
		return domain.ColumnInt, nil
	case "bigint":
		if strings.Contains(strings.ToLower(columnType), "unsigned") {
			return "", fmt.Errorf("unsigned bigint")
		}
		return domain.ColumnInt, nil
	case "decimal":
		return domain.ColumnDecimal, nil
	case "char", "varchar", "tinytext", "text", "mediumtext", "longtext", "enum", "set", "json":
		return domain.ColumnText, nil
	case "binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob":
		return domain.ColumnBytes, nil
	case "datetime", "timestamp":
		return domain.ColumnDateTime, nil
	case "date":
		return domain.ColumnDate, nil
	}
	return "", fmt.Errorf("type %s", columnType)
}

// quoteIdent quotes a validated identifier for SQL.
func quoteIdent(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = quoteIdent(n)
	}
	return strings.Join(q, ", ")
}

// Statement builders. Table and column names come from information_schema or from a
// validated backup header (domain.BackupHeader.Validate allows [a-z][a-z0-9_]*) and are
// always quoted; values are always placeholders.

// selectSQL reads cols of a (quoted, possibly qualified) table in key order.
func selectSQL(table string, cols, order []string) string {
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(quoteList(cols))
	b.WriteString(" FROM ")
	b.WriteString(table)
	b.WriteString(" ORDER BY ")
	b.WriteString(quoteList(order))
	return b.String()
}

// loadTables describes every base table of schema except the excluded ones, parents
// before children (by foreign keys).
func loadTables(ctx context.Context, q queryer, schema string) ([]tableInfo, error) {
	names, err := baseTables(ctx, q, schema)
	if err != nil {
		return nil, err
	}
	byName := map[string]*tableInfo{}
	for _, n := range names {
		if !domain.IsBackupExcluded(n) {
			byName[n] = &tableInfo{BackupTable: domain.BackupTable{Name: n}}
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_GENERATED
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var table, col, dataType, colType, generated string
		if err := rows.Scan(&table, &col, &dataType, &colType, &generated); err != nil {
			return nil, err
		}
		t, ok := byName[table]
		if !ok || generated == "ALWAYS" {
			continue
		}
		kind, err := columnKind(dataType, colType)
		if err != nil {
			return nil, fmt.Errorf("column %s.%s cannot be backed up: %w", table, col, err)
		}
		t.Columns = append(t.Columns, domain.BackupColumn{Name: col, Kind: kind})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := loadPrimaryKeys(ctx, q, schema, byName); err != nil {
		return nil, err
	}
	order, err := parentsFirst(ctx, q, schema, byName)
	if err != nil {
		return nil, err
	}
	out := make([]tableInfo, len(order))
	for i, n := range order {
		out[i] = *byName[n]
	}
	return out, nil
}

func baseTables(ctx context.Context, q queryer, schema string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT TABLE_NAME FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func loadPrimaryKeys(ctx context.Context, q queryer, schema string, byName map[string]*tableInfo) error {
	rows, err := q.QueryContext(ctx, `SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND INDEX_NAME = 'PRIMARY' ORDER BY TABLE_NAME, SEQ_IN_INDEX`, schema)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			return err
		}
		if t, ok := byName[table]; ok {
			t.PK = append(t.PK, col)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range byName {
		if len(t.PK) == 0 { // no primary key: a stable order over every column
			for _, c := range t.Columns {
				t.PK = append(t.PK, c.Name)
			}
		}
	}
	return nil
}

// parentsFirst orders tables so every table comes after the tables it references.
func parentsFirst(ctx context.Context, q queryer, schema string, byName map[string]*tableInfo) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT TABLE_NAME, REFERENCED_TABLE_NAME FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND REFERENCED_TABLE_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL`, schema, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parents := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		if child != parent && byName[child] != nil && byName[parent] != nil {
			parents[child] = append(parents[child], parent)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pending := make([]string, 0, len(byName))
	for n := range byName {
		pending = append(pending, n)
	}
	sort.Strings(pending)
	var out []string
	done := map[string]bool{}
	for len(pending) > 0 {
		progressed := false
		for i := 0; i < len(pending); i++ {
			n := pending[i]
			if slices.ContainsFunc(parents[n], func(p string) bool { return !done[p] }) {
				continue
			}
			out, done[n] = append(out, n), true
			pending = slices.Delete(pending, i, i+1)
			progressed = true
			break
		}
		if !progressed {
			return nil, fmt.Errorf("foreign keys form a cycle among %v", pending)
		}
	}
	return out, nil
}

// toBackupValue converts a scanned driver value to the kind's backup value.
func toBackupValue(k domain.ColumnKind, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch k {
	case domain.ColumnInt:
		switch x := v.(type) {
		case int64:
			return x, nil
		case uint64:
			if x > math.MaxInt64 {
				return nil, fmt.Errorf("%d does not fit in int64", x)
			}
			return int64(x), nil
		case []byte:
			return strconv.ParseInt(string(x), 10, 64)
		}
	case domain.ColumnDecimal, domain.ColumnText:
		switch x := v.(type) {
		case []byte:
			return string(x), nil
		case string:
			return x, nil
		}
	case domain.ColumnBytes:
		if x, ok := v.([]byte); ok {
			return x, nil
		}
	case domain.ColumnDateTime:
		if x, ok := v.(time.Time); ok {
			return x.UTC(), nil
		}
	case domain.ColumnDate:
		if x, ok := v.(time.Time); ok {
			return x.Format(time.DateOnly), nil
		}
	}
	return nil, fmt.Errorf("unexpected %T for a %s column", v, k)
}

// fromBackupValue converts a backup value to a driver argument.
func fromBackupValue(v any) any {
	if t, ok := v.(time.Time); ok {
		return t.UTC()
	}
	return v
}

// Snapshotter implements ports.Snapshotter with one consistent InnoDB snapshot.
type Snapshotter struct {
	db *sql.DB
}

// NewSnapshotter returns a Snapshotter on db.
func NewSnapshotter(db *sql.DB) *Snapshotter { return &Snapshotter{db: db} }

// Snapshot implements ports.Snapshotter: START TRANSACTION WITH CONSISTENT SNAPSHOT,
// READ ONLY at REPEATABLE READ on one connection; every table is read in that view.
func (s *Snapshotter) Snapshot(ctx context.Context, fn func(context.Context, ports.BackupSource) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("mariadb.Snapshot: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return fmt.Errorf("mariadb.Snapshot: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY"); err != nil {
		return fmt.Errorf("mariadb.Snapshot: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") }()
	var schema string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		return fmt.Errorf("mariadb.Snapshot: %w", err)
	}
	version, err := schemaVersion(ctx, conn)
	if err != nil {
		return fmt.Errorf("mariadb.Snapshot: %w", err)
	}
	tables, err := loadTables(ctx, conn, schema)
	if err != nil {
		return fmt.Errorf("mariadb.Snapshot: %w", err)
	}
	return fn(ctx, &snapshotSource{conn: conn, version: version, tables: tables})
}

// schemaVersion is the highest applied goose migration.
func schemaVersion(ctx context.Context, q queryer) (int64, error) {
	var v int64
	err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&v)
	return v, err
}

type snapshotSource struct {
	conn    *sql.Conn
	version int64
	tables  []tableInfo
}

func (s *snapshotSource) SchemaVersion() int64 { return s.version }

func (s *snapshotSource) Tables() []domain.BackupTable {
	out := make([]domain.BackupTable, len(s.tables))
	for i, t := range s.tables {
		out[i] = t.BackupTable
	}
	return out
}

func (s *snapshotSource) ScanTable(ctx context.Context, table string, fn func([]any) error) error {
	i := slices.IndexFunc(s.tables, func(t tableInfo) bool { return t.Name == table })
	if i < 0 {
		return fmt.Errorf("mariadb.ScanTable: unknown table %q", table)
	}
	t := s.tables[i]
	cols := make([]string, len(t.Columns))
	for j, c := range t.Columns {
		cols[j] = c.Name
	}
	rows, err := s.conn.QueryContext(ctx, selectSQL(quoteIdent(t.Name), cols, t.PK))
	if err != nil {
		return fmt.Errorf("mariadb.ScanTable %s: %w", table, err)
	}
	defer rows.Close()
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for j := range raw {
		ptrs[j] = &raw[j]
	}
	row := make([]any, len(cols))
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("mariadb.ScanTable %s: %w", table, err)
		}
		for j, c := range t.Columns {
			v, err := toBackupValue(c.Kind, raw[j])
			if err != nil {
				return fmt.Errorf("mariadb.ScanTable %s.%s: %w", table, c.Name, err)
			}
			row[j] = v
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mariadb.ScanTable %s: %w", table, err)
	}
	return nil
}
