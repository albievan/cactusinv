// Package hostinfo looks at the running process and its surroundings for the Database &
// containers page and the dashboard's health (M57): memory and goroutines, free disk, and
// whether a TCP service answers.
package hostinfo

import (
	"context"
	"net"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/albievan/cactusinv/internal/ports"
)

// Probe implements ports.HostProbe.
type Probe struct {
	// StartedAt is when the process started.
	StartedAt time.Time
}

var _ ports.HostProbe = Probe{}

// Runtime implements ports.HostProbe.
func (p Probe) Runtime() ports.RuntimeStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	host, _ := os.Hostname()
	return ports.RuntimeStats{
		Goroutines: runtime.NumGoroutine(), HeapBytes: m.HeapAlloc, SysBytes: m.Sys, GCRuns: m.NumGC, CPUs: runtime.NumCPU(),
		GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: host, StartedAt: p.StartedAt,
	}
}

// Disk implements ports.HostProbe.
func (Probe) Disk(path string) (ports.DiskUsage, error) { return diskUsage(path) }

// Reach implements ports.HostProbe.
func (Probe) Reach(ctx context.Context, host string, port int) (time.Duration, error) {
	start := time.Now()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	took := time.Since(start)
	_ = c.Close()
	return took, nil
}
