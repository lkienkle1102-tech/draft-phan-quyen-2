package application

import (
	"context"
	"errors"

	"example.com/phan-quyen-golang/internal/security/domain"
)

var ErrEmptyActions = errors.New("actions required")

func (e *SoftEngine) DecideAll(ctx context.Context, requests []domain.Request) ([]domain.Decision, error) {
	if len(requests) == 0 {
		return nil, ErrEmptyActions
	}
	decisions := make([]domain.Decision, 0, len(requests))
	for _, request := range requests {
		decision, err := e.Decide(ctx, request)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			return []domain.Decision{decision}, nil
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

// DecideAny only probes alternatives. Quota is consumed later for the action actually executed.
func (e *SoftEngine) DecideAny(ctx context.Context, requests []domain.Request) (domain.Decision, error) {
	if len(requests) == 0 {
		return domain.Decision{}, ErrEmptyActions
	}
	last := domain.Deny(domain.DenyPermission)
	for _, request := range requests {
		decision, err := e.Decide(ctx, request)
		if err != nil {
			return domain.Decision{}, err
		}
		if decision.Allowed {
			return decision, nil
		}
		last = decision
	}
	return last, nil
}

type Strategy[I, O any] interface {
	Execute(context.Context, I, domain.Decision) (O, error)
}
type StrategyRegistry[I, O any] struct{ values map[string]Strategy[I, O] }

func NewStrategyRegistry[I, O any](values map[string]Strategy[I, O]) *StrategyRegistry[I, O] {
	return &StrategyRegistry[I, O]{values: values}
}
func (r *StrategyRegistry[I, O]) Execute(ctx context.Context, name string, input I, decision domain.Decision) (O, error) {
	value, found := r.values[name]
	if !found {
		var zero O
		return zero, errors.New("strategy not registered")
	}
	return value.Execute(ctx, input, decision)
}
