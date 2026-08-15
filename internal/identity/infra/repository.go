// Package infra loads identity snapshots from SQLite.
package infra

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	identity "example.com/phan-quyen-golang/internal/identity/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
)

type Repository struct{ database *sql.DB }

func NewRepository(database *sql.DB) *Repository { return &Repository{database: database} }

type scopeAccumulator struct {
	target                     *identity.Entitlements
	roles, groups, permissions map[string]*identity.EffectiveFact
	features                   map[string]*identity.EffectiveFact
	quotas                     map[string]*quotaAccumulator
}

type quotaAccumulator struct {
	value           identity.Quota
	finiteLimit     int64
	finiteLeft      int64
	hasAllow        bool
	unboundedPeriod bool
}

func newScopeAccumulator(target *identity.Entitlements) *scopeAccumulator {
	return &scopeAccumulator{target: target, roles: map[string]*identity.EffectiveFact{}, groups: map[string]*identity.EffectiveFact{}, permissions: map[string]*identity.EffectiveFact{}, features: map[string]*identity.EffectiveFact{}, quotas: map[string]*quotaAccumulator{}}
}

func (r *Repository) Read(ctx context.Context, actor security.Actor, at time.Time) (identity.Snapshot, error) {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return identity.Snapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot := identity.Snapshot{GeneratedAt: at, Identity: identity.Identity{ID: actor.ID, ActorType: string(actor.Type), TokenOrganizationID: actor.OrganizationID, AMR: append([]string{}, actor.AMR...), AuthTime: actor.AuthTime}, Personal: identity.Scope{Subject: identity.Subject{Type: "user", ID: actor.ID}}, Organizations: []identity.OrganizationScope{}, ExternalGrants: []identity.ExternalGrant{}}
	if err = loadOrganizations(ctx, tx, actor.ID, &snapshot); err != nil {
		return identity.Snapshot{}, err
	}
	scopes := indexScopes(&snapshot)
	if err = loadScopedFacts(ctx, tx, actor.ID, at, scopes); err != nil {
		return identity.Snapshot{}, err
	}
	if err = r.loadExternalGrants(ctx, tx, actor.ID, at, &snapshot); err != nil {
		return identity.Snapshot{}, err
	}
	finalizeScopes(scopes)
	if err = tx.Commit(); err != nil {
		return identity.Snapshot{}, err
	}
	return snapshot, nil
}

