package domain

import (
	"strconv"
	"strings"
	"time"
)

// ScheduleStatus is a schedule's state.
type ScheduleStatus string

// Schedule statuses.
const (
	ScheduleActive ScheduleStatus = "active"
	SchedulePaused ScheduleStatus = "paused"
	ScheduleEnded  ScheduleStatus = "ended"
)

// MaxScheduleFailures pauses a schedule after this many consecutive failed runs.
const MaxScheduleFailures = 5

// ScheduleLine is a line template. A nil UnitPrice means "use the product's price in the
// customer's currency at run time".
type ScheduleLine struct {
	ProductID   string
	Description string // may contain {month}, {year}, {period}
	Quantity    Quantity
	UnitPrice   *int64
	Discount    BasisPoints
}

// Schedule automatically issues (and optionally e-mails) an invoice for a customer on a
// recurrence (docs/ARCHITECTURE.md §3.7).
type Schedule struct {
	ID             string
	CustomerID     string
	Name           string
	Status         ScheduleStatus
	Recurrence     Recurrence
	Lines          []ScheduleLine
	Header         InvoiceHeader
	Notes          string // empty → customer default notes
	TaxRate        BasisPoints
	AutoSend       bool
	EmailSubject   string // empty → business default
	EmailBody      string // empty → business default
	NextOccurrence time.Time
	NextRunAt      time.Time
	LastRunAt      time.Time
	FailureCount   int
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

// Validate checks a schedule's invariants.
func (s *Schedule) Validate() error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || len(s.Name) > 120 {
		return invalid("name", "is required (max 120 characters)")
	}
	if s.CustomerID == "" {
		return invalid("customerId", "is required")
	}
	if err := s.Recurrence.Validate(); err != nil {
		return err
	}
	if len(s.Lines) == 0 || len(s.Lines) > MaxInvoiceLines {
		return invalid("lines", "between 1 and 200 lines are required")
	}
	for i := range s.Lines {
		l := &s.Lines[i]
		l.Description = strings.TrimSpace(l.Description)
		if l.UnitPrice == nil && l.ProductID == "" {
			return invalid("lines.unitPrice", "is required when no product is selected")
		}
		price := int64(0)
		if l.UnitPrice != nil {
			price = *l.UnitPrice
		}
		probe := Line{Description: l.Description, Quantity: l.Quantity, UnitPrice: price, Discount: l.Discount}
		if probe.Description == "" && l.ProductID != "" {
			probe.Description = "product"
		}
		if err := probe.validate(); err != nil {
			return err
		}
	}
	if s.TaxRate < 0 || s.TaxRate > 10000 {
		return invalid("taxRate", "must be between 0 and 100 percent")
	}
	if len(s.EmailSubject) > 300 || len(s.EmailBody) > 10000 || len(s.Notes) > 4000 {
		return invalid("email", "subject, body or notes too long")
	}
	return nil
}

// Activate (re)computes the next occurrence strictly after now and marks the schedule
// active, or ended if nothing is left.
func (s *Schedule) Activate(now time.Time) {
	s.FailureCount = 0
	s.LastError = ""
	next, ok := s.Recurrence.NextAfter(now)
	if !ok {
		s.end(now)
		return
	}
	s.Status = ScheduleActive
	s.NextOccurrence = next
	s.NextRunAt = next
	s.UpdatedAt = now
}

// Pause stops an active schedule.
func (s *Schedule) Pause(now time.Time) error {
	if s.Status != ScheduleActive {
		return ErrInvalidTransition
	}
	s.Status = SchedulePaused
	s.NextRunAt = time.Time{}
	s.UpdatedAt = now
	return nil
}

// Resume restarts a paused schedule from the next occurrence after now (missed periods
// while paused are skipped deliberately).
func (s *Schedule) Resume(now time.Time) error {
	if s.Status != SchedulePaused {
		return ErrInvalidTransition
	}
	s.Activate(now)
	return nil
}

// IsDue reports whether the runner should process the schedule at now.
func (s *Schedule) IsDue(now time.Time) bool {
	return s.Status == ScheduleActive && !s.NextRunAt.IsZero() && !s.NextRunAt.After(now)
}

// Period is the schedule-local date of the occurrence being processed.
func (s *Schedule) Period() Date { return s.Recurrence.LocalDate(s.NextOccurrence) }

// Advance records a successful (or already-done) run and moves to the next occurrence.
// Missed occurrences are caught up one per run so no billing period is skipped.
func (s *Schedule) Advance(now time.Time) {
	s.LastRunAt = now
	s.FailureCount = 0
	s.LastError = ""
	next, ok := s.Recurrence.NextAfter(s.NextOccurrence)
	if !ok {
		s.end(now)
		return
	}
	s.NextOccurrence = next
	s.NextRunAt = next
	s.UpdatedAt = now
}

// RecordFailure retries the same occurrence with backoff, pausing after
// MaxScheduleFailures consecutive failures.
func (s *Schedule) RecordFailure(reason string, now time.Time) {
	s.FailureCount++
	s.LastError = truncate(reason, 1000)
	s.UpdatedAt = now
	if s.FailureCount >= MaxScheduleFailures {
		s.Status = SchedulePaused
		s.NextRunAt = time.Time{}
		return
	}
	s.NextRunAt = now.Add(Backoff(s.FailureCount))
}

func (s *Schedule) end(now time.Time) {
	s.Status = ScheduleEnded
	s.NextRunAt = time.Time{}
	s.NextOccurrence = time.Time{}
	s.UpdatedAt = now
}

// BuildDraft turns the schedule's template into draft content for its current period,
// pricing product lines from the customer's currency price list.
func (s *Schedule) BuildDraft(c Customer, products map[string]Product) (DraftContent, error) {
	period := s.Period()
	content := DefaultDraftContent(c, period)
	content.Header = s.Header
	content.TaxRate = s.TaxRate
	if s.Notes != "" {
		content.Notes = s.Notes
	}
	periodReplacer := strings.NewReplacer(
		"{month}", period.Month.String(),
		"{year}", strconv.Itoa(period.Year),
		"{period}", period.Month.String()+" "+strconv.Itoa(period.Year),
	)
	for _, tl := range s.Lines {
		l := Line{ProductID: tl.ProductID, Quantity: tl.Quantity, Discount: tl.Discount, Description: tl.Description}
		if tl.ProductID != "" {
			p, ok := products[tl.ProductID]
			if !ok {
				return DraftContent{}, ErrNotFound
			}
			if l.Description == "" {
				l.Description = p.LineDescription()
			}
			if tl.UnitPrice == nil {
				price, err := p.PriceIn(c.Currency)
				if err != nil {
					return DraftContent{}, err
				}
				l.UnitPrice = price.Amount
			}
		}
		if tl.UnitPrice != nil {
			l.UnitPrice = *tl.UnitPrice
		}
		l.Description = periodReplacer.Replace(l.Description)
		content.Lines = append(content.Lines, l)
	}
	return content, nil
}

// ScheduleRun records one processed occurrence; (ScheduleID, Period) is unique.
type ScheduleRun struct {
	ScheduleID string
	Period     Date
	InvoiceID  string
	At         time.Time
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
