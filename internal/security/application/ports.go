// Package application implements authorization orchestration.
package application

import (
	"context"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type EndpointBinding struct {
	ID, Method, Route, Loader, Intent string
	Operation                         domain.Operation
	PolicyID                          string
	PolicyVersion                     int64
	ScopeMode                         domain.ScopeMode
}

type EndpointInput struct {
	Params map[string]string
	Body   []byte
	Actor  domain.Actor
}
type LoadedResources struct {
	Subject  domain.Subject
	TenantID string
	Primary  domain.Resource
	Related  []domain.Resource
}
type EndpointRepository interface {
	FindEndpoint(context.Context, string, string) (EndpointBinding, error)
}
type ResourceLoader interface {
	Load(context.Context, EndpointInput) (LoadedResources, error)
}
type IntentResolver interface {
	Resolve(context.Context, EndpointInput) (domain.Operation, error)
}
type PolicyRepository interface {
	LoadPolicy(context.Context, string, int64) (domain.Policy, error)
}

type Facts interface {
	ActorActive(context.Context, domain.Actor) (bool, error)
	HasPermission(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error)
	HasRole(context.Context, domain.Actor, domain.Subject, string) (bool, error)
	InGroup(context.Context, domain.Actor, domain.Subject, string) (bool, error)
	IsMember(context.Context, domain.Actor, domain.Subject) (bool, error)
	HasClientGrant(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error)
	HasConsent(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error)
	HasFeature(context.Context, domain.Subject, string) (bool, error)
	HasPlan(context.Context, domain.Subject, string) (bool, error)
	QuotaAvailable(context.Context, domain.Subject, string, int64) (bool, error)
	FindGrant(context.Context, domain.GrantRequest) (domain.OrganizationGrant, bool, error)
	DelegatedFeature(context.Context, string, string) (bool, error)
	DelegatedQuota(context.Context, string, string, int64) (bool, error)
}

type ExternalFacts interface {
	ResolveExternalAccess(context.Context, domain.Actor, domain.Resource, domain.Operation, time.Time) (*domain.ExternalAccess, error)
	ExternalPermission(context.Context, domain.ExternalAccess, domain.Operation) (bool, error)
	ExternalRole(context.Context, domain.ExternalAccess, string) (bool, error)
	ExternalGroup(context.Context, domain.ExternalAccess, string) (bool, error)
	ExternalFeature(context.Context, domain.ExternalAccess, string) (bool, error)
	ExternalPlan(context.Context, domain.ExternalAccess, string) (bool, error)
	ExternalQuota(context.Context, domain.ExternalAccess, string, int64) (bool, error)
}
