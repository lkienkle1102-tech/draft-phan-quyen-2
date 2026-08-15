package application

import (
	"context"
	"errors"
	"strings"
)

var ErrDuplicateEndpoint = errors.New("duplicate endpoint binding")

// Catalog is an immutable in-process endpoint registry. Route metadata is
// deployed with the code while authorization policy remains in Casbin.
type Catalog struct{ bindings map[string]EndpointBinding }

func NewCatalog(bindings []EndpointBinding) (*Catalog, error) {
	values := make(map[string]EndpointBinding, len(bindings))
	for _, binding := range bindings {
		key := endpointKey(binding.Method, binding.Route)
		if binding.ID == "" || binding.Method == "" || binding.Route == "" || binding.Loader == "" || binding.Intent == "" || binding.Operation.ResourceType == "" || binding.Operation.Action == "" {
			return nil, ErrEndpointConfiguration
		}
		if _, exists := values[key]; exists {
			return nil, ErrDuplicateEndpoint
		}
		values[key] = binding
	}
	return &Catalog{bindings: values}, nil
}

func (c *Catalog) FindEndpoint(_ context.Context, method, route string) (EndpointBinding, error) {
	binding, found := c.bindings[endpointKey(method, route)]
	if !found {
		return EndpointBinding{}, ErrEndpointConfiguration
	}
	return binding, nil
}

func endpointKey(method, route string) string { return strings.ToUpper(method) + "\x00" + route }
