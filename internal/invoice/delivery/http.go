// Package delivery exposes invoice HTTP adapters.
package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	invoiceapp "example.com/phan-quyen-golang/internal/invoice/application"
	securityapp "example.com/phan-quyen-golang/internal/security/application"
	security "example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

type ResourceLoader struct{ database *sql.DB }

func NewResourceLoader(database *sql.DB) *ResourceLoader { return &ResourceLoader{database: database} }
func (l *ResourceLoader) Load(ctx context.Context, input securityapp.EndpointInput) (securityapp.LoadedResources, error) {
	var resource security.Resource
	var region string
	var amount int64
	err := l.database.QueryRowContext(ctx, `SELECT id,organization_id,owner_id,system,region,amount FROM invoices WHERE id=? AND organization_id=?`, input.Params["invoiceID"], input.Params["organizationID"]).Scan(&resource.ID, &resource.TenantID, &resource.OwnerID, &resource.System, &region, &amount)
	if err != nil {
		return securityapp.LoadedResources{}, err
	}
	resource.Type = "invoice"
	resource.Attributes = map[string]string{"region": region, "amount": strconv.FormatInt(amount, 10)}
	return securityapp.LoadedResources{Primary: resource}, nil
}

type ApproveIntent struct{}

func (ApproveIntent) Resolve(context.Context, securityapp.EndpointInput) (security.Operation, error) {
	return security.Operation{ResourceType: "invoice", Action: "approve"}, nil
}

type Handler struct{ service *invoiceapp.ApproveService }

func NewHandler(service *invoiceapp.ApproveService) *Handler { return &Handler{service: service} }

// RegisterRoutes exposes invoice endpoints behind the authorization handlers
// supplied by the application composition root.
type RouteDependencies struct {
	Guard   func(security.EndpointContract) gin.HandlerFunc
	Handler *Handler
}

func RegisterRoutes(router *gin.RouterGroup, dependencies RouteDependencies) {
	contract := security.EndpointContract{Operation: security.Operation{ResourceType: "invoice", Action: "approve"}, ActorConstraint: security.UserOnly, TenantAccess: security.ExplicitGrant, RequireTenant: true, RequireResourceTenant: true, RequireOrganizationMembership: true, ProtectSystemResources: true, RequireRelatedAuthorization: true, RequireMFA: true, MaxAuthAge: 30 * time.Minute}
	router.POST("/organizations/:organizationID/invoices/:invoiceID/approve", dependencies.Guard(contract), dependencies.Handler.Approve)
}

type approveBody struct {
	Version int64 `json:"version" binding:"required"`
}

func (h *Handler) Approve(c *gin.Context) {
	var body approveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, _ := securityapp.ActorFromContext(c.Request.Context())
	decision, _ := securityapp.DecisionFromContext(c.Request.Context())
	rawKey := c.GetHeader("Idempotency-Key")
	if rawKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "idempotency_key_required"})
		return
	}
	scopedKey := actor.ID + ":" + string(decision.SelectedSubject.Type) + ":" + decision.SelectedSubject.ID + ":invoice:approve:" + rawKey
	hash := sha256.Sum256([]byte(scopedKey + ":" + c.Param("invoiceID") + ":" + strconv.FormatInt(body.Version, 10)))
	result, err := h.service.Execute(c.Request.Context(), invoiceapp.Command{Actor: actor, InvoiceID: c.Param("invoiceID"), Version: body.Version, IdempotencyKey: scopedKey, RequestHash: hex.EncodeToString(hash[:])}, decision)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, security.ErrQuotaExceeded) {
			status = http.StatusTooManyRequests
		}
		if errors.Is(err, invoiceapp.ErrUnknownStrategy) {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
