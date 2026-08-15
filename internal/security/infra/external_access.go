package infra

import (
	"context"
	"database/sql"
	"errors"
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
	allowed, denied, err := r.externalDirectPermission(ctx, access, operation)
	if err != nil {
		return false, err
	}
	directoryAllowed, directoryDenied, err := r.externalDirectoryPermission(ctx, access, operation)
	if err != nil {
		return false, err
	}
	return (allowed || directoryAllowed) && !denied && !directoryDenied, nil
}

func (r *Repository) externalDirectPermission(ctx context.Context, access domain.ExternalAccess, operation domain.Operation) (bool, bool, error) {
	clause, args, ok := externalIDs(access)
	if !ok {
		return false, false, nil
	}
	rows, err := r.database.QueryContext(ctx, `SELECT item.effect FROM external_grant_permissions_v2 item JOIN permissions permission ON permission.id=item.permission_id WHERE item.grant_id IN (`+clause+`) AND permission.resource_type=? AND permission.action=?`, append(args, operation.ResourceType, operation.Action)...)
	if err != nil {
		return false, false, err
	}
	return readEffects(rows)
}

func (r *Repository) externalDirectoryPermission(ctx context.Context, access domain.ExternalAccess, operation domain.Operation) (bool, bool, error) {
	items, err := r.externalDirectoryItems(ctx, access)
	if err != nil {
		return false, false, err
	}
	if len(items) == 0 {
		return false, false, nil
	}
	if r.directory == nil {
		return false, false, errors.New("authorization directory is required")
	}
	snapshot, err := r.directory.Snapshot(ctx)
	if err != nil {
		return false, false, err
	}
	allowed, denied := false, false
	for _, item := range items {
		for _, policy := range domain.PoliciesForPrincipal(snapshot.Rules, item.kind+"::"+item.id, "organization::"+access.OwnerOrganizationID) {
			if policy.V2 == operation.ResourceType && policy.V3 == operation.Action {
				allowed = allowed || (item.effect == "allow" && policy.V4 == "allow")
				denied = denied || item.effect == "deny" || policy.V4 == "deny"
			}
		}
	}
	return allowed, denied, nil
}

func (r *Repository) ExternalRole(ctx context.Context, access domain.ExternalAccess, name string) (bool, error) {
	return r.externalDirectoryNamed(ctx, access, "role", name)
}
func (r *Repository) ExternalGroup(ctx context.Context, access domain.ExternalAccess, name string) (bool, error) {
	return r.externalDirectoryNamed(ctx, access, "group", name)
}

type externalDirectoryItem struct{ kind, id, effect string }

func (r *Repository) externalDirectoryItems(ctx context.Context, access domain.ExternalAccess) ([]externalDirectoryItem, error) {
	clause, args, ok := externalIDs(access)
	if !ok {
		return nil, nil
	}
	query := `SELECT 'role',role_id,effect FROM external_grant_roles_v2 WHERE grant_id IN (` + clause + `) UNION ALL SELECT 'group',group_id,effect FROM external_grant_groups_v2 WHERE grant_id IN (` + clause + `)`
	rows, err := r.database.QueryContext(ctx, query, append(args, accessArgs(access)...)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []externalDirectoryItem
	for rows.Next() {
		var item externalDirectoryItem
		if err = rows.Scan(&item.kind, &item.id, &item.effect); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) externalDirectoryNamed(ctx context.Context, access domain.ExternalAccess, kind, name string) (bool, error) {
	items, err := r.externalDirectoryItems(ctx, access)
	if err != nil || len(items) == 0 {
		return false, err
	}
	if r.directory == nil {
		return false, errors.New("authorization directory is required")
	}
	snapshot, err := r.directory.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	directory := snapshot.Roles
	if kind == "group" {
		directory = snapshot.Groups
	}
	allowed, denied := false, false
	for _, item := range items {
		value := directory[item.id]
		if item.kind != kind || (item.id != name && value.Name != name) {
			continue
		}
		allowed = allowed || item.effect == "allow"
		denied = denied || item.effect == "deny"
	}
	return allowed && !denied, nil
}

func readEffects(rows *sql.Rows) (allowed, denied bool, resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var effect string
		if err := rows.Scan(&effect); err != nil {
			return false, false, err
		}
		allowed = allowed || effect == "allow"
		denied = denied || effect == "deny"
	}
	return allowed, denied, rows.Err()
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
