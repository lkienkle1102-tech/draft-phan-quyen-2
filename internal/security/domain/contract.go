package domain

import "time"

type ActorConstraint string

const (
	AnyActor    ActorConstraint = "any"
	UserOnly    ActorConstraint = "user"
	MachineOnly ActorConstraint = "machine"
)

type TenantAccessMode string

const (
	StrictIsolation TenantAccessMode = "strict_isolation"
	ExplicitGrant   TenantAccessMode = "explicit_organization_grant"
)

type EndpointContract struct {
	Operation                     Operation
	ActorConstraint               ActorConstraint
	TenantAccess                  TenantAccessMode
	RequireTenant                 bool
	ProtectSystemResources        bool
	DenySelfEscalation            bool
	RequireRelatedAuthorization   bool
	RequireMFA                    bool
	MaxAuthAge                    time.Duration
	RequireOrganizationMembership bool
	RequireResourceTenant         bool
}

type GrantRequest struct {
	OwnerOrganizationID, GranteeOrganizationID, ActorID string
	Resource                                            Resource
	Operation                                           Operation
	At                                                  time.Time
}
