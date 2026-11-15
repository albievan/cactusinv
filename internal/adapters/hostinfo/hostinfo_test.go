package hostinfo_test

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/hostinfo"
)

func TestProbe(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	p := hostinfo.Probe{StartedAt: start}
	r := p.Runtime()
	if r.Goroutines < 1 || r.HeapBytes == 0 || r.CPUs < 1 || r.OS != runtime.GOOS || !r.StartedAt.Equal(start) {
		t.Fatalf("runtime = %+v", r)
	}
	if runtime.GOOS != "windows" {
		d, err := p.Disk(t.TempDir())
		if err != nil || d.TotalBytes == 0 || d.FreeBytes > d.TotalBytes {
			t.Fatalf("disk = %+v, %v", d, err)
		}
	}
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if _, err := p.Reach(context.Background(), "127.0.0.1", port); err != nil {
		t.Fatalf("reach an open port: %v", err)
	}
	_ = l.Close()
	if _, err := p.Reach(context.Background(), "127.0.0.1", port); err == nil {
		t.Fatal("a closed port answered")
	}
}
