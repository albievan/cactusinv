package domain

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// TimeOfDay is a wall-clock time (hour and minute).
type TimeOfDay struct {
	Hour, Minute int
}

// ParseBackupTimes parses a comma-separated list of daily times ("02:00,14:30"). An empty
// list means no scheduled backups. The result is sorted and without duplicates.
func ParseBackupTimes(s string) ([]TimeOfDay, error) {
	var out []TimeOfDay
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		t, err := time.Parse("15:04", part)
		if err != nil || len(part) != 5 {
			return nil, invalid("backupTimes", "must be HH:MM times separated by commas, e.g. 02:00,14:00")
		}
		tod := TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}
		if !slices.Contains(out, tod) {
			out = append(out, tod)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Hour*60+out[i].Minute < out[j].Hour*60+out[j].Minute
	})
	return out, nil
}

// backupSlots returns the slot instants (UTC) of the days around now, ascending and unique.
// A time that does not exist on a day (a spring-forward gap) resolves as time.Date does;
// the slot's identity is its instant, so it is the same on every computation.
func backupSlots(times []TimeOfDay, loc *time.Location, now time.Time) []time.Time {
	local := now.In(loc)
	var out []time.Time
	for d := -2; d <= 2; d++ {
		for _, t := range times {
			s := time.Date(local.Year(), local.Month(), local.Day()+d, t.Hour, t.Minute, 0, 0, loc).UTC()
			if !slices.ContainsFunc(out, s.Equal) {
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

// DueBackupSlot returns the most recent slot at or before now (false when no times are
// configured). Only the latest missed slot is due: missed earlier ones are not caught up.
func DueBackupSlot(times []TimeOfDay, loc *time.Location, now time.Time) (time.Time, bool) {
	var due time.Time
	for _, s := range backupSlots(times, loc, now) {
		if !s.After(now) {
			due = s
		}
	}
	return due, !due.IsZero()
}

// NextBackupSlot returns the first slot after now (false when no times are configured).
func NextBackupSlot(times []TimeOfDay, loc *time.Location, now time.Time) (time.Time, bool) {
	for _, s := range backupSlots(times, loc, now) {
		if s.After(now) {
			return s, true
		}
	}
	return time.Time{}, false
}

// BackupFile is a file in the backups directory, as retention sees it.
type BackupFile struct {
	Name   string
	Time   time.Time // from the name for .age files, the modification time for legacy dumps
	Legacy bool      // a plaintext .sql.gz from the retired sidecar
}

// SelectPrunable returns the files older than retentionDays at now. The newest file and
// the newest encrypted backup are never pruned; retentionDays <= 0 prunes nothing.
func SelectPrunable(files []BackupFile, now time.Time, retentionDays int) []string {
	if retentionDays <= 0 || len(files) == 0 {
		return nil
	}
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b BackupFile) int { return b.Time.Compare(a.Time) })
	keep := map[string]bool{sorted[0].Name: true}
	if i := slices.IndexFunc(sorted, func(f BackupFile) bool { return !f.Legacy }); i >= 0 {
		keep[sorted[i].Name] = true
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	var out []string
	for _, f := range sorted {
		if !keep[f.Name] && f.Time.Before(cutoff) {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}
