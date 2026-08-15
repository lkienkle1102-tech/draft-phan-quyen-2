// Package delivery exposes the authenticated identity snapshot over HTTP.
package delivery

import (
	"context"
	"net/http"
	"time"

	identityapp "example.com/phan-quyen-golang/internal/identity/application"
	identity "example.com/phan-quyen-golang/internal/identity/domain"
	securityapp "example.com/phan-quyen-golang/internal/security/application"
	security "example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

type MeLoader struct{}

func (MeLoader) Load(_ context.Context, input securityapp.EndpointInput) (securityapp.LoadedResources, error) {
	return securityapp.LoadedResources{Primary: security.Resource{Type: "identity", ID: input.Actor.ID, OwnerID: input.Actor.ID}}, nil
}

type MeIntent struct{}

func (MeIntent) Resolve(context.Context, securityapp.EndpointInput) (security.Operation, error) {
	return security.Operation{ResourceType: "identity", Action: "read_self"}, nil
}

type Handler struct{ service *identityapp.Service }

func NewHandler(service *identityapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Get(c *gin.Context) {
	actor, ok := securityapp.ActorFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "authorization_context_missing"})
		return
	}
	snapshot, err := h.service.GetMe(c.Request.Context(), actor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "identity_snapshot_unavailable"})
		return
	}
	c.JSON(http.StatusOK, mapSnapshot(snapshot))
}

func RegisterRoutes(router *gin.RouterGroup, guard func(security.EndpointContract) gin.HandlerFunc, handler *Handler) {
	contract := security.EndpointContract{Operation: security.Operation{ResourceType: "identity", Action: "read_self"}, ActorConstraint: security.UserOnly, TenantAccess: security.StrictIsolation}
	router.GET("/me", guard(contract), handler.Get)
}

type sourceResponse struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Effect string `json:"effect"`
}

type factResponse struct {
	Key     string           `json:"key"`
	Effect  string           `json:"effect"`
	Sources []sourceResponse `json:"sources"`
}

type planResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Effect      string     `json:"effect"`
	Status      string     `json:"status"`
	ValidFrom   time.Time  `json:"valid_from"`
	ValidUntil  *time.Time `json:"valid_until,omitempty"`
	PeriodStart *time.Time `json:"current_period_start,omitempty"`
	PeriodEnd   *time.Time `json:"current_period_end,omitempty"`
}

type quotaResponse struct {
	Key         string           `json:"key"`
	Effect      string           `json:"effect"`
	Limit       *int64           `json:"limit"`
	Used        int64            `json:"used"`
	Reserved    int64            `json:"reserved"`
	Remaining   *int64           `json:"remaining"`
	Unlimited   bool             `json:"unlimited"`
	PeriodStart *time.Time       `json:"period_start,omitempty"`
	PeriodEnd   *time.Time       `json:"period_end,omitempty"`
	Sources     []sourceResponse `json:"sources"`
}

type entitlementsResponse struct {
	Roles       []factResponse  `json:"roles"`
	Groups      []factResponse  `json:"groups"`
	Permissions []factResponse  `json:"permissions"`
	Features    []factResponse  `json:"features"`
	Plan        *planResponse   `json:"plan"`
	Quotas      []quotaResponse `json:"quotas"`
}

type organizationResponse struct {
	Organization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organization"`
	Membership struct {
		ID       string    `json:"id"`
		JoinedAt time.Time `json:"joined_at"`
	} `json:"membership"`
	Entitlements entitlementsResponse `json:"entitlements"`
}

type externalGrantResponse struct {
	ID                string `json:"id"`
	OwnerOrganization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"owner_organization"`
	Target struct {
		Type           string `json:"type"`
		UserID         string `json:"user_id,omitempty"`
		OrganizationID string `json:"organization_id,omitempty"`
		MembershipID   string `json:"membership_id,omitempty"`
	} `json:"target"`
	Resource struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
	} `json:"resource"`
	Action       string               `json:"action"`
	Effect       string               `json:"effect"`
	ValidFrom    time.Time            `json:"valid_from"`
	ValidUntil   *time.Time           `json:"valid_until,omitempty"`
	Entitlements entitlementsResponse `json:"entitlements"`
}

type snapshotResponse struct {
	GeneratedAt time.Time `json:"generated_at"`
	Identity    struct {
		ID                  string `json:"id"`
		ActorType           string `json:"actor_type"`
		TokenOrganizationID string `json:"token_organization_id,omitempty"`
		Authentication      struct {
			AMR      []string  `json:"amr"`
			AuthTime time.Time `json:"auth_time"`
		} `json:"authentication"`
	} `json:"identity"`
	Personal struct {
		Subject struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"subject"`
		Roles       []factResponse  `json:"roles"`
		Groups      []factResponse  `json:"groups"`
		Permissions []factResponse  `json:"permissions"`
		Features    []factResponse  `json:"features"`
		Plan        *planResponse   `json:"plan"`
		Quotas      []quotaResponse `json:"quotas"`
	} `json:"personal"`
	Organizations  []organizationResponse  `json:"organizations"`
	ExternalGrants []externalGrantResponse `json:"external_grants"`
}

