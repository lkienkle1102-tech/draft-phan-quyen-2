package casdoortest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	security "example.com/phan-quyen-golang/internal/security/domain"
)

var ErrFakeToken = errors.New("invalid fake Casdoor token")

type FakeCasdoor struct {
	mu       sync.RWMutex
	snapshot security.AuthorizationSnapshot
}

func NewFakeCasdoor() *FakeCasdoor {
	roles := map[string]security.DirectoryObject{
		"organization:org-a:finance": {ID: "organization:org-a:finance", Name: "finance-manager", Domains: []string{"organization::org-a"}},
		"organization:org-b:finance": {ID: "organization:org-b:finance", Name: "finance-manager", Domains: []string{"organization::org-b"}},
	}
	rules := []security.PolicyRule{
		{PType: "g", V0: "user::user-a", V1: "role::organization:org-a:finance", V2: "organization::org-a"},
		{PType: "g", V0: "user::user-b", V1: "role::organization:org-b:finance", V2: "organization::org-b"},
		{PType: "p", V0: "role::organization:org-a:finance", V1: "organization::org-a", V2: "invoice", V3: "approve", V4: "allow"},
		{PType: "p", V0: "role::organization:org-a:finance", V1: "organization::org-a", V2: "organization_membership", V3: "review", V4: "allow"},
		{PType: "p", V0: "role::organization:org-a:finance", V1: "organization::org-a", V2: "organization_membership", V3: "invite", V4: "allow"},
		{PType: "p", V0: "role::organization:org-a:finance", V1: "organization::org-a", V2: "external_grant", V3: "manage", V4: "allow"},
		{PType: "p", V0: "role::organization:org-b:finance", V1: "organization::org-b", V2: "invoice", V3: "approve", V4: "allow"},
		{PType: "p", V0: "role::organization:org-b:finance", V1: "organization::org-b", V2: "organization_membership", V3: "review", V4: "allow"},
		{PType: "p", V0: "role::organization:org-b:finance", V1: "organization::org-b", V2: "organization_membership", V3: "invite", V4: "allow"},
		{PType: "p", V0: "user::user-personal", V1: "user::user-personal", V2: "organization_membership", V3: "apply", V4: "allow"},
		{PType: "p", V0: "user::user-personal", V1: "user::user-personal", V2: "organization_membership", V3: "accept", V4: "allow"},
		{PType: "p", V0: "user::user-a", V1: "user::user-a", V2: "identity", V3: "read_self", V4: "allow"},
	}
	return &FakeCasdoor{snapshot: security.AuthorizationSnapshot{Rules: rules, Roles: roles, Groups: map[string]security.DirectoryObject{}}}
}

func (f *FakeCasdoor) AddRule(rule security.PolicyRule) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.snapshot.Rules, rule) {
		f.snapshot.Rules = append(f.snapshot.Rules, rule)
	}
}

func (f *FakeCasdoor) AddGroup(group security.DirectoryObject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot.Groups[group.ID] = group
}

