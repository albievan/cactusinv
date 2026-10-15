package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func jhb(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Africa/Johannesburg")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestRecurrenceNextAfter(t *testing.T) {
	loc := jhb(t)
	monthly := Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 21, SendHour: 9, TimeZone: "Africa/Johannesburg", StartDate: NewDate(2026, 8, 1)}
	lastDay := Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 0, SendHour: 9, TimeZone: "UTC", StartDate: NewDate(2028, 1, 15)}
	day31 := Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 31, TimeZone: "UTC", StartDate: NewDate(2026, 1, 1)}
	quarterly := Recurrence{Frequency: FrequencyQuarterly, Interval: 1, DayOfMonth: 1, TimeZone: "UTC", StartDate: NewDate(2026, 1, 1)}
	yearly := Recurrence{Frequency: FrequencyYearly, Interval: 1, DayOfMonth: 29, TimeZone: "UTC", StartDate: NewDate(2028, 2, 1)}
	weekly := Recurrence{Frequency: FrequencyWeekly, Interval: 2, Weekday: time.Friday, SendHour: 8, SendMinute: 30, TimeZone: "UTC", StartDate: NewDate(2026, 10, 15)} // Thu
	ended := monthly
	ended.EndDate = NewDate(2026, 9, 30)
	once := Recurrence{Frequency: FrequencyOnce, TimeZone: "UTC", SendHour: 10, StartDate: NewDate(2026, 11, 1)}
	nyDST := Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 8, SendHour: 2, SendMinute: 30, TimeZone: "America/New_York", StartDate: NewDate(2026, 3, 1)}

	utc := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, time.UTC) }
	tests := []struct {
		name  string
		r     Recurrence
		after time.Time
		want  time.Time
		ok    bool
	}{
		{"monthly first", monthly, utc(2026, 8, 1, 0, 0), time.Date(2026, 8, 21, 9, 0, 0, 0, loc), true},
		{"monthly same instant is excluded", monthly, time.Date(2026, 8, 21, 9, 0, 0, 0, loc), time.Date(2026, 9, 21, 9, 0, 0, 0, loc), true},
		{"monthly far future", monthly, utc(2030, 5, 30, 0, 0), time.Date(2030, 6, 21, 9, 0, 0, 0, loc), true},
		{"last day leap Feb", lastDay, utc(2028, 2, 1, 0, 0), utc(2028, 2, 29, 9, 0), true},
		{"last day Jan", lastDay, utc(2028, 1, 15, 0, 0), utc(2028, 1, 31, 9, 0), true},
		{"day 31 clamps to Feb 28", day31, utc(2026, 2, 1, 0, 0), utc(2026, 2, 28, 0, 0), true},
		{"day 31 back to 31 in Mar", day31, utc(2026, 2, 28, 0, 0), utc(2026, 3, 31, 0, 0), true},
		{"quarterly", quarterly, utc(2026, 1, 1, 0, 0), utc(2026, 4, 1, 0, 0), true},
		{"yearly 29 Feb then 28", yearly, utc(2028, 3, 1, 0, 0), utc(2029, 2, 28, 0, 0), true},
		{"weekly first Friday", weekly, utc(2026, 10, 15, 0, 0), utc(2026, 10, 16, 8, 30), true},
		{"weekly every 2 weeks", weekly, utc(2026, 10, 16, 8, 30), utc(2026, 10, 30, 8, 30), true},
		{"end date", ended, time.Date(2026, 9, 21, 9, 0, 0, 0, loc), time.Time{}, false},
		{"once before", once, utc(2026, 10, 1, 0, 0), utc(2026, 11, 1, 10, 0), true},
		{"once after", once, utc(2026, 11, 1, 10, 0), utc(2026, 11, 1, 10, 0), false},
		{"DST spring-forward gap normalises", nyDST, utc(2026, 3, 1, 0, 0), time.Date(2026, 3, 8, 3, 30, 0, 0, mustLoc(t, "America/New_York")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.r.Validate(); err != nil {
				t.Fatal(err)
			}
			got, ok := tt.r.NextAfter(tt.after)
			if ok != tt.ok || (ok && !got.Equal(tt.want)) {
				t.Fatalf("NextAfter = %v %v, want %v %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestRecurrenceValidateAndUpcoming(t *testing.T) {
	r := Recurrence{Frequency: FrequencyMonthly, DayOfMonth: 1, StartDate: NewDate(2026, 1, 1)}
	if err := r.Validate(); err != nil || r.Interval != 1 || r.TimeZone != "UTC" {
		t.Fatalf("defaults: %v %+v", err, r)
	}
	up := r.Upcoming(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 3)
	if len(up) != 3 || up[2].Month() != time.April {
		t.Fatalf("Upcoming = %v", up)
	}
	r.EndDate = NewDate(2026, 2, 15)
	if up := r.Upcoming(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5); len(up) != 1 {
		t.Fatalf("Upcoming with end = %v", up)
	}
	bad := []Recurrence{
		{Frequency: "daily", StartDate: NewDate(2026, 1, 1)},
		{Frequency: FrequencyMonthly, Interval: 53, StartDate: NewDate(2026, 1, 1)},
		{Frequency: FrequencyMonthly, DayOfMonth: 32, StartDate: NewDate(2026, 1, 1)},
		{Frequency: FrequencyWeekly, Weekday: 7, StartDate: NewDate(2026, 1, 1)},
		{Frequency: FrequencyMonthly, SendHour: 24, StartDate: NewDate(2026, 1, 1)},
		{Frequency: FrequencyMonthly, TimeZone: "Nowhere/City", StartDate: NewDate(2026, 1, 1)},
		{Frequency: FrequencyMonthly},
		{Frequency: FrequencyMonthly, StartDate: NewDate(2026, 2, 1), EndDate: NewDate(2026, 1, 1)},
	}
	for i, b := range bad {
		if err := b.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func price(v int64) *int64 { return &v }

func cityDeepSchedule() Schedule {
	return Schedule{
		ID: "s1", CustomerID: "cust-1", Name: "Acme monthly",
		Recurrence: Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 21, SendHour: 9, TimeZone: "Africa/Johannesburg", StartDate: NewDate(2026, 8, 1)},
		Lines: []ScheduleLine{
			{ProductID: "p-users", Quantity: 14000},
			{Description: "Alchamex integration - monthly subscription ({period})", Quantity: 1000, UnitPrice: price(55000)},
		},
		AutoSend: true,
	}
}

func TestScheduleLifecycle(t *testing.T) {
	loc := jhb(t)
	s := cityDeepSchedule()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	s.Activate(created)
	first := time.Date(2026, 8, 21, 9, 0, 0, 0, loc)
	if s.Status != ScheduleActive || !s.NextRunAt.Equal(first) || !s.NextOccurrence.Equal(first) {
		t.Fatalf("Activate: %+v", s)
	}
	if s.IsDue(first.Add(-time.Second)) || !s.IsDue(first) {
		t.Fatal("IsDue boundary")
	}
	if s.Period() != NewDate(2026, 8, 21) {
		t.Fatalf("Period = %v", s.Period())
	}

	// Failure retries the same period with backoff, then pauses.
	s.RecordFailure("smtp down", first)
	if s.Period() != NewDate(2026, 8, 21) || !s.NextRunAt.Equal(first.Add(time.Minute)) || s.FailureCount != 1 {
		t.Fatalf("RecordFailure: %+v", s)
	}
	// Success advances to the next occurrence and clears the failure.
	s.Advance(first.Add(time.Minute))
	if s.FailureCount != 0 || s.LastError != "" || s.Period() != NewDate(2026, 9, 21) {
		t.Fatalf("Advance: %+v", s)
	}
	for range MaxScheduleFailures {
		s.RecordFailure(strings.Repeat("x", 2000), first)
	}
	if s.Status != SchedulePaused || !s.NextRunAt.IsZero() || len(s.LastError) != 1000 {
		t.Fatalf("pause after failures: %v %v %d", s.Status, s.NextRunAt, len(s.LastError))
	}
	if err := s.Pause(first); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("cannot pause paused")
	}
	if err := s.Resume(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil || s.Period() != NewDate(2026, 11, 21) {
		t.Fatalf("Resume skips missed periods: %v %v", err, s.Period())
	}
	if err := s.Resume(first); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("cannot resume active")
	}
	if err := s.Pause(first); err != nil || s.Status != SchedulePaused {
		t.Fatal("Pause")
	}

	// Ending.
	e := cityDeepSchedule()
	e.Recurrence.EndDate = NewDate(2026, 8, 31)
	e.Activate(created)
	e.Advance(first)
	if e.Status != ScheduleEnded || !e.NextRunAt.IsZero() {
		t.Fatalf("end: %+v", e)
	}
	o := cityDeepSchedule()
	o.Recurrence = Recurrence{Frequency: FrequencyOnce, TimeZone: "UTC", StartDate: NewDate(2026, 1, 1)}
	o.Activate(created)
	if o.Status != ScheduleEnded {
		t.Fatal("once in the past ends immediately")
	}
}

func TestScheduleValidate(t *testing.T) {
	tests := []func(*Schedule){
		func(s *Schedule) { s.Name = "" },
		func(s *Schedule) { s.CustomerID = "" },
		func(s *Schedule) { s.Lines = nil },
		func(s *Schedule) { s.Lines[1].UnitPrice = nil },
		func(s *Schedule) { s.Lines[0].Quantity = 0 },
		func(s *Schedule) { s.TaxRate = -1 },
		func(s *Schedule) { s.EmailSubject = strings.Repeat("x", 301) },
		func(s *Schedule) { s.Recurrence.Frequency = "x" },
	}
	for i, m := range tests {
		s := cityDeepSchedule()
		m(&s)
		if err := s.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestScheduleBuildDraft(t *testing.T) {
	s := cityDeepSchedule()
	s.Notes = "Disclaimer: third party software."
	s.TaxRate = 0
	s.Activate(time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	products := map[string]Product{"p-users": {ID: "p-users", SKU: "U", Name: "Users", Description: "Stock system usage for 14 concurrent users.", Prices: map[CurrencyCode]int64{"ZAR": 19294}}}
	content, err := s.BuildDraft(cityDeep(), products)
	if err != nil {
		t.Fatal(err)
	}
	if content.IssueDate != NewDate(2026, 8, 21) || content.Notes != s.Notes {
		t.Fatalf("content = %+v", content)
	}
	if content.Lines[0].UnitPrice != 19294 || content.Lines[0].Description != "Stock system usage for 14 concurrent users." {
		t.Fatalf("line 0 = %+v", content.Lines[0])
	}
	if content.Lines[1].Description != "Alchamex integration - monthly subscription (August 2026)" {
		t.Fatalf("line 1 = %q", content.Lines[1].Description)
	}
	if got := ComputeTotals(content.Lines, 0).Total; got != 325116 {
		t.Fatalf("total = %d", got)
	}

	usd := cityDeep()
	usd.Currency = "USD"
	if _, err := s.BuildDraft(usd, products); !errors.Is(err, ErrNoPriceForCurrency) {
		t.Fatalf("no USD price: %v", err)
	}
	if _, err := s.BuildDraft(cityDeep(), map[string]Product{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing product: %v", err)
	}
}

func TestEmailMessage(t *testing.T) {
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	m, err := NewEmailMessage("m1", "inv", []string{"a@x.example"}, nil, "Invoice", "body", time.Time{}, now, "k")
	if err != nil || !m.SendAt.Equal(now) || m.Status != EmailPending {
		t.Fatalf("New: %v %+v", err, m)
	}
	if !m.Claimable(now) {
		t.Fatal("claimable now")
	}
	if err := m.Claim(now); err != nil || m.Status != EmailSending || m.Attempts != 1 {
		t.Fatal("Claim")
	}
	if err := m.Claim(now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("double claim")
	}
	if !m.Claimable(now.Add(StaleSendingAfter)) {
		t.Fatal("stale sending reclaimable")
	}
	m.Failed("dial tcp: refused", now)
	if m.Status != EmailPending || !m.NextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("Failed: %+v", m)
	}
	if m.Claimable(now) {
		t.Fatal("backoff respected")
	}
	m.Attempts = MaxEmailAttempts
	m.Failed("again", now)
	if m.Status != EmailFailed {
		t.Fatal("gives up after max attempts")
	}
	if err := m.Retry(now); err != nil || m.Status != EmailPending || m.Attempts != 0 {
		t.Fatal("Retry")
	}
	if err := m.Cancel(now); err != nil || m.Status != EmailCancelled {
		t.Fatal("Cancel")
	}
	if err := m.Cancel(now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("cancel twice")
	}
	m.Status = EmailSending
	if err := m.Retry(now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("retry sending")
	}
	m.Succeeded(now)
	if m.Status != EmailSent || m.Claimable(now.Add(time.Hour)) {
		t.Fatal("Succeeded")
	}

	later := now.Add(48 * time.Hour)
	m2, _ := NewEmailMessage("m2", "inv", []string{"a@x.example"}, []string{"b@x.example"}, "S", "", later, now, "")
	if m2.Claimable(now) || !m2.Claimable(later) {
		t.Fatal("scheduled send")
	}

	bad := []struct {
		to      []string
		cc      []string
		subject string
		sendAt  time.Time
	}{
		{nil, nil, "S", now},
		{[]string{"nope"}, nil, "S", now},
		{[]string{"a@x.example"}, []string{"nope"}, "S", now},
		{[]string{"a@x.example"}, nil, "", now},
		{[]string{"a@x.example"}, nil, "a\r\nBcc: evil@x.example", now},
		{[]string{"a@x.example"}, nil, "S", now.AddDate(2, 0, 0)},
		{make([]string, 21), nil, "S", now},
	}
	for i, b := range bad {
		if _, err := NewEmailMessage("x", "i", b.to, b.cc, b.subject, "", b.sendAt, now, ""); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: %v", i, err)
		}
	}
	if _, err := NewEmailMessage("x", "i", []string{"a@x.example"}, nil, "S", strings.Repeat("x", 20001), now, now, ""); err == nil {
		t.Error("long body")
	}
}

func TestBackoff(t *testing.T) {
	tests := map[int]time.Duration{0: time.Minute, 1: time.Minute, 2: 2 * time.Minute, 5: 16 * time.Minute, 9: 256 * time.Minute, 10: 6 * time.Hour, 50: 6 * time.Hour}
	for n, want := range tests {
		if got := Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestTemplates(t *testing.T) {
	c := cityDeep()
	d := DefaultDraftContent(c, NewDate(2026, 8, 21))
	d.Lines = referenceLines()
	inv, _ := NewDraftInvoice("i", c, d, t0)
	_ = inv.Issue("CD/1195/26", NewDate(2026, 8, 21), BankDetails{}, t0)
	data := TemplateDataFor(inv, Currency{Code: "ZAR", Symbol: "R", MinorUnits: 2}, BusinessProfile{Name: "Example"})
	got := RenderTemplate(DefaultEmailBody, data)
	for _, want := range []string{"Acme Commercial", "CD/1195/26", "R 3,251.16", "7 September 2026", "Example"} {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q:\n%s", want, got)
		}
	}
	if s := SubjectLine("Inv {{invoice_number}}\r\nBcc: x {{unknown}}", data); s != "Inv CD/1195/26 Bcc: x {{unknown}}" {
		t.Errorf("SubjectLine = %q", s)
	}
	if d := TemplateDataFor(inv, Currency{Code: "USD", MinorUnits: 2}, BusinessProfile{}); !strings.HasPrefix(d.Total, "USD ") {
		t.Errorf("no symbol → code: %q", d.Total)
	}
}
