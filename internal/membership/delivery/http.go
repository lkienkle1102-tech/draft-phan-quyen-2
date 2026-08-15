// Package delivery exposes organization membership HTTP adapters.
package delivery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	membershipapp "example.com/phan-quyen-golang/internal/membership/application"
	membershipdomain "example.com/phan-quyen-golang/internal/membership/domain"
	securityapp "example.com/phan-quyen-golang/internal/security/application"
	security "example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

type ApplyLoader struct{}

func (ApplyLoader) Load(_ context.Context, input securityapp.EndpointInput) (securityapp.LoadedResources, error) {
	return securityapp.LoadedResources{Primary: security.Resource{Type: "organization", ID: input.Params["organizationID"]}}, nil
}

type ApplyIntent struct{}

func (ApplyIntent) Resolve(context.Context, securityapp.EndpointInput) (security.Operation, error) {
	return security.Operation{ResourceType: "organization_membership", Action: "apply"}, nil
}

type ReviewLoader struct{ database *sql.DB }

func NewReviewLoader(database *sql.DB) *ReviewLoader { return &ReviewLoader{database: database} }
func (l *ReviewLoader) Load(ctx context.Context, input securityapp.EndpointInput) (securityapp.LoadedResources, error) {
	var resource security.Resource
	err := l.database.QueryRowContext(ctx, `SELECT id,organization_id FROM organization_membership_applications WHERE id=? AND organization_id=?`, input.Params["applicationID"], input.Params["organizationID"]).Scan(&resource.ID, &resource.TenantID)
	resource.Type = "organization_membership_application"
	return securityapp.LoadedResources{Primary: resource}, err
}

type ReviewIntent struct{}

func (ReviewIntent) Resolve(context.Context, securityapp.EndpointInput) (security.Operation, error) {
	return security.Operation{ResourceType: "organization_membership", Action: "review"}, nil
}

type InviteLoader struct{}

func (InviteLoader) Load(_ context.Context, input securityapp.EndpointInput) (securityapp.LoadedResources, error) {
	organizationID := input.Params["organizationID"]
	resource := security.Resource{Type: "organization", ID: organizationID, TenantID: organizationID}
	return securityapp.LoadedResources{TenantID: organizationID, Primary: resource}, nil
}

type InviteIntent struct{}

func (InviteIntent) Resolve(context.Context, securityapp.EndpointInput) (security.Operation, error) {
	return security.Operation{ResourceType: "organization_membership", Action: "invite"}, nil
}

type AcceptLoader struct{}

func (AcceptLoader) Load(_ context.Context, input securityapp.EndpointInput) (securityapp.LoadedResources, error) {
	return securityapp.LoadedResources{Primary: security.Resource{Type: "user", ID: input.Actor.ID, OwnerID: input.Actor.ID}}, nil
}

type AcceptIntent struct{}

func (AcceptIntent) Resolve(context.Context, securityapp.EndpointInput) (security.Operation, error) {
	return security.Operation{ResourceType: "organization_membership", Action: "accept"}, nil
}

type Handler struct {
	service     *membershipapp.Service
	invitations *membershipapp.InvitationService
}

func NewHandler(service *membershipapp.Service, invitations *membershipapp.InvitationService) *Handler {
	return &Handler{service: service, invitations: invitations}
}

type RouteDependencies struct {
	Guard   func(security.EndpointContract) gin.HandlerFunc
	Handler *Handler
}

