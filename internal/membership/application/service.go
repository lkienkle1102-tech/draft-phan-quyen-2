// Package application implements organization membership use cases.
package application

import (
	"context"
	"errors"

	security "example.com/phan-quyen-golang/internal/security/domain"
)

var ErrAlreadyMember = errors.New("already an organization member")
var ErrApplicationPending = errors.New("membership application already pending")
var ErrInvalidDecision = errors.New("invalid authorization decision")

type Repository interface {
	IsActiveMember(context.Context, string, string) (bool, error)
	HasPendingApplication(context.Context, string, string) (bool, error)
	CreateApplication(context.Context, string, string, string, []security.QuotaCost) error
	ReviewApplication(context.Context, string, string, string, bool) error
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Apply(ctx context.Context, id, organizationID, userID string, decision security.Decision) error {
	if !decision.Allowed || decision.Operation != (security.Operation{ResourceType: "organization_membership", Action: "apply"}) {
		return ErrInvalidDecision
	}
	member, err := s.repository.IsActiveMember(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if member {
		return ErrAlreadyMember
	}
	pending, err := s.repository.HasPendingApplication(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if pending {
		return ErrApplicationPending
	}
	return s.repository.CreateApplication(ctx, id, organizationID, userID, decision.QuotaCosts)
}

func (s *Service) Review(ctx context.Context, id, organizationID, reviewerID string, approve bool, decision security.Decision) error {
	if !decision.Allowed || decision.Operation != (security.Operation{ResourceType: "organization_membership", Action: "review"}) {
		return ErrInvalidDecision
	}
	return s.repository.ReviewApplication(ctx, id, organizationID, reviewerID, approve)
}
