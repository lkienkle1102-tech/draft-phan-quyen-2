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

func (r *Repository) ClaimAcceptance(ctx context.Context, tokenHash, userID string, at, leaseUntil time.Time) (_ domain.InvitationAcceptance, returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return domain.InvitationAcceptance{}, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	acceptance, startedAt, err := claimInvitationAcceptance(ctx, tx, tokenHash, userID, at, leaseUntil)
	if err != nil {
		return domain.InvitationAcceptance{}, err
	}
	if err = ensureProvisioningMembership(ctx, tx, &acceptance, userID, startedAt); err != nil {
		return domain.InvitationAcceptance{}, err
	}
	if err = loadInvitationRoles(ctx, tx, &acceptance); err != nil {
		return domain.InvitationAcceptance{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.InvitationAcceptance{}, err
	}
	return acceptance, nil
}

func claimInvitationAcceptance(ctx context.Context, tx *sql.Tx, tokenHash, userID string, at, leaseUntil time.Time) (domain.InvitationAcceptance, string, error) {
	now := at.UTC().Format(time.RFC3339)
	lease := leaseUntil.UTC().Format(time.RFC3339)
	var acceptance domain.InvitationAcceptance
	var startedAt string
	var attemptCount int
	err := tx.QueryRowContext(ctx, `INSERT INTO invitation_acceptances_v2(invitation_id,membership_id,claim_id,lease_until,started_at)
		SELECT invitation.id,'invitation:'||invitation.id,lower(hex(randomblob(16))),?,?
		FROM organization_invitations_v2 invitation
		WHERE invitation.token_hash=? AND invitation.user_id=? AND invitation.status='pending' AND(
			(NOT EXISTS(SELECT 1 FROM invitation_acceptances_v2 current WHERE current.invitation_id=invitation.id)
			 AND invitation.valid_from<=? AND invitation.valid_until>?)
			OR EXISTS(SELECT 1 FROM invitation_acceptances_v2 current WHERE current.invitation_id=invitation.id AND current.lease_until<=?)
		)
		ON CONFLICT(invitation_id) DO UPDATE SET claim_id=lower(hex(randomblob(16))),lease_until=excluded.lease_until,attempt_count=invitation_acceptances_v2.attempt_count+1
		WHERE invitation_acceptances_v2.lease_until<=?
		RETURNING invitation_id,membership_id,claim_id,started_at,attempt_count`, lease, now, tokenHash, userID, now, now, now, now).
		Scan(&acceptance.Invitation.ID, &acceptance.MembershipID, &acceptance.ClaimID, &startedAt, &attemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InvitationAcceptance{}, "", ErrApplicationNotPending
	}
	if err != nil {
		return domain.InvitationAcceptance{}, "", err
	}
	acceptance.Recovery = attemptCount > 1
	return acceptance, startedAt, nil
}

func ensureProvisioningMembership(ctx context.Context, tx *sql.Tx, acceptance *domain.InvitationAcceptance, userID, startedAt string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO organization_members(id,organization_id,user_id,active,provisioning,joined_at)
		SELECT ?,organization_id,user_id,0,1,? FROM organization_invitations_v2 WHERE id=?
		ON CONFLICT(id) DO NOTHING`, acceptance.MembershipID, startedAt, acceptance.Invitation.ID); err != nil {
		return err
	}
	var active, provisioning int
	var validFrom, validUntil string
	if err := tx.QueryRowContext(ctx, `SELECT member.organization_id,member.user_id,member.active,member.provisioning,
		invitation.token_hash,invitation.invited_by,invitation.valid_from,invitation.valid_until
		FROM organization_members member JOIN organization_invitations_v2 invitation ON invitation.id=?
		WHERE member.id=?`, acceptance.Invitation.ID, acceptance.MembershipID).
		Scan(&acceptance.Invitation.OrganizationID, &acceptance.Invitation.UserID, &active, &provisioning,
			&acceptance.Invitation.TokenHash, &acceptance.Invitation.InvitedBy, &validFrom, &validUntil); err != nil {
		return err
	}
	if acceptance.Invitation.UserID != userID || active != 0 || provisioning != 1 {
		return ErrApplicationNotPending
	}
	var err error
	if acceptance.Invitation.ValidFrom, err = time.Parse(time.RFC3339, validFrom); err != nil {
		return err
	}
	if acceptance.Invitation.ValidUntil, err = time.Parse(time.RFC3339, validUntil); err != nil {
		return err
	}
	return nil
}

func loadInvitationRoles(ctx context.Context, tx *sql.Tx, acceptance *domain.InvitationAcceptance) error {
	rows, err := tx.QueryContext(ctx, `SELECT role_id FROM organization_invitation_roles_v2 WHERE invitation_id=? ORDER BY role_id`, acceptance.Invitation.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var roleID string
		if err = rows.Scan(&roleID); err != nil {
			return err
		}
		acceptance.Invitation.RoleIDs = append(acceptance.Invitation.RoleIDs, roleID)
	}
	return rows.Err()
}

func (r *Repository) CompleteAcceptance(ctx context.Context, claimID string, at time.Time) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var invitationID, membershipID, organizationID, userID string
	if err = tx.QueryRowContext(ctx, `SELECT acceptance.invitation_id,acceptance.membership_id,invitation.organization_id,invitation.user_id
		FROM invitation_acceptances_v2 acceptance JOIN organization_invitations_v2 invitation ON invitation.id=acceptance.invitation_id
		WHERE acceptance.claim_id=? AND invitation.status='pending'`, claimID).
		Scan(&invitationID, &membershipID, &organizationID, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrApplicationNotPending
		}
		return err
	}
	now := at.UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, `UPDATE organization_members SET active=1,provisioning=0,joined_at=?
		WHERE id=? AND organization_id=? AND user_id=? AND active=0 AND provisioning=1`, now, membershipID, organizationID, userID)
	if err != nil {
		return err
	}
	if err = exactlyOneInvitation(result); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE organization_invitations_v2 SET status='accepted',accepted_at=? WHERE id=? AND status='pending'`, now, invitationID)
	if err != nil {
		return err
	}
	if err := exactlyOneInvitation(result); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM invitation_acceptances_v2 WHERE claim_id=?`, claimID)
	if err != nil {
		return err
	}
	if err = exactlyOneInvitation(result); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ReleaseAcceptance(ctx context.Context, claimID string, at time.Time) error {
	_, err := r.database.ExecContext(ctx, `UPDATE invitation_acceptances_v2 SET lease_until=? WHERE claim_id=?`, at.UTC().Format(time.RFC3339), claimID)
	return err
}

func (r *Repository) AbortAcceptance(ctx context.Context, claimID string) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var membershipID string
	err = tx.QueryRowContext(ctx, `DELETE FROM invitation_acceptances_v2 WHERE claim_id=? RETURNING membership_id`, claimID).Scan(&membershipID)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM organization_members WHERE id=? AND active=0 AND provisioning=1`, membershipID)
	if err != nil {
		return err
	}
	if err = exactlyOneInvitation(result); err != nil {
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