func RegisterRoutes(router *gin.RouterGroup, dependencies RouteDependencies) {
	applyContract := security.EndpointContract{Operation: security.Operation{ResourceType: "organization_membership", Action: "apply"}, ActorConstraint: security.UserOnly, ProtectSystemResources: true, RequireMFA: true, MaxAuthAge: 30 * time.Minute}
	reviewContract := security.EndpointContract{Operation: security.Operation{ResourceType: "organization_membership", Action: "review"}, ActorConstraint: security.UserOnly, TenantAccess: security.StrictIsolation, RequireTenant: true, RequireResourceTenant: true, RequireOrganizationMembership: true, ProtectSystemResources: true, RequireMFA: true, MaxAuthAge: 30 * time.Minute}
	inviteContract := security.EndpointContract{Operation: security.Operation{ResourceType: "organization_membership", Action: "invite"}, ActorConstraint: security.UserOnly, TenantAccess: security.StrictIsolation, RequireTenant: true, RequireResourceTenant: true, RequireOrganizationMembership: true, ProtectSystemResources: true, RequireMFA: true, MaxAuthAge: 30 * time.Minute}
	acceptContract := security.EndpointContract{Operation: security.Operation{ResourceType: "organization_membership", Action: "accept"}, ActorConstraint: security.UserOnly, DenySelfEscalation: false, RequireMFA: true, MaxAuthAge: 30 * time.Minute}
	router.POST("/organizations/:organizationID/membership-applications", dependencies.Guard(applyContract), dependencies.Handler.Apply)
	router.POST("/organizations/:organizationID/membership-applications/:applicationID/review", dependencies.Guard(reviewContract), dependencies.Handler.Review)
	router.POST("/organizations/:organizationID/membership-invitations", dependencies.Guard(inviteContract), dependencies.Handler.Invite)
	router.POST("/membership-invitations/accept", dependencies.Guard(acceptContract), dependencies.Handler.AcceptInvitation)
}

func (h *Handler) Apply(c *gin.Context) {
	actor, actorOK := securityapp.ActorFromContext(c.Request.Context())
	decision, decisionOK := securityapp.DecisionFromContext(c.Request.Context())
	if !actorOK || !decisionOK {
		c.JSON(http.StatusForbidden, gin.H{"error": "authorization_context_missing"})
		return
	}
	id, err := randomID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "id_generation_failed"})
		return
	}
	if err := h.service.Apply(c.Request.Context(), id, c.Param("organizationID"), actor.ID, decision); err != nil {
		status := http.StatusConflict
		if errors.Is(err, security.ErrQuotaExceeded) {
			status = http.StatusTooManyRequests
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"id": id, "status": "pending"})
}

type reviewBody struct {
	Approve bool `json:"approve"`
}

func (h *Handler) Review(c *gin.Context) {
	var body reviewBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, actorOK := securityapp.ActorFromContext(c.Request.Context())
	decision, decisionOK := securityapp.DecisionFromContext(c.Request.Context())
	if !actorOK || !decisionOK {
		c.JSON(http.StatusForbidden, gin.H{"error": "authorization_context_missing"})
		return
	}
	if err := h.service.Review(c.Request.Context(), c.Param("applicationID"), c.Param("organizationID"), actor.ID, body.Approve, decision); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

type inviteBody struct {
	UserID     string   `json:"user_id" binding:"required"`
	RoleIDs    []string `json:"role_ids" binding:"required"`
	ValidUntil string   `json:"valid_until" binding:"required"`
}

func (h *Handler) Invite(c *gin.Context) {
	var body inviteBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, ok := securityapp.ActorFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "authorization_context_missing"})
		return
	}
	validUntil, err := time.Parse(time.RFC3339, body.ValidUntil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_valid_until"})
		return
	}
	id, token, err := invitationCredentials()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "id_generation_failed"})
		return
	}
	hash := sha256.Sum256([]byte(token))
	invitation := membershipdomain.Invitation{ID: id, OrganizationID: c.Param("organizationID"), UserID: body.UserID, TokenHash: hex.EncodeToString(hash[:]), InvitedBy: actor.ID, RoleIDs: body.RoleIDs, ValidFrom: time.Now().UTC(), ValidUntil: validUntil}
	if err := h.invitations.Invite(c.Request.Context(), invitation); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "token": token, "status": "pending"})
}

type acceptInvitationBody struct {
	Token string `json:"token" binding:"required"`
}

func (h *Handler) AcceptInvitation(c *gin.Context) {
	var body acceptInvitationBody
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil || body.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, ok := securityapp.ActorFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "authorization_context_missing"})
		return
	}
	hash := sha256.Sum256([]byte(body.Token))
	if err := h.invitations.Accept(c.Request.Context(), hex.EncodeToString(hash[:]), actor.ID, time.Now().UTC()); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func invitationCredentials() (string, string, error) {
	id, err := randomID()
	if err != nil {
		return "", "", err
	}
	token, err := randomID()
	return id, token, err
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
