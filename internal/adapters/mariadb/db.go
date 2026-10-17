// Package mariadb implements cactusInv's repositories on MariaDB 11 (ADR-002) using
// sqlc-generated queries in ./sqlcgen.
package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/migrations"
)

// Config locates the database.
type Config struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
}

// DSN builds a go-sql-driver DSN that stores and reads every time in UTC.
func (c Config) DSN() string {
	mc := mysql.NewConfig()
	mc.User = c.User
	mc.Passwd = c.Password
	mc.Net = "tcp"
	mc.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	mc.DBName = c.Name
	mc.ParseTime = true
	// Report matched (not changed) rows, so an idempotent UPDATE is not "not found".
	mc.ClientFoundRows = true
	mc.Loc = time.UTC
	mc.Params = map[string]string{"time_zone": "'+00:00'", "sql_mode": "'STRICT_ALL_TABLES,NO_ZERO_DATE,NO_ENGINE_SUBSTITUTION'"}
	mc.Collation = "utf8mb4_unicode_ci"
	return mc.FormatDSN()
}

// Open connects and pings MariaDB.
func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("mariadb.Open: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mariadb.Open: ping: %w", err)
	}
	return db, nil
}

// OpenLazy returns a pool without connecting: for a database that may not be reachable or
// granted yet (the restore staging database); use it after a successful Ping.
func OpenLazy(cfg Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("mariadb.OpenLazy: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// migrationLock holds a database-wide named lock while migrations run, so two replicas
// starting together never apply the same migration at once (MariaDB DDL is not
// transactional). It waits up to 10 minutes for another process's migrations.
func migrationLock(ctx context.Context, db *sql.DB) (release func(), err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('cactusinv_migrate', 600)").Scan(&got); err != nil || got.Int64 != 1 {
		_ = conn.Close()
		if err == nil {
			err = errors.New("another process is still migrating the database")
		}
		return nil, err
	}
	return func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK('cactusinv_migrate')")
		_ = conn.Close()
	}, nil
}

// Migrate applies (or with down=true, rolls back all) embedded goose migrations.
func Migrate(ctx context.Context, db *sql.DB, down bool) error {
	release, err := migrationLock(ctx, db)
	if err != nil {
		return fmt.Errorf("mariadb.Migrate: lock: %w", err)
	}
	defer release()
	provider, err := goose.NewProvider(goose.DialectMySQL, db, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
	if err != nil {
		return fmt.Errorf("mariadb.Migrate: %w", err)
	}
	if down {
		_, err = provider.DownTo(ctx, 0)
	} else {
		_, err = provider.Up(ctx)
	}
	if err != nil {
		return fmt.Errorf("mariadb.Migrate: %w", err)
	}
	return nil
}

// MigrateTo applies migrations up to and including version (tests and recovery).
func MigrateTo(ctx context.Context, db *sql.DB, version int64) error {
	release, err := migrationLock(ctx, db)
	if err != nil {
		return fmt.Errorf("mariadb.MigrateTo: lock: %w", err)
	}
	defer release()
	provider, err := goose.NewProvider(goose.DialectMySQL, db, migrations.FS, goose.WithGoMigrations(migrations.GoMigrations()...))
	if err != nil {
		return fmt.Errorf("mariadb.MigrateTo: %w", err)
	}
	if _, err := provider.UpTo(ctx, version); err != nil {
		return fmt.Errorf("mariadb.MigrateTo: %w", err)
	}
	return nil
}

// Store implements ports.TxManager on a *sql.DB.
type Store struct {
	db *sql.DB
}

// NewStore wraps db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// maxTxAttempts bounds retries of transactions aborted by deadlocks or lock timeouts.
const maxTxAttempts = 3

// WithinTx implements ports.TxManager. It runs fn at READ COMMITTED and retries it when
// MariaDB aborts the transaction with a deadlock or lock-wait timeout.
func (s *Store) WithinTx(ctx context.Context, fn func(context.Context, ports.Repos) error) error {
	var err error
	for attempt := 1; attempt <= maxTxAttempts; attempt++ {
		err = s.once(ctx, fn)
		if !retryable(err) || ctx.Err() != nil {
			return err
		}
		time.Sleep(time.Duration(attempt*20) * time.Millisecond)
	}
	return err
}

func (s *Store) once(ctx context.Context, fn func(context.Context, ports.Repos) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("mariadb.WithinTx: begin: %w", err)
	}
	if err := fn(ctx, repos(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mariadb.WithinTx: commit: %w", mapErr(err))
	}
	return nil
}

// Repos implements ports.TxManager.
func (s *Store) Repos() ports.Repos { return repos(s.db) }

// Ping checks the database for readiness probes.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func repos(db sqlcgen.DBTX) ports.Repos {
	q := sqlcgen.New(db)
	return ports.Repos{
		Business: businessRepo{q}, Currencies: currencyRepo{q}, Banks: bankRepo{q},
		Customers: customerRepo{q}, Products: productRepo{q}, Invoices: invoiceRepo{q},
		Sequences: sequenceRepo{q}, Schedules: scheduleRepo{q}, Emails: emailRepo{q},
		Users: userRepo{q}, Groups: groupRepo{q}, Sessions: sessionRepo{q}, Idempotency: idempotencyRepo{q}, SMTP: smtpRepo{q}, Audit: auditRepo{q},
		System: systemRepo{q}, BackupRuns: backupRunRepo{q}, SystemSettings: systemSettingsRepo{q},
	}
}

// MariaDB error numbers this adapter translates.
const (
	errDupEntry        = 1062
	errRowIsReferenced = 1451
	errNoReferencedRow = 1452
	errLockWaitTimeout = 1205
	errDeadlock        = 1213
	errCheckViolation  = 4025
	errDataTooLong     = 1406
	errBadStringValue  = 1366
)

// tooLongColumn reads the column from "Data too long for column 'x' at row 1".
var tooLongColumn = regexp.MustCompile(`column '([^']+)'`)

// mapErr translates driver errors into domain sentinels, keeping the original text.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case errDupEntry, errRowIsReferenced:
			return fmt.Errorf("%w: %s", domain.ErrConflict, me.Message)
		case errNoReferencedRow:
			return fmt.Errorf("%w: %s", domain.ErrNotFound, me.Message)
		case errCheckViolation:
			return fmt.Errorf("%w: %s", domain.ErrValidation, me.Message)
		case errDataTooLong, errBadStringValue:
			// A value longer than its column (or not storable in it) is the caller's input:
			// a 422 naming the field, not a 500.
			field := "input"
			if m := tooLongColumn.FindStringSubmatch(me.Message); m != nil {
				field = m[1]
			}
			reason := "is too long"
			if me.Number == errBadStringValue {
				reason = "contains characters that cannot be stored"
			}
			return &domain.ValidationError{Field: field, Reason: reason}
		}
	}
	return err
}

func retryable(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == errDeadlock || me.Number == errLockWaitTimeout)
}

// affected maps an :execrows result to ErrConflict (version mismatch) or ErrNotFound.
func affected(n int64, err error, missing error) error {
	if err != nil {
		return mapErr(err)
	}
	if n == 0 {
		return missing
	}
	return nil
}
