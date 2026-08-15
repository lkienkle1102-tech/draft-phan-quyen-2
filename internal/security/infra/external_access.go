package infra

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

func (r *Repository) ResolveExternalAccess(ctx context.Context, actor domain.Actor, resource domain.Resource, operation domain.Operation, at time.Time) (*domain.ExternalAccess, error) {
	if actor.Type != domain.ActorUser || actor.ID == "" || resource.TenantID == "" {
		return nil, nil
	}
	rows, err := r.database.QueryContext(ctx, `SELECT g.id,g.effect FROM external_grants_v2 g
		WHERE g.owner_organization_id=? AND g.resource_type=? AND g.action=?
		  AND(g.resource_id IS NULL OR g.resource_id=?) AND g.status='active'
		  AND g.valid_from<=? AND(g.valid_until IS NULL OR g.valid_until>?)
		  AND(
		    (g.target_type='global_user' AND g.target_user_id=?) OR
		    (g.target_type='organization' AND EXISTS(SELECT 1 FROM organization_members m WHERE m.organization_id=g.target_organization_id AND m.user_id=? AND m.active=1)) OR
		    (g.target_type='organization_member' AND g.target_user_id=? AND EXISTS(SELECT 1 FROM organization_members m WHERE m.id=g.target_membership_id AND m.organization_id=g.target_organization_id AND m.user_id=g.target_user_id AND m.active=1))
		  ) ORDER BY g.id`, resource.TenantID, operation.ResourceType, operation.Action, resource.ID,
		at.UTC().Format(time.RFC3339), at.UTC().Format(time.RFC3339), actor.ID, actor.ID, actor.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	denied := false
	for rows.Next() {
		var id string
		var effect domain.Effect
		if err := rows.Scan(&id, &effect); err != nil {
			return nil, err
		}
		if effect == domain.EffectDeny {
			denied = true
		}
		if effect == domain.EffectAllow {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if denied || len(ids) == 0 {
		return nil, nil
	}
	return &domain.ExternalAccess{GrantIDs: ids, OwnerOrganizationID: resource.TenantID, ActorID: actor.ID}, nil
}

func (r *Repository) ExternalPermission(ctx context.Context, access domain.ExternalAccess, operation domain.Operation) (bool, error) {
	clause, args, ok := externalIDs(access)
	if !ok {
		return false, nil
	}
	args = append(args, operation.ResourceType, operation.Action)
	query := `WITH effects AS(
		SELECT item.effect FROM external_grant_permissions_v2 item JOIN permissions p ON p.id=item.permission_id WHERE item.grant_id IN (` + clause + `) AND p.resource_type=? AND p.action=?
		UNION ALL
		SELECT CASE WHEN item.effect='deny' OR rp.effect='deny' THEN 'deny' ELSE 'allow' END FROM external_grant_roles_v2 item JOIN role_permission_rules_v2 rp ON rp.role_id=item.role_id JOIN permissions p ON p.id=rp.permission_id WHERE item.grant_id IN (` + clause + `) AND p.resource_type=? AND p.action=?
		UNION ALL
		SELECT CASE WHEN item.effect='deny' OR gr.effect='deny' OR rp.effect='deny' THEN 'deny' ELSE 'allow' END FROM external_grant_groups_v2 item JOIN group_role_rules_v2 gr ON gr.group_id=item.group_id JOIN role_permission_rules_v2 rp ON rp.role_id=gr.role_id JOIN permissions p ON p.id=rp.permission_id WHERE item.grant_id IN (` + clause + `) AND p.resource_type=? AND p.action=?
	) SELECT EXISTS(SELECT 1 FROM effects WHERE effect='allow') AND NOT EXISTS(SELECT 1 FROM effects WHERE effect='deny')`
	all := append([]any{}, args...)
	all = append(all, accessArgs(access)...)
	all = append(all, operation.ResourceType, operation.Action)
	all = append(all, accessArgs(access)...)
	all = append(all, operation.ResourceType, operation.Action)
	return queryBool(ctx, r.database, query, all...)
}

func (r *Repository) ExternalRole(ctx context.Context, access domain.ExternalAccess, name string) (bool, error) {
	return r.externalNamed(ctx, access, `SELECT item.effect FROM external_grant_roles_v2 item JOIN roles_v2 value ON value.id=item.role_id WHERE item.grant_id IN (%s) AND value.owner_type='organization' AND value.owner_id=? AND value.name=? AND value.active=1`, name)
}
func (r *Repository) ExternalGroup(ctx context.Context, access domain.ExternalAccess, name string) (bool, error) {
	return r.externalNamed(ctx, access, `SELECT item.effect FROM external_grant_groups_v2 item JOIN groups_v2 value ON value.id=item.group_id WHERE item.grant_id IN (%s) AND value.owner_type='organization' AND value.owner_id=? AND value.name=? AND value.active=1`, name)
}
func (r *Repository) externalNamed(ctx context.Context, access domain.ExternalAccess, template, name string) (bool, error) {
	clause, args, ok := externalIDs(access)
	if !ok {
		return false, nil
	}
	args = append(args, access.OwnerOrganizationID, name)
	query := `WITH effects AS(` + fmt.Sprintf(template, clause) + `) SELECT EXISTS(SELECT 1 FROM effects WHERE effect='allow') AND NOT EXISTS(SELECT 1 FROM effects WHERE effect='deny')`
	return queryBool(ctx, r.database, query, args...)
}
func (r *Repository) ExternalFeature(ctx context.Context, access domain.ExternalAccess, key string) (bool, error) {
	return r.externalSimple(ctx, access, "external_grant_features_v2", "feature_key", key)
}
func (r *Repository) ExternalPlan(ctx context.Context, access domain.ExternalAccess, key string) (bool, error) {
	return r.externalSimple(ctx, access, "external_grant_plans_v2", "plan_id", key)
}
func (r *Repository) externalSimple(ctx context.Context, access domain.ExternalAccess, table, column, value string) (bool, error) {
	clause, args, ok := externalIDs(access)
	if !ok {
		return false, nil
	}
	args = append(args, value)
	query := `WITH effects AS(SELECT effect FROM ` + table + ` WHERE grant_id IN (` + clause + `) AND ` + column + `=?) SELECT EXISTS(SELECT 1 FROM effects WHERE effect='allow') AND NOT EXISTS(SELECT 1 FROM effects WHERE effect='deny')`
	return queryBool(ctx, r.database, query, args...)
}
func (r *Repository) ExternalQuota(ctx context.Context, access domain.ExternalAccess, key string, cost int64) (bool, error) {
	clause, args, ok := externalIDs(access)
	if !ok || cost <= 0 {
		return false, nil
	}
	args = append(args, key, cost)
	query := `SELECT EXISTS(SELECT 1 FROM external_grant_quota_allocations_v2 WHERE grant_id IN (` + clause + `) AND quota_key=? AND effect='allow')
		AND NOT EXISTS(SELECT 1 FROM external_grant_quota_allocations_v2 WHERE grant_id IN (` + clause + `) AND quota_key=? AND effect='deny')
		AND COALESCE((SELECT SUM(allocated-used-reserved) FROM external_grant_quota_allocations_v2 WHERE grant_id IN (` + clause + `) AND quota_key=? AND effect='allow'),0)>=?`
	all := append([]any{}, args[:len(args)-2]...)
	all = append(all, key)
	all = append(all, accessArgs(access)...)
	all = append(all, key)
	all = append(all, accessArgs(access)...)
	all = append(all, key, cost)
	return queryBool(ctx, r.database, query, all...)
}

func externalIDs(access domain.ExternalAccess) (string, []any, bool) {
	if len(access.GrantIDs) == 0 {
		return "", nil, false
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(access.GrantIDs)), ","), accessArgs(access), true
}
func accessArgs(access domain.ExternalAccess) []any {
	result := make([]any, len(access.GrantIDs))
	for i, v := range access.GrantIDs {
		result[i] = v
	}
	return result
}
func queryBool(ctx context.Context, db *sql.DB, query string, args ...any) (bool, error) {
	var ok bool
	err := db.QueryRowContext(ctx, query, args...).Scan(&ok)
	return ok, err
}
