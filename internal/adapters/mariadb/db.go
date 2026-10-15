// Package mariadb implements cactusInv's repositories on MariaDB 11 (ADR-002) using
// sqlc-generated queries in ./sqlcgen.
package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"

	"github.com/albievan/simpleinvoice/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/simpleinvoice/internal/domain"
	"github.com/albievan/simpleinvoice/internal/ports"
	"github.com/albievan/simpleinvoice/migrations"
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

// Migrate applies (or with down=true, rolls back all) embedded goose migrations.
func Migrate(ctx context.Context, db *sql.DB, down bool) error {
	provider, err := goose.NewProvider(goose.DialectMySQL, db, migrations.FS)
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
		Users: userRepo{q}, Sessions: sessionRepo{q}, Idempotency: idempotencyRepo{q},
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
)

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
