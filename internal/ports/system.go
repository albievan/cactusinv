package ports

import (
	"context"
	"time"
)

// System status and health ports (M57): what the app can see of itself, its database and
// the services it talks to. Nothing here changes anything.

// Setting is a named value: a database variable or status counter, or a configuration item.
type Setting struct {
	Name  string
	Value string
}

// DBPool is the application's connection pool to the database.
type DBPool struct {
	MaxOpen, Open, InUse, Idle int
	WaitCount                  int64
	WaitDuration               time.Duration
}

// DatabaseStatus is the database server as the application's connection sees it.
type DatabaseStatus struct {
	Version, VersionComment string
	Uptime                  time.Duration
	// Ping is how long a round trip took.
	Ping                 time.Duration
	SizeBytes            int64
	Tables               int
	Connections          int64 // Threads_connected
	MaxConnections       int64 // max_connections
	MaxUsedConnections   int64 // Max_used_connections
	Queries, SlowQueries int64
	Charset, Collation   string
	TimeZone             string
	// Variables and Status are every global variable and status counter, sorted by name.
	Variables, Status []Setting
	Pool              DBPool
}

// DatabaseInspector reads the database server's settings and status.
type DatabaseInspector interface {
	Inspect(ctx context.Context) (DatabaseStatus, error)
}

// RuntimeStats is the running process.
type RuntimeStats struct {
	Goroutines          int
	HeapBytes, SysBytes uint64
	GCRuns              uint32
	CPUs                int
	GoVersion, OS, Arch string
	Hostname            string
	StartedAt           time.Time
}

// DiskUsage is a filesystem's space.
type DiskUsage struct {
	FreeBytes, TotalBytes uint64
}

// HostProbe looks at the process and its surroundings.
type HostProbe interface {
	Runtime() RuntimeStats
	// Disk reports the space of the filesystem holding path (ErrNotConfigured where the
	// platform cannot tell).
	Disk(path string) (DiskUsage, error)
	// Reach opens and closes a TCP connection to host:port and reports how long it took.
	Reach(ctx context.Context, host string, port int) (time.Duration, error)
}

// RequestStats are the API requests served since the start.
type RequestStats struct {
	Total, ClientErrors, ServerErrors int64
	// Average and P95 are over the most recent requests (up to 1000).
	Average, P95 time.Duration
	// LastMinute is how many requests finished in the last 60 seconds.
	LastMinute int64
}

// RequestStatsSource reports the request statistics.
type RequestStatsSource interface {
	RequestStats() RequestStats
}
