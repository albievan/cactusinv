package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestMigrateCommandArgs (M27): bad arguments are refused before the database is opened;
// down-to never means "down to 0" (that is migrate down, spelt out).
func TestMigrateCommandArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"sideways"}, {"down-to"}, {"down-to", "0"}, {"down-to", "-3"}, {"down-to", "12abc"}, {"up", "now"}} {
		err := migrateCommand(context.Background(), config{}, args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "migrate") {
			t.Errorf("%q: %v", args, err)
		}
	}
}
