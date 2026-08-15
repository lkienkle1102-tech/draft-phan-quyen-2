package infra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
)

func (r *Repository) CreateExternalGrant(ctx context.Context, g domain.ExternalGrantDefinition) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var until any
	if g.ValidUntil != nil {
		until = g.ValidUntil.UTC().Format(time.RFC3339)
	}
	null := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO external_grants_v2(id,owner_organization_id,target_type,target_user_id,target_organization_id,target_membership_id,resource_type,resource_id,action,effect,status,valid_from,valid_until,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,'active',?,?,?,?)`,
		g.ID, g.OwnerOrganizationID, g.Target.Type, null(g.Target.UserID), null(g.Target.OrganizationID), null(g.Target.MembershipID), g.Resource.Type, null(g.Resource.ID), g.Operation.Action, g.Effect, g.ValidFrom.UTC().Format(time.RFC3339), until, g.CreatedBy, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	if err = insertGrantItems(ctx, tx, g); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO external_grant_events_v2(grant_id,actor_id,event,occurred_at) VALUES(?,?,'created',?)`, g.ID, g.CreatedBy, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) PermissionOperation(ctx context.Context, id string) (domain.Operation, error) {
	var result domain.Operation
	err := r.database.QueryRowContext(ctx, `SELECT resource_type,action FROM permissions WHERE id=?`, id).Scan(&result.ResourceType, &result.Action)
	return result, err
}

func insertGrantItems(ctx context.Context, tx *sql.Tx, g domain.ExternalGrantDefinition) error {
	if err := insertPermissionItems(ctx, tx, g); err != nil {
		return err
	}
	if err := insertOwnedItems(ctx, tx, g, "role"); err != nil {
		return err
	}
	if err := insertOwnedItems(ctx, tx, g, "group"); err != nil {
		return err
	}
	if err := insertFeatureItems(ctx, tx, g); err != nil {
		return err
	}
	if err := insertPlanItems(ctx, tx, g); err != nil {
		return err
	}
	return insertExplicitQuotaItems(ctx, tx, g)
}

func insertPermissionItems(ctx context.Context, tx *sql.Tx, g domain.ExternalGrantDefinition) error {
	for _, item := range g.Permissions {
		if item.Key == "external_grant.manage" {
			return application.ErrGrantEscalation
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO external_grant_permissions_v2(grant_id,permission_id,effect) SELECT ?,id,? FROM permissions WHERE id=?`, g.ID, item.Effect, item.Key)
		if err != nil {
			return err
		}
		if err = exactlyOne(result, "permission"); err != nil {
			return err
		}
	}
	return nil
}
func insertOwnedItems(ctx context.Context, tx *sql.Tx, g domain.ExternalGrantDefinition, kind string) error {
	items, column := g.Roles, "role_id"
	if kind == "group" {
		items, column = g.Groups, "group_id"
	}
	query := `INSERT INTO external_grant_` + kind + `s_v2(grant_id,` + column + `,effect) VALUES(?,?,?)`
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, query, g.ID, item.Key, item.Effect); err != nil {
			return err
		}
	}
	return nil
}
func insertFeatureItems(ctx context.Context, tx *sql.Tx, g domain.ExternalGrantDefinition) error {
	for _, item := range g.Features {
		if _, err := tx.ExecContext(ctx, `INSERT INTO external_grant_features_v2 VALUES(?,?,?,'explicit',?)`, g.ID, item.Key, item.Effect, item.Key); err != nil {
			return err
		}
	}
	return nil
}
func insertPlanItems(ctx context.Context, tx *sql.Tx, g domain.ExternalGrantDefinition) error {
	for _, item := range g.Plans {
		result, err := tx.ExecContext(ctx, `INSERT INTO external_grant_plans_v2(grant_id,plan_id,effect) SELECT ?,id,? FROM plans_v2 WHERE id=? AND active=1 AND(owner_type='system' OR(owner_type='organization' AND owner_id=?))`, g.ID, item.Effect, item.Key, g.OwnerOrganizationID)
		if err != nil {
			return err
		}
		if err = exactlyOne(result, "plan"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO external_grant_features_v2(grant_id,feature_key,effect,source_type,source_id) SELECT ?,feature_key,CASE WHEN ?='deny' OR effect='deny' THEN 'deny' ELSE 'allow' END,'plan',plan_id FROM plan_feature_rules_v2 WHERE plan_id=?`, g.ID, item.Effect, item.Key); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT quota_key,CASE WHEN ?='deny' OR effect='deny' THEN 'deny' ELSE 'allow' END,quota_limit FROM plan_quota_rules_v2 WHERE plan_id=?`, item.Effect, item.Key)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key string
			var effect domain.Effect
			var limit sql.NullInt64
			if err = rows.Scan(&key, &effect, &limit); err != nil {
				_ = rows.Close()
				return err
			}
			var amount int64
			if limit.Valid {
				amount = limit.Int64
			}
			allocation := quotaAllocation{grant: g.ID, owner: g.OwnerOrganizationID, key: key, effect: effect, amount: amount, sourceType: "plan", sourceID: item.Key}
			if err = allocateGrantQuota(ctx, tx, allocation); err != nil {
				_ = rows.Close()
				return err
			}
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}
func insertExplicitQuotaItems(ctx context.Context, tx *sql.Tx, g domain.ExternalGrantDefinition) error {
	for _, item := range g.Quotas {
		amount := int64(0)
		if item.Limit != nil {
			amount = *item.Limit
		}
		allocation := quotaAllocation{grant: g.ID, owner: g.OwnerOrganizationID, key: item.Key, effect: item.Effect, amount: amount, sourceType: "explicit", sourceID: item.Key}
		if err := allocateGrantQuota(ctx, tx, allocation); err != nil {
			return err
		}
	}
	return nil
}

type quotaAllocation struct {
	grant, owner, key    string
	effect               domain.Effect
	amount               int64
	sourceType, sourceID string
}

func allocateGrantQuota(ctx context.Context, tx *sql.Tx, allocation quotaAllocation) error {
	if allocation.effect == domain.EffectDeny {
		return insertQuotaAllocation(ctx, tx, allocation, "", 0)
	}
	if allocation.amount < 0 {
		return application.ErrInvalidExternalGrant
	}
	remaining := allocation.amount
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := tx.QueryContext(ctx, `SELECT id,quota_limit,used,reserved FROM subject_quota_entitlements_v2 WHERE subject_type='organization' AND subject_id=? AND quota_key=? AND effect='allow' AND period_start<=? AND(period_end IS NULL OR period_end>?) ORDER BY CASE WHEN quota_limit IS NULL THEN 1 ELSE 0 END,period_end,id`, allocation.owner, allocation.key, now, now)
	if err != nil {
		return err
	}
	for rows.Next() && remaining > 0 {
		var id string
		var limit sql.NullInt64
		var used, reserved int64
		if err = rows.Scan(&id, &limit, &used, &reserved); err != nil {
			_ = rows.Close()
			return err
		}
		available := remaining
		if limit.Valid && limit.Int64-used-reserved < available {
			available = limit.Int64 - used - reserved
		}
		if available <= 0 {
			continue
		}
		result, e := tx.ExecContext(ctx, `UPDATE subject_quota_entitlements_v2 SET reserved=reserved+? WHERE id=? AND(quota_limit IS NULL OR used+reserved+?<=quota_limit)`, available, id, available)
		if e != nil {
			_ = rows.Close()
			return e
		}
		if e = exactlyOne(result, "quota"); e != nil {
			_ = rows.Close()
			return e
		}
		if e = insertQuotaAllocation(ctx, tx, allocation, id, available); e != nil {
			_ = rows.Close()
			return e
		}
		remaining -= available
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if remaining != 0 {
		return domain.ErrQuotaExceeded
	}
	return nil
}
func insertQuotaAllocation(ctx context.Context, tx *sql.Tx, allocation quotaAllocation, owner string, amount int64) error {
	var ownerValue any = owner
	if owner == "" {
		ownerValue = nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO external_grant_quota_allocations_v2(grant_id,quota_key,owner_entitlement_id,effect,allocated,source_type,source_id) VALUES(?,?,?,?,?,?,?)`, allocation.grant, allocation.key, ownerValue, allocation.effect, amount, allocation.sourceType, allocation.sourceID)
	return err
}
func exactlyOne(result sql.Result, kind string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: unknown %s", application.ErrInvalidExternalGrant, kind)
	}
	return nil
}

