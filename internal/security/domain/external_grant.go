package domain

import "time"

type ExternalGrantItem struct {
	Key    string `json:"key"`
	Effect Effect `json:"effect"`
	Limit  *int64 `json:"limit,omitempty"`
}

type ExternalGrantTarget struct {
	Type           ExternalGrantTargetType `json:"type"`
	UserID         string                  `json:"user_id,omitempty"`
	OrganizationID string                  `json:"organization_id,omitempty"`
	MembershipID   string                  `json:"membership_id,omitempty"`
}

type ExternalGrantDefinition struct {
	ID, OwnerOrganizationID, CreatedBy string
	Target                             ExternalGrantTarget
	Resource                           Resource
	Operation                          Operation
	Effect                             Effect
	ValidFrom                          time.Time
	ValidUntil                         *time.Time
	Permissions, Roles, Groups         []ExternalGrantItem
	Features, Plans, Quotas            []ExternalGrantItem
}

type ExternalGrantSummary struct {
	ID                  string              `json:"id"`
	OwnerOrganizationID string              `json:"owner_organization_id"`
	Target              ExternalGrantTarget `json:"target"`
	ResourceType        string              `json:"resource_type"`
	ResourceID          string              `json:"resource_id,omitempty"`
	Action              string              `json:"action"`
	Effect              Effect              `json:"effect"`
	Status              string              `json:"status"`
	ValidFrom           time.Time           `json:"valid_from"`
	ValidUntil          *time.Time          `json:"valid_until,omitempty"`
}
