package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ActorCustomerLink is who answers a quote through the link in its e-mail (M41).
var ActorCustomerLink = Actor{ID: "customer:link", Name: "Customer (e-mail link)"}

// RenewQuote makes a new draft quote from an expired or declined one (M41): the same
// customer, lines, prices, notes and terms, dated today and valid for Settings → Quotes'
// days, with its own number. It is sent like any other quote; the old one stays as it is.
func (s *Service) RenewQuote(ctx context.Context, id string) (*domain.Quote, error) {
	newID := s.ids.NewID()
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		old, err := r.Quotes.Get(ctx, id)
		if err != nil {
			return err
		}
		b, err := s.business(ctx, r)
		if err != nil {
			return err
		}
		qs, err := s.quoteSettings(ctx, r)
		if err != nil {
			return err
		}
		today := s.today(b)
		content, err := old.RenewalContent(today, qs.ValidDays, today)
		if err != nil {
			return err
		}
		to := domain.QuoteFor{CustomerID: old.CustomerID, Currency: old.Currency}
		if to.CustomerID == "" { // still an ad-hoc customer
			to.AdhocCustomerID = old.AdhocCustomerID
		}
		seq, err := r.QuoteSequences.Next(ctx)
		if err != nil {
			return err
		}
		number, err := domain.FormatQuoteNumber(qs.NumberFormat, seq, today)
		if err != nil {
			return err
		}
		q, err := domain.NewQuote(newID, number, to, content, s.now())
		if err != nil {
			return err
		}
		q.RenewedFrom = old.ID
		if err := r.Quotes.Create(ctx, q); err != nil {
			return err
		}
		if err := s.quoteEvent(ctx, r, newID, "created", number+" for "+q.BillTo.Name+", renewing "+old.Number); err != nil {
			return err
		}
		return s.quoteEvent(ctx, r, old.ID, "renewed", "as "+number)
	})
	if err != nil {
		return nil, wrap("app.RenewQuote", err)
	}
	q, err := s.tx.Repos().Quotes.Get(ctx, newID)
	return q, wrap("app.RenewQuote", err)
}

// QuoteResponseView is what the customer's page shows for a link in a quote e-mail.
type QuoteResponseView struct {
	Quote    *domain.Quote
	Business domain.BusinessProfile
	Stage    domain.QuoteStage
	Expired  bool
}

// quoteForToken finds the quote a response token belongs to. An unknown, malformed or
// deleted quote's token is ErrNotFound, never anything that tells them apart.
func (s *Service) quoteForToken(ctx context.Context, r ports.Repos, token string) (*domain.Quote, error) {
	if len(token) != 43 {
		return nil, domain.ErrNotFound
	}
	id, err := r.Quotes.QuoteIDByResponseToken(ctx, HashToken(token))
	if err != nil {
		return nil, err
	}
	return r.Quotes.Get(ctx, id)
}

func (s *Service) quoteResponseView(ctx context.Context, r ports.Repos, token string) (QuoteResponseView, error) {
	q, err := s.quoteForToken(ctx, r, token)
	if err != nil {
		return QuoteResponseView{}, err
	}
	b, err := s.business(ctx, r)
	if err != nil {
		return QuoteResponseView{}, err
	}
	today := s.today(b)
	return QuoteResponseView{Quote: q, Business: b, Stage: q.Stage(today), Expired: q.Expired(today)}, nil
}

// GetQuoteResponse is the customer's page for a quote e-mail link: no sign-in, the token is
// the only key (M41). Opening it changes nothing.
func (s *Service) GetQuoteResponse(ctx context.Context, token string) (QuoteResponseView, error) {
	v, err := s.quoteResponseView(ctx, s.tx.Repos(), token)
	return v, wrap("app.GetQuoteResponse", err)
}

// RespondToQuote records the customer's answer through a quote e-mail link (M41): the
// quote is accepted or declined by the customer. An expired quote cannot be accepted. The
// same answer again is no change; a quote already answered otherwise, or converted, is not
// changed by the customer.
func (s *Service) RespondToQuote(ctx context.Context, token string, answer domain.QuoteAnswer, reason string) (QuoteResponseView, error) {
	ctx = WithActor(ctx, ActorCustomerLink)
	var v QuoteResponseView
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		var err error
		if v, err = s.quoteResponseView(ctx, r, token); err != nil {
			return err
		}
		q := v.Quote
		switch {
		case answer == domain.QuoteAnswerAccept && q.Status == domain.QuoteAccepted,
			answer == domain.QuoteAnswerDecline && q.Status == domain.QuoteDeclined:
			return nil // answered so already
		case q.Status != domain.QuoteDraft && q.Status != domain.QuoteSent:
			return &domain.ValidationError{Field: "answer", Reason: "this quote was already " + string(v.Stage) + "; contact " + v.Business.Name + " to change it"}
		}
		event, detail := "accepted", q.Number+" by the customer (e-mail link)"
		if answer == domain.QuoteAnswerAccept {
			err = q.Accept(s.today(v.Business), domain.AnsweredByCustomer, s.now())
		} else {
			event = "declined"
			err = q.Decline(reason, domain.AnsweredByCustomer, s.now())
			if q.DeclineReason != "" {
				detail += ": " + q.DeclineReason
			}
		}
		if err != nil {
			return err
		}
		if err := r.Quotes.Update(ctx, q); err != nil {
			return err
		}
		return s.quoteEvent(ctx, r, q.ID, event, detail)
	})
	if err != nil {
		return QuoteResponseView{}, wrap("app.RespondToQuote", err)
	}
	v, err = s.quoteResponseView(ctx, s.tx.Repos(), token)
	return v, wrap("app.RespondToQuote", err)
}

// QuoteResponsePDF is the quote's PDF for the customer's page (the one their e-mail
// carried, as the quote is now). Viewing it is audited as the customer's.
func (s *Service) QuoteResponsePDF(ctx context.Context, token string) ([]byte, string, error) {
	q, err := s.quoteForToken(ctx, s.tx.Repos(), token)
	if err != nil {
		return nil, "", wrap("app.QuoteResponsePDF", err)
	}
	return s.QuotePDF(WithActor(ctx, ActorCustomerLink), q.ID)
}
