package domain

import (
	"testing"
	"time"
)

func TestAtInDaylightSavingGaps(t *testing.T) {
	cases := []struct {
		zone         string
		d            Date
		h, m         int
		wantH, wantM int
		wantDay      int
	}{
		{"America/New_York", NewDate(2026, 3, 8), 2, 30, 3, 30, 8},     // 60-minute gap
		{"Australia/Lord_Howe", NewDate(2026, 10, 4), 2, 15, 2, 45, 4}, // 30-minute gap
		{"America/New_York", NewDate(2026, 3, 8), 9, 0, 9, 0, 8},       // no gap
	}
	for _, c := range cases {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Skip("no tzdata")
		}
		r := Recurrence{TimeZone: c.zone, SendHour: c.h, SendMinute: c.m}
		at := r.At(c.d).In(loc)
		if at.Hour() != c.wantH || at.Minute() != c.wantM || at.Day() != c.wantDay {
			t.Errorf("%s %v %02d:%02d → %v", c.zone, c.d, c.h, c.m, at)
		}
	}
	// A gap that reaches midnight never moves the run to the next date.
	for _, zone := range []string{"America/Nuuk", "America/Havana", "Asia/Beirut", "America/Santiago"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			continue
		}
		for day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); day.Year() == 2026; day = day.AddDate(0, 0, 1) {
			d := NewDate(day.Year(), day.Month(), day.Day())
			for _, hm := range [][2]int{{23, 30}, {0, 0}, {0, 30}, {2, 30}} {
				at := Recurrence{TimeZone: zone, SendHour: hm[0], SendMinute: hm[1]}.At(d).In(loc)
				if at.Day() != d.Day {
					t.Fatalf("%s %v %v: fires on another date: %v", zone, d, hm, at)
				}
			}
		}
	}
}
