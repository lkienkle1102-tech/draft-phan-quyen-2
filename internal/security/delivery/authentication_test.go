package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

func TestAuthenticationRejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewAuthentication(fakeAuthenticator{err: errors.New("invalid")}, &fakeUsers{}).Middleware())
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer invalid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestAuthenticationProjectsUserAndSetsActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &fakeUsers{}
	router := gin.New()
	router.Use(NewAuthentication(fakeAuthenticator{actor: domain.Actor{ID: "user-a", Type: domain.ActorUser}}, users).Middleware())
	router.GET("/", func(c *gin.Context) {
		actor, found := Actor(c)
		if !found || actor.ID != "user-a" {
			t.Fatalf("actor=%+v found=%v", actor, found)
		}
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || users.id != "user-a" {
		t.Fatalf("status=%d projected=%q", response.Code, users.id)
	}
}

type fakeAuthenticator struct {
	actor domain.Actor
	err   error
}

func (f fakeAuthenticator) Authenticate(context.Context, string) (domain.Actor, error) {
	return f.actor, f.err
}

type fakeUsers struct{ id string }

func (f *fakeUsers) EnsureUser(_ context.Context, id string) error {
	f.id = id
	return nil
}
