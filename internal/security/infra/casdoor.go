package infra

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/shared/config"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

var ErrInvalidCasdoorConfiguration = errors.New("invalid Casdoor configuration")
var ErrInvalidCasdoorToken = errors.New("invalid Casdoor token")
var ErrUnknownCasdoorRole = errors.New("unknown Casdoor role")
var ErrUnknownCasdoorGroup = errors.New("unknown Casdoor group")
var ErrIncompleteCasdoorRoleSync = errors.New("incomplete Casdoor role synchronization")

type casdoorClient interface {
	IntrospectToken(string, string) (*casdoorsdk.IntrospectTokenResult, error)
	ParseJwtToken(string) (*casdoorsdk.Claims, error)
	Enforce(string, string, string, string, string, casdoorsdk.CasbinRequest) (bool, error)
	GetPolicies(string, string) ([]*casdoorsdk.CasbinRule, error)
	GetRoles() ([]*casdoorsdk.Role, error)
	GetGroups() ([]*casdoorsdk.Group, error)
	AddPolicy(*casdoorsdk.Enforcer, *casdoorsdk.CasbinRule) (bool, error)
	RemovePolicy(*casdoorsdk.Enforcer, *casdoorsdk.CasbinRule) (bool, error)
}

type Casdoor struct {
	client casdoorClient
	config config.Casdoor
}

func NewCasdoor(value config.Casdoor) (*Casdoor, error) {
	if err := validateCasdoorConfig(value); err != nil {
		return nil, err
	}
	timeout := value.HTTPTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	casdoorsdk.SetHttpClient(&http.Client{Timeout: timeout})
	client := casdoorsdk.NewClient(value.Endpoint, value.ClientID, value.ClientSecret, value.Certificate, value.Organization, value.Application)
	return &Casdoor{client: client, config: value}, nil
}

func newCasdoorWithClient(value config.Casdoor, client casdoorClient) *Casdoor {
	return &Casdoor{client: client, config: value}
}

func validateCasdoorConfig(value config.Casdoor) error {
	values := []string{value.Endpoint, value.ClientID, value.ClientSecret, value.Certificate, value.Organization, value.Application, value.PermissionID, value.ModelID, value.ResourceID, value.EnforcerID, value.Owner}
	for _, item := range values {
		if strings.TrimSpace(item) == "" {
			return ErrInvalidCasdoorConfiguration
		}
	}
	return nil
}

func (c *Casdoor) Authenticate(ctx context.Context, token string) (domain.Actor, error) {
	if err := ctx.Err(); err != nil {
		return domain.Actor{}, err
	}
	introspection, err := c.client.IntrospectToken(token, "access_token")
	if err != nil || introspection == nil || !introspection.Active {
		return domain.Actor{}, errors.Join(ErrInvalidCasdoorToken, err)
	}
	claims, err := c.client.ParseJwtToken(token)
	if err != nil || claims == nil || !validIntrospection(c.config, introspection) {
		return domain.Actor{}, errors.Join(ErrInvalidCasdoorToken, err)
	}
	actor := domain.Actor{ClientID: introspection.ClientId, Attributes: cloneStrings(claims.Properties), AuthTime: time.Unix(int64(introspection.Iat), 0).UTC()}
	actor.OrganizationID = actor.Attributes["organization_id"]
	if introspection.Username != "" {
		actor.ID = introspection.Username
		actor.Type = domain.ActorUser
		if len(claims.MfaAccounts) > 0 {
			actor.AMR = []string{"mfa"}
		}
	} else if introspection.ClientId != "" {
		actor.ID = introspection.ClientId
		actor.Type = domain.ActorMachine
	} else {
		return domain.Actor{}, ErrInvalidCasdoorToken
	}
	return actor, nil
}

func validIntrospection(value config.Casdoor, result *casdoorsdk.IntrospectTokenResult) bool {
	now := uint(time.Now().Unix())
	issuer := strings.TrimSuffix(value.Endpoint, "/")
	return result.Sub != "" && strings.TrimSuffix(result.Iss, "/") == issuer && slices.Contains(result.Aud, value.ClientID) && result.Exp > now && result.Nbf <= now
}

func (c *Casdoor) Enforce(ctx context.Context, actor domain.Actor, subject domain.Subject, operation domain.Operation) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	request := casdoorsdk.CasbinRequest{actorPrincipal(actor), subjectDomain(subject), operation.ResourceType, operation.Action}
	return c.client.Enforce(c.config.PermissionID, c.config.ModelID, c.config.ResourceID, c.config.EnforcerID, c.config.Owner, request)
}