func mapSnapshot(value identity.Snapshot) snapshotResponse {
	result := snapshotResponse{GeneratedAt: value.GeneratedAt, Organizations: make([]organizationResponse, 0, len(value.Organizations)), ExternalGrants: make([]externalGrantResponse, 0, len(value.ExternalGrants))}
	result.Identity.ID, result.Identity.ActorType = value.Identity.ID, value.Identity.ActorType
	result.Identity.TokenOrganizationID = value.Identity.TokenOrganizationID
	result.Identity.Authentication.AMR = value.Identity.AMR
	result.Identity.Authentication.AuthTime = value.Identity.AuthTime
	result.Personal.Subject.Type, result.Personal.Subject.ID = value.Personal.Subject.Type, value.Personal.Subject.ID
	personal := mapEntitlements(value.Personal.Entitlements)
	result.Personal.Roles, result.Personal.Groups = personal.Roles, personal.Groups
	result.Personal.Permissions, result.Personal.Features = personal.Permissions, personal.Features
	result.Personal.Plan, result.Personal.Quotas = personal.Plan, personal.Quotas
	for _, item := range value.Organizations {
		result.Organizations = append(result.Organizations, mapOrganization(item))
	}
	for _, item := range value.ExternalGrants {
		result.ExternalGrants = append(result.ExternalGrants, mapExternalGrant(item))
	}
	return result
}

func mapEntitlements(value identity.Entitlements) entitlementsResponse {
	result := entitlementsResponse{Roles: mapFacts(value.Roles), Groups: mapFacts(value.Groups), Permissions: mapFacts(value.Permissions), Features: mapFacts(value.Features), Quotas: make([]quotaResponse, 0, len(value.Quotas))}
	if value.Plan != nil {
		result.Plan = mapPlan(*value.Plan)
	}
	for _, quota := range value.Quotas {
		result.Quotas = append(result.Quotas, mapQuota(quota))
	}
	return result
}

func mapFacts(values []identity.EffectiveFact) []factResponse {
	result := make([]factResponse, 0, len(values))
	for _, value := range values {
		item := factResponse{Key: value.Key, Effect: value.Effect, Sources: mapSources(value.Sources)}
		result = append(result, item)
	}
	return result
}

func mapSources(values []identity.Source) []sourceResponse {
	result := make([]sourceResponse, 0, len(values))
	for _, value := range values {
		result = append(result, sourceResponse{Type: value.Type, ID: value.ID, Effect: value.Effect})
	}
	return result
}

func mapPlan(value identity.Plan) *planResponse {
	result := &planResponse{ID: value.ID, Name: value.Name, Effect: value.Effect, Status: value.Status, ValidFrom: value.ValidFrom, ValidUntil: value.ValidUntil}
	if !value.PeriodStart.IsZero() {
		result.PeriodStart = &value.PeriodStart
	}
	if !value.PeriodEnd.IsZero() {
		result.PeriodEnd = &value.PeriodEnd
	}
	return result
}

func mapQuota(value identity.Quota) quotaResponse {
	result := quotaResponse{Key: value.Key, Effect: value.Effect, Limit: value.Limit, Used: value.Used, Reserved: value.Reserved, Remaining: value.Remaining, Unlimited: value.Unlimited, PeriodEnd: value.PeriodEnd, Sources: mapSources(value.Sources)}
	if !value.PeriodStart.IsZero() {
		result.PeriodStart = &value.PeriodStart
	}
	return result
}

func mapOrganization(value identity.OrganizationScope) organizationResponse {
	var result organizationResponse
	result.Organization.ID, result.Organization.Name = value.Organization.ID, value.Organization.Name
	result.Membership.ID, result.Membership.JoinedAt = value.Membership.ID, value.Membership.JoinedAt
	result.Entitlements = mapEntitlements(value.Entitlements)
	return result
}

func mapExternalGrant(value identity.ExternalGrant) externalGrantResponse {
	var result externalGrantResponse
	result.ID, result.Action, result.Effect = value.ID, value.Action, value.Effect
	result.OwnerOrganization.ID, result.OwnerOrganization.Name = value.OwnerOrganizationID, value.OwnerOrganizationName
	result.Target.Type, result.Target.UserID = value.Target.Type, value.Target.UserID
	result.Target.OrganizationID, result.Target.MembershipID = value.Target.OrganizationID, value.Target.MembershipID
	result.Resource.Type, result.Resource.ID = value.ResourceType, value.ResourceID
	result.ValidFrom, result.ValidUntil = value.ValidFrom, value.ValidUntil
	result.Entitlements = mapEntitlements(value.Entitlements)
	return result
}
