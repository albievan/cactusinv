package domain

import (
	"time"
)

// Date is a calendar date with no time or zone (issue dates, due dates).
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate normalises y/m/d (e.g. 31 Feb → 3 Mar) like time.Date does.
func NewDate(y int, m time.Month, d int) Date {
	return DateOf(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

// DateOf returns the calendar date of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate parses an ISO date "2006-01-02".
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, invalid("date", "must be YYYY-MM-DD")
	}
	return DateOf(t), nil
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool { return d == Date{} }

// String renders ISO "2006-01-02".
func (d Date) String() string { return d.Time(time.UTC).Format(time.DateOnly) }

// Long renders "21 August 2026", as printed on invoices.
func (d Date) Long() string { return d.Time(time.UTC).Format("2 January 2006") }

// Time returns midnight of d in loc.
func (d Date) Time(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// AddDays returns d shifted by n days.
func (d Date) AddDays(n int) Date { return NewDate(d.Year, d.Month, d.Day+n) }

// Before reports whether d is strictly before o.
func (d Date) Before(o Date) bool { return d.Time(time.UTC).Before(o.Time(time.UTC)) }

// After reports whether d is strictly after o.
func (d Date) After(o Date) bool { return o.Before(d) }

// DaysIn returns the number of days in month m of year y.
func DaysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// clampedDate returns day d of month m (normalised across years), clamped to the month
// end; d <= 0 means the last day of the month.
func clampedDate(y int, m time.Month, d int) Date {
	first := NewDate(y, m, 1)
	last := DaysIn(first.Year, first.Month)
	if d <= 0 || d > last {
		d = last
	}
	return Date{Year: first.Year, Month: first.Month, Day: d}
}
