package mariadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Stager implements ports.RestoreStager (ADR-010). A backup is loaded into a separate
// staging database on the same server, migrated to head there, and swapped into the live
// database in one transaction with every table locked.
type Stager struct {
	live, staging         *sql.DB
	liveName, stagingName string
}

// StagingSuffix ends every staging database name (e.g. cactusinv_restore): the stager
// drops every table in it, so it refuses any database not named for that purpose.
const StagingSuffix = "_restore"

// NewStager returns a Stager. staging must be a database of its own whose name ends in
// StagingSuffix. If the staging pool is reachable, it must really be connected to that
// database (a misconfigured DSN must never point the stager at live data).
func NewStager(ctx context.Context, live, staging *sql.DB, liveName, stagingName string) (*Stager, error) {
	if !strings.HasSuffix(stagingName, StagingSuffix) || len(stagingName) == len(StagingSuffix) || strings.EqualFold(liveName, stagingName) {
		return nil, fmt.Errorf("mariadb.NewStager: the staging database must be named <db>%s and differ from the live one (%q)", StagingSuffix, liveName)
	}
	s := &Stager{live: live, staging: staging, liveName: liveName, stagingName: stagingName}
	if err := s.checkStagingPool(ctx); err != nil && !errors.Is(err, errStagingUnreachable) {
		return nil, err
	}
	return s, nil
}

var errStagingUnreachable = errors.New("staging database unreachable")

// checkStagingPool verifies the staging pool's default database is the staging database.
func (s *Stager) checkStagingPool(ctx context.Context) error {
	var current sql.NullString
	if err := s.staging.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&current); err != nil {
		return fmt.Errorf("mariadb.Stager: %w (MariaDB error %d)", errStagingUnreachable, errNumber(err))
	}
	if current.String != s.stagingName {
		return fmt.Errorf("mariadb.Stager: the staging pool is connected to %q, not %q", current.String, s.stagingName)
	}
	return nil
}

// Lock takes the installation-wide backup lock (GET_LOCK on a dedicated connection), so
// only one stage, apply or discard runs at a time across every replica. domain.ErrBusy
// when another process holds it.
func (s *Stager) Lock(ctx context.Context) (func(), error) {
	conn, err := s.live.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("mariadb.Stager.Lock: %w", err)
	}
	release, err := s.lockOn(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return func() {
		release()
		_ = conn.Close()
	}, nil
}

// lockOn takes the backup lock in conn's session; the returned func releases it (or, if
// that fails, discards the connection: ending the session frees the lock).
func (s *Stager) lockOn(ctx context.Context, conn *sql.Conn) (func(), error) {
	name := lockName(s.liveName)
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&got); err != nil || !got.Valid {
		return nil, fmt.Errorf("mariadb.Stager: GET_LOCK failed (MariaDB error %d)", errNumber(err))
	}
	if got.Int64 != 1 {
		return nil, fmt.Errorf("mariadb.Stager: %w: another backup operation is running", domain.ErrBusy)
	}
	return func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(rctx, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil || released.Int64 != 1 {
			discardConn(conn) // ending the session frees the lock
		}
	}, nil
}

// lockName is the GET_LOCK name for one live database (names are server-wide, ≤ 64).
func lockName(liveName string) string {
	return domain.TruncateUTF8("cactusinv_backup:"+liveName, 64)
}

// discardConn makes sure a connection whose session settings were changed is closed
// rather than returned to the pool.
func discardConn(conn *sql.Conn) { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }

// qualified names a table of the staging database.
func (s *Stager) qualified(table string) string {
	return quoteIdent(s.stagingName) + "." + quoteIdent(table)
}

// Ready implements ports.RestoreStager: the staging database is reachable and the app user
// may use it.
func (s *Stager) Ready(ctx context.Context) error {
	if err := s.staging.PingContext(ctx); err != nil {
		return fmt.Errorf("mariadb.Stager: staging database %s: %w", s.stagingName, err)
	}
	return nil
}

// SchemaVersion implements ports.RestoreStager (the live schema).
func (s *Stager) SchemaVersion(ctx context.Context) (int64, error) {
	v, err := schemaVersion(ctx, s.live)
	if err != nil {
		return 0, fmt.Errorf("mariadb.Stager.SchemaVersion: %w", err)
	}
	return v, nil
}

