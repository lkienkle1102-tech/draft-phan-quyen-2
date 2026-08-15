package infra

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/membership/domain"
)

func (r *Repository) CreateInvitation(ctx context.Context, invitation domain.Invitation) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	if _, err := tx.ExecContext(ctx, `INSERT INTO organization_invitations_v2(id,organization_id,user_id,token_hash,status,invited_by,valid_from,valid_until) VALUES(?,?,?,?,'pending',?,?,?)`, invitation.ID, invitation.OrganizationID, invitation.UserID, invitation.TokenHash, invitation.InvitedBy, invitation.ValidFrom.UTC().Format(time.RFC3339), invitation.ValidUntil.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	for _, roleID := range invitation.RoleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO organization_invitation_roles_v2(invitation_id,role_id) VALUES(?,?)`, invitation.ID, roleID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) LoadInvitation(ctx context.Context, tokenHash, userID string, at time.Time) (domain.Invitation, error) {
	var invitation domain.Invitation
	var status, validFrom, validUntil string
	if err := r.database.QueryRowContext(ctx, `SELECT id,organization_id,user_id,status,valid_from,valid_until FROM organization_invitations_v2 WHERE token_hash=? AND user_id=?`, tokenHash, userID).Scan(&invitation.ID, &invitation.OrganizationID, &invitation.UserID, &status, &validFrom, &validUntil); err != nil {
		return domain.Invitation{}, err
	}
	now := at.UTC().Format(time.RFC3339)
	if status != "pending" || validFrom > now || validUntil <= now {
		return domain.Invitation{}, ErrApplicationNotPending
	}
	rows, err := r.database.QueryContext(ctx, `SELECT role_id FROM organization_invitation_roles_v2 WHERE invitation_id=? ORDER BY role_id`, invitation.ID)
	if err != nil {
		return domain.Invitation{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var roleID string
		if err = rows.Scan(&roleID); err != nil {
			return domain.Invitation{}, err
		}
		invitation.RoleIDs = append(invitation.RoleIDs, roleID)
	}
	if err = rows.Err(); err != nil {
		return domain.Invitation{}, err
	}
	return invitation, nil
}

func (r *Repository) AcceptInvitation(ctx context.Context, tokenHash, userID string, at time.Time) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var invitationID, organizationID, status, validFrom, validUntil string
	if err := tx.QueryRowContext(ctx, `SELECT id,organization_id,status,valid_from,valid_until FROM organization_invitations_v2 WHERE token_hash=? AND user_id=?`, tokenHash, userID).Scan(&invitationID, &organizationID, &status, &validFrom, &validUntil); err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339)
	if status != "pending" || validFrom > now || validUntil <= now {
		return ErrApplicationNotPending
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES(?,?,?,1,?)`, "invitation:"+invitationID, organizationID, userID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE organization_invitations_v2 SET status='accepted',accepted_at=? WHERE id=? AND status='pending'`, now, invitationID)
	if err != nil {
		return err
	}
	if err := exactlyOneInvitation(result); err != nil {
		return err
	}
	return tx.Commit()
}

func exactlyOneInvitation(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrApplicationNotPending
	}
	return nil
}
