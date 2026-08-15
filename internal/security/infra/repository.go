// Package infra implements security persistence with database/sql.
package infra

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
)

type Repository struct{ database *sql.DB }

func NewRepository(database *sql.DB) *Repository { return &Repository{database: database} }

func (r *Repository) FindEndpoint(ctx context.Context, method, route string) (application.EndpointBinding, error) {
	var result application.EndpointBinding
	err := r.database.QueryRowContext(ctx, `SELECT id,method,route_template,resource_loader,intent_resolver,resource_type,action,policy_id,policy_version,scope_mode FROM endpoint_bindings WHERE method=? AND route_template=? AND active=1 AND scope_mode IN('user','organization') AND allow_personal_fallback=0`, method, route).Scan(&result.ID, &result.Method, &result.Route, &result.Loader, &result.Intent, &result.Operation.ResourceType, &result.Operation.Action, &result.PolicyID, &result.PolicyVersion, &result.ScopeMode)
	return result, err
}

func (r *Repository) LoadPolicy(ctx context.Context, id string, version int64) (domain.Policy, error) {
	var active bool
	if err := r.database.QueryRowContext(ctx, `SELECT active FROM authorization_policies WHERE id=? AND version=?`, id, version).Scan(&active); err != nil {
		return domain.Policy{}, err
	}
	if !active {
		return domain.Policy{}, errors.New("inactive policy")
	}
	result := domain.Policy{ID: id, Version: version, Nodes: map[string]domain.PolicyNode{}, Children: map[string][]string{}}
	rows, err := r.database.QueryContext(ctx, `SELECT id,COALESCE(parent_id,''),node_type,COALESCE(rule_type,''),config_json,position,purpose FROM policy_nodes WHERE policy_id=? AND policy_version=? ORDER BY position,id`, id, version)
	if err != nil {
		return domain.Policy{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var node domain.PolicyNode
		var raw string
		if err := rows.Scan(&node.ID, &node.ParentID, &node.Type, &node.Rule, &raw, &node.Position, &node.Purpose); err != nil {
			return domain.Policy{}, err
		}
		if err := json.Unmarshal([]byte(raw), &node.Config); err != nil {
			return domain.Policy{}, err
		}
		result.Nodes[node.ID] = node
		if node.ParentID == "" && node.Purpose == "allow" {
			result.RootID = node.ID
		} else {
			result.Children[node.ParentID] = append(result.Children[node.ParentID], node.ID)
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Policy{}, err
	}
	if err := r.loadBehaviors(ctx, &result); err != nil {
		return domain.Policy{}, err
	}
	if err := r.loadDenials(ctx, &result); err != nil {
		return domain.Policy{}, err
	}
	return result, nil
}

func (r *Repository) loadDenials(ctx context.Context, policy *domain.Policy) error {
	rows, err := r.database.QueryContext(ctx, `SELECT root_node_id,denial_code FROM policy_denials_v2 WHERE policy_id=? AND policy_version=?`, policy.ID, policy.Version)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var denial domain.PolicyDenial
		if err := rows.Scan(&denial.RootID, &denial.Code); err != nil {
			return err
		}
		policy.Denials = append(policy.Denials, denial)
	}
	return rows.Err()
}

func (r *Repository) loadBehaviors(ctx context.Context, policy *domain.Policy) error {
	rows, err := r.database.QueryContext(ctx, `SELECT condition_root_id,strategy,priority,parameters_json,obligations_json FROM policy_behaviors_v2 WHERE policy_id=? AND policy_version=? ORDER BY priority`, policy.ID, policy.Version)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var behavior domain.Behavior
		var parameters, obligations string
		if err := rows.Scan(&behavior.ConditionRoot, &behavior.Strategy, &behavior.Priority, &parameters, &obligations); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(parameters), &behavior.Parameters); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(obligations), &behavior.Obligations); err != nil {
			return err
		}
		policy.Behaviors = append(policy.Behaviors, behavior)
	}
	return rows.Err()
}

