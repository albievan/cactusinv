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
	// Paused by failures: resuming keeps the failed period and runs it now (later missed
	// periods are caught up one by one, as after downtime).
	if !s.PausedByFailures() {
		t.Fatal("paused by failures")
	}
	resumed := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Resume(resumed); err != nil || s.Period() != NewDate(2026, 9, 21) || !s.NextRunAt.Equal(resumed) || s.FailureCount != 0 {
		t.Fatalf("Resume after failures keeps the failed period: %v %v %v", err, s.Period(), s.NextRunAt)
	}
	if err := s.Resume(first); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("cannot resume active")
	}
	if err := s.Pause(first); err != nil || s.Status != SchedulePaused || s.PausedByFailures() {
		t.Fatal("Pause")
	}
	// Paused by the user: resuming skips the periods missed while paused.
	if err := s.Resume(resumed); err != nil || s.Period() != NewDate(2026, 11, 21) {
		t.Fatalf("Resume after a user pause skips missed periods: %v %v", err, s.Period())
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
		func(s *Schedule) { neg := BasisPoints(-1); s.TaxRate = &neg },
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
	s.Activate(time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	products := map[string]Product{"p-users": {ID: "p-users", SKU: "U", Name: "Users", Description: "Stock system usage for 14 concurrent users.", Prices: map[CurrencyCode]int64{"ZAR": 19294}, Active: true}}
	content, err := s.BuildDraft(cityDeep(), products, 0, s.RunDate())
	if err != nil {
		t.Fatal(err)
	}
	if content.TaxRate != 0 {
		t.Fatal("no VAT by default")
	}
	if c15, _ := s.BuildDraft(cityDeep(), products, 1500, s.RunDate()); c15.TaxRate != 1500 {
		t.Fatal("business default VAT applies")
	}
	own := BasisPoints(0)
	s.TaxRate = &own
	if c0, _ := s.BuildDraft(cityDeep(), products, 1500, s.RunDate()); c0.TaxRate != 0 {
		t.Fatal("schedule's own VAT (0) overrides the default")
	}
	s.TaxRate = nil
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
	if _, err := s.BuildDraft(usd, products, 0, s.RunDate()); !errors.Is(err, ErrNoPriceForCurrency) {
		t.Fatalf("no USD price: %v", err)
	}
	if _, err := s.BuildDraft(cityDeep(), map[string]Product{}, 0, s.RunDate()); !errors.Is(err, ErrNotFound) {
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

func TestEmailSchedules(t *testing.T) {
	s := cityDeepSchedule()
	s.Kind = ScheduleEmail
	if err := s.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatal("scope required")
	}
	s.EmailScope = EmailUnsent
	if err := s.Validate(); err != nil || s.Lines != nil || !s.AutoSend {
		t.Fatalf("email schedule: %v %+v", err, s)
	}
	s.Kind = "sms"
	if err := s.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatal("bad kind")
	}
	inv := &Invoice{Status: InvoiceIssued}
	if !EmailUnsent.Selects(inv) || !EmailOutstanding.Selects(inv) {
		t.Fatal("issued, never sent")
	}
	inv.Status, inv.SentAt = InvoiceSent, t0
	if EmailUnsent.Selects(inv) || !EmailOutstanding.Selects(inv) {
		t.Fatal("sent")
	}
	inv.Status = InvoiceOverdue
	if !EmailOutstanding.Selects(inv) {
		t.Fatal("overdue is outstanding")
	}
	for _, st := range []InvoiceStatus{InvoiceDraft, InvoicePaid, InvoiceVoid} {
		inv.Status = st
		if EmailOutstanding.Selects(inv) || EmailUnsent.Selects(inv) || EmailScope("x").Selects(inv) {
			t.Errorf("%s must not be sent", st)
		}
	}
	plain := cityDeepSchedule()
	if err := plain.Validate(); err != nil || plain.Kind != ScheduleInvoice {
		t.Fatal("default kind")
	}
}

func TestScheduleLineProductView(t *testing.T) {
	products := map[string]Product{
		"p1": {ID: "p1", Name: "Stock system users", Description: "Stock system usage", Active: true, Prices: map[CurrencyCode]int64{"ZAR": 19294}},
		"p2": {ID: "p2", Name: "Old support plan", Active: false, Prices: map[CurrencyCode]int64{"ZAR": 55000}},
		"p3": {ID: "p3", Name: "Dollar thing", Active: true, Prices: map[CurrencyCode]int64{"USD": 1000}},
	}
	price := func(v int64) *int64 { return &v }
	tests := []struct {
		name string
		line ScheduleLine
		cur  CurrencyCode
		want ScheduleLineProduct
	}{
		{"active, priced; the stored description is not the name", ScheduleLine{ProductID: "p1", Description: ""}, "ZAR", ScheduleLineProduct{Name: "Stock system users", State: ProductActive, UnitPrice: price(19294)}},
		{"stored description ignored", ScheduleLine{ProductID: "p1", Description: "Users for {month}"}, "ZAR", ScheduleLineProduct{Name: "Stock system users", State: ProductActive, UnitPrice: price(19294)}},
		{"fixed price does not hide the product's price", ScheduleLine{ProductID: "p1", UnitPrice: price(1)}, "ZAR", ScheduleLineProduct{Name: "Stock system users", State: ProductActive, UnitPrice: price(19294)}},
		{"inactive keeps its name", ScheduleLine{ProductID: "p2"}, "ZAR", ScheduleLineProduct{Name: "Old support plan", State: ProductInactive, UnitPrice: price(55000)}},
		{"no price in the currency", ScheduleLine{ProductID: "p3"}, "ZAR", ScheduleLineProduct{Name: "Dollar thing", State: ProductActive}},
		{"missing", ScheduleLine{ProductID: "gone"}, "ZAR", ScheduleLineProduct{State: ProductMissing}},
		{"no product", ScheduleLine{}, "ZAR", ScheduleLineProduct{State: ProductMissing}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.line.ProductView(products, tc.cur)
			if got.Name != tc.want.Name || got.State != tc.want.State || (got.UnitPrice == nil) != (tc.want.UnitPrice == nil) ||
				(got.UnitPrice != nil && *got.UnitPrice != *tc.want.UnitPrice) {
				t.Fatalf("got %+v (price %v), want %+v", got, got.UnitPrice, tc.want)
			}
		})
	}
}

