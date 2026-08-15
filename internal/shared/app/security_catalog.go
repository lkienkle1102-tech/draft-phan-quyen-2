package app

import (
	"net/http"

	securityapp "example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
)

func securityCatalog() (*securityapp.Catalog, error) {
	reason := "low_value_manual_review"
	return securityapp.NewCatalog([]securityapp.EndpointBinding{
		binding("invoice-approve", http.MethodPost, "/v1/organizations/:organizationID/invoices/:invoiceID/approve", endpointImplementation{"invoice", "approve"}, domain.Operation{ResourceType: "invoice", Action: "approve"}, "invoice-approve", domain.ScopeOrganization, domain.Requirement{
			RequirePermission: true,
			FeatureKey:        "invoice_management",
			QuotaKey:          "invoice_approvals",
			QuotaCost:         1,
			Behavior: &domain.BehaviorRequirement{
				Attribute: "amount",
				Maximum:   30000,
				Strategy:  "approve",
				Parameters: map[string]domain.Value{
					"reason": {String: &reason},
				},
				Obligations: []domain.Obligation{{Type: "require_manual_review", Config: map[string]domain.Value{}}},
			},
		}),
		binding("membership-apply", http.MethodPost, "/v1/organizations/:organizationID/membership-applications", endpointImplementation{"membership-apply", "membership-apply"}, domain.Operation{ResourceType: "organization_membership", Action: "apply"}, "membership-apply", domain.ScopeUser, domain.Requirement{RequirePermission: true, FeatureKey: "organization_membership", QuotaKey: "membership_applications", QuotaCost: 1}),
		binding("membership-review", http.MethodPost, "/v1/organizations/:organizationID/membership-applications/:applicationID/review", endpointImplementation{"membership-review", "membership-review"}, domain.Operation{ResourceType: "organization_membership", Action: "review"}, "membership-review", domain.ScopeOrganization, domain.Requirement{RequirePermission: true}),
		binding("membership-invite", http.MethodPost, "/v1/organizations/:organizationID/membership-invitations", endpointImplementation{"membership-invite", "membership-invite"}, domain.Operation{ResourceType: "organization_membership", Action: "invite"}, "membership-invite", domain.ScopeOrganization, domain.Requirement{RequirePermission: true}),
		binding("membership-accept", http.MethodPost, "/v1/membership-invitations/accept", endpointImplementation{"membership-accept", "membership-accept"}, domain.Operation{ResourceType: "organization_membership", Action: "accept"}, "membership-accept", domain.ScopeUser, domain.Requirement{RequirePermission: true}),
		binding("external-grant-create", http.MethodPost, "/v1/organizations/:organizationID/external-user-grants", endpointImplementation{"external-grant-owner", "external-grant-manage"}, domain.Operation{ResourceType: "external_grant", Action: "manage"}, "external-grant-manage", domain.ScopeOrganization, domain.Requirement{RequirePermission: true}),
		binding("external-grant-list", http.MethodGet, "/v1/organizations/:organizationID/external-user-grants", endpointImplementation{"external-grant-owner", "external-grant-manage"}, domain.Operation{ResourceType: "external_grant", Action: "manage"}, "external-grant-manage", domain.ScopeOrganization, domain.Requirement{RequirePermission: true}),
		binding("external-grant-revoke", http.MethodDelete, "/v1/organizations/:organizationID/external-user-grants/:grantID", endpointImplementation{"external-grant-owner", "external-grant-manage"}, domain.Operation{ResourceType: "external_grant", Action: "manage"}, "external-grant-manage", domain.ScopeOrganization, domain.Requirement{RequirePermission: true}),
		binding("identity-me", http.MethodGet, "/v1/me", endpointImplementation{"me", "me-read"}, domain.Operation{ResourceType: "identity", Action: "read_self"}, "identity-read-self", domain.ScopeUser, domain.Requirement{RequireSelf: true}),
	})
}

type endpointImplementation struct{ loader, intent string }

func binding(id, method, route string, implementation endpointImplementation, operation domain.Operation, policy string, scope domain.ScopeMode, requirement domain.Requirement) securityapp.EndpointBinding {
	return securityapp.EndpointBinding{ID: id, Method: method, Route: route, Loader: implementation.loader, Intent: implementation.intent, Operation: operation, PolicyID: policy, PolicyVersion: 1, ScopeMode: scope, Requirement: requirement}
}
