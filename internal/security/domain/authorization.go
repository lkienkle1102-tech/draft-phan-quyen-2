package domain

import "context"

// PolicyRule is a transport-independent Casbin policy row.
type PolicyRule struct {
	PType string
	V0    string
	V1    string
	V2    string
	V3    string
	V4    string
	V5    string
}

type DirectoryObject struct {
	ID, Name string
	Domains  []string
}

type AuthorizationSnapshot struct {
	Rules  []PolicyRule
	Roles  map[string]DirectoryObject
	Groups map[string]DirectoryObject
}

type RoleSyncResult struct{ ExternalMutationPossible bool }

type AuthorizationDirectory interface {
	Snapshot(context.Context) (AuthorizationSnapshot, error)
	ValidateRoles(context.Context, Subject, []string) error
	ValidateGroups(context.Context, Subject, []string) error
	EnsureMembershipRoles(context.Context, Actor, Subject, string, []string) (RoleSyncResult, error)
}

func PoliciesForPrincipal(rules []PolicyRule, principal, domainID string) []PolicyRule {
	reachable := map[string]bool{principal: true}
	for changed := true; changed; {
		changed = false
		for _, rule := range rules {
			if rule.PType == "g" && rule.V2 == domainID && reachable[rule.V0] && !reachable[rule.V1] {
				reachable[rule.V1], changed = true, true
			}
		}
	}
	var result []PolicyRule
	for _, rule := range rules {
		if rule.PType == "p" && rule.V1 == domainID && reachable[rule.V0] {
			result = append(result, rule)
		}
	}
	return result
}