func TestScheduleLastRunResult(t *testing.T) {
	ran := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		s    Schedule
		want RunOutcome
	}{
		{"never ran", Schedule{}, RunNever},
		{"ran", Schedule{LastRunAt: ran}, RunOK},
		{"failed before any success", Schedule{FailureCount: 1, LastError: "no bank account"}, RunFailed},
		{"failed since the last success", Schedule{LastRunAt: ran, FailureCount: 2}, RunFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.LastRunResult(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	// The lifecycle sets it: a failure, then a successful run.
	s := Schedule{Status: ScheduleActive, Recurrence: Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 1, TimeZone: "UTC", StartDate: NewDate(2026, 10, 1)}}
	s.Activate(ran)
	s.RecordFailure("boom", ran)
	if s.LastRunResult() != RunFailed {
		t.Fatal("after a failure")
	}
	s.Advance(ran.Add(time.Hour))
	if s.LastRunResult() != RunOK {
		t.Fatal("after a success")
	}
}

func TestWeekendRule(t *testing.T) {
	utc := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }
	monthly := func(day int, rule WeekendRule, start Date, tz string) Recurrence {
		r := Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: day, SendHour: 9, TimeZone: tz, StartDate: start, Weekend: rule}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		return r
	}
	pf := WeekendPreviousFriday
	tests := []struct {
		name         string
		r            Recurrence
		after        time.Time
		nominal, run time.Time
	}{
		{"first day on a Sunday moves into the previous month", monthly(1, pf, NewDate(2026, 1, 1), "UTC"), utc(2026, 1, 15, 0), utc(2026, 2, 1, 9), utc(2026, 1, 30, 9)},
		{"between the moved run and its day, still that occurrence", monthly(1, pf, NewDate(2026, 1, 1), "UTC"), utc(2026, 1, 31, 10), utc(2026, 2, 1, 9), utc(2026, 1, 30, 9)},
		{"after its day, the next one", monthly(1, pf, NewDate(2026, 1, 1), "UTC"), utc(2026, 2, 1, 10), utc(2026, 3, 1, 9), utc(2026, 2, 27, 9)},
		{"first day on a Saturday moves into the previous year", monthly(1, pf, NewDate(2027, 1, 1), "UTC"), utc(2027, 12, 15, 0), utc(2028, 1, 1, 9), utc(2027, 12, 31, 9)},
		{"first day on a Friday stays", monthly(1, pf, NewDate(2026, 12, 1), "UTC"), utc(2026, 12, 15, 0), utc(2027, 1, 1, 9), utc(2027, 1, 1, 9)},
		{"31st in February is the 28th, a Saturday", monthly(31, pf, NewDate(2026, 1, 1), "UTC"), utc(2026, 2, 1, 0), utc(2026, 2, 28, 9), utc(2026, 2, 27, 9)},
		{"last day of February in a leap year, a Tuesday", monthly(0, pf, NewDate(2028, 1, 1), "UTC"), utc(2028, 2, 1, 0), utc(2028, 2, 29, 9), utc(2028, 2, 29, 9)},
		{"last day of February 2027, a Sunday", monthly(0, pf, NewDate(2027, 1, 1), "UTC"), utc(2027, 2, 1, 0), utc(2027, 2, 28, 9), utc(2027, 2, 26, 9)},
		{"last day of October 2026, a Saturday", monthly(0, pf, NewDate(2026, 1, 1), "UTC"), utc(2026, 10, 1, 0), utc(2026, 10, 31, 9), utc(2026, 10, 30, 9)},
		{"rule none runs on the weekend", monthly(1, WeekendNone, NewDate(2026, 1, 1), "UTC"), utc(2026, 1, 15, 0), utc(2026, 2, 1, 9), utc(2026, 2, 1, 9)},
		// Sun 29 Mar 2026 09:00 BST (08:00 UTC) moves to Fri 27 Mar 09:00 GMT (09:00 UTC).
		{"moved across a daylight-saving change", monthly(29, pf, NewDate(2026, 1, 1), "Europe/London"), utc(2026, 3, 1, 0), utc(2026, 3, 29, 8), utc(2026, 3, 27, 9)},
		{"in the business zone", monthly(1, pf, NewDate(2026, 1, 1), "Africa/Johannesburg"), utc(2026, 10, 20, 0), utc(2026, 11, 1, 7), utc(2026, 10, 30, 7)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nominal, ok := tc.r.NextAfter(tc.after)
			if !ok || !nominal.Equal(tc.nominal) {
				t.Fatalf("NextAfter = %v %v, want %v", nominal.UTC(), ok, tc.nominal)
			}
			if got := tc.r.RunTime(nominal); !got.Equal(tc.run) {
				t.Fatalf("RunTime = %v, want %v", got.UTC(), tc.run)
			}
		})
	}

	// Validate: weekly and once never move; an unknown rule is refused; "" is none.
	weekly := Recurrence{Frequency: FrequencyWeekly, Weekday: time.Saturday, TimeZone: "UTC", StartDate: NewDate(2026, 10, 1), Weekend: pf}
	if err := weekly.Validate(); err != nil || weekly.Weekend != WeekendNone {
		t.Fatalf("weekly: %v %q", err, weekly.Weekend)
	}
	if next, _ := weekly.NextAfter(utc(2026, 10, 1, 0)); weekly.RunTime(next).Weekday() != time.Saturday {
		t.Fatalf("weekly moved: %v", next)
	}
	bad := Recurrence{Frequency: FrequencyMonthly, TimeZone: "UTC", StartDate: NewDate(2026, 10, 1), Weekend: "monday-after"}
	if err := bad.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown rule: %v", err)
	}
	plain := Recurrence{Frequency: FrequencyMonthly, TimeZone: "UTC", StartDate: NewDate(2026, 10, 1)}
	if err := plain.Validate(); err != nil || plain.Weekend != WeekendNone {
		t.Fatalf("empty rule: %v %q", err, plain.Weekend)
	}
	// DefaultWeekendRule: day-of-month recurrences only, never overriding a choice.
	for _, tc := range []struct {
		r    Recurrence
		want WeekendRule
	}{
		{Recurrence{Frequency: FrequencyMonthly}, pf},
		{Recurrence{Frequency: FrequencyQuarterly}, pf},
		{Recurrence{Frequency: FrequencyYearly}, pf},
		{Recurrence{Frequency: FrequencyMonthly, Weekend: WeekendNone}, WeekendNone},
		{Recurrence{Frequency: FrequencyWeekly}, ""},
		{Recurrence{Frequency: FrequencyOnce}, ""},
	} {
		r := tc.r
		r.DefaultWeekendRule()
		if r.Weekend != tc.want {
			t.Errorf("%s/%q → %q, want %q", tc.r.Frequency, tc.r.Weekend, r.Weekend, tc.want)
		}
	}
}

