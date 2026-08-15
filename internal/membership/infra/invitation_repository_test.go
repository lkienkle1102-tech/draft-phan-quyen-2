package infra_test

import (
	"context"
	"testing"
	"time"

	membershipapp "example.com/phan-quyen-golang/internal/membership/application"
	"example.com/phan-quyen-golang/internal/membership/domain"
	membershipinfra "example.com/phan-quyen-golang/internal/membership/infra"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestInvitationAssignsOnlyOrganizationRole(t *testing.T) {
	database := testutil.Database(t)
	repository := membershipinfra.NewRepository(database)
	service := membershipapp.NewInvitationService(repository)
	now := time.Now().UTC().Truncate(time.Second)
	invitation := domain.Invitation{ID: "invitation", OrganizationID: "org-a", UserID: "user-personal", TokenHash: "hash", InvitedBy: "user-a", RoleIDs: []string{"organization:org-a:finance"}, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	if err := service.Invite(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	if err := service.Accept(context.Background(), invitation.TokenHash, invitation.UserID, now); err != nil {
		t.Fatal(err)
	}
	var membership, orgRole, personalRole bool
	if err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id='org-a' AND user_id='user-personal' AND active=1)`).Scan(&membership); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM role_assignments_v2 WHERE subject_type='organization' AND subject_id='org-a' AND user_id='user-personal' AND role_id='organization:org-a:finance')`).Scan(&orgRole); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM role_assignments_v2 WHERE subject_type='user' AND subject_id='user-personal' AND user_id='user-personal' AND role_id='organization:org-a:finance')`).Scan(&personalRole); err != nil {
		t.Fatal(err)
	}
	if !membership || !orgRole || personalRole {
		t.Fatalf("membership=%v orgRole=%v personalRole=%v", membership, orgRole, personalRole)
	}
}

func TestInvitationRejectsRoleFromAnotherOrganization(t *testing.T) {
	database := testutil.Database(t)
	service := membershipapp.NewInvitationService(membershipinfra.NewRepository(database))
	now := time.Now().UTC()
	invitation := domain.Invitation{ID: "wrong-role", OrganizationID: "org-a", UserID: "user-personal", TokenHash: "other-hash", InvitedBy: "user-a", RoleIDs: []string{"organization:org-b:finance"}, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	if err := service.Invite(context.Background(), invitation); err == nil {
		t.Fatal("cross-organization role was accepted")
	}
}
