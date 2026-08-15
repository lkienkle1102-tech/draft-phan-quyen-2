// Package infra implements SQLite entitlement persistence and materialization.
package infra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"example.com/phan-quyen-golang/internal/entitlement/domain"
)

type Repository struct{ database *sql.DB }

func NewRepository(database *sql.DB) *Repository { return &Repository{database: database} }

func (r *Repository) ApplySubscription(ctx context.Context, value domain.Subscription) error {
	return r.withTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions_v2(id,subject_type,subject_id,plan_id,effect,status,valid_from,valid_until,current_period_start,current_period_end) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET plan_id=excluded.plan_id,effect=excluded.effect,status=excluded.status,valid_from=excluded.valid_from,valid_until=excluded.valid_until,current_period_start=excluded.current_period_start,current_period_end=excluded.current_period_end`, value.ID, value.Subject.Type, value.Subject.ID, value.PlanID, value.Effect, value.Status, format(value.ValidFrom), formatOptional(value.ValidUntil), format(value.PeriodStart), format(value.PeriodEnd)); err != nil {
			return err
		}
		if err := clearPlanMaterialization(ctx, tx, value); err != nil {
			return err
		}
		return materializePlan(ctx, tx, value)
	})
}

func clearPlanMaterialization(ctx context.Context, tx *sql.Tx, value domain.Subscription) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM subject_feature_entitlements_v2 WHERE subject_type=? AND subject_id=? AND source_type='plan'`, value.Subject.Type, value.Subject.ID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND source_type='plan' AND used=0 AND reserved=0`, value.Subject.Type, value.Subject.ID)
	return err
}

func materializePlan(ctx context.Context, tx *sql.Tx, value domain.Subscription) error {
	features, err := tx.QueryContext(ctx, `SELECT feature_key,effect FROM plan_feature_rules_v2 WHERE plan_id=?`, value.PlanID)
	if err != nil {
		return err
	}
	for features.Next() {
		var key, effect string
		if err := features.Scan(&key, &effect); err != nil {
			_ = features.Close()
			return err
		}
		id := fmt.Sprintf("%s:feature:%s", value.ID, key)
		if _, err := tx.ExecContext(ctx, `INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from,valid_until) VALUES(?,?,?,?,?,'plan',?,?,?)`, id, value.Subject.Type, value.Subject.ID, key, effect, value.ID, format(value.ValidFrom), formatOptional(value.ValidUntil)); err != nil {
			_ = features.Close()
			return err
		}
	}
	if err := features.Close(); err != nil {
		return err
	}
	quotas, err := tx.QueryContext(ctx, `SELECT quota_key,effect,quota_limit FROM plan_quota_rules_v2 WHERE plan_id=?`, value.PlanID)
	if err != nil {
		return err
	}
	defer func() { _ = quotas.Close() }()
	for quotas.Next() {
		var key, effect string
		var limit sql.NullInt64
		if err := quotas.Scan(&key, &effect, &limit); err != nil {
			return err
		}
		id := fmt.Sprintf("%s:quota:%s:%s", value.ID, key, format(value.PeriodStart))
		if _, err := tx.ExecContext(ctx, `INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,period_start,period_end,source_type,source_id) VALUES(?,?,?,?,?,?,?,?,'plan',?)`, id, value.Subject.Type, value.Subject.ID, key, effect, nullableLimit(limit), format(value.PeriodStart), format(value.PeriodEnd), value.ID); err != nil {
			return err
		}
	}
	return quotas.Err()
}

func (r *Repository) RenewSubscription(ctx context.Context, id string, start, end time.Time) error {
	return r.withTransaction(ctx, func(tx *sql.Tx) error {
		value, err := loadSubscription(ctx, tx, id)
		if err != nil {
			return err
		}
		value.PeriodStart, value.PeriodEnd = start, end
		if _, err := tx.ExecContext(ctx, `UPDATE subscriptions_v2 SET current_period_start=?,current_period_end=? WHERE id=?`, format(start), format(end), id); err != nil {
			return err
		}
		return materializePlan(ctx, tx, value)
	})
}

func loadSubscription(ctx context.Context, tx *sql.Tx, id string) (domain.Subscription, error) {
	var value domain.Subscription
	var validFrom, validUntil, periodStart, periodEnd string
	err := tx.QueryRowContext(ctx, `SELECT id,subject_type,subject_id,plan_id,effect,status,valid_from,COALESCE(valid_until,''),current_period_start,current_period_end FROM subscriptions_v2 WHERE id=?`, id).Scan(&value.ID, &value.Subject.Type, &value.Subject.ID, &value.PlanID, &value.Effect, &value.Status, &validFrom, &validUntil, &periodStart, &periodEnd)
	if err != nil {
		return value, err
	}
	value.ValidFrom, err = time.Parse(time.RFC3339, validFrom)
	if err != nil {
		return value, err
	}
	if validUntil != "" {
		parsed, parseErr := time.Parse(time.RFC3339, validUntil)
		if parseErr != nil {
			return value, parseErr
		}
		value.ValidUntil = &parsed
	}
	value.PeriodStart, err = time.Parse(time.RFC3339, periodStart)
	if err != nil {
		return value, err
	}
	value.PeriodEnd, err = time.Parse(time.RFC3339, periodEnd)
	return value, err
}

func (r *Repository) CancelSubscription(ctx context.Context, id string, at time.Time) error {
	result, err := r.database.ExecContext(ctx, `UPDATE subscriptions_v2 SET status='cancelled',valid_until=? WHERE id=? AND status IN('trialing','active')`, format(at), id)
	if err != nil {
		return err
	}
	return exactlyOne(result)
}

func (r *Repository) ApplyFeatureOverride(ctx context.Context, value domain.Override) error {
	_, err := r.database.ExecContext(ctx, `INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from,valid_until) VALUES(?,?,?,?,?,'manual',?,?,?) ON CONFLICT(id) DO UPDATE SET effect=excluded.effect,valid_from=excluded.valid_from,valid_until=excluded.valid_until`, value.ID, value.Subject.Type, value.Subject.ID, value.Key, value.Effect, value.ID, format(value.ValidFrom), formatOptional(value.ValidUntil))
	return err
}

func (r *Repository) ApplyQuotaOverride(ctx context.Context, value domain.Override) error {
	_, err := r.database.ExecContext(ctx, `INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,period_start,period_end,source_type,source_id) VALUES(?,?,?,?,?,?,?,?,'manual',?) ON CONFLICT(id) DO UPDATE SET effect=excluded.effect,quota_limit=excluded.quota_limit,period_start=excluded.period_start,period_end=excluded.period_end`, value.ID, value.Subject.Type, value.Subject.ID, value.Key, value.Effect, value.Limit, format(*value.PeriodStart), format(*value.PeriodEnd), value.ID)
	return err
}

func (r *Repository) withTransaction(ctx context.Context, operation func(*sql.Tx) error) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func format(value time.Time) string { return value.UTC().Format(time.RFC3339) }
func formatOptional(value *time.Time) any {
	if value == nil {
		return nil
	}
	return format(*value)
}
func nullableLimit(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
func exactlyOne(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("entitlement not found or invalid state")
	}
	return nil
}
