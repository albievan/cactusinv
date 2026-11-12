package app

import (
	"context"
	"errors"
	"sort"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// UpcomingInvoice is the next invoice an invoice schedule will raise and has not raised
// yet, active or paused (M32). Invoice is a sample (domain.Schedule.SampleInvoice); when it
// cannot be built (an inactive customer or product, a missing price or bank account)
// Invoice is nil and Problem says why, as the run itself would fail.
type UpcomingInvoice struct {
	Schedule domain.Schedule // placed on the pending occurrence (domain.Schedule.Upcoming)
	Customer string          // the customer's name
	Currency domain.CurrencyCode
	Period   domain.Date // the occurrence's nominal date (its {month})
	RunDate  domain.Date // the date it is raised on and will carry
	Invoice  *domain.Invoice
	Problem  string
}

// ListUpcomingInvoices lists, for every invoice schedule (active or paused) the user may
// run, the next invoice it will raise, soonest first. Nothing is stored.
func (s *Service) ListUpcomingInvoices(ctx context.Context) ([]UpcomingInvoice, error) {
	r := s.tx.Repos()
	var out []UpcomingInvoice
	for _, st := range []domain.ScheduleStatus{domain.ScheduleActive, domain.SchedulePaused} {
		page := ports.Page{Limit: 200}.Normalize()
		for {
			ss, total, err := r.Schedules.List(ctx, ports.ScheduleFilter{Status: st, Page: page})
			if err != nil {
				return nil, wrap("app.ListUpcomingInvoices", err)
			}
			for i := range ss {
				if checkScheduleAccess(ctx, ss[i]) != nil {
					continue
				}
				u, ok, err := s.upcoming(ctx, r, &ss[i])
				if err != nil {
					return nil, wrap("app.ListUpcomingInvoices", err)
				}
				if ok {
					out = append(out, u)
				}
			}
			page.Offset += len(ss)
			if len(ss) == 0 || page.Offset >= total {
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Schedule.NextOccurrence, out[j].Schedule.NextOccurrence
		if !a.Equal(b) {
			return a.Before(b)
		}
		return out[i].Schedule.Name < out[j].Schedule.Name
	})
	return out, nil
}

// ScheduleSamplePDF renders the next invoice a schedule will raise as a sample: numbered
// SAMPLE and watermarked "SAMPLE - DO NOT EMAIL TO CUSTOMER" on every page. It is built in
// memory: no invoice, number or run is stored and nothing is e-mailed. The viewing is
// audited, as an invoice PDF's is.
func (s *Service) ScheduleSamplePDF(ctx context.Context, id string) ([]byte, string, error) {
	r := s.tx.Repos()
	sc, err := r.Schedules.Get(ctx, id)
	if err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	if err := checkScheduleAccess(ctx, *sc); err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	u, ok, err := s.upcoming(ctx, r, sc)
	if err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	if !ok {
		return nil, "", wrap("app.ScheduleSamplePDF", domain.ErrInvalidTransition)
	}
	if u.Invoice == nil {
		return nil, "", wrap("app.ScheduleSamplePDF", &domain.ValidationError{Field: "schedule", Reason: u.Problem})
	}
	b, err := s.business(ctx, r)
	if err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	cur, err := r.Currencies.Get(ctx, u.Invoice.Currency)
	if err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	c, err := r.Customers.Get(ctx, u.Invoice.CustomerID)
	if err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	pdf, err := s.renderer.RenderPDF(ctx, ports.InvoiceDocument{Invoice: u.Invoice, Business: b, Currency: cur, Disclaimer: domain.Disclaimer(c, b)})
	if err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	if err := s.auditNow(ctx, "schedule.sample_viewed", "schedule", sc.ID, sc.Name+" "+u.Period.String()); err != nil {
		return nil, "", wrap("app.ScheduleSamplePDF", err)
	}
	return pdf, "Sample_" + safeFileName(sc.Name) + "_" + u.Period.String() + ".pdf", nil
}

// upcoming places sc on its next invoice and builds that invoice as a sample. ok is false
// when the schedule raises no invoice (an e-mail schedule, or nothing left to run).
func (s *Service) upcoming(ctx context.Context, r ports.Repos, sc *domain.Schedule) (UpcomingInvoice, bool, error) {
	billed, err := r.Schedules.BilledUntil(ctx, sc.ID)
	if err != nil {
		return UpcomingInvoice{}, false, err
	}
	up, ok := sc.Upcoming(s.now(), billed)
	if !ok {
		return UpcomingInvoice{}, false, nil
	}
	u := UpcomingInvoice{Schedule: up, Period: up.Period(), RunDate: up.RunDate()}
	c, err := r.Customers.Get(ctx, up.CustomerID)
	if err != nil {
		return UpcomingInvoice{}, false, err
	}
	u.Customer, u.Currency = c.Name, c.Currency
	inv, err := s.sampleInvoice(ctx, r, &up, &c)
	if err != nil {
		var ve *domain.ValidationError
		if !errors.As(err, &ve) && !errors.Is(err, domain.ErrInactive) && !errors.Is(err, domain.ErrNotFound) {
			return UpcomingInvoice{}, false, err
		}
		u.Problem = problemText(err)
		return u, true, nil
	}
	u.Invoice = inv
	return u, true, nil
}

// sampleInvoice builds what runOccurrence would raise for sc's current period, dated its
// run date (as the runner dates it), without storing anything.
func (s *Service) sampleInvoice(ctx context.Context, r ports.Repos, sc *domain.Schedule, c *domain.Customer) (*domain.Invoice, error) {
	if _, err := activeCurrency(ctx, r, c.Currency); err != nil {
		return nil, err
	}
	var ids []string
	for _, l := range sc.Lines {
		if l.ProductID != "" {
			ids = append(ids, l.ProductID)
		}
	}
	products, err := r.Products.GetMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	b, err := s.business(ctx, r)
	if err != nil {
		return nil, err
	}
	accounts, err := r.Banks.List(ctx, c.Currency)
	if err != nil {
		return nil, err
	}
	acct, err := domain.ResolveBankAccount(*c, accounts)
	if err != nil {
		return nil, err
	}
	return sc.SampleInvoice(*c, products, b.VATRate, sc.RunDate(), acct.Details(), s.now())
}

// problemText says why a schedule's next invoice cannot be built, as its run would fail.
func problemText(err error) string {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		return ve.Error()
	case errors.Is(err, domain.ErrInactive) && err.Error() == domain.ErrInactive.Error():
		return "the customer or its currency is inactive"
	case errors.Is(err, domain.ErrInactive):
		return err.Error()
	default:
		return "a product, price or bank account the invoice needs is missing"
	}
}
