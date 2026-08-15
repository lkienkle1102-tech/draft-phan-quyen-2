package application

import (
	"context"
	"strconv"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type Authorizer interface {
	Authorize(context.Context, domain.Request, domain.EndpointContract) (domain.Request, domain.Decision, error)
}

type Engine struct {
	hard        *HardEngine
	permissions PermissionEnforcer
	facts       BusinessFacts
}

func NewEngine(hard *HardEngine, permissions PermissionEnforcer, facts BusinessFacts) *Engine {
	return &Engine{hard: hard, permissions: permissions, facts: facts}
}

func (e *Engine) Authorize(ctx context.Context, request domain.Request, contract domain.EndpointContract) (domain.Request, domain.Decision, error) {
	resolved, decision, err := e.hard.Evaluate(ctx, request, contract)
	if err != nil || !decision.Allowed {
		return resolved, decision, err
	}
	decision, err = e.decide(ctx, resolved)
	return resolved, decision, err
}

func (e *Engine) decide(ctx context.Context, request domain.Request) (domain.Decision, error) {
	requirement := request.Requirement
	if requirement.RequireSelf && request.Primary.ID != request.Actor.ID {
		return domain.Deny(domain.DenyPermission), nil
	}
	if requirement.RequirePermission {
		allowed, err := e.permissionAllowed(ctx, request)
		if err != nil {
			return domain.Deny(domain.DenyPolicy), err
		}
		if !allowed {
			return domain.Deny(domain.DenyPermission), nil
		}
	}
	if requirement.FeatureKey != "" {
		allowed, err := e.featureAllowed(ctx, request, requirement.FeatureKey)
		if err != nil {
			return domain.Deny(domain.DenyPolicy), err
		}
		if !allowed {
			return domain.Deny(domain.DenyFeature), nil
		}
	}
	decision := domain.Allow(request)
	if requirement.QuotaKey != "" {
		allowed, cost, err := e.quotaAllowed(ctx, request, requirement.QuotaKey, requirement.QuotaCost)
		if err != nil {
			return domain.Deny(domain.DenyPolicy), err
		}
		if !allowed {
			return domain.Deny(domain.DenyQuota), nil
		}
		decision.QuotaCosts = []domain.QuotaCost{cost}
	}
	return applyBehavior(request, decision), nil
}

func (e *Engine) permissionAllowed(ctx context.Context, request domain.Request) (bool, error) {
	if request.ExternalAccess != nil {
		external, ok := e.facts.(ExternalFacts)
		if !ok {
			return false, nil
		}
		return external.ExternalPermission(ctx, *request.ExternalAccess, request.Operation)
	}
	return e.permissions.Enforce(ctx, request.Actor, request.Subject, request.Operation)
}

func (e *Engine) featureAllowed(ctx context.Context, request domain.Request, key string) (bool, error) {
	if request.ExternalAccess != nil {
		external, ok := e.facts.(ExternalFacts)
		if !ok {
			return false, nil
		}
		return external.ExternalFeature(ctx, *request.ExternalAccess, key)
	}
	return e.facts.HasFeature(ctx, request.Subject, key)
}

func (e *Engine) quotaAllowed(ctx context.Context, request domain.Request, key string, amount int64) (bool, domain.QuotaCost, error) {
	if request.ExternalAccess != nil {
		external, ok := e.facts.(ExternalFacts)
		if !ok {
			return false, domain.QuotaCost{}, nil
		}
		allowed, err := external.ExternalQuota(ctx, *request.ExternalAccess, key, amount)
		cost := domain.QuotaCost{Subject: domain.Subject{Type: domain.SubjectOrganization, ID: request.ExternalAccess.OwnerOrganizationID}, ExternalGrantIDs: append([]string(nil), request.ExternalAccess.GrantIDs...), QuotaKey: key, Cost: amount}
		return allowed, cost, err
	}
	allowed, err := e.facts.QuotaAvailable(ctx, request.Subject, key, amount)
	return allowed, domain.QuotaCost{Subject: request.Subject, QuotaKey: key, Cost: amount}, err
}

func applyBehavior(request domain.Request, decision domain.Decision) domain.Decision {
	behavior := request.Requirement.Behavior
	if behavior == nil {
		return decision
	}
	value, err := strconv.ParseInt(request.Primary.Attributes[behavior.Attribute], 10, 64)
	if err != nil || value > behavior.Maximum {
		return decision
	}
	decision.Strategy = behavior.Strategy
	decision.Parameters = cloneValues(behavior.Parameters)
	decision.Obligations = append([]domain.Obligation(nil), behavior.Obligations...)
	return decision
}

func cloneValues(values map[string]domain.Value) map[string]domain.Value {
	result := make(map[string]domain.Value, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

var _ Authorizer = (*Engine)(nil)
