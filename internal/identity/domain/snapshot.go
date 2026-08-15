// Package domain defines the transport-independent identity snapshot.
package domain

import "time"

type Source struct {
	Type, ID, Effect string
}

type EffectiveFact struct {
	Key, Effect string
	Sources     []Source
}

type Plan struct {
	ID, Name, Effect, Status string
	ValidFrom, PeriodStart   time.Time
	ValidUntil               *time.Time
	PeriodEnd                time.Time
}

type Quota struct {
	Key, Effect      string
	Limit, Remaining *int64
	Used, Reserved   int64
	Unlimited        bool
	PeriodStart      time.Time
	PeriodEnd        *time.Time
	Sources          []Source
}

type Entitlements struct {
	Roles, Groups, Permissions, Features []EffectiveFact
	Plan                                 *Plan
	Quotas                               []Quota
}

type Subject struct{ Type, ID string }

type Scope struct {
	Subject      Subject
	Entitlements Entitlements
}

type Organization struct{ ID, Name string }

type Membership struct {
	ID       string
	JoinedAt time.Time
}

type OrganizationScope struct {
	Organization Organization
	Membership   Membership
	Entitlements Entitlements
}

type Identity struct {
	ID, ActorType, TokenOrganizationID string
	AMR                                []string
	AuthTime                           time.Time
}

type ExternalTarget struct {
	Type, UserID, OrganizationID, MembershipID string
}

type ExternalGrant struct {
	ID, OwnerOrganizationID, OwnerOrganizationName string
	Target                                         ExternalTarget
	ResourceType, ResourceID, Action, Effect       string
	ValidFrom                                      time.Time
	ValidUntil                                     *time.Time
	Entitlements                                   Entitlements
}

type Snapshot struct {
	GeneratedAt    time.Time
	Identity       Identity
	Personal       Scope
	Organizations  []OrganizationScope
	ExternalGrants []ExternalGrant
}
