// Package delivery provides Gin security middleware.
package delivery

import (
	"errors"
	"net/http"
	"strings"

	"example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

type Authentication struct {
	authenticator application.Authenticator
	users         application.UserProjection
}

func NewAuthentication(authenticator application.Authenticator, users application.UserProjection) *Authentication {
	return &Authentication{authenticator: authenticator, users: users}
}

func (m *Authentication) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearer(c.GetHeader("Authorization"))
		if err != nil {
			abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
			return
		}
		actor, err := m.authenticator.Authenticate(c.Request.Context(), raw)
		if err != nil {
			abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
			return
		}
		if actor.Type == domain.ActorUser {
			if err := m.users.EnsureUser(c.Request.Context(), actor.ID); err != nil {
				abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
				return
			}
		}
		setActor(c, actor)
		c.Next()
	}
}

func bearer(value string) (string, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("bearer required")
	}
	return parts[1], nil
}
