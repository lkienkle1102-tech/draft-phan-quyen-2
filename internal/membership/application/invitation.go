package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/membership/domain"
)

var ErrInvalidInvitation = errors.New("invalid organization invitation")

type InvitationRepository interface {
	CreateInvitation(context.Context, domain.Invitation) error
	AcceptInvitation(context.Context, string, string, time.Time) error
}

type InvitationService struct{ repository InvitationRepository }

func NewInvitationService(repository InvitationRepository) *InvitationService {
	return &InvitationService{repository: repository}
}

func (s *InvitationService) Invite(ctx context.Context, invitation domain.Invitation) error {
	if err := invitation.Validate(); err != nil {
		return errors.Join(ErrInvalidInvitation, err)
	}
	return s.repository.CreateInvitation(ctx, invitation)
}

func (s *InvitationService) Accept(ctx context.Context, tokenHash, userID string, at time.Time) error {
	if tokenHash == "" || userID == "" || at.IsZero() {
		return ErrInvalidInvitation
	}
	return s.repository.AcceptInvitation(ctx, tokenHash, userID, at)
}
