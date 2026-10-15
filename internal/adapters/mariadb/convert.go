package mariadb

import (
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/albievan/cactusinve/internal/domain"
)

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func fromNullTime(t sql.NullTime) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time.UTC()
}

func dateValue(d domain.Date) time.Time { return d.Time(time.UTC) }

func nullDate(d domain.Date) sql.NullTime {
	if d.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: d.Time(time.UTC), Valid: true}
}

func fromDate(t time.Time) domain.Date { return domain.DateOf(t.UTC()) }

func fromNullDate(t sql.NullTime) domain.Date {
	if !t.Valid {
		return domain.Date{}
	}
	return fromDate(t.Time)
}

// joinList stores an address list one per line; splitList reverses it.
func joinList(xs []string) string { return strings.Join(xs, "\n") }

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// i32 converts a bounded int (page sizes, counts, enum values) for the driver, clamping
// instead of overflowing.
func i32(n int) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	default:
		return int32(n)
	}
}
