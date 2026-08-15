package infra_test

import (
	"context"
	"testing"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/security/infra"
	"example.com/phan-quyen-golang/internal/shared/casdoortest"
	sharedquota "example.com/phan-quyen-golang/internal/shared/quota"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestExternalGrantTargetsHaveDifferentKickAndRejoinSemantics(t *testing.T) {
	database := testutil.Database(t)
	repository := infra.NewRepository(database)
	if _, err := database.Exec(`INSERT INTO users(id) VALUES('external-user'); INSERT INTO organization_members(id,organization_id,user_id,active) VALUES('member-old','org-b','external-user',1)`); err != nil {
		t.Fatal(err)
	}
	resource := domain.Resource{Type: "invoice", ID: "invoice-partner", TenantID: "org-a"}
	operation := domain.Operation{ResourceType: "invoice", Action: "approve"}
	actor := domain.Actor{ID: "external-user", Type: domain.ActorUser}
	now := time.Now().UTC()
	createGrant(t, repository, externalDefinition("global", domain.ExternalGrantTarget{Type: domain.ExternalTargetGlobalUser, UserID: actor.ID}, now))
	createGrant(t, repository, externalDefinition("member", domain.ExternalGrantTarget{Type: domain.ExternalTargetOrganizationMember, UserID: actor.ID, OrganizationID: "org-b", MembershipID: "member-old"}, now))
	createGrant(t, repository, externalDefinition("organization", domain.ExternalGrantTarget{Type: domain.ExternalTargetOrganization, OrganizationID: "org-b"}, now))
	access := resolveExternal(t, repository, actor, resource, operation, now)
	assertGrantIDs(t, access, "global", "member", "organization")
	if _, err := database.Exec(`UPDATE organization_members SET active=0,left_at=? WHERE id='member-old'`, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	access = resolveExternal(t, repository, actor, resource, operation, now)
	assertGrantIDs(t, access, "global")
	if _, err := database.Exec(`INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES('member-new','org-b','external-user',1,?)`, now.Add(time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	access = resolveExternal(t, repository, actor, resource, operation, now.Add(2*time.Minute))
	assertGrantIDs(t, access, "global", "organization")
}

func TestExternalRoleAndGroupPermissionsUseCasbinSnapshotWithDenyWins(t *testing.T) {
	database := testutil.Database(t)
	directory := casdoortest.NewFakeCasdoor()
	directory.AddGroup(domain.DirectoryObject{ID: "organization:org-a:reviewers", Name: "reviewers", Domains: []string{"organization::org-a"}})
	directory.AddRule(domain.PolicyRule{PType: "g", V0: "group::organization:org-a:reviewers", V1: "role::organization:org-a:finance", V2: "organization::org-a"})
	repository := infra.NewRepository(database, directory)
	now := time.Now().UTC()
	target := domain.ExternalGrantTarget{Type: domain.ExternalTargetGlobalUser, UserID: "user-personal"}
	roleGrant := externalDefinition("role-allow", target, now)
	roleGrant.Permissions = nil
	roleGrant.Roles = []domain.ExternalGrantItem{{Key: "organization:org-a:finance", Effect: domain.EffectAllow}}
	createGrant(t, repository, roleGrant)
	access := resolveExternal(t, repository, domain.Actor{ID: "user-personal", Type: domain.ActorUser}, roleGrant.Resource, roleGrant.Operation, now)
	allowed, err := repository.ExternalPermission(context.Background(), *access, roleGrant.Operation)
	if err != nil || !allowed {
		t.Fatalf("role-derived allowed=%v err=%v", allowed, err)
	}
	groupGrant := externalDefinition("group-deny", target, now)
	groupGrant.Permissions = nil
	groupGrant.Groups = []domain.ExternalGrantItem{{Key: "organization:org-a:reviewers", Effect: domain.EffectDeny}}
	createGrant(t, repository, groupGrant)
	access = resolveExternal(t, repository, domain.Actor{ID: "user-personal", Type: domain.ActorUser}, roleGrant.Resource, roleGrant.Operation, now)
	allowed, err = repository.ExternalPermission(context.Background(), *access, roleGrant.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("group-derived deny must win over role-derived allow")
	}
}

func TestExternalGrantDenyWinsAcrossMatchingGrants(t *testing.T) {
	database := testutil.Database(t)
	repository := infra.NewRepository(database)
	now := time.Now().UTC()
	target := domain.ExternalGrantTarget{Type: domain.ExternalTargetGlobalUser, UserID: "user-personal"}
	createGrant(t, repository, externalDefinition("allow", target, now))
	denied := externalDefinition("deny", target, now)
	denied.Effect = domain.EffectDeny
	createGrant(t, repository, denied)
	access, err := repository.ResolveExternalAccess(context.Background(), domain.Actor{ID: "user-personal", Type: domain.ActorUser}, domain.Resource{Type: "invoice", ID: "invoice-partner", TenantID: "org-a"}, domain.Operation{ResourceType: "invoice", Action: "approve"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if access != nil {
		t.Fatalf("whole-grant deny must win: %+v", access)
	}
}

func TestExternalItemsAndOwnerBackedQuotaUseDenyWins(t *testing.T) {
	database := testutil.Database(t)
	repository := infra.NewRepository(database)
	now := time.Now().UTC()
	definition := externalDefinition("bundle", domain.ExternalGrantTarget{Type: domain.ExternalTargetGlobalUser, UserID: "user-personal"}, now)
	limit := int64(3)
	definition.Features = []domain.ExternalGrantItem{{Key: "invoice_management", Effect: domain.EffectAllow}}
	definition.Quotas = []domain.ExternalGrantItem{{Key: "invoice_approvals", Effect: domain.EffectAllow, Limit: &limit}}
	createGrant(t, repository, definition)
	access := resolveExternal(t, repository, domain.Actor{ID: "user-personal", Type: domain.ActorUser}, domain.Resource{Type: "invoice", ID: "invoice-partner", TenantID: "org-a"}, domain.Operation{ResourceType: "invoice", Action: "approve"}, now)
	allowed, err := repository.ExternalPermission(context.Background(), *access, domain.Operation{ResourceType: "invoice", Action: "approve"})
	if err != nil || !allowed {
		t.Fatalf("permission allowed=%v err=%v", allowed, err)
	}
	allowed, err = repository.ExternalFeature(context.Background(), *access, "invoice_management")
	if err != nil || !allowed {
		t.Fatalf("feature allowed=%v err=%v", allowed, err)
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	operation := sharedquota.Operation{ID: "external-op", ActorID: "user-personal", ResourceType: "invoice", Action: "approve", ExpiresAt: now.Add(time.Minute)}
	cost := domain.QuotaCost{Subject: domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, ExternalGrantIDs: access.GrantIDs, QuotaKey: "invoice_approvals", Cost: 2}
	if err = sharedquota.ConsumeQuotas(context.Background(), tx, []domain.QuotaCost{cost}, operation); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var externalUsed, ownerUsed int64
	if err = database.QueryRow(`SELECT used FROM external_grant_quota_allocations_v2 WHERE grant_id='bundle' AND quota_key='invoice_approvals'`).Scan(&externalUsed); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT used FROM subject_quota_entitlements_v2 WHERE id='organization:org-a:invoice_approvals'`).Scan(&ownerUsed); err != nil {
		t.Fatal(err)
	}
	if externalUsed != 2 || ownerUsed != 2 {
		t.Fatalf("external used=%d owner used=%d", externalUsed, ownerUsed)
	}
	createGrant(t, repository, domain.ExternalGrantDefinition{ID: "feature-deny", OwnerOrganizationID: "org-a", CreatedBy: "user-a", Target: definition.Target, Resource: definition.Resource, Operation: definition.Operation, Effect: domain.EffectAllow, ValidFrom: now.Add(-time.Minute), Features: []domain.ExternalGrantItem{{Key: "invoice_management", Effect: domain.EffectDeny}}})
	access = resolveExternal(t, repository, domain.Actor{ID: "user-personal", Type: domain.ActorUser}, definition.Resource, definition.Operation, now)
	allowed, err = repository.ExternalFeature(context.Background(), *access, "invoice_management")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("feature deny from another matching grant must win")
	}
}

func externalDefinition(id string, target domain.ExternalGrantTarget, now time.Time) domain.ExternalGrantDefinition {
	return domain.ExternalGrantDefinition{ID: id, OwnerOrganizationID: "org-a", CreatedBy: "user-a", Target: target, Resource: domain.Resource{Type: "invoice", ID: "invoice-partner", TenantID: "org-a"}, Operation: domain.Operation{ResourceType: "invoice", Action: "approve"}, Effect: domain.EffectAllow, ValidFrom: now.Add(-time.Minute), Permissions: []domain.ExternalGrantItem{{Key: "invoice.approve", Effect: domain.EffectAllow}}}
}
func createGrant(t *testing.T, r *infra.Repository, g domain.ExternalGrantDefinition) {
	t.Helper()
	if err := r.CreateExternalGrant(context.Background(), g); err != nil {
		t.Fatal(err)
	}
}
func resolveExternal(t *testing.T, r *infra.Repository, a domain.Actor, res domain.Resource, op domain.Operation, at time.Time) *domain.ExternalAccess {
	t.Helper()
	access, err := r.ResolveExternalAccess(context.Background(), a, res, op, at)
	if err != nil {
		t.Fatal(err)
	}
	if access == nil {
		t.Fatal("expected external access")
	}
	return access
}
func assertGrantIDs(t *testing.T, access *domain.ExternalAccess, wanted ...string) {
	t.Helper()
	found := map[string]bool{}
	for _, id := range access.GrantIDs {
		found[id] = true
	}
	for _, id := range wanted {
		if !found[id] {
			t.Fatalf("missing grant %s in %v", id, access.GrantIDs)
		}
	}
	if len(found) != len(wanted) {
		t.Fatalf("grant ids=%v want=%v", access.GrantIDs, wanted)
	}
}
