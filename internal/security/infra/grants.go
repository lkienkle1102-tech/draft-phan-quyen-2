package infra

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/security/application"
)

func (r *Repository) CreateGrant(ctx context.Context, command application.GrantCommand) (returnErr error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	var resourceID any
	if command.Resource.ID != "" {
		resourceID = command.Resource.ID
	}
	var validUntil any
	if command.ValidUntil != nil {
		validUntil = command.ValidUntil.UTC().Format(time.RFC3339)
	}
	var granteeUserID any
	if command.GranteeUserID != "" {
		granteeUserID = command.GranteeUserID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO organization_access_grants(id,owner_organization_id,grantee_organization_id,resource_type,resource_id,action,valid_from,valid_until,status,allow_subdelegation,created_by,grantee_user_id) VALUES(?,?,?,?,?,?,?,?,'active',0,?,?)`, command.ID, command.OwnerOrganizationID, command.GranteeOrganizationID, command.Resource.Type, resourceID, command.Operation.Action, command.ValidFrom.UTC().Format(time.RFC3339), validUntil, command.CreatedBy, granteeUserID)
	if err != nil {
		return err
	}
	for _, feature := range command.Features {
		if _, err = tx.ExecContext(ctx, `INSERT INTO organization_feature_grants VALUES(?,?)`, command.ID, feature); err != nil {
			return err
		}
	}
	for key, amount := range command.Quotas {
		if _, err = tx.ExecContext(ctx, `INSERT INTO organization_quota_allocations(access_grant_id,quota_key,allocated) VALUES(?,?,?)`, command.ID, key, amount); err != nil {
			return err
		}
	}
	return tx.Commit()
}
