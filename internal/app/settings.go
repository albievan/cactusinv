package app

import (
	"context"
	"fmt"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// BusinessSettings is the business profile plus the next invoice sequence value.
type BusinessSettings struct {
	Profile      domain.BusinessProfile
	NextSequence int64
}

// GetBusiness returns the business profile (defaults before it is first saved).
func (s *Service) GetBusiness(ctx context.Context) (BusinessSettings, error) {
	r := s.tx.Repos()
	b, err := s.business(ctx, r)
	if err != nil {
		return BusinessSettings{}, wrap("app.GetBusiness", err)
	}
	next, err := r.Sequences.Peek(ctx)
	if err != nil {
		return BusinessSettings{}, wrap("app.GetBusiness", err)
	}
	return BusinessSettings{Profile: b, NextSequence: next}, nil
}

// UpdateBusiness saves the profile. nextSequence (0 = unchanged) may only move forward,
// so issued numbers can never be reused.
func (s *Service) UpdateBusiness(ctx context.Context, p domain.BusinessProfile, nextSequence int64) (BusinessSettings, error) {
	if err := p.Validate(); err != nil {
		return BusinessSettings{}, wrap("app.UpdateBusiness", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.Business.Save(ctx, p); err != nil {
			return err
		}
		if err := s.audit(ctx, r, "settings.business_updated", "settings", "business", p.Name); err != nil {
			return err
		}
		if nextSequence == 0 {
			return nil
		}
		cur, err := r.Sequences.Peek(ctx)
		if err != nil {
			return err
		}
		if nextSequence < cur {
			return &domain.ValidationError{Field: "nextSequence", Reason: "may only increase (numbers already used)"}
		}
		if err := r.Sequences.Set(ctx, nextSequence); err != nil {
			return err
		}
		return s.audit(ctx, r, "settings.next_number_set", "settings", "business", fmt.Sprintf("next invoice sequence %d", nextSequence))
	})
	if err != nil {
		return BusinessSettings{}, wrap("app.UpdateBusiness", err)
	}
	return s.GetBusiness(ctx)
}

// Today is the current date in the business time zone.
func (s *Service) Today(ctx context.Context) (domain.Date, error) {
	b, err := s.business(ctx, s.tx.Repos())
	if err != nil {
		return domain.Date{}, wrap("app.Today", err)
	}
	return s.today(b), nil
}
