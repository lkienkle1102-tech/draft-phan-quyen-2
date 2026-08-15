// Package infra implements membership persistence.
package infra

import (
	"context"
	"database/sql"
	"errors"
	"time"

	security "example.com/phan-quyen-golang/internal/security/domain"
	sharedquota "example.com/phan-quyen-golang/internal/shared/quota"
)

var ErrApplicationNotPending = errors.New("membership application is not pending")

type Repository struct{ database *sql.DB }

func NewRepository(database *sql.DB) *Repository { return &Repository{database: database} }

func (r *Repository) IsActiveMember(ctx context.Context, organizationID, userID string) (bool, error) {
	var found bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=? AND user_id=? AND active=1)`, organizationID, userID).Scan(&found)
	return found, err
}

func (r *Repository) HasPendingApplication(ctx context.Context, organizationID, userID string) (bool, error) {
	var found bool
	err := r.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organization_membership_applications WHERE organization_id=? AND user_id=? AND status='pending')`, organizationID, userID).Scan(&found)
	return found, err
}

func (r *Repository) CreateApplication(ctx context.Context, id, organizationID, userID string, costs []security.QuotaCost) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	operation := sharedquota.Operation{ID: id, ActorID: userID, ResourceType: "organization_membership", Action: "apply", ExpiresAt: time.Now().UTC().Add(time.Minute)}
	if err := sharedquota.ConsumeQuotas(ctx, tx, costs, operation); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO organization_membership_applications(id,organization_id,user_id,status,created_at) VALUES(?,?,?,'pending',?)`, id, organizationID, userID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ReviewApplication(ctx context.Context, id, organizationID, reviewerID string, approve bool) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var userID, status string
	if err := tx.QueryRowContext(ctx, `SELECT user_id,status FROM organization_membership_applications WHERE id=? AND organization_id=?`, id, organizationID).Scan(&userID, &status); err != nil {
		return err
	}
	if status != "pending" {
		return ErrApplicationNotPending
	}
	next := "rejected"
	if approve {
		next = "approved"
		if _, err := tx.ExecContext(ctx, `INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES(?,?,?,1,?)`, "application:"+id, organizationID, userID, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE organization_membership_applications SET status=?,reviewed_by=?,reviewed_at=? WHERE id=? AND organization_id=? AND status='pending'`, next, reviewerID, time.Now().UTC().Format(time.RFC3339), id, organizationID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrApplicationNotPending
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,resource_id,action,policy_id,policy_version) VALUES(?,?,?,?,?)`, reviewerID, id, "organization_membership.review", "membership-review", 1); err != nil {
		return err
	}
	return tx.Commit()
}
