package delivery

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

const actorKey = "security.actor"
const requestKey = "security.request"
const decisionKey = "security.decision"

func setActor(c *gin.Context, value domain.Actor) {
	c.Set(actorKey, value)
	c.Request = c.Request.WithContext(application.WithActor(c.Request.Context(), value))
}
func Actor(c *gin.Context) (domain.Actor, bool) {
	value, found := c.Get(actorKey)
	result, valid := value.(domain.Actor)
	return result, found && valid
}
func Decision(c *gin.Context) (domain.Decision, bool) {
	value, found := c.Get(decisionKey)
	result, valid := value.(domain.Decision)
	return result, found && valid
}
func request(c *gin.Context) (domain.Request, bool) {
	value, found := c.Get(requestKey)
	result, valid := value.(domain.Request)
	return result, found && valid
}

type EndpointContext struct {
	resolver *application.EndpointResolver
	auditor  DecisionAuditor
}

func NewEndpointContext(resolver *application.EndpointResolver, auditor DecisionAuditor) *EndpointContext {
	return &EndpointContext{resolver: resolver, auditor: auditor}
}
func (m *EndpointContext) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := Actor(c)
		if !ok {
			abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			abort(c, http.StatusBadRequest, domain.DenyEndpoint)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		params := map[string]string{}
		for _, item := range c.Params {
			params[item.Key] = item.Value
		}
		resolved, err := m.resolver.Resolve(c.Request.Context(), c.Request.Method, c.FullPath(), application.EndpointInput{Params: params, Body: body, Actor: actor})
		if err != nil {
			request := domain.Request{Actor: actor, Method: c.Request.Method, RouteTemplate: c.FullPath()}
			_ = m.auditor.AuditDecision(c.Request.Context(), request, domain.Deny(domain.DenyEndpoint), "deny", string(domain.DenyEndpoint))
			abort(c, http.StatusNotFound, domain.DenyEndpoint)
			return
		}
		c.Set(requestKey, resolved)
		c.Next()
	}
}

type DecisionAuditor interface {
	AuditDecision(context.Context, domain.Request, domain.Decision, string, string) error
}

type Authorization struct {
	engine  application.Authorizer
	auditor DecisionAuditor
}

func NewAuthorization(engine application.Authorizer, auditor DecisionAuditor) *Authorization {
	return &Authorization{engine: engine, auditor: auditor}
}

func (m *Authorization) Enforce(contract domain.EndpointContract) gin.HandlerFunc {
	return func(c *gin.Context) {
		current, ok := request(c)
		if !ok {
			abort(c, http.StatusInternalServerError, domain.DenyPolicy)
			return
		}
		resolved, decision, err := m.engine.Authorize(c.Request.Context(), current, contract)
		if err != nil {
			_ = m.auditor.AuditDecision(c.Request.Context(), resolved, decision, "error", err.Error())
			abort(c, http.StatusInternalServerError, domain.DenyPolicy)
			return
		}
		if !decision.Allowed {
			_ = m.auditor.AuditDecision(c.Request.Context(), resolved, decision, "deny", string(decision.Code))
			abortDecision(c, decision)
			return
		}
		c.Set(requestKey, resolved)
		_ = m.auditor.AuditDecision(c.Request.Context(), resolved, decision, "allow", "authorized")
		c.Set(decisionKey, decision)
		c.Request = c.Request.WithContext(application.WithDecision(c.Request.Context(), decision))
		c.Next()
	}
}

func abortDecision(c *gin.Context, d domain.Decision) {
	status := http.StatusForbidden
	if d.Code == domain.DenyTenant || d.Code == domain.DenyGrant || d.Code == domain.DenyMembership {
		status = http.StatusNotFound
	}
	if d.Code == domain.DenyQuota {
		status = http.StatusTooManyRequests
	}
	abort(c, status, d.Code)
}
func abort(c *gin.Context, status int, code domain.DenialCode) {
	c.AbortWithStatusJSON(status, gin.H{"error": code})
}
