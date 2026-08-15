package infra_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	membershipapp "example.com/phan-quyen-golang/internal/membership/application"
	"example.com/phan-quyen-golang/internal/membership/domain"
	membershipinfra "example.com/phan-quyen-golang/internal/membership/infra"
	security "example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/shared/app"
	"example.com/phan-quyen-golang/internal/shared/casdoortest"
	"example.com/phan-quyen-golang/internal/shared/database/sqlite"
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

func TestInvitationAcceptanceLeaseAndFencing(t *testing.T) {
	database := testutil.Database(t)
	repository := membershipinfra.NewRepository(database)
	now := time.Now().UTC().Truncate(time.Second)
	invitation := testInvitation(now, "leased-invitation")
	if err := repository.CreateInvitation(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}

	first, err := repository.ClaimAcceptance(context.Background(), invitation.TokenHash, invitation.UserID, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Recovery {
		t.Fatal("first acceptance was marked as recovery")
	}
	assertInvitationState(t, database, invitation.ID, "pending", first.MembershipID, 0, 1, 1)

	if _, err = repository.ClaimAcceptance(context.Background(), invitation.TokenHash, invitation.UserID, now.Add(time.Second), now.Add(time.Minute)); !errors.Is(err, membershipinfra.ErrApplicationNotPending) {
		t.Fatalf("concurrent claim error=%v", err)
	}
	if err = repository.ReleaseAcceptance(context.Background(), first.ClaimID, now); err != nil {
		t.Fatal(err)
	}
	second, err := repository.ClaimAcceptance(context.Background(), invitation.TokenHash, invitation.UserID, now.Add(time.Second), now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.ClaimID == first.ClaimID || second.MembershipID != first.MembershipID {
		t.Fatalf("reclaimed acceptance=%+v first=%+v", second, first)
	}
	if !second.Recovery {
		t.Fatal("reclaimed acceptance was not marked as recovery")
	}

	if err = repository.CompleteAcceptance(context.Background(), first.ClaimID, now.Add(2*time.Second)); !errors.Is(err, membershipinfra.ErrApplicationNotPending) {
		t.Fatalf("stale complete error=%v", err)
	}
	if err = repository.AbortAcceptance(context.Background(), first.ClaimID); err != nil {
		t.Fatal(err)
	}
	assertInvitationState(t, database, invitation.ID, "pending", second.MembershipID, 0, 1, 1)

	if err = repository.CompleteAcceptance(context.Background(), second.ClaimID, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertInvitationState(t, database, invitation.ID, "accepted", second.MembershipID, 1, 0, 0)
}

func TestAbortAcceptanceRemovesOnlyProvisioningState(t *testing.T) {
	database := testutil.Database(t)
	repository := membershipinfra.NewRepository(database)
	now := time.Now().UTC().Truncate(time.Second)
	invitation := testInvitation(now, "aborted-invitation")
	if err := repository.CreateInvitation(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	acceptance, err := repository.ClaimAcceptance(context.Background(), invitation.TokenHash, invitation.UserID, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.AbortAcceptance(context.Background(), acceptance.ClaimID); err != nil {
		t.Fatal(err)
	}

	var status string
	if err = database.QueryRow(`SELECT status FROM organization_invitations_v2 WHERE id=?`, invitation.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	var members, acceptances int
	if err = database.QueryRow(`SELECT COUNT(*) FROM organization_members WHERE id=?`, acceptance.MembershipID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT COUNT(*) FROM invitation_acceptances_v2 WHERE invitation_id=?`, invitation.ID).Scan(&acceptances); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || members != 0 || acceptances != 0 {
		t.Fatalf("status=%s members=%d acceptances=%d", status, members, acceptances)
	}
}

func TestConcurrentInvitationClaimsHaveOneOwner(t *testing.T) {
	repositories := concurrentInvitationRepositories(t)
	now := time.Now().UTC().Truncate(time.Second)
	invitation := testInvitation(now, "concurrent-invitation")
	if err := repositories[0].CreateInvitation(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	assertOneClaimOwner(t, claimConcurrently(repositories, invitation, now))
}

func concurrentInvitationRepositories(t *testing.T) []*membershipinfra.Repository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "concurrent.sqlite")
	firstDatabase, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstDatabase.Close() })
	if err = app.Migrate(firstDatabase); err != nil {
		t.Fatal(err)
	}
	secondDatabase, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondDatabase.Close() })
	return []*membershipinfra.Repository{
		membershipinfra.NewRepository(firstDatabase),
		membershipinfra.NewRepository(secondDatabase),
	}
}

func claimConcurrently(repositories []*membershipinfra.Repository, invitation domain.Invitation, now time.Time) <-chan error {
	start := make(chan struct{})
	results := make(chan error, len(repositories))
	var workers sync.WaitGroup
	for _, repository := range repositories {
		workers.Add(1)
		go func(repository *membershipinfra.Repository) {
			defer workers.Done()
			<-start
			_, claimErr := repository.ClaimAcceptance(context.Background(), invitation.TokenHash, invitation.UserID, now, now.Add(time.Minute))
			results <- claimErr
		}(repository)
	}
	close(start)
	workers.Wait()
	close(results)
	return results
}

func assertOneClaimOwner(t *testing.T, results <-chan error) {
	t.Helper()
	var claimed, rejected int
	for claimErr := range results {
		switch {
		case claimErr == nil:
			claimed++
		case errors.Is(claimErr, membershipinfra.ErrApplicationNotPending):
			rejected++
		default:
			t.Fatalf("unexpected claim error: %v", claimErr)
		}
	}
	if claimed != 1 || rejected != 1 {
		t.Fatalf("claimed=%d rejected=%d", claimed, rejected)
	}
}

func testInvitation(now time.Time, id string) domain.Invitation {
	return domain.Invitation{
		ID:             id,
		OrganizationID: "org-a",
		UserID:         "user-personal",
		TokenHash:      id + "-hash",
		InvitedBy:      "user-a",
		RoleIDs:        []string{"organization:org-a:finance"},
		ValidFrom:      now.Add(-time.Minute),
		ValidUntil:     now.Add(time.Hour),
	}
}

func assertInvitationState(t *testing.T, database interface {
	QueryRow(string, ...any) *sql.Row
}, invitationID, status, membershipID string, active, provisioning, acceptances int) {
	t.Helper()
	var actualStatus string
	var actualActive, actualProvisioning, actualAcceptances int
	if err := database.QueryRow(`SELECT status FROM organization_invitations_v2 WHERE id=?`, invitationID).Scan(&actualStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT active,provisioning FROM organization_members WHERE id=?`, membershipID).Scan(&actualActive, &actualProvisioning); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM invitation_acceptances_v2 WHERE invitation_id=?`, invitationID).Scan(&actualAcceptances); err != nil {
		t.Fatal(err)
	}
	if actualStatus != status || actualActive != active || actualProvisioning != provisioning || actualAcceptances != acceptances {
		t.Fatalf("status=%s active=%d provisioning=%d acceptances=%d", actualStatus, actualActive, actualProvisioning, actualAcceptances)
	}
}
