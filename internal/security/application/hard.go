package application

import (
	"context"
	"slices"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type HardEngine struct{ facts BusinessFacts }

func NewHardEngine(facts BusinessFacts) *HardEngine { return &HardEngine{facts: facts} }

func (e *HardEngine) Evaluate(ctx context.Context, request domain.Request, contract domain.EndpointContract) (domain.Request, domain.Decision, error) {
	if request.Operation != contract.Operation || !actorAllowed(request.Actor, contract.ActorConstraint) {
		return request, domain.Deny(domain.DenyHard), nil
	}
	if contract.RequireTenant && request.TenantID == "" {
		return request, domain.Deny(domain.DenyTenant), nil
	}
	if contract.RequireResourceTenant && request.Primary.TenantID == "" {
		return request, domain.Deny(domain.DenyTenant), nil
	}
	member, err := e.membershipAllowed(ctx, request, contract)
	if err != nil {
		return request, domain.Deny(domain.DenyPolicy), err
	}
	if !member {
		resolved, denial, resolveErr := e.resolveExternal(ctx, request, contract)
		if resolveErr != nil || !denial.Allowed {
			return resolved, denial, resolveErr
		}
		request = resolved
	}
	if !resourceAllowed(request, contract) || !assuranceAllowed(request, contract) {
		return request, domain.Deny(domain.DenyHard), nil
	}
	return e.evaluateTenant(request, contract)
}

func (e *HardEngine) resolveExternal(ctx context.Context, request domain.Request, contract domain.EndpointContract) (domain.Request, domain.Decision, error) {
	if contract.TenantAccess != domain.ExplicitGrant || request.Actor.Type != domain.ActorUser {
		return request, domain.Deny(domain.DenyMembership), nil
	}
	external, supported := e.facts.(ExternalFacts)
	if !supported {
		return request, domain.Deny(domain.DenyGrant), nil
	}
	access, err := external.ResolveExternalAccess(ctx, request.Actor, request.Primary, request.Operation, request.Now)
	if err != nil {
		return request, domain.Deny(domain.DenyPolicy), err
	}
	if access == nil {
		return request, domain.Deny(domain.DenyGrant), nil
	}
	request.ExternalAccess = access
	return request, domain.Allow(request), nil
}

func (e *HardEngine) membershipAllowed(ctx context.Context, request domain.Request, contract domain.EndpointContract) (bool, error) {
	if !contract.RequireOrganizationMembership {
		return true, nil
	}
	if request.Actor.Type != domain.ActorUser || request.Subject.Type != domain.SubjectOrganization {
		return false, nil
	}
	return e.facts.IsMember(ctx, request.Actor, request.Subject)
}

func actorAllowed(actor domain.Actor, wanted domain.ActorConstraint) bool {
	return wanted == "" || wanted == domain.AnyActor || wanted == domain.UserOnly && actor.Type == domain.ActorUser || wanted == domain.MachineOnly && actor.Type == domain.ActorMachine
}

func resourceAllowed(request domain.Request, contract domain.EndpointContract) bool {
	if contract.DenySelfEscalation && request.Actor.ID == request.Primary.ID {
		return false
	}
	if !contract.ProtectSystemResources {
		return true
	}
	if request.Primary.System {
		return false
	}
	for _, related := range request.Related {
		if related.System {
			return false
		}
	}
	return true
}

func assuranceAllowed(request domain.Request, contract domain.EndpointContract) bool {
	if !contract.RequireMFA {
		return true
	}
	age := request.Now.Sub(request.Actor.AuthTime)
	return slices.Contains(request.Actor.AMR, "mfa") && !request.Actor.AuthTime.IsZero() && age >= 0 && age <= contract.MaxAuthAge
}

func (e *HardEngine) evaluateTenant(request domain.Request, contract domain.EndpointContract) (domain.Request, domain.Decision, error) {
	owner := request.Primary.TenantID
	if owner == "" && !contract.RequireResourceTenant {
		return request, domain.Allow(request), nil
	}
	if owner == request.TenantID && relatedAllowed(request, contract, owner) {
		return request, domain.Allow(request), nil
	}
	return request, domain.Deny(domain.DenyTenant), nil
}

func relatedAllowed(request domain.Request, contract domain.EndpointContract, owner string) bool {
	if !contract.RequireRelatedAuthorization {
		return true
	}
	for _, related := range request.Related {
		if related.TenantID != "" && related.TenantID != owner {
			return false
		}
	}
	return true
}
