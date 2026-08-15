package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

var ErrEndpointConfiguration = errors.New("invalid endpoint configuration")
var ErrOrganizationRequired = errors.New("organization context required")

type EndpointResolver struct {
	endpoints EndpointRepository
	loaders   map[string]ResourceLoader
	intents   map[string]IntentResolver
}

func NewEndpointResolver(repo EndpointRepository, loaders map[string]ResourceLoader, intents map[string]IntentResolver) *EndpointResolver {
	return &EndpointResolver{endpoints: repo, loaders: loaders, intents: intents}
}

func (r *EndpointResolver) Resolve(ctx context.Context, method, route string, input EndpointInput) (domain.Request, error) {
	binding, err := r.endpoints.FindEndpoint(ctx, method, route)
	if err != nil {
		return domain.Request{}, err
	}
	loader, exists := r.loaders[binding.Loader]
	if !exists {
		return domain.Request{}, ErrEndpointConfiguration
	}
	intent, exists := r.intents[binding.Intent]
	if !exists {
		return domain.Request{}, ErrEndpointConfiguration
	}
	resources, err := loader.Load(ctx, input)
	if err != nil {
		return domain.Request{}, err
	}
	operation, err := intent.Resolve(ctx, input)
	if err != nil || operation != binding.Operation {
		return domain.Request{}, ErrEndpointConfiguration
	}
	subject, err := resolveSubject(binding, input.Actor, resources)
	if err != nil {
		return domain.Request{}, err
	}
	return domain.Request{Method: method, RouteTemplate: route, EndpointBindingID: binding.ID, Actor: input.Actor, Subject: subject, TenantID: subject.ID, Primary: resources.Primary, Related: resources.Related, Operation: operation, Now: time.Now().UTC(), PolicyID: binding.PolicyID, PolicyVersion: binding.PolicyVersion, ScopeMode: binding.ScopeMode, Requirement: binding.Requirement}, nil
}

func resolveSubject(binding EndpointBinding, actor domain.Actor, resources LoadedResources) (domain.Subject, error) {
	user := domain.Subject{Type: domain.SubjectUser, ID: actor.ID}
	switch binding.ScopeMode {
	case domain.ScopeUser:
		return user, nil
	case domain.ScopeOrganization:
		organizationID := resources.TenantID
		if organizationID == "" {
			organizationID = resources.Primary.TenantID
		}
		if organizationID == "" {
			return domain.Subject{}, ErrOrganizationRequired
		}
		return domain.Subject{Type: domain.SubjectOrganization, ID: organizationID}, nil
	default:
		return domain.Subject{}, ErrEndpointConfiguration
	}
}
