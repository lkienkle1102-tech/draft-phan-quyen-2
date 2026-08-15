package infra

import (
	"context"
	"database/sql"
	"time"

	identity "example.com/phan-quyen-golang/internal/identity/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
)

func (r *Repository) loadExternalGrants(ctx context.Context, tx *sql.Tx, userID string, at time.Time, authorization security.AuthorizationSnapshot, snapshot *identity.Snapshot) error {
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
	return loadExternalBundles(ctx, tx, authorization, snapshot.ExternalGrants)
}

func loadExternalBundles(ctx context.Context, tx *sql.Tx, authorization security.AuthorizationSnapshot, grants []identity.ExternalGrant) error {
	indexes := make(map[string]*scopeAccumulator, len(grants))
	domains := make(map[string]string, len(grants))
	args := make([]any, 0, len(grants))
	for index := range grants {
		indexes[grants[index].ID] = newScopeAccumulator(&grants[index].Entitlements)
		domains[grants[index].ID] = "organization::" + grants[index].OwnerOrganizationID
		args = append(args, grants[index].ID)
	}
	clause := placeholders(len(args))
	if err := loadExternalFacts(ctx, tx, clause, args, authorization, domains, indexes); err != nil {
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

func loadExternalFacts(ctx context.Context, tx *sql.Tx, clause string, args []any, authorization security.AuthorizationSnapshot, domains map[string]string, scopes map[string]*scopeAccumulator) error {
	queries := []struct {
		query, sourceType string
		selectMap         factMap
	}{
		{`SELECT item.grant_id,item.permission_id,item.effect,item.permission_id FROM external_grant_permissions_v2 item WHERE item.grant_id IN (` + clause + `)`, "direct", func(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.permissions }},
		{`SELECT item.grant_id,item.feature_key,item.effect,item.source_type||':'||item.source_id FROM external_grant_features_v2 item WHERE item.grant_id IN (` + clause + `)`, "feature", func(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.features }},
	}
	for _, item := range queries {
		if err := loadExternalFactRows(ctx, tx, item.query, args, scopes, item.sourceType, item.selectMap); err != nil {
			return err
		}
	}
	if err := loadExternalDirectoryFacts(ctx, tx, clause, args, authorization, domains, scopes, "role"); err != nil {
		return err
	}
	return loadExternalDirectoryFacts(ctx, tx, clause, args, authorization, domains, scopes, "group")
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

func loadExternalDirectoryFacts(ctx context.Context, tx *sql.Tx, clause string, args []any, authorization security.AuthorizationSnapshot, domains map[string]string, scopes map[string]*scopeAccumulator, kind string) error {
	column, table := "role_id", "external_grant_roles_v2"
	directory := authorization.Roles
	if kind == "group" {
		column, table, directory = "group_id", "external_grant_groups_v2", authorization.Groups
	}
	rows, err := tx.QueryContext(ctx, `SELECT grant_id,`+column+`,effect FROM `+table+` WHERE grant_id IN (`+clause+`)`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var grantID, id, itemEffect string
		if err = rows.Scan(&grantID, &id, &itemEffect); err != nil {
			return err
		}
		addFact(mapForDirectoryKind(scopes[grantID], kind), directoryName(directory, id), identity.Source{Type: kind, ID: id, Effect: itemEffect})
		principal := kind + "::" + id
		for _, policy := range security.PoliciesForPrincipal(authorization.Rules, principal, domains[grantID]) {
			effect := policy.V4
			if itemEffect == "deny" {
				effect = "deny"
			}
			addFact(scopes[grantID].permissions, policy.V2+"."+policy.V3, identity.Source{Type: kind, ID: id, Effect: effect})
		}
	}
	return rows.Err()
}

func mapForDirectoryKind(scope *scopeAccumulator, kind string) map[string]*identity.EffectiveFact {
	if kind == "group" {
		return scope.groups
	}
	return scope.roles
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
