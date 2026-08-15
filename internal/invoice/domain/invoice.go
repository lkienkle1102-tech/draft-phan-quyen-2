// Package domain contains invoice invariants.
package domain

import "errors"

type Invoice struct {
	ID, OrganizationID, OwnerID, ApproverID, Status, Region string
	Amount, Version                                         int64
}

var ErrNotPending = errors.New("invoice is not pending")
var ErrWrongApprover = errors.New("actor is not assigned approver")

func CanApprove(actorID string, invoice Invoice) error {
	if invoice.Status != "pending" {
		return ErrNotPending
	}
	if invoice.ApproverID != "" && invoice.ApproverID != actorID {
		return ErrWrongApprover
	}
	return nil
}