// Discard implements ports.RestoreStager: it drops every table of the staging database.
func (s *Stager) Discard(ctx context.Context) error {
	if err := s.checkStagingPool(ctx); err != nil {
		return err
	}
	conn, err := s.staging.Conn(ctx)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Discard: %w", err)
	}
	defer conn.Close()
	// FOREIGN_KEY_CHECKS is a session setting: this connection never goes back to the pool.
	defer discardConn(conn)
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return fmt.Errorf("mariadb.Stager.Discard: %w", err)
	}
	tables, err := baseTables(ctx, conn, s.stagingName)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Discard: %w", err)
	}
	for _, t := range tables {
		if _, err := conn.ExecContext(ctx, dropSQL(s.qualified(t))); err != nil {
			return fmt.Errorf("mariadb.Stager.Discard: %w", err)
		}
	}
	return nil
}

func dropSQL(qualified string) string { return "DROP TABLE " + qualified }

func deleteSQL(qualified string) string { return "DELETE FROM " + qualified }

func countSQL(qualified string) string { return "SELECT COUNT(*) FROM " + qualified }

// insertSQL inserts rows of values (placeholders) into cols of table.
func insertSQL(qualified string, cols []string, rows int) string {
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(qualified)
	b.WriteString(" (")
	b.WriteString(quoteList(cols))
	b.WriteString(") VALUES ")
	one := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
	for i := range rows {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(one)
	}
	return b.String()
}

// copySQL copies cols of every row from one table to another.
func copySQL(to, from string, cols []string) string {
	list := quoteList(cols)
	return "INSERT INTO " + to + " (" + list + ") SELECT " + list + " FROM " + from
}

// lockSQL locks every live table for writing and every staging table for reading.
func lockSQL(write, read []string) string {
	parts := make([]string, 0, len(write)+len(read))
	for _, t := range write {
		parts = append(parts, t+" WRITE")
	}
	for _, t := range read {
		parts = append(parts, t+" READ")
	}
	return "LOCK TABLES " + strings.Join(parts, ", ")
}

// Stage implements ports.RestoreStager.
func (s *Stager) Stage(ctx context.Context, load func(context.Context, ports.BackupSink) error) (ports.StagedRestore, error) {
	if err := s.Discard(ctx); err != nil {
		return ports.StagedRestore{}, err
	}
	conn, err := s.staging.Conn(ctx)
	if err != nil {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Stage: %w", err)
	}
	defer conn.Close()
	// The loading session turns FOREIGN_KEY_CHECKS off: it never goes back to the pool.
	defer discardConn(conn)
	sink := &stagingSink{s: s, conn: conn}
	if err := load(ctx, sink); err != nil {
		return ports.StagedRestore{}, err
	}
	if !sink.started {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Stage: %w: no header", domain.ErrBackupInvalid)
	}
	if err := sink.flush(ctx); err != nil {
		return ports.StagedRestore{}, err
	}
	// The real migrations bring the backup up to this version (Go migrations included).
	if err := Migrate(ctx, s.staging, false); err != nil {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Stage: migrate the staged backup: %w", err)
	}
	return s.Staged(ctx)
}

// stagingSink loads a decoded backup into the staging database. Rows are inserted in
// batches with foreign key checks off (the rows come from one consistent snapshot).
type stagingSink struct {
	s       *Stager
	conn    *sql.Conn
	started bool
	header  domain.BackupHeader
	table   int // index into header.Tables of the rows being buffered
	batch   []any
	rows    int
	bytes   int
}

const (
	batchRows  = 500
	batchBytes = 1 << 20
	maxParams  = 60000
)

