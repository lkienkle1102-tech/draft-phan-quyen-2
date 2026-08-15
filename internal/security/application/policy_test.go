package application

import (
	"context"
	"errors"
	"testing"

	"example.com/phan-quyen-golang/internal/security/domain"
)

func TestValidatePolicyRejectsCyclesAndUnreachableNodes(t *testing.T) {
	t.Parallel()
	policy := testPolicy()
	policy.Children["permission"] = []string{"root"}
	if validatePolicy(policy) == nil {
		t.Fatal("cycle accepted")
	}
	policy = testPolicy()
	policy.Nodes["orphan"] = domain.PolicyNode{ID: "orphan", Type: domain.NodeRule, Rule: domain.RulePermission}
	if validatePolicy(policy) == nil {
		t.Fatal("unreachable node accepted")
	}
}

func TestSoftPolicyFailsClosedWhenPolicyLoadingFails(t *testing.T) {
	t.Parallel()
	engine := NewSoftEngine(failingPolicies{}, &fakeFacts{})
	request := domain.Request{
		PolicyID: "broken", PolicyVersion: 1,
		Subject: domain.Subject{Type: domain.SubjectOrganization, ID: "org"},
	}
	decision, err := engine.Decide(context.Background(), request)
	if err == nil || decision.Code != domain.DenyPolicy {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}
func TestSoftPolicyRequiresPermissionFeatureAndQuota(t *testing.T) {
	t.Parallel()
	facts := &fakeFacts{permission: true, feature: true, quota: true}
	engine := NewSoftEngine(fakePolicies{policy: testPolicy()}, facts)
	request := domain.Request{PolicyID: "test", PolicyVersion: 1, Subject: domain.Subject{Type: domain.SubjectOrganization, ID: "org"}, Operation: domain.Operation{ResourceType: "invoice", Action: "approve"}}
	decision, err := engine.Decide(context.Background(), request)
	if err != nil || !decision.Allowed || len(decision.QuotaCosts) != 1 {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	facts.permission = false
	decision, err = engine.Decide(context.Background(), request)
	if err != nil || decision.Code != domain.DenyPermission {
		t.Fatalf("denial=%+v err=%v", decision, err)
	}
}

func TestSoftPolicyEvaluatesOnlySelectedSubject(t *testing.T) {
	t.Parallel()
	facts := &fakeFacts{
		feature: true,
		quota:   true,
		permissions: map[domain.Subject]bool{
			{Type: domain.SubjectOrganization, ID: "org"}: false,
			{Type: domain.SubjectUser, ID: "user"}:        true,
		},
	}
	engine := NewSoftEngine(fakePolicies{policy: testPolicy()}, facts)
	request := domain.Request{
		PolicyID: "test", PolicyVersion: 1,
		Subject:   domain.Subject{Type: domain.SubjectOrganization, ID: "org"},
		Operation: domain.Operation{ResourceType: "invoice", Action: "approve"},
	}
	decision, err := engine.Decide(context.Background(), request)
	if err != nil || decision.Allowed || decision.Code != domain.DenyPermission {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestExplicitPolicyDenialWinsOverAllowTree(t *testing.T) {
	t.Parallel()
	policy := testPolicy()
	policy.Denials = []domain.PolicyDenial{{RootID: policy.RootID, Code: domain.DenyPolicy}}
	engine := NewSoftEngine(fakePolicies{policy: policy}, &fakeFacts{permission: true, feature: true, quota: true})
	request := domain.Request{PolicyID: "test", PolicyVersion: 1, Subject: domain.Subject{Type: domain.SubjectOrganization, ID: "org"}, Operation: domain.Operation{ResourceType: "invoice", Action: "approve"}}
	decision, err := engine.Decide(context.Background(), request)
	if err != nil || decision.Allowed || decision.Code != domain.DenyPolicy {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestHardPolicyRequiresActiveOrganizationMembership(t *testing.T) {
	t.Parallel()
	facts := &fakeFacts{member: false}
	engine := NewHardEngine(facts)
	request := domain.Request{
		Actor:     domain.Actor{ID: "user", Type: domain.ActorUser, OrganizationID: "org"},
		Subject:   domain.Subject{Type: domain.SubjectOrganization, ID: "org"},
		TenantID:  "org",
		Primary:   domain.Resource{Type: "invoice", ID: "invoice", TenantID: "org"},
		Operation: domain.Operation{ResourceType: "invoice", Action: "approve"},
	}
	contract := domain.EndpointContract{Operation: request.Operation, ActorConstraint: domain.UserOnly, RequireTenant: true, RequireResourceTenant: true, RequireOrganizationMembership: true}
	_, decision, err := engine.Evaluate(context.Background(), request, contract)
	if err != nil || decision.Code != domain.DenyMembership {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}
func testPolicy() domain.Policy {
	feature := "invoice"
	quota := "approvals"
	cost := int64(1)
	nodes := map[string]domain.PolicyNode{"root": {ID: "root", Type: domain.NodeAll}, "permission": {ID: "permission", ParentID: "root", Type: domain.NodeRule, Rule: domain.RulePermission}, "feature": {ID: "feature", ParentID: "root", Type: domain.NodeRule, Rule: domain.RuleFeature, Config: map[string]domain.Value{"feature": {String: &feature}}}, "quota": {ID: "quota", ParentID: "root", Type: domain.NodeRule, Rule: domain.RuleQuota, Config: map[string]domain.Value{"quota": {String: &quota}, "cost": {Int: &cost}}}}
	return domain.Policy{ID: "test", Version: 1, RootID: "root", Nodes: nodes, Children: map[string][]string{"root": {"permission", "feature", "quota"}}}
}

type fakePolicies struct{ policy domain.Policy }

func (f fakePolicies) LoadPolicy(context.Context, string, int64) (domain.Policy, error) {
	return f.policy, nil
}

type failingPolicies struct{}

func (failingPolicies) LoadPolicy(context.Context, string, int64) (domain.Policy, error) {
	return domain.Policy{}, errors.New("load failed")
}

type fakeFacts struct {
	permission, feature, quota bool
	member                     bool
	permissions                map[domain.Subject]bool
}

func (f *fakeFacts) ActorActive(context.Context, domain.Actor) (bool, error) { return true, nil }

func (f *fakeFacts) HasPermission(_ context.Context, _ domain.Actor, subject domain.Subject, _ domain.Operation) (bool, error) {
	if f.permissions != nil {
		return f.permissions[subject], nil
	}
	return f.permission, nil
}
func (f *fakeFacts) HasRole(context.Context, domain.Actor, domain.Subject, string) (bool, error) {
	return false, nil
}
func (f *fakeFacts) InGroup(context.Context, domain.Actor, domain.Subject, string) (bool, error) {
	return false, nil
}
func (f *fakeFacts) IsMember(_ context.Context, _ domain.Actor, subject domain.Subject) (bool, error) {
	if f.member {
		return true, nil
	}
	return false, nil
}
func (f *fakeFacts) HasClientGrant(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error) {
	return false, nil
}
func (f *fakeFacts) HasConsent(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error) {
	return false, nil
}
func (f *fakeFacts) HasFeature(context.Context, domain.Subject, string) (bool, error) {
	return f.feature, nil
}
func (f *fakeFacts) HasPlan(context.Context, domain.Subject, string) (bool, error) { return false, nil }
func (f *fakeFacts) QuotaAvailable(context.Context, domain.Subject, string, int64) (bool, error) {
	return f.quota, nil
}
func (f *fakeFacts) FindGrant(context.Context, domain.GrantRequest) (domain.OrganizationGrant, bool, error) {
	return domain.OrganizationGrant{}, false, nil
}
func (f *fakeFacts) DelegatedFeature(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f *fakeFacts) DelegatedQuota(context.Context, string, string, int64) (bool, error) {
	return false, nil
}
