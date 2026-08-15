package application

import (
	"context"
	"errors"
	"testing"

	"example.com/phan-quyen-golang/internal/security/domain"
)

func TestCatalogIsImmutableAndRejectsDuplicateRoutes(t *testing.T) {
	binding := EndpointBinding{ID: "me", Method: "GET", Route: "/v1/me", Loader: "me", Intent: "me", Operation: domain.Operation{ResourceType: "identity", Action: "read_self"}}
	if _, err := NewCatalog([]EndpointBinding{binding, binding}); !errors.Is(err, ErrDuplicateEndpoint) {
		t.Fatalf("duplicate err=%v", err)
	}
	catalog, err := NewCatalog([]EndpointBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	found, err := catalog.FindEndpoint(context.Background(), "get", "/v1/me")
	if err != nil || found.ID != "me" {
		t.Fatalf("binding=%+v err=%v", found, err)
	}
}
