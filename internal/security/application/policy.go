package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"example.com/phan-quyen-golang/internal/security/domain"
)

var ErrInvalidPolicy = errors.New("invalid policy")

type ruleResult struct {
	allowed bool
	code    domain.DenialCode
	costs   []domain.QuotaCost
}
type ruleEvaluator func(context.Context, domain.Request, domain.PolicyNode) (ruleResult, error)

type SoftEngine struct {
	policies PolicyRepository
	rules    map[domain.RuleType]ruleEvaluator
}

func NewSoftEngine(policies PolicyRepository, facts Facts) *SoftEngine {
	engine := &SoftEngine{policies: policies}
	engine.rules = buildRules(facts)
	return engine
}

func (e *SoftEngine) Decide(ctx context.Context, request domain.Request) (domain.Decision, error) {
	return e.decideSubject(ctx, request)
}

func (e *SoftEngine) decideSubject(ctx context.Context, request domain.Request) (domain.Decision, error) {
	policy, err := e.policies.LoadPolicy(ctx, request.PolicyID, request.PolicyVersion)
	if err != nil {
		return domain.Deny(domain.DenyPolicy), err
	}
	if err := validatePolicy(policy); err != nil {
		return domain.Deny(domain.DenyPolicy), err
	}
	for _, denial := range policy.Denials {
		matched, denialErr := e.evaluateNode(ctx, request, policy, denial.RootID)
		if denialErr != nil {
			return domain.Deny(domain.DenyPolicy), denialErr
		}
		if matched.allowed {
			return domain.Deny(denial.Code), nil
		}
	}
	result, err := e.evaluateNode(ctx, request, policy, policy.RootID)
	if err != nil {
		return domain.Deny(domain.DenyPolicy), err
	}
	if !result.allowed {
		return domain.Deny(result.code), nil
	}
	decision := domain.Allow(request)
	decision.QuotaCosts = combineCosts(result.costs)
	return e.applyBehavior(ctx, request, policy, decision)
}

func (e *SoftEngine) evaluateNode(ctx context.Context, request domain.Request, policy domain.Policy, id string) (ruleResult, error) {
	node := policy.Nodes[id]
	switch node.Type {
	case domain.NodeAll:
		return e.evaluateAll(ctx, request, policy, policy.Children[id])
	case domain.NodeAny:
		return e.evaluateAny(ctx, request, policy, policy.Children[id])
	case domain.NodeNot:
		result, err := e.evaluateNode(ctx, request, policy, policy.Children[id][0])
		return ruleResult{allowed: !result.allowed, code: domain.DenyPermission}, err
	case domain.NodeRule:
		evaluator, found := e.rules[node.Rule]
		if !found {
			return ruleResult{}, ErrInvalidPolicy
		}
		return evaluator(ctx, request, node)
	default:
		return ruleResult{}, ErrInvalidPolicy
	}
}

func (e *SoftEngine) evaluateAll(ctx context.Context, request domain.Request, policy domain.Policy, children []string) (ruleResult, error) {
	result := ruleResult{allowed: true}
	for _, child := range children {
		current, err := e.evaluateNode(ctx, request, policy, child)
		if err != nil || !current.allowed {
			return current, err
		}
		result.costs = append(result.costs, current.costs...)
	}
	return result, nil
}

func (e *SoftEngine) evaluateAny(ctx context.Context, request domain.Request, policy domain.Policy, children []string) (ruleResult, error) {
	last := ruleResult{code: domain.DenyPermission}
	for _, child := range children {
		current, err := e.evaluateNode(ctx, request, policy, child)
		if err != nil {
			return ruleResult{}, err
		}
		if current.allowed {
			return current, nil
		}
		last = current
	}
	return last, nil
}

func (e *SoftEngine) applyBehavior(ctx context.Context, request domain.Request, policy domain.Policy, decision domain.Decision) (domain.Decision, error) {
	behaviors := append([]domain.Behavior(nil), policy.Behaviors...)
	sort.SliceStable(behaviors, func(i, j int) bool { return behaviors[i].Priority < behaviors[j].Priority })
	for _, behavior := range behaviors {
		result, err := e.evaluateNode(ctx, request, policy, behavior.ConditionRoot)
		if err != nil {
			return domain.Deny(domain.DenyPolicy), err
		}
		if result.allowed {
			decision.Strategy, decision.Parameters, decision.Obligations = behavior.Strategy, behavior.Parameters, behavior.Obligations
			break
		}
	}
	return decision, nil
}

func validatePolicy(policy domain.Policy) error {
	root, found := policy.Nodes[policy.RootID]
	if !found || root.ParentID != "" {
		return ErrInvalidPolicy
	}
	state := map[string]uint8{}
	if err := validateNode(policy, policy.RootID, state); err != nil {
		return err
	}
	for _, behavior := range policy.Behaviors {
		if err := validateNode(policy, behavior.ConditionRoot, state); err != nil {
			return err
		}
	}
	for _, denial := range policy.Denials {
		if err := validateNode(policy, denial.RootID, state); err != nil {
			return err
		}
	}
	if len(state) != len(policy.Nodes) {
		return ErrInvalidPolicy
	}
	return nil
}

func validateNode(policy domain.Policy, id string, state map[string]uint8) error {
	if state[id] == 1 {
		return fmt.Errorf("%w: cycle", ErrInvalidPolicy)
	}
	if state[id] == 2 {
		return nil
	}
	node, found := policy.Nodes[id]
	if !found || !validShape(node, policy.Children[id]) {
		return ErrInvalidPolicy
	}
	state[id] = 1
	for _, child := range policy.Children[id] {
		childNode, exists := policy.Nodes[child]
		if !exists || childNode.ParentID != id {
			return ErrInvalidPolicy
		}
		if err := validateNode(policy, child, state); err != nil {
			return err
		}
	}
	state[id] = 2
	return nil
}

func validShape(node domain.PolicyNode, children []string) bool {
	switch node.Type {
	case domain.NodeAll, domain.NodeAny:
		return len(children) > 0
	case domain.NodeNot:
		return len(children) == 1
	case domain.NodeRule:
		return len(children) == 0 && node.Rule != ""
	default:
		return false
	}
}

func combineCosts(costs []domain.QuotaCost) []domain.QuotaCost {
	type key struct {
		kind                            domain.SubjectType
		subject, grant, external, quota string
	}
	combined := map[key]int64{}
	for _, cost := range costs {
		combined[key{cost.Subject.Type, cost.Subject.ID, cost.GrantID, strings.Join(cost.ExternalGrantIDs, "\x1f"), cost.QuotaKey}] += cost.Cost
	}
	result := make([]domain.QuotaCost, 0, len(combined))
	for item, cost := range combined {
		external := []string(nil)
		if item.external != "" {
			external = strings.Split(item.external, "\x1f")
		}
		result = append(result, domain.QuotaCost{Subject: domain.Subject{Type: item.kind, ID: item.subject}, GrantID: item.grant, ExternalGrantIDs: external, QuotaKey: item.quota, Cost: cost})
	}
	return result
}
