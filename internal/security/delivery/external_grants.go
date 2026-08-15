package delivery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

type ExternalGrantOwnerLoader struct{}

func (ExternalGrantOwnerLoader) Load(_ context.Context, input application.EndpointInput) (application.LoadedResources, error) {
	id := input.Params["organizationID"]
	return application.LoadedResources{TenantID: id, Primary: domain.Resource{Type: "external_grant", ID: input.Params["grantID"], TenantID: id}}, nil
}

type ExternalGrantManageIntent struct{}

func (ExternalGrantManageIntent) Resolve(context.Context, application.EndpointInput) (domain.Operation, error) {
	return domain.Operation{ResourceType: "external_grant", Action: "manage"}, nil
}

type ExternalGrantHandler struct {
	service *application.ExternalGrantService
}

func NewExternalGrantHandler(s *application.ExternalGrantService) *ExternalGrantHandler {
	return &ExternalGrantHandler{service: s}
}

type externalGrantBody struct {
	Target       domain.ExternalGrantTarget `json:"target" binding:"required"`
	ResourceType string                     `json:"resource_type" binding:"required"`
	ResourceID   string                     `json:"resource_id"`
	Action       string                     `json:"action" binding:"required"`
	Effect       domain.Effect              `json:"effect" binding:"required"`
	ValidFrom    *time.Time                 `json:"valid_from"`
	ValidUntil   *time.Time                 `json:"valid_until"`
	Permissions  []domain.ExternalGrantItem `json:"permissions"`
	Roles        []domain.ExternalGrantItem `json:"roles"`
	Groups       []domain.ExternalGrantItem `json:"groups"`
	Features     []domain.ExternalGrantItem `json:"features"`
	Plans        []domain.ExternalGrantItem `json:"plans"`
	Quotas       []domain.ExternalGrantItem `json:"quotas"`
}

func (h *ExternalGrantHandler) Create(c *gin.Context) {
	var body externalGrantBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, _ := Actor(c)
	from := time.Now().UTC()
	if body.ValidFrom != nil {
		from = *body.ValidFrom
	}
	definition := domain.ExternalGrantDefinition{ID: newGrantID(), OwnerOrganizationID: c.Param("organizationID"), CreatedBy: actor.ID, Target: body.Target, Resource: domain.Resource{Type: body.ResourceType, ID: body.ResourceID, TenantID: c.Param("organizationID")}, Operation: domain.Operation{ResourceType: body.ResourceType, Action: body.Action}, Effect: body.Effect, ValidFrom: from, ValidUntil: body.ValidUntil, Permissions: body.Permissions, Roles: body.Roles, Groups: body.Groups, Features: body.Features, Plans: body.Plans, Quotas: body.Quotas}
	if err := h.service.Create(c.Request.Context(), definition); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": definition.ID})
}
func (h *ExternalGrantHandler) Revoke(c *gin.Context) {
	actor, _ := Actor(c)
	if err := h.service.Revoke(c.Request.Context(), c.Param("organizationID"), c.Param("grantID"), actor.ID, time.Now().UTC()); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *ExternalGrantHandler) List(c *gin.Context) {
	actor, _ := Actor(c)
	items, err := h.service.List(c.Request.Context(), c.Param("organizationID"), actor.ID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func newGrantID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return "grant:" + hex.EncodeToString(value[:])
}

func RegisterExternalGrantRoutes(router *gin.RouterGroup, guard func(domain.EndpointContract) gin.HandlerFunc, handler *ExternalGrantHandler) {
	contract := domain.EndpointContract{Operation: domain.Operation{ResourceType: "external_grant", Action: "manage"}, ActorConstraint: domain.UserOnly, TenantAccess: domain.StrictIsolation, RequireTenant: true, RequireResourceTenant: true, RequireOrganizationMembership: true, ProtectSystemResources: true, RequireMFA: true, MaxAuthAge: 30 * time.Minute}
	router.POST("/organizations/:organizationID/external-user-grants", guard(contract), handler.Create)
	router.GET("/organizations/:organizationID/external-user-grants", guard(contract), handler.List)
	router.DELETE("/organizations/:organizationID/external-user-grants/:grantID", guard(contract), handler.Revoke)
}
