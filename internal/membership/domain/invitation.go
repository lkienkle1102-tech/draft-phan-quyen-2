package domain

import (
	"errors"
	"time"
)

type Invitation struct {
	ID, OrganizationID, UserID, TokenHash, InvitedBy string
	RoleIDs                                          []string
	ValidFrom, ValidUntil                            time.Time
}

type InvitationAcceptance struct {
	ClaimID, MembershipID string
	Invitation            Invitation
	Recovery              bool
}

func (i Invitation) Validate() error {
	if i.ID == "" || i.OrganizationID == "" || i.UserID == "" || i.TokenHash == "" || i.InvitedBy == "" || i.ValidFrom.IsZero() || !i.ValidUntil.After(i.ValidFrom) || len(i.RoleIDs) == 0 {
		return errors.New("invalid organization invitation")
	}
	return nil
}