func (c *Casdoor) Snapshot(ctx context.Context) (domain.AuthorizationSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return domain.AuthorizationSnapshot{}, err
	}
	rules, err := c.client.GetPolicies(c.config.EnforcerID, c.config.ResourceID)
	if err != nil {
		return domain.AuthorizationSnapshot{}, err
	}
	roles, err := c.client.GetRoles()
	if err != nil {
		return domain.AuthorizationSnapshot{}, err
	}
	groups, err := c.client.GetGroups()
	if err != nil {
		return domain.AuthorizationSnapshot{}, err
	}
	result := domain.AuthorizationSnapshot{Rules: make([]domain.PolicyRule, 0, len(rules)), Roles: map[string]domain.DirectoryObject{}, Groups: map[string]domain.DirectoryObject{}}
	for _, rule := range rules {
		result.Rules = append(result.Rules, fromCasbinRule(rule))
	}
	for _, role := range roles {
		result.Roles[role.Name] = domain.DirectoryObject{ID: role.Name, Name: role.DisplayName, Domains: append([]string(nil), role.Domains...)}
	}
	for _, group := range groups {
		result.Groups[group.Name] = domain.DirectoryObject{ID: group.Name, Name: group.DisplayName}
	}
	return result, nil
}

func (c *Casdoor) ValidateRoles(ctx context.Context, subject domain.Subject, roleIDs []string) error {
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return err
	}
	domainID := subjectDomain(subject)
	for _, roleID := range roleIDs {
		role, found := snapshot.Roles[roleID]
		if !found || (!slices.Contains(role.Domains, domainID) && !principalHasDomain(snapshot.Rules, rolePrincipal(roleID), domainID)) {
			return fmt.Errorf("%w: %s", ErrUnknownCasdoorRole, roleID)
		}
	}
	return nil
}

func (c *Casdoor) ValidateGroups(ctx context.Context, subject domain.Subject, groupIDs []string) error {
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return err
	}
	domainID := subjectDomain(subject)
	for _, groupID := range groupIDs {
		_, found := snapshot.Groups[groupID]
		if !found || !principalHasDomain(snapshot.Rules, "group::"+groupID, domainID) {
			return fmt.Errorf("%w: %s", ErrUnknownCasdoorGroup, groupID)
		}
	}
	return nil
}

func (c *Casdoor) EnsureMembershipRoles(ctx context.Context, actor domain.Actor, subject domain.Subject, membershipID string, roleIDs []string) (domain.RoleSyncResult, error) {
	if err := c.ValidateRoles(ctx, subject, roleIDs); err != nil {
		return domain.RoleSyncResult{}, err
	}
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return domain.RoleSyncResult{}, err
	}
	result := domain.RoleSyncResult{}
	required := membershipRoleRules(actor, subject, membershipID, roleIDs)
	for _, rule := range required {
		if containsRule(snapshot.Rules, rule) {
			continue
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		result.ExternalMutationPossible = true
		if _, err = c.client.AddPolicy(c.enforcer(), toCasbinRule(rule)); err != nil {
			return result, err
		}
	}
	verified, err := c.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	for _, rule := range required {
		if !containsRule(verified.Rules, rule) {
			return result, ErrIncompleteCasdoorRoleSync
		}
	}
	return result, nil
}

func (c *Casdoor) enforcer() *casdoorsdk.Enforcer {
	return &casdoorsdk.Enforcer{Owner: c.config.Owner, Name: c.config.EnforcerID, Model: c.config.ModelID, Adapter: c.config.ResourceID, IsEnabled: true}
}

func actorPrincipal(actor domain.Actor) string {
	if actor.Type == domain.ActorMachine {
		return "machine::" + actor.ClientID
	}
	return "user::" + actor.ID
}

func rolePrincipal(value string) string { return "role::" + value }

func membershipPrincipal(value string) string { return "membership::" + value }

func membershipRoleRules(actor domain.Actor, subject domain.Subject, membershipID string, roleIDs []string) []domain.PolicyRule {
	domainID, membership := subjectDomain(subject), membershipPrincipal(membershipID)
	rules := make([]domain.PolicyRule, 0, len(roleIDs)+1)
	for _, roleID := range roleIDs {
		rules = append(rules, domain.PolicyRule{PType: "g", V0: membership, V1: rolePrincipal(roleID), V2: domainID})
	}
	return append(rules, domain.PolicyRule{PType: "g", V0: actorPrincipal(actor), V1: membership, V2: domainID})
}

func subjectDomain(subject domain.Subject) string { return string(subject.Type) + "::" + subject.ID }

func principalHasDomain(rules []domain.PolicyRule, principal, value string) bool {
	for _, rule := range rules {
		if (rule.PType == "p" && rule.V0 == principal && rule.V1 == value) || (rule.PType == "g" && rule.V1 == principal && rule.V2 == value) {
			return true
		}
	}
	return false
}

func containsRule(rules []domain.PolicyRule, wanted domain.PolicyRule) bool {
	return slices.Contains(rules, wanted)
}

func fromCasbinRule(value *casdoorsdk.CasbinRule) domain.PolicyRule {
	return domain.PolicyRule{PType: value.Ptype, V0: value.V0, V1: value.V1, V2: value.V2, V3: value.V3, V4: value.V4, V5: value.V5}
}

func toCasbinRule(value domain.PolicyRule) *casdoorsdk.CasbinRule {
	return &casdoorsdk.CasbinRule{Ptype: value.PType, V0: value.V0, V1: value.V1, V2: value.V2, V3: value.V3, V4: value.V4, V5: value.V5}
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