func (r *Repository) ActorActive(ctx context.Context, actor domain.Actor) (bool, error) {
	query, id := `SELECT active FROM users WHERE id=?`, actor.ID
	if actor.Type == domain.ActorMachine {
		query, id = `SELECT active FROM oauth_clients WHERE id=?`, actor.ClientID
	}
	var active bool
	err := r.database.QueryRowContext(ctx, query, id).Scan(&active)
	return active, err
}

func (r *Repository) HasPermission(ctx context.Context, actor domain.Actor, subject domain.Subject, operation domain.Operation) (bool, error) {
	if actor.Type == domain.ActorMachine {
		return r.machinePermission(ctx, actor, subject, operation)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	const query = `WITH wanted AS(
	SELECT id FROM permissions WHERE resource_type=? AND action=?
	),effects AS(
	SELECT rule.permission_id,
	       CASE WHEN assignment.effect='deny' OR rule.effect='deny' THEN 'deny' ELSE 'allow' END effect
	FROM role_assignments_v2 assignment
	JOIN roles_v2 role ON role.id=assignment.role_id
	JOIN role_permission_rules_v2 rule ON rule.role_id=role.id
	WHERE assignment.user_id=? AND assignment.subject_type=? AND assignment.subject_id=?
	  AND role.owner_type=assignment.subject_type AND role.owner_id=assignment.subject_id
	  AND role.active=1 AND assignment.valid_from<=?
	  AND(assignment.valid_until IS NULL OR assignment.valid_until>?)
	UNION ALL
	SELECT permission_id,effect FROM subject_permission_rules_v2
	WHERE user_id=? AND subject_type=? AND subject_id=? AND valid_from<=?
	  AND(valid_until IS NULL OR valid_until>?)
	UNION ALL
	SELECT permission.permission_id,
	       CASE WHEN membership.effect='deny' OR group_role.effect='deny' OR permission.effect='deny' THEN 'deny' ELSE 'allow' END
	FROM group_memberships_v2 membership
	JOIN groups_v2 group_row ON group_row.id=membership.group_id
	JOIN group_role_rules_v2 group_role ON group_role.group_id=group_row.id
	JOIN roles_v2 role ON role.id=group_role.role_id
	JOIN role_permission_rules_v2 permission ON permission.role_id=role.id
	WHERE membership.user_id=? AND group_row.owner_type=? AND group_row.owner_id=?
	  AND group_row.active=1 AND role.active=1
	  AND membership.valid_from<=? AND(membership.valid_until IS NULL OR membership.valid_until>?)
	)
	SELECT EXISTS(
	SELECT 1 FROM wanted
	WHERE EXISTS(SELECT 1 FROM effects WHERE effects.permission_id=wanted.id AND effect='allow')
	  AND NOT EXISTS(SELECT 1 FROM effects WHERE effects.permission_id=wanted.id AND effect='deny')
	)`
	var ok bool
	err := r.database.QueryRowContext(ctx, query,
		operation.ResourceType, operation.Action,
		actor.ID, subject.Type, subject.ID, now, now,
		actor.ID, subject.Type, subject.ID, now, now,
		actor.ID, subject.Type, subject.ID, now, now,
	).Scan(&ok)
	return ok, err
}

func (r *Repository) machinePermission(ctx context.Context, actor domain.Actor, subject domain.Subject, operation domain.Operation) (bool, error) {
	var ok bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM client_grants cg JOIN permissions p ON p.id=cg.permission_id WHERE cg.client_id=? AND cg.subject_type=? AND cg.subject_id=? AND cg.active=1 AND p.resource_type=? AND p.action=?)`, actor.ClientID, subject.Type, subject.ID, operation.ResourceType, operation.Action).Scan(&ok)
	return ok, err
}
func (r *Repository) HasRole(ctx context.Context, a domain.Actor, s domain.Subject, value string) (bool, error) {
	const query = `SELECT EXISTS(
		SELECT 1 FROM role_assignments_v2 assignment
		JOIN roles_v2 role ON role.id=assignment.role_id
		WHERE assignment.user_id=? AND assignment.subject_type=? AND assignment.subject_id=?
		  AND role.owner_type=assignment.subject_type AND role.owner_id=assignment.subject_id
		  AND role.name=? AND role.active=1 AND assignment.effect='allow'
		  AND assignment.valid_from<=? AND(assignment.valid_until IS NULL OR assignment.valid_until>?)
		  AND NOT EXISTS(
		      SELECT 1 FROM role_assignments_v2 denied
		      JOIN roles_v2 denied_role ON denied_role.id=denied.role_id
		      WHERE denied.user_id=assignment.user_id
		        AND denied.subject_type=assignment.subject_type AND denied.subject_id=assignment.subject_id
		        AND denied_role.name=role.name AND denied.effect='deny'
		        AND denied.valid_from<=? AND(denied.valid_until IS NULL OR denied.valid_until>?)
		)
	)`
	return r.hasNamedAssignment(ctx, query, a, s, value)
}
func (r *Repository) InGroup(ctx context.Context, a domain.Actor, s domain.Subject, value string) (bool, error) {
	const query = `SELECT EXISTS(
		SELECT 1 FROM group_memberships_v2 membership
		JOIN groups_v2 group_row ON group_row.id=membership.group_id
		WHERE membership.user_id=? AND group_row.owner_type=? AND group_row.owner_id=?
		  AND group_row.name=? AND group_row.active=1 AND membership.effect='allow'
		  AND membership.valid_from<=? AND(membership.valid_until IS NULL OR membership.valid_until>?)
		  AND NOT EXISTS(
		      SELECT 1 FROM group_memberships_v2 denied
		      JOIN groups_v2 denied_group ON denied_group.id=denied.group_id
		      WHERE denied.user_id=membership.user_id
		        AND denied_group.owner_type=group_row.owner_type AND denied_group.owner_id=group_row.owner_id
		        AND denied_group.name=group_row.name AND denied.effect='deny'
		        AND denied.valid_from<=? AND(denied.valid_until IS NULL OR denied.valid_until>?)
		)
	)`
	return r.hasNamedAssignment(ctx, query, a, s, value)
}

func (r *Repository) hasNamedAssignment(ctx context.Context, query string, actor domain.Actor, subject domain.Subject, value string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var ok bool
	err := r.database.QueryRowContext(ctx, query, actor.ID, subject.Type, subject.ID, value, now, now, now, now).Scan(&ok)
	return ok, err
}
func (r *Repository) IsMember(ctx context.Context, a domain.Actor, s domain.Subject) (bool, error) {
	if s.Type == domain.SubjectUser {
		return a.ID == s.ID, nil
	}
	var ok bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=? AND user_id=? AND active=1)`, s.ID, a.ID).Scan(&ok)
	return ok, err
}
func (r *Repository) HasClientGrant(ctx context.Context, a domain.Actor, s domain.Subject, o domain.Operation) (bool, error) {
	if a.ClientID == "" {
		return false, nil
	}
	return r.machinePermission(ctx, domain.Actor{Type: domain.ActorMachine, ClientID: a.ClientID}, s, o)
}
func (r *Repository) HasConsent(ctx context.Context, a domain.Actor, s domain.Subject, o domain.Operation) (bool, error) {
	var ok bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_consents c JOIN permissions p ON p.id=c.permission_id WHERE c.user_id=? AND c.client_id=? AND c.subject_type=? AND c.subject_id=? AND c.active=1 AND p.resource_type=? AND p.action=?)`, a.ID, a.ClientID, s.Type, s.ID, o.ResourceType, o.Action).Scan(&ok)
	return ok, err
}
func (r *Repository) HasFeature(ctx context.Context, s domain.Subject, key string) (bool, error) {
	var ok bool
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.database.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM subject_feature_entitlements_v2 WHERE subject_type=? AND subject_id=? AND feature_key=? AND effect='allow' AND valid_from<=? AND(valid_until IS NULL OR valid_until>?))
		AND NOT EXISTS(SELECT 1 FROM subject_feature_entitlements_v2 WHERE subject_type=? AND subject_id=? AND feature_key=? AND effect='deny' AND valid_from<=? AND(valid_until IS NULL OR valid_until>?))`,
		s.Type, s.ID, key, now, now, s.Type, s.ID, key, now, now).Scan(&ok)
	return ok, err
}
func (r *Repository) HasPlan(ctx context.Context, s domain.Subject, key string) (bool, error) {
	var ok bool
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM subscriptions_v2 subscription
		JOIN plans_v2 plan ON plan.id=subscription.plan_id
		WHERE subscription.subject_type=? AND subscription.subject_id=?
		  AND plan.id=? AND subscription.effect='allow'
		  AND subscription.status IN('trialing','active') AND plan.active=1
		  AND subscription.valid_from<=? AND(subscription.valid_until IS NULL OR subscription.valid_until>?)
		  AND subscription.current_period_start<=? AND subscription.current_period_end>?
		  AND plan.valid_from<=? AND(plan.valid_until IS NULL OR plan.valid_until>?)
	)`, s.Type, s.ID, key, now, now, now, now, now, now).Scan(&ok)
	return ok, err
}
func (r *Repository) QuotaAvailable(ctx context.Context, s domain.Subject, key string, cost int64) (bool, error) {
	var ok bool
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.database.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow' AND period_start<=? AND(period_end IS NULL OR period_end>?))
		AND NOT EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='deny' AND period_start<=? AND(period_end IS NULL OR period_end>?))
		AND(
			EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow' AND quota_limit IS NULL AND period_start<=? AND(period_end IS NULL OR period_end>?))
			OR COALESCE((SELECT SUM(quota_limit-used-reserved) FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow' AND quota_limit IS NOT NULL AND period_start<=? AND(period_end IS NULL OR period_end>?)),0)>=?
		)`,
		s.Type, s.ID, key, now, now,
		s.Type, s.ID, key, now, now,
		s.Type, s.ID, key, now, now,
		s.Type, s.ID, key, now, now, cost,
	).Scan(&ok)
	return ok, err
}
func (r *Repository) FindGrant(ctx context.Context, g domain.GrantRequest) (domain.OrganizationGrant, bool, error) {
	var result domain.OrganizationGrant
	err := r.database.QueryRowContext(ctx, `SELECT grant_row.id,grant_row.owner_organization_id,grant_row.grantee_organization_id,COALESCE(grant_row.grantee_user_id,'') FROM organization_access_grants grant_row JOIN organization_members member ON member.organization_id=grant_row.grantee_organization_id AND member.user_id=? AND member.active=1 WHERE grant_row.owner_organization_id=? AND grant_row.grantee_organization_id=? AND(grant_row.grantee_user_id IS NULL OR grant_row.grantee_user_id=?) AND grant_row.resource_type=? AND grant_row.action=? AND(grant_row.resource_id IS NULL OR grant_row.resource_id=?) AND grant_row.status='active' AND grant_row.valid_from<=? AND(grant_row.valid_until IS NULL OR grant_row.valid_until>?) LIMIT 1`, g.ActorID, g.OwnerOrganizationID, g.GranteeOrganizationID, g.ActorID, g.Resource.Type, g.Operation.Action, g.Resource.ID, g.At.Format(time.RFC3339), g.At.Format(time.RFC3339)).Scan(&result.ID, &result.OwnerOrganizationID, &result.GranteeOrganizationID, &result.GranteeUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	return result, err == nil, err
}
func (r *Repository) DelegatedFeature(ctx context.Context, grant, key string) (bool, error) {
	var ok bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organization_feature_grants WHERE access_grant_id=? AND feature_key=?)`, grant, key).Scan(&ok)
	return ok, err
}
func (r *Repository) DelegatedQuota(ctx context.Context, grant, key string, cost int64) (bool, error) {
	var ok bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organization_quota_allocations WHERE access_grant_id=? AND quota_key=? AND used+reserved+?<=allocated)`, grant, key, cost).Scan(&ok)
	return ok, err
}
