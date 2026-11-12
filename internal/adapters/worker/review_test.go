package worker

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// A panicking pass is logged and does not take the process down (M36).
func TestPanickingPassIsContained(t *testing.T) {
	var log bytes.Buffer
	r := &Runner{log: slog.New(slog.NewTextHandler(&log, nil))}
	r.safely(context.Background(), "emails", func(context.Context) { panic("bad row") })
	if !strings.Contains(log.String(), "worker pass panicked") || !strings.Contains(log.String(), "bad row") {
		t.Fatalf("log = %s", log.String())
	}
}