func (f *FakeCasdoor) Authenticate(_ context.Context, token string) (security.Actor, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return security.Actor{}, ErrFakeToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return security.Actor{}, ErrFakeToken
	}
	var claims struct {
		Sub            string            `json:"sub"`
		ActorType      string            `json:"actor_type"`
		ClientID       string            `json:"client_id"`
		OrganizationID string            `json:"organization_id"`
		Exp            int64             `json:"exp"`
		NBF            int64             `json:"nbf"`
		AuthTime       int64             `json:"auth_time"`
		AMR            []string          `json:"amr"`
		Attributes     map[string]string `json:"attributes"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return security.Actor{}, ErrFakeToken
	}
	now := time.Now().Unix()
	if claims.Sub == "" || claims.Exp <= now || claims.NBF > now {
		return security.Actor{}, ErrFakeToken
	}
	kind := security.ActorType(claims.ActorType)
	if kind != security.ActorUser && kind != security.ActorMachine {
		return security.Actor{}, ErrFakeToken
	}
	return security.Actor{ID: claims.Sub, Type: kind, ClientID: claims.ClientID, OrganizationID: claims.OrganizationID, AMR: append([]string(nil), claims.AMR...), Attributes: claims.Attributes, AuthTime: time.Unix(claims.AuthTime, 0).UTC()}, nil
}

func (f *FakeCasdoor) Enforce(_ context.Context, actor security.Actor, subject security.Subject, operation security.Operation) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	principal := "user::" + actor.ID
	if actor.Type == security.ActorMachine {
		principal = "machine::" + actor.ClientID
	}
	domainID := string(subject.Type) + "::" + subject.ID
	principals := f.principals(principal, domainID)
	allowed, denied := false, false
	for _, rule := range f.snapshot.Rules {
		if rule.PType != "p" || !slices.Contains(principals, rule.V0) || rule.V1 != domainID || rule.V2 != operation.ResourceType || rule.V3 != operation.Action {
			continue
		}
		allowed = allowed || rule.V4 == "allow"
		denied = denied || rule.V4 == "deny"
	}
	return allowed && !denied, nil
}

func (f *FakeCasdoor) principals(value, domainID string) []string {
	result := []string{value}
	for changed := true; changed; {
		changed = false
		for _, rule := range f.snapshot.Rules {
			if rule.PType == "g" && rule.V2 == domainID && slices.Contains(result, rule.V0) && !slices.Contains(result, rule.V1) {
				result = append(result, rule.V1)
				changed = true
			}
		}
	}
	return result
}

func (f *FakeCasdoor) Snapshot(context.Context) (security.AuthorizationSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return cloneSnapshot(f.snapshot), nil
}

func (f *FakeCasdoor) ValidateRoles(_ context.Context, subject security.Subject, roleIDs []string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return validateDirectoryObjects(f.snapshot.Roles, subject, roleIDs, "unknown role")
}

func (f *FakeCasdoor) ValidateGroups(_ context.Context, subject security.Subject, groupIDs []string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return validateDirectoryObjects(f.snapshot.Groups, subject, groupIDs, "unknown group")
}

func validateDirectoryObjects(values map[string]security.DirectoryObject, subject security.Subject, ids []string, message string) error {
	domainID := string(subject.Type) + "::" + subject.ID
	for _, id := range ids {
		value, found := values[id]
		if !found || !slices.Contains(value.Domains, domainID) {
			return errors.New(message)
		}
	}
	return nil
}

func (f *FakeCasdoor) EnsureMembershipRoles(ctx context.Context, actor security.Actor, subject security.Subject, membershipID string, roleIDs []string) (security.RoleSyncResult, error) {
	if err := f.ValidateRoles(ctx, subject, roleIDs); err != nil {
		return security.RoleSyncResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	result := security.RoleSyncResult{}
	domainID := string(subject.Type) + "::" + subject.ID
	membership := "membership::" + membershipID
	for _, roleID := range roleIDs {
		rule := security.PolicyRule{PType: "g", V0: membership, V1: "role::" + roleID, V2: domainID}
		if !slices.Contains(f.snapshot.Rules, rule) {
			f.snapshot.Rules = append(f.snapshot.Rules, rule)
			result.ExternalMutationPossible = true
		}
	}
	userRule := security.PolicyRule{PType: "g", V0: "user::" + actor.ID, V1: membership, V2: domainID}
	if !slices.Contains(f.snapshot.Rules, userRule) {
		f.snapshot.Rules = append(f.snapshot.Rules, userRule)
		result.ExternalMutationPossible = true
	}
	return result, nil
}

func cloneSnapshot(value security.AuthorizationSnapshot) security.AuthorizationSnapshot {
	result := security.AuthorizationSnapshot{Rules: append([]security.PolicyRule(nil), value.Rules...), Roles: map[string]security.DirectoryObject{}, Groups: map[string]security.DirectoryObject{}}
	for key, item := range value.Roles {
		item.Domains = append([]string(nil), item.Domains...)
		result.Roles[key] = item
	}
	for key, item := range value.Groups {
		result.Groups[key] = item
	}
	return result
}
