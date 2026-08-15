package application

import (
	"context"
	"errors"
	"testing"
	"time"

	membership "example.com/phan-quyen-golang/internal/membership/domain"
	"example.com/phan-quyen-golang/internal/security/domain"
)

func TestInvitationValidatesRolesBeforePersistence(t *testing.T) {
	directory := &invitationDirectory{validateErr: errors.New("wrong domain")}
	repository := &invitationRepository{}
	service := NewInvitationService(repository, directory)
	now := time.Now().UTC()
	value := membership.Invitation{ID: "invitation", OrganizationID: "org-a", UserID: "user-a", TokenHash: "hash", InvitedBy: "admin", RoleIDs: []string{"role-b"}, ValidFrom: now, ValidUntil: now.Add(time.Hour)}
	if err := service.Invite(context.Background(), value); !errors.Is(err, ErrInvalidInvitation) || repository.created {
		t.Fatalf("err=%v created=%v", err, repository.created)
	}
}

func TestInvitationAbortsTechnicalStateWhenRoleSyncDidNotMutate(t *testing.T) {
	repository := acceptanceRepository()
	directory := &invitationDirectory{syncErr: errors.New("validation failed")}
	err := NewInvitationService(repository, directory).Accept(context.Background(), "hash", "user-a", time.Now().UTC())
	if err == nil || !repository.aborted || repository.released || repository.completed {
		t.Fatalf("err=%v repository=%+v", err, repository)
	}
}

func TestInvitationKeepsProvisioningWhenRoleSyncMayHaveMutated(t *testing.T) {
	repository := acceptanceRepository()
	directory := &invitationDirectory{syncResult: domain.RoleSyncResult{ExternalMutationPossible: true}, syncErr: errors.New("Casdoor timeout")}
	err := NewInvitationService(repository, directory).Accept(context.Background(), "hash", "user-a", time.Now().UTC())
	if err == nil || repository.aborted || !repository.released || repository.completed {
		t.Fatalf("err=%v repository=%+v", err, repository)
	}
}

func TestInvitationKeepsProvisioningWhenRecoveryFailsBeforeMutation(t *testing.T) {
	repository := acceptanceRepository()
	repository.acceptance.Recovery = true
	directory := &invitationDirectory{syncErr: errors.New("Casdoor unavailable")}
	err := NewInvitationService(repository, directory).Accept(context.Background(), "hash", "user-a", time.Now().UTC())
	if err == nil || repository.aborted || !repository.released || repository.completed {
		t.Fatalf("err=%v repository=%+v", err, repository)
	}
}

func TestInvitationReleasesAcceptanceWhenCompletionFails(t *testing.T) {
	repository := acceptanceRepository()
	repository.completeErr = errors.New("database failed")
	err := NewInvitationService(repository, &invitationDirectory{}).Accept(context.Background(), "hash", "user-a", time.Now().UTC())
	if err == nil || !repository.completed || !repository.released || repository.aborted {
		t.Fatalf("err=%v repository=%+v", err, repository)
	}
}

func TestInvitationCompletesAfterRolesAreReady(t *testing.T) {
	repository := acceptanceRepository()
	directory := &invitationDirectory{}
	if err := NewInvitationService(repository, directory).Accept(context.Background(), "hash", "user-a", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if !repository.completed || repository.released || repository.aborted || directory.membershipID != "invitation:invitation" {
		t.Fatalf("repository=%+v membershipID=%q", repository, directory.membershipID)
	}
}

type invitationRepository struct {
	acceptance                  membership.InvitationAcceptance
	created, completed          bool
	released, aborted           bool
	completeErr, release, abort error
}

func acceptanceRepository() *invitationRepository {
	invitation := membership.Invitation{ID: "invitation", OrganizationID: "org-a", UserID: "user-a", TokenHash: "hash", RoleIDs: []string{"finance"}}
	return &invitationRepository{acceptance: membership.InvitationAcceptance{ClaimID: "claim", MembershipID: "invitation:invitation", Invitation: invitation}}
}

func (f *invitationRepository) CreateInvitation(context.Context, membership.Invitation) error {
	f.created = true
	return nil
}

func (f *invitationRepository) ClaimAcceptance(context.Context, string, string, time.Time, time.Time) (membership.InvitationAcceptance, error) {
	return f.acceptance, nil
}

func (f *invitationRepository) CompleteAcceptance(context.Context, string, time.Time) error {
	f.completed = true
	return f.completeErr
}

func (f *invitationRepository) ReleaseAcceptance(context.Context, string, time.Time) error {
	f.released = true
	return f.release
}

func (f *invitationRepository) AbortAcceptance(context.Context, string) error {
	f.aborted = true
	return f.abort
}

type invitationDirectory struct {
	validateErr, syncErr error
	syncResult           domain.RoleSyncResult
	membershipID         string
}

func (f *invitationDirectory) Snapshot(context.Context) (domain.AuthorizationSnapshot, error) {
	return domain.AuthorizationSnapshot{}, nil
}

func (f *invitationDirectory) ValidateRoles(context.Context, domain.Subject, []string) error {
	return f.validateErr
}

func (f *invitationDirectory) ValidateGroups(context.Context, domain.Subject, []string) error {
	return nil
}

func (f *invitationDirectory) EnsureMembershipRoles(_ context.Context, _ domain.Actor, _ domain.Subject, membershipID string, _ []string) (domain.RoleSyncResult, error) {
	f.membershipID = membershipID
	return f.syncResult, f.syncErr
}
