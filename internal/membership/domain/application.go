// Package domain contains organization membership invariants.
package domain

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

type Application struct {
	ID, OrganizationID, UserID, ReviewedBy string
	Status                                 Status
	CreatedAt                              time.Time
	ReviewedAt                             *time.Time
}