func (r *Repository) RevokeExternalGrant(ctx context.Context, owner, grant, actor string, at time.Time) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var activeReservations int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved),0) FROM external_grant_quota_allocations_v2 WHERE grant_id=?`, grant).Scan(&activeReservations); err != nil {
		return err
	}
	if activeReservations != 0 {
		return errors.New("external grant has active quota reservations")
	}
	rows, err := tx.QueryContext(ctx, `SELECT owner_entitlement_id,allocated-used FROM external_grant_quota_allocations_v2 WHERE grant_id=? AND effect='allow'`, grant)
	if err != nil {
		return err
	}
	type release struct {
		id     string
		amount int64
	}
	var releases []release
	for rows.Next() {
		var v release
		if err = rows.Scan(&v.id, &v.amount); err != nil {
			_ = rows.Close()
			return err
		}
		releases = append(releases, v)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, v := range releases {
		result, e := tx.ExecContext(ctx, `UPDATE subject_quota_entitlements_v2 SET reserved=reserved-? WHERE id=? AND reserved>=?`, v.amount, v.id, v.amount)
		if e != nil {
			return e
		}
		if e = exactlyOne(result, "owner quota"); e != nil {
			return e
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE external_grants_v2 SET status='revoked',revoked_by=?,revoked_at=? WHERE id=? AND owner_organization_id=? AND status='active'`, actor, at.UTC().Format(time.RFC3339), grant, owner)
	if err != nil {
		return err
	}
	if err = exactlyOne(result, "grant"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO external_grant_events_v2(grant_id,actor_id,event,occurred_at) VALUES(?,?,'revoked',?)`, grant, actor, at.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ListExternalGrants(ctx context.Context, owner string) ([]domain.ExternalGrantSummary, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT id,target_type,COALESCE(target_user_id,''),COALESCE(target_organization_id,''),COALESCE(target_membership_id,''),resource_type,COALESCE(resource_id,''),action,effect,status,valid_from,valid_until FROM external_grants_v2 WHERE owner_organization_id=? ORDER BY created_at DESC,id`, owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.ExternalGrantSummary
	for rows.Next() {
		var item domain.ExternalGrantSummary
		var from string
		var until sql.NullString
		item.OwnerOrganizationID = owner
		if err := rows.Scan(&item.ID, &item.Target.Type, &item.Target.UserID, &item.Target.OrganizationID, &item.Target.MembershipID, &item.ResourceType, &item.ResourceID, &item.Action, &item.Effect, &item.Status, &from, &until); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return nil, err
		}
		item.ValidFrom = parsed
		if until.Valid {
			parsed, err = time.Parse(time.RFC3339, until.String)
			if err != nil {
				return nil, err
			}
			item.ValidUntil = &parsed
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
