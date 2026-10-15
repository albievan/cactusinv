package clock

import (
	"testing"
	"time"
)

func TestSystem(t *testing.T) {
	if n := (System{}).Now(); n.Location() != time.UTC || time.Since(n) > time.Second {
		t.Fatalf("Now = %v", n)
	}
}