// TestWeekendRuleProperties: a moved run is never later than its nominal time, at most two
// days (and a daylight-saving hour) earlier, on a weekday, and in order.
func TestWeekendRuleProperties(t *testing.T) {
	const weekendLead = 2*24*time.Hour + 2*time.Hour
	zones := []string{"UTC", "Africa/Johannesburg", "Europe/London", "America/New_York", "Australia/Sydney"}
	freqs := []Frequency{FrequencyMonthly, FrequencyQuarterly, FrequencyYearly}
	seed := uint64(20261016)
	next := func(n int) int { // a small deterministic generator (no shared state)
		seed = seed*6364136223846793005 + 1442695040888963407
		return int((seed >> 33) % uint64(n))
	}
	for range 300 {
		r := Recurrence{
			Frequency: freqs[next(len(freqs))], Interval: 1 + next(3), DayOfMonth: next(32), SendHour: next(24), SendMinute: next(60),
			TimeZone: zones[next(len(zones))], StartDate: NewDate(2020+next(20), time.Month(1+next(12)), 1+next(28)), Weekend: WeekendPreviousFriday,
		}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		loc, _ := time.LoadLocation(r.TimeZone)
		nominal := r.StartDate.Time(time.UTC).AddDate(0, 0, -1)
		var prevRun time.Time
		for range 40 {
			n, ok := r.NextAfter(nominal)
			if !ok {
				t.Fatalf("%+v ended", r)
			}
			run := r.RunTime(n)
			if run.After(n) || n.Sub(run) > weekendLead {
				t.Fatalf("%+v: nominal %v run %v", r, n, run)
			}
			if wd := run.In(loc).Weekday(); wd == time.Saturday || wd == time.Sunday {
				t.Fatalf("%+v: runs on %v %v", r, wd, run)
			}
			if !prevRun.IsZero() && !run.After(prevRun) {
				t.Fatalf("%+v: run %v not after %v", r, run, prevRun)
			}
			prevRun, nominal = run, n
		}
	}
}

