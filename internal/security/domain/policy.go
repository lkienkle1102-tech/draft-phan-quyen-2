package domain

type NodeType string

const (
	NodeAll  NodeType = "ALL"
	NodeAny  NodeType = "ANY"
	NodeNot  NodeType = "NOT"
	NodeRule NodeType = "RULE"
)

type RuleType string

const (
	RulePermission        RuleType = "permission"
	RuleRole              RuleType = "role"
	RuleGroup             RuleType = "group"
	RuleMember            RuleType = "organization_member"
	RuleClientGrant       RuleType = "client_grant"
	RuleConsent           RuleType = "consent"
	RuleOwner             RuleType = "owner"
	RuleSelf              RuleType = "self"
	RuleAttributeMatch    RuleType = "actor_resource_attribute_match"
	RuleRecentMFA         RuleType = "recent_mfa"
	RuleFeature           RuleType = "feature"
	RulePlan              RuleType = "subscription_plan"
	RuleQuota             RuleType = "quota_available"
	RuleAmount            RuleType = "amount_threshold"
	RuleTimeWindow        RuleType = "time_window"
	RuleOrganizationGrant RuleType = "organization_access_grant"
	RuleDelegatedFeature  RuleType = "delegated_feature"
	RuleDelegatedQuota    RuleType = "delegated_quota_available"
)

type PolicyNode struct {
	ID, ParentID string
	Purpose      string
	Type         NodeType
	Rule         RuleType
	Config       map[string]Value
	Position     int
}
type Behavior struct {
	Priority                int
	ConditionRoot, Strategy string
	Parameters              map[string]Value
	Obligations             []Obligation
}
type Policy struct {
	ID        string
	Version   int64
	RootID    string
	Nodes     map[string]PolicyNode
	Children  map[string][]string
	Behaviors []Behavior
	Denials   []PolicyDenial
}

type PolicyDenial struct {
	RootID string
	Code   DenialCode
}
