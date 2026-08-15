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

func TestInvitationCompensatesOnlyCreatedPoliciesWhenDatabaseFails(t *testing.T) {
	now := time.Now().UTC()
	value := membership.Invitation{ID: "invitation", OrganizationID: "org-a", UserID: "user-a", TokenHash: "hash", RoleIDs: []string{"finance"}}
	added := domain.PolicyRule{PType: "g", V0: "user::user-a", V1: "role::finance", V2: "organization::org-a"}
	directory := &invitationDirectory{receipt: domain.AssignmentReceipt{Added: []domain.PolicyRule{added}}}
	repository := &invitationRepository{loaded: value, acceptErr: errors.New("database failed")}
	service := NewInvitationService(repository, directory)
	err := service.Accept(context.Background(), "hash", "user-a", now)
	if err == nil || !repository.accepted || len(directory.compensated.Added) != 1 || directory.compensated.Added[0] != added {
		t.Fatalf("err=%v accepted=%v compensated=%+v", err, repository.accepted, directory.compensated)
	}
}

func TestInvitationAssignmentFailureDoesNotWriteMembership(t *testing.T) {
	repository := &invitationRepository{loaded: membership.Invitation{OrganizationID: "org-a", RoleIDs: []string{"finance"}}}
	directory := &invitationDirectory{ensureErr: errors.New("Casdoor unavailable")}
	err := NewInvitationService(repository, directory).Accept(context.Background(), "hash", "user-a", time.Now().UTC())
	if err == nil || repository.accepted {
		t.Fatalf("err=%v accepted=%v", err, repository.accepted)
	}
}

type invitationRepository struct {
	loaded            membership.Invitation
	created, accepted bool
	acceptErr         error
}

func (f *invitationRepository) CreateInvitation(context.Context, membership.Invitation) error {
	f.created = true
	return nil
}
func (f *invitationRepository) LoadInvitation(context.Context, string, string, time.Time) (membership.Invitation, error) {
	return f.loaded, nil
}
func (f *invitationRepository) AcceptInvitation(context.Context, string, string, time.Time) error {
	f.accepted = true
	return f.acceptErr
}

type invitationDirectory struct {
	validateErr, ensureErr error
	receipt, compensated   domain.AssignmentReceipt
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
func (f *invitationDirectory) EnsureRoles(context.Context, domain.Actor, domain.Subject, []string) (domain.AssignmentReceipt, error) {
	return f.receipt, f.ensureErr
}
func (f *invitationDirectory) CompensateRoles(_ context.Context, receipt domain.AssignmentReceipt) error {
	f.compensated = receipt
	return nil
}