func TestScheduleWeekendRun(t *testing.T) {
	r := Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: 1, SendHour: 9, TimeZone: "UTC", StartDate: NewDate(2026, 1, 1), Weekend: WeekendPreviousFriday}
	s := Schedule{Recurrence: r, Lines: []ScheduleLine{{ProductID: "p", Quantity: 1000}}}
	s.Activate(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	if s.Period() != NewDate(2026, 11, 1) || s.RunDate() != NewDate(2026, 10, 30) || !s.NextRunAt.Equal(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("period %v run %v at %v", s.Period(), s.RunDate(), s.NextRunAt)
	}
	// Activated after the moved run of November but before its nominal day (a new schedule,
	// a resume, an edit or a rule switched on): November is not skipped, it runs now.
	sat := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	late := Schedule{Recurrence: r}
	late.Activate(sat)
	if late.Period() != NewDate(2026, 11, 1) || !late.NextRunAt.Equal(sat) || !late.IsDue(sat) {
		t.Fatalf("late: period %v next run %v", late.Period(), late.NextRunAt)
	}
	paused := Schedule{Recurrence: r, Status: ScheduleActive}
	paused.Activate(time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)) // Thursday
	_ = paused.Pause(time.Date(2026, 10, 29, 13, 0, 0, 0, time.UTC))
	if err := paused.Resume(sat); err != nil || paused.Period() != NewDate(2026, 11, 1) || !paused.NextRunAt.Equal(sat) {
		t.Fatalf("resumed on Saturday: %v period %v at %v", err, paused.Period(), paused.NextRunAt)
	}
	// Issue date: the scheduled run date, never after today.
	for _, tc := range []struct{ today, want Date }{
		{NewDate(2026, 10, 30), NewDate(2026, 10, 30)}, // on time
		{NewDate(2026, 12, 2), NewDate(2026, 10, 30)},  // caught up late: dated its run day
		{NewDate(2026, 10, 20), NewDate(2026, 10, 20)}, // Run now ahead of time: today
	} {
		if got := s.IssueDate(tc.today); got != tc.want {
			t.Errorf("IssueDate(%v) = %v, want %v", tc.today, got, tc.want)
		}
	}
	if up := s.UpcomingRuns(3); len(up) != 3 || up[0].Day() != 30 || up[1].Day() != 1 || up[2] != time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC) {
		t.Fatalf("upcoming = %v", up)
	}
	if (&Schedule{}).UpcomingRuns(3) != nil {
		t.Fatal("ended schedule has no upcoming runs")
	}
	c := Customer{Name: "Acme", Currency: "ZAR"}
	products := map[string]Product{"p": {ID: "p", Name: "Support", Description: "Support for {period}", Prices: map[CurrencyCode]int64{"ZAR": 100}, Active: true}}
	d, err := s.BuildDraft(c, products, 0, s.IssueDate(NewDate(2026, 10, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if d.IssueDate != NewDate(2026, 10, 30) || d.Lines[0].Description != "Support for November 2026" {
		t.Fatalf("draft issued %v: %q", d.IssueDate, d.Lines[0].Description)
	}
	retired := products["p"]
	retired.Active = false
	if _, err := s.BuildDraft(c, map[string]Product{"p": retired}, 0, NewDate(2026, 10, 30)); !errors.Is(err, ErrInactive) || !strings.Contains(err.Error(), `"Support" is no longer available`) {
		t.Fatalf("inactive product: %v", err)
	}
	s.Advance(s.NextRunAt)
	if s.Period() != NewDate(2026, 12, 1) || !s.NextRunAt.Equal(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("advanced to %v at %v", s.Period(), s.NextRunAt)
	}
}

func TestPeriodKeyAndStart(t *testing.T) {
	d := NewDate(2026, 11, 15)
	tests := []struct {
		f     Frequency
		key   string
		start Date
	}{
		{FrequencyMonthly, "2026-11", NewDate(2026, 11, 1)},
		{FrequencyQuarterly, "2026-Q4", NewDate(2026, 10, 1)},
		{FrequencyYearly, "2026", NewDate(2026, 1, 1)},
		{FrequencyWeekly, "2026-11-15", d},
		{FrequencyOnce, "2026-11-15", d},
	}
	for _, tc := range tests {
		r := Recurrence{Frequency: tc.f}
		if got := r.PeriodKey(d); got != tc.key {
			t.Errorf("%s key = %q, want %q", tc.f, got, tc.key)
		}
		if got := r.PeriodStart(d); got != tc.start {
			t.Errorf("%s start = %v, want %v", tc.f, got, tc.start)
		}
	}
	if (Recurrence{Frequency: FrequencyQuarterly}).PeriodKey(NewDate(2026, 3, 31)) != "2026-Q1" {
		t.Fatal("Q1")
	}
}

// TestActivateFrom: an unrun occurrence whose time has passed is kept when the schedule is
// (re)activated, even when the edit moves its day within the period; a future one is not
// pulled forward.
func TestActivateFrom(t *testing.T) {
	monthly := func(day int) Recurrence {
		return Recurrence{Frequency: FrequencyMonthly, Interval: 1, DayOfMonth: day, SendHour: 9, TimeZone: "UTC", StartDate: NewDate(2026, 1, 1), Weekend: WeekendNone}
	}
	failed := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		r       Recurrence
		pending time.Time
		period  Date
		runAt   time.Time
	}{
		{"same day, failed this morning", monthly(15), failed, NewDate(2026, 10, 15), now},
		{"day moved earlier in the month", monthly(10), failed, NewDate(2026, 10, 10), now},
		{"day moved later in the month", monthly(20), failed, NewDate(2026, 10, 20), time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)},
		{"overdue by months (downtime)", monthly(15), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC), NewDate(2026, 8, 15), now},
		{"pending still ahead: not pulled forward", monthly(1), time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), NewDate(2026, 11, 1), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)},
		{"nothing pending", monthly(15), time.Time{}, NewDate(2026, 11, 15), time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := Schedule{Recurrence: tc.r, FailureCount: 2, LastError: "x"}
			s.ActivateFrom(tc.pending, now)
			if s.Status != ScheduleActive || s.Period() != tc.period || !s.NextRunAt.Equal(tc.runAt) || s.FailureCount != 0 {
				t.Fatalf("period %v run %v (%+v)", s.Period(), s.NextRunAt, s)
			}
		})
	}
}
