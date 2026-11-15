package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/albievan/cactusinv/internal/ports"
)

// Inspector reads the database server's settings and status for the Database & containers
// page (M57). It only reads: SHOW GLOBAL VARIABLES and STATUS need no privilege beyond a
// connection, and the size comes from information_schema for this database.
type Inspector struct {
	DB *sql.DB
}

var _ ports.DatabaseInspector = Inspector{}

// Inspect implements ports.DatabaseInspector.
func (i Inspector) Inspect(ctx context.Context) (ports.DatabaseStatus, error) {
	var st ports.DatabaseStatus
	start := time.Now()
	if err := i.DB.PingContext(ctx); err != nil {
		return st, fmt.Errorf("mariadb.Inspect: ping: %w", err)
	}
	st.Ping = time.Since(start)
	vars, err := i.pairs(ctx, "SHOW GLOBAL VARIABLES")
	if err != nil {
		return st, fmt.Errorf("mariadb.Inspect: variables: %w", err)
	}
	status, err := i.pairs(ctx, "SHOW GLOBAL STATUS")
	if err != nil {
		return st, fmt.Errorf("mariadb.Inspect: status: %w", err)
	}
	get := func(list []ports.Setting, name string) string {
		n := sort.Search(len(list), func(k int) bool { return list[k].Name >= name })
		if n < len(list) && list[n].Name == name {
			return list[n].Value
		}
		return ""
	}
	num := func(list []ports.Setting, name string) int64 {
		v, _ := strconv.ParseInt(get(list, name), 10, 64)
		return v
	}
	st.Variables, st.Status = vars, status
	st.Version, st.VersionComment = get(vars, "version"), get(vars, "version_comment")
	st.Uptime = time.Duration(num(status, "Uptime")) * time.Second
	st.Connections, st.MaxUsedConnections = num(status, "Threads_connected"), num(status, "Max_used_connections")
	st.MaxConnections = num(vars, "max_connections")
	st.Queries, st.SlowQueries = num(status, "Questions"), num(status, "Slow_queries")
	st.Charset, st.Collation = get(vars, "character_set_server"), get(vars, "collation_server")
	st.TimeZone = get(vars, "time_zone")
	if st.TimeZone == "SYSTEM" {
		st.TimeZone = "SYSTEM (" + get(vars, "system_time_zone") + ")"
	}
	err = i.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(data_length + index_length), 0), COUNT(*)
		FROM information_schema.tables WHERE table_schema = DATABASE()`).Scan(&st.SizeBytes, &st.Tables)
	if err != nil {
		return st, fmt.Errorf("mariadb.Inspect: size: %w", err)
	}
	p := i.DB.Stats()
	st.Pool = ports.DBPool{MaxOpen: p.MaxOpenConnections, Open: p.OpenConnections, InUse: p.InUse, Idle: p.Idle, WaitCount: p.WaitCount, WaitDuration: p.WaitDuration}
	return st, nil
}

// pairs reads a two-column SHOW statement, sorted by name.
func (i Inspector) pairs(ctx context.Context, query string) ([]ports.Setting, error) {
	rows, err := i.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.Setting
	for rows.Next() {
		var name string
		var value sql.NullString
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out = append(out, ports.Setting{Name: name, Value: value.String})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, rows.Err()
}