// Header migrates the empty staging database to the backup's schema and checks the
// backup describes exactly that schema's tables and columns.
func (k *stagingSink) Header(ctx context.Context, h domain.BackupHeader) error {
	if k.started {
		return fmt.Errorf("mariadb.Stager: %w: a second header", domain.ErrBackupInvalid)
	}
	k.started, k.header, k.table = true, h, -1
	if err := MigrateTo(ctx, k.s.staging, h.SchemaVersion); err != nil {
		return fmt.Errorf("mariadb.Stager: migrate staging to schema %d: %w", h.SchemaVersion, err)
	}
	v, err := schemaVersion(ctx, k.conn)
	if err != nil {
		return fmt.Errorf("mariadb.Stager: %w", err)
	}
	if v != h.SchemaVersion {
		return fmt.Errorf("mariadb.Stager: %w: no migration to schema %d (at %d)", domain.ErrBackupUnsupported, h.SchemaVersion, v)
	}
	tables, err := loadTables(ctx, k.conn, k.s.stagingName)
	if err != nil {
		return fmt.Errorf("mariadb.Stager: %w", err)
	}
	if len(tables) != len(h.Tables) {
		return fmt.Errorf("mariadb.Stager: %w: the backup has %d tables, schema %d has %d", domain.ErrBackupInvalid, len(h.Tables), h.SchemaVersion, len(tables))
	}
	for _, ht := range h.Tables {
		i := slices.IndexFunc(tables, func(t tableInfo) bool { return t.Name == ht.Name })
		if i < 0 || !slices.Equal(sortedColumns(tables[i].Columns), sortedColumns(ht.Columns)) {
			return fmt.Errorf("mariadb.Stager: %w: table %s does not match schema %d", domain.ErrBackupInvalid, ht.Name, h.SchemaVersion)
		}
	}
	if _, err := k.conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return fmt.Errorf("mariadb.Stager: %w", err)
	}
	// Rows the migrations seeded (e.g. the invoice sequence) make way for the backup's.
	for _, t := range h.Tables {
		if _, err := k.conn.ExecContext(ctx, deleteSQL(k.s.qualified(t.Name))); err != nil {
			return safeErr("clear", t.Name, err)
		}
	}
	return nil
}

func sortedColumns(cols []domain.BackupColumn) []domain.BackupColumn {
	out := slices.Clone(cols)
	slices.SortFunc(out, func(a, b domain.BackupColumn) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Row buffers one row; rows must arrive table by table in header order.
func (k *stagingSink) Row(ctx context.Context, table string, row []any) error {
	if !k.started {
		return fmt.Errorf("mariadb.Stager: %w: rows before the header", domain.ErrBackupInvalid)
	}
	if k.table < 0 || k.header.Tables[k.table].Name != table {
		if err := k.flush(ctx); err != nil {
			return err
		}
		next := slices.IndexFunc(k.header.Tables, func(t domain.BackupTable) bool { return t.Name == table })
		if next <= k.table {
			return fmt.Errorf("mariadb.Stager: %w: rows of %s out of order", domain.ErrBackupInvalid, table)
		}
		k.table = next
	}
	t := k.header.Tables[k.table]
	if err := t.CheckRow(row); err != nil {
		return err
	}
	for _, v := range row {
		k.batch = append(k.batch, fromBackupValue(v))
		switch x := v.(type) {
		case string:
			k.bytes += len(x)
		case []byte:
			k.bytes += len(x)
		default:
			k.bytes += 8
		}
	}
	k.rows++
	if k.rows >= batchRows || k.bytes >= batchBytes || len(k.batch)+len(row) > maxParams {
		return k.flush(ctx)
	}
	return nil
}

func (k *stagingSink) flush(ctx context.Context) error {
	if k.rows == 0 {
		return nil
	}
	t := k.header.Tables[k.table]
	cols := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = c.Name
	}
	if _, err := k.conn.ExecContext(ctx, insertSQL(k.s.qualified(t.Name), cols, k.rows), k.batch...); err != nil {
		return safeErr("load", t.Name, err)
	}
	k.batch, k.rows, k.bytes = k.batch[:0], 0, 0
	return nil
}

// Staged implements ports.RestoreStager.
func (s *Stager) Staged(ctx context.Context) (ports.StagedRestore, error) {
	version, err := schemaVersion(ctx, s.staging)
	if err != nil {
		var exists int
		if qerr := s.live.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'goose_db_version'", s.stagingName).Scan(&exists); qerr == nil && exists == 0 {
			return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", domain.ErrNotFound)
		}
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
	}
	out := ports.StagedRestore{SchemaVersion: version, Counts: map[string]int64{}, LiveCounts: map[string]int64{}}
	tables, err := loadTables(ctx, s.live, s.liveName)
	if err != nil {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
	}
	for _, t := range tables {
		var staged, live int64
		if err := s.live.QueryRowContext(ctx, countSQL(s.qualified(t.Name))).Scan(&staged); err != nil {
			return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %s: %w", t.Name, err)
		}
		if err := s.live.QueryRowContext(ctx, countSQL(quoteIdent(t.Name))).Scan(&live); err != nil {
			return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %s: %w", t.Name, err)
		}
		out.Counts[t.Name], out.LiveCounts[t.Name] = staged, live
	}
	q := sqlcgen.New(s.staging)
	if _, enc, err := (smtpRepo{q}).Get(ctx); err == nil {
		out.SMTPPasswordEnc = enc
	} else if !errors.Is(err, domain.ErrNotFound) {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
	}
	rows, err := s.staging.QueryContext(ctx, "SELECT id FROM users WHERE disabled_at IS NULL ORDER BY id")
	if err != nil {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
		}
		out.UserIDs = append(out.UserIDs, id)
	}
	if err := rows.Err(); err != nil {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
	}
	// The staged database is migrated to this version, so access groups exist.
	admins, err := s.staging.QueryContext(ctx, `SELECT m.user_id FROM access_group_members m JOIN users u ON u.id = m.user_id
		WHERE m.group_id = 'administrators' AND u.disabled_at IS NULL ORDER BY m.user_id`)
	if err != nil {
		return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
	}
	defer admins.Close()
	for admins.Next() {
		var id string
		if err := admins.Scan(&id); err != nil {
			return ports.StagedRestore{}, fmt.Errorf("mariadb.Stager.Staged: %w", err)
		}
		out.AdminIDs = append(out.AdminIDs, id)
	}
	return out, admins.Err()
}

