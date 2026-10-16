package domain

import (
	"fmt"
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

// WeekendRule says when a day-of-month run that falls on a weekend fires.
type WeekendRule string

// Weekend rules.
const (
	// WeekendNone runs on the day itself.
	WeekendNone WeekendRule = "none"
	// WeekendPreviousFriday runs on the Friday before: a Saturday 1 day, a Sunday 2 days
	// earlier (the first of a month can move into the previous month).
	WeekendPreviousFriday WeekendRule = "previous-friday"
)

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
	EndDate    Date        // zero = no end
	Weekend    WeekendRule // monthly, quarterly, yearly; "" = none (see DefaultWeekendRule)
}

// dayBased reports whether the recurrence picks a day of the month.
func (r Recurrence) dayBased() bool {
	return r.Frequency == FrequencyMonthly || r.Frequency == FrequencyQuarterly || r.Frequency == FrequencyYearly
}

// DefaultWeekendRule gives a new recurrence that names no weekend rule the default: a
// day-of-month recurrence moves weekend runs to the Friday before.
func (r *Recurrence) DefaultWeekendRule() {
	if r.Weekend == "" && r.dayBased() {
		r.Weekend = WeekendPreviousFriday
	}
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
	switch r.Weekend {
	case "", WeekendNone, WeekendPreviousFriday:
	default:
		return invalid("weekendRule", "must be previous-friday or none")
	}
	if r.Weekend == "" || !r.dayBased() {
		r.Weekend = WeekendNone
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

// PeriodKey names the billing period of an occurrence nominally on d: its month
// ("2026-10") for monthly, its calendar quarter ("2026-Q4") for quarterly, its year
// ("2026") for yearly, and the date ("2026-10-15") for weekly and once. A schedule raises
// at most one invoice per key, so moving the day or the start date after a run cannot bill
// the same period twice.
func (r Recurrence) PeriodKey(d Date) string {
	switch r.Frequency {
	case FrequencyMonthly:
		return fmt.Sprintf("%04d-%02d", d.Year, int(d.Month))
	case FrequencyQuarterly:
		return fmt.Sprintf("%04d-Q%d", d.Year, (int(d.Month)-1)/3+1)
	case FrequencyYearly:
		return fmt.Sprintf("%04d", d.Year)
	default:
		return d.String()
	}
}

// PeriodStart is the first day of the billing period (see PeriodKey) of the date d.
func (r Recurrence) PeriodStart(d Date) Date {
	switch r.Frequency {
	case FrequencyMonthly:
		return NewDate(d.Year, d.Month, 1)
	case FrequencyQuarterly:
		return NewDate(d.Year, time.Month((int(d.Month)-1)/3*3+1), 1)
	case FrequencyYearly:
		return NewDate(d.Year, time.January, 1)
	default:
		return d
	}
}

// RunDate is the local date an occurrence nominally on d fires: d itself, or under
// WeekendPreviousFriday the Friday before when d is a Saturday or Sunday.
func (r Recurrence) RunDate(d Date) Date {
	if r.Weekend != WeekendPreviousFriday || !r.dayBased() {
		return d
	}
	switch d.Time(time.UTC).Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(-2)
	default:
		return d
	}
}

// RunTime is when the occurrence whose nominal instant is nominal (as NextAfter returns
// it) fires, after the weekend rule.
func (r Recurrence) RunTime(nominal time.Time) time.Time {
	d := r.LocalDate(nominal)
	if rd := r.RunDate(d); rd != d {
		return r.At(rd)
	}
	return nominal
}

// Upcoming lists the run times of the next n occurrences nominally after t, as a schedule
// activated at t would run them: a run the weekend rule moved before t runs at t.
func (r Recurrence) Upcoming(t time.Time, n int) []time.Time {
	var out []time.Time
	if n <= 0 {
		return out
	}
	nominal, ok := r.NextAfter(t)
	if !ok {
		return out
	}
	run := r.RunTime(nominal)
	if run.Before(t) {
		run = t
	}
	return append([]time.Time{run}, r.RunsAfter(nominal, n-1)...)
}

// RunsAfter lists the run times of up to n occurrences nominally after nominal.
func (r Recurrence) RunsAfter(nominal time.Time, n int) []time.Time {
	var out []time.Time
	for len(out) < n {
		next, ok := r.NextAfter(nominal)
		if !ok {
			break
		}
		out = append(out, r.RunTime(next))
		nominal = next
	}
	return out
}
