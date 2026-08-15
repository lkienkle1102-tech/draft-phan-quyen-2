package infra

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/shared/config"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

func TestCasdoorAuthenticateMapsUserMachineAndMFA(t *testing.T) {
	now := uint(time.Now().Unix())
	client := &fakeCasdoorClient{
		introspection: &casdoorsdk.IntrospectTokenResult{Active: true, Sub: "subject", Iss: "https://casdoor.example", Aud: []string{"client"}, Exp: now + 60, Nbf: now - 1, Iat: now, Username: "user-a", ClientId: "client"},
		claims:        &casdoorsdk.Claims{User: casdoorsdk.User{Properties: map[string]string{"organization_id": "org-a"}, MfaAccounts: []casdoorsdk.MfaAccount{{}}}},
	}
	adapter := newCasdoorWithClient(testCasdoorConfig(), client)
	actor, err := adapter.Authenticate(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if actor.ID != "user-a" || actor.Type != domain.ActorUser || actor.OrganizationID != "org-a" || !slices.Equal(actor.AMR, []string{"mfa"}) {
		t.Fatalf("user actor=%+v", actor)
	}

	client.introspection.Username = ""
	client.introspection.ClientId = "machine-client"
	actor, err = adapter.Authenticate(context.Background(), "machine-token")
	if err != nil {
		t.Fatal(err)
	}
	if actor.ID != "machine-client" || actor.Type != domain.ActorMachine || actor.ClientID != "machine-client" || len(actor.AMR) != 0 {
		t.Fatalf("machine actor=%+v", actor)
	}
}

func TestCasdoorAuthenticateFailsClosed(t *testing.T) {
	now := uint(time.Now().Unix())
	valid := casdoorsdk.IntrospectTokenResult{Active: true, Sub: "subject", Iss: "https://casdoor.example", Aud: []string{"client"}, Exp: now + 60, Nbf: now - 1, Iat: now, Username: "user-a", ClientId: "client"}
	cases := []struct {
		name   string
		mutate func(*fakeCasdoorClient)
	}{
		{"inactive", func(client *fakeCasdoorClient) { client.introspection.Active = false }},
		{"expired", func(client *fakeCasdoorClient) { client.introspection.Exp = now - 1 }},
		{"future", func(client *fakeCasdoorClient) { client.introspection.Nbf = now + 60 }},
		{"issuer", func(client *fakeCasdoorClient) { client.introspection.Iss = "https://other.example" }},
		{"audience", func(client *fakeCasdoorClient) { client.introspection.Aud = []string{"other"} }},
		{"introspection error", func(client *fakeCasdoorClient) { client.err = errors.New("outage") }},
		{"parse error", func(client *fakeCasdoorClient) { client.parseErr = errors.New("invalid signature") }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			client := &fakeCasdoorClient{introspection: &value, claims: &casdoorsdk.Claims{}}
			test.mutate(client)
			if _, err := newCasdoorWithClient(testCasdoorConfig(), client).Authenticate(context.Background(), "token"); !errors.Is(err, ErrInvalidCasdoorToken) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestCasdoorEnforceSnapshotAndRoleReceipt(t *testing.T) {
	client := &fakeCasdoorClient{
		enforce: true,
		policies: []*casdoorsdk.CasbinRule{
			{Ptype: "p", V0: "role::finance", V1: "organization::org-a", V2: "invoice", V3: "approve", V4: "allow"},
		},
		roles:  []*casdoorsdk.Role{{Name: "finance", DisplayName: "Finance", Domains: []string{"organization::org-a"}}},
		groups: []*casdoorsdk.Group{{Name: "reviewers", DisplayName: "Reviewers"}},
	}
	adapter := newCasdoorWithClient(testCasdoorConfig(), client)
	allowed, err := adapter.Enforce(context.Background(), domain.Actor{ID: "user-a", Type: domain.ActorUser}, domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, domain.Operation{ResourceType: "invoice", Action: "approve"})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil || len(snapshot.Rules) != 1 || snapshot.Roles["finance"].Name != "Finance" || snapshot.Groups["reviewers"].Name != "Reviewers" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	receipt, err := adapter.EnsureRoles(context.Background(), domain.Actor{ID: "user-a", Type: domain.ActorUser}, domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, []string{"finance"})
	if err != nil || len(receipt.Added) != 1 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	second, err := adapter.EnsureRoles(context.Background(), domain.Actor{ID: "user-a", Type: domain.ActorUser}, domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, []string{"finance"})
	if err != nil || len(second.Added) != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err = adapter.CompensateRoles(context.Background(), receipt); err != nil || len(client.policies) != 1 {
		t.Fatalf("policies=%+v err=%v", client.policies, err)
	}
}

func TestNewCasdoorRequiresCompleteConfiguration(t *testing.T) {
	value := testCasdoorConfig()
	value.ClientSecret = ""
	if _, err := NewCasdoor(value); !errors.Is(err, ErrInvalidCasdoorConfiguration) {
		t.Fatalf("err=%v", err)
	}
}

func testCasdoorConfig() config.Casdoor {
	return config.Casdoor{Endpoint: "https://casdoor.example", ClientID: "client", ClientSecret: "secret", Certificate: "certificate", Organization: "identity", Application: "app", PermissionID: "permission", ModelID: "model", ResourceID: "adapter", EnforcerID: "enforcer", Owner: "admin", HTTPTimeout: time.Second}
}

type fakeCasdoorClient struct {
	introspection *casdoorsdk.IntrospectTokenResult
	claims        *casdoorsdk.Claims
	policies      []*casdoorsdk.CasbinRule
	roles         []*casdoorsdk.Role
	groups        []*casdoorsdk.Group
	enforce       bool
	err, parseErr error
}

func (f *fakeCasdoorClient) IntrospectToken(string, string) (*casdoorsdk.IntrospectTokenResult, error) {
	return f.introspection, f.err
}
func (f *fakeCasdoorClient) ParseJwtToken(string) (*casdoorsdk.Claims, error) {
	return f.claims, f.parseErr
}
func (f *fakeCasdoorClient) Enforce(string, string, string, string, string, casdoorsdk.CasbinRequest) (bool, error) {
	return f.enforce, f.err
}
func (f *fakeCasdoorClient) GetPolicies(string, string) ([]*casdoorsdk.CasbinRule, error) {
	return f.policies, f.err
}
func (f *fakeCasdoorClient) GetRoles() ([]*casdoorsdk.Role, error)   { return f.roles, f.err }
func (f *fakeCasdoorClient) GetGroups() ([]*casdoorsdk.Group, error) { return f.groups, f.err }
func (f *fakeCasdoorClient) AddPolicy(_ *casdoorsdk.Enforcer, rule *casdoorsdk.CasbinRule) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	for _, current := range f.policies {
		if fromCasbinRule(current) == fromCasbinRule(rule) {
			return false, nil
		}
	}
	f.policies = append(f.policies, rule)
	return true, nil
}
func (f *fakeCasdoorClient) RemovePolicy(_ *casdoorsdk.Enforcer, rule *casdoorsdk.CasbinRule) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	wanted := fromCasbinRule(rule)
	before := len(f.policies)
	f.policies = slices.DeleteFunc(f.policies, func(current *casdoorsdk.CasbinRule) bool {
		return fromCasbinRule(current) == wanted
	})
	return len(f.policies) != before, nil
}
