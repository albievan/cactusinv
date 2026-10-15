package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// AuditEntry is one record in the append-only audit trail. Entries are hash-chained: each
// Hash covers the entry's content and the previous entry's Hash, so removing or changing
// any entry — even directly in the database — breaks the chain and is detected by
// VerifyAuditChain.
type AuditEntry struct {
	Seq        int64 // assigned by the store, ascending
	At         time.Time
	ActorID    string
	ActorName  string
	Action     string // e.g. "invoice.voided", "customer.updated", "auth.login_failed"
	EntityType string // invoice | customer | product | bank_account | currency | settings | schedule | email | user
	EntityID   string
	Summary    string
	PrevHash   string
	Hash       string
}

// AuditTimeLayout is the canonical timestamp format used in the hash (UTC, microseconds).
const AuditTimeLayout = "2006-01-02 15:04:05.000000"

// ComputeHash returns the chained SHA-256 of e given the previous entry's hash.
func (e AuditEntry) ComputeHash(prev string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		prev, e.At.UTC().Format(AuditTimeLayout), e.ActorID, e.ActorName, e.Action, e.EntityType, e.EntityID, e.Summary,
	}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// Seal sets PrevHash and Hash so e follows prev in the chain.
func (e *AuditEntry) Seal(prev string) {
	e.At = e.At.UTC().Truncate(time.Microsecond)
	e.Summary = truncate(e.Summary, 1000)
	e.PrevHash = prev
	e.Hash = e.ComputeHash(prev)
}

// AuditVerification is the result of checking the chain.
type AuditVerification struct {
	OK       bool
	Entries  int
	BrokenAt int64  // Seq of the first entry that does not verify (0 when OK)
	Reason   string // why it broke
}

// VerifyAuditChain checks entries (in Seq order, starting at the first entry ever
// written) link and hash correctly, and that no sequence numbers are missing.
func VerifyAuditChain(entries []AuditEntry) AuditVerification {
	prev := ""
	var lastSeq int64
	for i, e := range entries {
		switch {
		case i > 0 && e.Seq != lastSeq+1:
			return AuditVerification{Entries: i, BrokenAt: e.Seq, Reason: "entries are missing before this one"}
		case e.PrevHash != prev:
			return AuditVerification{Entries: i, BrokenAt: e.Seq, Reason: "does not link to the previous entry"}
		case e.ComputeHash(prev) != e.Hash:
			return AuditVerification{Entries: i, BrokenAt: e.Seq, Reason: "content was altered"}
		}
		prev, lastSeq = e.Hash, e.Seq
	}
	return AuditVerification{OK: true, Entries: len(entries)}
}
