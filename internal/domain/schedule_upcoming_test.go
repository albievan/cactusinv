package domain

import (
	"testing"
	"time"
)

// The next invoice a schedule raises: an active one's pending occurrence; a paused one's
// as if resumed now (M32).
func TestScheduleUpcoming(t *testing.T) {
	loc := jhb(t)
	s := cityDeepSchedule()
	s.Activate(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	up, ok := s.Upcoming(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), Date{})
	if !ok || up.Period() != NewDate(2026, 8, 21) || up.Status != ScheduleActive {
		t.Fatalf("active: %v %+v", ok, up)
	}

	// Paused in August, looked at in October: it would raise October's, not August's,
	// and stays shown as paused. The stored schedule is untouched.
	if err := s.Pause(time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	up, ok = s.Upcoming(now, NewDate(2026, 8, 1))
	if !ok || up.Period() != NewDate(2026, 10, 21) || up.Status != SchedulePaused || !up.NextRunAt.IsZero() {
		t.Fatalf("paused: %v %+v", ok, up)
	}
	if s.Status != SchedulePaused || s.Period() != NewDate(2026, 8, 21) {
		t.Fatalf("the stored schedule changed: %+v", s)
	}

	// Paused after failing: the failed period is still the next one.
	f := cityDeepSchedule()
	f.Activate(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	f.RecordFailure("product inactive", time.Date(2026, 8, 21, 9, 0, 0, 0, loc))
	if err := f.Pause(time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if up, ok := f.Upcoming(now, Date{}); !ok || up.Period() != NewDate(2026, 8, 21) {
		t.Fatalf("failed pending: %v %+v", ok, up)
	}

	e := cityDeepSchedule()
	e.Kind, e.EmailScope = ScheduleEmail, EmailUnsent
	e.Activate(now)
	if _, ok := e.Upcoming(now, Date{}); ok {
		t.Fatal("an e-mail schedule raises no invoice")
	}
	ended := cityDeepSchedule()
	ended.Status = ScheduleEnded
	if _, ok := ended.Upcoming(now, Date{}); ok {
		t.Fatal("an ended schedule raises no invoice")
	}
}

func TestScheduleSampleInvoice(t *testing.T) {
	s := cityDeepSchedule()
	s.Notes = "Hours for {period}."
	s.Activate(time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	products := map[string]Product{"p-users": {ID: "p-users", SKU: "U", Name: "Users", Prices: map[CurrencyCode]int64{"ZAR": 19294}, Active: true}}
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	inv, err := s.SampleInvoice(cityDeep(), products, 0, s.RunDate(), BankDetails{BankName: "FNB"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Sample || inv.Number != SampleNumber || inv.ScheduleID != s.ID || inv.IssueDate != NewDate(2026, 8, 21) ||
		inv.DueDate.IsZero() || inv.Notes != "Hours for August 2026." || inv.Totals.Total != 325116 || inv.Bank.BankName != "FNB" {
		t.Fatalf("sample = %+v", inv)
	}
	inactive := cityDeep()
	inactive.Active = false
	if _, err := s.SampleInvoice(inactive, products, 0, s.RunDate(), BankDetails{}, now); err == nil {
		t.Fatal("an inactive customer gets no sample, as it gets no invoice")
	}
}
