// Package application implements entitlement lifecycle use cases.
package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/entitlement/domain"
)

var ErrInvalidEntitlement = errors.New("invalid entitlement command")

type Repository interface {
	ApplySubscription(context.Context, domain.Subscription) error
	RenewSubscription(context.Context, string, time.Time, time.Time) error
	CancelSubscription(context.Context, string, time.Time) error
	ApplyFeatureOverride(context.Context, domain.Override) error
	ApplyQuotaOverride(context.Context, domain.Override) error
}

type Manager interface {
	Activate(context.Context, domain.Subscription) error
	ChangePlan(context.Context, domain.Subscription) error
	Renew(context.Context, string, time.Time, time.Time) error
	Cancel(context.Context, string, time.Time) error
	OverrideFeature(context.Context, domain.Override) error
	OverrideQuota(context.Context, domain.Override) error
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Activate(ctx context.Context, subscription domain.Subscription) error {
	if err := subscription.Validate(); err != nil {
		return errors.Join(ErrInvalidEntitlement, err)
	}
	return s.repository.ApplySubscription(ctx, subscription)
}

func (s *Service) ChangePlan(ctx context.Context, subscription domain.Subscription) error {
	return s.Activate(ctx, subscription)
}

func (s *Service) Renew(ctx context.Context, id string, start, end time.Time) error {
	if id == "" || start.IsZero() || !end.After(start) {
		return ErrInvalidEntitlement
	}
	return s.repository.RenewSubscription(ctx, id, start, end)
}

func (s *Service) Cancel(ctx context.Context, id string, at time.Time) error {
	if id == "" || at.IsZero() {
		return ErrInvalidEntitlement
	}
	return s.repository.CancelSubscription(ctx, id, at)
}

func (s *Service) OverrideFeature(ctx context.Context, override domain.Override) error {
	if err := override.Validate(); err != nil {
		return errors.Join(ErrInvalidEntitlement, err)
	}
	return s.repository.ApplyFeatureOverride(ctx, override)
}

func (s *Service) OverrideQuota(ctx context.Context, override domain.Override) error {
	if err := override.Validate(); err != nil || override.PeriodStart == nil || override.PeriodEnd == nil || !override.PeriodEnd.After(*override.PeriodStart) {
		return errors.Join(ErrInvalidEntitlement, err)
	}
	return s.repository.ApplyQuotaOverride(ctx, override)
}

var _ Manager = (*Service)(nil)
