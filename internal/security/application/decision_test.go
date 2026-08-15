package application

import (
	"context"
	"errors"
	"testing"

	"example.com/phan-quyen-golang/internal/security/domain"
)

func TestDecisionOrderPermissionFeatureQuotaAndBehavior(t *testing.T) {
	facts := &decisionFacts{member: true, feature: true, quota: true}
	permission := &decisionPermission{allowed: true}
	engine := NewEngine(NewHardEngine(facts), permission, facts)
	reason := "low_value_manual_review"
	request := domain.Request{
		Actor:       domain.Actor{ID: "user-a", Type: domain.ActorUser},
		Subject:     domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"},
		Operation:   domain.Operation{ResourceType: "invoice", Action: "approve"},
		Primary:     domain.Resource{Type: "invoice", ID: "invoice-low", Attributes: map[string]string{"amount": "10000"}},
		Requirement: domain.Requirement{RequirePermission: true, FeatureKey: "invoice_management", QuotaKey: "invoice_approvals", QuotaCost: 1, Behavior: &domain.BehaviorRequirement{Attribute: "amount", Maximum: 30000, Strategy: "approve", Parameters: map[string]domain.Value{"reason": {String: &reason}}, Obligations: []domain.Obligation{{Type: "require_manual_review"}}}},
	}
	contract := domain.EndpointContract{Operation: request.Operation, ActorConstraint: domain.UserOnly}
	_, decision, err := engine.Authorize(context.Background(), request, contract)
	if err != nil || !decision.Allowed || decision.Strategy != "approve" || len(decision.QuotaCosts) != 1 || decision.QuotaCosts[0].Cost != 1 {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}

	permission.allowed = false
	_, decision, err = engine.Authorize(context.Background(), request, contract)
	if err != nil || decision.Code != domain.DenyPermission || facts.featureCalls != 1 {
		t.Fatalf("permission decision=%+v err=%v featureCalls=%d", decision, err, facts.featureCalls)
	}
	permission.allowed = true
	facts.feature = false
	_, decision, err = engine.Authorize(context.Background(), request, contract)
	if err != nil || decision.Code != domain.DenyFeature || facts.quotaCalls != 1 {
		t.Fatalf("feature decision=%+v err=%v quotaCalls=%d", decision, err, facts.quotaCalls)
	}
	facts.feature = true
	facts.quota = false
	_, decision, err = engine.Authorize(context.Background(), request, contract)
	if err != nil || decision.Code != domain.DenyQuota {
		t.Fatalf("quota decision=%+v err=%v", decision, err)
	}
}

func TestDecisionFailsClosedOnEnforcerError(t *testing.T) {
	facts := &decisionFacts{member: true, feature: true, quota: true}
	engine := NewEngine(NewHardEngine(facts), &decisionPermission{err: errors.New("Casdoor unavailable")}, facts)
	request := domain.Request{Actor: domain.Actor{ID: "user-a", Type: domain.ActorUser}, Subject: domain.Subject{Type: domain.SubjectUser, ID: "user-a"}, Operation: domain.Operation{ResourceType: "identity", Action: "read_self"}, Primary: domain.Resource{ID: "user-a"}, Requirement: domain.Requirement{RequirePermission: true}}
	_, decision, err := engine.Authorize(context.Background(), request, domain.EndpointContract{Operation: request.Operation, ActorConstraint: domain.UserOnly})
	if err == nil || decision.Code != domain.DenyPolicy {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

type decisionPermission struct {
	allowed bool
	err     error
}

func (f *decisionPermission) Enforce(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error) {
	return f.allowed, f.err
}

type decisionFacts struct {
	member, feature, quota   bool
	featureCalls, quotaCalls int
}

func (f *decisionFacts) IsMember(context.Context, domain.Actor, domain.Subject) (bool, error) {
	return f.member, nil
}
func (f *decisionFacts) HasFeature(context.Context, domain.Subject, string) (bool, error) {
	f.featureCalls++
	return f.feature, nil
}
func (f *decisionFacts) HasPlan(context.Context, domain.Subject, string) (bool, error) {
	return false, nil
}
func (f *decisionFacts) QuotaAvailable(context.Context, domain.Subject, string, int64) (bool, error) {
	f.quotaCalls++
	return f.quota, nil
}