// StagedAudit implements ports.RestoreStager.
func (s *Stager) StagedAudit(ctx context.Context) ([]domain.AuditEntry, error) {
	out, err := readAudit(ctx, auditRepo{sqlcgen.New(s.staging)})
	if err != nil {
		return nil, fmt.Errorf("mariadb.Stager.StagedAudit: %w", err)
	}
	return out, nil
}

func readAudit(ctx context.Context, r auditRepo) ([]domain.AuditEntry, error) {
	var all []domain.AuditEntry
	var after int64
	for {
		batch, err := r.Chain(ctx, after, 5000)
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

// Apply implements ports.RestoreStager: on one connection it takes the backup lock
// (GET_LOCK), then SET autocommit=0, LOCK TABLES (every live table WRITE, every staging
// table READ), fn, COMMIT and UNLOCK TABLES — or ROLLBACK and UNLOCK TABLES when anything
// fails — and releases the backup lock last. The lock lives in the swap's own session: if
// that connection drops, the swap aborts and the lock is freed with it, so no other replica
// can hold the lock while a swap runs. A connection that cannot be unlocked is closed,
// never returned to the pool.
func (s *Stager) Apply(ctx context.Context, fn func(context.Context, ports.RestoreTx) error) (err error) {
	conn, err := s.live.Conn(ctx)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: %w", err)
	}
	defer conn.Close()
	releaseLock, err := s.lockOn(ctx, conn)
	if err != nil {
		return err
	}
	defer releaseLock() // runs after the cleanup below (deferred calls run last-in first-out)
	liveVersion, err := schemaVersion(ctx, conn)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: %w", err)
	}
	if v, err := schemaVersion(ctx, s.staging); err != nil || v != liveVersion {
		return fmt.Errorf("mariadb.Stager.Apply: %w: the staging database is not at schema %d", domain.ErrNotFound, liveVersion)
	}
	included, err := loadTables(ctx, conn, s.liveName)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: %w", err)
	}
	allLive, err := baseTables(ctx, conn, s.liveName)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: %w", err)
	}
	allStaging, err := baseTables(ctx, conn, s.stagingName)
	if err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: %w", err)
	}
	for _, t := range included {
		if !slices.Contains(allStaging, t.Name) {
			return fmt.Errorf("mariadb.Stager.Apply: staging has no table %s", t.Name)
		}
	}
	write := make([]string, len(allLive))
	for i, t := range allLive {
		write[i] = quoteIdent(s.liveName) + "." + quoteIdent(t)
	}
	read := make([]string, len(allStaging))
	for i, t := range allStaging {
		read[i] = s.qualified(t)
	}

	// From here on, every way out — an error, a panic in fn, a failed COMMIT — runs
	// release: ROLLBACK, UNLOCK TABLES, autocommit back on, or the connection is discarded.
	// A connection holding table locks or a half-applied swap never reaches the pool.
	cleanup := context.WithoutCancel(ctx)
	committed := false
	defer func() {
		if !committed {
			s.release(cleanup, conn, false)
		}
	}()
	if _, err := conn.ExecContext(ctx, "SET autocommit = 0"); err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: %w", err)
	}
	if _, err := conn.ExecContext(ctx, lockSQL(write, read)); err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: lock tables: %w", err)
	}
	if err := fn(ctx, &restoreTx{s: s, conn: conn, included: included}); err != nil {
		return err
	}
	// COMMIT must not be abandoned by a cancelled request.
	commitCtx, cancel := context.WithTimeout(cleanup, commitTimeout)
	defer cancel()
	if _, err := conn.ExecContext(commitCtx, "COMMIT"); err != nil {
		return fmt.Errorf("mariadb.Stager.Apply: commit failed (MariaDB error %d)", errNumber(err))
	}
	committed = true
	s.release(cleanup, conn, true) // the swap is durable; a failed unlock only discards the connection
	return nil
}

