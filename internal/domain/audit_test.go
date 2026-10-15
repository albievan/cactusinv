package domain

import (
	"testing"
	"time"
)

func chain(n int) []AuditEntry {
	var out []AuditEntry
	prev := ""
	for i := range n {
		e := AuditEntry{Seq: int64(i + 1), At: time.Date(2026, 10, 15, 10, 0, i, 123456789, time.UTC), ActorID: "u1", ActorName: "admin", Action: "invoice.created", EntityType: "invoice", EntityID: "inv"}
		e.Seal(prev)
		prev = e.Hash
		out = append(out, e)
	}
	return out
}

func TestAuditChain(t *testing.T) {
	c := chain(5)
	if v := VerifyAuditChain(c); !v.OK || v.Entries != 5 {
		t.Fatalf("intact chain: %+v", v)
	}
	if c[0].PrevHash != "" || c[1].PrevHash != c[0].Hash || len(c[0].Hash) != 64 {
		t.Fatal("linking")
	}
	if c[0].At.Nanosecond()%1000 != 0 {
		t.Fatal("sealed at microsecond precision")
	}
	altered := chain(5)
	altered[2].ActorName = "mallory"
	if v := VerifyAuditChain(altered); v.OK || v.BrokenAt != 3 || v.Reason != "content was altered" {
		t.Fatalf("altered: %+v", v)
	}
	full := chain(5)
	deleted := []AuditEntry{full[0], full[1], full[3], full[4]}
	if v := VerifyAuditChain(deleted); v.OK || v.BrokenAt != 4 {
		t.Fatalf("deleted: %+v", v)
	}
	relinked := chain(3)
	relinked[1].PrevHash = "x"
	if v := VerifyAuditChain(relinked); v.OK || v.Reason != "does not link to the previous entry" {
		t.Fatalf("relinked: %+v", v)
	}
	if v := VerifyAuditChain(nil); !v.OK {
		t.Fatal("empty chain is valid")
	}
}
