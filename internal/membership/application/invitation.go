package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/membership/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
)

var ErrInvalidInvitation = errors.New("invalid organization invitation")

const invitationAcceptanceLease = time.Minute

type InvitationRepository interface {
	CreateInvitation(context.Context, domain.Invitation) error
	ClaimAcceptance(context.Context, string, string, time.Time, time.Time) (domain.InvitationAcceptance, error)
	CompleteAcceptance(context.Context, string, time.Time) error
	ReleaseAcceptance(context.Context, string, time.Time) error
	AbortAcceptance(context.Context, string) error
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
	acceptance, err := s.repository.ClaimAcceptance(ctx, tokenHash, userID, at, at.Add(invitationAcceptanceLease))
	if err != nil {
		return err
	}
	invitation := acceptance.Invitation
	actor := security.Actor{ID: userID, Type: security.ActorUser}
	subject := security.Subject{Type: security.SubjectOrganization, ID: invitation.OrganizationID}
	syncResult, err := s.directory.EnsureMembershipRoles(ctx, actor, subject, acceptance.MembershipID, invitation.RoleIDs)
	if err != nil {
		if !acceptance.Recovery && !syncResult.ExternalMutationPossible {
			return errors.Join(err, s.repository.AbortAcceptance(ctx, acceptance.ClaimID))
		}
		return errors.Join(err, s.repository.ReleaseAcceptance(ctx, acceptance.ClaimID, at))
	}
	if err = s.repository.CompleteAcceptance(ctx, acceptance.ClaimID, at); err != nil {
		return errors.Join(err, s.repository.ReleaseAcceptance(ctx, acceptance.ClaimID, at))
	}
	return nil
}
