// Package domain defines framework-independent authorization concepts.
package domain

import (
	"errors"
	"time"
)

var ErrQuotaExceeded = errors.New("quota exceeded")

type ActorType string

const (
	ActorUser    ActorType = "user"
	ActorMachine ActorType = "machine"
)

type SubjectType string

const (
	SubjectUser         SubjectType = "user"
	SubjectOrganization SubjectType = "organization"
)

type ScopeMode string

const (
	ScopeUser         ScopeMode = "user"
	ScopeOrganization ScopeMode = "organization"
)

type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

type Actor struct {
	ID, ClientID, OrganizationID string
	Type                         ActorType
	Attributes                   map[string]string
	AMR                          []string
	AuthTime                     time.Time
}

type Subject struct {
	Type SubjectType
	ID   string
}
type Operation struct{ ResourceType, Action string }

// Requirement describes the endpoint-specific checks that remain after the
// invariant hard-contract checks have succeeded.
type Requirement struct {
	RequirePermission bool
	RequireSelf       bool
	FeatureKey        string
	QuotaKey          string
	QuotaCost         int64
	Behavior          *BehaviorRequirement
}

// BehaviorRequirement describes deterministic decision enrichment. It does
// not grant access; it only selects a handler strategy after all checks pass.
type BehaviorRequirement struct {
	Attribute   string
	Maximum     int64
	Strategy    string
	Parameters  map[string]Value
	Obligations []Obligation
}

type Resource struct {
	Type, ID, TenantID, OwnerID string
	System                      bool
	Attributes                  map[string]string
}

type OrganizationGrant struct{ ID, OwnerOrganizationID, GranteeOrganizationID, GranteeUserID string }

type ExternalGrantTargetType string

const (
	ExternalTargetGlobalUser         ExternalGrantTargetType = "global_user"
	ExternalTargetOrganization       ExternalGrantTargetType = "organization"
	ExternalTargetOrganizationMember ExternalGrantTargetType = "organization_member"
)

// ExternalAccess is the immutable authorization context resolved from active
// grants owned by the resource organization. GrantIDs may contain several
// matching grants; every fact evaluation therefore applies deny-wins.
type ExternalAccess struct {
	GrantIDs            []string
	OwnerOrganizationID string
	ActorID             string
}

type Request struct {
	Method, RouteTemplate, EndpointBindingID string
	Actor                                    Actor
	Subject                                  Subject
	TenantID                                 string
	Operation                                Operation
	Primary                                  Resource
	Related                                  []Resource
	Now                                      time.Time
	PolicyID                                 string
	PolicyVersion                            int64
	Requirement                              Requirement
	Grant                                    *OrganizationGrant
	ExternalAccess                           *ExternalAccess
	ScopeMode                                ScopeMode
}

type DenialCode string

const (
	DenyUnauthenticated DenialCode = "unauthenticated"
	DenyEndpoint        DenialCode = "endpoint_unregistered"
	DenyHard            DenialCode = "hard_contract_denied"
	DenyPermission      DenialCode = "permission_denied"
	DenyFeature         DenialCode = "feature_disabled"
	DenyQuota           DenialCode = "quota_exceeded"
	DenyTenant          DenialCode = "tenant_mismatch"
	DenyPolicy          DenialCode = "policy_invalid"
	DenyGrant           DenialCode = "organization_grant_required"
	DenyMembership      DenialCode = "organization_membership_required"
	DenyPlan            DenialCode = "plan_unavailable"
)

type Value struct {
	String *string
	Int    *int64
	Bool   *bool
}
type QuotaCost struct {
	Subject           Subject
	GrantID, QuotaKey string
	ExternalGrantIDs  []string
	Cost              int64
}
type Evidence struct {
	Kind, Key, SourceID string
	Effect              Effect
	ExpiresAt           *time.Time
}
type Obligation struct {
	Type   string
	Config map[string]Value
}

type Decision struct {
	Allowed                     bool
	Code                        DenialCode
	Operation                   Operation
	Strategy                    string
	Parameters                  map[string]Value
	Obligations                 []Obligation
	QuotaCosts                  []QuotaCost
	EndpointBindingID, PolicyID string
	PolicyVersion               int64
	SelectedSubject             Subject
	Evidence                    []Evidence
}

func Allow(request Request) Decision {
	return Decision{Allowed: true, Operation: request.Operation, SelectedSubject: request.Subject, Parameters: map[string]Value{}, EndpointBindingID: request.EndpointBindingID, PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion}
}

func Deny(code DenialCode) Decision { return Decision{Code: code} }
