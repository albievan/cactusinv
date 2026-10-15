package idgen

import "testing"

func TestUUIDv7(t *testing.T) {
	a, b := UUIDv7{}.NewID(), UUIDv7{}.NewID()
	if len(a) != 36 || a == b || a > b {
		t.Fatalf("ids not unique/ordered: %s %s", a, b)
	}
}
