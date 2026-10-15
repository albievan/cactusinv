package domain

import (
	"time"
)

// Frequency is how often a schedule recurs.
type Frequency string

// Frequencies.
const (
	FrequencyOnce      Frequency = "once"
	FrequencyWeekly    Frequency = "weekly"
	FrequencyMonthly   Frequency = "monthly"
	FrequencyQuarterly Frequency = "quarterly"
	FrequencyYearly    Frequency = "yearly"
)

// maxOccurrenceScan bounds the search for the next occurrence.
const maxOccurrenceScan = 5000

// Recurrence describes when a schedule fires, in the schedule's own time zone.
type Recurrence struct {
	Frequency  Frequency
	Interval   int          // every N periods, >= 1
	DayOfMonth int          // monthly/quarterly/yearly: 1–31 (clamped to month end), 0 = last day
	Weekday    time.Weekday // weekly
	SendHour   int
	SendMinute int
	TimeZone   string
	StartDate  Date
	EndDate    Date // zero = no end
}

// Validate checks a recurrence's invariants.
func (r *Recurrence) Validate() error {
	switch r.Frequency {
	case FrequencyOnce, FrequencyWeekly, FrequencyMonthly, FrequencyQuarterly, FrequencyYearly:
	default:
		return invalid("frequency", "must be once, weekly, monthly, quarterly or yearly")
	}
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 52 {
		return invalid("interval", "must be between 1 and 52")
	}
	if r.DayOfMonth < 0 || r.DayOfMonth > 31 {
		return invalid("dayOfMonth", "must be between 0 (last day) and 31")
	}
	if r.Weekday < time.Sunday || r.Weekday > time.Saturday {
		return invalid("weekday", "must be between 0 (Sunday) and 6 (Saturday)")
	}
	if r.SendHour < 0 || r.SendHour > 23 || r.SendMinute < 0 || r.SendMinute > 59 {
		return invalid("sendTime", "must be a valid HH:MM")
	}
	if r.TimeZone == "" {
		r.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(r.TimeZone); err != nil {
		return invalid("timeZone", "unknown IANA time zone")
	}
	if r.StartDate.IsZero() {
		return invalid("startDate", "is required")
	}
	if !r.EndDate.IsZero() && r.EndDate.Before(r.StartDate) {
		return invalid("endDate", "must not be before startDate")
	}
	return nil
}

func (r Recurrence) location() *time.Location {
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// At returns the instant the recurrence fires on local date d. A send time that falls in
// a daylight-saving gap fires just after the gap, never before the requested time.
func (r Recurrence) At(d Date) time.Time {
	loc := r.location()
	at := time.Date(d.Year, d.Month, d.Day, r.SendHour, r.SendMinute, 0, 0, loc)
	if at.Hour() != r.SendHour || at.Minute() != r.SendMinute {
		at = time.Date(d.Year, d.Month, d.Day, r.SendHour+1, r.SendMinute, 0, 0, loc)
	}
	return at
}

// LocalDate returns the schedule-local calendar date of instant t.
func (r Recurrence) LocalDate(t time.Time) Date { return DateOf(t.In(r.location())) }

// occurrence returns the k-th candidate date (k >= 0). Candidates before StartDate are
// skipped by the caller.
func (r Recurrence) occurrence(k int) Date {
	s := r.StartDate
	switch r.Frequency {
	case FrequencyWeekly:
		offset := (int(r.Weekday) - int(s.Time(time.UTC).Weekday()) + 7) % 7
		return s.AddDays(offset + 7*r.Interval*k)
	case FrequencyMonthly, FrequencyQuarterly, FrequencyYearly:
		return clampedDate(s.Year, s.Month+time.Month(r.monthStep()*k), r.DayOfMonth)
	default:
		return s
	}
}

func (r Recurrence) monthStep() int {
	switch r.Frequency {
	case FrequencyQuarterly:
		return 3 * r.Interval
	case FrequencyYearly:
		return 12 * r.Interval
	default:
		return r.Interval
	}
}

// startIndex estimates a k at or before the first occurrence after t, to keep scans short.
func (r Recurrence) startIndex(t time.Time) int {
	d := r.LocalDate(t)
	var k int
	switch r.Frequency {
	case FrequencyWeekly:
		days := int(d.Time(time.UTC).Sub(r.StartDate.Time(time.UTC)).Hours() / 24)
		k = days/(7*r.Interval) - 1
	case FrequencyMonthly, FrequencyQuarterly, FrequencyYearly:
		months := (d.Year-r.StartDate.Year)*12 + int(d.Month-r.StartDate.Month)
		k = months/r.monthStep() - 1
	}
	return max(k, 0)
}

// NextAfter returns the first occurrence strictly after t, or ok=false when the
// recurrence has no further occurrences.
func (r Recurrence) NextAfter(t time.Time) (time.Time, bool) {
	if r.Frequency == FrequencyOnce {
		at := r.At(r.StartDate)
		return at, at.After(t)
	}
	start := r.startIndex(t)
	for k := start; k < start+maxOccurrenceScan; k++ {
		d := r.occurrence(k)
		if d.Before(r.StartDate) {
			continue
		}
		if !r.EndDate.IsZero() && d.After(r.EndDate) {
			return time.Time{}, false
		}
		if at := r.At(d); at.After(t) {
			return at, true
		}
	}
	return time.Time{}, false
}

// Upcoming lists up to n occurrences strictly after t.
func (r Recurrence) Upcoming(t time.Time, n int) []time.Time {
	var out []time.Time
	for len(out) < n {
		next, ok := r.NextAfter(t)
		if !ok {
			break
		}
		out = append(out, next)
		t = next
	}
	return out
}
