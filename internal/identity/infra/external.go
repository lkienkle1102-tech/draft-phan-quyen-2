package infra

import (
	"context"
	"database/sql"
	"time"

	identity "example.com/phan-quyen-golang/internal/identity/domain"
)

func (r *Repository) loadExternalGrants(ctx context.Context, tx *sql.Tx, userID string, at time.Time, snapshot *identity.Snapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT grant_row.id,grant_row.owner_organization_id,owner.name,grant_row.target_type,
		COALESCE(grant_row.target_user_id,''),COALESCE(grant_row.target_organization_id,''),COALESCE(grant_row.target_membership_id,''),
		grant_row.resource_type,COALESCE(grant_row.resource_id,''),grant_row.action,grant_row.effect,grant_row.valid_from,grant_row.valid_until
		FROM external_grants_v2 grant_row JOIN organizations owner ON owner.id=grant_row.owner_organization_id
		WHERE grant_row.status='active' AND grant_row.valid_from<=? AND(grant_row.valid_until IS NULL OR grant_row.valid_until>?) AND(
			(grant_row.target_type='global_user' AND grant_row.target_user_id=?) OR
			(grant_row.target_type='organization' AND EXISTS(SELECT 1 FROM organization_members member WHERE member.organization_id=grant_row.target_organization_id AND member.user_id=? AND member.active=1)) OR
			(grant_row.target_type='organization_member' AND grant_row.target_user_id=? AND EXISTS(SELECT 1 FROM organization_members member WHERE member.id=grant_row.target_membership_id AND member.organization_id=grant_row.target_organization_id AND member.user_id=? AND member.active=1))
		) ORDER BY grant_row.id`, stamp(at), stamp(at), userID, userID, userID, userID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var value identity.ExternalGrant
		var validFrom string
		var validUntil sql.NullString
		if err = rows.Scan(&value.ID, &value.OwnerOrganizationID, &value.OwnerOrganizationName, &value.Target.Type, &value.Target.UserID, &value.Target.OrganizationID, &value.Target.MembershipID, &value.ResourceType, &value.ResourceID, &value.Action, &value.Effect, &validFrom, &validUntil); err != nil {
			return err
		}
		if value.ValidFrom, err = parseTime(validFrom); err != nil {
			return err
		}
		if value.ValidUntil, err = optionalTime(validUntil); err != nil {
			return err
		}
		snapshot.ExternalGrants = append(snapshot.ExternalGrants, value)
	}
	if err = rows.Err(); err != nil || len(snapshot.ExternalGrants) == 0 {
		return err
	}
	return loadExternalBundles(ctx, tx, snapshot.ExternalGrants)
}

func loadExternalBundles(ctx context.Context, tx *sql.Tx, grants []identity.ExternalGrant) error {
	indexes := make(map[string]*scopeAccumulator, len(grants))
	args := make([]any, 0, len(grants))
	for index := range grants {
		indexes[grants[index].ID] = newScopeAccumulator(&grants[index].Entitlements)
		args = append(args, grants[index].ID)
	}
	clause := placeholders(len(args))
	if err := loadExternalFacts(ctx, tx, clause, args, indexes); err != nil {
		return err
	}
	if err := loadExternalPlans(ctx, tx, clause, args, indexes); err != nil {
		return err
	}
	if err := loadExternalQuotas(ctx, tx, clause, args, indexes); err != nil {
		return err
	}
	finalizeScopes(indexes)
	return nil
}

func loadExternalFacts(ctx context.Context, tx *sql.Tx, clause string, args []any, scopes map[string]*scopeAccumulator) error {
	queries := []struct {
		query, sourceType string
		selectMap         factMap
	}{
		{`SELECT item.grant_id,item.permission_id,item.effect,item.permission_id FROM external_grant_permissions_v2 item WHERE item.grant_id IN (` + clause + `)`, "direct", func(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.permissions }},
		{`SELECT item.grant_id,role.name,item.effect,role.id FROM external_grant_roles_v2 item JOIN roles_v2 role ON role.id=item.role_id WHERE item.grant_id IN (` + clause + `)`, "role", func(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.roles }},
		{`SELECT item.grant_id,group_row.name,item.effect,group_row.id FROM external_grant_groups_v2 item JOIN groups_v2 group_row ON group_row.id=item.group_id WHERE item.grant_id IN (` + clause + `)`, "group", func(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.groups }},
		{`SELECT item.grant_id,item.feature_key,item.effect,item.source_type||':'||item.source_id FROM external_grant_features_v2 item WHERE item.grant_id IN (` + clause + `)`, "feature", func(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.features }},
	}
	for _, item := range queries {
		if err := loadExternalFactRows(ctx, tx, item.query, args, scopes, item.sourceType, item.selectMap); err != nil {
			return err
		}
	}
	return loadExternalDerivedPermissions(ctx, tx, clause, args, scopes)
}

func loadExternalFactRows(ctx context.Context, tx *sql.Tx, query string, args []any, scopes map[string]*scopeAccumulator, sourceType string, selectMap factMap) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var grantID, key, effect, sourceID string
		if err = rows.Scan(&grantID, &key, &effect, &sourceID); err != nil {
			return err
		}
		addFact(selectMap(scopes[grantID]), key, identity.Source{Type: sourceType, ID: sourceID, Effect: effect})
	}
	return rows.Err()
}

func loadExternalDerivedPermissions(ctx context.Context, tx *sql.Tx, clause string, args []any, scopes map[string]*scopeAccumulator) error {
	query := `SELECT item.grant_id,permission.permission_id,CASE WHEN item.effect='deny' OR permission.effect='deny' THEN 'deny' ELSE 'allow' END,'role',role.id
		FROM external_grant_roles_v2 item JOIN roles_v2 role ON role.id=item.role_id JOIN role_permission_rules_v2 permission ON permission.role_id=role.id WHERE item.grant_id IN (` + clause + `)
		UNION ALL SELECT item.grant_id,permission.permission_id,CASE WHEN item.effect='deny' OR group_role.effect='deny' OR permission.effect='deny' THEN 'deny' ELSE 'allow' END,'group',group_row.id
		FROM external_grant_groups_v2 item JOIN groups_v2 group_row ON group_row.id=item.group_id JOIN group_role_rules_v2 group_role ON group_role.group_id=group_row.id JOIN role_permission_rules_v2 permission ON permission.role_id=group_role.role_id WHERE item.grant_id IN (` + clause + `)`
	combined := append(append([]any(nil), args...), args...)
	rows, err := tx.QueryContext(ctx, query, combined...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var grantID, key string
		var source identity.Source
		if err = rows.Scan(&grantID, &key, &source.Effect, &source.Type, &source.ID); err != nil {
			return err
		}
		addFact(scopes[grantID].permissions, key, source)
	}
	return rows.Err()
}

func loadExternalPlans(ctx context.Context, tx *sql.Tx, clause string, args []any, scopes map[string]*scopeAccumulator) error {
	rows, err := tx.QueryContext(ctx, `SELECT item.grant_id,plan.id,plan.name,item.effect,plan.valid_from,plan.valid_until
		FROM external_grant_plans_v2 item JOIN plans_v2 plan ON plan.id=item.plan_id WHERE item.grant_id IN (`+clause+`)`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var grantID, validFrom string
		var validUntil sql.NullString
		var plan identity.Plan
		if err = rows.Scan(&grantID, &plan.ID, &plan.Name, &plan.Effect, &validFrom, &validUntil); err != nil {
			return err
		}
		plan.Status = "granted"
		if plan.ValidFrom, err = parseTime(validFrom); err != nil {
			return err
		}
		if plan.ValidUntil, err = optionalTime(validUntil); err != nil {
			return err
		}
		scopes[grantID].target.Plan = &plan
	}
	return rows.Err()
}

func loadExternalQuotas(ctx context.Context, tx *sql.Tx, clause string, args []any, scopes map[string]*scopeAccumulator) error {
	rows, err := tx.QueryContext(ctx, `SELECT allocation.grant_id,allocation.quota_key,allocation.effect,allocation.allocated,allocation.used,allocation.reserved,allocation.source_type,allocation.source_id
		FROM external_grant_quota_allocations_v2 allocation WHERE allocation.grant_id IN (`+clause+`)`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var grantID, key, effect, sourceType, sourceID string
		var allocated, used, reserved int64
		if err = rows.Scan(&grantID, &key, &effect, &allocated, &used, &reserved, &sourceType, &sourceID); err != nil {
			return err
		}
		value := scopes[grantID].quotas[key]
		if value == nil {
			value = &quotaAccumulator{value: identity.Quota{Key: key, Effect: "allow"}}
			scopes[grantID].quotas[key] = value
		}
		value.value.Sources = append(value.value.Sources, identity.Source{Type: sourceType, ID: sourceID, Effect: effect})
		if effect == "deny" {
			value.value.Effect = "deny"
			continue
		}
		value.hasAllow = true
		value.finiteLimit += allocated
		value.finiteLeft += allocated - used - reserved
		value.value.Used += used
		value.value.Reserved += reserved
	}
	return rows.Err()
}

func optionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	return &parsed, err
}
