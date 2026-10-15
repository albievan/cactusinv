package domain

import (
	"fmt"
	"time"
)

// AuditMergeMode is how a restore brings the backup's audit entries into the live trail
// (ADR-009 amendment). No mode ever deletes or rewrites a live entry.
type AuditMergeMode string

// Audit merge modes.
const (
	// AuditKeepCurrent: every backup entry is already in the live trail.
	AuditKeepCurrent AuditMergeMode = "keep_current"
	// AuditAppendVerbatim: the live trail is a prefix of the backup's; the missing entries
	// are appended unchanged, keeping their hashes.
	AuditAppendVerbatim AuditMergeMode = "append_verbatim"
	// AuditAppendResealed: the trails diverged; the backup's entries after the fork are
	// appended with their content unchanged but re-sealed onto the live head, between two
	// marker entries.
	AuditAppendResealed AuditMergeMode = "append_resealed"
)

// Marker actions written around re-sealed entries.
const (
	AuditResealBegin = "backup.audit_resealed_begin"
	AuditResealEnd   = "backup.audit_resealed_end"
)

// AuditMergePlan says what a restore appends to the audit trail.
type AuditMergePlan struct {
	Mode        AuditMergeMode
	LiveHeadSeq int64 // last entry of the live trail when planned (0: empty)
	CommonSeq   int64 // last entry both trails share (0: none)
	FromSeq     int64 // first backup entry appended (0: none)
	Count       int   // backup entries appended
	BackupHead  AuditRef
	// BackupBroken is set when the backup's own chain does not verify; its entries are
	// then re-sealed and the marker names where it broke.
	BackupBroken   bool
	BackupBrokenAt int64
}

// PlanAuditMerge compares the live trail with the backup's (both oldest first, complete).
func PlanAuditMerge(current, backup []AuditEntry) AuditMergePlan {
	k := 0
	for k < len(current) && k < len(backup) && current[k].Seq == backup[k].Seq && current[k].Hash == backup[k].Hash {
		k++
	}
	p := AuditMergePlan{Mode: AuditKeepCurrent}
	if n := len(current); n > 0 {
		p.LiveHeadSeq = current[n-1].Seq
	}
	if n := len(backup); n > 0 {
		p.BackupHead = AuditRef{Seq: backup[n-1].Seq, Hash: backup[n-1].Hash}
	}
	if k > 0 {
		p.CommonSeq = backup[k-1].Seq
	}
	if k == len(backup) {
		return p
	}
	v := VerifyAuditChain(backup)
	p.FromSeq, p.Count = backup[k].Seq, len(backup)-k
	p.BackupBroken, p.BackupBrokenAt = !v.OK, v.BrokenAt
	if k == len(current) && v.OK {
		p.Mode = AuditAppendVerbatim
	} else {
		p.Mode = AuditAppendResealed
	}
	return p
}

// ResealSeqs are the sequence numbers a re-seal writes: the begin marker, the first and
// last re-sealed entry, and the end marker. They follow the live head the plan was made on.
func (p AuditMergePlan) ResealSeqs() (begin, first, last, end int64) {
	begin = p.LiveHeadSeq + 1
	return begin, begin + 1, begin + int64(p.Count), begin + int64(p.Count) + 1
}

// Summary describes the plan in one line for the preview and the audit trail, naming the
// exact entries it appends.
func (p AuditMergePlan) Summary() string {
	var s string
	switch p.Mode {
	case AuditAppendVerbatim:
		s = fmt.Sprintf("append %d entries from the backup unchanged (#%d–#%d)", p.Count, p.FromSeq, p.BackupHead.Seq)
	case AuditAppendResealed:
		begin, first, last, end := p.ResealSeqs()
		s = fmt.Sprintf("append %d entries from the backup (originally #%d–#%d, trails diverged after #%d) re-sealed as #%d–#%d between markers #%d and #%d",
			p.Count, p.FromSeq, p.BackupHead.Seq, p.CommonSeq, first, last, begin, end)
	default:
		s = "keep the current audit trail (it already has every backup entry)"
	}
	if p.BackupBroken {
		s += fmt.Sprintf("; the backup's own chain is broken at #%d", p.BackupBrokenAt)
	}
	return s
}

// AuditMarker is who and when the re-seal markers record.
type AuditMarker struct {
	At        time.Time
	ActorID   string
	ActorName string
}

// Appendix returns the entries to append to the live trail, in order. Verbatim entries
// keep Seq, PrevHash and Hash (append them with AuditRepository.AppendVerbatim);
// re-sealed entries and markers have them cleared (append them with Append, which seals).
func (p AuditMergePlan) Appendix(backup []AuditEntry, m AuditMarker) []AuditEntry {
	if p.Mode == AuditKeepCurrent || p.Count == 0 {
		return nil
	}
	tail := backup[len(backup)-p.Count:]
	if p.Mode == AuditAppendVerbatim {
		out := make([]AuditEntry, len(tail))
		copy(out, tail)
		return out
	}
	marker := func(action, summary string) AuditEntry {
		return AuditEntry{At: m.At, ActorID: m.ActorID, ActorName: m.ActorName, Action: action, EntityType: "backup", Summary: summary}
	}
	broken := ""
	if p.BackupBroken {
		broken = fmt.Sprintf("; the backup's chain was broken at #%d", p.BackupBrokenAt)
	}
	// The markers name the exact range they enclose, so a marker-like entry inside the
	// backup's own content cannot pass for the end of the re-sealed block.
	begin, first, last, end := p.ResealSeqs()
	out := make([]AuditEntry, 0, len(tail)+2)
	out = append(out, marker(AuditResealBegin, fmt.Sprintf(
		"the next %d entries (#%d–#%d) are re-sealed copies of backup entries #%d–#%d (original head %s); the trails diverged after #%d; this block ends at marker #%d%s",
		p.Count, first, last, p.FromSeq, p.BackupHead.Seq, p.BackupHead.Hash, p.CommonSeq, end, broken)))
	for _, e := range tail {
		e.Seq, e.PrevHash, e.Hash = 0, "", ""
		out = append(out, e)
	}
	out = append(out, marker(AuditResealEnd, fmt.Sprintf("end of the block begun at marker #%d: %d re-sealed entries #%d–#%d (original head #%d %s)",
		begin, p.Count, first, last, p.BackupHead.Seq, p.BackupHead.Hash)))
	return out
}
