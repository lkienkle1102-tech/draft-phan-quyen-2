package application

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

func buildRules(facts Facts) map[domain.RuleType]ruleEvaluator {
	return map[domain.RuleType]ruleEvaluator{
		domain.RulePermission: permissionRule(facts), domain.RuleRole: roleRule(facts), domain.RuleGroup: groupRule(facts),
		domain.RuleMember: memberRule(facts), domain.RuleClientGrant: clientGrantRule(facts), domain.RuleConsent: consentRule(facts),
		domain.RuleOwner: ownerRule, domain.RuleSelf: selfRule, domain.RuleAttributeMatch: attributeRule,
		domain.RuleRecentMFA: mfaRule, domain.RuleFeature: featureRule(facts), domain.RulePlan: planRule(facts),
		domain.RuleQuota: quotaRule(facts), domain.RuleAmount: amountRule, domain.RuleTimeWindow: timeRule,
		domain.RuleOrganizationGrant: organizationGrantRule(facts), domain.RuleDelegatedFeature: delegatedFeatureRule(facts),
		domain.RuleDelegatedQuota: delegatedQuotaRule(facts),
	}
}

func permissionRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, _ domain.PolicyNode) (ruleResult, error) {
		if r.ExternalAccess != nil {
			external, ok := f.(ExternalFacts)
			if !ok {
				return basic(false, domain.DenyGrant), nil
			}
			allowed, err := external.ExternalPermission(ctx, *r.ExternalAccess, r.Operation)
			return basic(allowed, domain.DenyPermission), err
		}
		ok, err := f.HasPermission(ctx, r.Actor, r.Subject, r.Operation)
		return basic(ok, domain.DenyPermission), err
	}
}
func memberRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, _ domain.PolicyNode) (ruleResult, error) {
		ok, err := f.IsMember(ctx, r.Actor, r.Subject)
		return basic(ok, domain.DenyPermission), err
	}
}
func clientGrantRule(f Facts) ruleEvaluator {
	return operationRule(f.HasClientGrant)
}
func consentRule(f Facts) ruleEvaluator {
	return operationRule(f.HasConsent)
}
func roleRule(f Facts) ruleEvaluator {
	return externalNamedRule("role", f, f.HasRole, func(ctx context.Context, x ExternalFacts, a domain.ExternalAccess, value string) (bool, error) {
		return x.ExternalRole(ctx, a, value)
	})
}
func groupRule(f Facts) ruleEvaluator {
	return externalNamedRule("group", f, f.InGroup, func(ctx context.Context, x ExternalFacts, a domain.ExternalAccess, value string) (bool, error) {
		return x.ExternalGroup(ctx, a, value)
	})
}

type externalNamedFact func(context.Context, ExternalFacts, domain.ExternalAccess, string) (bool, error)

func externalNamedRule(key string, facts Facts, normal namedFact, external externalNamedFact) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
		value, err := stringValue(n, key)
		if err != nil {
			return ruleResult{}, err
		}
		if r.ExternalAccess != nil {
			x, supported := facts.(ExternalFacts)
			if !supported {
				return basic(false, domain.DenyGrant), nil
			}
			ok, loadErr := external(ctx, x, *r.ExternalAccess, value)
			return basic(ok, domain.DenyPermission), loadErr
		}
		ok, loadErr := normal(ctx, r.Actor, r.Subject, value)
		return basic(ok, domain.DenyPermission), loadErr
	}
}

type operationFact func(context.Context, domain.Actor, domain.Subject, domain.Operation) (bool, error)

func operationRule(check operationFact) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, _ domain.PolicyNode) (ruleResult, error) {
		ok, err := check(ctx, r.Actor, r.Subject, r.Operation)
		return basic(ok, domain.DenyPermission), err
	}
}

type namedFact func(context.Context, domain.Actor, domain.Subject, string) (bool, error)

func ownerRule(_ context.Context, r domain.Request, _ domain.PolicyNode) (ruleResult, error) {
	return basic(r.Primary.OwnerID == r.Actor.ID, domain.DenyPermission), nil
}
func selfRule(_ context.Context, r domain.Request, _ domain.PolicyNode) (ruleResult, error) {
	return basic(r.Primary.ID == r.Actor.ID, domain.DenyPermission), nil
}

func attributeRule(_ context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
	key, err := stringValue(n, "attribute")
	if err != nil {
		return ruleResult{}, err
	}
	return basic(r.Actor.Attributes[key] != "" && r.Actor.Attributes[key] == r.Primary.Attributes[key], domain.DenyPermission), nil
}

func mfaRule(_ context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
	seconds, err := intValue(n, "max_age_seconds")
	if err != nil {
		return ruleResult{}, err
	}
	age := r.Now.Sub(r.Actor.AuthTime)
	ok := slices.Contains(r.Actor.AMR, "mfa") && !r.Actor.AuthTime.IsZero() && age >= 0 && age <= time.Duration(seconds)*time.Second
	return basic(ok, domain.DenyPermission), nil
}

func featureRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
		key, err := stringValue(n, "feature")
		if err != nil {
			return ruleResult{}, err
		}
		if r.Grant != nil {
			ok, loadErr := f.DelegatedFeature(ctx, r.Grant.ID, key)
			return basic(ok, domain.DenyFeature), loadErr
		}
		if r.ExternalAccess != nil {
			x, supported := f.(ExternalFacts)
			if !supported {
				return basic(false, domain.DenyGrant), nil
			}
			ok, loadErr := x.ExternalFeature(ctx, *r.ExternalAccess, key)
			return basic(ok, domain.DenyFeature), loadErr
		}
		ok, loadErr := f.HasFeature(ctx, r.Subject, key)
		return basic(ok, domain.DenyFeature), loadErr
	}
}

func planRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
		key, err := stringValue(n, "plan")
		if err != nil {
			return ruleResult{}, err
		}
		if r.ExternalAccess != nil {
			x, supported := f.(ExternalFacts)
			if !supported {
				return basic(false, domain.DenyGrant), nil
			}
			ok, loadErr := x.ExternalPlan(ctx, *r.ExternalAccess, key)
			return basic(ok, domain.DenyPlan), loadErr
		}
		ok, loadErr := f.HasPlan(ctx, r.Subject, key)
		return basic(ok, domain.DenyFeature), loadErr
	}
}

func quotaRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
		key, cost, err := quotaConfig(n)
		if err != nil {
			return ruleResult{}, err
		}
		if r.Grant != nil {
			ok, loadErr := f.DelegatedQuota(ctx, r.Grant.ID, key, cost)
			return quotaResult(ok, r.Grant.ID, domain.Subject{Type: domain.SubjectOrganization, ID: r.Primary.TenantID}, key, cost), loadErr
		}
		if r.ExternalAccess != nil {
			x, supported := f.(ExternalFacts)
			if !supported {
				return basic(false, domain.DenyGrant), nil
			}
			ok, loadErr := x.ExternalQuota(ctx, *r.ExternalAccess, key, cost)
			return externalQuotaResult(ok, *r.ExternalAccess, key, cost), loadErr
		}
		ok, loadErr := f.QuotaAvailable(ctx, r.Subject, key, cost)
		return quotaResult(ok, "", r.Subject, key, cost), loadErr
	}
}

func externalQuotaResult(ok bool, access domain.ExternalAccess, key string, cost int64) ruleResult {
	result := basic(ok, domain.DenyQuota)
	if ok {
		result.costs = []domain.QuotaCost{{Subject: domain.Subject{Type: domain.SubjectOrganization, ID: access.OwnerOrganizationID}, ExternalGrantIDs: append([]string(nil), access.GrantIDs...), QuotaKey: key, Cost: cost}}
	}
	return result
}

func organizationGrantRule(_ Facts) ruleEvaluator {
	return func(_ context.Context, r domain.Request, _ domain.PolicyNode) (ruleResult, error) {
		return basic(r.Grant != nil, domain.DenyGrant), nil
	}
}
func delegatedFeatureRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
		if r.Grant == nil {
			return basic(false, domain.DenyGrant), nil
		}
		key, err := stringValue(n, "feature")
		if err != nil {
			return ruleResult{}, err
		}
		ok, loadErr := f.DelegatedFeature(ctx, r.Grant.ID, key)
		return basic(ok, domain.DenyFeature), loadErr
	}
}
func delegatedQuotaRule(f Facts) ruleEvaluator {
	return func(ctx context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
		if r.Grant == nil {
			return basic(false, domain.DenyGrant), nil
		}
		key, cost, err := quotaConfig(n)
		if err != nil {
			return ruleResult{}, err
		}
		ok, loadErr := f.DelegatedQuota(ctx, r.Grant.ID, key, cost)
		subject := domain.Subject{Type: domain.SubjectOrganization, ID: r.Primary.TenantID}
		return quotaResult(ok, r.Grant.ID, subject, key, cost), loadErr
	}
}

func amountRule(_ context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
	maximum, err := intValue(n, "maximum")
	if err != nil {
		return ruleResult{}, err
	}
	amount, err := strconv.ParseInt(r.Primary.Attributes["amount"], 10, 64)
	if err != nil {
		return ruleResult{}, err
	}
	return basic(amount <= maximum, domain.DenyPermission), nil
}
func timeRule(_ context.Context, r domain.Request, n domain.PolicyNode) (ruleResult, error) {
	start, err := stringValue(n, "start")
	if err != nil {
		return ruleResult{}, err
	}
	end, err := stringValue(n, "end")
	if err != nil {
		return ruleResult{}, err
	}
	now := r.Now.Format("15:04")
	return basic(now >= start && now <= end, domain.DenyPermission), nil
}

func basic(ok bool, code domain.DenialCode) ruleResult { return ruleResult{allowed: ok, code: code} }
func quotaResult(ok bool, grant string, subject domain.Subject, key string, cost int64) ruleResult {
	result := basic(ok, domain.DenyQuota)
	if ok {
		result.costs = []domain.QuotaCost{{Subject: subject, GrantID: grant, QuotaKey: key, Cost: cost}}
	}
	return result
}
func quotaConfig(node domain.PolicyNode) (string, int64, error) {
	key, err := stringValue(node, "quota")
	if err != nil {
		return "", 0, err
	}
	cost, err := intValue(node, "cost")
	return key, cost, err
}
func stringValue(node domain.PolicyNode, key string) (string, error) {
	value, ok := node.Config[key]
	if !ok || value.String == nil || *value.String == "" {
		return "", fmt.Errorf("%w: %s", ErrInvalidPolicy, key)
	}
	return *value.String, nil
}
func intValue(node domain.PolicyNode, key string) (int64, error) {
	value, ok := node.Config[key]
	if !ok || value.Int == nil || *value.Int <= 0 {
		return 0, fmt.Errorf("%w: %s", ErrInvalidPolicy, key)
	}
	return *value.Int, nil
}
