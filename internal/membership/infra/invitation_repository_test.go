package infra_test

import (
	"context"
	"testing"
	"time"

	membershipapp "example.com/phan-quyen-golang/internal/membership/application"
	"example.com/phan-quyen-golang/internal/membership/domain"
	membershipinfra "example.com/phan-quyen-golang/internal/membership/infra"
	security "example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/shared/casdoortest"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestInvitationAssignsOnlyOrganizationRole(t *testing.T) {
	database := testutil.Database(t)
	repository := membershipinfra.NewRepository(database)
	directory := casdoortest.NewFakeCasdoor()
	service := membershipapp.NewInvitationService(repository, directory)
	now := time.Now().UTC().Truncate(time.Second)
	invitation := domain.Invitation{ID: "invitation", OrganizationID: "org-a", UserID: "user-personal", TokenHash: "hash", InvitedBy: "user-a", RoleIDs: []string{"organization:org-a:finance"}, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	if err := service.Invite(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	if err := service.Accept(context.Background(), invitation.TokenHash, invitation.UserID, now); err != nil {
		t.Fatal(err)
	}
	var membership bool
	if err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id='org-a' AND user_id='user-personal' AND active=1)`).Scan(&membership); err != nil {
		t.Fatal(err)
	}
	allowed, err := directory.Enforce(context.Background(), security.Actor{ID: "user-personal", Type: security.ActorUser}, security.Subject{Type: security.SubjectOrganization, ID: "org-a"}, security.Operation{ResourceType: "invoice", Action: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if !membership || !allowed {
		t.Fatalf("membership=%v roleAllowed=%v", membership, allowed)
	}
}

func TestInvitationRejectsRoleFromAnotherOrganization(t *testing.T) {
	database := testutil.Database(t)
	service := membershipapp.NewInvitationService(membershipinfra.NewRepository(database), casdoortest.NewFakeCasdoor())
	now := time.Now().UTC()
	invitation := domain.Invitation{ID: "wrong-role", OrganizationID: "org-a", UserID: "user-personal", TokenHash: "other-hash", InvitedBy: "user-a", RoleIDs: []string{"organization:org-b:finance"}, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	if err := service.Invite(context.Background(), invitation); err == nil {
		t.Fatal("cross-organization role was accepted")
	}
}
