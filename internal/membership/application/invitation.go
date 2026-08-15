package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/membership/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
)

var ErrInvalidInvitation = errors.New("invalid organization invitation")

type InvitationRepository interface {
	CreateInvitation(context.Context, domain.Invitation) error
	LoadInvitation(context.Context, string, string, time.Time) (domain.Invitation, error)
	AcceptInvitation(context.Context, string, string, time.Time) error
}

type InvitationService struct {
	repository InvitationRepository
	directory  security.AuthorizationDirectory
}

func NewInvitationService(repository InvitationRepository, directory security.AuthorizationDirectory) *InvitationService {
	return &InvitationService{repository: repository, directory: directory}
}

func (s *InvitationService) Invite(ctx context.Context, invitation domain.Invitation) error {
	if err := invitation.Validate(); err != nil {
		return errors.Join(ErrInvalidInvitation, err)
	}
	subject := security.Subject{Type: security.SubjectOrganization, ID: invitation.OrganizationID}
	if err := s.directory.ValidateRoles(ctx, subject, invitation.RoleIDs); err != nil {
		return errors.Join(ErrInvalidInvitation, err)
	}
	return s.repository.CreateInvitation(ctx, invitation)
}

func (s *InvitationService) Accept(ctx context.Context, tokenHash, userID string, at time.Time) error {
	if tokenHash == "" || userID == "" || at.IsZero() {
		return ErrInvalidInvitation
	}
	invitation, err := s.repository.LoadInvitation(ctx, tokenHash, userID, at)
	if err != nil {
		return err
	}
	actor := security.Actor{ID: userID, Type: security.ActorUser}
	subject := security.Subject{Type: security.SubjectOrganization, ID: invitation.OrganizationID}
	receipt, err := s.directory.EnsureRoles(ctx, actor, subject, invitation.RoleIDs)
	if err != nil {
		return err
	}
	if err = s.repository.AcceptInvitation(ctx, tokenHash, userID, at); err != nil {
		return errors.Join(err, s.directory.CompensateRoles(ctx, receipt))
	}
	return nil
}
