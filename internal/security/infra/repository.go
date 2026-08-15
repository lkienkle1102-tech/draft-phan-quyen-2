// Package infra implements security persistence with database/sql.
package infra

import (
	"context"
	"database/sql"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type authorizationDirectory interface {
	Snapshot(context.Context) (domain.AuthorizationSnapshot, error)
}

type Repository struct {
	database  *sql.DB
	directory authorizationDirectory
}

func NewRepository(database *sql.DB, directories ...authorizationDirectory) *Repository {
	var directory authorizationDirectory
	if len(directories) != 0 {
		directory = directories[0]
	}
	return &Repository{database: database, directory: directory}
}

func (r *Repository) EnsureUser(ctx context.Context, id string) error {
	_, err := r.database.ExecContext(ctx, `INSERT OR IGNORE INTO users(id) VALUES(?)`, id)
	return err
}

func (r *Repository) IsMember(ctx context.Context, actor domain.Actor, subject domain.Subject) (bool, error) {
	if subject.Type == domain.SubjectUser {
		return actor.ID == subject.ID, nil
	}
	var found bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=? AND user_id=? AND active=1)`, subject.ID, actor.ID).Scan(&found)
	return found, err
}

func (r *Repository) HasFeature(ctx context.Context, subject domain.Subject, key string) (bool, error) {
	var found bool
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.database.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM subject_feature_entitlements_v2 WHERE subject_type=? AND subject_id=? AND feature_key=? AND effect='allow' AND valid_from<=? AND(valid_until IS NULL OR valid_until>?))
		AND NOT EXISTS(SELECT 1 FROM subject_feature_entitlements_v2 WHERE subject_type=? AND subject_id=? AND feature_key=? AND effect='deny' AND valid_from<=? AND(valid_until IS NULL OR valid_until>?))`,
		subject.Type, subject.ID, key, now, now, subject.Type, subject.ID, key, now, now).Scan(&found)
	return found, err
}

func (r *Repository) HasPlan(ctx context.Context, subject domain.Subject, key string) (bool, error) {
	var found bool
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
	)`, subject.Type, subject.ID, key, now, now, now, now, now, now).Scan(&found)
	return found, err
}

func (r *Repository) QuotaAvailable(ctx context.Context, subject domain.Subject, key string, cost int64) (bool, error) {
	var found bool
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.database.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow' AND period_start<=? AND(period_end IS NULL OR period_end>?))
		AND NOT EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='deny' AND period_start<=? AND(period_end IS NULL OR period_end>?))
		AND(
			EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow' AND quota_limit IS NULL AND period_start<=? AND(period_end IS NULL OR period_end>?))
			OR COALESCE((SELECT SUM(quota_limit-used-reserved) FROM subject_quota_entitlements_v2 WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow' AND quota_limit IS NOT NULL AND period_start<=? AND(period_end IS NULL OR period_end>?)),0)>=?
		)`,
		subject.Type, subject.ID, key, now, now,
		subject.Type, subject.ID, key, now, now,
		subject.Type, subject.ID, key, now, now,
		subject.Type, subject.ID, key, now, now, cost,
	).Scan(&found)
	return found, err
}