func loadOrganizations(ctx context.Context, tx *sql.Tx, userID string, snapshot *identity.Snapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT organization.id,organization.name,membership.id,membership.joined_at
		FROM organization_members membership JOIN organizations organization ON organization.id=membership.organization_id
		WHERE membership.user_id=? AND membership.active=1 ORDER BY organization.id`, userID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var value identity.OrganizationScope
		var joined string
		if err = rows.Scan(&value.Organization.ID, &value.Organization.Name, &value.Membership.ID, &joined); err != nil {
			return err
		}
		value.Membership.JoinedAt, err = parseTime(joined)
		if err != nil {
			return err
		}
		snapshot.Organizations = append(snapshot.Organizations, value)
	}
	return rows.Err()
}

func indexScopes(snapshot *identity.Snapshot) map[string]*scopeAccumulator {
	result := map[string]*scopeAccumulator{scopeKey("user", snapshot.Identity.ID): newScopeAccumulator(&snapshot.Personal.Entitlements)}
	for index := range snapshot.Organizations {
		organization := &snapshot.Organizations[index]
		result[scopeKey("organization", organization.Organization.ID)] = newScopeAccumulator(&organization.Entitlements)
	}
	return result
}

func loadScopedFacts(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	loaders := []func(context.Context, *sql.Tx, string, time.Time, map[string]*scopeAccumulator) error{
		loadRoles, loadGroups, loadPermissions, loadFeatures, loadPlans, loadQuotas,
	}
	for _, load := range loaders {
		if err := load(ctx, tx, userID, at, scopes); err != nil {
			return err
		}
	}
	return nil
}

const eligibleScopes = `WITH eligible(subject_type,subject_id) AS(
	SELECT 'user',? UNION ALL
	SELECT 'organization',organization_id FROM organization_members WHERE user_id=? AND active=1
) `

func loadRoles(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	query := eligibleScopes + `SELECT assignment.subject_type,assignment.subject_id,role.name,assignment.effect,role.id
		FROM role_assignments_v2 assignment JOIN eligible USING(subject_type,subject_id)
		JOIN roles_v2 role ON role.id=assignment.role_id
		WHERE assignment.user_id=? AND role.active=1 AND assignment.valid_from<=? AND(assignment.valid_until IS NULL OR assignment.valid_until>?)`
	return loadNamedFacts(ctx, tx, query, userID, at, scopes, "role_assignment", roleFacts)
}

func loadGroups(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	query := eligibleScopes + `SELECT group_row.owner_type,group_row.owner_id,group_row.name,membership.effect,group_row.id
		FROM group_memberships_v2 membership JOIN groups_v2 group_row ON group_row.id=membership.group_id
		JOIN eligible ON eligible.subject_type=group_row.owner_type AND eligible.subject_id=group_row.owner_id
		WHERE membership.user_id=? AND group_row.active=1 AND membership.valid_from<=? AND(membership.valid_until IS NULL OR membership.valid_until>?)`
	return loadNamedFacts(ctx, tx, query, userID, at, scopes, "group_membership", groupFacts)
}

type factMap func(*scopeAccumulator) map[string]*identity.EffectiveFact

func roleFacts(scope *scopeAccumulator) map[string]*identity.EffectiveFact  { return scope.roles }
func groupFacts(scope *scopeAccumulator) map[string]*identity.EffectiveFact { return scope.groups }

func loadNamedFacts(ctx context.Context, tx *sql.Tx, query, userID string, at time.Time, scopes map[string]*scopeAccumulator, sourceType string, selectMap factMap) error {
	args := []any{userID, userID, userID, stamp(at), stamp(at)}
	return loadFacts(ctx, tx, query, args, scopes, sourceType, selectMap)
}

func loadFacts(ctx context.Context, tx *sql.Tx, query string, args []any, scopes map[string]*scopeAccumulator, sourceType string, selectMap factMap) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var subjectType, subjectID, key, effect, sourceID string
		if err = rows.Scan(&subjectType, &subjectID, &key, &effect, &sourceID); err != nil {
			return err
		}
		if scope := scopes[scopeKey(subjectType, subjectID)]; scope != nil {
			addFact(selectMap(scope), key, identity.Source{Type: sourceType, ID: sourceID, Effect: effect})
		}
	}
	return rows.Err()
}

func loadPermissions(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	query := eligibleScopes + `SELECT direct.subject_type,direct.subject_id,direct.permission_id,direct.effect,'direct',direct.permission_id
		FROM subject_permission_rules_v2 direct JOIN eligible USING(subject_type,subject_id)
		WHERE direct.user_id=? AND direct.valid_from<=? AND(direct.valid_until IS NULL OR direct.valid_until>?)
		UNION ALL SELECT assignment.subject_type,assignment.subject_id,rule.permission_id,
		CASE WHEN assignment.effect='deny' OR rule.effect='deny' THEN 'deny' ELSE 'allow' END,'role',role.id
		FROM role_assignments_v2 assignment JOIN eligible USING(subject_type,subject_id)
		JOIN roles_v2 role ON role.id=assignment.role_id JOIN role_permission_rules_v2 rule ON rule.role_id=role.id
		WHERE assignment.user_id=? AND role.active=1 AND assignment.valid_from<=? AND(assignment.valid_until IS NULL OR assignment.valid_until>?)
		UNION ALL SELECT group_row.owner_type,group_row.owner_id,permission.permission_id,
		CASE WHEN membership.effect='deny' OR group_role.effect='deny' OR permission.effect='deny' THEN 'deny' ELSE 'allow' END,'group',group_row.id
		FROM group_memberships_v2 membership JOIN groups_v2 group_row ON group_row.id=membership.group_id
		JOIN eligible ON eligible.subject_type=group_row.owner_type AND eligible.subject_id=group_row.owner_id
		JOIN group_role_rules_v2 group_role ON group_role.group_id=group_row.id JOIN roles_v2 role ON role.id=group_role.role_id
		JOIN role_permission_rules_v2 permission ON permission.role_id=role.id
		WHERE membership.user_id=? AND group_row.active=1 AND role.active=1 AND membership.valid_from<=? AND(membership.valid_until IS NULL OR membership.valid_until>?)`
	now := stamp(at)
	args := []any{userID, userID, userID, now, now, userID, now, now, userID, now, now}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var subjectType, subjectID, key, effect, sourceType, sourceID string
		if err = rows.Scan(&subjectType, &subjectID, &key, &effect, &sourceType, &sourceID); err != nil {
			return err
		}
		if scope := scopes[scopeKey(subjectType, subjectID)]; scope != nil {
			addFact(scope.permissions, key, identity.Source{Type: sourceType, ID: sourceID, Effect: effect})
		}
	}
	return rows.Err()
}

func loadFeatures(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	query := eligibleScopes + `SELECT entitlement.subject_type,entitlement.subject_id,entitlement.feature_key,entitlement.effect,entitlement.source_type,entitlement.source_id
		FROM subject_feature_entitlements_v2 entitlement JOIN eligible USING(subject_type,subject_id)
		WHERE entitlement.valid_from<=? AND(entitlement.valid_until IS NULL OR entitlement.valid_until>?)`
	rows, err := tx.QueryContext(ctx, query, userID, userID, stamp(at), stamp(at))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var subjectType, subjectID, key string
		var source identity.Source
		if err = rows.Scan(&subjectType, &subjectID, &key, &source.Effect, &source.Type, &source.ID); err != nil {
			return err
		}
		if scope := scopes[scopeKey(subjectType, subjectID)]; scope != nil {
			addFact(scope.features, key, source)
		}
	}
	return rows.Err()
}

func loadPlans(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	query := eligibleScopes + `SELECT subscription.subject_type,subscription.subject_id,plan.id,plan.name,subscription.effect,subscription.status,
		subscription.valid_from,subscription.valid_until,subscription.current_period_start,subscription.current_period_end
		FROM subscriptions_v2 subscription JOIN eligible USING(subject_type,subject_id) JOIN plans_v2 plan ON plan.id=subscription.plan_id
		WHERE plan.active=1 AND subscription.status IN('trialing','active') AND subscription.valid_from<=? AND(subscription.valid_until IS NULL OR subscription.valid_until>?)
		AND subscription.current_period_start<=? AND subscription.current_period_end>? AND plan.valid_from<=? AND(plan.valid_until IS NULL OR plan.valid_until>?)`
	rows, err := tx.QueryContext(ctx, query, userID, userID, stamp(at), stamp(at), stamp(at), stamp(at), stamp(at), stamp(at))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var subjectType, subjectID, validFrom, periodStart, periodEnd string
		var validUntil sql.NullString
		var plan identity.Plan
		if err = rows.Scan(&subjectType, &subjectID, &plan.ID, &plan.Name, &plan.Effect, &plan.Status, &validFrom, &validUntil, &periodStart, &periodEnd); err != nil {
			return err
		}
		if err = parsePlanTimes(&plan, validFrom, validUntil, periodStart, periodEnd); err != nil {
			return err
		}
		if scope := scopes[scopeKey(subjectType, subjectID)]; scope != nil {
			scope.target.Plan = &plan
		}
	}
	return rows.Err()
}

func loadQuotas(ctx context.Context, tx *sql.Tx, userID string, at time.Time, scopes map[string]*scopeAccumulator) error {
	query := eligibleScopes + `SELECT entitlement.subject_type,entitlement.subject_id,entitlement.quota_key,entitlement.effect,entitlement.quota_limit,
		entitlement.used,entitlement.reserved,entitlement.period_start,entitlement.period_end,entitlement.source_type,entitlement.source_id
		FROM subject_quota_entitlements_v2 entitlement JOIN eligible USING(subject_type,subject_id)
		WHERE entitlement.period_start<=? AND(entitlement.period_end IS NULL OR entitlement.period_end>?)`
	rows, err := tx.QueryContext(ctx, query, userID, userID, stamp(at), stamp(at))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var value scopedQuotaRow
		if err = rows.Scan(&value.subjectType, &value.subjectID, &value.key, &value.effect, &value.limit, &value.used, &value.reserved, &value.periodStart, &value.periodEnd, &value.sourceType, &value.sourceID); err != nil {
			return err
		}
		if scope := scopes[scopeKey(value.subjectType, value.subjectID)]; scope != nil {
			if err = addQuota(scope, value); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

type scopedQuotaRow struct {
	subjectType, subjectID, key, effect string
	limit                               sql.NullInt64
	used, reserved                      int64
	periodStart, sourceType, sourceID   string
	periodEnd                           sql.NullString
}

func addFact(values map[string]*identity.EffectiveFact, key string, source identity.Source) {
	value := values[key]
	if value == nil {
		value = &identity.EffectiveFact{Key: key, Effect: "allow"}
		values[key] = value
	}
	value.Sources = append(value.Sources, source)
	if source.Effect == "deny" {
		value.Effect = "deny"
	}
}

func addQuota(scope *scopeAccumulator, row scopedQuotaRow) error {
	value := scope.quotas[row.key]
	if value == nil {
		parsedStart, err := parseTime(row.periodStart)
		if err != nil {
			return err
		}
		value = &quotaAccumulator{value: identity.Quota{Key: row.key, Effect: "allow", PeriodStart: parsedStart}}
		scope.quotas[row.key] = value
	}
	value.value.Sources = append(value.value.Sources, identity.Source{Type: row.sourceType, ID: row.sourceID, Effect: row.effect})
	if row.effect == "deny" {
		value.value.Effect = "deny"
		return mergePeriod(value, row.periodStart, row.periodEnd)
	}
	value.hasAllow = true
	value.value.Used += row.used
	value.value.Reserved += row.reserved
	if !row.limit.Valid {
		value.value.Unlimited = true
	} else {
		value.finiteLimit += row.limit.Int64
		value.finiteLeft += row.limit.Int64 - row.used - row.reserved
	}
	return mergePeriod(value, row.periodStart, row.periodEnd)
}

func mergePeriod(value *quotaAccumulator, start string, end sql.NullString) error {
	parsedStart, err := parseTime(start)
	if err != nil {
		return err
	}
	if parsedStart.Before(value.value.PeriodStart) {
		value.value.PeriodStart = parsedStart
	}
	if !end.Valid {
		value.unboundedPeriod = true
		value.value.PeriodEnd = nil
		return nil
	}
	if value.unboundedPeriod {
		return nil
	}
	parsedEnd, err := parseTime(end.String)
	if err != nil {
		return err
	}
	if value.value.PeriodEnd != nil && parsedEnd.Before(*value.value.PeriodEnd) {
		return nil
	}
	value.value.PeriodEnd = &parsedEnd
	return nil
}

func finalizeScopes(scopes map[string]*scopeAccumulator) {
	for _, scope := range scopes {
		scope.target.Roles = sortedFacts(scope.roles)
		scope.target.Groups = sortedFacts(scope.groups)
		scope.target.Permissions = sortedFacts(scope.permissions)
		scope.target.Features = sortedFacts(scope.features)
		scope.target.Quotas = sortedQuotas(scope.quotas)
	}
}

func sortedFacts(values map[string]*identity.EffectiveFact) []identity.EffectiveFact {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]identity.EffectiveFact, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		sort.Slice(value.Sources, func(i, j int) bool { return sourceOrder(value.Sources[i]) < sourceOrder(value.Sources[j]) })
		result = append(result, *value)
	}
	return result
}

func sortedQuotas(values map[string]*quotaAccumulator) []identity.Quota {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]identity.Quota, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if value.hasAllow && !value.value.Unlimited {
			value.value.Limit, value.value.Remaining = pointer(value.finiteLimit), pointer(value.finiteLeft)
		}
		sort.Slice(value.value.Sources, func(i, j int) bool { return sourceOrder(value.value.Sources[i]) < sourceOrder(value.value.Sources[j]) })
		result = append(result, value.value)
	}
	return result
}

func parsePlanTimes(plan *identity.Plan, validFrom string, validUntil sql.NullString, periodStart, periodEnd string) (err error) {
	if plan.ValidFrom, err = parseTime(validFrom); err != nil {
		return err
	}
	if validUntil.Valid {
		value, parseErr := parseTime(validUntil.String)
		if parseErr != nil {
			return parseErr
		}
		plan.ValidUntil = &value
	}
	if plan.PeriodStart, err = parseTime(periodStart); err != nil {
		return err
	}
	plan.PeriodEnd, err = parseTime(periodEnd)
	return err
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed, nil
	}
	parsed, fallbackErr := time.Parse("2006-01-02 15:04:05", value)
	if fallbackErr != nil {
		return time.Time{}, fmt.Errorf("parse database time %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

func scopeKey(subjectType, subjectID string) string { return subjectType + "\x00" + subjectID }
func stamp(value time.Time) string                  { return value.UTC().Format(time.RFC3339) }
func sourceOrder(value identity.Source) string {
	return value.Type + "\x00" + value.ID + "\x00" + value.Effect
}
func pointer(value int64) *int64 { return &value }

func placeholders(count int) string { return strings.TrimSuffix(strings.Repeat("?,", count), ",") }