// commitTimeout bounds the COMMIT of a restore, which runs even if the request is gone;
// cleanupTimeout bounds each detached cleanup (rollback, unlock, lock release). A cleanup
// that fails or times out discards its connection.
const (
	commitTimeout  = 2 * time.Minute
	cleanupTimeout = 30 * time.Second
)

// errNumber is the MariaDB error number of err (0 if it has none). Error messages are
// never passed on: they can quote row values (customer e-mails, bank details).
func errNumber(err error) uint16 {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number
	}
	return 0
}

// safeErr reports a failed statement on table by MariaDB error number only. Rows that
// break a constraint or do not fit a column make the backup invalid.
func safeErr(op, table string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("mariadb.Stager: %s %s: %w", op, table, err)
	}
	switch n := errNumber(err); n {
	case errDupEntry, errRowIsReferenced, errNoReferencedRow, errCheckViolation, 1048, 1264, 1265, 1292, 1366, 1406:
		return fmt.Errorf("mariadb.Stager: %w: rows of %s do not load (MariaDB error %d)", domain.ErrBackupInvalid, table, n)
	case 0:
		return fmt.Errorf("mariadb.Stager: %s %s failed", op, table)
	default:
		return fmt.Errorf("mariadb.Stager: %s %s failed (MariaDB error %d)", op, table, n)
	}
}

// release rolls back (unless committed), unlocks and restores autocommit. If that fails
// the connection is discarded.
func (s *Stager) release(ctx context.Context, conn *sql.Conn, committed bool) {
	// Detached from the request but bounded: a hung server must not hang the caller.
	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()
	steps := []string{"UNLOCK TABLES", "SET autocommit = 1"}
	if !committed {
		steps = append([]string{"ROLLBACK"}, steps...)
	}
	for _, q := range steps {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			return
		}
	}
}

// restoreTx is the live database during Apply.
type restoreTx struct {
	s        *Stager
	conn     *sql.Conn
	included []tableInfo
}

func (t *restoreTx) Repos() ports.Repos { return repos(t.conn) }

// ReplaceTables deletes every included live table's rows child → parent (never the audit
// trail) and copies the staged rows parent → child, with explicit column lists.
func (t *restoreTx) ReplaceTables(ctx context.Context) error {
	for i := len(t.included) - 1; i >= 0; i-- {
		tb := t.included[i]
		if tb.Name == domain.AuditTableName {
			continue
		}
		if _, err := t.conn.ExecContext(ctx, deleteSQL(quoteIdent(tb.Name))); err != nil {
			return safeErr("delete", tb.Name, err)
		}
	}
	for _, tb := range t.included {
		if tb.Name == domain.AuditTableName {
			continue
		}
		cols := make([]string, len(tb.Columns))
		for i, c := range tb.Columns {
			cols[i] = c.Name
		}
		if _, err := t.conn.ExecContext(ctx, copySQL(quoteIdent(tb.Name), t.s.qualified(tb.Name), cols)); err != nil {
			return safeErr("copy", tb.Name, err)
		}
	}
	return nil
}

// StagedAudit reads the staged audit trail on the locked connection.
func (t *restoreTx) StagedAudit(ctx context.Context) ([]domain.AuditEntry, error) {
	rows, err := t.conn.QueryContext(ctx, selectSQL(t.s.qualified(domain.AuditTableName), auditColumns, []string{"seq"}))
	if err != nil {
		return nil, fmt.Errorf("mariadb.StagedAudit: %w", err)
	}
	defer rows.Close()
	var out []domain.AuditEntry
	for rows.Next() {
		var r sqlcgen.AuditLog
		if err := rows.Scan(&r.Seq, &r.At, &r.ActorID, &r.ActorName, &r.Action, &r.EntityType, &r.EntityID, &r.Summary, &r.PrevHash, &r.Hash); err != nil {
			return nil, fmt.Errorf("mariadb.StagedAudit: %w", err)
		}
		out = append(out, toAudit(r))
	}
	return out, rows.Err()
}

var auditColumns = []string{"seq", "at", "actor_id", "actor_name", "action", "entity_type", "entity_id", "summary", "prev_hash", "hash"}
